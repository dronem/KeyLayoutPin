//go:build windows

package main

import (
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// The Settings window, opened from the tray menu: plain Win32 controls,
// changes apply and are saved at once. Only one is open at a time.

var (
	procSendMessageW                 = user32.NewProc("SendMessageW")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procSetFocus                     = user32.NewProc("SetFocus")
	procGetSysColor                  = user32.NewProc("GetSysColor")
	procGetSysColorBrush             = user32.NewProc("GetSysColorBrush")
	procLoadCursorW                  = user32.NewProc("LoadCursorW")
	procGetDpiForWindow              = user32.NewProc("GetDpiForWindow")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procAdjustWindowRectExForDpi     = user32.NewProc("AdjustWindowRectExForDpi")
	procCreateActCtxW                = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx               = kernel32.NewProc("ActivateActCtx")
	procDeactivateActCtx             = kernel32.NewProc("DeactivateActCtx")
)

// Win32 constants. The standard library doesn't define them, so they are
// copied from the Windows SDK headers (WinUser.h, WinGDI.h) under Go names:
// the comment on each gives the SDK name to look up on learn.microsoft.com.
const (
	// Window styles (CreateWindowEx): what the window or control is like.
	wsChild           = 0x40000000 // WS_CHILD: a control inside another window
	wsVisible         = 0x10000000 // WS_VISIBLE: shown right after creation
	wsCaption         = 0x00C00000 // WS_CAPTION: a title bar (and a border)
	wsSysMenu         = 0x00080000 // WS_SYSMENU: the close button (X) in the title bar
	wsTabStop         = 0x00010000 // WS_TABSTOP: Tab stops at this control
	wsExControlParent = 0x00010000 // WS_EX_CONTROLPARENT: Tab moves between the controls inside

	// Button styles: the kind of a "BUTTON" control.
	bsDefPushButton = 0x1 // BS_DEFPUSHBUTTON: the default button, pressed by Enter
	bsAutoCheckBox  = 0x3 // BS_AUTOCHECKBOX: a checkbox that toggles itself on click

	// Messages to our window.
	wmDestroy        = 0x0002 // WM_DESTROY: the window is being closed
	wmSetFont        = 0x0030 // WM_SETFONT: sets a control's font (sent by us)
	wmSetIcon        = 0x0080 // WM_SETICON: sets the title bar / taskbar icon (sent by us)
	wmCommand        = 0x0111 // WM_COMMAND: a control was used (click, Enter, Esc)
	wmCtlColorStatic = 0x0138 // WM_CTLCOLORSTATIC: asks which colors to draw a label or checkbox with
	wmDpiChanged     = 0x02E0 // WM_DPICHANGED: moved to a monitor with another scale

	// The checkbox: messages to it and the values in its notifications.
	bmGetCheck = 0x00F0 // BM_GETCHECK: is it checked?
	bmSetCheck = 0x00F1 // BM_SETCHECK: check or uncheck it
	bstChecked = 1      // BST_CHECKED: the "checked" state
	bnClicked  = 0      // BN_CLICKED: the notification code for a click

	// Control IDs: they tell WM_COMMAND which control it is about.
	idOK       = 1   // IDOK: standard; Enter sends it — our Close button
	idCancel   = 2   // IDCANCEL: standard; Esc sends it
	idFieldDot = 100 // ours: the "Show a dot" checkbox

	// System colors (GetSysColor, GetSysColorBrush): follow the Windows theme.
	colorWindow     = 5  // COLOR_WINDOW: the window background
	colorWindowText = 8  // COLOR_WINDOWTEXT: normal text
	colorGrayText   = 17 // COLOR_GRAYTEXT: the grey explanation text

	// Arguments of other functions.
	swShow            = 5      // SW_SHOW: ShowWindow — show the window
	swpNoZOrder       = 0x0004 // SWP_NOZORDER: SetWindowPos — keep the order of windows
	swpNoActivate     = 0x0010 // SWP_NOACTIVATE: SetWindowPos — don't activate the window
	spiGetWorkArea    = 0x0030 // SPI_GETWORKAREA: SystemParametersInfo — the screen without the taskbar, to center the window
	idcArrow          = 32512  // IDC_ARROW: LoadCursor — the normal arrow cursor
	transparentBkMode = 1      // TRANSPARENT: SetBkMode — draw text without filling its background

	// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (a special handle, -4):
	// the window scales itself to the monitor, so its text is sharp. The rest
	// of the app is not DPI aware and is scaled by Windows.
	dpiPerMonitorV2 = ^uintptr(3)

	// Ours: the Settings window's style, used when creating it and when
	// computing its size.
	settingsStyle   = wsCaption | wsSysMenu
	settingsExStyle = wsExControlParent
)

type settingsWindow struct {
	app      *msgWindow // the app window: its loop handles our Tab, Esc, Enter
	hwnd     uintptr
	fieldDot uintptr // the checkbox
	note     uintptr // grey text under it
	closeBtn uintptr
	font     uintptr
	s        *settings
	changed  func()
	icon     uintptr
}

// openSettingsWindow is the open window, if any. Like all UI here it is
// only touched on the main thread.
var openSettingsWindow *settingsWindow

var (
	settingsClassOnce sync.Once
	settingsClass     *uint16
	settingsClassErr  error
)

// showSettings opens the Settings window, or brings it to front if open.
// changed is called after a setting was changed and saved.
func showSettings(app *msgWindow, s *settings, icon uintptr, changed func()) {
	if w := openSettingsWindow; w != nil {
		procShowWindow.Call(w.hwnd, swShow)
		procSetForegroundWindow.Call(w.hwnd)
		return
	}
	w := &settingsWindow{app: app, s: s, changed: changed, icon: icon}
	if err := w.create(); err != nil {
		messageBox("Cannot open Settings: " + err.Error())
	}
}

func registerSettingsClass() (*uint16, error) {
	settingsClassOnce.Do(func() {
		module, _, _ := procGetModuleHandleW.Call(0)
		cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
		settingsClass, _ = syscall.UTF16PtrFromString("KeyLayoutPin_Settings")
		wc := wndClassEx{
			CbSize:     uint32(unsafe.Sizeof(wndClassEx{})),
			WndProc:    syscall.NewCallback(settingsWndProc),
			Instance:   module,
			Cursor:     cursor,
			Background: colorWindow + 1,
			ClassName:  settingsClass,
		}
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			settingsClassErr = err
		}
	})
	return settingsClass, settingsClassErr
}

