#include "overlay_ui.h"

#if FANG_OVERLAY
#include "command_catalog.h"

#include <stdio.h>
#include <string.h>

static void refresh_noun_list(overlay_ui* view) {
    overlay_query query;
    unsigned int index;
    overlay_make_query(&query, fang_spawn_nouns, view->noun_filter);
    if (!overlay_begin_list(&view->noun_list, &query)) {
        return;
    }
    for (index = 0; index < fang_spawn_noun_count; index++) {
        if (overlay_matches_filter(fang_spawn_nouns[index], view->noun_filter)) {
            overlay_add_match(&view->noun_list, index);
        }
    }
}

static int is_zone_dependent_noun(const char* noun) {
    return strcmp(noun, "MutationAgent.Noun") == 0;
}

void overlay_draw_enemies(mu_Context* context, overlay_ui* view) {
    unsigned int limit = OVERLAY_SPAWN_FALLBACK_LIMIT;
    int pair_widths[] = {60, -1};
    overlay_row_range row_range;
    unsigned int row;
    if (view->catalog != NULL && view->catalog->spawn_count_limit != 0) {
        limit = view->catalog->spawn_count_limit;
    }
    overlay_filter_row(context, view, view->noun_filter, sizeof(view->noun_filter));
    refresh_noun_list(view);
    overlay_begin_rows(context, "Nouns", -1, OVERLAY_LIST_HEIGHT, view->noun_list.count,
        &row_range);
    for (row = row_range.first; row < row_range.last; row++) {
        unsigned int index = view->noun_list.indexes[row];
        const char* noun = fang_spawn_nouns[index];
        char label[96];
        snprintf(label, sizeof(label), "%s%s", noun,
            is_zone_dependent_noun(noun) ? "  (zone-dependent)" : "");
        if (overlay_list_row(context, label, (int)index == view->noun_index)) {
            view->noun_index = (int)index;
        }
    }
    overlay_end_rows(context, view->noun_list.count, &row_range);
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Selected");
    mu_label(context, view->noun_index >= 0 ? fang_spawn_nouns[view->noun_index] : "none");
    mu_label(context, "Count");
    overlay_slider(context, &view->spawn_count, 1, (mu_Real)limit, "%.0f");
    if (overlay_action_row(context, view, "Spawn", OVERLAY_ACTION_SPAWN,
        view->noun_index < 0 ? "select an enemy" : NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_SPAWN);
        action.count = (unsigned int)(view->spawn_count + 0.5f);
        snprintf(action.text, sizeof(action.text), "%s", fang_spawn_nouns[view->noun_index]);
        overlay_send_action(view, &action);
    }
    overlay_draw_result(context, view);
}

static void submit_simple(overlay_ui* view, unsigned int kind) {
    overlay_action action;
    overlay_new_action(&action, kind);
    overlay_send_action(view, &action);
}

void overlay_draw_player(mu_Context* context, overlay_ui* view) {
    static const float dna_presets[] = {1000.0f, 10000.0f, 100000.0f, 1000000.0f};
    static const char* const dna_labels[] = {"1K", "10K", "100K", "1M"};
    unsigned int limit = OVERLAY_LEVEL_FALLBACK_LIMIT;
    int pair_widths[] = {60, -1};
    int preset_widths[] = {60, 60, 60, 60};
    char confirm[64];
    int index;
    if (view->catalog != NULL && view->catalog->level_limit != 0) {
        limit = view->catalog->level_limit;
    }
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Level");
    overlay_slider(context, &view->level, 1, (mu_Real)limit, "%.0f");
    snprintf(confirm, sizeof(confirm), "set the account to level %.0f", view->level);
    if (overlay_confirm_row(context, view, "Apply level", confirm, OVERLAY_ACTION_LEVEL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_LEVEL);
        action.level = (unsigned int)(view->level + 0.5f);
        overlay_send_action(view, &action);
    }
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "DNA");
    mu_number_ex(context, &view->dna, 100, "%.0f", MU_OPT_ALIGNCENTER);
    overlay_clamp_real(&view->dna, 1, OVERLAY_DNA_LIMIT);
    mu_layout_row(context, 4, preset_widths, 0);
    for (index = 0; index < 4; index++) {
        if (mu_button_ex(context, dna_labels[index], 0, MU_OPT_ALIGNCENTER) & MU_RES_SUBMIT) {
            view->dna = dna_presets[index];
        }
    }
    if (overlay_action_row(context, view, "Grant DNA", OVERLAY_ACTION_DNA, NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_DNA);
        action.dna = (unsigned int)(view->dna + 0.5f);
        overlay_send_action(view, &action);
    }
    if (overlay_action_row(context, view, "Heal", OVERLAY_ACTION_HEAL, NULL)) {
        submit_simple(view, OVERLAY_ACTION_HEAL);
    }
    if (overlay_action_row(context, view, "Fill power", OVERLAY_ACTION_POWER_FILL, NULL)) {
        submit_simple(view, OVERLAY_ACTION_POWER_FILL);
    }
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Damage");
    mu_number_ex(context, &view->damage, 1, "%.0f", MU_OPT_ALIGNCENTER);
    overlay_clamp_real(&view->damage, 1, OVERLAY_AMOUNT_LIMIT);
    if (overlay_action_row(context, view, "Damage hero", OVERLAY_ACTION_DAMAGE, NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_DAMAGE);
        action.amount = view->damage;
        overlay_send_action(view, &action);
    }
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Drain");
    mu_number_ex(context, &view->drain, 1, "%.0f", MU_OPT_ALIGNCENTER);
    overlay_clamp_real(&view->drain, 1, OVERLAY_AMOUNT_LIMIT);
    if (overlay_action_row(context, view, "Drain power", OVERLAY_ACTION_POWER_DRAIN, NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_POWER_DRAIN);
        action.amount = view->drain;
        overlay_send_action(view, &action);
    }
    if (overlay_confirm_row(context, view, "Recap", "show the recap to the whole party",
        OVERLAY_ACTION_RECAP)) {
        submit_simple(view, OVERLAY_ACTION_RECAP);
    }
    if (overlay_action_row(context, view, "Reset (server)", OVERLAY_ACTION_RESET, NULL)) {
        submit_simple(view, OVERLAY_ACTION_RESET);
    }
    {
        int width = -1;
        mu_layout_row(context, 1, &width, 0);
        overlay_dim_label(context, "Reset sends the server half of /reset only.");
    }
    overlay_draw_result(context, view);
}

