#include "overlay_internal.h"

#if FANG_OVERLAY
#include "hook.h"

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "_cgo_export.h"

enum {
    overlay_mailbox_free,
    overlay_mailbox_ready,
    overlay_mailbox_taken
};

enum {
    overlay_refused_not_installed = 1,
    overlay_refused_disabled = 2,
    overlay_refused_window = 3,
    overlay_refused_device = 4,
    overlay_refused_present = 5
};

static BYTE* overlay_client_base;
static volatile LONG overlay_is_installed;
static volatile LONG overlay_is_disabled;
static volatile LONG overlay_is_visible;
static volatile LONG overlay_generation;
static volatile LONG overlay_is_keyboard_owned;
static volatile LONG overlay_net_mode;
static HANDLE overlay_net_event;

static char overlay_session_key[OVERLAY_KEY_LENGTH + 1];
static volatile LONG overlay_is_key_published;

static volatile LONG overlay_state_sequence;
static overlay_state overlay_published_state;
static volatile LONG overlay_result_sequence;
static overlay_result overlay_published_result;
static overlay_catalog* volatile overlay_published_catalog;

static volatile LONG overlay_mailbox_state;
static volatile LONG overlay_action_sequence;
static overlay_action overlay_mailbox;

/* Single writer per sequence. Interlocked operations are full barriers, so
 * the copy cannot move outside the odd/even window. */
void overlay_seqlock_write(volatile LONG* sequence, void* destination,
    const void* source, size_t size) {
    InterlockedIncrement(sequence);
    memcpy(destination, source, size);
    InterlockedIncrement(sequence);
}

/* The destination is undefined when this returns 0; callers read into a
 * scratch copy and keep their previous snapshot on failure. */
int overlay_seqlock_read(volatile LONG* sequence, void* destination,
    const void* source, size_t size) {
    int attempt;
    for (attempt = 0; attempt < 64; attempt++) {
        LONG before = InterlockedCompareExchange(sequence, 0, 0);
        if ((before & 1) != 0) {
            YieldProcessor();
            continue;
        }
        memcpy(destination, source, size);
        if (InterlockedCompareExchange(sequence, 0, 0) == before) {
            return 1;
        }
    }
    return 0;
}

