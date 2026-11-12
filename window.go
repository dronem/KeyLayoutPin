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
	procIsDialogMessageW       = user32.NewProc("IsDialogMessageW")
	procMessageBoxW            = user32.NewProc("MessageBoxW")
	procCreateMutexW           = kernel32.NewProc("CreateMutexW")
)

// Win32 constants from the Windows SDK headers; the comment gives the SDK name.
const (
	wmNull  = 0x0000 // WM_NULL: does nothing; wakes up a menu's message loop
	wmClose = 0x0010 // WM_CLOSE: a request to close (e.g. taskkill without /F)

	errorAlreadyExists = 183 // ERROR_ALREADY_EXISTS: the mutex exists — another copy runs

	// MessageBox: buttons, icon and the answer.
	mbYesNo           = 0x4  // MB_YESNO: Yes and No buttons
	mbIconInformation = 0x40 // MB_ICONINFORMATION: the "i" icon
	idYes             = 6    // IDYES: Yes was clicked
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
	timers   map[timerID]func()

	nextMsg   uint32  // last message number handed out by handle
	nextTimer timerID // last timer id handed out by newTimer
	dialog    uintptr // window whose Tab/Esc/Enter the loop handles; 0 — none
}

// timerID names a timer of a msgWindow; get one from newTimer.
type timerID uintptr

var windowCount atomic.Int32

func newMsgWindow() (*msgWindow, error) {
	w := &msgWindow{handlers: map[uint32]func(uintptr, uintptr){}, timers: map[timerID]func(){}, nextMsg: wmApp}
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
		if f := w.timers[timerID(id)]; f != nil {
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

// on sets the handler for a system message (WM_TIMER, a registered message).
// The app's own messages get their numbers from handle.
func (w *msgWindow) on(msg uint32, h func(wParam, lParam uintptr)) { w.handlers[msg] = h }

// handle allocates a new app message (WM_APP+n) handled by h and returns
// its number, so the app's messages can't collide.
func (w *msgWindow) handle(h func(wParam, lParam uintptr)) uint32 {
	w.nextMsg++
	w.on(w.nextMsg, h)
	return w.nextMsg
}

// post queues a message to the window; safe from any thread.
func (w *msgWindow) post(msg uint32) { procPostMessageW.Call(w.hwnd, uintptr(msg), 0, 0) }

// newTimer allocates a timer id for after and cancel.
func (w *msgWindow) newTimer() timerID {
	w.nextTimer++
	return w.nextTimer
}

// every runs f on the window's thread every d.
func (w *msgWindow) every(d time.Duration, f func()) error {
	id := w.newTimer()
	if r, _, err := procSetTimer.Call(w.hwnd, uintptr(id), uintptr(d.Milliseconds()), 0); r == 0 {
		return err
	}
	w.timers[id] = f
	return nil
}

// after runs f once on the window's thread after d. Calling it again with
// the same id before then restarts the countdown, so only the last f runs.
func (w *msgWindow) after(id timerID, d time.Duration, f func()) {
	if r, _, err := procSetTimer.Call(w.hwnd, uintptr(id), uintptr(d.Milliseconds()), 0); r == 0 {
		errorf("SetTimer: %v", err)
		return
	}
	w.timers[id] = func() {
		w.cancel(id)
		f()
	}
}

// cancel stops a timer started by after.
func (w *msgWindow) cancel(id timerID) {
	procKillTimer.Call(w.hwnd, uintptr(id))
	delete(w.timers, id)
}

func (w *msgWindow) close() {
	for id := range w.timers {
		procKillTimer.Call(w.hwnd, uintptr(id))
	}
	procDestroyWindow.Call(w.hwnd)
}

// setDialog makes the loop handle keyboard navigation (Tab, Esc, Enter)
// for a dialog-like window on this thread; 0 turns it off.
func (w *msgWindow) setDialog(hwnd uintptr) { w.dialog = hwnd }

// loop runs the thread's message loop until WM_QUIT (tray "Exit", Ctrl+C,
// WM_CLOSE).
func (w *msgWindow) loop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if w.dialog != 0 {
			if r, _, _ := procIsDialogMessageW.Call(w.dialog, uintptr(unsafe.Pointer(&m))); r != 0 {
				continue
			}
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