static int has_suffix(const char* name, const char* suffix) {
    size_t name_length = strlen(name);
    size_t suffix_length = strlen(suffix);
    return name_length > suffix_length &&
        _stricmp(name + name_length - suffix_length, suffix) == 0;
}

static int warp_group_of(const char* name) {
    if (_stricmp(name, "front_end_ship") == 0) {
        return overlay_warp_hub;
    }
    if (has_suffix(name, "_SM")) {
        return overlay_warp_survival;
    }
    if (has_suffix(name, "_PVP")) {
        return overlay_warp_pvp;
    }
    if (_strnicmp(name, "CreatureEditor", 14) == 0 ||
        fang_warp_name_contains(name, "Creature_Vid_Capture", 20)) {
        return overlay_warp_editor;
    }
    if (_strnicmp(name, "test_", 5) == 0 || _stricmp(name, "Juggernaut_Mode_Testing") == 0) {
        return overlay_warp_test;
    }
    return overlay_warp_campaign;
}

static void refresh_warp_list(overlay_ui* view) {
    overlay_query query;
    unsigned int index;
    overlay_make_query(&query, fang_warp_locations, view->warp_filter);
    query.parameters[0] = (unsigned int)view->warp_group;
    if (!overlay_begin_list(&view->warp_list, &query)) {
        return;
    }
    for (index = 0; index < fang_warp_location_count; index++) {
        const char* location = fang_warp_locations[index];
        if ((view->warp_group == overlay_warp_all || warp_group_of(location) == view->warp_group) &&
            overlay_matches_filter(location, view->warp_filter)) {
            overlay_add_match(&view->warp_list, index);
        }
    }
}

static void draw_warps(mu_Context* context, overlay_ui* view) {
    static const char* const group_names[] = {
        "All", "Campaign", "SM", "PvP", "Test", "Editor", "Hub"
    };
    int group_widths[] = {40, 64, 36, 40, 44, 50, 40};
    int pair_widths[] = {90, -1};
    int width = -1;
    overlay_row_range row_range;
    unsigned int row;
    int group;
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Pending warp");
    mu_label(context, view->state.pending_warp[0] != '\0' ? view->state.pending_warp : "none");
    mu_layout_row(context, overlay_warp_group_count, group_widths, 0);
    for (group = 0; group < overlay_warp_group_count; group++) {
        if (overlay_colored_button(context, group_names[group],
            group == view->warp_group ? overlay_accent : context->style->colors[MU_COLOR_BUTTON],
            MU_OPT_ALIGNCENTER)) {
            view->warp_group = group;
        }
    }
    overlay_filter_row(context, view, view->warp_filter, sizeof(view->warp_filter));
    refresh_warp_list(view);
    overlay_begin_rows(context, "Warps", -1, OVERLAY_LIST_HEIGHT, view->warp_list.count,
        &row_range);
    for (row = row_range.first; row < row_range.last; row++) {
        unsigned int index = view->warp_list.indexes[row];
        if (overlay_list_row(context, fang_warp_locations[index], (int)index == view->warp_index)) {
            view->warp_index = (int)index;
        }
    }
    overlay_end_rows(context, view->warp_list.count, &row_range);
    if (overlay_action_row(context, view, "Warp", OVERLAY_ACTION_WARP,
        view->warp_index < 0 ? "select a level" : NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_WARP);
        snprintf(action.text, sizeof(action.text), "%s", fang_warp_locations[view->warp_index]);
        overlay_send_action(view, &action);
    }
    mu_layout_row(context, 1, &width, 0);
    overlay_dim_label(context,
        "Applies to the next campaign game; any warp loads as a Chain game.");
}

