package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCreatesFoldersAndReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b", "f.json")
	if err := Write(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("two")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "two" {
		t.Errorf("content = %q, want %q", got, "two")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}

func TestWriteFailsWithoutLeftovers(t *testing.T) {
	// The target is a non-empty folder: the rename fails.
	path := filepath.Join(t.TempDir(), "f")
	if err := os.MkdirAll(filepath.Join(path, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("data")); err == nil {
		t.Fatal("want an error")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temporary file left behind: %v", err)
	}
}
