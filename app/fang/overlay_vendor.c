/* Deliberately includes no Windows header: wingdi.h defines RELATIVE and
 * ABSOLUTE, which collide with microui.c's layout enum. */
#ifndef FANG_OVERLAY
#define FANG_OVERLAY 0
#endif

#if FANG_OVERLAY
/* cgo compiles only top-level package files; the vendored microui
 * implementation is built through this translation unit. */
#include "thirdparty/microui/microui.c"
#endif
