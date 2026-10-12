package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dronem/keylayoutpin/internal/kbd"
)

const secretKey = "browser.exe|MainWindow|MainWindow|42|Enter the code from Jane Doe"

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
	for _, p := range []string{"Jane Doe", "browser", "MainWindow"} {
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
		s.entries[s.hash(string(rune(i)))] = entry{Layout: "0x04110411", Seq: uint64(i + 1)}
	}
	s.seq = maxEntries + 5
	if err := s.Set("newest", kbd.HKL(0x04090409)); err != nil {
		t.Fatal(err)
	}
	if len(s.entries) != maxEntries {
		t.Fatalf("%d entries, want %d", len(s.entries), maxEntries)
	}
	if _, ok := s.Get("newest"); !ok {
		t.Fatal("the newest entry was dropped")
	}
	if _, ok := s.entries[s.hash(string(rune(0)))]; ok {
		t.Fatal("the oldest entry should have been dropped")
	}
}

// The file format is a contract with files already on disk.
func TestReadsFileFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.json")
	s, _ := Open(path)
	key := s.hash(secretKey)
	os.WriteFile(path, []byte(`{
  "salt": "`+s.salt+`",
  "seq": 7,
  "entries": { "`+key+`": { "layout": "0x04110411", "seq": 7 } }
}`), 0o600)

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if h, ok := s2.Get(secretKey); !ok || h != 0x04110411 || s2.seq != 7 {
		t.Fatalf("got %v %v, seq %d", h, ok, s2.seq)
	}
}
