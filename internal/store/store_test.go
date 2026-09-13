package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"change_lang/internal/kbd"
)

const legacyJSON = `{
  "entries": {
    "chrome.exe|Chrome_WidgetWin_1|Chrome_WidgetWin_1|42|Enter the code from Jane Doe": {
      "layout": "0x04110411", "lang": "ja-JP", "app": "chrome.exe",
      "field": "Enter the code from Jane Doe", "updated": "2026-10-11T20:42:00+02:00"
    }
  }
}`

const secretKey = "chrome.exe|Chrome_WidgetWin_1|Chrome_WidgetWin_1|42|Enter the code from Jane Doe"

func noPlaintext(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Jane Doe", "chrome", "updated", "2026"} {
		if strings.Contains(string(data), s) {
			t.Fatalf("file still contains %q:\n%s", s, data)
		}
	}
}

func TestMigrateFromRoaming(t *testing.T) {
	roaming, local := t.TempDir(), t.TempDir()
	t.Setenv("APPDATA", roaming)         // os.UserConfigDir on Windows
	t.Setenv("XDG_CONFIG_HOME", roaming) // os.UserConfigDir elsewhere
	t.Setenv("LOCALAPPDATA", local)
	old := filepath.Join(roaming, "change_lang", "layouts.json")
	os.MkdirAll(filepath.Dir(old), 0o700)
	os.WriteFile(old, []byte(legacyJSON), 0o600)

	s, err := Open(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := s.Get(secretKey); !ok || h != 0x04110411 {
		t.Fatalf("entry lost after migration: %v %v", h, ok)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the old plain-text file must be deleted")
	}
	noPlaintext(t, DefaultPath())

	// Reload — same salt, the entry is still there.
	s2, err := Open(DefaultPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get(secretKey); !ok {
		t.Fatal("entry not found after reload")
	}
}

func TestMigrateCustomPathInPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my.json")
	os.WriteFile(path, []byte(legacyJSON), 0o600)
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(secretKey); !ok {
		t.Fatal("entry lost")
	}
	noPlaintext(t, path)
}

func TestTrimKeepsNewest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxEntries+5; i++ {
		s.Entries[s.hash(string(rune(i)))] = entry{Layout: "0x04110411", Seq: uint64(i + 1)}
	}
	s.Seq = maxEntries + 5
	if err := s.Set("newest", kbd.HKL(0x04090409)); err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != maxEntries {
		t.Fatalf("%d entries, want %d", len(s.Entries), maxEntries)
	}
	if _, ok := s.Get("newest"); !ok {
		t.Fatal("the newest entry was dropped")
	}
	if _, ok := s.Entries[s.hash(string(rune(0)))]; ok {
		t.Fatal("the oldest entry should have been dropped")
	}
}
