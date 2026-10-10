//go:build windows && cgo && (!fangoverlay || !fangdebug)

package main

/*
#cgo CFLAGS: -DFANG_OVERLAY=0
*/
import "C"
