#include "overlay_ui.h"

#if FANG_OVERLAY
#include <stdio.h>
#include <string.h>

static const char* vocabulary_name(const overlay_vocabulary* vocabulary, unsigned int mask) {
    unsigned int index;
    if ((mask & OVERLAY_VOCABULARY_ALL) != 0) {
        return "all";
    }
    for (index = 0; index < vocabulary->count && index < OVERLAY_VOCABULARY_LIMIT; index++) {
        if ((mask & (1u << index)) != 0) {
            return vocabulary->names[index];
        }
    }
    return "";
}

static const overlay_item* selected_item(const overlay_item* items, unsigned int count,
    int index) {
    if (items == NULL || index < 0 || (unsigned int)index >= count) {
        return NULL;
    }
    return &items[index];
}

static const overlay_item* selected_rigblock(const overlay_ui* view) {
    if (view->catalog == NULL) {
        return NULL;
    }
    return selected_item(view->catalog->rigblocks, view->catalog->rigblock_count,
        view->rigblock_index);
}

/* Advisory only: affixes for another part type or science are hidden unless
 * the window shows all affixes; /summon itself enforces no compatibility. */
static int is_affix_compatible(const overlay_item* rigblock, const overlay_item* affix) {
    unsigned int science;
    if (rigblock == NULL) {
        return 1;
    }
    if (affix->slot_mask != 0 && (affix->slot_mask & OVERLAY_VOCABULARY_ALL) == 0 &&
        (affix->slot_mask & rigblock->slot_mask) == 0) {
        return 0;
    }
    science = affix->science_mask | rigblock->science_mask;
    if (affix->science_mask == 0 || rigblock->science_mask == 0 ||
        (science & OVERLAY_VOCABULARY_ALL) != 0) {
        return 1;
    }
    return (affix->science_mask & rigblock->science_mask) != 0;
}

/* /summon always creates a level-1 Basic item. */
static int is_basic_rollable(const overlay_item* affix) {
    return affix->minimum_level <= 1 && affix->maximum_level >= 1 && affix->is_basic_eligible;
}

static const overlay_vocabulary* picker_vocabulary(const overlay_ui* view) {
    switch (view->picker) {
    case overlay_picker_slot:
        return &view->catalog->slots;
    case overlay_picker_class:
        return &view->catalog->classes;
    case overlay_picker_science:
        return &view->catalog->sciences;
    default:
        return NULL;
    }
}

static const overlay_item* picker_items(const overlay_ui* view, unsigned int* count) {
    switch (view->picker) {
    case overlay_picker_prefix1:
    case overlay_picker_prefix2:
        *count = view->catalog->prefix_count;
        return view->catalog->prefixes;
    case overlay_picker_suffix:
        *count = view->catalog->suffix_count;
        return view->catalog->suffixes;
    default:
        *count = 0;
        return NULL;
    }
}

static void refresh_picker_list(overlay_ui* view) {
    overlay_list* list = &view->picker_list;
    const overlay_vocabulary* vocabulary = picker_vocabulary(view);
    const overlay_item* rigblock = selected_rigblock(view);
    const overlay_item* items;
    overlay_query query;
    unsigned int count;
    unsigned int index;
    overlay_make_query(&query, view->catalog, view->picker_filter);
    query.parameters[0] = (unsigned int)view->picker;
    query.parameters[1] = (unsigned int)view->rigblock_index;
    query.parameters[2] = (unsigned int)view->is_affix_unfiltered;
    if (!overlay_begin_list(list, &query)) {
        return;
    }
    if (vocabulary != NULL) {
        for (index = 0; index < vocabulary->count && index < OVERLAY_VOCABULARY_LIMIT; index++) {
            if (overlay_matches_filter(vocabulary->names[index], view->picker_filter)) {
                overlay_add_match(list, index);
            }
        }
        return;
    }
    if (view->picker == overlay_picker_effect) {
        for (index = 0; index < view->catalog->effect_count; index++) {
            if (overlay_matches_filter(view->catalog->effects[index].text, view->picker_filter)) {
                overlay_add_match(list, index);
            }
        }
        return;
    }
    items = picker_items(view, &count);
    for (index = 0; items != NULL && index < count; index++) {
        if ((view->is_affix_unfiltered || is_affix_compatible(rigblock, &items[index])) &&
            overlay_matches_filter(items[index].name, view->picker_filter)) {
            overlay_add_match(list, index);
        }
    }
}

