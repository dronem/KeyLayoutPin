//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/dronem/keylayoutpin/internal/kbd"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
	oleaut32 = syscall.NewLazyDLL("oleaut32.dll")
	oleacc   = syscall.NewLazyDLL("oleacc.dll")

	procSetWinEventHook        = user32.NewProc("SetWinEventHook")
	procUnhookWinEvent         = user32.NewProc("UnhookWinEvent")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW     = user32.NewProc("PostThreadMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procSetTimer               = user32.NewProc("SetTimer")
	procKillTimer              = user32.NewProc("KillTimer")
	procGetForegroundWindow    = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout      = user32.NewProc("GetKeyboardLayout")
	procGetKeyboardLayoutList  = user32.NewProc("GetKeyboardLayoutList")
	procGetGUIThreadInfo       = user32.NewProc("GetGUIThreadInfo")
	procGetAncestor            = user32.NewProc("GetAncestor")
	procGetClassNameW          = user32.NewProc("GetClassNameW")
	procGetDlgCtrlID           = user32.NewProc("GetDlgCtrlID")
	procSystemParametersInfoW  = user32.NewProc("SystemParametersInfoW")
	procSetWindowsHookExW      = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx    = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx         = user32.NewProc("CallNextHookEx")
	procGetAsyncKeyState       = user32.NewProc("GetAsyncKeyState")
	procEnumChildWindows       = user32.NewProc("EnumChildWindows")
	procSendMessageTimeoutW    = user32.NewProc("SendMessageTimeoutW")
	procIsWindow               = user32.NewProc("IsWindow")

	procGetCurrentThreadId         = kernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW           = kernel32.NewProc("GetModuleHandleW")
	procOpenProcess                = kernel32.NewProc("OpenProcess")
	procCloseHandle                = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageNameW = kernel32.NewProc("QueryFullProcessImageNameW")
	procLCIDToLocaleName           = kernel32.NewProc("LCIDToLocaleName")

	procCoInitializeEx = ole32.NewProc("CoInitializeEx")
	procCoUninitialize = ole32.NewProc("CoUninitialize")
	procSysFreeString  = oleaut32.NewProc("SysFreeString")
	procVariantClear   = oleaut32.NewProc("VariantClear")

	procAccessibleObjectFromEvent = oleacc.NewProc("AccessibleObjectFromEvent")
)

// Win32 constants, copied from the Windows SDK headers (WinUser.h,
// WinNT.h, objbase.h, WinNls.h) under Go names; the comment gives the SDK name.
const (
	eventObjectFocus = 0x8005 // EVENT_OBJECT_FOCUS: something got keyboard focus

	// SetWinEventHook: how the hook is called.
	wineventOutOfContext   = 0x0000 // WINEVENT_OUTOFCONTEXT: in our process, no DLL injected into others
	wineventSkipOwnProcess = 0x0002 // WINEVENT_SKIPOWNPROCESS: not for our own windows

	wmQuit                   = 0x0012 // WM_QUIT: ends the message loop
	wmTimer                  = 0x0113 // WM_TIMER: a SetTimer timer fired
	wmInputLangChangeRequest = 0x0050 // WM_INPUTLANGCHANGEREQUEST: asks a window to switch the layout
	wmApp                    = 0x8000 // WM_APP: the app's own messages start above it (msgWindow.handle)

	gaRoot                  = 2          // GA_ROOT: GetAncestor — the top-level window
	spiGetDefaultInputLang  = 0x0059     // SPI_GETDEFAULTINPUTLANG: SystemParametersInfo — the default layout
	processQueryLimitedInfo = 0x1000     // PROCESS_QUERY_LIMITED_INFORMATION: OpenProcess — enough to read the exe name
	coinitMultithreaded     = 0x0        // COINIT_MULTITHREADED: CoInitializeEx — COM in MTA mode
	localeNameMaxLength     = 85         // LOCALE_NAME_MAX_LENGTH: buffer size for "en-US" etc.
	localeAllowNeutralNames = 0x08000000 // LOCALE_ALLOW_NEUTRAL_NAMES: LCIDToLocaleName — "ja" is fine too
)

type point struct{ X, Y int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type rect struct{ Left, Top, Right, Bottom int32 }

type guiThreadInfo struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       rect
}

