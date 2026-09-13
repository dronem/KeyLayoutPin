//go:build windows

package main

import (
	"testing"

	"change_lang/internal/engine"
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
	text := accInfo{Name: "Terminal 2, bash", Role: roleSystemText, Editable: true}
	cases := []struct {
		name      string
		rootClass string
		info      accInfo
		ok        bool
		key       string
		kind      engine.Kind
	}{
		{"input field, digits normalized", "Chrome_WidgetWin_1", text, true,
			"Code.exe|Chrome_WidgetWin_1|ctl|42|Terminal #, bash", engine.Input},
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
