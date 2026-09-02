//go:build windows

// change_lang remembers the keyboard layout for each input field
// separately. Switch the command field to en and the comment field to ja,
// and the layout comes back by itself when you return to either. Fields
// where the layout was never switched get the default layout
// from Windows settings.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// winSystem implements System on top of WinAPI.
type winSystem struct{}

func (winSystem) Layout(tid uint32) HKL              { return keyboardLayout(tid) }
func (winSystem) Request(target uintptr, h HKL) bool { return requestLayout(target, h) }
func (winSystem) Foreground() uintptr                { return rootWindow(getForegroundWindow()) }
func (winSystem) LastHotkey() time.Time              { return lastHotkeyTime() }
func (winSystem) Now() time.Time                     { return time.Now() }

// storeMemory implements Memory on top of the JSON file.
type storeMemory struct{ s *Store }

func (m storeMemory) Get(key string) (HKL, bool) {
	e, ok := m.s.Get(key)
	if !ok {
		return 0, false
	}
	h, ok := e.HKL()
	if !ok {
		return 0, false
	}
	h = installed(h) // the layout may have been removed from the system
	return h, h != 0
}

func (m storeMemory) Set(key string, h HKL) error { return m.s.Set(key, h) }
func (m storeMemory) Forget(key string) error     { return m.s.Forget(key) }

type tracker struct {
	engine   *Engine
	verbose  bool
	threadID uint32
	window   uintptr // tray window: all of the app's messages go there

	// latest focus event from the hook, not yet handed to the resolver
	ev       focusEvent
	evNew    bool
	evQueued bool

	res       *resolver
	gen       uint64 // id of the last event handed to the resolver
	busy      bool   // resolver is processing an event
	busySince time.Time
}

var theTracker *tracker

// winEventProc is called by Windows from GetMessage on our own thread.
// It only records the event: resolving happens on a separate thread.
func winEventProc(hook, event, hwnd, idObject, idChild, idThread, eventTime uintptr) uintptr {
	t := theTracker
	if hwnd == 0 || t == nil {
		return 0
	}
	t.ev = focusEvent{hwnd: hwnd, obj: int32(idObject), child: int32(idChild)}
	t.evNew = true
	if !t.evQueued {
		t.evQueued = true
		procPostMessageW.Call(t.window, wmFocusChanged, 0, 0)
	}
	return 0
}

// handleMessage receives the app's own messages via the tray window.
func (t *tracker) handleMessage(msg uint32, _, _ uintptr) {
	switch msg {
	case wmFocusChanged:
		t.dispatch()
	case wmFieldResolved:
		t.handleResolved()
	case wmTimer:
		t.tick()
	}
}

// dispatch hands the latest event to the resolver if it is idle.
// While it is busy, new events just overwrite t.ev — only the latest
// focus matters.
func (t *tracker) dispatch() {
	t.evQueued = false
	if t.busy || !t.evNew {
		return
	}
	t.evNew = false
	t.gen++
	t.busy, t.busySince = true, time.Now()
	t.res.reqs <- resolveReq{gen: t.gen, ev: t.ev}
}

func (t *tracker) handleResolved() {
	for _, r := range takeResults() {
		if r.gen != t.gen {
			continue // reply from an abandoned hung thread
		}
		t.busy = false
		if !r.ok {
			continue
		}
		f := r.field
		// Field labels and titles can be personal — logged only with -v.
		if !t.verbose && f.Name != "(window)" {
			f.Name = "field"
		}
		if t.verbose {
			kind := "not a field"
			if f.Editable {
				kind = "field"
			}
			log.Printf("focus [%s] %s (%s): %s", f.App, f.Name, kind, f.Key)
		}
		t.engine.Focus(f)
	}
	t.dispatch()
}

func (t *tracker) tick() {
	if t.busy && time.Since(t.busySince) > resolveTimeout {
		_, pid := windowThreadProcess(t.ev.hwnd)
		errorf("app %s is not responding — skipping", processName(pid))
		close(t.res.reqs) // the hung thread exits when (if) it returns
		t.res = newResolver(t.window)
		t.busy = false
		t.evNew = true // retry the latest event on the new thread
		t.dispatch()
	}
	t.engine.Tick()
}

