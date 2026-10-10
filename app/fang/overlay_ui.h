#ifndef DARKSPIN_FANG_OVERLAY_UI_H
#define DARKSPIN_FANG_OVERLAY_UI_H

/* Shared by the overlay UI files: overlay_ui.c (window, widgets, Info),
 * overlay_items.c (Items tab and pickers) and overlay_actions.c (Enemies,
 * Player and World tabs). Render thread only. */

#include "overlay_internal.h"

#if FANG_OVERLAY
#include <stddef.h>

#define OVERLAY_FILTER_LENGTH 48
#define OVERLAY_MATCH_LIMIT 8192
#define OVERLAY_ROW_HEIGHT 16
#define OVERLAY_LIST_HEIGHT 150
#define OVERLAY_PICKER_WIDTH 300
#define OVERLAY_PICKER_HEIGHT 200
#define OVERLAY_CONFIRM_MS 4000u
#define OVERLAY_SCALE_MAXIMUM 8
#define OVERLAY_SPAWN_FALLBACK_LIMIT 10
#define OVERLAY_LEVEL_FALLBACK_LIMIT 100
#define OVERLAY_DNA_LIMIT 10000000.0f
#define OVERLAY_AMOUNT_LIMIT 1000000.0f
#define OVERLAY_COORDINATE_LIMIT 100000.0f

enum overlay_tab {
    overlay_tab_info,
    overlay_tab_enemies,
    overlay_tab_items,
    overlay_tab_player,
    overlay_tab_world,
    overlay_tab_count
};

enum overlay_picker {
    overlay_picker_none,
    overlay_picker_slot,
    overlay_picker_class,
    overlay_picker_science,
    overlay_picker_prefix1,
    overlay_picker_prefix2,
    overlay_picker_suffix,
    overlay_picker_effect
};

enum overlay_warp_group {
    overlay_warp_all,
    overlay_warp_campaign,
    overlay_warp_survival,
    overlay_warp_pvp,
    overlay_warp_test,
    overlay_warp_editor,
    overlay_warp_hub,
    overlay_warp_group_count
};

/* What a filtered list was built from; the list is rebuilt only when this
 * changes, not on every frame. */
typedef struct overlay_query {
    const void* source;
    char filter[OVERLAY_FILTER_LENGTH];
    unsigned int parameters[6];
} overlay_query;

typedef struct overlay_list {
    overlay_query query;
    int is_built;
    unsigned int count;
    unsigned short indexes[OVERLAY_MATCH_LIMIT];
} overlay_list;

typedef struct overlay_row_range {
    unsigned int first;
    unsigned int last;
    int pitch;
} overlay_row_range;

/* Render-thread UI state. Window position lives in microui's container and
 * is kept in memory only. */
typedef struct overlay_ui {
    int tab;
    int scale_delta;
    int is_text_focused;
    overlay_state state;
    overlay_result result;
    const overlay_catalog* catalog;
    unsigned int pending_sequence;
    unsigned int confirm_kind;
    DWORD confirm_tick;
    char noun_filter[OVERLAY_FILTER_LENGTH];
    int noun_index;
    mu_Real spawn_count;
    char item_filter[OVERLAY_FILTER_LENGTH];
    unsigned int slot_bit;
    unsigned int class_bit;
    unsigned int science_bit;
    mu_Real item_level;
    int rigblock_index;
    int prefix1_index;
    int prefix2_index;
    int suffix_index;
    int is_affix_unfiltered;
    mu_Real level;
    mu_Real dna;
    mu_Real damage;
    mu_Real drain;
    char warp_filter[OVERLAY_FILTER_LENGTH];
    int warp_group;
    int warp_index;
    mu_Real goto_x;
    mu_Real goto_y;
    mu_Real goto_z;
    int is_goto_prefilled;
    int effect_index;
    int picker;
    char picker_filter[OVERLAY_FILTER_LENGTH];
    overlay_list noun_list;
    overlay_list rigblock_list;
    overlay_list warp_list;
    overlay_list picker_list;
} overlay_ui;

extern const mu_Color overlay_accent;

/* overlay_ui.c: widgets */
void overlay_label_format(mu_Context* context, const char* format, ...);
void overlay_dim_label(mu_Context* context, const char* text);
int overlay_button(mu_Context* context, const char* label, int is_enabled);
int overlay_colored_button(mu_Context* context, const char* label, mu_Color color, int opt);
int overlay_list_row(mu_Context* context, const char* label, int is_selected);
void overlay_text_box(mu_Context* context, overlay_ui* view, char* buffer, int size);
void overlay_filter_row(mu_Context* context, overlay_ui* view, char* filter, int size);
void overlay_clamp_real(mu_Real* number, mu_Real minimum, mu_Real maximum);
void overlay_slider(mu_Context* context, mu_Real* number, mu_Real low, mu_Real high,
    const char* format);
void overlay_begin_rows(mu_Context* context, const char* name, int width, int height,
    unsigned int count, overlay_row_range* row_range);
void overlay_end_rows(mu_Context* context, unsigned int count, const overlay_row_range* row_range);

/* overlay_ui.c: filtered lists */
int overlay_matches_filter(const char* name, const char* filter);
int overlay_begin_list(overlay_list* list, const overlay_query* query);
void overlay_add_match(overlay_list* list, unsigned int index);
void overlay_make_query(overlay_query* query, const void* source, const char* filter);

/* overlay_ui.c: availability, actions and results */
const char* overlay_action_block(const overlay_ui* view, unsigned int kind);
void overlay_new_action(overlay_action* action, unsigned int kind);
void overlay_send_action(overlay_ui* view, overlay_action* action);
int overlay_action_row(mu_Context* context, const overlay_ui* view, const char* label,
    unsigned int kind, const char* local_block);
int overlay_confirm_row(mu_Context* context, overlay_ui* view, const char* label,
    const char* confirm_label, unsigned int kind);
void overlay_draw_result(mu_Context* context, const overlay_ui* view);

/* overlay_items.c */
void overlay_open_picker(mu_Context* context, overlay_ui* view, int picker);
void overlay_draw_picker(mu_Context* context, overlay_ui* view);
void overlay_draw_items(mu_Context* context, overlay_ui* view);

/* overlay_actions.c */
void overlay_draw_enemies(mu_Context* context, overlay_ui* view);
void overlay_draw_player(mu_Context* context, overlay_ui* view);
void overlay_draw_world(mu_Context* context, overlay_ui* view);
#endif

#endif
