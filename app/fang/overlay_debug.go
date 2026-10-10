//go:build windows && cgo && fangoverlay && fangdebug

package main

/*
#cgo CFLAGS: -DFANG_OVERLAY=1
*/
import "C"
