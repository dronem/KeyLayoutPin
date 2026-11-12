//go:build windows

package main

import (
	"flag"
	"math"
	"syscall"
	"time"
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
	procSetMenuDefaultItem  = user32.NewProc("SetMenuDefaultItem")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procCreateIconIndirect  = user32.NewProc("CreateIconIndirect")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procDrawTextW           = user32.NewProc("DrawTextW")
	procShellNotifyIconW    = shell.NewProc("Shell_NotifyIconW")
	procShellExecuteW       = shell.NewProc("ShellExecuteW")
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

// Win32 constants, copied from the Windows SDK headers (WinUser.h,
// shellapi.h) under Go names; the comment gives the SDK name to look up.
const (
	// Mouse messages: the tray icon reports clicks with these.
	wmLButtonUp = 0x0202 // WM_LBUTTONUP: left button released
	wmRButtonUp = 0x0205 // WM_RBUTTONUP: right button released

	// Shell_NotifyIcon: what to do with the tray icon.
	nimAdd    = 0 // NIM_ADD: add it
	nimModify = 1 // NIM_MODIFY: change it (the dot)
	nimDelete = 2 // NIM_DELETE: remove it
	// NOTIFYICONDATA.uFlags: which of its fields are filled in.
	nifMessage = 0x1 // NIF_MESSAGE: CallbackMessage
	nifIcon    = 0x2 // NIF_ICON: Icon
	nifTip     = 0x4 // NIF_TIP: Tip, the hover text

	// AppendMenu: the kind of a menu item.
	mfString    = 0x0000 // MF_STRING: a normal text item
	mfGrayed    = 0x0001 // MF_GRAYED: disabled
	mfChecked   = 0x0008 // MF_CHECKED: with a check mark
	mfSeparator = 0x0800 // MF_SEPARATOR: a divider line

	// TrackPopupMenu: how the menu behaves.
	tpmRightButton = 0x0002 // TPM_RIGHTBUTTON: items react to the right button too
	tpmNoNotify    = 0x0080 // TPM_NONOTIFY: don't send WM_COMMAND for the choice...
	tpmReturnCmd   = 0x0100 // TPM_RETURNCMD: ...return it from the call instead

	swShowNormal = 1 // SW_SHOWNORMAL: ShellExecute — open the browser normally

	// Ours: menu item IDs, returned by TrackPopupMenu.
	menuAutostart = 1
	menuExit      = 2
	menuTitle     = 3
	menuAbout     = 4
	menuHelp      = 5
	menuSettings  = 6

	// dotTime: how long the field dot stays on the tray icon.
	dotTime = time.Second
)

// Tray icon dots, as ARGB (alpha, red, green, blue):
// the field has a remembered layout / gets the default.
const (
	dotGreen  = 0xFF22C55E
	dotYellow = 0xFFFACC15
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
	win    *msgWindow
	icon   uintptr // the plain icon
	green  uintptr // with a green dot: the field has a remembered layout
	yellow uintptr // with a yellow dot: the field gets the default
	shown  uintptr // the icon in the notification area now

	callback uint32  // the message the icon sends to win on mouse events
	dotTimer timerID // hides the field dot

	openSettings func() // the Settings menu item
}

func newTray(win *msgWindow) *tray {
	t := &tray{win: win, icon: makeIcon(0), green: makeIcon(dotGreen), yellow: makeIcon(dotYellow)}
	t.shown = t.icon
	t.dotTimer = win.newTimer()
	t.callback = win.handle(func(_, lParam uintptr) {
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
		CallbackMessage: t.callback,
		Icon:            t.shown,
	}
	copy(d.Tip[:len(d.Tip)-1], syscall.StringToUTF16("KeyLayoutPin "+versionString()+" — keyboard layout pinned to each field"))
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
	for _, i := range []uintptr{t.icon, t.green, t.yellow} {
		if i != 0 {
			procDestroyIcon.Call(i)
		}
	}
	t.icon, t.green, t.yellow = 0, 0, 0
}

// flashDot shows the field dot for dotTime. A newer field replaces the dot
// at once and restarts the countdown — no queue of dots.
func (t *tray) flashDot(remembered bool) {
	i := t.yellow
	if remembered {
		i = t.green
	}
	t.setIcon(i)
	t.win.after(t.dotTimer, dotTime, func() { t.setIcon(t.icon) })
}

// hideDot removes the field dot right away.
func (t *tray) hideDot() {
	t.win.cancel(t.dotTimer)
	t.setIcon(t.icon)
}

func (t *tray) setIcon(i uintptr) {
	if i == 0 || i == t.shown {
		return
	}
	t.shown = i
	d := t.data()
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&d)))
}

