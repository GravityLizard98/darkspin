/* C COM call macros; must precede the first Windows header. */
#define COBJMACROS
#include "overlay_internal.h"

#if FANG_OVERLAY
#include "hook.h"

#include <d3d9.h>
#include <stddef.h>
#include <string.h>

#include "thirdparty/stb_easy_font.h"

typedef HRESULT (WINAPI* overlay_device_present_fn)(IDirect3DDevice9* device,
    const RECT* source, const RECT* destination, HWND window, const RGNDATA* dirty_region);

#define OVERLAY_DEVICE_PRESENT_SLOT (offsetof(IDirect3DDevice9Vtbl, Present) / sizeof(void*))
#define OVERLAY_VERTEX_FORMAT (D3DFVF_XYZRHW | D3DFVF_DIFFUSE)
#define OVERLAY_QUAD_LIMIT 4096u
#define OVERLAY_GLYPH_LIMIT 255
#define OVERLAY_GLYPH_BUFFER_SIZE (256 * 640)
#define OVERLAY_SCALE_LIMIT 8

typedef char overlay_device_present_slot_check[(OVERLAY_DEVICE_PRESENT_SLOT == 17) ? 1 : -1];

typedef struct overlay_vertex {
    float x;
    float y;
    float z;
    float rhw;
    D3DCOLOR color;
} overlay_vertex;

/* Per-frame drawing state owned by the render thread. Everything is drawn
 * from user memory; no device resource outlives a frame, so Reset needs no
 * handling here. */
typedef struct overlay_batch {
    IDirect3DDevice9* device;
    int scale;
    int width;
    int height;
    unsigned int quad_count;
} overlay_batch;

typedef struct overlay_clock {
    LARGE_INTEGER frequency;
    LONGLONG window_start;
    unsigned int frame_count;
    float frame_ms;
    unsigned int frame_rate;
} overlay_clock;

static overlay_vertex overlay_vertices[OVERLAY_QUAD_LIMIT * 4];
static WORD overlay_indices[OVERLAY_QUAD_LIMIT * 6];
static char overlay_glyph_buffer[OVERLAY_GLYPH_BUFFER_SIZE];
static mu_Context overlay_context;
static overlay_clock overlay_frame_clock;
static volatile LONG overlay_is_clock_reset;
static DWORD overlay_present_tls = TLS_OUT_OF_INDEXES;
static overlay_device_present_fn original_overlay_device_present;
static volatile LONG overlay_is_device_lost;
static volatile LONG overlay_is_frame_traced;

/* stb_easy_font indexes its glyph table by (character - 32) and reads one
 * entry past it, so only printable ASCII may reach it. */
static void copy_glyph_text(char* output, size_t capacity, const char* text, int length) {
    size_t index = 0;
    if (text != NULL) {
        while (index + 1 < capacity && (length < 0 || index < (size_t)length) &&
            text[index] != '\0') {
            unsigned char character = (unsigned char)text[index];
            output[index] = (character >= 32 && character <= 126) ? (char)character : '?';
            index++;
        }
    }
    output[index] = '\0';
}

/* microui layout units are unscaled; only the renderer applies the scale.
 * A bounded copy is measured for both terminated and counted strings. */
static int overlay_text_width(mu_Font font, const char* text, int length) {
    char glyphs[OVERLAY_GLYPH_LIMIT + 1];
    (void)font;
    copy_glyph_text(glyphs, sizeof(glyphs), text, length);
    return stb_easy_font_width(glyphs);
}

/* stb_easy_font's unscaled line pitch. */
static int overlay_text_height(mu_Font font) {
    (void)font;
    return 12;
}

static D3DCOLOR overlay_color(mu_Color color) {
    return D3DCOLOR_ARGB(color.a, color.r, color.g, color.b);
}

static void flush_batch(overlay_batch* batch) {
    if (batch->quad_count == 0) {
        return;
    }
    IDirect3DDevice9_DrawIndexedPrimitiveUP(batch->device, D3DPT_TRIANGLELIST, 0,
        batch->quad_count * 4, batch->quad_count * 2, overlay_indices, D3DFMT_INDEX16,
        overlay_vertices, sizeof(overlay_vertex));
    batch->quad_count = 0;
}

