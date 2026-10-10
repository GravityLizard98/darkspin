#include "overlay_internal.h"

#if FANG_OVERLAY
#include "hook.h"

#include <string.h>
#include <windowsx.h>

#define OVERLAY_INPUT_RING_SIZE 256u
#define OVERLAY_LAYOUT_STALE_MS 500u
#define OVERLAY_MOUSE_UNKNOWN ((LONG)0x80008000u)
#define OVERLAY_BUTTON_X1 0x8u
#define OVERLAY_BUTTON_X2 0x10u

enum overlay_input_type {
    overlay_input_mouse_down = 1,
    overlay_input_mouse_up,
    overlay_input_scroll,
    overlay_input_key_down,
    overlay_input_key_up,
    overlay_input_text,
    overlay_input_blur,
    overlay_input_reset
};

typedef struct overlay_input_event {
    int type;
    int button;
    int x;
    int y;
    char text[8];
} overlay_input_event;

/* Window-procedure state; touched only on the game window thread. Masks use
 * microui button bits plus the two X buttons, which only the game uses. */
typedef struct overlay_window_input {
    HWND window;
    int client_width;
    int client_height;
    unsigned int overlay_button_mask;
    unsigned int game_button_mask;
    int is_capture_owned;
    int is_mouse_known;
    int mouse_x;
    int mouse_y;
    unsigned char lead_byte;
    /* Set when the latest key-down went to the game, so the WM_CHAR that
     * TranslateMessage posts for it goes there too. */
    int is_key_passed;
    unsigned char consumed_keys[256 / 8];
} overlay_window_input;

/* Single-producer (window thread) single-consumer (render thread) ring. */
static overlay_input_event overlay_input_events[OVERLAY_INPUT_RING_SIZE];
static volatile LONG overlay_input_head;
static volatile LONG overlay_input_tail;
static volatile LONG overlay_input_dropped;
static volatile LONG overlay_mouse_position = OVERLAY_MOUSE_UNKNOWN;
static volatile LONG overlay_layout_sequence;
static overlay_layout overlay_published_layout;
static overlay_window_input overlay_window;
/* Set while a Return press consumed by a text field is held, so the Enter
 * poller stays suppressed after the field submits and drops focus. */
static volatile LONG overlay_is_return_consumed;

static void push_input(int type, int button, int x, int y, const char* text) {
    LONG head = InterlockedCompareExchange(&overlay_input_head, 0, 0);
    LONG tail = InterlockedCompareExchange(&overlay_input_tail, 0, 0);
    overlay_input_event* event;
    if ((unsigned long)(head - tail) >= OVERLAY_INPUT_RING_SIZE) {
        InterlockedIncrement(&overlay_input_dropped);
        return;
    }
    event = &overlay_input_events[(unsigned long)head % OVERLAY_INPUT_RING_SIZE];
    event->type = type;
    event->button = button;
    event->x = x;
    event->y = y;
    event->text[0] = '\0';
    if (text != NULL) {
        strncpy(event->text, text, sizeof(event->text) - 1);
        event->text[sizeof(event->text) - 1] = '\0';
    }
    InterlockedExchange(&overlay_input_head, head + 1);
}

static void publish_mouse_position(int x, int y) {
    overlay_window.is_mouse_known = 1;
    overlay_window.mouse_x = x;
    overlay_window.mouse_y = y;
    InterlockedExchange(&overlay_mouse_position,
        (LONG)(((unsigned long)(x & 0xFFFF) << 16) | (unsigned long)(y & 0xFFFF)));
}

static void reset_microui_input(mu_Context* context) {
    mu_input_mouseup(context, context->mouse_pos.x, context->mouse_pos.y,
        MU_MOUSE_LEFT | MU_MOUSE_RIGHT | MU_MOUSE_MIDDLE);
    mu_input_keyup(context, MU_KEY_SHIFT | MU_KEY_CTRL | MU_KEY_ALT |
        MU_KEY_BACKSPACE | MU_KEY_RETURN);
    mu_set_focus(context, 0);
}

