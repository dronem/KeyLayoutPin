//go:build windows

package main

import (
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

// msgWindow is a hidden top-level window that receives the app's own
// messages (focus events, resolver results, timers) and the tray
// callbacks. Window messages, unlike thread messages, survive modal loops
// such as an open tray menu. All handlers run on the thread that created
// the window, so they need no locking.

var (
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procRegisterWindowMessageW = user32.NewProc("RegisterWindowMessageW")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procCreateMutexW           = kernel32.NewProc("CreateMutexW")
)

const (
	wmNull  = 0x0000
	wmClose = 0x0010

	errorAlreadyExists = 183
	mbYesNo            = 0x4
	mbIconInformation  = 0x40
	idYes              = 6
)

type wndClassEx struct {
	CbSize     uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msgWindow struct {
	hwnd     uintptr
	handlers map[uint32]func(wParam, lParam uintptr)
	timers   map[uintptr]func()
}

var windowCount atomic.Int32

func newMsgWindow() (*msgWindow, error) {
	w := &msgWindow{handlers: map[uint32]func(uintptr, uintptr){}, timers: map[uintptr]func(){}}
	module, _, _ := procGetModuleHandleW.Call(0)
	// One class per window: the window procedure is a closure over w.
	class, _ := syscall.UTF16PtrFromString("KeyLayoutPin_" + strconv.Itoa(int(windowCount.Add(1))))
	wc := wndClassEx{
		CbSize:    uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(w.wndProc),
		Instance:  module,
		ClassName: class,
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, err
	}
	// A real (hidden) top-level window rather than a message-only one:
	// TrackPopupMenu needs a window that can become foreground.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(class)),
		0, 0, 0, 0, 0, 0, 0, module, 0)
	if hwnd == 0 {
		return nil, err
	}
	w.hwnd = hwnd
	w.on(wmTimer, func(id, _ uintptr) {
		if f := w.timers[id]; f != nil {
			f()
		}
	})
	return w, nil
}

func (w *msgWindow) wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	if h := w.handlers[uint32(msg)]; h != nil {
		h(wParam, lParam)
		return 0
	}
	if msg == wmClose {
		procPostQuitMessage.Call(0) // e.g. taskkill without /F
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// on sets the handler for a message.
func (w *msgWindow) on(msg uint32, h func(wParam, lParam uintptr)) { w.handlers[msg] = h }

// post queues a message to the window; safe from any thread.
func (w *msgWindow) post(msg uint32) { procPostMessageW.Call(w.hwnd, uintptr(msg), 0, 0) }

// every runs f on the window's thread every d.
func (w *msgWindow) every(id uintptr, d time.Duration, f func()) error {
	if r, _, err := procSetTimer.Call(w.hwnd, id, uintptr(d.Milliseconds()), 0); r == 0 {
		return err
	}
	w.timers[id] = f
	return nil
}

func (w *msgWindow) close() {
	for id := range w.timers {
		procKillTimer.Call(w.hwnd, id)
	}
	procDestroyWindow.Call(w.hwnd)
}

// messageLoop runs until WM_QUIT (tray "Exit", Ctrl+C, WM_CLOSE).
func messageLoop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func registerWindowMessage(name string) uint32 {
	p, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(p)))
	return uint32(r)
}

func messageBox(text string) { showBox(text, mbIconInformation) }

// askBox shows a Yes/No question and reports whether Yes was chosen.
func askBox(text string) bool { return showBox(text, mbIconInformation|mbYesNo) == idYes }

func showBox(text string, flags uintptr) uintptr {
	t, _ := syscall.UTF16PtrFromString(text)
	c, _ := syscall.UTF16PtrFromString("KeyLayoutPin")
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), flags)
	return r
}

// singleInstance returns false if another copy is already running.
// The mutex lives as long as the process.
func singleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(`Local\KeyLayoutPin_single_instance`)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	return h != 0 && err != syscall.Errno(errorAlreadyExists)
}
