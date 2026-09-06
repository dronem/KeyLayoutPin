//go:build windows

package main

import (
	"flag"
	"os"
	"strings"
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

func TestAutostartArgsKeepOnlyPersistentFlags(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("db", "", "")
	fs.String("default", "", "")
	fs.Bool("v", false, "")
	fs.Bool("list", false, "")
	fs.String("log", "", "")
	fs.Duration("watch", 0, "")
	if err := fs.Parse([]string{`-db=C:\a b\m.json`, "-v", "-default=ja", "-watch=3m", `-log=C:\l.txt`}); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(autostartArgs(fs), " ")
	want := `"-db=C:\a b\m.json" -default=ja -log=C:\l.txt`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestPESubsystem(t *testing.T) {
	self, _ := os.Executable() // the test binary is a console program
	s, err := peSubsystem(self)
	if err != nil || s != 3 {
		t.Fatalf("subsystem of %s = %d, %v; want 3 (console)", self, s, err)
	}
	if _, err := peSubsystem("autostart.go"); err == nil {
		t.Fatal("a non-PE file must be rejected")
	}
}
