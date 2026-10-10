/* C COM call macros; must precede the first Windows header. */
#define COBJMACROS
#include "overlay_internal.h"

#if FANG_OVERLAY
#include "hook.h"

#include <d3d9.h>
#include <stddef.h>

typedef IDirect3D9* (WINAPI* overlay_direct3d_factory_fn)(UINT sdk_version);
typedef HRESULT (WINAPI* overlay_create_device_fn)(IDirect3D9* direct3d, UINT adapter,
    D3DDEVTYPE device_type, HWND focus_window, DWORD behavior_flags,
    D3DPRESENT_PARAMETERS* parameters, IDirect3DDevice9** device);

/* Build-103 addresses: the Direct3DCreate9 call site and import thunk also
 * observed by scenario_renderer.c, and the renderer vtable display.c checks.
 * The lazy renderer-singleton read (section 6.3) waits for S1a to record the
 * device field offset; until then only the call-site capture is used. */
#define OVERLAY_FACTORY_CALL_RVA 0x912B7Eu
#define OVERLAY_FACTORY_THUNK_RVA 0xA8EB88u
#define OVERLAY_RENDERER_VTABLE_RVA 0xC0F558u
#define OVERLAY_CREATE_DEVICE_SLOT (offsetof(IDirect3D9Vtbl, CreateDevice) / sizeof(void*))

typedef char overlay_create_device_slot_check[(OVERLAY_CREATE_DEVICE_SLOT == 16) ? 1 : -1];

enum {
    overlay_device_none = 0,
    overlay_device_renderer = 1,
    overlay_device_call_site = 2
};

static overlay_direct3d_factory_fn original_overlay_direct3d_factory;
static overlay_create_device_fn original_overlay_create_device;
static volatile LONG overlay_is_create_device_attempted;
static void* volatile overlay_game_direct3d;
static void* volatile overlay_captured_device;

/* Same fail-closed shape as display.c: an i386 PE image large enough for
 * every RVA used here, whose renderer vtable still holds the build-103
 * resize and mode-read entries. */
static int is_overlay_client(BYTE* base) {
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)base;
    IMAGE_NT_HEADERS* nt;
    if (sizeof(void*) != 4 || base == NULL || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE || dos->e_lfanew <= 0) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)(base + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE ||
        nt->FileHeader.Machine != IMAGE_FILE_MACHINE_I386 ||
        nt->OptionalHeader.SizeOfImage < OVERLAY_RENDERER_VTABLE_RVA + 0x20 ||
        !readable_range(base + OVERLAY_RENDERER_VTABLE_RVA, 0x20)) {
        return 0;
    }
    return *(void**)(base + OVERLAY_RENDERER_VTABLE_RVA + 0x18) == base + 0x48E8C0 &&
        *(void**)(base + OVERLAY_RENDERER_VTABLE_RVA + 0x1C) == base + 0x48DC90;
}

static int d3d9_image_range(const BYTE** start, size_t* size) {
    HMODULE module = GetModuleHandleA("d3d9.dll");
    IMAGE_DOS_HEADER* dos = (IMAGE_DOS_HEADER*)module;
    IMAGE_NT_HEADERS* nt;
    if (module == NULL || !readable_range(dos, sizeof(*dos)) ||
        dos->e_magic != IMAGE_DOS_SIGNATURE || dos->e_lfanew <= 0) {
        return 0;
    }
    nt = (IMAGE_NT_HEADERS*)((BYTE*)module + dos->e_lfanew);
    if (!readable_range(nt, sizeof(*nt)) || nt->Signature != IMAGE_NT_SIGNATURE) {
        return 0;
    }
    *start = (const BYTE*)module;
    *size = nt->OptionalHeader.SizeOfImage;
    return 1;
}

int is_d3d9_image_pointer(const void* pointer) {
    const BYTE* start;
    size_t size;
    if (pointer == NULL || !d3d9_image_range(&start, &size)) {
        return 0;
    }
    return (const BYTE*)pointer >= start && (const BYTE*)pointer < start + size;
}

/* A COM object implemented by the loaded d3d9.dll: its vtable and IUnknown
 * methods live in that image. Other slots may legitimately be hooked. */
static int is_d3d9_object(const void* object) {
    void* const* methods;
    if (!readable_range(object, sizeof(void*))) {
        return 0;
    }
    methods = *(void* const* const*)object;
    if (!is_d3d9_image_pointer(methods) || !readable_range(methods, 3 * sizeof(void*))) {
        return 0;
    }
    return is_d3d9_image_pointer(methods[0]) && is_d3d9_image_pointer(methods[1]) &&
        is_d3d9_image_pointer(methods[2]);
}

/* Variant of patch_pointer for vtable slots: the slot may share a page with
 * code (merged sections, wrappers), so an executable page stays executable
 * while it is written. The old protection is restored either way. */