/* Render thread, before mu_begin. A press or an edit key ends the batch so it
 * is seen at its own position and frame; text that would overflow microui's
 * 32-byte input buffer waits for the next frame. */
void overlay_input_drain(mu_Context* context) {
    LONG head = InterlockedCompareExchange(&overlay_input_head, 0, 0);
    LONG tail = InterlockedCompareExchange(&overlay_input_tail, 0, 0);
    LONG position;
    int is_stopped = 0;
    if (InterlockedExchange(&overlay_input_dropped, 0) != 0) {
        reset_microui_input(context);
    }
    while (tail != head && !is_stopped) {
        const overlay_input_event* event =
            &overlay_input_events[(unsigned long)tail % OVERLAY_INPUT_RING_SIZE];
        if (event->type == overlay_input_text &&
            strlen(context->input_text) + strlen(event->text) + 1 > sizeof(context->input_text)) {
            is_stopped = 1;
            break;
        }
        if (event->type == overlay_input_mouse_down &&
            (event->x != context->last_mouse_pos.x || event->y != context->last_mouse_pos.y)) {
            /* microui sets hover only on a frame with no button down: lay out
             * one frame at the press point before applying the press. */
            mu_input_mousemove(context, event->x, event->y);
            is_stopped = 1;
            break;
        }
        switch (event->type) {
        case overlay_input_mouse_down:
            mu_input_mousedown(context, event->x, event->y, event->button);
            is_stopped = 1;
            break;
        case overlay_input_mouse_up:
            mu_input_mouseup(context, event->x, event->y, event->button);
            break;
        case overlay_input_scroll:
            mu_input_scroll(context, 0, event->y);
            break;
        case overlay_input_key_down:
            mu_input_keydown(context, event->button);
            is_stopped = (event->button & (MU_KEY_BACKSPACE | MU_KEY_RETURN)) != 0;
            break;
        case overlay_input_key_up:
            mu_input_keyup(context, event->button);
            break;
        case overlay_input_text:
            mu_input_text(context, event->text);
            break;
        case overlay_input_blur:
            mu_set_focus(context, 0);
            break;
        case overlay_input_reset:
            reset_microui_input(context);
            break;
        default:
            break;
        }
        tail++;
    }
    InterlockedExchange(&overlay_input_tail, tail);
    if (is_stopped) {
        return;
    }
    position = InterlockedCompareExchange(&overlay_mouse_position, 0, 0);
    if (position != OVERLAY_MOUSE_UNKNOWN) {
        mu_input_mousemove(context, (short)((unsigned long)position >> 16),
            (short)((unsigned long)position & 0xFFFF));
    }
}

void overlay_input_publish(const overlay_layout* layout) {
    overlay_seqlock_write(&overlay_layout_sequence, &overlay_published_layout,
        layout, sizeof(*layout));
}

/* A layout counts only while frames are being drawn; otherwise nothing is
 * consumed and the game keeps its input. */
static int read_layout(overlay_layout* layout) {
    if (!overlay_seqlock_read(&overlay_layout_sequence, layout,
        &overlay_published_layout, sizeof(*layout))) {
        return 0;
    }
    return layout->scale > 0 && layout->back_buffer_width > 0 &&
        layout->back_buffer_height > 0 &&
        GetTickCount() - layout->published_tick <= OVERLAY_LAYOUT_STALE_MS;
}

static void refresh_client_size(HWND window) {
    RECT client;
    if (window == NULL || !GetClientRect(window, &client)) {
        return;
    }
    overlay_window.client_width = client.right - client.left;
    overlay_window.client_height = client.bottom - client.top;
}

/* client -> back buffer -> UI units. Present's destination rectangle, when
 * the game passes one, replaces the client area. */