func (w *settingsWindow) create() error {
	class, err := registerSettingsClass()
	if err != nil {
		return err
	}
	// Windows created now are per-monitor DPI aware; the rest of the app is not.
	if procSetThreadDpiAwarenessContext.Find() == nil {
		old, _, _ := procSetThreadDpiAwarenessContext.Call(dpiPerMonitorV2)
		defer procSetThreadDpiAwarenessContext.Call(old)
	}
	// Modern-looking controls (common controls 6) without a manifest in the exe.
	if cookie, ok := activateVisualStyles(); ok {
		defer procDeactivateActCtx.Call(0, cookie)
	}

	module, _, _ := procGetModuleHandleW.Call(0)
	title, _ := syscall.UTF16PtrFromString("KeyLayoutPin Settings")
	hwnd, _, err := procCreateWindowExW.Call(settingsExStyle, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		settingsStyle, 0, 0, 0, 0, 0, 0, module, 0)
	if hwnd == 0 {
		return err
	}
	w.hwnd = hwnd
	openSettingsWindow = w
	w.app.setDialog(hwnd)

	w.fieldDot = w.child("BUTTON", "Show a dot on the tray icon when you enter a field",
		wsTabStop|bsAutoCheckBox, idFieldDot)
	w.note = w.child("STATIC", "Green: the field has a remembered layout.\n"+
		"Yellow: it gets the default layout.\n"+
		"The dot disappears after a second.", 0, 0)
	w.closeBtn = w.child("BUTTON", "Close", wsTabStop|bsDefPushButton, idOK)
	if w.s.FieldDot {
		procSendMessageW.Call(w.fieldDot, bmSetCheck, bstChecked, 0)
	}
	if w.icon != 0 {
		procSendMessageW.Call(hwnd, wmSetIcon, 0, w.icon) // ICON_SMALL
		procSendMessageW.Call(hwnd, wmSetIcon, 1, w.icon) // ICON_BIG
	}

	w.layout(windowDPI(hwnd), nil)
	procShowWindow.Call(hwnd, swShow)
	procSetForegroundWindow.Call(hwnd)
	procSetFocus.Call(w.fieldDot)
	return nil
}

func (w *settingsWindow) child(class, text string, style, id uintptr) uintptr {
	c, _ := syscall.UTF16PtrFromString(class)
	t, _ := syscall.UTF16PtrFromString(text)
	module, _, _ := procGetModuleHandleW.Call(0)
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)),
		wsChild|wsVisible|style, 0, 0, 0, 0, w.hwnd, id, module, 0)
	return h
}

// Layout in 96-DPI pixels.
const (
	settingsWidth   = 400
	settingsMargin  = 16
	settingsButtonW = 88
	settingsButtonH = 26
)