int patch_vtable_slot(void** slot, void* expected, void* replacement) {
    MEMORY_BASIC_INFORMATION information;
    DWORD protection = PAGE_READWRITE;
    DWORD old_protection;
    DWORD ignored;
    PVOID previous;
    if (slot == NULL || replacement == NULL || !readable_range(slot, sizeof(void*)) ||
        *slot != expected || VirtualQuery(slot, &information, sizeof(information)) == 0) {
        return 0;
    }
    if ((information.Protect & (PAGE_EXECUTE | PAGE_EXECUTE_READ |
        PAGE_EXECUTE_READWRITE | PAGE_EXECUTE_WRITECOPY)) != 0) {
        protection = PAGE_EXECUTE_READWRITE;
    }
    if (!VirtualProtect(slot, sizeof(void*), protection, &old_protection)) {
        return 0;
    }
    previous = InterlockedCompareExchangePointer(slot, replacement, expected);
    VirtualProtect(slot, sizeof(void*), old_protection, &ignored);
    return previous == expected;
}

/* Pass-through: the game receives exactly the original result. The patched
 * vtable is shared by every IDirect3D9 in the process, so only devices made
 * through the game's own factory result are recorded. */
static HRESULT WINAPI capture_overlay_create_device(IDirect3D9* direct3d, UINT adapter,
    D3DDEVTYPE device_type, HWND focus_window, DWORD behavior_flags,
    D3DPRESENT_PARAMETERS* parameters, IDirect3DDevice9** device) {
    HRESULT result = original_overlay_create_device(direct3d, adapter, device_type,
        focus_window, behavior_flags, parameters, device);
    DWORD last_error = GetLastError();
    if (SUCCEEDED(result) && device != NULL && *device != NULL &&
        (void*)direct3d == InterlockedCompareExchangePointer(&overlay_game_direct3d,
            NULL, NULL)) {
        InterlockedExchangePointer(&overlay_captured_device, *device);
        trace_client_state("overlay_device_captured", 1);
    }
    SetLastError(last_error);
    return result;
}

static int patch_create_device(IDirect3D9* direct3d) {
    void** methods;
    void** slot;
    if (!is_d3d9_object(direct3d)) {
        return 0;
    }
    methods = *(void***)direct3d;
    if (!readable_range(methods, (OVERLAY_CREATE_DEVICE_SLOT + 1) * sizeof(void*))) {
        return 0;
    }
    slot = methods + OVERLAY_CREATE_DEVICE_SLOT;
    original_overlay_create_device = (overlay_create_device_fn)*slot;
    if (original_overlay_create_device == NULL ||
        !patch_vtable_slot(slot, (void*)original_overlay_create_device,
            (void*)capture_overlay_create_device)) {
        original_overlay_create_device = NULL;
        return 0;
    }
    return 1;
}

static IDirect3D9* WINAPI capture_overlay_direct3d(UINT sdk_version) {
    IDirect3D9* direct3d = original_overlay_direct3d_factory(sdk_version);
    DWORD last_error = GetLastError();
    if (direct3d != NULL) {
        InterlockedExchangePointer(&overlay_game_direct3d, direct3d);
    }
    /* Every IDirect3D9 shares one vtable, so its CreateDevice slot is
     * patched once; each later successful CreateDevice through the game's
     * IDirect3D9 is recorded. */
    if (direct3d != NULL &&
        InterlockedCompareExchange(&overlay_is_create_device_attempted, 1, 0) == 0) {
        trace_client_state("overlay_create_device_hook",
            (unsigned int)patch_create_device(direct3d));
    }
    SetLastError(last_error);
    return direct3d;
}

int install_overlay_device_capture(void* executable) {
    BYTE* base = (BYTE*)executable;
    if (!is_overlay_client(base) || !readable_range(base + OVERLAY_FACTORY_CALL_RVA, 5)) {
        return 0;
    }
    original_overlay_direct3d_factory =
        (overlay_direct3d_factory_fn)(base + OVERLAY_FACTORY_THUNK_RVA);
    /* patch_call refuses the site unless it still calls the expected thunk,
     * which also keeps a scenario observation of this site intact. */
    return patch_call(base + OVERLAY_FACTORY_CALL_RVA,
        (void*)original_overlay_direct3d_factory, (void*)capture_overlay_direct3d);
}

/* The game's latest device from the call-site capture. Without one (for
 * example Wine loading Fang after device creation) the overlay is unavailable
 * until S1a supplies the renderer device offset for the lazy read. No COM
 * method is called on the pointer here. */
void* overlay_acquire_device(void* executable) {
    void* device = InterlockedCompareExchangePointer(&overlay_captured_device, NULL, NULL);
    (void)executable;
    if (device != NULL && is_d3d9_object(device)) {
        trace_client_state("overlay_device_source", overlay_device_call_site);
        return device;
    }
    if (device != NULL) {
        /* A capture that no longer looks like a d3d9 object is dropped so
         * Present never compares against it. */
        InterlockedCompareExchangePointer(&overlay_captured_device, NULL, device);
    }
    trace_client_state("overlay_device_source", overlay_device_none);
    return NULL;
}

/* The device Present compares against: the game's latest CreateDevice
 * result, so a recreated device keeps the overlay. */
void* overlay_current_device(void) {
    return InterlockedCompareExchangePointer(&overlay_captured_device, NULL, NULL);
}
#endif