/* Corners are in unscaled units, clockwise from the top-left. The -0.5
 * offset maps texel centers for XYZRHW geometry. */
static void push_quad(overlay_batch* batch, const float* xs, const float* ys, D3DCOLOR color) {
    overlay_vertex* vertex;
    int corner;
    if (batch->quad_count == OVERLAY_QUAD_LIMIT) {
        flush_batch(batch);
    }
    vertex = overlay_vertices + batch->quad_count * 4;
    for (corner = 0; corner < 4; corner++) {
        vertex[corner].x = xs[corner] * (float)batch->scale - 0.5f;
        vertex[corner].y = ys[corner] * (float)batch->scale - 0.5f;
        vertex[corner].z = 0.0f;
        vertex[corner].rhw = 1.0f;
        vertex[corner].color = color;
    }
    batch->quad_count++;
}

static void push_rect(overlay_batch* batch, mu_Rect rect, D3DCOLOR color) {
    float xs[4];
    float ys[4];
    if (rect.w <= 0 || rect.h <= 0) {
        return;
    }
    xs[0] = xs[3] = (float)rect.x;
    xs[1] = xs[2] = (float)(rect.x + rect.w);
    ys[0] = ys[1] = (float)rect.y;
    ys[2] = ys[3] = (float)(rect.y + rect.h);
    push_quad(batch, xs, ys, color);
}

static void push_text(overlay_batch* batch, const char* text, mu_Vec2 position, D3DCOLOR color) {
    char glyphs[OVERLAY_GLYPH_LIMIT + 1];
    int quad_count;
    int quad;
    copy_glyph_text(glyphs, sizeof(glyphs), text, -1);
    quad_count = stb_easy_font_print((float)position.x, (float)position.y, glyphs, NULL,
        overlay_glyph_buffer, sizeof(overlay_glyph_buffer));
    for (quad = 0; quad < quad_count; quad++) {
        float xs[4];
        float ys[4];
        int corner;
        /* stb vertices: x, y, z floats and four color bytes, 16 bytes each. */
        for (corner = 0; corner < 4; corner++) {
            const char* source = overlay_glyph_buffer + quad * 64 + corner * 16;
            memcpy(&xs[corner], source, sizeof(float));
            memcpy(&ys[corner], source + 4, sizeof(float));
        }
        push_quad(batch, xs, ys, color);
    }
}

static void push_icon(overlay_batch* batch, int icon, mu_Rect rect, mu_Color color) {
    const char* glyph;
    mu_Vec2 position;
    switch (icon) {
    case MU_ICON_CHECK:
        push_rect(batch, mu_rect(rect.x + 4, rect.y + 4, rect.w - 8, rect.h - 8),
            overlay_color(color));
        return;
    case MU_ICON_CLOSE:
        glyph = "x";
        break;
    case MU_ICON_COLLAPSED:
        glyph = "+";
        break;
    case MU_ICON_EXPANDED:
        glyph = "-";
        break;
    default:
        return;
    }
    position.x = rect.x + (rect.w - overlay_text_width(NULL, glyph, -1)) / 2;
    position.y = rect.y + (rect.h - overlay_text_height(NULL)) / 2;
    push_text(batch, glyph, position, overlay_color(color));
}

static LONG clamp_pixel(long long coordinate, int limit) {
    if (coordinate < 0) {
        return 0;
    }
    if (coordinate > limit) {
        return limit;
    }
    return (LONG)coordinate;
}

static void apply_clip(overlay_batch* batch, mu_Rect rect) {
    RECT scissor;
    flush_batch(batch);
    scissor.left = clamp_pixel((long long)rect.x * batch->scale, batch->width);
    scissor.top = clamp_pixel((long long)rect.y * batch->scale, batch->height);
    scissor.right = clamp_pixel((long long)(rect.x + rect.w) * batch->scale, batch->width);
    scissor.bottom = clamp_pixel((long long)(rect.y + rect.h) * batch->scale, batch->height);
    IDirect3DDevice9_SetScissorRect(batch->device, &scissor);
}

