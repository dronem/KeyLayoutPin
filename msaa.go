//go:build windows

package main

import (
	"fmt"
	"strconv"
	"syscall"
	"unsafe"
)

// Minimal MSAA (IAccessible) wrapper. It is needed to tell apart
// fields inside one window: in Chrome, Electron, WPF etc. all inputs
// live in a single HWND, and only the accessibility tree knows which
// one has focus.

// MSAA constants from the Windows SDK headers (oleacc.h, wtypes.h);
// the comment gives the SDK name.
const (
	vtI4 = 3 // VT_I4: a VARIANT holding a 32-bit integer (a child ID)

	// What an element is (ROLE_SYSTEM_*, from get_accRole).
	roleSystemWindow   = 0x09 // ROLE_SYSTEM_WINDOW: a whole window
	roleSystemClient   = 0x0A // ROLE_SYSTEM_CLIENT: a window's client area
	roleSystemDocument = 0x0F // ROLE_SYSTEM_DOCUMENT: a document / web page
	roleSystemText     = 0x2A // ROLE_SYSTEM_TEXT: a text field
	roleSystemComboBox = 0x2E // ROLE_SYSTEM_COMBOBOX: a combo box (often editable)

	stateSystemReadonly = 0x40 // STATE_SYSTEM_READONLY: can't be edited

	// IAccessible methods by their position in the COM vtable (0 is
	// QueryInterface, then AddRef, Release, the IDispatch methods...).
	iaRelease = 2  // IUnknown::Release
	iaName    = 10 // IAccessible::get_accName
	iaRole    = 13 // IAccessible::get_accRole
	iaState   = 14 // IAccessible::get_accState

	maxNameLen = 120 // ours: field names are cut to this many characters
)

// variant matches VARIANT in size: 16 bytes on 386, 24 on amd64/arm64.
type variant struct {
	VT  uint16
	_   [3]uint16
	Val int64
	_   [unsafe.Sizeof(uintptr(0))/4 - 1]uint64
}

type accInfo struct {
	Name     string
	Role     int32
	State    int32
	Editable bool
}

// initCOM joins the calling OS thread to the multithreaded apartment (MTA):
// unlike STA, it doesn't require the thread to keep pumping messages. The
// returned func undoes it; it must run on the same thread. CoUninitialize
// is paired only with a successful CoInitializeEx (S_OK or S_FALSE).
func initCOM() (done func(), err error) {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if int32(hr) < 0 {
		return func() {}, fmt.Errorf("CoInitializeEx: 0x%08X", uint32(hr))
	}
	return func() { procCoUninitialize.Call() }, nil
}

// comCall calls method number `method` of the COM object obj.
//
// Pointers to Go memory may be passed only as uintptr(unsafe.Pointer(p))
// written directly in the argument list of the call: //go:uintptrescapes
// then makes the compiler keep the object on the heap and alive until the
// call returns. Converting earlier (into a variable, a slice, a helper's
// result) leaves a bare number the GC knows nothing about: a stack object
// can move when the goroutine stack grows, a heap object can be freed.
//
//go:uintptrescapes
func comCall(obj uintptr, method int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(method)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

// accGet calls an IAccessible get_accXxx(VARIANT varChild, out) method.
// varChild is passed by value: on x64/arm64 a VARIANT (24 bytes) goes as a
// pointer to a copy, on x86 (16 bytes) as four words on the stack.
func accGet(acc uintptr, method int, child *variant, out unsafe.Pointer) uintptr {
	c := *child // the callee may treat the by-value argument as its own
	if unsafe.Sizeof(uintptr(0)) == 8 {
		return comCall(acc, method, uintptr(unsafe.Pointer(&c)), uintptr(out))
	}
	w := (*[4]uintptr)(unsafe.Pointer(&c))
	return comCall(acc, method, w[0], w[1], w[2], w[3], uintptr(out))
}

func bstrToString(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := *(*uint32)(unsafe.Pointer(p - 4)) / 2
	s := syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(p)), n))
	procSysFreeString.Call(p)
	return s
}

// accessibleFromEvent returns the name, role and state of the object that
// received focus. ok=false if the app does not support MSAA.
func accessibleFromEvent(hwnd uintptr, idObject, idChild int32) (info accInfo, ok bool) {
	var acc uintptr
	var child variant
	hr, _, _ := procAccessibleObjectFromEvent.Call(hwnd, uintptr(uint32(idObject)), uintptr(uint32(idChild)),
		uintptr(unsafe.Pointer(&acc)), uintptr(unsafe.Pointer(&child)))
	if int32(hr) < 0 || acc == 0 {
		return info, false
	}
	defer comCall(acc, iaRelease)
	defer procVariantClear.Call(uintptr(unsafe.Pointer(&child)))
	info = readInfo(acc, &child)
	return info, true
}

