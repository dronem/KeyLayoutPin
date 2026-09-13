//go:build windows

package main

import (
	"fmt"
	"strings"
)

// installed returns the installed layout matching h, or one with the same
// language if that exact keyboard was removed; 0 if none.
func installed(h HKL) HKL {
	list := keyboardLayoutList()
	for _, l := range list {
		if l == h {
			return l
		}
	}
	for _, l := range list {
		if l.LangID() == h.LangID() {
			return l
		}
	}
	return 0
}

// layoutByLocale finds a layout by language ("en-US" or just "en"),
// preferring the language's standard keyboard.
func layoutByLocale(name string) HKL {
	var found HKL
	for _, l := range keyboardLayoutList() {
		loc := localeName(l)
		if !strings.EqualFold(loc, name) && !strings.EqualFold(strings.SplitN(loc, "-", 2)[0], name) {
			continue
		}
		if l.Variant() == l.LangID() {
			return l
		}
		if found == 0 {
			found = l
		}
	}
	return found
}

// fallbackFor turns -default into the layout for new fields:
// "system" — from Windows settings, "keep" — none, or a language.
func fallbackFor(mode string) (func() HKL, error) {
	switch mode {
	case "keep":
		return nil, nil
	case "system":
		return defaultInputLayout, nil
	}
	if layoutByLocale(mode) == 0 {
		return nil, fmt.Errorf("layout %q is not installed (see -list)", mode)
	}
	return func() HKL { return layoutByLocale(mode) }, nil
}

func printLayouts() {
	def := defaultInputLayout()
	fmt.Println("Installed layouts:")
	for _, l := range keyboardLayoutList() {
		mark := ""
		if l == def {
			mark = "  (default)"
		}
		fmt.Printf("  %s  %s%s\n", l, layoutName(l), mark)
	}
}
