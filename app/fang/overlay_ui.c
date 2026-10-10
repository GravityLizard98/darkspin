#include "overlay_ui.h"

#if FANG_OVERLAY
#include "command_catalog.h"
#include "hook.h"

#include <stdarg.h>
#include <stdio.h>
#include <string.h>

#define OVERLAY_WINDOW_TITLE "Dark Spin debug"

static overlay_ui overlay_view = {
    .noun_index = -1,
    .spawn_count = 1,
    .rigblock_index = -1,
    .prefix1_index = -1,
    .prefix2_index = -1,
    .suffix_index = -1,
    .level = 1,
    .dna = 1000,
    .damage = 100,
    .drain = 10,
    .warp_index = -1,
    .effect_index = -1,
};

const mu_Color overlay_accent = {70, 110, 170, 255};
static const mu_Color overlay_row = {40, 42, 50, 255};
static const mu_Color overlay_dim = {140, 140, 140, 255};
static const mu_Color overlay_hit_point = {170, 50, 50, 255};
static const mu_Color overlay_power_point = {50, 90, 180, 255};

int overlay_ui_scale_delta(void) {
    return overlay_view.scale_delta;
}

void overlay_label_format(mu_Context* context, const char* format, ...) {
    char text[256];
    va_list arguments;
    va_start(arguments, format);
    vsnprintf(text, sizeof(text), format, arguments);
    va_end(arguments);
    mu_label(context, text);
}

void overlay_dim_label(mu_Context* context, const char* text) {
    mu_Color color = context->style->colors[MU_COLOR_TEXT];
    context->style->colors[MU_COLOR_TEXT] = overlay_dim;
    mu_label(context, text);
    context->style->colors[MU_COLOR_TEXT] = color;
}

/* Disabled controls cannot be hovered or clicked and are drawn dimmed. */
int overlay_button(mu_Context* context, const char* label, int is_enabled) {
    mu_Color color = context->style->colors[MU_COLOR_TEXT];
    if (is_enabled) {
        return (mu_button_ex(context, label, 0, MU_OPT_ALIGNCENTER) & MU_RES_SUBMIT) != 0;
    }
    context->style->colors[MU_COLOR_TEXT] = overlay_dim;
    mu_button_ex(context, label, 0, MU_OPT_ALIGNCENTER | MU_OPT_NOINTERACT);
    context->style->colors[MU_COLOR_TEXT] = color;
    return 0;
}

int overlay_colored_button(mu_Context* context, const char* label, mu_Color color, int opt) {
    mu_Color button = context->style->colors[MU_COLOR_BUTTON];
    int is_clicked;
    context->style->colors[MU_COLOR_BUTTON] = color;
    is_clicked = (mu_button_ex(context, label, 0, opt) & MU_RES_SUBMIT) != 0;
    context->style->colors[MU_COLOR_BUTTON] = button;
    return is_clicked;
}

int overlay_list_row(mu_Context* context, const char* label, int is_selected) {
    return overlay_colored_button(context, label, is_selected ? overlay_accent : overlay_row, 0);
}

/* Append/backspace text field; reports keyboard ownership for the input
 * filter and the Enter poller. */
void overlay_text_box(mu_Context* context, overlay_ui* view, char* buffer, int size) {
    mu_textbox_ex(context, buffer, size, 0);
    if (context->focus != 0 && context->focus == context->last_id) {
        view->is_text_focused = 1;
    }
}

void overlay_filter_row(mu_Context* context, overlay_ui* view, char* filter, int size) {
    int widths[] = {50, -1};
    mu_layout_row(context, 2, widths, 0);
    mu_label(context, "Filter");
    overlay_text_box(context, view, filter, size);
}

void overlay_clamp_real(mu_Real* number, mu_Real minimum, mu_Real maximum) {
    if (!(*number >= minimum)) {
        *number = minimum;
    }
    if (*number > maximum) {
        *number = maximum;
    }
}

/* microui divides by (high - low); a degenerate range is shown as text. */
void overlay_slider(mu_Context* context, mu_Real* number, mu_Real low, mu_Real high,
    const char* format) {
    if (!(high > low)) {
        *number = low;
        overlay_label_format(context, format, (double)low);
        return;
    }
    mu_slider_ex(context, number, low, high, 1, format, MU_OPT_ALIGNCENTER);
    overlay_clamp_real(number, low, high);
}

