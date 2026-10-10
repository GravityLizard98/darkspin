#ifndef DARKSPIN_FANG_COMMAND_CATALOG_H
#define DARKSPIN_FANG_COMMAND_CATALOG_H

#include <stddef.h>

/* Client-side chat vocabulary shared by chat normalization and the
   development overlay. The arrays are canonical names the server receives. */
extern const char* const fang_warp_locations[];
extern const size_t fang_warp_location_count;
extern const char* const fang_warp_aliases[];
extern const size_t fang_warp_alias_count;
extern const char* const fang_spawn_nouns[];
extern const size_t fang_spawn_noun_count;

int fang_warp_name_contains(const char* location, const char* partial, size_t partial_length);
const char* find_fang_warp_location(const char* partial, size_t partial_length);
const char* find_fang_spawn_noun(const char* partial, size_t partial_length);
int is_first_chat_token(const char* text, const char* command_text);

#endif