static int picker_selection(const overlay_ui* view) {
    switch (view->picker) {
    case overlay_picker_prefix1:
        return view->prefix1_index;
    case overlay_picker_prefix2:
        return view->prefix2_index;
    case overlay_picker_suffix:
        return view->suffix_index;
    case overlay_picker_effect:
        return view->effect_index;
    default:
        return -1;
    }
}

static void choose_picker(overlay_ui* view, int index) {
    unsigned int bit = index >= 0 ? 1u << index : 0;
    switch (view->picker) {
    case overlay_picker_slot:
        view->slot_bit = bit;
        return;
    case overlay_picker_class:
        view->class_bit = bit;
        return;
    case overlay_picker_science:
        view->science_bit = bit;
        return;
    case overlay_picker_prefix1:
        view->prefix1_index = index;
        return;
    case overlay_picker_prefix2:
        view->prefix2_index = index;
        return;
    case overlay_picker_suffix:
        view->suffix_index = index;
        return;
    case overlay_picker_effect:
        view->effect_index = index;
        return;
    default:
        return;
    }
}

static void picker_label(const overlay_ui* view, unsigned int index, char* label, size_t size) {
    const overlay_vocabulary* vocabulary = picker_vocabulary(view);
    const overlay_item* items;
    unsigned int count;
    if (vocabulary != NULL) {
        snprintf(label, size, "%s", vocabulary->names[index]);
        return;
    }
    if (view->picker == overlay_picker_effect) {
        snprintf(label, size, "%s", view->catalog->effects[index].text);
        return;
    }
    items = picker_items(view, &count);
    snprintf(label, size, "%s%s  #%u  L%u-%u", is_basic_rollable(&items[index]) ? "" : "* ",
        items[index].name, items[index].id, items[index].minimum_level,
        items[index].maximum_level);
}

void overlay_open_picker(mu_Context* context, overlay_ui* view, int picker) {
    view->picker = picker;
    view->picker_filter[0] = '\0';
    view->picker_list.is_built = 0;
    mu_open_popup(context, "Picker");
}

void overlay_draw_picker(mu_Context* context, overlay_ui* view) {
    overlay_row_range row_range;
    unsigned int row;
    int width = OVERLAY_PICKER_WIDTH;
    int is_chosen = 0;
    int selection = picker_selection(view);
    if (!mu_begin_popup(context, "Picker")) {
        return;
    }
    if (view->catalog == NULL || view->picker == overlay_picker_none) {
        mu_get_current_container(context)->open = 0;
        mu_end_popup(context);
        return;
    }
    mu_layout_row(context, 1, &width, 0);
    overlay_text_box(context, view, view->picker_filter, sizeof(view->picker_filter));
    refresh_picker_list(view);
    if (overlay_list_row(context, view->picker <= overlay_picker_science ? "Any" : "None",
        selection < 0 && view->picker > overlay_picker_science)) {
        choose_picker(view, -1);
        is_chosen = 1;
    }
    overlay_begin_rows(context, "PickerRows", OVERLAY_PICKER_WIDTH, OVERLAY_PICKER_HEIGHT,
        view->picker_list.count, &row_range);
    for (row = row_range.first; row < row_range.last; row++) {
        unsigned int index = view->picker_list.indexes[row];
        char label[160];
        picker_label(view, index, label, sizeof(label));
        if (overlay_list_row(context, label, (int)index == selection)) {
            choose_picker(view, (int)index);
            is_chosen = 1;
        }
    }
    overlay_end_rows(context, view->picker_list.count, &row_range);
    if (is_chosen) {
        mu_get_current_container(context)->open = 0;
    }
    mu_end_popup(context);
}