static void spacer(mu_Context* context, int width, int height) {
    if (height <= 0) {
        return;
    }
    mu_layout_row(context, 1, &width, height);
    mu_layout_next(context);
}

/* Virtualized list: a scrolling panel that lays out only the visible rows,
 * with spacers standing in for the rest so the scrollbar stays exact. */
void overlay_begin_rows(mu_Context* context, const char* name, int width, int height,
    unsigned int count, overlay_row_range* row_range) {
    mu_Container* panel;
    unsigned int visible;
    int fill = -1;
    mu_layout_row(context, 1, &width, height);
    mu_begin_panel(context, name);
    panel = mu_get_current_container(context);
    row_range->pitch = OVERLAY_ROW_HEIGHT + context->style->spacing;
    row_range->first = panel->scroll.y > 0 ? (unsigned int)(panel->scroll.y / row_range->pitch) : 0;
    if (row_range->first > count) {
        row_range->first = count;
    }
    visible = (unsigned int)(panel->body.h > 0 ? panel->body.h / row_range->pitch : 0) + 2;
    row_range->last = count - row_range->first < visible ? count : row_range->first + visible;
    spacer(context, fill, (int)row_range->first * row_range->pitch - context->style->spacing);
    mu_layout_row(context, 1, &fill, OVERLAY_ROW_HEIGHT);
}

void overlay_end_rows(mu_Context* context, unsigned int count, const overlay_row_range* row_range) {
    spacer(context, -1,
        (int)(count - row_range->last) * row_range->pitch - context->style->spacing);
    mu_end_panel(context);
}

int overlay_matches_filter(const char* name, const char* filter) {
    size_t length = strlen(filter);
    return length == 0 || fang_warp_name_contains(name, filter, length);
}

/* Returns 1 when the list must be rebuilt for this query. */
int overlay_begin_list(overlay_list* list, const overlay_query* query) {
    if (list->is_built && memcmp(&list->query, query, sizeof(*query)) == 0) {
        return 0;
    }
    list->query = *query;
    list->is_built = 1;
    list->count = 0;
    return 1;
}

void overlay_add_match(overlay_list* list, unsigned int index) {
    if (list->count < OVERLAY_MATCH_LIMIT) {
        list->indexes[list->count++] = (unsigned short)index;
    }
}

void overlay_make_query(overlay_query* query, const void* source, const char* filter) {
    memset(query, 0, sizeof(*query));
    query->source = source;
    snprintf(query->filter, sizeof(query->filter), "%s", filter);
}

static const char* connection_text(const overlay_ui* view) {
    switch (overlay_current_net_mode()) {
    case OVERLAY_NET_STARTED:
        break;
    case OVERLAY_NET_REMOTE:
        return "overlay needs a local server";
    case OVERLAY_NET_KEY_FAILED:
        return "overlay session key unavailable";
    default:
        return "overlay network not started";
    }
    switch (view->state.connection) {
    case OVERLAY_CONNECTION_CONNECTING:
        return "Connecting...";
    case OVERLAY_CONNECTION_CONNECTED:
        return "Connected";
    case OVERLAY_CONNECTION_UNBOUND:
        return "Session not bound: type /debug again";
    case OVERLAY_CONNECTION_DISABLED:
        return "debug API disabled on this server";
    case OVERLAY_CONNECTION_FORBIDDEN:
        return "refused by the server request guard";
    case OVERLAY_CONNECTION_UNREACHABLE:
        return "server unreachable";
    case OVERLAY_CONNECTION_SCHEMA:
        return "server reply did not match the overlay schema";
    case OVERLAY_CONNECTION_FAILED:
        return "server error";
    default:
        return "waiting for the server";
    }
}

static const char* reason_text(const char* reason) {
    if (strcmp(reason, "no_game") == 0) {
        return "no game";
    }
    if (strcmp(reason, "not_deployed") == 0) {
        return "hero not deployed";
    }
    if (strcmp(reason, "not_warped") == 0) {
        return "game was not started by a warp";
    }
    if (strcmp(reason, "wrong_mode") == 0) {
        return "not available in this game mode";
    }
    if (strcmp(reason, "zone_terminal") == 0) {
        return "zone already finished";
    }
    if (strcmp(reason, "busy") == 0) {
        return "busy";
    }
    if (reason[0] == '\0') {
        return "unavailable";
    }
    return reason;
}

