//go:build windows

package main

import (
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// Low-level keyboard hook: records the moment the user pressed
// a layout switch hotkey. Only a layout change right after such
// a hotkey counts as the user's choice — changes made by Windows
// (restoring a window's layout) or by apps are not remembered.
//
// Privacy: the hook sees every keystroke but only looks at modifiers,
// Space and `, and keeps only the time of the last hotkey. The keys
// themselves are never stored or logged.
//
// The hook lives on its own thread: Windows silently removes an LL hook
// whose thread stops responding, and MSAA calls can be slow.

const (
	whKeyboardLL = 13
	wmKeyDown    = 0x0100
	wmSysKeyDown = 0x0104

	vkShift, vkLShift, vkRShift = 0x10, 0xA0, 0xA1
	vkCtrl, vkLCtrl, vkRCtrl    = 0x11, 0xA2, 0xA3
	vkAlt, vkLAlt, vkRAlt       = 0x12, 0xA4, 0xA5
	vkLWin, vkRWin              = 0x5B, 0x5C
	vkSpace                     = 0x20
	vkGrave                     = 0xC0
)

type kbdLLHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

var lastHotkey atomic.Int64 // UnixNano of the last hotkey press

func lastHotkeyTime() time.Time { return time.Unix(0, lastHotkey.Load()) }

func keyDown(vk uintptr) bool {
	r, _, _ := procGetAsyncKeyState.Call(vk)
	return r&0x8000 != 0
}

func isAny(vk uint32, keys ...uint32) bool {
	for _, k := range keys {
		if vk == k {
			return true
		}
	}
	return false
}

func keyboardProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 {
		k := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
		down := wParam == wmKeyDown || wParam == wmSysKeyDown
		vk := k.VkCode
		// GetAsyncKeyState does not reflect the current key yet,
		// so it is taken into account separately (on both press and release:
		// Windows switches the layout on release).
		shift := isAny(vk, vkShift, vkLShift, vkRShift) || keyDown(vkShift)
		ctrl := isAny(vk, vkCtrl, vkLCtrl, vkRCtrl) || keyDown(vkCtrl)
		alt := isAny(vk, vkAlt, vkLAlt, vkRAlt) || keyDown(vkAlt)
		win := keyDown(vkLWin) || keyDown(vkRWin)

		switch {
		case shift && (ctrl || alt) && isAny(vk, vkShift, vkLShift, vkRShift, vkCtrl, vkLCtrl, vkRCtrl, vkAlt, vkLAlt, vkRAlt):
			lastHotkey.Store(time.Now().UnixNano()) // Ctrl+Shift, Alt+Shift
		case vk == vkSpace && win && down:
			lastHotkey.Store(time.Now().UnixNano()) // Win+Space
		case vk == vkGrave && down && !shift && !ctrl && !alt:
			lastHotkey.Store(time.Now().UnixNano()) // ` (if configured in Windows)
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// startHotkeyWatcher starts the keyboard hook thread.
func startHotkeyWatcher() error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		module, _, _ := procGetModuleHandleW.Call(0)
		hook, _, err := procSetWindowsHookExW.Call(whKeyboardLL, syscall.NewCallback(keyboardProc), module, 0)
		if hook == 0 {
			errc <- err
			return
		}
		defer procUnhookWindowsHookEx.Call(hook)
		errc <- nil
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
		}
	}()
	return <-errc
}
