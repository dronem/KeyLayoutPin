//go:build windows

package main

import "time"

const (
	// requestTTL is how long to wait for an app to apply our layout
	// change request.
	requestTTL = 1500 * time.Millisecond
	// hotkeyWindow: a layout change within this time after Ctrl+Shift/Win+Space
	// counts as the user's choice.
	hotkeyWindow = 1500 * time.Millisecond
	// guardWindow: how long after a focus change foreign layout changes
	// are reverted (Windows or the app restoring the window's layout).
	guardWindow = time.Second
)

// Field is the object that received focus.
type Field struct {
	Key      string  // unique field key for the memory
	App      string  // exe name, for the log
	Name     string  // field name, for the log
	Root     uintptr // top-level window
	Tid      uint32  // window thread (the layout is read from it)
	Target   uintptr // window that receives layout change requests
	Editable bool    // input field; false for buttons, menus, lists, etc.
}

// System is everything the engine needs from Windows (faked in tests).
type System interface {
	Layout(tid uint32) HKL
	Request(target uintptr, h HKL) bool
	Foreground() uintptr   // top-level foreground window
	LastHotkey() time.Time // when a layout switch hotkey was last pressed
	Now() time.Time
}

// Memory is the "field → layout" memory.
type Memory interface {
	Get(key string) (HKL, bool)
	Set(key string, h HKL) error
	Forget(key string) error
}

type request struct {
	h  HKL
	at time.Time
}

// Engine decides when to switch the layout and when to remember it.
//
// Every observed layout change is one of three kinds:
//   - ours: we requested it ourselves (inflight). If the request was
//     for the previous field, the current field's layout is re-applied;
//   - the user's: right after Ctrl+Shift / Alt+Shift / Win+Space —
//     remembered for the current field;
//   - foreign: Windows or the app — not remembered, and reverted during
//     the first second after focus.
type Engine struct {
	sys      System
	mem      Memory
	fallback func() HKL                       // layout for new fields; 0 — leave as is
	logf     func(format string, args ...any) // info: goes to the log file only with -log
	errf     func(format string, args ...any) // errors: always go to the log file

	cur      *Field    // field holding the caret; buttons and menus don't change it
	focusAt  time.Time // when focus entered cur
	want     HKL       // layout cur should have (0 — any)
	last     HKL       // last observed layout
	inflight []request
}

func NewEngine(sys System, mem Memory, fallback func() HKL, logf, errf func(string, ...any)) *Engine {
	return &Engine{sys: sys, mem: mem, fallback: fallback, logf: logf, errf: errf}
}

// Focus is called on every focus change.
func (e *Engine) Focus(f Field) {
	// The user may have switched the layout and left the field before
	// the timer noticed. Account for it in the old field.
	e.Tick()

	// Buttons, menus, lists, the taskbar are not fields: leave the layout,
	// the last field stays current.
	if !f.Editable {
		return
	}

	same := e.cur != nil && e.cur.Key == f.Key
	if !same {
		e.leave()
	}
	e.cur = &f
	e.focusAt = e.sys.Now()
	e.last = e.sys.Layout(f.Tid)

	want, why := e.desired(f.Key)
	e.want = want
	if want == 0 || want == e.last {
		if !same && want != 0 {
			e.logf("[%s] %s: %s (%s)", f.App, f.Name, want.Display(), why)
		}
		return
	}
	if e.request(want) {
		e.logf("[%s] %s: %s → %s (%s)", f.App, f.Name, e.last.Display(), want.Display(), why)
	}
}

// Tick is called by the timer and detects layout changes.
func (e *Engine) Tick() {
	e.prune()
	if e.cur == nil {
		return
	}
	// Another thread's layout is reliable only while its window is active.
	// Keep last as is: the change is seen once the field's window is back in front.
	if e.sys.Foreground() != e.cur.Root {
		return
	}
	h := e.sys.Layout(e.cur.Tid)
	if h == 0 || h == e.last {
		return
	}
	e.last = h
	now := e.sys.Now()

	switch {
	case e.consume(h):
		// Our request landed, but the current field needs another layout
		// (the request was for the previous field) — apply the right one.
		if e.want != 0 && h != e.want && !e.pending(e.want) {
			e.request(e.want)
		}

	case now.Sub(e.sys.LastHotkey()) <= hotkeyWindow:
		e.want = h
		e.remember(h)

	case e.want != 0 && h != e.want && now.Sub(e.focusAt) <= guardWindow && !e.pending(e.want):
		e.logf("[%s] %s: layout changed without you (%s) — restoring %s", e.cur.App, e.cur.Name, h.Display(), e.want.Display())
		e.request(e.want)
	}
}

// leave remembers the layout the field had when it was left:
// it comes back on return even if Ctrl+Shift was never pressed there.
func (e *Engine) leave() {
	if e.cur == nil {
		return
	}
	h := e.last // last layout seen while the field's window was active
	if e.want != 0 && e.pending(e.want) {
		h = e.want // our request is in flight — the field will get what we asked for
	}
	if h == 0 {
		return
	}
	if old, ok := e.mem.Get(e.cur.Key); ok && old == h {
		return
	}
	e.remember(h)
}

// remember stores the current field's layout. If it equals the
// default layout, no entry is needed and it is removed: the less
// is stored about where you typed, the better.
func (e *Engine) remember(h HKL) {
	var err error
	if e.fallback != nil && e.fallback() == h {
		err = e.mem.Forget(e.cur.Key)
	} else {
		err = e.mem.Set(e.cur.Key, h)
	}
	if err != nil {
		e.errf("cannot save: %v", err)
		return
	}
	e.logf("[%s] %s: remembered %s", e.cur.App, e.cur.Name, h.Display())
}

func (e *Engine) desired(key string) (HKL, string) {
	if h, ok := e.mem.Get(key); ok {
		return h, "remembered"
	}
	if e.fallback != nil {
		if h := e.fallback(); h != 0 {
			return h, "default"
		}
	}
	return 0, ""
}

func (e *Engine) request(h HKL) bool {
	if !e.sys.Request(e.cur.Target, h) {
		e.errf("[%s] %s: window rejected the request for %s (running as administrator?)", e.cur.App, e.cur.Name, h.Display())
		return false
	}
	e.inflight = append(e.inflight, request{h: h, at: e.sys.Now()})
	return true
}

// consume removes a request for h from inflight, if present.
func (e *Engine) consume(h HKL) bool {
	for i, r := range e.inflight {
		if r.h == h {
			e.inflight = append(e.inflight[:i], e.inflight[i+1:]...)
			return true
		}
	}
	return false
}

func (e *Engine) pending(h HKL) bool {
	for _, r := range e.inflight {
		if r.h == h {
			return true
		}
	}
	return false
}

func (e *Engine) prune() {
	now := e.sys.Now()
	kept := e.inflight[:0]
	for _, r := range e.inflight {
		if now.Sub(r.at) < requestTTL {
			kept = append(kept, r)
			continue
		}
		if e.cur != nil && r.h == e.want && e.last != e.want {
			e.errf("[%s] %s: app did not switch the layout to %s", e.cur.App, e.cur.Name, r.h.Display())
		}
	}
	e.inflight = kept
}