static int is_action_pending(const overlay_ui* view) {
    return view->pending_sequence != 0 &&
        view->result.action_sequence != view->pending_sequence;
}

/* NULL when the action may be sent; otherwise the inline reason. */
const char* overlay_action_block(const overlay_ui* view, unsigned int kind) {
    if (overlay_current_net_mode() != OVERLAY_NET_STARTED ||
        view->state.connection != OVERLAY_CONNECTION_CONNECTED) {
        return connection_text(view);
    }
    if (!view->state.is_received) {
        return "no server state yet";
    }
    if (is_overlay_action_busy() || is_action_pending(view)) {
        return "busy: waiting for the last action";
    }
    if (kind < OVERLAY_ACTION_KIND_COUNT && !view->state.actions[kind].is_available) {
        return reason_text(view->state.actions[kind].reason);
    }
    return NULL;
}

void overlay_send_action(overlay_ui* view, overlay_action* action) {
    unsigned int sequence = overlay_submit_action(action);
    if (sequence == 0) {
        return;
    }
    view->pending_sequence = sequence;
    view->confirm_kind = 0;
}

void overlay_new_action(overlay_action* action, unsigned int kind) {
    memset(action, 0, sizeof(*action));
    action->kind = kind;
}

/* [label] plus the inline reason when it cannot be sent. local_block is a
 * client-side precondition such as a missing selection. */
int overlay_action_row(mu_Context* context, const overlay_ui* view, const char* label,
    unsigned int kind, const char* local_block) {
    const char* block = overlay_action_block(view, kind);
    int widths[] = {130, -1};
    int is_clicked;
    if (block == NULL) {
        block = local_block;
    }
    mu_layout_row(context, 2, widths, 0);
    is_clicked = overlay_button(context, label, block == NULL);
    overlay_dim_label(context, block != NULL ? block : "");
    return is_clicked;
}

/* Destructive or party-wide actions need a second click within a few
 * seconds; the armed button shows the confirmation text. */
int overlay_confirm_row(mu_Context* context, overlay_ui* view, const char* label,
    const char* confirm_label, unsigned int kind) {
    const char* block = overlay_action_block(view, kind);
    int widths[] = {130, -1};
    int is_armed = view->confirm_kind == kind + 1;
    int is_clicked;
    mu_layout_row(context, 2, widths, 0);
    is_clicked = overlay_button(context, is_armed ? "Confirm" : label, block == NULL);
    overlay_dim_label(context, block != NULL ? block : (is_armed ? confirm_label : ""));
    if (!is_clicked) {
        return 0;
    }
    if (!is_armed) {
        view->confirm_kind = kind + 1;
        view->confirm_tick = GetTickCount();
        return 0;
    }
    view->confirm_kind = 0;
    return 1;
}

void overlay_draw_result(mu_Context* context, const overlay_ui* view) {
    const overlay_result* result = &view->result;
    char line[192];
    int width = -1;
    mu_layout_row(context, 1, &width, 0);
    if (view->pending_sequence == 0) {
        overlay_dim_label(context, "No action sent yet");
        return;
    }
    if (result->action_sequence != view->pending_sequence) {
        mu_label(context, "Pending...");
        return;
    }
    if (strcmp(result->code, "applied") == 0) {
        if (result->kind == OVERLAY_ACTION_DNA) {
            snprintf(line, sizeof(line), "Done (DNA total %llu)", result->dna_total);
        } else {
            snprintf(line, sizeof(line), "Done");
        }
        mu_label(context, line);
        return;
    }
    if (strcmp(result->code, "queued") == 0) {
        if (result->kind == OVERLAY_ACTION_SPAWN) {
            snprintf(line, sizeof(line), "Queued %u of %u", result->queued_count,
                result->requested_count);
        } else {
            snprintf(line, sizeof(line), "Queued");
        }
        mu_label(context, line);
        return;
    }
    mu_text(context, result->message[0] != '\0' ? result->message : result->code);
}

static int is_finite_number(float number) {
    return number == number && number - number == 0.0f;
}