// HKL is the platform-neutral layout handle; this file adds the parts
// that need Windows (names).
type HKL = kbd.HKL

// layoutName returns the language plus the keyboard variant if different: "en-US (0415)".
func layoutName(h HKL) string {
	name := localeName(h)
	if h.Variant() != h.LangID() {
		name += fmt.Sprintf(" (%04X)", h.Variant())
	}
	return name
}

// localeName returns the layout's language name, e.g. "en-US" or "ja-JP".
func localeName(h HKL) string {
	buf := make([]uint16, localeNameMaxLength)
	n, _, _ := procLCIDToLocaleName.Call(uintptr(h.LangID()), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), localeAllowNeutralNames)
	if n == 0 {
		return fmt.Sprintf("lang-%04X", h.LangID())
	}
	return syscall.UTF16ToString(buf)
}

func getCurrentThreadID() uint32 {
	r, _, _ := procGetCurrentThreadId.Call()
	return uint32(r)
}

func getForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

func windowThreadProcess(hwnd uintptr) (tid, pid uint32) {
	r, _, _ := procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return uint32(r), pid
}

func keyboardLayout(tid uint32) HKL {
	r, _, _ := procGetKeyboardLayout.Call(uintptr(tid))
	return HKL(r)
}

func keyboardLayoutList() []HKL {
	n, _, _ := procGetKeyboardLayoutList.Call(0, 0)
	if n == 0 {
		return nil
	}
	list := make([]HKL, n)
	n, _, _ = procGetKeyboardLayoutList.Call(n, uintptr(unsafe.Pointer(&list[0])))
	return list[:n]
}

// defaultInputLayout returns the default layout from Windows settings.
func defaultInputLayout() HKL {
	var h HKL
	r, _, _ := procSystemParametersInfoW.Call(spiGetDefaultInputLang, 0, uintptr(unsafe.Pointer(&h)), 0)
	if r == 0 {
		return 0
	}
	return h
}

func focusWindow(tid uint32) uintptr {
	gti := guiThreadInfo{CbSize: uint32(unsafe.Sizeof(guiThreadInfo{}))}
	r, _, _ := procGetGUIThreadInfo.Call(uintptr(tid), uintptr(unsafe.Pointer(&gti)))
	if r == 0 {
		return 0
	}
	return gti.HwndFocus
}

func rootWindow(hwnd uintptr) uintptr {
	r, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	if r == 0 {
		return hwnd
	}
	return r
}

func className(hwnd uintptr) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

func dlgCtrlID(hwnd uintptr) int32 {
	r, _, _ := procGetDlgCtrlID.Call(hwnd)
	return int32(r)
}

func processName(pid uint32) string {
	h, _, _ := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(pid))
	if h == 0 {
		return fmt.Sprintf("pid-%d", pid)
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageNameW.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return fmt.Sprintf("pid-%d", pid)
	}
	return filepath.Base(syscall.UTF16ToString(buf[:size]))
}

// requestLayout asks the focused window to switch the layout — the same
// way Windows itself does on Ctrl+Shift.
func requestLayout(hwnd uintptr, h HKL) bool {
	r, _, _ := procPostMessageW.Call(hwnd, wmInputLangChangeRequest, 0, uintptr(h))
	return r != 0
}

const (
	wmGetObject     = 0x003D // WM_GETOBJECT: asks a window for its accessibility object
	smtoAbortIfHung = 0x0002 // SMTO_ABORTIFHUNG: SendMessageTimeout — don't wait for a hung window
	// The window class of Chromium's page area, which answers WM_GETOBJECT.
	chromiumRenderer = "Chrome_RenderWidgetHostHWND"
)

var wakeChildProc = syscall.NewCallback(func(h, _ uintptr) uintptr {
	if className(h) == chromiumRenderer {
		var res uintptr
		procSendMessageTimeoutW.Call(h, wmGetObject, 0, 1, smtoAbortIfHung, 200, uintptr(unsafe.Pointer(&res)))
	}
	return 1
})

// wakeChromium enables accessibility in a Chromium/Electron window:
// after WM_GETOBJECT to the render widget, Chromium builds the page's
// accessibility tree and starts reporting focus in input fields.
func wakeChromium(root uintptr) {
	procEnumChildWindows.Call(root, wakeChildProc, 0)
}

func isWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}