static int map_client_point(const overlay_layout* layout, int client_x, int client_y,
    int* ui_x, int* ui_y) {
    int origin_x = 0;
    int origin_y = 0;
    int width = overlay_window.client_width;
    int height = overlay_window.client_height;
    long long x;
    long long y;
    if (layout->is_destination) {
        origin_x = layout->destination.left;
        origin_y = layout->destination.top;
        width = layout->destination.right - layout->destination.left;
        height = layout->destination.bottom - layout->destination.top;
    }
    if (width <= 0 || height <= 0) {
        return 0;
    }
    x = (long long)(client_x - origin_x) * layout->back_buffer_width / width / layout->scale;
    y = (long long)(client_y - origin_y) * layout->back_buffer_height / height / layout->scale;
    if (x < -32767 || x > 32767 || y < -32767 || y > 32767) {
        return 0;
    }
    *ui_x = (int)x;
    *ui_y = (int)y;
    return 1;
}

static int is_inside_layout(const overlay_layout* layout, int x, int y) {
    int index;
    for (index = 0; index < layout->rect_count && index < OVERLAY_RECT_LIMIT; index++) {
        mu_Rect rect = layout->rects[index];
        if (x >= rect.x && x < rect.x + rect.w && y >= rect.y && y < rect.y + rect.h) {
            return 1;
        }
    }
    return 0;
}

static void release_capture(void) {
    if (!overlay_window.is_capture_owned) {
        return;
    }
    /* Cleared first: ReleaseCapture sends WM_CAPTURECHANGED synchronously. */
    overlay_window.is_capture_owned = 0;
    ReleaseCapture();
}

/* Focus loss: drop every press, drag and text focus on both sides. */
static void release_overlay_input(void) {
    overlay_window.overlay_button_mask = 0;
    overlay_window.game_button_mask = 0;
    overlay_window.lead_byte = 0;
    overlay_window.is_key_passed = 0;
    memset(overlay_window.consumed_keys, 0, sizeof(overlay_window.consumed_keys));
    InterlockedExchange(&overlay_is_return_consumed, 0);
    release_capture();
    push_input(overlay_input_reset, 0, 0, 0, NULL);
}

static unsigned int pressed_button_mask(WPARAM wparam) {
    unsigned int mask = 0;
    WORD keys = GET_KEYSTATE_WPARAM(wparam);
    if ((keys & MK_LBUTTON) != 0) {
        mask |= MU_MOUSE_LEFT;
    }
    if ((keys & MK_RBUTTON) != 0) {
        mask |= MU_MOUSE_RIGHT;
    }
    if ((keys & MK_MBUTTON) != 0) {
        mask |= MU_MOUSE_MIDDLE;
    }
    if ((keys & MK_XBUTTON1) != 0) {
        mask |= OVERLAY_BUTTON_X1;
    }
    if ((keys & MK_XBUTTON2) != 0) {
        mask |= OVERLAY_BUTTON_X2;
    }
    return mask;
}

/* Buttons released while the overlay could not see them must not keep a
 * drag alive; every mouse message carries the true button state. */
static void sync_button_masks(WPARAM wparam) {
    unsigned int pressed = pressed_button_mask(wparam);
    overlay_window.overlay_button_mask &= pressed;
    overlay_window.game_button_mask &= pressed;
}

static int map_message_point(LPARAM lparam, int* x, int* y) {
    overlay_layout layout;
    if (!read_layout(&layout) ||
        !map_client_point(&layout, GET_X_LPARAM(lparam), GET_Y_LPARAM(lparam), x, y)) {
        return 0;
    }
    publish_mouse_position(*x, *y);
    return is_inside_layout(&layout, *x, *y) ? 2 : 1;
}

static int handle_mouse_move(WPARAM wparam, LPARAM lparam) {
    int x;
    int y;
    int placement;
    sync_button_masks(wparam);
    placement = map_message_point(lparam, &x, &y);
    if (overlay_window.game_button_mask != 0) {
        return 0;
    }
    if (overlay_window.overlay_button_mask != 0) {
        return 1;
    }
    return placement == 2;
}