static int is_point_in_layout(const overlay_layout* layout, mu_Vec2 point) {
    int index;
    for (index = 0; index < layout->rect_count; index++) {
        mu_Rect rect = layout->rects[index];
        if (point.x >= rect.x && point.x < rect.x + rect.w &&
            point.y >= rect.y && point.y < rect.y + rect.h) {
            return 1;
        }
    }
    return 0;
}

static void render_commands(overlay_batch* batch, const overlay_layout* layout) {
    mu_Command* command = NULL;
    while (mu_next_command(&overlay_context, &command)) {
        switch (command->type) {
        case MU_COMMAND_RECT:
            push_rect(batch, command->rect.rect, overlay_color(command->rect.color));
            break;
        case MU_COMMAND_TEXT:
            push_text(batch, command->text.str, command->text.pos,
                overlay_color(command->text.color));
            break;
        case MU_COMMAND_ICON:
            push_icon(batch, command->icon.id, command->icon.rect, command->icon.color);
            break;
        case MU_COMMAND_CLIP:
            apply_clip(batch, command->clip.rect);
            break;
        default:
            break;
        }
    }
    /* A small pointer marker over the panel, in case the game cursor is
     * drawn into the frame beneath the overlay. */
    if (is_point_in_layout(layout, overlay_context.mouse_pos)) {
        mu_Vec2 mouse = overlay_context.mouse_pos;
        apply_clip(batch, mu_rect(0, 0, batch->width, batch->height));
        push_rect(batch, mu_rect(mouse.x - 1, mouse.y - 1, 4, 4), D3DCOLOR_ARGB(255, 0, 0, 0));
        push_rect(batch, mu_rect(mouse.x, mouse.y, 2, 2), D3DCOLOR_ARGB(255, 255, 255, 255));
    }
    flush_batch(batch);
}

/* QueryPerformanceCounter deltas between overlay Present calls, averaged
 * over windows of at least one second. Independent of App::Update. */
static void update_frame_clock(void) {
    overlay_clock* clock = &overlay_frame_clock;
    LARGE_INTEGER now;
    LONGLONG elapsed;
    if (clock->frequency.QuadPart <= 0 || !QueryPerformanceCounter(&now)) {
        return;
    }
    if (InterlockedExchange(&overlay_is_clock_reset, 0) != 0 || clock->window_start == 0) {
        clock->window_start = now.QuadPart;
        clock->frame_count = 0;
        clock->frame_ms = 0.0f;
        clock->frame_rate = 0;
        return;
    }
    clock->frame_count++;
    elapsed = now.QuadPart - clock->window_start;
    if (elapsed < clock->frequency.QuadPart) {
        return;
    }
    clock->frame_ms = (float)((double)elapsed * 1000.0 /
        (double)clock->frequency.QuadPart / (double)clock->frame_count);
    clock->frame_rate = (unsigned int)((double)clock->frame_count *
        (double)clock->frequency.QuadPart / (double)elapsed + 0.5);
    clock->window_start = now.QuadPart;
    clock->frame_count = 0;
}

void reset_overlay_frame_clock(void) {
    InterlockedExchange(&overlay_is_clock_reset, 1);
}

/* Integer UI scale: one unit per pixel up to 1620 lines, plus the window's
 * -/+ override. */
static int overlay_scale(UINT back_buffer_height, int* automatic_scale) {
    int automatic = (int)((back_buffer_height + 540) / 1080);
    int scale;
    if (automatic < 1) {
        automatic = 1;
    }
    *automatic_scale = automatic;
    scale = automatic + overlay_ui_scale_delta();
    if (scale < 1) {
        return 1;
    }
    if (scale > OVERLAY_SCALE_LIMIT) {
        return OVERLAY_SCALE_LIMIT;
    }
    return scale;
}

/* Drains input, lays out the UI and publishes what the input filter needs.
 * Touches no device state. */