void overlay_publish_key(const char* key, unsigned int length) {
    unsigned int index;
    if (key == NULL || length != OVERLAY_KEY_LENGTH ||
        InterlockedCompareExchange(&overlay_is_key_published, 0, 0) != 0) {
        return;
    }
    for (index = 0; index < length; index++) {
        char digit = key[index];
        if (!((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f'))) {
            return;
        }
    }
    memcpy(overlay_session_key, key, OVERLAY_KEY_LENGTH);
    overlay_session_key[OVERLAY_KEY_LENGTH] = '\0';
    InterlockedExchange(&overlay_is_key_published, 1);
}

int overlay_normalize_debug_command(const char* text, const char* command_text,
    char* output, size_t output_capacity) {
    size_t prefix_length;
    int written;
    if (InterlockedCompareExchange(&overlay_is_key_published, 0, 0) == 0 ||
        text == NULL || command_text == NULL || command_text < text ||
        output == NULL || output_capacity == 0) {
        return -1;
    }
    prefix_length = (size_t)(command_text - text);
    if (prefix_length >= output_capacity) {
        return -1;
    }
    if (prefix_length != 0) {
        memcpy(output, text, prefix_length);
    }
    written = snprintf(output + prefix_length, output_capacity - prefix_length,
        "/debug %s", overlay_session_key);
    if (written < 0 || (size_t)written >= output_capacity - prefix_length) {
        return -1;
    }
    return (int)(prefix_length + (size_t)written);
}

void overlay_trace_net(unsigned int trace, unsigned int value) {
    switch (trace) {
    case OVERLAY_TRACE_KEY_LENGTH:
        trace_client_state("overlay_key_length", value);
        return;
    case OVERLAY_TRACE_NET_STATUS:
        trace_client_state("overlay_net_status", value);
        return;
    case OVERLAY_TRACE_CATALOG_STATUS:
        trace_client_state("overlay_net_catalog_status", value);
        return;
    case OVERLAY_TRACE_ACTION_STATUS:
        trace_client_state("overlay_net_action_status", value);
        return;
    case OVERLAY_TRACE_CATALOG_RIGBLOCKS:
        trace_client_state("overlay_net_catalog_rigblocks", value);
        return;
    case OVERLAY_TRACE_NET_RECOVERED:
        trace_client_state("overlay_net_recovered", value);
        return;
    default:
        return;
    }
}

void overlay_net_wait(unsigned int milliseconds) {
    if (overlay_net_event == NULL) {
        Sleep(milliseconds);
        return;
    }
    WaitForSingleObject(overlay_net_event, milliseconds);
}

int is_overlay_open(void) {
    return InterlockedCompareExchange(&overlay_is_visible, 0, 0) != 0;
}

unsigned int overlay_open_generation(void) {
    return (unsigned int)InterlockedCompareExchange(&overlay_generation, 0, 0);
}

unsigned int overlay_current_net_mode(void) {
    return (unsigned int)InterlockedCompareExchange(&overlay_net_mode, 0, 0);
}

void overlay_publish_state(const overlay_state* state) {
    if (state == NULL) {
        return;
    }
    overlay_seqlock_write(&overlay_state_sequence, &overlay_published_state,
        state, sizeof(*state));
}

int overlay_read_state(overlay_state* state) {
    return overlay_seqlock_read(&overlay_state_sequence, state,
        &overlay_published_state, sizeof(*state));
}

/* calloc rather than cgo's C.malloc, which aborts the process on failure;
 * a catalog that cannot be allocated is simply not published. */
overlay_catalog* overlay_allocate_catalog(size_t size) {
    if (size < sizeof(overlay_catalog)) {
        return NULL;
    }
    return (overlay_catalog*)calloc(1, size);
}

void overlay_publish_catalog(overlay_catalog* catalog) {
    if (catalog == NULL) {
        return;
    }
    InterlockedExchangePointer((PVOID volatile*)&overlay_published_catalog, catalog);
}

const overlay_catalog* overlay_current_catalog(void) {
    return (const overlay_catalog*)InterlockedCompareExchangePointer(
        (PVOID volatile*)&overlay_published_catalog, NULL, NULL);
}

/* Render thread only. Returns the action sequence, or 0 while the single
 * mailbox slot is still owned by the network worker; never blocks. */
unsigned int overlay_submit_action(overlay_action* action) {
    LONG sequence;
    if (action == NULL ||
        InterlockedCompareExchange(&overlay_mailbox_state, 0, 0) != overlay_mailbox_free) {
        return 0;
    }
    sequence = InterlockedIncrement(&overlay_action_sequence);
    action->sequence = (unsigned int)sequence;
    overlay_mailbox = *action;
    InterlockedExchange(&overlay_mailbox_state, overlay_mailbox_ready);
    if (overlay_net_event != NULL) {
        SetEvent(overlay_net_event);
    }
    return (unsigned int)sequence;
}

int is_overlay_action_busy(void) {
    return InterlockedCompareExchange(&overlay_mailbox_state, 0, 0) != overlay_mailbox_free;
}

int overlay_take_action(overlay_action* action) {
    if (action == NULL ||
        InterlockedCompareExchange(&overlay_mailbox_state, overlay_mailbox_taken,
            overlay_mailbox_ready) != overlay_mailbox_ready) {
        return 0;
    }
    *action = overlay_mailbox;
    return 1;
}

/* Publishes the result before freeing the slot, so a free mailbox always has
 * its previous outcome visible. */
void overlay_publish_result(const overlay_result* result) {
    if (result != NULL) {
        overlay_seqlock_write(&overlay_result_sequence, &overlay_published_result,
            result, sizeof(*result));
    }
    InterlockedCompareExchange(&overlay_mailbox_state, overlay_mailbox_free,
        overlay_mailbox_taken);
}

int overlay_read_result(overlay_result* result) {
    return overlay_seqlock_read(&overlay_result_sequence, result,
        &overlay_published_result, sizeof(*result));
}

void overlay_set_keyboard_owned(int is_owned) {
    InterlockedExchange(&overlay_is_keyboard_owned, is_owned != 0);
}

/* A consumed Return keeps ownership until its release, because the field
 * submits and drops focus before the Enter poller may sample the press. */
int is_overlay_keyboard_owned(void) {
    return is_overlay_open() &&
        (InterlockedCompareExchange(&overlay_is_keyboard_owned, 0, 0) != 0 ||
            is_overlay_return_consumed());
}

static void open_overlay(HWND window) {
    void* device;
    if (InterlockedCompareExchange(&overlay_is_installed, 0, 0) == 0) {
        trace_client_state("overlay_open_refused", overlay_refused_not_installed);
        return;
    }
    if (InterlockedCompareExchange(&overlay_is_disabled, 0, 0) != 0) {
        trace_client_state("overlay_open_refused", overlay_refused_disabled);
        return;
    }
    if (window == NULL) {
        trace_client_state("overlay_open_refused", overlay_refused_window);
        return;
    }
    device = overlay_acquire_device(overlay_client_base);
    if (device == NULL) {
        trace_client_state("overlay_open_refused", overlay_refused_device);
        return;
    }
    if (!install_overlay_present(device)) {
        trace_client_state("overlay_open_refused", overlay_refused_present);
        return;
    }
    overlay_input_open(window);
    reset_overlay_frame_clock();
    InterlockedIncrement(&overlay_generation);
    InterlockedExchange(&overlay_is_visible, 1);
    if (overlay_net_event != NULL) {
        SetEvent(overlay_net_event);
    }
    trace_client_state("overlay_visible", 1);
}

static void close_overlay(void) {
    InterlockedExchange(&overlay_is_visible, 0);
    InterlockedExchange(&overlay_is_keyboard_owned, 0);
    overlay_input_close();
    trace_client_state("overlay_visible", 0);
}

/* The open overlay reports "type /debug again" only in this state, so that
 * /debug rebinds instead of closing. */
static int is_overlay_unbound(void) {
    overlay_state state;
    if (overlay_current_net_mode() != OVERLAY_NET_STARTED) {
        return 0;
    }
    if (!overlay_read_state(&state)) {
        return 0;
    }
    return state.connection == OVERLAY_CONNECTION_UNBOUND;
}

/* The /debug that triggered this toggle already sent the new bind; a new
 * generation restarts the worker's unbound grace and polls at once. */
static void rebind_overlay(void) {
    InterlockedIncrement(&overlay_generation);
    if (overlay_net_event != NULL) {
        SetEvent(overlay_net_event);
    }
    trace_client_state("overlay_rebind", 1);
}

void overlay_toggle(HWND window) {
    if (!is_overlay_open()) {
        open_overlay(window);
        return;
    }
    if (is_overlay_unbound()) {
        rebind_overlay();
        return;
    }
    close_overlay();
}

void install_overlay(void* executable, const char* hostname, unsigned short port) {
    int net_mode = OVERLAY_NET_IDLE;
    overlay_client_base = (BYTE*)executable;
    if (!install_overlay_draw()) {
        InterlockedExchange(&overlay_is_disabled, 1);
        trace_client_state("overlay_install", 0);
        return;
    }
    overlay_net_event = CreateEventA(NULL, FALSE, FALSE, NULL);
    trace_client_state("overlay_net_event", overlay_net_event != NULL);
    trace_client_state("overlay_capture_hook",
        (unsigned int)install_overlay_device_capture(executable));
    if (hostname != NULL && hostname[0] != '\0' && port != 0) {
        net_mode = GoOverlayStart((char*)hostname, port);
    }
    InterlockedExchange(&overlay_net_mode, net_mode);
    trace_client_state("overlay_net_mode", (unsigned int)net_mode);
    InterlockedExchange(&overlay_is_installed, 1);
    trace_client_state("overlay_install", 1);
}
#endif