static void draw_bar(mu_Context* context, float current, float maximum, mu_Color fill) {
    mu_Rect rect = mu_layout_next(context);
    char text[64];
    mu_draw_rect(context, rect, context->style->colors[MU_COLOR_BASE]);
    if (is_finite_number(current) && is_finite_number(maximum) && maximum > 0.0f &&
        current > 0.0f) {
        float fraction = current >= maximum ? 1.0f : current / maximum;
        mu_draw_rect(context, mu_rect(rect.x, rect.y, (int)((float)rect.w * fraction), rect.h),
            fill);
    }
    snprintf(text, sizeof(text), "%.0f / %.0f", current, maximum);
    mu_draw_control_text(context, text, rect, MU_COLOR_TEXT, MU_OPT_ALIGNCENTER);
}

static float float_from_bits(unsigned int bits) {
    float number;
    memcpy(&number, &bits, sizeof(number));
    return number;
}

/* The client-received values /stat appends, next to the server's. */
static void draw_client_resource(mu_Context* context) {
    unsigned int object_id;
    unsigned int hit_point_bits;
    unsigned int power_point_bits;
    unsigned int resource_mask;
    char hit_point[32] = "n/a";
    char power_point[32] = "n/a";
    fang_read_stat_resource(&object_id, &hit_point_bits, &power_point_bits, &resource_mask);
    if ((resource_mask & 1u) != 0) {
        snprintf(hit_point, sizeof(hit_point), "%.0f", float_from_bits(hit_point_bits));
    }
    if ((resource_mask & 2u) != 0) {
        snprintf(power_point, sizeof(power_point), "%.0f", float_from_bits(power_point_bits));
    }
    mu_label(context, "Client /stat");
    overlay_label_format(context, "object %u  HP %s  power %s", object_id, hit_point, power_point);
}

static void draw_info(mu_Context* context, const overlay_ui* view, const overlay_frame* frame) {
    const overlay_state* state = &view->state;
    int pair_widths[] = {100, -1};
    int columns[] = {150, 70, 80, -1};
    unsigned int index;
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Connection");
    mu_label(context, connection_text(view));
    mu_label(context, "Frame");
    overlay_label_format(context, "%.1f ms (%u fps)", frame->frame_ms, frame->frame_rate);
    if (!state->is_received) {
        mu_label(context, "State");
        overlay_dim_label(context, "no state received yet");
        return;
    }
    mu_label(context, "State latency");
    overlay_label_format(context, "%u ms", state->latency_ms);
    mu_label(context, "Server build");
    overlay_label_format(context, "%s %s (%s)", state->version, state->build_id,
        state->is_build_match ? "matches Fang" : "differs from Fang");
    mu_label(context, "Account");
    overlay_label_format(context, "%s  level %u  DNA %llu", state->display_name, state->level,
        state->dna);
    mu_label(context, "Pending warp");
    mu_label(context, state->pending_warp[0] != '\0' ? state->pending_warp : "none");
    mu_label(context, "Game");
    if (state->is_game) {
        overlay_label_format(context, "#%llu  %s  %s  players in game %u", state->game_id,
            state->mode, state->is_warped ? "warped" : "not warped", state->player_count);
    } else {
        overlay_dim_label(context, "no game");
    }
    mu_label(context, "Hero");
    if (!state->is_hero) {
        overlay_dim_label(context, "no gameplay session");
    } else {
        overlay_label_format(context, "object %llu  %s", state->object_id,
            state->is_deployed ? "deployed" : "not deployed");
        mu_label(context, "Position");
        overlay_label_format(context, "%.1f, %.1f, %.1f", state->x, state->y, state->z);
        mu_label(context, "HP (server)");
        draw_bar(context, state->hit_point, state->hit_point_max, overlay_hit_point);
        mu_label(context, "Power (server)");
        draw_bar(context, state->power_point, state->power_point_max, overlay_power_point);
    }
    draw_client_resource(context);
    mu_label(context, "Alive NPCs");
    overlay_label_format(context, "%u", state->alive_npc_count);
    mu_layout_row(context, 4, columns, 0);
    overlay_dim_label(context, "Nearest hostile");
    overlay_dim_label(context, "Distance");
    overlay_dim_label(context, "Direction");
    overlay_dim_label(context, "HP");
    for (index = 0; index < state->npc_count && index < OVERLAY_NPC_LIMIT; index++) {
        const overlay_npc* npc = &state->npcs[index];
        mu_label(context, npc->name);
        overlay_label_format(context, "%.1f", npc->distance);
        mu_label(context, npc->direction);
        overlay_label_format(context, "%.0f / %.0f", npc->hit_point, npc->hit_point_max);
    }
}