static int handle_button_down(HWND window, unsigned int button, WPARAM wparam, LPARAM lparam) {
    int x;
    int y;
    int placement;
    sync_button_masks(wparam);
    placement = map_message_point(lparam, &x, &y);
    if (overlay_window.game_button_mask != 0 || placement != 2) {
        overlay_window.game_button_mask |= button;
        /* A press the game receives ends overlay text entry, as an outside
         * press would in microui; keys then pass through again. */
        push_input(overlay_input_blur, 0, 0, 0, NULL);
        return 0;
    }
    overlay_window.overlay_button_mask |= button;
    if ((button & (MU_MOUSE_LEFT | MU_MOUSE_RIGHT | MU_MOUSE_MIDDLE)) != 0) {
        push_input(overlay_input_mouse_down, (int)button, x, y, NULL);
    }
    if (!overlay_window.is_capture_owned && GetCapture() == NULL) {
        SetCapture(window);
        overlay_window.is_capture_owned = 1;
    }
    return 1;
}

/* An up is the overlay's only when its down was. */
static int handle_button_up(unsigned int button, LPARAM lparam) {
    int x;
    int y;
    if ((overlay_window.overlay_button_mask & button) == 0) {
        overlay_window.game_button_mask &= ~button;
        return 0;
    }
    overlay_window.overlay_button_mask &= ~button;
    if (map_message_point(lparam, &x, &y) == 0) {
        x = overlay_window.mouse_x;
        y = overlay_window.mouse_y;
    }
    if ((button & (MU_MOUSE_LEFT | MU_MOUSE_RIGHT | MU_MOUSE_MIDDLE)) != 0) {
        push_input(overlay_input_mouse_up, (int)button, x, y, NULL);
    }
    if (overlay_window.overlay_button_mask == 0) {
        release_capture();
    }
    return 1;
}

/* WM_MOUSEWHEEL carries screen coordinates: hit-test the last mapped
 * position and pass only the delta. */
static int handle_wheel(WPARAM wparam) {
    overlay_layout layout;
    int delta = GET_WHEEL_DELTA_WPARAM(wparam);
    if (overlay_window.game_button_mask != 0 || !overlay_window.is_mouse_known ||
        !read_layout(&layout) ||
        !is_inside_layout(&layout, overlay_window.mouse_x, overlay_window.mouse_y)) {
        return 0;
    }
    push_input(overlay_input_scroll, 0, 0, -delta * 30 / WHEEL_DELTA, NULL);
    return 1;
}

static int is_text_field_focused(void) {
    overlay_layout layout;
    return read_layout(&layout) && layout.is_text_focused;
}

/* Shift is not forwarded: microui uses it only for Shift+click number editing,
 * whose text focus the overlay does not report as keyboard ownership. */
static int microui_key(WPARAM wparam) {
    switch (wparam) {
    case VK_BACK:
        return MU_KEY_BACKSPACE;
    case VK_RETURN:
        return MU_KEY_RETURN;
    case VK_CONTROL:
        return MU_KEY_CTRL;
    case VK_MENU:
        return MU_KEY_ALT;
    default:
        return 0;
    }
}