static void run_overlay_ui(const D3DSURFACE_DESC* description, const RECT* destination,
    overlay_batch* batch, overlay_layout* layout) {
    overlay_frame frame;
    int is_text_focused = 0;
    int index;
    batch->scale = overlay_scale(description->Height, &frame.automatic_scale);
    batch->width = (int)description->Width;
    batch->height = (int)description->Height;
    frame.scale = batch->scale;
    frame.width = batch->width / batch->scale;
    frame.height = batch->height / batch->scale;
    frame.frame_ms = overlay_frame_clock.frame_ms;
    frame.frame_rate = overlay_frame_clock.frame_rate;
    overlay_input_drain(&overlay_context);
    mu_begin(&overlay_context);
    overlay_build_ui(&overlay_context, &frame, &is_text_focused);
    mu_end(&overlay_context);
    memset(layout, 0, sizeof(*layout));
    layout->published_tick = GetTickCount();
    layout->back_buffer_width = batch->width;
    layout->back_buffer_height = batch->height;
    layout->scale = batch->scale;
    layout->is_text_focused = is_text_focused;
    if (destination != NULL) {
        layout->is_destination = 1;
        layout->destination = *destination;
    }
    for (index = 0; index < overlay_context.root_list.idx &&
        layout->rect_count < OVERLAY_RECT_LIMIT; index++) {
        layout->rects[layout->rect_count++] = overlay_context.root_list.items[index]->rect;
    }
    overlay_input_publish(layout);
    overlay_set_keyboard_owned(is_text_focused);
}

static void set_overlay_render_state(IDirect3DDevice9* device, const overlay_batch* batch) {
    D3DVIEWPORT9 viewport;
    RECT scissor;
    viewport.X = 0;
    viewport.Y = 0;
    viewport.Width = (DWORD)batch->width;
    viewport.Height = (DWORD)batch->height;
    viewport.MinZ = 0.0f;
    viewport.MaxZ = 1.0f;
    IDirect3DDevice9_SetViewport(device, &viewport);
    IDirect3DDevice9_SetVertexShader(device, NULL);
    IDirect3DDevice9_SetPixelShader(device, NULL);
    IDirect3DDevice9_SetFVF(device, OVERLAY_VERTEX_FORMAT);
    IDirect3DDevice9_SetTexture(device, 0, NULL);
    IDirect3DDevice9_SetTextureStageState(device, 0, D3DTSS_COLOROP, D3DTOP_SELECTARG1);
    IDirect3DDevice9_SetTextureStageState(device, 0, D3DTSS_COLORARG1, D3DTA_DIFFUSE);
    IDirect3DDevice9_SetTextureStageState(device, 0, D3DTSS_ALPHAOP, D3DTOP_SELECTARG1);
    IDirect3DDevice9_SetTextureStageState(device, 0, D3DTSS_ALPHAARG1, D3DTA_DIFFUSE);
    IDirect3DDevice9_SetTextureStageState(device, 1, D3DTSS_COLOROP, D3DTOP_DISABLE);
    IDirect3DDevice9_SetTextureStageState(device, 1, D3DTSS_ALPHAOP, D3DTOP_DISABLE);
    IDirect3DDevice9_SetStreamSourceFreq(device, 0, 1);
    IDirect3DDevice9_SetRenderState(device, D3DRS_ZENABLE, D3DZB_FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_ZWRITEENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_STENCILENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_ALPHATESTENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_FOGENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_LIGHTING, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_SRGBWRITEENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_CULLMODE, D3DCULL_NONE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_FILLMODE, D3DFILL_SOLID);
    IDirect3DDevice9_SetRenderState(device, D3DRS_COLORWRITEENABLE, 0xF);
    IDirect3DDevice9_SetRenderState(device, D3DRS_ALPHABLENDENABLE, TRUE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_SEPARATEALPHABLENDENABLE, FALSE);
    IDirect3DDevice9_SetRenderState(device, D3DRS_SRCBLEND, D3DBLEND_SRCALPHA);
    IDirect3DDevice9_SetRenderState(device, D3DRS_DESTBLEND, D3DBLEND_INVSRCALPHA);
    IDirect3DDevice9_SetRenderState(device, D3DRS_BLENDOP, D3DBLENDOP_ADD);
    IDirect3DDevice9_SetRenderState(device, D3DRS_SCISSORTESTENABLE, TRUE);
    scissor.left = 0;
    scissor.top = 0;
    scissor.right = batch->width;
    scissor.bottom = batch->height;
    IDirect3DDevice9_SetScissorRect(device, &scissor);
}

