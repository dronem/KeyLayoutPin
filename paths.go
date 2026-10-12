//go:build windows

package main

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"unsafe"
)

var procExpandEnvironmentStringsW = kernel32.NewProc("ExpandEnvironmentStringsW")

// psEnv matches PowerShell-style variables: $env:LOCALAPPDATA.
var psEnv = regexp.MustCompile(`(?i)\$env:([a-z_][a-z0-9_]*)`)

// expandPath makes a path from the command line usable anywhere: it expands
// both %VAR% (cmd) and $env:VAR (PowerShell, in case it reached us
// unexpanded, e.g. from cmd or a shortcut) and makes the path absolute —
// autostart runs with a different working directory.
func expandPath(p string) (string, error) {
	if p == "" {
		return "", nil
	}
	p = psEnv.ReplaceAllString(p, "%$1%")
	src, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	buf := make([]uint16, 32768)
	n, _, callErr := procExpandEnvironmentStringsW.Call(uintptr(unsafe.Pointer(src)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", callErr
	}
	return filepath.Abs(syscall.UTF16ToString(buf))
}

// resolvePathFlag expands a path flag in place, so that autostart later
// records the resolved value.
func resolvePathFlag(name string) {
	f := flag.Lookup(name)
	p, err := expandPath(f.Value.String())
	if err != nil {
		fatalf("-%s %q: %v", name, f.Value.String(), err)
	}
	f.Value.Set(p)
}

// dataDir is the app's data folder under %LOCALAPPDATA%: unlike Roaming,
// it is not synced to servers along with the user profile.
func dataDir() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "KeyLayoutPin")
}

// defaultStorePath is where the memory lives unless -db says otherwise.
func defaultStorePath() string { return filepath.Join(dataDir(), "layouts.json") }

// defaultLogPath is where the log goes unless -log says otherwise.
func defaultLogPath() string { return filepath.Join(dataDir(), "log.txt") }