// Parts of window class names that change from run to run.
var volatileClass = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`\.app\.0\.[0-9a-fA-F]+(_r\d+_ad\d+)?`), ".app"},     // WinForms
	{regexp.MustCompile(`^HwndWrapper\[([^;\]]*);.*\]$`), "HwndWrapper[$1]"}, // WPF
	{regexp.MustCompile(`^Afx:.*`), "Afx"},                                   // MFC
	{regexp.MustCompile(`^ATL:[0-9a-fA-F]+$`), "ATL"},                        // ATL
}

// The taskbar, desktop and shell flyouts are not input fields,
// even though they don't say so via MSAA.
var shellWindows = map[string]bool{
	"Shell_TrayWnd":                       true,
	"Shell_SecondaryTrayWnd":              true,
	"Progman":                             true,
	"WorkerW":                             true,
	"NotifyIconOverflowWindow":            true,
	"TopLevelWindowForOverflowXamlIsland": true,
	"XamlExplorerHostIslandWindow":        true,
	"ForegroundStaging":                   true,
	"MultitaskingViewFrame":               true,
	"TaskSwitcherWnd":                     true,
}

func stableClassName(hwnd uintptr) string {
	c := className(hwnd)
	for _, v := range volatileClass {
		c = v.re.ReplaceAllString(c, v.repl)
	}
	return c
}

var (
	digits = regexp.MustCompile(`\d+`)
	// wakeChromiumApps controls whether accessibility is enabled in Chromium/Electron.
	wakeChromiumApps = true

	wokenMu sync.Mutex
	woken   = map[uintptr]bool{} // Chromium windows where accessibility is already enabled
)

// wakeOnce wakes a Chromium window only once.
func wakeOnce(root uintptr) {
	wokenMu.Lock()
	done := woken[root]
	woken[root] = true
	wokenMu.Unlock()
	if !done {
		wakeChromium(root)
	}
}

func isChromium(rootClass string) bool { return strings.HasPrefix(rootClass, "Chrome_WidgetWin") }

// describeFocus finds out what received focus and builds the field key:
// exe | window class | control class[#ID] | role | field name.
func describeFocus(hwnd uintptr, obj, child int32) (Field, bool) {
	tid, pid := windowThreadProcess(hwnd)
	if tid == 0 {
		return Field{}, false
	}
	root := rootWindow(hwnd)
	rootClass := stableClassName(root)

	// Take the control from the thread's keyboard focus, not the event window,
	// so the key is the same no matter how the field was found.
	focus := focusWindow(tid)
	if focus == 0 {
		focus = hwnd
	}
	ctl := stableClassName(focus)
	if focus != root {
		// WinForms and some other frameworks use the HWND as the ID —
		// it changes on every run, so such an ID is useless.
		if id := dlgCtrlID(focus); id != 0 && uintptr(uint32(id)) != focus&0xFFFFFFFF {
			ctl += "#" + strconv.Itoa(int(id))
		}
	}
	f := Field{App: processName(pid), Root: root, Tid: tid, Target: focus}
	if shellWindows[rootClass] {
		f.Name = "Windows shell"
		return f, true
	}

	// Chrome, VS Code, Slack and other Electron apps don't expose page
	// content via MSAA by default — turn it on.
	if wakeChromiumApps && isChromium(rootClass) {
		wakeOnce(root)
	}

	info, ok := accessibleFromEvent(hwnd, obj, child)
	if !ok || !info.Editable {
		// The event targets the whole window (common in Chromium) — find
		// which element inside actually has focus.
		if leaf, _, wok := focusedInWindow(root); wok && leaf.Role != 0 &&
			(!ok || leaf.Editable || info.Role == roleSystemClient || info.Role == roleSystemWindow) {
			info, ok = leaf, true
		}
	}
	// A Chromium document is the whole page, not an input field.
	if ok && info.Role == roleSystemDocument && isChromium(rootClass) {
		info.Editable = false
	}

	base := f.App + "|" + rootClass + "|" + ctl
	switch {
	case ok && info.Editable:
		// Numbers in names usually change ("Terminal 2", "3 unread").
		name := digits.ReplaceAllString(info.Name, "#")
		f.Key = base + "|" + strconv.Itoa(int(info.Role)) + "|" + name
		f.Name = info.Name
		if f.Name == "" {
			f.Name = "(unnamed)"
		}
		f.Editable = true
	case !ok || info.Role == roleSystemClient || info.Role == roleSystemWindow:
		// The app doesn't say what has focus (games, custom controls
		// without accessibility) — remember the layout for the whole window.
		f.Key = base
		f.Name = "(window)"
		f.Editable = true
	default:
		f.Name = roleName(info.Role) + " " + strconv.Quote(info.Name)
	}
	return f, true
}