/* Runs inside Present before the original call. Order: save RT0 and the
 * depth-stencil surface, capture all state, size from the back buffer (never
 * from creation parameters), draw, restore RT0/depth-stencil first (that
 * resets viewport and scissor) and then apply the state block. */
static void draw_overlay_frame(IDirect3DDevice9* device, const RECT* destination) {
    IDirect3DSurface9* render_target = NULL;
    IDirect3DSurface9* depth_stencil = NULL;
    IDirect3DStateBlock9* state_block = NULL;
    IDirect3DSurface9* back_buffer = NULL;
    D3DSURFACE_DESC description;
    overlay_batch batch;
    overlay_layout layout;
    int is_state_changed = 0;
    if (InterlockedCompareExchange(&overlay_is_device_lost, 0, 0) != 0) {
        if (IDirect3DDevice9_TestCooperativeLevel(device) != D3D_OK) {
            return;
        }
        InterlockedExchange(&overlay_is_device_lost, 0);
        trace_client_state("overlay_device_lost", 0);
    }
    update_frame_clock();
    if (FAILED(IDirect3DDevice9_GetRenderTarget(device, 0, &render_target))) {
        goto release;
    }
    if (FAILED(IDirect3DDevice9_GetDepthStencilSurface(device, &depth_stencil))) {
        depth_stencil = NULL;
    }
    if (FAILED(IDirect3DDevice9_CreateStateBlock(device, D3DSBT_ALL, &state_block))) {
        goto release;
    }
    if (FAILED(IDirect3DDevice9_GetBackBuffer(device, 0, 0, D3DBACKBUFFER_TYPE_MONO,
        &back_buffer)) || FAILED(IDirect3DSurface9_GetDesc(back_buffer, &description)) ||
        description.Width == 0 || description.Height == 0 ||
        description.Width > 32767 || description.Height > 32767) {
        goto release;
    }
    batch.device = device;
    batch.quad_count = 0;
    run_overlay_ui(&description, destination, &batch, &layout);
    is_state_changed = 1;
    IDirect3DDevice9_SetRenderTarget(device, 0, back_buffer);
    IDirect3DDevice9_SetDepthStencilSurface(device, NULL);
    set_overlay_render_state(device, &batch);
    if (SUCCEEDED(IDirect3DDevice9_BeginScene(device))) {
        render_commands(&batch, &layout);
        IDirect3DDevice9_EndScene(device);
    }
    if (InterlockedCompareExchange(&overlay_is_frame_traced, 1, 0) == 0) {
        trace_client_state("overlay_first_frame",
            ((unsigned int)description.Width << 16) | (unsigned int)description.Height);
    }
release:
    if (is_state_changed) {
        IDirect3DDevice9_SetRenderTarget(device, 0, render_target);
        IDirect3DDevice9_SetDepthStencilSurface(device, depth_stencil);
        IDirect3DStateBlock9_Apply(state_block);
    }
    if (back_buffer != NULL) {
        IDirect3DSurface9_Release(back_buffer);
    }
    if (state_block != NULL) {
        IDirect3DStateBlock9_Release(state_block);
    }
    if (depth_stencil != NULL) {
        IDirect3DSurface9_Release(depth_stencil);
    }
    if (render_target != NULL) {
        IDirect3DSurface9_Release(render_target);
    }
}

/* Thread-local guard: a Present re-entered on the same thread (for example
 * through a wrapper that calls back into the hooked slot) draws only once. */
static int enter_overlay_present(void) {
    if (overlay_present_tls == TLS_OUT_OF_INDEXES ||
        TlsGetValue(overlay_present_tls) != NULL) {
        return 0;
    }
    TlsSetValue(overlay_present_tls, (LPVOID)1);
    return 1;
}