static int is_rigblock_match(const overlay_ui* view, const overlay_item* item) {
    unsigned int level = (unsigned int)(view->item_level + 0.5f);
    if (view->slot_bit != 0 && (item->slot_mask & view->slot_bit) == 0) {
        return 0;
    }
    if (view->class_bit != 0 &&
        (item->class_mask & (view->class_bit | OVERLAY_VOCABULARY_ALL)) == 0) {
        return 0;
    }
    if (view->science_bit != 0 &&
        (item->science_mask & (view->science_bit | OVERLAY_VOCABULARY_ALL)) == 0) {
        return 0;
    }
    if (level != 0 && (item->minimum_level > level || item->maximum_level < level)) {
        return 0;
    }
    return overlay_matches_filter(item->name, view->item_filter);
}

static void refresh_rigblock_list(overlay_ui* view) {
    overlay_query query;
    unsigned int index;
    overlay_make_query(&query, view->catalog, view->item_filter);
    query.parameters[0] = view->slot_bit;
    query.parameters[1] = view->class_bit;
    query.parameters[2] = view->science_bit;
    query.parameters[3] = (unsigned int)(view->item_level + 0.5f);
    if (!overlay_begin_list(&view->rigblock_list, &query)) {
        return;
    }
    for (index = 0; index < view->catalog->rigblock_count; index++) {
        if (is_rigblock_match(view, &view->catalog->rigblocks[index])) {
            overlay_add_match(&view->rigblock_list, index);
        }
    }
}

static const char* filter_name(const overlay_vocabulary* vocabulary, unsigned int bit) {
    return bit == 0 ? "any" : vocabulary_name(vocabulary, bit);
}

static void draw_item_filters(mu_Context* context, overlay_ui* view) {
    const overlay_catalog* catalog = view->catalog;
    int widths[] = {130, 130, -1};
    int pair_widths[] = {60, -1};
    char label[64];
    mu_layout_row(context, 3, widths, 0);
    snprintf(label, sizeof(label), "Slot: %s", filter_name(&catalog->slots, view->slot_bit));
    if (mu_button_ex(context, label, 0, MU_OPT_ALIGNCENTER) & MU_RES_SUBMIT) {
        overlay_open_picker(context, view, overlay_picker_slot);
    }
    snprintf(label, sizeof(label), "Class: %s", filter_name(&catalog->classes, view->class_bit));
    if (mu_button_ex(context, label, 0, MU_OPT_ALIGNCENTER) & MU_RES_SUBMIT) {
        overlay_open_picker(context, view, overlay_picker_class);
    }
    snprintf(label, sizeof(label), "Science: %s",
        filter_name(&catalog->sciences, view->science_bit));
    if (mu_button_ex(context, label, 0, MU_OPT_ALIGNCENTER) & MU_RES_SUBMIT) {
        overlay_open_picker(context, view, overlay_picker_science);
    }
    mu_layout_row(context, 2, pair_widths, 0);
    mu_label(context, "Level");
    overlay_slider(context, &view->item_level, 0, (mu_Real)catalog->level_limit,
        "%.0f (0 = any)");
    overlay_filter_row(context, view, view->item_filter, sizeof(view->item_filter));
}

static void affix_row(mu_Context* context, overlay_ui* view, const char* title, int picker,
    const overlay_item* items, unsigned int count, int index) {
    const overlay_item* affix = selected_item(items, count, index);
    int widths[] = {70, -1};
    char label[128];
    int is_clicked;
    mu_layout_row(context, 2, widths, 0);
    mu_label(context, title);
    if (affix == NULL) {
        snprintf(label, sizeof(label), "None");
    } else {
        snprintf(label, sizeof(label), "%s%s  #%u", is_basic_rollable(affix) ? "" : "* ",
            affix->name, affix->id);
    }
    /* Three rows may all read "None"; scope each button id by its row. The
     * popup is opened outside that scope so its id stays the shared one. */
    mu_push_id(context, title, (int)strlen(title));
    is_clicked = (mu_button_ex(context, label, 0, 0) & MU_RES_SUBMIT) != 0;
    mu_pop_id(context);
    if (is_clicked) {
        overlay_open_picker(context, view, picker);
    }
}

static unsigned int selected_affix_id(const overlay_item* items, unsigned int count, int index) {
    const overlay_item* affix = selected_item(items, count, index);
    return affix != NULL ? affix->id : 0;
}

