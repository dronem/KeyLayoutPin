//go:build windows

package main

import (
	"testing"
	"time"
)

const (
	en HKL = 0x04090409
	ja HKL = 0x04110411
)

// fakeSystem: a single global layout (the Windows default), layout
// change requests queue up until the test "delivers" them.
type fakeSystem struct {
	layout HKL
	queue  []HKL
	now    time.Time
	fg     uintptr   // foreground window
	hotkey time.Time // last Ctrl+Shift press
}

func (s *fakeSystem) Layout(uint32) HKL             { return s.layout }
func (s *fakeSystem) Request(_ uintptr, h HKL) bool { s.queue = append(s.queue, h); return true }
func (s *fakeSystem) Now() time.Time                { return s.now }
func (s *fakeSystem) wait(d time.Duration)          { s.now = s.now.Add(d) }
func (s *fakeSystem) Foreground() uintptr           { return s.fg }
func (s *fakeSystem) LastHotkey() time.Time         { return s.hotkey }

// userSwitch: the user pressed Ctrl+Shift.
func (s *fakeSystem) userSwitch(h HKL) { s.layout, s.hotkey = h, s.now }

// external: someone else changed the layout (Windows, the app).
func (s *fakeSystem) external(h HKL) { s.layout = h }

// deliver: the app applied all requests.
func (s *fakeSystem) deliver() {
	for _, h := range s.queue {
		s.layout = h
	}
	s.queue = nil
}

// deliverOne: the app applied the oldest request.
func (s *fakeSystem) deliverOne() {
	s.layout, s.queue = s.queue[0], s.queue[1:]
}

type fakeMemory map[string]HKL

func (m fakeMemory) Get(k string) (HKL, bool)  { h, ok := m[k]; return h, ok }
func (m fakeMemory) Set(k string, h HKL) error { m[k] = h; return nil }
func (m fakeMemory) Forget(k string) error     { delete(m, k); return nil }

func setup(def HKL) (*fakeSystem, fakeMemory, *Engine) {
	sys := &fakeSystem{layout: en, now: time.Unix(100, 0), fg: 1}
	mem := fakeMemory{}
	e := NewEngine(sys, mem, func() HKL { return def }, func(string, ...any) {})
	return sys, mem, e
}

func field(key string, root uintptr) Field {
	return Field{Key: key, Name: key, Root: root, Editable: true}
}

func button(root uintptr) Field { return Field{Name: "button", Root: root} }

func expect(t *testing.T, what string, got, want HKL) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: got %s, want %s", what, got, want)
	}
}

func TestRememberPerField(t *testing.T) {
	sys, mem, e := setup(en)
	cmd, comment := field("cmd", 1), field("comment", 1)

	e.Focus(cmd)
	sys.deliver()
	e.Tick()

	e.Focus(comment)
	sys.deliver()
	sys.userSwitch(ja)
	e.Tick()
	expect(t, "memory comment", mem["comment"], ja)

	e.Focus(cmd)
	sys.deliver()
	e.Tick()
	expect(t, "layout in cmd", sys.layout, en)

	e.Focus(comment)
	sys.deliver()
	e.Tick()
	expect(t, "layout in comment", sys.layout, ja)

	if _, ok := mem["cmd"]; ok {
		t.Fatal("cmd has the default layout — no need to store it")
	}
}

// The field went back to the default layout — the entry is removed.
func TestBackToDefaultForgets(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	sys.deliver()
	e.Tick()
	sys.userSwitch(ja)
	e.Tick()
	expect(t, "memory A", mem["A"], ja)
	sys.wait(2 * time.Second)
	sys.userSwitch(en)
	e.Tick()
	if _, ok := mem["A"]; ok {
		t.Fatal("the default layout must not be stored")
	}
}

func TestNewFieldGetsDefault(t *testing.T) {
	sys, mem, e := setup(en)
	sys.layout = ja

	e.Focus(field("new", 1))
	sys.deliver()
	e.Tick()
	expect(t, "layout", sys.layout, en)
	if len(mem) != 0 {
		t.Fatalf("the default layout must not be remembered: %v", mem)
	}
}

// The request for field A lands when focus is already in field B.
// It used to be remembered as the user's choice for B.
func TestStaleRequestIsNotUserChoice(t *testing.T) {
	sys, mem, e := setup(en)
	mem["A"], mem["B"] = en, ja
	sys.layout = ja

	e.Focus(field("A", 1)) // ask for en
	e.Focus(field("B", 1)) // B needs ja — it is already set, no request needed
	sys.deliverOne()       // the request for A lands
	e.Tick()

	expect(t, "memory B", mem["B"], ja)
	sys.deliver() // the engine must have asked for ja again
	e.Tick()
	expect(t, "layout in B", sys.layout, ja)
}

// Ctrl+Shift right after clicking into a field is remembered too.
func TestImmediateUserSwitch(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	sys.userSwitch(ja)
	e.Tick()
	expect(t, "memory A", mem["A"], ja)
}

