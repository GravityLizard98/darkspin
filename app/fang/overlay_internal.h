#ifndef DARKSPIN_FANG_OVERLAY_INTERNAL_H
#define DARKSPIN_FANG_OVERLAY_INTERNAL_H

/* Shared by the overlay C files and overlay_net.go. This header is part of a
 * cgo export preamble, so it may contain declarations only. */

#include "fang.h"
#include "overlay.h"

#if FANG_OVERLAY
#include "thirdparty/microui/microui.h"

#define OVERLAY_KEY_LENGTH 64
#define OVERLAY_NPC_LIMIT 16
#define OVERLAY_NAME_LENGTH 64
#define OVERLAY_REASON_LENGTH 24
#define OVERLAY_TEXT_LENGTH 128
#define OVERLAY_TOKEN_LENGTH 24
#define OVERLAY_VOCABULARY_LIMIT 30
#define OVERLAY_VOCABULARY_ALL 0x80000000u
#define OVERLAY_RECT_LIMIT 4

/* Wire kinds, in the order of section 12.2; overlay_net.go names them. */
enum overlay_action_kind {
    OVERLAY_ACTION_SPAWN,
    OVERLAY_ACTION_SUMMON,
    OVERLAY_ACTION_DROP,
    OVERLAY_ACTION_LEVEL,
    OVERLAY_ACTION_DNA,
    OVERLAY_ACTION_HEAL,
    OVERLAY_ACTION_POWER_FILL,
    OVERLAY_ACTION_DAMAGE,
    OVERLAY_ACTION_POWER_DRAIN,
    OVERLAY_ACTION_GOTO,
    OVERLAY_ACTION_EVENT,
    OVERLAY_ACTION_KILL,
    OVERLAY_ACTION_RECAP,
    OVERLAY_ACTION_VICTORY,
    OVERLAY_ACTION_RESET,
    OVERLAY_ACTION_DEFEAT,
    OVERLAY_ACTION_WARP,
    OVERLAY_ACTION_EFFECT,
    OVERLAY_ACTION_KIND_COUNT
};

/* GoOverlayStart result. */
enum overlay_net_mode {
    OVERLAY_NET_IDLE,
    OVERLAY_NET_STARTED,
    OVERLAY_NET_REMOTE,
    OVERLAY_NET_KEY_FAILED
};

enum overlay_connection {
    OVERLAY_CONNECTION_IDLE,
    OVERLAY_CONNECTION_CONNECTING,
    OVERLAY_CONNECTION_CONNECTED,
    OVERLAY_CONNECTION_UNBOUND,
    OVERLAY_CONNECTION_DISABLED,
    OVERLAY_CONNECTION_FORBIDDEN,
    OVERLAY_CONNECTION_UNREACHABLE,
    OVERLAY_CONNECTION_SCHEMA,
    OVERLAY_CONNECTION_FAILED
};

/* Integer-only network traces; the key and request paths are never traced. */
enum overlay_net_trace {
    OVERLAY_TRACE_KEY_LENGTH,
    OVERLAY_TRACE_NET_STATUS,
    OVERLAY_TRACE_CATALOG_STATUS,
    OVERLAY_TRACE_ACTION_STATUS,
    OVERLAY_TRACE_CATALOG_RIGBLOCKS,
    OVERLAY_TRACE_NET_RECOVERED
};

typedef struct overlay_npc {
    unsigned long long object_id;
    float hit_point;
    float hit_point_max;
    float distance;
    char name[OVERLAY_NAME_LENGTH];
    char direction[16];
} overlay_npc;

typedef struct overlay_availability {
    unsigned int is_available;
    char reason[OVERLAY_REASON_LENGTH];
} overlay_availability;

/* Latest GET /debug/v1/state plus connection status, published by Go under
 * a seqlock and copied by the render thread once per frame. */
typedef struct overlay_state {
    unsigned int connection;
    int http_status;
    unsigned int latency_ms;
    unsigned int is_received;
    unsigned int is_build_match;
    char build_id[48];
    char version[32];
    char display_name[OVERLAY_NAME_LENGTH];
    unsigned int level;
    unsigned long long dna;
    char pending_warp[OVERLAY_TEXT_LENGTH];
    unsigned int is_game;
    unsigned long long game_id;
    char mode[16];
    unsigned int is_warped;
    unsigned int player_count;
    unsigned int is_hero;
    unsigned int is_deployed;
    unsigned long long object_id;
    float x;
    float y;
    float z;
    float hit_point;
    float hit_point_max;
    float power_point;
    float power_point_max;
    unsigned int alive_npc_count;
    unsigned int npc_count;
    overlay_npc npcs[OVERLAY_NPC_LIMIT];
    overlay_availability actions[OVERLAY_ACTION_KIND_COUNT];
} overlay_state;

/* Masks index the matching catalog vocabulary; OVERLAY_VOCABULARY_ALL means
 * the item lists "all". slot_mask is a rigblock slot or affix part types. */