static int handle_key_down(UINT message, WPARAM wparam, LPARAM lparam) {
    int key = microui_key(wparam);
    unsigned char bit;
    int is_repeat;
    int is_consumed;
    int is_focused;
    overlay_window.is_key_passed = 1;
    /* Alt+F4 and Alt+Enter keep their window behavior while typing. */
    if (message == WM_SYSKEYDOWN && (wparam == VK_F4 || wparam == VK_RETURN)) {
        return 0;
    }
    if (wparam > 0xFF) {
        return 0;
    }
    bit = (unsigned char)(1u << (wparam % 8));
    is_repeat = (lparam & (1L << 30)) != 0;
    is_consumed = (overlay_window.consumed_keys[wparam / 8] & bit) != 0;
    /* A repeat stays with whichever side received its first down. */
    if (is_repeat && !is_consumed) {
        return 0;
    }
    is_focused = is_text_field_focused();
    if (!is_focused && !is_repeat) {
        /* The game receives this down, so it also owns the matching up. */
        overlay_window.consumed_keys[wparam / 8] &= (unsigned char)~bit;
        if (wparam == VK_RETURN) {
            InterlockedExchange(&overlay_is_return_consumed, 0);
        }
        return 0;
    }
    overlay_window.is_key_passed = 0;
    if (!is_focused) {
        /* A consumed key stays the overlay's until its up, even after its
         * field lost focus. */
        return 1;
    }
    overlay_window.consumed_keys[wparam / 8] |= bit;
    if (wparam == VK_RETURN) {
        /* Full barrier before the push: the render thread cannot drop
         * keyboard ownership for this Return before the latch is visible. */
        InterlockedExchange(&overlay_is_return_consumed, 1);
    }
    if (wparam == VK_ESCAPE) {
        push_input(overlay_input_blur, 0, 0, 0, NULL);
    } else if (key != 0) {
        push_input(overlay_input_key_down, key, 0, 0, NULL);
    }
    return 1;
}

/* An up is consumed only when its down was, so keys held before a text
 * field took focus are still released in the game. */
static int handle_key_up(WPARAM wparam) {
    int key = microui_key(wparam);
    unsigned char bit;
    if (wparam > 0xFF) {
        return 0;
    }
    bit = (unsigned char)(1u << (wparam % 8));
    if ((overlay_window.consumed_keys[wparam / 8] & bit) == 0) {
        return 0;
    }
    overlay_window.consumed_keys[wparam / 8] &= (unsigned char)~bit;
    if (wparam == VK_RETURN) {
        InterlockedExchange(&overlay_is_return_consumed, 0);
    }
    if (key != 0) {
        push_input(overlay_input_key_up, key, 0, 0, NULL);
    }
    return 1;
}

int is_overlay_return_consumed(void) {
    return InterlockedCompareExchange(&overlay_is_return_consumed, 0, 0) != 0;
}

/* The game window procedure is the ANSI variant: convert each character
 * (or DBCS pair) from the ANSI code page to UTF-8 for microui. */
static int handle_character(WPARAM wparam) {
    char ansi[2];
    WCHAR wide[2];
    char utf8[8];
    int ansi_length = 1;
    int wide_length;
    int utf8_length;
    unsigned char character = (unsigned char)(wparam & 0xFF);
    if (!is_text_field_focused()) {
        overlay_window.lead_byte = 0;
        return 0;
    }
    /* TranslateMessage posts the character, so it arrives right after its
     * key-down and follows that key-down to the game. */
    if (overlay_window.is_key_passed) {
        overlay_window.lead_byte = 0;
        return 0;
    }
    if (overlay_window.lead_byte != 0) {
        ansi[0] = (char)overlay_window.lead_byte;
        ansi[1] = (char)character;
        ansi_length = 2;
        overlay_window.lead_byte = 0;
    } else if (IsDBCSLeadByte(character)) {
        overlay_window.lead_byte = character;
        return 1;
    } else {
        ansi[0] = (char)character;
    }
    if (ansi_length == 1 && (character < 32 || character == 127)) {
        /* Enter, Backspace, Tab and Escape arrive as keys. */
        return 1;
    }
    wide_length = MultiByteToWideChar(CP_ACP, 0, ansi, ansi_length, wide, 2);
    if (wide_length <= 0) {
        return 1;
    }
    utf8_length = WideCharToMultiByte(CP_UTF8, 0, wide, wide_length, utf8,
        (int)sizeof(utf8) - 1, NULL, NULL);
    if (utf8_length <= 0) {
        return 1;
    }
    utf8[utf8_length] = '\0';
    push_input(overlay_input_text, 0, 0, 0, utf8);
    return 1;
}

/* First statement of the game window procedure. Never consumes the chat,
 * reset and toggle messages, timers or raw input. */
