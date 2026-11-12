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

// Win32 constants from WinUser.h; the comment gives the SDK name.
const (
	whKeyboardLL = 13     // WH_KEYBOARD_LL: SetWindowsHookEx — a low-level keyboard hook
	wmKeyDown    = 0x0100 // WM_KEYDOWN: a key was pressed
	wmSysKeyDown = 0x0104 // WM_SYSKEYDOWN: a key was pressed with Alt held

	// Virtual-key codes (VK_*): either / left / right key.
	vkShift, vkLShift, vkRShift = 0x10, 0xA0, 0xA1 // VK_SHIFT, VK_LSHIFT, VK_RSHIFT
	vkCtrl, vkLCtrl, vkRCtrl    = 0x11, 0xA2, 0xA3 // VK_CONTROL, VK_LCONTROL, VK_RCONTROL
	vkAlt, vkLAlt, vkRAlt       = 0x12, 0xA4, 0xA5 // VK_MENU, VK_LMENU, VK_RMENU (Alt)
	vkLWin, vkRWin              = 0x5B, 0x5C       // VK_LWIN, VK_RWIN
	vkSpace                     = 0x20             // VK_SPACE
	vkGrave                     = 0xC0             // VK_OEM_3: the grave accent / tilde key (can be set to switch layouts on its own)
)

type kbdLLHookStruct struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// hotkeys watches for layout switch hotkeys with a low-level keyboard hook.
type hotkeys struct {
	last atomic.Int64 // UnixNano of the last hotkey press
	proc uintptr      // the hook procedure, created once
}

// Last is when a layout switch hotkey was last pressed. Safe from any thread.
func (k *hotkeys) Last() time.Time { return time.Unix(0, k.last.Load()) }

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

// isLayoutHotkey decides whether a key event is part of a layout switch
// hotkey: Ctrl+Shift or Alt+Shift (on press and on release — Windows
// switches on release), Win+Space, or ` alone (if configured in Windows).
// shift/ctrl/alt include the key itself.
func isLayoutHotkey(vk uint32, down, shift, ctrl, alt, win bool) bool {
	switch {
	case shift && (ctrl || alt) && isAny(vk, vkShift, vkLShift, vkRShift, vkCtrl, vkLCtrl, vkRCtrl, vkAlt, vkLAlt, vkRAlt):
		return true
	case vk == vkSpace && win && down:
		return true
	case vk == vkGrave && down && !shift && !ctrl && !alt:
		return true
	}
	return false
}

func (k *hotkeys) keyboardProc(nCode, wParam, lParam uintptr) uintptr {
	if int32(nCode) >= 0 {
		ev := (*kbdLLHookStruct)(unsafe.Pointer(lParam))
		vk := ev.VkCode
		// GetAsyncKeyState does not reflect the current key yet,
		// so it is taken into account separately.
		shift := isAny(vk, vkShift, vkLShift, vkRShift) || keyDown(vkShift)
		ctrl := isAny(vk, vkCtrl, vkLCtrl, vkRCtrl) || keyDown(vkCtrl)
		alt := isAny(vk, vkAlt, vkLAlt, vkRAlt) || keyDown(vkAlt)
		win := keyDown(vkLWin) || keyDown(vkRWin)
		down := wParam == wmKeyDown || wParam == wmSysKeyDown
		if isLayoutHotkey(vk, down, shift, ctrl, alt, win) {
			k.last.Store(time.Now().UnixNano())
		}
	}
	r, _, _ := procCallNextHookEx.Call(0, nCode, wParam, lParam)
	return r
}

// hookRefresh: Windows silently removes a low-level hook whose callback
// was once too slow (LowLevelHooksTimeout), and there is no notification.
// Re-installing it periodically bounds how long hotkeys can go unnoticed.
const hookRefresh = 5 * time.Minute

func (k *hotkeys) install() (uintptr, error) {
	module, _, _ := procGetModuleHandleW.Call(0)
	hook, _, err := procSetWindowsHookExW.Call(whKeyboardLL, k.proc, module, 0)
	if hook == 0 {
		return 0, err
	}
	return hook, nil
}

// startHotkeys starts the keyboard hook on its own thread.
func startHotkeys() (*hotkeys, error) {
	k := &hotkeys{}
	k.proc = syscall.NewCallback(k.keyboardProc)
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		hook, err := k.install()
		if err != nil {
			errc <- err
			return
		}
		errc <- nil
		// A thread timer: this thread runs no modal loops, so thread
		// messages are safe here.
		procSetTimer.Call(0, 0, uintptr(hookRefresh.Milliseconds()), 0)
		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				procUnhookWindowsHookEx.Call(hook)
				return
			}
			if m.Hwnd == 0 && m.Message == wmTimer {
				// Install the new hook first, so there is no gap.
				if fresh, err := k.install(); err == nil {
					procUnhookWindowsHookEx.Call(hook)
					hook = fresh
				} else {
					errorf("keyboard hook refresh: %v", err)
				}
			}
		}
	}()
	return k, <-errc
}