// layout sizes the font, the controls and the window for dpi. at is the
// window rect Windows suggests on a DPI change; nil centers the window on
// the primary monitor's work area.
func (w *settingsWindow) layout(dpi int, at *rect) {
	px := func(v int) int32 { return int32(v * dpi / 96) }

	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	height := -(9*dpi + 36) / 72 // 9 pt
	// CreateFont: weight 400 (FW_NORMAL), charset 1 (DEFAULT_CHARSET),
	// quality 5 (CLEARTYPE_QUALITY).
	font, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	for _, c := range []uintptr{w.fieldDot, w.note, w.closeBtn} {
		procSendMessageW.Call(c, wmSetFont, font, 1)
	}
	if w.font != 0 {
		procDeleteObject.Call(w.font)
	}
	w.font = font

	const m, inner = settingsMargin, settingsWidth - 2*settingsMargin
	move := func(h uintptr, x, y, cx, cy int) {
		procSetWindowPos.Call(h, 0, uintptr(px(x)), uintptr(px(y)), uintptr(px(cx)), uintptr(px(cy)), swpNoZOrder|swpNoActivate)
	}
	move(w.fieldDot, m, m, inner, 22)
	move(w.note, m+20, m+28, inner-20, 52)
	btnY := m + 28 + 52 + 16
	move(w.closeBtn, settingsWidth-m-settingsButtonW, btnY, settingsButtonW, settingsButtonH)

	r := rect{0, 0, px(settingsWidth), px(btnY + settingsButtonH + m)}
	if procAdjustWindowRectExForDpi.Find() == nil {
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&r)), settingsStyle, 0, settingsExStyle, uintptr(dpi))
	}
	cx, cy := r.Right-r.Left, r.Bottom-r.Top
	var x, y int32
	if at != nil {
		x, y = at.Left, at.Top
	} else {
		var wa rect
		procSystemParametersInfoW.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
		x, y = wa.Left+(wa.Right-wa.Left-cx)/2, wa.Top+(wa.Bottom-wa.Top-cy)/2
	}
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y), uintptr(cx), uintptr(cy), swpNoZOrder|swpNoActivate)
}

func windowDPI(hwnd uintptr) int {
	if procGetDpiForWindow.Find() == nil {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	return 96
}

func settingsWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := openSettingsWindow
	if w == nil || w.hwnd != hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	switch msg {
	case wmCommand:
		switch id, code := wParam&0xFFFF, wParam>>16&0xFFFF; {
		case id == idFieldDot && code == bnClicked:
			on, _, _ := procSendMessageW.Call(w.fieldDot, bmGetCheck, 0, 0)
			w.s.FieldDot = on == bstChecked
			if err := w.s.save(); err != nil {
				messageBox("Cannot save the settings: " + err.Error())
			}
			if w.changed != nil {
				w.changed()
			}
		case id == idOK || id == idCancel: // Close, Enter, Esc
			procDestroyWindow.Call(hwnd)
		}
		return 0
	case wmCtlColorStatic: // the checkbox and the note: on the window background
		procSetBkMode.Call(wParam, transparentBkMode)
		color := uintptr(colorWindowText)
		if lParam == w.note {
			color = colorGrayText
		}
		c, _, _ := procGetSysColor.Call(color)
		procSetTextColor.Call(wParam, c)
		b, _, _ := procGetSysColorBrush.Call(colorWindow)
		return b
	case wmDpiChanged:
		w.layout(int(wParam&0xFFFF), (*rect)(unsafe.Pointer(lParam)))
		return 0
	case wmDestroy:
		if w.font != 0 {
			procDeleteObject.Call(w.font)
		}
		openSettingsWindow = nil
		w.app.setDialog(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// visualStylesManifest asks for common controls 6: themed buttons and
// checkboxes. The exe has no manifest of its own (no resource files).
const visualStylesManifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0"
        processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
</assembly>`

type actCtx struct {
	CbSize                uint32
	Flags                 uint32
	Source                *uint16
	ProcessorArchitecture uint16
	LangID                uint16
	AssemblyDirectory     *uint16
	ResourceName          uintptr
	ApplicationName       *uint16
	Module                uintptr
}

var (
	visualStylesOnce sync.Once
	visualStylesCtx  uintptr // 0: not available, plain controls
)

// activateVisualStyles activates an activation context with common controls
// 6 for the windows created next. The context is read from a temporary
// manifest file once and kept for the life of the process.
func activateVisualStyles() (cookie uintptr, ok bool) {
	visualStylesOnce.Do(func() {
		f, err := os.CreateTemp("", "keylayoutpin-*.manifest")
		if err != nil {
			return
		}
		name := f.Name()
		defer os.Remove(name)
		_, err = f.WriteString(visualStylesManifest)
		if f.Close() != nil || err != nil {
			return
		}
		src, _ := syscall.UTF16PtrFromString(name)
		ac := actCtx{CbSize: uint32(unsafe.Sizeof(actCtx{})), Source: src}
		if h, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ac))); h != ^uintptr(0) {
			visualStylesCtx = h
		}
	})
	if visualStylesCtx == 0 {
		return 0, false
	}
	r, _, _ := procActivateActCtx.Call(visualStylesCtx, uintptr(unsafe.Pointer(&cookie)))
	return cookie, r != 0
}
