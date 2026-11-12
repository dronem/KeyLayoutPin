//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s, err := loadSettings(path)
	if err != nil || s.FieldDot {
		t.Fatalf("missing file: %+v, %v; want defaults, no error", s, err)
	}
	s.FieldDot = true
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	s, err = loadSettings(path)
	if err != nil || !s.FieldDot {
		t.Fatalf("reloaded %+v, %v", s, err)
	}
}

func TestSettingsBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, []byte(`{"fieldDot": tru`), 0o600)
	s, err := loadSettings(path)
	if err == nil || s.FieldDot || s.path != path {
		t.Fatalf("broken file: %+v, %v; want defaults and an error", s, err)
	}
	if err := s.save(); err != nil {
		t.Fatal("a broken file must be overwritable:", err)
	}
}

func TestSettingsPath(t *testing.T) {
	if got, want := settingsPath(`C:\data\layouts.json`), `C:\data\settings.json`; got != want {
		t.Fatalf("settingsPath = %q, want %q", got, want)
	}
}

func TestBlend(t *testing.T) {
	const blue, red = 0xFF2563EB, 0xFFFF0000
	if got := blend(blue, red, 0); got != blue {
		t.Fatalf("no coverage: %08X", got)
	}
	if got := blend(blue, red, 1); got != red {
		t.Fatalf("full coverage: %08X", got)
	}
	if got := blend(0, red, 0.5); got>>24 != 0x80 || got&0xFFFFFF != 0xFF0000 {
		t.Fatalf("over transparent: %08X, want half-transparent red", got)
	}
}

func TestDrawDot(t *testing.T) {
	const size = 32
	px := make([]uint32, size*size)
	drawDot(px, size, dotGreen)
	if got := px[(size-9)*size+size-9]; got != dotGreen {
		t.Fatalf("dot center = %08X, want green", got)
	}
	if got := px[0]; got != 0 {
		t.Fatalf("top left = %08X, want untouched", got)
	}
}
