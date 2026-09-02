//go:build windows

package main

import (
	"errors"
	"flag"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Autostart via HKCU\...\Run. It is turned on automatically on the very
// first run; after that only the tray menu changes it, so turning it off
// sticks. The "first run already happened" flag lives in HKCU\Software\change_lang.

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegDeleteKeyW    = advapi32.NewProc("RegDeleteKeyW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

// HKEY_CURRENT_USER is (HKEY)(LONG)0x80000001, i.e. sign-extended on 64-bit.
var hkeyCurrentUser = signExtend(0x80000001)

func signExtend(v uint32) uintptr { return uintptr(int(int32(v))) }

const (
	keyRead  = 0x20019
	keyWrite = 0x20006
	regSz    = 1

	runKey      = `Software\Microsoft\Windows\CurrentVersion\Run`
	settingsKey = `Software\change_lang`
	runValue    = "change_lang"
	initValue   = "AutostartInitialized"
)

var errNotFound = errors.New("not found")

func regOpen(path string, create bool) (uintptr, error) {
	p, _ := syscall.UTF16PtrFromString(path)
	var h uintptr
	var r uintptr
	if create {
		r, _, _ = procRegCreateKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)), 0, 0, 0,
			keyRead|keyWrite, 0, uintptr(unsafe.Pointer(&h)), 0)
	} else {
		r, _, _ = procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(p)), 0,
			keyRead|keyWrite, uintptr(unsafe.Pointer(&h)))
	}
	if r != 0 {
		return 0, syscall.Errno(r)
	}
	return h, nil
}

func regGetString(path, name string) (string, error) {
	h, err := regOpen(path, false)
	if err != nil {
		return "", errNotFound
	}
	defer procRegCloseKey.Call(h)
	n, _ := syscall.UTF16PtrFromString(name)
	buf := make([]uint16, 1024)
	size := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ := procRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(n)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", errNotFound
	}
	return syscall.UTF16ToString(buf[:size/2]), nil
}

func regSetString(path, name, value string) error {
	h, err := regOpen(path, true)
	if err != nil {
		return err
	}
	defer procRegCloseKey.Call(h)
	n, _ := syscall.UTF16PtrFromString(name)
	v := syscall.StringToUTF16(value)
	r, _, _ := procRegSetValueExW.Call(h, uintptr(unsafe.Pointer(n)), 0, regSz,
		uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2))
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

func regDeleteValue(path, name string) error {
	h, err := regOpen(path, false)
	if err != nil {
		return nil // no key — nothing to delete
	}
	defer procRegCloseKey.Call(h)
	n, _ := syscall.UTF16PtrFromString(name)
	r, _, _ := procRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(n)))
	if r != 0 && syscall.Errno(r) != syscall.ERROR_FILE_NOT_FOUND {
		return syscall.Errno(r)
	}
	return nil
}

// autostartCommand is the command line written to the Run key: this exe
// with the flags it was started with. Path flags are already resolved to
// absolute paths (see resolvePathFlag), because autostart runs with
// a different working directory.
func autostartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	parts := []string{syscall.EscapeArg(exe)}
	flag.Visit(func(f *flag.Flag) {
		parts = append(parts, syscall.EscapeArg("-"+f.Name+"="+f.Value.String()))
	})
	return strings.Join(parts, " "), nil
}

// commandExe extracts the executable path from a Run command line.
func commandExe(cmd string) string {
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1]
		}
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

func autostartEnabled() bool {
	_, err := regGetString(runKey, runValue)
	return err == nil
}

func setAutostart(on bool) error {
	if !on {
		return regDeleteValue(runKey, runValue)
	}
	cmd, err := autostartCommand()
	if err != nil {
		return err
	}
	return regSetString(runKey, runValue, cmd)
}

// initAutostart turns autostart on the first time the app runs, and fixes
// the entry if the exe it points to no longer exists (the app was moved).
func initAutostart() error {
	if _, err := regGetString(settingsKey, initValue); err != nil {
		if err := setAutostart(true); err != nil {
			return err
		}
		return regSetString(settingsKey, initValue, "1")
	}
	if cmd, err := regGetString(runKey, runValue); err == nil {
		if _, err := os.Stat(commandExe(cmd)); err != nil {
			return setAutostart(true)
		}
	}
	return nil
}