// The user switched and moved to another field before the timer tick.
func TestSwitchThenLeaveBeforeTick(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	sys.userSwitch(ja)
	e.Focus(field("B", 1))
	expect(t, "memory A", mem["A"], ja)
	if _, ok := mem["B"]; ok {
		t.Fatal("nothing was chosen in B")
	}
}

func TestButtonInSameWindowKeepsField(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	e.Focus(button(1))
	if len(sys.queue) != 0 {
		t.Fatal("a button must not switch the layout")
	}
	sys.userSwitch(ja) // Ctrl+Shift while a button has focus
	e.Tick()
	expect(t, "memory A", mem["A"], ja)
}

func TestNonFieldInOtherWindowIsIgnored(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	e.Focus(button(2)) // another window, not a field
	sys.fg = 2
	sys.userSwitch(ja)
	e.Tick()
	if len(mem) != 0 || len(sys.queue) != 0 {
		t.Fatalf("non-field in another window: memory=%v queue=%v", mem, sys.queue)
	}
}

func TestKeepModeDoesNotTouchNewFields(t *testing.T) {
	sys, _, e := setup(0)
	sys.layout = ja
	e.Focus(field("new", 1))
	if len(sys.queue) != 0 {
		t.Fatal("keep mode must leave new fields alone")
	}
}

// The app ignored the request, then the user picked the same layout
// manually — that is their choice and must be remembered.
func TestIgnoredRequestExpires(t *testing.T) {
	sys, mem, e := setup(en)
	mem["A"] = en
	sys.layout = ja
	e.Focus(field("A", 1))
	sys.queue = nil // the app did not apply the request
	sys.wait(2 * time.Second)
	e.Tick()
	sys.userSwitch(en)
	e.Tick()
	sys.userSwitch(ja)
	e.Tick()
	expect(t, "memory A", mem["A"], ja)
}

// Windows/the app restored the window's layout right after focus:
// don't remember it, re-apply the right one.
func TestExternalChangeAfterFocusIsReverted(t *testing.T) {
	sys, mem, e := setup(en)
	mem["A"] = en
	e.Focus(field("A", 1))
	sys.wait(200 * time.Millisecond)
	sys.external(ja)
	e.Tick()
	expect(t, "memory A", mem["A"], en)
	sys.deliver()
	e.Tick()
	expect(t, "layout", sys.layout, en)
}

// A later foreign change (e.g. Punto Switcher fixing text) —
// neither remember nor fight it.
func TestLateExternalChangeIsLeftAlone(t *testing.T) {
	sys, mem, e := setup(en)
	mem["A"] = en
	e.Focus(field("A", 1))
	sys.wait(5 * time.Second)
	sys.external(ja)
	e.Tick()
	expect(t, "memory A", mem["A"], en)
	if len(sys.queue) != 0 {
		t.Fatal("late foreign changes must not be reverted")
	}
}

// Win+Space while a shell flyout is in the foreground: the change
// is credited to the field once its window is active again.
func TestSwitchWhileFlyoutInFront(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("A", 1))
	sys.deliver()
	e.Tick()
	sys.fg = 3
	sys.userSwitch(ja)
	e.Tick()
	if _, ok := mem["A"]; ok {
		t.Fatal("the layout must not be read while the field window is inactive")
	}
	sys.wait(300 * time.Millisecond)
	sys.fg = 1
	e.Tick()
	expect(t, "memory A", mem["A"], ja)
}

// The reported scenario: the field has ja (Ctrl+Shift never pressed there),
// set en in the terminal (another window), come back — ja again.
func TestLeaveRemembersLayout(t *testing.T) {
	sys, mem, e := setup(0) // leave new fields as is, so ja comes from outside
	sys.layout = ja
	e.Focus(field("chat", 1))
	e.Tick()

	sys.fg = 2
	e.Focus(field("terminal", 2))
	sys.userSwitch(en)
	e.Tick()
	expect(t, "memory terminal", mem["terminal"], en)

	sys.fg = 1
	e.Focus(field("chat", 1))
	sys.deliver()
	e.Tick()
	expect(t, "layout in chat", sys.layout, ja)

	sys.fg = 2
	e.Focus(field("terminal", 2))
	sys.deliver()
	e.Tick()
	expect(t, "layout in terminal", sys.layout, en)
}

// Same, but the new field first got the default layout and the user
// changed it without a hotkey (e.g. with the mouse in the tray).
func TestLeaveRemembersNonHotkeyChange(t *testing.T) {
	sys, mem, e := setup(en)
	e.Focus(field("chat", 1))
	sys.deliver()
	e.Tick()
	sys.wait(3 * time.Second)
	sys.external(ja)
	e.Tick()
	e.Focus(field("other", 1))
	expect(t, "memory chat", mem["chat"], ja)
}
