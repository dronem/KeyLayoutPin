//go:build windows

package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"
	"unsafe"
)

var (
	procExpandEnvironmentStringsW = kernel32.NewProc("ExpandEnvironmentStringsW")
	procGetConsoleWindow          = kernel32.NewProc("GetConsoleWindow")
)

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

// fatalf reports a startup error and exits. The GUI build has no console,
// so there the error is also shown in a message box instead of vanishing.
func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	errorf("%s", msg) // console and, once set up, the log file
	if c, _, _ := procGetConsoleWindow.Call(); c == 0 {
		messageBox("change_lang cannot start:\n\n" + msg)
	} else {
		time.Sleep(100 * time.Millisecond) // let the async log writers flush
	}
	os.Exit(1)
}

// logFlag is the -log flag. It works bare (-log → the default file) and with
// a value (-log=path, -log path, -log=off). Go's flag package allows bare
// flags only for booleans, so it reports itself as one; a path given after
// a space is picked up from the remaining arguments (see parseFlags).
type logFlag struct {
	path string
	bare bool
}

func (l *logFlag) String() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *logFlag) Set(v string) error {
	switch v {
	case "true":
		l.path, l.bare = defaultLogPath(), true
	case "false":
		l.path, l.bare = "off", false
	default:
		l.path, l.bare = v, false
	}
	return nil
}

func (l *logFlag) IsBoolFlag() bool { return true }

// parseFlags parses the command line, taking the argument after a bare
// -log as its path: "-log C:\x\log.txt -v" works like "-log=C:\x\log.txt -v".
func parseFlags(lf *logFlag) {
	flag.Parse()
	for lf.bare && flag.NArg() > 0 {
		lf.path, lf.bare = flag.Arg(0), false
		flag.CommandLine.Parse(flag.Args()[1:])
	}
	if flag.NArg() > 0 {
		fatalf("unexpected arguments: %q (see -h)", flag.Args())
	}
}

// errLog receives errors: they go to the log file even without -log.
var errLog = log.New(os.Stderr, "ERROR ", log.Ltime)

func errorf(format string, args ...any) { errLog.Printf(format, args...) }