func appendMenu(menu uintptr, flags, id uintptr, text string) {
	if text == "" {
		procAppendMenuW.Call(menu, flags, id, 0)
		return
	}
	s, _ := syscall.UTF16PtrFromString(text)
	// The conversion must stay inside the call, see comCall.
	procAppendMenuW.Call(menu, flags, id, uintptr(unsafe.Pointer(s)))
}

func (t *tray) showMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)
	appendMenu(menu, mfString, menuTitle, "KeyLayoutPin")
	procSetMenuDefaultItem.Call(menu, menuTitle, 0) // the default item is drawn bold
	appendMenu(menu, mfSeparator, 0, "")
	auto := uintptr(mfString)
	if autostartEnabled() {
		auto |= mfChecked
	}
	appendMenu(menu, auto, menuAutostart, "Start with Windows")
	if t.openSettings != nil {
		appendMenu(menu, mfString, menuSettings, "Settings…")
	}
	appendMenu(menu, mfSeparator, 0, "")
	appendMenu(menu, mfString, menuAbout, "About")
	appendMenu(menu, mfString, menuHelp, "Help")
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
	case menuSettings:
		t.openSettings()
	case menuTitle, menuAbout:
		if askBox(aboutText() + "\n\nOpen the project page?") {
			openURL(projectURL)
		}
	case menuHelp:
		if askBox(helpText(flag.CommandLine) + "\n\nOpen the full documentation?") {
			openURL(docsURL)
		}
	case menuExit:
		procPostQuitMessage.Call(0)
	}
}

// openURL opens a link in the default browser.
func openURL(url string) {
	verb, _ := syscall.UTF16PtrFromString("open")
	u, _ := syscall.UTF16PtrFromString(url)
	procShellExecuteW.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(u)), 0, 0, swShowNormal)
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

// makeIcon draws the tray icon: white "Aa" on a blue rounded square, plus a
// dot of the given ARGB color in the corner (0: no dot).
// Drawn at runtime so the app needs no resource files.
func makeIcon(dot uint32) uintptr {
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
	// CreateFont: height -20 px, weight 700 (FW_BOLD), charset 1 (DEFAULT_CHARSET),
	// quality 4 (ANTIALIASED_QUALITY).
	font, _, _ := procCreateFontW.Call(uintptr(^uintptr(19)), 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 4, 0, uintptr(unsafe.Pointer(face)))
	oldFont, _, _ := procSelectObject.Call(dc, font)
	procSetBkMode.Call(dc, 1)             // TRANSPARENT: no background behind the text
	procSetTextColor.Call(dc, 0x00FFFFFF) // white
	text, _ := syscall.UTF16PtrFromString("Aa")
	r := rect{0, 0, size, size}
	procDrawTextW.Call(dc, uintptr(unsafe.Pointer(text)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), 0x1|0x4|0x20) // DT_CENTER | DT_VCENTER | DT_SINGLELINE
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

	if dot != 0 {
		drawDot(px, size, dot)
	}

	mask, _, _ := procCreateBitmap.Call(size, size, 1, 1, 0)
	defer procDeleteObject.Call(mask)
	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: color}
	icon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	return icon
}

// drawDot paints an antialiased dot with a white ring into the bottom right
// corner of a size×size ARGB (straight alpha) image.
func drawDot(px []uint32, size int, color uint32) {
	const r, ring = 6.5, 2.0 // radii in pixels: the dot, and the white ring around it
	c := float64(size) - r - ring
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx, dy := float64(x)+0.5-c, float64(y)+0.5-c
			d := math.Sqrt(dx*dx + dy*dy)
			px[y*size+x] = blend(px[y*size+x], 0xFFFFFFFF, coverage(d, r+ring))
			px[y*size+x] = blend(px[y*size+x], color, coverage(d, r))
		}
	}
}

// coverage: how much of a pixel at distance d from the center a circle of
// radius r covers, 0..1 (a one-pixel soft edge).
func coverage(d, r float64) float64 { return math.Max(0, math.Min(1, r+0.5-d)) }

// blend draws opaque color src over dst (straight alpha) with coverage a.
func blend(dst, src uint32, a float64) uint32 {
	if a <= 0 {
		return dst
	}
	da := float64(dst>>24) / 255
	oa := a + da*(1-a)
	ch := func(shift uint) uint32 {
		s, d := float64(src>>shift&0xFF), float64(dst>>shift&0xFF)
		return uint32((s*a+d*da*(1-a))/oa+0.5) << shift
	}
	return uint32(oa*255+0.5)<<24 | ch(16) | ch(8) | ch(0)
}
