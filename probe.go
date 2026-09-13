//go:build windows

package main

import (
	"fmt"
	"log"
	"runtime"
	"time"
)

// Diagnostic modes. Their output contains field labels and window titles.

// runProbe prints once a second what Windows sees in focus: window,
// control, the MSAA chain to the focused element, and the layout.
func runProbe(d time.Duration, wake bool) {
	runtime.LockOSThread()
	coInitializeMTA()
	defer coUninitialize()
	det := newDetector(wake, false)
	var prev string
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(time.Second) {
		fg := getForegroundWindow()
		tid, pid := windowThreadProcess(fg)
		root := rootWindow(fg)
		if wake && isChromium(className(root)) {
			det.wakeOnce(root)
		}
		fw := focusWindow(tid)
		if fw == 0 {
			fw = fg
		}
		line := fmt.Sprintf("%s | window %s | control %s | layout %s\n", processName(pid), className(fg), className(fw), layoutName(keyboardLayout(tid)))
		if _, path, ok := focusedInWindow(root); ok {
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

// observer is the -watch sink: it logs every focus event in detail and
// every layout change, and never switches anything. It runs on the same
// tracker as the normal mode, so it shows exactly what the engine would get.
type observer struct {
	keys       *hotkeys
	lastLayout HKL
	lastApp    string
}

func (w *observer) Focus(f focus) {
	log.Printf("FOCUS %s hwnd=%s obj=%d child=%d layout=%s\n"+
		"    event:   ok=%v %s %q editable=%v\n"+
		"    walk:    ok=%v %s %q editable=%v\n"+
		"    result:  %s key=%s",
		f.field.App, className(f.ev.hwnd), f.ev.obj, f.ev.child, layoutName(keyboardLayout(f.field.Tid)),
		f.eventOK, roleName(f.event.Role), f.event.Name, f.event.Editable,
		f.walkOK, roleName(f.walk.Role), f.walk.Name, f.walk.Editable,
		f.field.Kind, f.field.Key)
}

func (w *observer) Tick() {
	fg := getForegroundWindow()
	tid, pid := windowThreadProcess(fg)
	h, app := keyboardLayout(tid), processName(pid)
	if h != w.lastLayout || app != w.lastApp {
		hot := time.Since(w.keys.Last()) < hotkeyWindow
		log.Printf("LAYOUT %s in %s (after hotkey: %v)", layoutName(h), app, hot)
		w.lastLayout, w.lastApp = h, app
	}
}

// hotkeyWindow mirrors the engine's rule for the -watch output.
const hotkeyWindow = 1500 * time.Millisecond

// runWatch is the observation mode.
func runWatch(d time.Duration, wake bool) {
	runtime.LockOSThread()
	keys, err := startHotkeys()
	if err != nil {
		fatalf("keyboard hook: %v", err)
	}
	win, err := newMsgWindow()
	if err != nil {
		fatalf("window: %v", err)
	}
	defer win.close()
	t := newTracker(&observer{keys: keys}, win, newDetector(wake, true).describe)
	unhook, err := t.hookFocus()
	if err != nil {
		fatalf("SetWinEventHook: %v", err)
	}
	defer unhook()
	if err := win.every(1, 100*time.Millisecond, t.tick); err != nil {
		fatalf("SetTimer: %v", err)
	}
	time.AfterFunc(d, func() { win.post(wmClose) })
	quitOnSignal()
	messageLoop()
}