/* A new catalog block may order items differently; selections into the old
 * block are dropped. */
static void refresh_snapshots(overlay_ui* view) {
    overlay_state state;
    overlay_result result;
    const overlay_catalog* catalog = overlay_current_catalog();
    if (overlay_read_state(&state)) {
        view->state = state;
    }
    if (overlay_read_result(&result)) {
        view->result = result;
    }
    if (catalog != view->catalog) {
        view->catalog = catalog;
        view->rigblock_index = -1;
        view->prefix1_index = -1;
        view->prefix2_index = -1;
        view->suffix_index = -1;
        view->effect_index = -1;
        view->slot_bit = 0;
        view->class_bit = 0;
        view->science_bit = 0;
    }
    if (view->confirm_kind != 0 && GetTickCount() - view->confirm_tick > OVERLAY_CONFIRM_MS) {
        view->confirm_kind = 0;
    }
}

static void clamp_window(mu_Container* window, const overlay_frame* frame) {
    mu_Rect* rect;
    if (window == NULL || window->rect.w == 0) {
        return;
    }
    rect = &window->rect;
    if (rect->w > frame->width) {
        rect->w = frame->width > 96 ? frame->width : 96;
    }
    if (rect->h > frame->height) {
        rect->h = frame->height > 64 ? frame->height : 64;
    }
    if (rect->x + rect->w > frame->width) {
        rect->x = frame->width - rect->w;
    }
    if (rect->y + rect->h > frame->height) {
        rect->y = frame->height - rect->h;
    }
    if (rect->x < 0) {
        rect->x = 0;
    }
    if (rect->y < 0) {
        rect->y = 0;
    }
}

static void draw_header(mu_Context* context, overlay_ui* view, const overlay_frame* frame) {
    static const char* const tab_names[] = {"Info", "Enemies", "Items", "Player", "World"};
    int widths[] = {48, 64, 52, 56, 52, 22, 22, -1};
    int status = -1;
    int tab;
    mu_layout_row(context, 8, widths, 0);
    for (tab = 0; tab < overlay_tab_count; tab++) {
        if (overlay_colored_button(context, tab_names[tab],
            tab == view->tab ? overlay_accent : context->style->colors[MU_COLOR_BUTTON],
            MU_OPT_ALIGNCENTER)) {
            view->tab = tab;
            view->confirm_kind = 0;
            view->picker = overlay_picker_none;
        }
    }
    if (overlay_button(context, "-", frame->scale > 1)) {
        view->scale_delta--;
    }
    if (overlay_button(context, "+", frame->scale < OVERLAY_SCALE_MAXIMUM)) {
        view->scale_delta++;
    }
    if (view->scale_delta < 1 - frame->automatic_scale) {
        view->scale_delta = 1 - frame->automatic_scale;
    }
    if (view->scale_delta > OVERLAY_SCALE_MAXIMUM - frame->automatic_scale) {
        view->scale_delta = OVERLAY_SCALE_MAXIMUM - frame->automatic_scale;
    }
    overlay_label_format(context, "x%d", frame->scale);
    mu_layout_row(context, 1, &status, 0);
    overlay_dim_label(context, connection_text(view));
}

void overlay_build_ui(mu_Context* context, const overlay_frame* frame, int* is_text_focused) {
    overlay_ui* view = &overlay_view;
    view->is_text_focused = 0;
    refresh_snapshots(view);
    clamp_window(mu_get_container(context, OVERLAY_WINDOW_TITLE), frame);
    if (mu_begin_window_ex(context, OVERLAY_WINDOW_TITLE, mu_rect(24, 24, 470, 520),
        MU_OPT_NOCLOSE)) {
        draw_header(context, view, frame);
        switch (view->tab) {
        case overlay_tab_enemies:
            overlay_draw_enemies(context, view);
            break;
        case overlay_tab_items:
            overlay_draw_items(context, view);
            break;
        case overlay_tab_player:
            overlay_draw_player(context, view);
            break;
        case overlay_tab_world:
            overlay_draw_world(context, view);
            break;
        default:
            draw_info(context, view, frame);
            break;
        }
        mu_end_window(context);
    }
    *is_text_focused = view->is_text_focused;
}
#endif