func installed(h HKL) HKL {
	list := keyboardLayoutList()
	for _, l := range list {
		if l == h {
			return l
		}
	}
	for _, l := range list {
		if l.LangID() == h.LangID() {
			return l
		}
	}
	return 0
}

// layoutByLocale finds a layout by language ("en-US" or just "en"),
// preferring the language's standard keyboard.
func layoutByLocale(name string) HKL {
	var found HKL
	for _, l := range keyboardLayoutList() {
		loc := l.LocaleName()
		if !strings.EqualFold(loc, name) && !strings.EqualFold(strings.SplitN(loc, "-", 2)[0], name) {
			continue
		}
		if uint16(l>>16) == l.LangID() {
			return l
		}
		if found == 0 {
			found = l
		}
	}
	return found
}

// defaultLogPath is where the log goes unless -log says otherwise.
func defaultLogPath() string {
	return filepath.Join(localDataDir(), "log.txt")
}

// flagSet reports whether a flag was given on the command line.
func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// maxLogSize keeps the log from growing forever and piling up history.
const maxLogSize = 1 << 20

// rotateLog renames a large log to .old on startup (the previous .old
// is deleted), so at most two megabytes of history stay on disk.
func rotateLog(path string) {
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogSize {
		old := strings.TrimSuffix(path, filepath.Ext(path)) + ".old" + filepath.Ext(path)
		os.Remove(old)
		os.Rename(path, old)
	}
}

func printLayouts() {
	def := defaultInputLayout()
	fmt.Println("Installed layouts:")
	for _, l := range keyboardLayoutList() {
		mark := ""
		if l == def {
			mark = "  (default)"
		}
		fmt.Printf("  %s  %s%s\n", l, l.Display(), mark)
	}
}

