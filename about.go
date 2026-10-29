//go:build windows

package main

import (
	"flag"
	"strings"
)

// Texts for the tray menu's About and Help.

const (
	projectURL = "https://github.com/dronem/keylayoutpin"
	docsURL    = "https://dronem.github.io/keylayoutpin/" // GitHub Pages, from docs/
)

func aboutText() string {
	return "KeyLayoutPin " + versionString() + "\n\n" +
		"Pins the keyboard layout to each input field separately: switch the layout " +
		"once in a field, and it comes back by itself whenever you return there.\n\n" +
		projectURL
}

const helpPrinciples = `How it works
• Switch the layout with your usual hotkey (Ctrl+Shift, Alt+Shift, Win+Space) in a field — it is remembered for that field and comes back whenever you return.
• The layout a field had when you left it is remembered too.
• Fields where you never switched get the default layout (-default).
• Buttons, menus and lists are not fields: the layout is left as is.
• Apps that don't tell Windows which field has focus (games, some custom apps) count as one field — the whole window.
• A layout change made by Windows or the app right after focus is undone.
• Fields are stored only as salted hashes, and fields with the default layout are not stored at all.`

// helpText is the principles plus every command-line option, so the help
// can't fall behind the flags.
func helpText(fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(helpPrinciples)
	b.WriteString("\n\nOptions (keylayoutpin.exe -h prints them with defaults)\n")
	fs.VisitAll(func(f *flag.Flag) {
		b.WriteString("-" + f.Name + " — " + f.Usage + "\n")
	})
	return strings.TrimSuffix(b.String(), "\n")
}
