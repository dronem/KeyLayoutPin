//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileCapsSize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	r, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 10; i++ {
		if _, err := r.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
	}
	r.Close()

	cur, _ := os.Stat(path)
	old, err := os.Stat(filepath.Join(filepath.Dir(path), "log.old.txt"))
	if err != nil {
		t.Fatal("log.old.txt must exist after rotation")
	}
	if cur.Size() > 100 || old.Size() > 100 {
		t.Fatalf("sizes %d and %d exceed the cap", cur.Size(), old.Size())
	}
	if cur.Size()+old.Size() == 0 {
		t.Fatal("nothing written")
	}
}

func TestRotatingFileRotatesBigFileOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.txt")
	os.WriteFile(path, []byte(strings.Repeat("y", 200)), 0o600)
	r, err := openRotating(path, 100)
	if err != nil {
		t.Fatal(err)
	}
	r.Write([]byte("new\n"))
	r.Close()
	data, _ := os.ReadFile(path)
	if string(data) != "new\n" {
		t.Fatalf("expected a fresh log, got %q", data)
	}
}
