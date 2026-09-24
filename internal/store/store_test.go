package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"keylayoutpin/internal/kbd"
)

const secretKey = "chrome.exe|Chrome_WidgetWin_1|Chrome_WidgetWin_1|42|Enter the code from Jane Doe"

func TestSetGetSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get(secretKey); ok {
		t.Fatal("a new memory must be empty")
	}
	if err := s.Set(secretKey, 0x04110411); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path) // same salt from the file
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := s2.Get(secretKey); !ok || h != 0x04110411 {
		t.Fatalf("after reopen: %v %v", h, ok)
	}
	if err := s2.Forget(secretKey); err != nil {
		t.Fatal(err)
	}
	if _, ok := s2.Get(secretKey); ok {
		t.Fatal("Forget must remove the entry")
	}
}

func TestFileHasNoPlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	if err := s.Set(secretKey, 0x04110411); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"Jane Doe", "chrome", "Chrome_WidgetWin"} {
		if strings.Contains(string(data), p) {
			t.Fatalf("the file contains %q:\n%s", p, data)
		}
	}
}

func TestSaltDiffersPerFile(t *testing.T) {
	a, _ := Open(filepath.Join(t.TempDir(), "a.json"))
	b, _ := Open(filepath.Join(t.TempDir(), "b.json"))
	if a.hash(secretKey) == b.hash(secretKey) {
		t.Fatal("the same key must hash differently in different installations")
	}
}

func TestBrokenFileIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	os.WriteFile(path, []byte("{ not json"), 0o600)
	if _, err := Open(path); err == nil {
		t.Fatal("a broken file must not be silently replaced")
	}
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