int overlay_window_message(HWND window, UINT message, WPARAM wparam, LPARAM lparam) {
    if (message == CHAT_OPEN_MESSAGE || message == CHAT_RESET_MESSAGE ||
        message == OVERLAY_TOGGLE_MESSAGE || message == WM_TIMER || message == WM_INPUT ||
        !is_overlay_open()) {
        return 0;
    }
    switch (message) {
    case WM_SIZE:
        refresh_client_size(window);
        return 0;
    case WM_KILLFOCUS:
        release_overlay_input();
        return 0;
    case WM_ACTIVATEAPP:
        if (wparam == FALSE) {
            release_overlay_input();
        }
        return 0;
    case WM_CAPTURECHANGED:
        if (overlay_window.is_capture_owned) {
            overlay_window.is_capture_owned = 0;
            release_overlay_input();
        }
        return 0;
    case WM_MOUSEMOVE:
        return handle_mouse_move(wparam, lparam);
    case WM_LBUTTONDOWN:
    case WM_LBUTTONDBLCLK:
        return handle_button_down(window, MU_MOUSE_LEFT, wparam, lparam);
    case WM_RBUTTONDOWN:
    case WM_RBUTTONDBLCLK:
        return handle_button_down(window, MU_MOUSE_RIGHT, wparam, lparam);
    case WM_MBUTTONDOWN:
    case WM_MBUTTONDBLCLK:
        return handle_button_down(window, MU_MOUSE_MIDDLE, wparam, lparam);
    case WM_XBUTTONDOWN:
    case WM_XBUTTONDBLCLK:
        return handle_button_down(window, GET_XBUTTON_WPARAM(wparam) == XBUTTON1 ?
            OVERLAY_BUTTON_X1 : OVERLAY_BUTTON_X2, wparam, lparam);
    case WM_LBUTTONUP:
        return handle_button_up(MU_MOUSE_LEFT, lparam);
    case WM_RBUTTONUP:
        return handle_button_up(MU_MOUSE_RIGHT, lparam);
    case WM_MBUTTONUP:
        return handle_button_up(MU_MOUSE_MIDDLE, lparam);
    case WM_XBUTTONUP:
        return handle_button_up(GET_XBUTTON_WPARAM(wparam) == XBUTTON1 ?
            OVERLAY_BUTTON_X1 : OVERLAY_BUTTON_X2, lparam);
    case WM_MOUSEWHEEL:
        return handle_wheel(wparam);
    case WM_KEYDOWN:
    case WM_SYSKEYDOWN:
        return handle_key_down(message, wparam, lparam);
    case WM_KEYUP:
    case WM_SYSKEYUP:
        return handle_key_up(wparam);
    case WM_CHAR:
        return handle_character(wparam);
    default:
        return 0;
    }
}

/* Window thread, when the overlay opens: start from a clean input state. */
void overlay_input_open(HWND window) {
    overlay_window.window = window;
    overlay_window.overlay_button_mask = 0;
    overlay_window.game_button_mask = 0;
    overlay_window.is_mouse_known = 0;
    overlay_window.lead_byte = 0;
    overlay_window.is_key_passed = 0;
    memset(overlay_window.consumed_keys, 0, sizeof(overlay_window.consumed_keys));
    InterlockedExchange(&overlay_is_return_consumed, 0);
    refresh_client_size(window);
    InterlockedExchange(&overlay_mouse_position, OVERLAY_MOUSE_UNKNOWN);
    push_input(overlay_input_reset, 0, 0, 0, NULL);
}

/* Window thread, when the overlay closes: return capture and presses. */
void overlay_input_close(void) {
    overlay_window.overlay_button_mask = 0;
    overlay_window.game_button_mask = 0;
    overlay_window.lead_byte = 0;
    overlay_window.is_key_passed = 0;
    memset(overlay_window.consumed_keys, 0, sizeof(overlay_window.consumed_keys));
    InterlockedExchange(&overlay_is_return_consumed, 0);
    release_capture();
    push_input(overlay_input_reset, 0, 0, 0, NULL);
}
#endif
