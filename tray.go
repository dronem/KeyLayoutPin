//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// tray is the notification area icon with its menu, on top of the
// app's message window.

var (
	gdi32 = syscall.NewLazyDLL("gdi32.dll")
	shell = syscall.NewLazyDLL("shell32.dll")

	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procShellNotifyIconW    = shell.NewProc("Shell_NotifyIconW")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap        = gdi32.NewProc("CreateBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procCreateFontW         = gdi32.NewProc("CreateFontW")
	procSetBkMode           = gdi32.NewProc("SetBkMode")
	procSetTextColor        = gdi32.NewProc("SetTextColor")
	procGdiFlush            = gdi32.NewProc("GdiFlush")
)

const (
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmTrayCallback = wmApp + 3

	nimAdd     = 0
	nimDelete  = 2
	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmNoNotify    = 0x0080
	tpmReturnCmd   = 0x0100

	menuAutostart = 1
	menuExit      = 2
)

type notifyIconData struct {
	CbSize          uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUIDItem        [16]byte
	BalloonIcon     uintptr
}

type tray struct {
	win  *msgWindow
	icon uintptr
}

func newTray(win *msgWindow) *tray {
	t := &tray{win: win, icon: makeIcon()}
	win.on(wmTrayCallback, func(_, lParam uintptr) {
		if lParam == wmRButtonUp || lParam == wmLButtonUp {
			t.showMenu()
		}
	})
	// Explorer restarted — the icon must be added again.
	win.on(registerWindowMessage("TaskbarCreated"), func(_, _ uintptr) { t.add() })
	t.add()
	return t
}

func (t *tray) data() notifyIconData {
	d := notifyIconData{
		CbSize:          uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             t.win.hwnd,
		ID:              1,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmTrayCallback,
		Icon:            t.icon,
	}
	copy(d.Tip[:len(d.Tip)-1], syscall.StringToUTF16("KeyLayoutPin — keyboard layout pinned to each field"))
	return d
}

func (t *tray) add() {
	d := t.data()
	if r, _, err := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d))); r == 0 {
		errorf("tray icon: %v", err)
	}
}

func (t *tray) close() {
	d := t.data()
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
	if t.icon != 0 {
		procDestroyIcon.Call(t.icon)
	}
}

func appendMenu(menu uintptr, flags, id uintptr, text string) {
	var p uintptr
	if text != "" {
		s, _ := syscall.UTF16PtrFromString(text)
		p = uintptr(unsafe.Pointer(s))
	}
	procAppendMenuW.Call(menu, flags, id, p)
}

func (t *tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, mfString|mfGrayed, 0, "KeyLayoutPin — keyboard layout pinned to each field")
	appendMenu(menu, mfSeparator, 0, "")
	auto := uintptr(mfString)
	if autostartEnabled() {
		auto |= mfChecked
	}
	appendMenu(menu, auto, menuAutostart, "Start with Windows")
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuExit, "Exit")

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu doesn't close when clicking elsewhere.
	procSetForegroundWindow.Call(t.win.hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(menu, tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(pt.X), uintptr(pt.Y), 0, t.win.hwnd, 0)
	procPostMessageW.Call(t.win.hwnd, wmNull, 0, 0)

	switch cmd {
	case menuAutostart:
		if err := setAutostart(!autostartEnabled()); err != nil {
			messageBox("Cannot change autostart: " + err.Error())
		}
	case menuExit:
		procPostQuitMessage.Call(0)
	}
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// makeIcon draws the tray icon: white "Aa" on a blue rounded square.
// Drawn at runtime so the app needs no resource files.
func makeIcon() uintptr {
	const size, radius = 32, 7
	const fill = 0xFF2563EB // ARGB

	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: size, Height: -size, Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dc, _, _ := procCreateCompatibleDC.Call(0)
	defer procDeleteDC.Call(dc)
	color, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 {
		return 0
	}
	defer procDeleteObject.Call(color)
	px := unsafe.Slice((*uint32)(bits), size*size)

	inside := func(x, y int) bool {
		cx, cy := x, y
		if cx < radius {
			cx = radius
		} else if cx > size-1-radius {
			cx = size - 1 - radius
		}
		if cy < radius {
			cy = radius
		} else if cy > size-1-radius {
			cy = size - 1 - radius
		}
		dx, dy := x-cx, y-cy
		return dx*dx+dy*dy <= radius*radius
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inside(x, y) {
				px[y*size+x] = fill
			}
		}
	}

	old, _, _ := procSelectObject.Call(dc, color)
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	font, _, _ := procCreateFontW.Call(uintptr(^uintptr(19)), 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face))) // height -20, bold, antialiased
	oldFont, _, _ := procSelectObject.Call(dc, font)
	procSetBkMode.Call(dc, 1)             // TRANSPARENT
	procSetTextColor.Call(dc, 0x00FFFFFF) // white
	text, _ := syscall.UTF16PtrFromString("Aa")
	r := rect{0, 0, size, size}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x1|0x4|0x20) // center, vcenter, single line
	procGdiFlush.Call()
	procSelectObject.Call(dc, oldFont)
	procDeleteObject.Call(font)
	procSelectObject.Call(dc, old)

	// GDI text clears the alpha byte; make the whole shape opaque again.
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inside(x, y) {
				px[y*size+x] |= 0xFF000000
			}
		}
	}

	mask, _, _ := procCreateBitmap.Call(size, size, 1, 1, 0)
	defer procDeleteObject.Call(mask)
	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: color}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}
