#ifndef DARKSPIN_FANG_OVERLAY_H
#define DARKSPIN_FANG_OVERLAY_H

/* In-game development overlay opened by /debug. Everything below is compiled
 * only into fangdebug,fangoverlay builds (overlay_debug.go defines
 * FANG_OVERLAY=1); release Fang keeps none of it.
 *
 * Vendored C, unmodified upstream. cgo does not track files below
 * thirdparty/, so update this note in the same change whenever one of them
 * changes; editing this top-level header rebuilds the whole package.
 *   thirdparty/microui/microui.c, thirdparty/microui/microui.h
 *     microui 2.02, MIT, (c) rxi, https://github.com/rxi/microui (src/),
 *     fetched 2026-10-10.
 *     microui.c sha256 880ce7e017fe307553586c9ba8291e9d9f56cdb20bc4188df072f1e8bd9af6dd
 *     microui.h sha256 9aa08e7f58c2152dbfcb4d7deb1f5d79ab1bf0c2809d87f3ff89dc34e24d0c63
 *   thirdparty/stb_easy_font.h
 *     stb_easy_font 1.1, public domain or MIT, Sean Barrett,
 *     https://github.com/nothings/stb, fetched 2026-10-10.
 *     sha256 7b48b52a316477766c0a7a096b33a29bcbd847b6fab4fabd32bb3fdb7cea1a60
 *     Header-only with static functions: never define an implementation macro.
 */

#ifndef FANG_OVERLAY
#define FANG_OVERLAY 0
#endif

#if FANG_OVERLAY
#include <stddef.h>
#include <windows.h>

/* Installs only the pass-through device capture and starts the loopback
 * network worker. Failures are traced and never reported to fang_install. */
void install_overlay(void* executable, const char* hostname, unsigned short port);

/* Returns nonzero when the game window procedure must not see the message. */
int overlay_window_message(HWND window, UINT message, WPARAM wparam, LPARAM lparam);

/* Handles OVERLAY_TOGGLE_MESSAGE on the game window thread. */
void overlay_toggle(HWND window);

/* True while an overlay text field owns the keyboard. */
int is_overlay_keyboard_owned(void);

/* Rewrites a first-word /debug to "/debug <key>" when a loopback key was
 * published. Returns the output length, or -1 to forward the text bare. */
int overlay_normalize_debug_command(const char* text, const char* command_text,
    char* output, size_t output_capacity);
#endif

#endif
