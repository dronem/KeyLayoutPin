//go:build windows

package main

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/dronem/keylayoutpin/internal/engine"
)

// focus is what the detector found out about a focus event: the field,
// plus the raw MSAA answers for diagnostics (-watch).
type focus struct {
	ev      focusEvent
	field   engine.Field
	ok      bool // false: the window is already gone
	event   accInfo
	eventOK bool
	walk    accInfo // filled when the focus walk ran
	walkOK  bool
}

// detector turns focus events into fields. It does the Windows/MSAA part;
// the decisions are in the pure functions below (classify, normalizeClass).
type detector struct {
	wakeEnabled bool // enable accessibility in Chromium/Electron windows
	alwaysWalk  bool // diagnostics: always run the focus walk

	wake  func(root uintptr)      // wakes a Chromium window (wakeChromium)
	alive func(hwnd uintptr) bool // the window still exists (IsWindow)

	mu    sync.Mutex
	woken map[uintptr]uint32 // Chromium windows already woken → their process
}

// prunePeriod: how many new windows to wake between sweeps of closed ones.
const prunePeriod = 32

func newDetector(wakeEnabled, alwaysWalk bool) *detector {
	return &detector{
		wakeEnabled: wakeEnabled,
		alwaysWalk:  alwaysWalk,
		wake:        wakeChromium,
		alive:       isWindow,
		woken:       map[uintptr]uint32{},
	}
}

// wakeOnce wakes a Chromium window once. Windows reuses HWND values, so a
// window is identified by its HWND and process together: a new window that
// got the number of a closed one is woken again. Entries of closed windows
// are dropped from time to time, so the map doesn't grow forever.
func (d *detector) wakeOnce(root uintptr, pid uint32) {
	d.mu.Lock()
	done := d.woken[root] == pid
	if !done {
		if len(d.woken)%prunePeriod == prunePeriod-1 {
			for h := range d.woken {
				if !d.alive(h) {
					delete(d.woken, h)
				}
			}
		}
		d.woken[root] = pid
	}
	d.mu.Unlock()
	if !done {
		d.wake(root)
	}
}

// describe finds out what received focus. Safe to call from several
// resolver threads at once.
func (d *detector) describe(ev focusEvent) focus {
	r := focus{ev: ev}
	tid, pid := windowThreadProcess(ev.hwnd)
	if tid == 0 {
		return r
	}
	root := rootWindow(ev.hwnd)
	rootClass := normalizeClass(className(root))

	// Take the control from the thread's keyboard focus, not the event window,
	// so the key is the same no matter how the field was found.
	ctlHwnd := focusWindow(tid)
	if ctlHwnd == 0 {
		ctlHwnd = ev.hwnd
	}
	ctl := controlPart(normalizeClass(className(ctlHwnd)), dlgCtrlID(ctlHwnd), ctlHwnd, ctlHwnd == root)
	app := processName(pid)
	r.ok = true
	if shellWindows[rootClass] { // no need to ask MSAA
		key, name, kind := classify(app, rootClass, ctl, accInfo{}, false)
		r.field = engine.Field{Key: key, App: app, Name: name, Root: root, Tid: tid, Target: ctlHwnd, Kind: kind}
		return r
	}

	if d.wakeEnabled && isChromium(rootClass) {
		d.wakeOnce(root, pid)
	}

	info, ok := accessibleFromEvent(ev.hwnd, ev.obj, ev.child)
	r.event, r.eventOK = info, ok
	needWalk := !ok || !info.Editable
	if needWalk || d.alwaysWalk {
		r.walk, _, r.walkOK = focusedInWindow(root)
	}
	// The event targets the whole window (common in Chromium) — use what
	// the walk found inside if it is more specific.
	if needWalk && r.walkOK && r.walk.Role != 0 &&
		(!ok || r.walk.Editable || info.Role == roleSystemClient || info.Role == roleSystemWindow) {
		info, ok = r.walk, true
	}

	key, name, kind := classify(app, rootClass, ctl, info, ok)
	r.field = engine.Field{Key: key, App: app, Name: name, Root: root, Tid: tid, Target: ctlHwnd, Kind: kind}
	return r
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

// normalizeClass strips the run-specific parts of a window class name.
func normalizeClass(c string) string {
	for _, v := range volatileClass {
		c = v.re.ReplaceAllString(c, v.repl)
	}
	return c
}

// controlPart is the control's share of the key: its class plus the
// dialog control ID when that ID is stable.
func controlPart(class string, id int32, hwnd uintptr, isRoot bool) string {
	// WinForms and some other frameworks use the HWND as the ID —
	// it changes on every run, so such an ID is useless.
	if !isRoot && id != 0 && uintptr(uint32(id)) != hwnd&0xFFFFFFFF {
		return class + "#" + strconv.Itoa(int(id))
	}
	return class
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

func isChromium(rootClass string) bool { return strings.HasPrefix(rootClass, "Chrome_WidgetWin") }

var digits = regexp.MustCompile(`\d+`)

// terminalName matches VS Code terminal labels: "Terminal 2, powershell Run
// the command: …". The part after the number follows the running process
// and the screen reader hints, so all terminals become one field.
var terminalName = regexp.MustCompile(`^Terminal #([ ,].*)?$`)

// fieldKeyName is the field name's share of the key.
func fieldKeyName(name string) string {
	name = digits.ReplaceAllString(name, "#")
	if terminalName.MatchString(name) {
		return "Terminal #"
	}
	return name
}

// classify decides what kind of focus this is and builds the field key:
// app | window class | control part | role | field name.
func classify(app, rootClass, ctl string, info accInfo, ok bool) (key, name string, kind engine.Kind) {
	if shellWindows[rootClass] {
		return "", "Windows shell", engine.NotField
	}
	// A Chromium document is the whole page, not an input field.
	if ok && info.Role == roleSystemDocument && isChromium(rootClass) {
		info.Editable = false
	}
	base := app + "|" + rootClass + "|" + ctl
	switch {
	case ok && info.Editable:
		// Numbers in names usually change ("Terminal 2", "3 unread").
		key = base + "|" + strconv.Itoa(int(info.Role)) + "|" + fieldKeyName(info.Name)
		name = info.Name
		if name == "" {
			name = "(unnamed)"
		}
		return key, name, engine.Input
	case !ok || info.Role == roleSystemClient || info.Role == roleSystemWindow:
		// The app doesn't say what has focus (games, custom controls
		// without accessibility) — remember the layout for the whole window.
		return base, "(window)", engine.Window
	default:
		return "", roleName(info.Role) + " " + strconv.Quote(info.Name), engine.NotField
	}
}
