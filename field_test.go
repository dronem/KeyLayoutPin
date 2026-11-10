//go:build windows

package main

import (
	"testing"
	"time"

	"github.com/dronem/keylayoutpin/internal/engine"
)

func TestNormalizeClass(t *testing.T) {
	cases := map[string]string{
		"WindowsForms10.EDIT.app.0.31b01ee_r8_ad1":            "WindowsForms10.EDIT.app",
		"WindowsForms10.Window.8.app.0.1f565bb_r8_ad1":        "WindowsForms10.Window.8.app",
		"HwndWrapper[MyApp.exe;;3f2a5b1c-1111-2222-3333-abc]": "HwndWrapper[MyApp.exe]",
		"Afx:00400000:8:00010003:00000000:00000000":           "Afx",
		"ATL:00A1B2C3":       "ATL",
		"Chrome_WidgetWin_1": "Chrome_WidgetWin_1",
	}
	for in, want := range cases {
		if got := normalizeClass(in); got != want {
			t.Errorf("normalizeClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestControlPart(t *testing.T) {
	if got := controlPart("Edit", 1001, 0x5000, false); got != "Edit#1001" {
		t.Errorf("stable dialog ID: %q", got)
	}
	if got := controlPart("Edit", 0x5000, 0x5000, false); got != "Edit" {
		t.Errorf("an ID equal to the HWND (WinForms) must be dropped: %q", got)
	}
	if got := controlPart("Main", 7, 0x5000, true); got != "Main" {
		t.Errorf("a top-level window has no dialog ID: %q", got)
	}
}

func TestClassify(t *testing.T) {
	text := accInfo{Name: "Chat 2", Role: roleSystemText, Editable: true}
	cases := []struct {
		name      string
		rootClass string
		info      accInfo
		ok        bool
		key       string
		kind      engine.Kind
	}{
		{"input field, digits normalized", "Chrome_WidgetWin_1", text, true,
			"Code.exe|Chrome_WidgetWin_1|ctl|42|Chat #", engine.Input},
		{"Chromium page document is not a field", "Chrome_WidgetWin_1",
			accInfo{Name: "Page", Role: roleSystemDocument, Editable: true}, true, "", engine.NotField},
		{"document elsewhere is a field (Word, WordPad)", "OpusApp",
			accInfo{Name: "Doc", Role: roleSystemDocument, Editable: true}, true,
			"Code.exe|OpusApp|ctl|15|Doc", engine.Input},
		{"no MSAA — whole window", "Game", accInfo{}, false, "Code.exe|Game|ctl", engine.Window},
		{"client area — whole window", "Game", accInfo{Role: roleSystemClient}, true, "Code.exe|Game|ctl", engine.Window},
		{"button is not a field", "Main", accInfo{Name: "OK", Role: 0x2B}, true, "", engine.NotField},
		{"taskbar is not a field", "Shell_TrayWnd", text, true, "", engine.NotField},
	}
	for _, c := range cases {
		key, _, kind := classify("Code.exe", c.rootClass, "ctl", c.info, c.ok)
		if key != c.key || kind != c.kind {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", c.name, key, kind, c.key, c.kind)
		}
	}
}

// VS Code terminals are one field: the title after the number follows the
// running process, and screen reader hints are appended.
func TestClassifyTerminal(t *testing.T) {
	const want = "Code.exe|Chrome_WidgetWin_1|ctl|42|Terminal #"
	for _, name := range []string{
		"Terminal 1",
		"Terminal 2, bash",
		"Terminal 1, keylayoutpin Run the command: Toggle Screen Reader Accessibility Mode",
		"Terminal 3 Run the command: Toggle Screen Reader Accessibility Mode for an optimized",
	} {
		key, _, _ := classify("Code.exe", "Chrome_WidgetWin_1", "ctl",
			accInfo{Name: name, Role: roleSystemText, Editable: true}, true)
		if key != want {
			t.Errorf("%q: got key %q, want %q", name, key, want)
		}
	}
	key, _, _ := classify("Code.exe", "Chrome_WidgetWin_1", "ctl",
		accInfo{Name: "Terminal tabs filter", Role: roleSystemText, Editable: true}, true)
	if key == want {
		t.Errorf("a field merely starting with \"Terminal\" must keep its name, got %q", key)
	}
}

func TestIsLayoutHotkey(t *testing.T) {
	cases := []struct {
		name                        string
		vk                          uint32
		down, shift, ctrl, alt, win bool
		want                        bool
	}{
		{"Ctrl+Shift released", vkLShift, false, true, true, false, false, true},
		{"Alt+Shift pressed", vkLAlt, true, true, false, true, false, true},
		{"Win+Space", vkSpace, true, false, false, false, true, true},
		{"grave alone", vkGrave, true, false, false, false, false, true},
		{"Shift alone", vkLShift, true, true, false, false, false, false},
		{"Ctrl+Shift+T — T is not a modifier", 'T', true, true, true, false, false, false},
		{"Space without Win", vkSpace, true, false, false, false, false, false},
		{"Shift+grave is a tilde, not the hotkey", vkGrave, true, true, false, false, false, false},
	}
	for _, c := range cases {
		if got := isLayoutHotkey(c.vk, c.down, c.shift, c.ctrl, c.alt, c.win); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

func TestWakeOnceHandlesReusedHWND(t *testing.T) {
	var woken []uintptr
	alive := map[uintptr]bool{}
	d := newDetector(true, false)
	d.wake = func(h uintptr) { woken = append(woken, h); alive[h] = true }
	d.alive = func(h uintptr) bool { return alive[h] }

	d.wakeOnce(0x10, 100)
	d.wakeOnce(0x10, 100) // same window again
	if len(woken) != 1 {
		t.Fatalf("woken %d times, want 1", len(woken))
	}
	d.wakeOnce(0x10, 200) // the HWND was reused by another process's window
	if len(woken) != 2 {
		t.Fatal("a reused HWND of another process must be woken again")
	}
}

func TestWakeOnceForgetsClosedWindows(t *testing.T) {
	alive := map[uintptr]bool{}
	d := newDetector(true, false)
	d.wake = func(h uintptr) { alive[h] = true }
	d.alive = func(h uintptr) bool { return alive[h] }

	for h := uintptr(1); h <= 200; h++ {
		d.wakeOnce(h, 1)
		delete(alive, h) // the window closes right away
	}
	if n := len(d.woken); n > prunePeriod {
		t.Fatalf("%d entries kept for closed windows, want at most %d", n, prunePeriod)
	}
}

// Chromium turns accessibility off again after a while: a window is woken
// again, but not more often than rewakeAfter.
func TestRewakeIsRateLimited(t *testing.T) {
	now := time.Unix(100, 0)
	woken := 0
	d := newDetector(true, false)
	d.wake = func(uintptr) { woken++ }
	d.alive = func(uintptr) bool { return true }
	d.now = func() time.Time { return now }

	if !d.wakeOnce(0x10, 1) || d.wakeOnce(0x10, 1) {
		t.Fatal("wakeOnce must report waking only the first time")
	}
	now = now.Add(rewakeAfter - time.Second)
	if d.rewake(0x10, 1) {
		t.Fatal("woken too soon after the last wake")
	}
	now = now.Add(time.Second)
	if !d.rewake(0x10, 1) || d.rewake(0x10, 1) {
		t.Fatal("must be woken again once, after rewakeAfter")
	}
	if !d.rewake(0x10, 2) {
		t.Fatal("a reused HWND of another process must be woken right away")
	}
	if woken != 3 {
		t.Fatalf("woken %d times, want 3", woken)
	}
}

func TestPageOnly(t *testing.T) {
	cases := []struct {
		info accInfo
		ok   bool
		want bool
	}{
		{accInfo{}, false, true},
		{accInfo{Role: roleSystemDocument}, true, true},
		{accInfo{Role: roleSystemClient}, true, true},
		{accInfo{Role: roleSystemText, Editable: true}, true, false},
		{accInfo{Role: 0x2B}, true, false}, // a button: focus inside is known
	}
	for _, c := range cases {
		if got := pageOnly(c.info, c.ok); got != c.want {
			t.Errorf("pageOnly(%+v, %v) = %v, want %v", c.info, c.ok, got, c.want)
		}
	}
}