var roleNames = map[int32]string{
	0x09: "window", 0x0A: "client", 0x0B: "menu", 0x0C: "menu item",
	0x0F: "document", 0x10: "pane", 0x14: "group", 0x21: "list",
	0x22: "list item", 0x23: "tree", 0x24: "tree item",
	0x25: "tab", 0x2A: "text", 0x2B: "button", 0x2C: "checkbox",
	0x2D: "radio button", 0x2E: "combo box", 0x1E: "link", 0x29: "label",
}

func roleName(role int32) string {
	if n, ok := roleNames[role]; ok {
		return n
	}
	return "role " + strconv.Itoa(int(role))
}

var (
	procAccessibleObjectFromWindow = oleacc.NewProc("AccessibleObjectFromWindow")
	iidIAccessible                 = [16]byte{0xE0, 0x36, 0x87, 0x61, 0x3D, 0x3C, 0xCF, 0x11, 0x81, 0x0C, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71} // {618736E0-3C3D-11CF-810C-00AA00389B71}
)

var objidClientArg = uintptr(0xFFFFFFFC) // OBJID_CLIENT (-4) as a DWORD

const (
	vtDispatch = 9  // VT_DISPATCH: a VARIANT holding an object (IDispatch)
	iaQI       = 0  // IUnknown::QueryInterface
	iaFocus    = 18 // IAccessible::get_accFocus
)

// readInfo reads the name, role and state of element (acc, child).
func readInfo(acc uintptr, child *variant) accInfo {
	var info accInfo
	var name uintptr
	if hr := accGet(acc, iaName, child, unsafe.Pointer(&name)); int32(hr) >= 0 {
		info.Name = bstrToString(name)
	}
	var role variant
	if hr := accGet(acc, iaRole, child, unsafe.Pointer(&role)); int32(hr) >= 0 {
		if role.VT == vtI4 {
			info.Role = int32(role.Val)
		}
		procVariantClear.Call(uintptr(unsafe.Pointer(&role)))
	}
	var state variant
	if hr := accGet(acc, iaState, child, unsafe.Pointer(&state)); int32(hr) >= 0 {
		if state.VT == vtI4 {
			info.State = int32(state.Val)
		}
		procVariantClear.Call(uintptr(unsafe.Pointer(&state)))
	}
	switch info.Role {
	case roleSystemText, roleSystemComboBox, roleSystemDocument:
		info.Editable = info.State&stateSystemReadonly == 0
	}
	if r := []rune(info.Name); len(r) > maxNameLen {
		info.Name = string(r[:maxNameLen])
	}
	return info
}

// focusedInWindow follows get_accFocus from the window's client area
// down to the deepest focused element. path is the chain, for diagnostics.
func focusedInWindow(hwnd uintptr) (leaf accInfo, path []accInfo, ok bool) {
	var acc uintptr
	hr, _, _ := procAccessibleObjectFromWindow.Call(hwnd, objidClientArg,
		uintptr(unsafe.Pointer(&iidIAccessible)), uintptr(unsafe.Pointer(&acc)))
	if int32(hr) < 0 || acc == 0 {
		return leaf, nil, false
	}
	self := variant{VT: vtI4}
	leaf = readInfo(acc, &self)
	path = append(path, leaf)
	for depth := 0; depth < 30; depth++ {
		var f variant
		hr := comCall(acc, iaFocus, uintptr(unsafe.Pointer(&f)))
		if int32(hr) < 0 || hr == 1 { // S_FALSE — nothing has focus
			break
		}
		if f.VT == vtI4 {
			if f.Val != 0 { // simple child element
				leaf = readInfo(acc, &f)
				path = append(path, leaf)
			}
			break
		}
		if f.VT != vtDispatch || f.Val == 0 {
			procVariantClear.Call(uintptr(unsafe.Pointer(&f)))
			break
		}
		var next uintptr
		comCall(uintptr(f.Val), iaQI, uintptr(unsafe.Pointer(&iidIAccessible)), uintptr(unsafe.Pointer(&next)))
		procVariantClear.Call(uintptr(unsafe.Pointer(&f)))
		if next == 0 || next == acc {
			if next != 0 {
				comCall(next, iaRelease)
			}
			break
		}
		comCall(acc, iaRelease)
		acc = next
		leaf = readInfo(acc, &self)
		path = append(path, leaf)
	}
	comCall(acc, iaRelease)
	return leaf, path, true
}