func main() {
	var (
		dbPath   = flag.String("db", defaultStorePath(), "file with remembered layouts")
		defMode  = flag.String("default", "system", "layout for new fields: system (from Windows settings), keep (leave as is) or a language, e.g. en-US")
		interval = flag.Duration("interval", 100*time.Millisecond, "how often to check the current layout")
		verbose  = flag.Bool("v", false, "verbose log: field labels and window titles (personal data!)")
		chromium = flag.Bool("chromium", true, "enable accessibility in Chrome/Edge/VS Code etc. to tell apart fields inside pages")
		list     = flag.Bool("list", false, "list installed layouts and exit")
		reset    = flag.Bool("reset", false, "forget all remembered layouts and exit")
		watchFor = flag.Duration("watch", 0, "diagnostics: log focus events and layout changes without switching anything (e.g. 3m)")
		probeFor = flag.Duration("probe", 0, "diagnostics: print what has focus for the given duration (e.g. 30s)")
	)
	logOpt := &logFlag{path: defaultLogPath()}
	flag.Var(logOpt, "log", "full log to a file (up to 1 MB, then log.old.txt); -log alone means the default file. Without -log only errors go to the default file; -log=off disables the file")
	parseFlags(logOpt)
	// Diagnostic modes print field labels and window titles — they go to
	// a file only if -log was given explicitly, never to the default log.
	diagnostic := *watchFor > 0 || *probeFor > 0 || *list || *reset
	if logOpt.path != "off" {
		resolvePathFlag("log")
	}
	resolvePathFlag("db")
	log.SetFlags(log.Ltime)
	wakeChromiumApps = *chromium

	// The file gets errors always (unless -log=off) and everything else
	// only when -log was given explicitly.
	logFile := logOpt.path
	if logFile == "off" || (diagnostic && !flagSet("log")) {
		logFile = ""
	}
	fullLog := flagSet("log") && logFile != ""

	infoOut, errOut := teeWriter{os.Stderr}, teeWriter{os.Stderr}
	if logFile != "" {
		os.MkdirAll(filepath.Dir(logFile), 0o700)
		rotateLog(logFile)
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			fatalf("cannot open log: %v", err)
		}
		defer f.Close()
		errOut = append(errOut, f)
		if fullLog {
			infoOut = append(infoOut, f)
		}
	}
	infoW, errW := newAsyncWriter(infoOut), newAsyncWriter(errOut)
	defer infoW.Close()
	defer errW.Close()
	log.SetOutput(infoW)
	errLog.SetOutput(errW)

	if *watchFor > 0 {
		runtime.LockOSThread()
		watch(*watchFor, log.Printf)
		return
	}
	if *probeFor > 0 {
		runtime.LockOSThread()
		probe(*probeFor)
		return
	}
	if *list {
		printLayouts()
		return
	}
	if *reset {
		if err := os.Remove(*dbPath); err != nil && !os.IsNotExist(err) {
			fatalf("%v", err)
		}
		fmt.Println("Memory cleared:", *dbPath)
		return
	}

	var fallback func() HKL
	switch m := *defMode; m {
	case "keep":
	case "system":
		fallback = defaultInputLayout
	default:
		if layoutByLocale(m) == 0 {
			fatalf("layout %q is not installed (see -list)", m)
		}
		fallback = func() HKL { return layoutByLocale(m) }
	}

	store, err := loadStore(*dbPath)
	if err != nil {
		fatalf("cannot read %s: %v", *dbPath, err)
	}

	// The hook and timer are bound to a thread — the main loop runs on one.
	// COM (MSAA) runs on the resolver thread.
	runtime.LockOSThread()

	t := &tracker{
		engine:   NewEngine(winSystem{}, storeMemory{store}, fallback, log.Printf, errorf),
		verbose:  *verbose,
		threadID: getCurrentThreadID(),
	}
	if !singleInstance() {
		messageBox("change_lang is already running — see the icon in the notification area.")
		return
	}
	tr, err := newTray(t.handleMessage)
	if err != nil {
		fatalf("tray: %v", err)
	}
	defer tr.close()
	t.window = tr.hwnd
	t.res = newResolver(t.window)
	if err := initAutostart(); err != nil {
		errorf("autostart: %v", err)
	}
	theTracker = t

	if err := startHotkeyWatcher(); err != nil {
		fatalf("keyboard hook: %v", err)
	}

	procSetTimer.Call(t.window, 1, uintptr(interval.Milliseconds()), 0)
	defer procKillTimer.Call(t.window, 1)

	hook, _, err := procSetWinEventHook.Call(eventObjectFocus, eventObjectFocus, 0,
		syscall.NewCallback(winEventProc), 0, 0, wineventOutOfContext|wineventSkipOwnProcess)
	if hook == 0 {
		fatalf("SetWinEventHook: %v", err)
	}
	defer procUnhookWinEvent.Call(hook)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		procPostThreadMessageW.Call(uintptr(t.threadID), wmQuit, 0, 0)
	}()

	printLayouts()
	log.Printf("Running. Memory: %s (fields: %d). Ctrl+C to exit.", *dbPath, len(store.Entries))

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break // WM_QUIT: "Exit" in the tray menu or Ctrl+C
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	log.Println("Exiting.")
}