static void leave_overlay_present(int is_outer) {
    if (is_outer) {
        TlsSetValue(overlay_present_tls, NULL);
    }
}

static void note_overlay_present_result(HRESULT result) {
    if (result == D3DERR_DEVICELOST &&
        InterlockedExchange(&overlay_is_device_lost, 1) == 0) {
        trace_client_state("overlay_device_lost", 1);
    }
}

/* The vtable is shared by every device of the class: act only for the
 * captured device, and always call the saved original, never the COM macro. */
static HRESULT WINAPI overlay_device_present(IDirect3DDevice9* device, const RECT* source,
    const RECT* destination, HWND window, const RGNDATA* dirty_region) {
    DWORD last_error = GetLastError();
    int is_outer = enter_overlay_present();
    int is_overlay_device = is_outer && (void*)device == overlay_current_device();
    HRESULT result;
    if (is_overlay_device && is_overlay_open()) {
        draw_overlay_frame(device, destination);
    }
    SetLastError(last_error);
    result = original_overlay_device_present(device, source, destination, window, dirty_region);
    last_error = GetLastError();
    if (is_overlay_device) {
        note_overlay_present_result(result);
    }
    leave_overlay_present(is_outer);
    SetLastError(last_error);
    return result;
}

/* Installed on the first open only; patches device Present (slot 17, H2). */
int install_overlay_present(void* device_pointer) {
    IDirect3DDevice9* device = (IDirect3DDevice9*)device_pointer;
    void** methods;
    void** slot;
    if (!readable_range(device, sizeof(void*))) {
        return 0;
    }
    methods = *(void***)device;
    if (!readable_range(methods, (OVERLAY_DEVICE_PRESENT_SLOT + 1) * sizeof(void*))) {
        return 0;
    }
    slot = methods + OVERLAY_DEVICE_PRESENT_SLOT;
    if (*slot != (void*)overlay_device_present) {
        if (original_overlay_device_present != NULL) {
            /* A second device class with its own vtable is not supported. */
            trace_client_state("overlay_present_hook", 0);
            return 0;
        }
        original_overlay_device_present = (overlay_device_present_fn)*slot;
        if (original_overlay_device_present == NULL ||
            !patch_vtable_slot(slot, (void*)original_overlay_device_present,
                (void*)overlay_device_present)) {
            original_overlay_device_present = NULL;
            trace_client_state("overlay_present_hook", 0);
            return 0;
        }
        trace_client_state("overlay_present_hook", 1);
    }
    return 1;
}

int install_overlay_draw(void) {
    unsigned int quad;
    if (overlay_present_tls == TLS_OUT_OF_INDEXES) {
        overlay_present_tls = TlsAlloc();
    }
    if (overlay_present_tls == TLS_OUT_OF_INDEXES) {
        return 0;
    }
    if (!QueryPerformanceFrequency(&overlay_frame_clock.frequency)) {
        overlay_frame_clock.frequency.QuadPart = 0;
    }
    for (quad = 0; quad < OVERLAY_QUAD_LIMIT; quad++) {
        WORD first = (WORD)(quad * 4);
        overlay_indices[quad * 6 + 0] = first;
        overlay_indices[quad * 6 + 1] = (WORD)(first + 1);
        overlay_indices[quad * 6 + 2] = (WORD)(first + 2);
        overlay_indices[quad * 6 + 3] = first;
        overlay_indices[quad * 6 + 4] = (WORD)(first + 2);
        overlay_indices[quad * 6 + 5] = (WORD)(first + 3);
    }
    mu_init(&overlay_context);
    overlay_context.text_width = overlay_text_width;
    overlay_context.text_height = overlay_text_height;
    overlay_context.style->colors[MU_COLOR_WINDOWBG] = mu_color(32, 34, 40, 235);
    overlay_context.style->colors[MU_COLOR_TITLEBG] = mu_color(18, 20, 26, 245);
    overlay_context.style->colors[MU_COLOR_PANELBG] = mu_color(24, 26, 32, 200);
    return 1;
}
#endif
