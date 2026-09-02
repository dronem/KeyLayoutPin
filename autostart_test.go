//go:build windows

package main

import (
	"syscall"
	"testing"
	"unsafe"
)

// Uses its own scratch key — never touches the real Run key.
const testKey = `Software\change_lang_test`

func TestRegistryRoundTrip(t *testing.T) {
	t.Cleanup(func() {
		p, _ := syscall.UTF16PtrFromString(testKey)
		procRegDeleteKeyW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)))
	})
	if _, err := regGetString(testKey, "v"); err == nil {
		t.Fatal("value must not exist yet")
	}
	cmd := `"C:\Program Files\x\change_lang_bg.exe" -log "C:\a b\log.txt"`
	if err := regSetString(testKey, "v", cmd); err != nil {
		t.Fatal(err)
	}
	got, err := regGetString(testKey, "v")
	if err != nil || got != cmd {
		t.Fatalf("got %q, %v", got, err)
	}
	if err := regDeleteValue(testKey, "v"); err != nil {
		t.Fatal(err)
	}
	if _, err := regGetString(testKey, "v"); err == nil {
		t.Fatal("value must be gone")
	}
	if err := regDeleteValue(testKey, "v"); err != nil {
		t.Fatalf("deleting a missing value must not fail: %v", err)
	}
}

func TestCommandExe(t *testing.T) {
	cases := map[string]string{
		`"C:\Program Files\x\a.exe" -log "C:\l.txt"`: `C:\Program Files\x\a.exe`,
		`C:\x\a.exe -v`: `C:\x\a.exe`,
		`C:\x\a.exe`:    `C:\x\a.exe`,
	}
	for in, want := range cases {
		if got := commandExe(in); got != want {
			t.Errorf("commandExe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutostartCommandQuotesPath(t *testing.T) {
	cmd, err := autostartCommand()
	if err != nil {
		t.Fatal(err)
	}
	if commandExe(cmd) == "" {
		t.Fatalf("cannot parse exe from %q", cmd)
	}
}

func TestSingleInstance(t *testing.T) {
	if !singleInstance() {
		t.Skip("change_lang is running — cannot test")
	}
	if singleInstance() {
		t.Fatal("second instance must be refused")
	}
}
