//go:build windows

package main

import (
	"strconv"
	"syscall"
	"unsafe"
)

// Minimal MSAA (IAccessible) wrapper. It is needed to tell apart
// fields inside one window: in Chrome, Electron, WPF etc. all inputs
// live in a single HWND, and only the accessibility tree knows which
// one has focus.

const (
	vtI4 = 3

	roleSystemWindow   = 0x09
	roleSystemClient   = 0x0A
	roleSystemDocument = 0x0F
	roleSystemText     = 0x2A
	roleSystemComboBox = 0x2E

	stateSystemReadonly = 0x40

	// IAccessible vtable method indices
	iaRelease  = 2
	iaName     = 10
	iaRole     = 13
	iaState    = 14
	maxNameLen = 120
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

func coInitializeMTA() uintptr {
	hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	return hr
}

func coUninitialize() {
	procCoUninitialize.Call()
}

func comCall(obj uintptr, method int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(obj))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(method)*unsafe.Sizeof(uintptr(0))))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{obj}, args...)...)
	return r
}

// variantArgs turns a by-value VARIANT into call arguments: on
// x64/arm64 structs larger than 16 bytes are passed as a pointer to
// a copy, on x86 directly on the stack.
func variantArgs(v *variant) []uintptr {
	if unsafe.Sizeof(uintptr(0)) == 8 {
		c := *v
		return []uintptr{uintptr(unsafe.Pointer(&c))}
	}
	w := (*[4]uintptr)(unsafe.Pointer(v))
	return w[:]
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
	vtDispatch = 9
	iaQI       = 0
	iaFocus    = 18
)

// readInfo reads the name, role and state of element (acc, child).
func readInfo(acc uintptr, child *variant) accInfo {
	var info accInfo
	var name uintptr
	if hr := comCall(acc, iaName, append(variantArgs(child), uintptr(unsafe.Pointer(&name)))...); int32(hr) >= 0 {
		info.Name = bstrToString(name)
	}
	var role variant
	if hr := comCall(acc, iaRole, append(variantArgs(child), uintptr(unsafe.Pointer(&role)))...); int32(hr) >= 0 {
		if role.VT == vtI4 {
			info.Role = int32(role.Val)
		}
		procVariantClear.Call(uintptr(unsafe.Pointer(&role)))
	}
	var state variant
	if hr := comCall(acc, iaState, append(variantArgs(child), uintptr(unsafe.Pointer(&state)))...); int32(hr) >= 0 {
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
