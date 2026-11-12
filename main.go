//go:build windows

// KeyLayoutPin pins the keyboard layout to each input field
// separately. Switch the command field to en and the comment field to ja,
// and the layout comes back by itself when you return to either. Fields
// where the layout was never switched get the default layout
// from Windows settings.
//
// Layout of the code:
//   - internal/engine — when to switch and what to remember (no Windows);
//   - internal/store  — the memory file (no Windows);
//   - field.go        — what has focus: field key and kind (MSAA);
//   - tracker.go      — focus events → resolver threads → engine;
//   - window.go, tray.go, autostart.go, hotkey.go — the Windows shell parts;
//   - main.go         — wiring.
package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/dronem/keylayoutpin/internal/engine"
	"github.com/dronem/keylayoutpin/internal/store"
)

func main() {
	o := parseOptions()
	if o.version {
		if guiBuild() {
			messageBox("KeyLayoutPin " + versionString())
		} else {
			fmt.Println("KeyLayoutPin", versionString())
		}
		return
	}
	// Check this before touching any shared file (log rotation, memory):
	// a second copy must leave everything as it is.
	if (!o.diagnostic() || o.reset) && !singleInstance() {
		if o.reset {
			fatalf("KeyLayoutPin is running — exit it first (tray icon → Exit), otherwise it rewrites the memory")
		}
		notify("KeyLayoutPin is already running — see the icon in the notification area.")
		return
	}
	closeLog := setupLogging(o.logFile(), o.fullLog())
	defer closeLog()

	switch {
	case o.watch > 0:
		runWatch(o.watch, o.chromium)
	case o.probe > 0:
		runProbe(o.probe, o.chromium)
	case o.list:
		printLayouts()
	case o.reset:
		if err := os.Remove(o.db); err != nil && !os.IsNotExist(err) {
			fatalf("%v", err)
		}
		fmt.Println("Memory cleared:", o.db)
	default:
		run(o)
	}
}

// run is the normal mode: tray icon, layout switching.
func run(o *options) {
	fallback, err := fallbackFor(o.def)
	if err != nil {
		fatalf("%v", err)
	}
	mem, err := store.Open(o.db)
	if err != nil {
		fatalf("cannot read %s: %v", o.db, err)
	}
	set, err := loadSettings(settingsPath(o.db))
	if err != nil {
		errorf("cannot read %s, using the defaults: %v", set.path, err)
	}

	// The hooks and the window are bound to this thread.
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
	tr := newTray(win)
	defer tr.close()
	atExit(tr.close) // fatalf skips defers — don't leave a ghost icon
	tr.openSettings = func() {
		showSettings(set, tr.icon, func() {
			if !set.FieldDot {
				tr.hideDot()
			}
		})
	}

	eng := engine.New(engine.Config{
		System:   winSystem{keys},
		Memory:   storeMemory{mem},
		Fallback: fallback,
		Logf:     log.Printf,
		Errf:     errorf,
		Name:     layoutName,
		OnEnter: func(_ engine.Field, remembered bool) {
			if set.FieldDot {
				tr.flashDot(remembered)
			}
		},
	})
	det := newDetector(o.chromium, false)
	t := newTracker(engineSink{eng, o.verbose}, win, det.describe, det.foregroundChromium)
	unhook, err := t.hookFocus()
	if err != nil {
		fatalf("SetWinEventHook: %v", err)
	}
	defer unhook()
	if err := win.every(1, o.interval, t.tick); err != nil {
		fatalf("SetTimer: %v", err) // without the timer layout changes go unnoticed
	}
	if err := initAutostart(); err != nil {
		errorf("autostart: %v", err)
	}
	quitOnSignal()

	printLayouts()
	log.Printf("KeyLayoutPin %s running. Memory: %s (fields: %d). Ctrl+C to exit.", versionString(), o.db, mem.Len())
	messageLoop()
	log.Println("Exiting.")
}

// quitOnSignal ends the message loop on Ctrl+C or console close.
func quitOnSignal() {
	thread := getCurrentThreadID()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		procPostThreadMessageW.Call(uintptr(thread), wmQuit, 0, 0)
	}()
}

// winSystem implements engine.System on top of WinAPI.
type winSystem struct{ keys *hotkeys }

func (winSystem) Layout(tid uint32) HKL              { return keyboardLayout(tid) }
func (winSystem) Request(target uintptr, h HKL) bool { return requestLayout(target, h) }
func (winSystem) Foreground() uintptr                { return rootWindow(getForegroundWindow()) }
func (s winSystem) LastHotkey() time.Time            { return s.keys.Last() }
func (winSystem) Now() time.Time                     { return time.Now() }

// storeMemory implements engine.Memory on top of the memory file.
type storeMemory struct{ s *store.Store }

func (m storeMemory) Get(key string) (HKL, bool) {
	h, ok := m.s.Get(key)
	if !ok {
		return 0, false
	}
	h = installed(h) // the layout may have been removed from the system
	return h, h != 0
}

func (m storeMemory) Set(key string, h HKL) error { return m.s.Set(key, h) }
func (m storeMemory) Forget(key string) error     { return m.s.Forget(key) }

// engineSink feeds resolved focus to the engine.
type engineSink struct {
	e       *engine.Engine
	verbose bool
}

func (s engineSink) Focus(f focus) {
	fl := f.field
	if s.verbose {
		log.Printf("focus [%s] %s (%s): %s", fl.App, fl.Name, fl.Kind, fl.Key)
	} else if fl.Kind != engine.Window {
		fl.Name = "field" // field labels can be personal — logged only with -v
	}
	s.e.Focus(fl)
}

func (s engineSink) Tick() { s.e.Tick() }