static void draw_events(mu_Context* context, overlay_ui* view) {
    const overlay_catalog* catalog = view->catalog;
    const char* block = overlay_action_block(view, OVERLAY_ACTION_EVENT);
    int widths[] = {120, 120, 120};
    int width = -1;
    unsigned int index;
    mu_layout_row(context, 1, &width, 0);
    if (catalog == NULL) {
        overlay_dim_label(context, "Events: catalog not loaded yet");
        return;
    }
    overlay_label_format(context, "Events%s%s", block != NULL ? ": " : "",
        block != NULL ? block : "");
    mu_layout_row(context, 3, widths, 0);
    for (index = 0; index < catalog->event_count; index++) {
        if (overlay_button(context, catalog->events[index].text, block == NULL)) {
            overlay_action action;
            overlay_new_action(&action, OVERLAY_ACTION_EVENT);
            snprintf(action.text, sizeof(action.text), "%s", catalog->events[index].text);
            overlay_send_action(view, &action);
        }
    }
}

static void draw_goto(mu_Context* context, overlay_ui* view) {
    int coordinate_widths[] = {20, 80, 20, 80, 20, 80, -1};
    if (!view->is_goto_prefilled && view->state.is_hero) {
        view->goto_x = view->state.x;
        view->goto_y = view->state.y;
        view->goto_z = view->state.z;
        view->is_goto_prefilled = 1;
    }
    mu_layout_row(context, 7, coordinate_widths, 0);
    mu_label(context, "x");
    mu_number_ex(context, &view->goto_x, 0.5f, "%.1f", MU_OPT_ALIGNCENTER);
    mu_label(context, "y");
    mu_number_ex(context, &view->goto_y, 0.5f, "%.1f", MU_OPT_ALIGNCENTER);
    mu_label(context, "z");
    mu_number_ex(context, &view->goto_z, 0.5f, "%.1f", MU_OPT_ALIGNCENTER);
    if (overlay_button(context, "From hero", view->state.is_hero)) {
        view->goto_x = view->state.x;
        view->goto_y = view->state.y;
        view->goto_z = view->state.z;
    }
    overlay_clamp_real(&view->goto_x, -OVERLAY_COORDINATE_LIMIT, OVERLAY_COORDINATE_LIMIT);
    overlay_clamp_real(&view->goto_y, -OVERLAY_COORDINATE_LIMIT, OVERLAY_COORDINATE_LIMIT);
    overlay_clamp_real(&view->goto_z, -OVERLAY_COORDINATE_LIMIT, OVERLAY_COORDINATE_LIMIT);
    if (overlay_action_row(context, view, "Go to", OVERLAY_ACTION_GOTO, NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_GOTO);
        action.x = view->goto_x;
        action.y = view->goto_y;
        action.z = view->goto_z;
        overlay_send_action(view, &action);
    }
}

static const char* victory_effect(const overlay_ui* view) {
    if (strcmp(view->state.mode, "tutorial") == 0) {
        return "completes the tutorial (saved on Return to Ship)";
    }
    if (strcmp(view->state.mode, "arena") == 0) {
        return "ends the zone for every Arena player";
    }
    return "completes the zone for the whole party";
}

void overlay_draw_world(mu_Context* context, overlay_ui* view) {
    const overlay_catalog* catalog = view->catalog;
    const char* effect = NULL;
    int widths[] = {70, -1};
    char label[96];
    draw_warps(context, view);
    draw_events(context, view);
    draw_goto(context, view);
    if (overlay_confirm_row(context, view, "Kill all", "kill every enemy for the whole party",
        OVERLAY_ACTION_KILL)) {
        submit_simple(view, OVERLAY_ACTION_KILL);
    }
    if (overlay_confirm_row(context, view, "Victory", victory_effect(view),
        OVERLAY_ACTION_VICTORY)) {
        submit_simple(view, OVERLAY_ACTION_VICTORY);
    }
    if (overlay_confirm_row(context, view, "Defeat",
        "defeat your squad (Game Over when all are down)",
        OVERLAY_ACTION_DEFEAT)) {
        submit_simple(view, OVERLAY_ACTION_DEFEAT);
    }
    if (catalog != NULL && view->effect_index >= 0 &&
        (unsigned int)view->effect_index < catalog->effect_count) {
        effect = catalog->effects[view->effect_index].text;
    }
    mu_layout_row(context, 2, widths, 0);
    mu_label(context, "Effect");
    snprintf(label, sizeof(label), "%s", effect != NULL ? effect : "None");
    if (overlay_button(context, label, catalog != NULL)) {
        overlay_open_picker(context, view, overlay_picker_effect);
    }
    if (overlay_action_row(context, view, "Preview effect", OVERLAY_ACTION_EFFECT,
        effect == NULL ? "select an effect" : NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_EFFECT);
        snprintf(action.text, sizeof(action.text), "%s", effect);
        overlay_send_action(view, &action);
    }
    overlay_draw_result(context, view);
    overlay_draw_picker(context, view);
}
#endif