static void draw_drops(mu_Context* context, overlay_ui* view) {
    static const char* const fallback_categories[] = {
        "any", "weapon", "hand", "foot", "offense", "defense", "utility"
    };
    const overlay_catalog* catalog = view->catalog;
    const char* block = overlay_action_block(view, OVERLAY_ACTION_DROP);
    unsigned int count = catalog->drop_category_count;
    unsigned int index;
    int widths[] = {90, 90, 90, 90};
    int width = -1;
    if (count == 0) {
        count = sizeof(fallback_categories) / sizeof(fallback_categories[0]);
    }
    mu_layout_row(context, 1, &width, 0);
    overlay_label_format(context, "Random drop%s%s", block != NULL ? ": " : "",
        block != NULL ? block : "");
    mu_layout_row(context, 4, widths, 0);
    for (index = 0; index < count; index++) {
        const char* category = catalog->drop_category_count != 0 ?
            catalog->drop_categories[index].text : fallback_categories[index];
        char label[80];
        snprintf(label, sizeof(label), "Drop %s", category);
        if (overlay_button(context, label, block == NULL)) {
            overlay_action action;
            overlay_new_action(&action, OVERLAY_ACTION_DROP);
            snprintf(action.text, sizeof(action.text), "%s", category);
            overlay_send_action(view, &action);
        }
    }
}

void overlay_draw_items(mu_Context* context, overlay_ui* view) {
    const overlay_catalog* catalog = view->catalog;
    const overlay_item* rigblock;
    overlay_row_range row_range;
    unsigned int row;
    int width = -1;
    if (catalog == NULL) {
        mu_layout_row(context, 1, &width, 0);
        overlay_dim_label(context, "Item catalog not loaded yet (needs a bound session)");
        overlay_draw_result(context, view);
        return;
    }
    draw_item_filters(context, view);
    refresh_rigblock_list(view);
    overlay_begin_rows(context, "Rigblocks", -1, OVERLAY_LIST_HEIGHT,
        view->rigblock_list.count, &row_range);
    for (row = row_range.first; row < row_range.last; row++) {
        unsigned int index = view->rigblock_list.indexes[row];
        const overlay_item* item = &catalog->rigblocks[index];
        char label[160];
        snprintf(label, sizeof(label), "%s  #%u  %s  L%u-%u%s", item->name, item->id,
            vocabulary_name(&catalog->slots, item->slot_mask), item->minimum_level,
            item->maximum_level, item->is_unique ? "  unique" : "");
        if (overlay_list_row(context, label, (int)index == view->rigblock_index)) {
            view->rigblock_index = (int)index;
        }
    }
    overlay_end_rows(context, view->rigblock_list.count, &row_range);
    rigblock = selected_rigblock(view);
    affix_row(context, view, "Prefix 1", overlay_picker_prefix1, catalog->prefixes,
        catalog->prefix_count, view->prefix1_index);
    affix_row(context, view, "Prefix 2", overlay_picker_prefix2, catalog->prefixes,
        catalog->prefix_count, view->prefix2_index);
    affix_row(context, view, "Suffix", overlay_picker_suffix, catalog->suffixes,
        catalog->suffix_count, view->suffix_index);
    mu_layout_row(context, 1, &width, 0);
    mu_checkbox(context, "Show affixes for any part type and science", &view->is_affix_unfiltered);
    overlay_dim_label(context, "* cannot roll on the level-1 Basic item /summon creates");
    if (overlay_action_row(context, view, "Summon", OVERLAY_ACTION_SUMMON,
        rigblock == NULL ? "select a rigblock" : NULL)) {
        overlay_action action;
        overlay_new_action(&action, OVERLAY_ACTION_SUMMON);
        action.rigblock = rigblock->id;
        action.prefix1 = selected_affix_id(catalog->prefixes, catalog->prefix_count,
            view->prefix1_index);
        action.prefix2 = selected_affix_id(catalog->prefixes, catalog->prefix_count,
            view->prefix2_index);
        action.suffix = selected_affix_id(catalog->suffixes, catalog->suffix_count,
            view->suffix_index);
        overlay_send_action(view, &action);
    }
    draw_drops(context, view);
    overlay_draw_result(context, view);
    overlay_draw_picker(context, view);
}
#endif