typedef struct overlay_item {
    unsigned int id;
    unsigned int minimum_level;
    unsigned int maximum_level;
    unsigned int slot_mask;
    unsigned int class_mask;
    unsigned int science_mask;
    unsigned int is_unique;
    unsigned int is_basic_eligible;
    char name[OVERLAY_NAME_LENGTH];
} overlay_item;

typedef struct overlay_name {
    char text[OVERLAY_NAME_LENGTH];
} overlay_name;

typedef struct overlay_vocabulary {
    unsigned int count;
    char names[OVERLAY_VOCABULARY_LIMIT][OVERLAY_TOKEN_LENGTH];
} overlay_vocabulary;

/* One immutable C.malloc block built by Go; a replaced block is retired and
 * never freed because the render thread may still read it. */
typedef struct overlay_catalog {
    unsigned int spawn_count_limit;
    unsigned int level_limit;
    unsigned int rigblock_count;
    unsigned int prefix_count;
    unsigned int suffix_count;
    unsigned int event_count;
    unsigned int effect_count;
    unsigned int drop_category_count;
    overlay_vocabulary slots;
    overlay_vocabulary classes;
    overlay_vocabulary sciences;
    const overlay_item* rigblocks;
    const overlay_item* prefixes;
    const overlay_item* suffixes;
    const overlay_name* events;
    const overlay_name* effects;
    const overlay_name* drop_categories;
} overlay_catalog;

/* Single-slot mailbox payload; text holds the noun, category, event, effect
 * or warp area of the kinds that take one. */
typedef struct overlay_action {
    unsigned int sequence;
    unsigned int kind;
    unsigned int count;
    unsigned int level;
    unsigned int dna;
    unsigned int rigblock;
    unsigned int prefix1;
    unsigned int prefix2;
    unsigned int suffix;
    float amount;
    float x;
    float y;
    float z;
    char text[OVERLAY_TEXT_LENGTH];
} overlay_action;

typedef struct overlay_result {
    unsigned int action_sequence;
    unsigned int kind;
    int http_status;
    unsigned int queued_count;
    unsigned int requested_count;
    unsigned long long dna_total;
    char code[16];
    char reason[OVERLAY_REASON_LENGTH];
    char message[OVERLAY_TEXT_LENGTH];
} overlay_result;

/* Render thread -> window procedure, under a seqlock: what the last frame
 * drew, so the input filter can decide whether a message is the overlay's. */
typedef struct overlay_layout {
    DWORD published_tick;
    int back_buffer_width;
    int back_buffer_height;
    int scale;
    int is_destination;
    RECT destination;
    int is_text_focused;
    int rect_count;
    mu_Rect rects[OVERLAY_RECT_LIMIT];
} overlay_layout;

typedef struct overlay_frame {
    int width;
    int height;
    int scale;
    int automatic_scale;
    float frame_ms;
    unsigned int frame_rate;
} overlay_frame;

/* overlay.c: bridge called by overlay_net.go. */
void overlay_publish_key(const char* key, unsigned int length);
void overlay_trace_net(unsigned int trace, unsigned int value);
void overlay_net_wait(unsigned int milliseconds);
int is_overlay_open(void);
unsigned int overlay_open_generation(void);
void overlay_publish_state(const overlay_state* state);
overlay_catalog* overlay_allocate_catalog(size_t size);
void overlay_publish_catalog(overlay_catalog* catalog);
int overlay_take_action(overlay_action* action);
void overlay_publish_result(const overlay_result* result);

/* overlay.c: render-thread side of the bridge. */
int overlay_read_state(overlay_state* state);
const overlay_catalog* overlay_current_catalog(void);
unsigned int overlay_submit_action(overlay_action* action);
int is_overlay_action_busy(void);
int overlay_read_result(overlay_result* result);
unsigned int overlay_current_net_mode(void);
void overlay_set_keyboard_owned(int is_owned);
void overlay_seqlock_write(volatile LONG* sequence, void* destination,
    const void* source, size_t size);
int overlay_seqlock_read(volatile LONG* sequence, void* destination,
    const void* source, size_t size);

/* overlay_device.c */
int install_overlay_device_capture(void* executable);
void* overlay_acquire_device(void* executable);
void* overlay_current_device(void);
int is_d3d9_image_pointer(const void* pointer);
int patch_vtable_slot(void** slot, void* expected, void* replacement);

/* overlay_draw.c */
int install_overlay_draw(void);
int install_overlay_present(void* device);
void reset_overlay_frame_clock(void);

/* overlay_input.c */
void overlay_input_open(HWND window);
void overlay_input_close(void);
void overlay_input_drain(mu_Context* context);
void overlay_input_publish(const overlay_layout* layout);
int is_overlay_return_consumed(void);

/* overlay_ui.c */
void overlay_build_ui(mu_Context* context, const overlay_frame* frame, int* is_text_focused);
int overlay_ui_scale_delta(void);
#endif

#endif
