//go:build windows

package main

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// probe prints once a second what Windows sees in focus: window, control,
// the MSAA chain to the focused element, and the layout. For diagnostics.
func probe(d time.Duration) {
	coInitialize()
	defer coUninitialize()
	var prev string
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(time.Second) {
		fg := getForegroundWindow()
		tid, pid := windowThreadProcess(fg)
		if root := rootWindow(fg); isChromium(className(root)) {
			wakeOnce(root)
		}
		fw := focusWindow(tid)
		if fw == 0 {
			fw = fg
		}
		line := fmt.Sprintf("%s | window %s | control %s | layout %s\n", processName(pid), className(fg), className(fw), keyboardLayout(tid).Display())
		if _, path, ok := focusedInWindow(rootWindow(fg)); ok {
			for i, p := range path {
				line += fmt.Sprintf("    %*s%s %q editable=%v\n", i*2, "", roleName(p.Role), p.Name, p.Editable)
			}
		} else {
			line += "    MSAA unavailable\n"
		}
		if line != prev {
			fmt.Print(time.Now().Format("15:04:05 "), line)
			prev = line
		}
	}
}

// watchEv is state for -watch mode: observe and log only, never switch.
var watchEv struct {
	hwnd       uintptr
	obj, child int32
	queued     bool
	thread     uint32
	lastLayout HKL
	lastFgApp  string
	out        func(string, ...any)
}

func watchProc(hook, event, hwnd, idObject, idChild, idThread, eventTime uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	watchEv.hwnd, watchEv.obj, watchEv.child = hwnd, int32(idObject), int32(idChild)
	if !watchEv.queued {
		watchEv.queued = true
		procPostThreadMessageW.Call(uintptr(watchEv.thread), wmFocusChanged, 0, 0)
	}
	return 0
}

func watchFocus() {
	watchEv.queued = false
	hwnd, obj, child := watchEv.hwnd, watchEv.obj, watchEv.child
	tid, pid := windowThreadProcess(hwnd)
	root := rootWindow(hwnd)
	if isChromium(className(root)) {
		wakeOnce(root)
	}
	ev, evOK := accessibleFromEvent(hwnd, obj, child)
	leaf, _, leafOK := focusedInWindow(root)
	f, _ := describeFocus(hwnd, obj, child)
	watchEv.out("FOCUS %s hwnd=%s obj=%d child=%d layout=%s\n"+
		"    event:   ok=%v %s %q editable=%v\n"+
		"    walk:    ok=%v %s %q editable=%v\n"+
		"    result:  field=%v key=%s",
		processName(pid), className(hwnd), obj, child, keyboardLayout(tid).Display(),
		evOK, roleName(ev.Role), ev.Name, ev.Editable,
		leafOK, roleName(leaf.Role), leaf.Name, leaf.Editable,
		f.Editable, f.Key)
}

func watchTick() {
	fg := getForegroundWindow()
	tid, pid := windowThreadProcess(fg)
	h := keyboardLayout(tid)
	app := processName(pid)
	if h != watchEv.lastLayout || app != watchEv.lastFgApp {
		hot := time.Since(lastHotkeyTime()) < hotkeyWindow
		watchEv.out("LAYOUT %s in %s (after hotkey: %v)", h.Display(), app, hot)
		watchEv.lastLayout, watchEv.lastFgApp = h, app
	}
}

// watch is the observation mode: logs every focus event in detail
// and every layout change. It never switches anything.
func watch(d time.Duration, out func(string, ...any)) {
	watchEv.out = out
	watchEv.thread = getCurrentThreadID()
	coInitialize()
	defer coUninitialize()
	if err := startHotkeyWatcher(); err != nil {
		out("keyboard hook: %v", err)
	}
	hook, _, _ := procSetWinEventHook.Call(eventObjectFocus, eventObjectFocus, 0,
		syscall.NewCallback(watchProc), 0, 0, wineventOutOfContext|wineventSkipOwnProcess)
	defer procUnhookWinEvent.Call(hook)
	timer, _, _ := procSetTimer.Call(0, 0, 100, 0)
	defer procKillTimer.Call(0, timer)
	end := time.Now().Add(d)
	var m msg
	for time.Now().Before(end) {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		switch {
		case m.Hwnd == 0 && m.Message == wmFocusChanged:
			watchFocus()
		case m.Hwnd == 0 && m.Message == wmTimer:
			watchTick()
		default:
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}
