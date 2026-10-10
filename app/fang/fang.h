#ifndef darkspin_FANG_H
#define darkspin_FANG_H

#include <winsock2.h>
#include <windows.h>

/* Private game-window messages posted by Fang. Keep them in one range so the
   chat and overlay handlers can never collide. */
#define CHAT_OPEN_MESSAGE (WM_APP + 0x45)
#define CHAT_RESET_MESSAGE (WM_APP + 0x46)
#define OVERLAY_TOGGLE_MESSAGE (WM_APP + 0x47)

int fang_install(const char* hostname, unsigned short port, unsigned short party_port,
    const char* trace_path, int skip_intro, int skip_cinematic,
    int enable_borderless_fullscreen, const char* jwt, const char* window_title);

int fang_install_display_preferences(HMODULE executable);
int fang_install_camera_zoom(HMODULE executable);
void fang_set_display_window(HWND window);
int fang_install_exception_trace(HANDLE trace, HMODULE executable);
void fang_trace_arsenal_snapshot(HMODULE executable);
void fang_read_stat_resource(unsigned int* object_id, unsigned int* hit_point_bits,
    unsigned int* power_point_bits, unsigned int* resource_mask);

__declspec(dllexport) DWORD WINAPI RecapInitializeThread(LPVOID parameter);

#endif
