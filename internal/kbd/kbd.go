// Package kbd holds the keyboard layout handle shared by the engine, the
// store and the Windows layer. It has no platform dependencies.
package kbd

import (
	"fmt"
	"strconv"
)

// HKL is a Windows keyboard layout handle: the low word is the language,
// the high word the keyboard variant (device).
type HKL uintptr

// LangID returns the language part, e.g. 0x0411 for Japanese.
func (h HKL) LangID() uint16 { return uint16(h & 0xFFFF) }

// Variant returns the keyboard part, e.g. 0x0415 for the Polish keyboard.
func (h HKL) Variant() uint16 { return uint16(h >> 16) }

func (h HKL) String() string { return fmt.Sprintf("0x%08X", uint64(h)) }

// Parse reads a layout written by String.
func Parse(s string) (HKL, bool) {
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		return 0, false
	}
	return HKL(v), true
}
