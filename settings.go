//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// settings are the options changed from the Settings window. They live in
// settings.json next to the memory file, so -db on a USB stick takes them
// along.
type settings struct {
	// FieldDot: on entering a field, briefly show a dot on the tray icon —
	// green if the field has a remembered layout, yellow if it gets the default.
	FieldDot bool `json:"fieldDot"`

	path string
}

// settingsPath is settings.json in the memory file's folder.
func settingsPath(db string) string { return filepath.Join(filepath.Dir(db), "settings.json") }

// loadSettings reads the settings. A missing file gives the defaults; a
// broken one gives the defaults and an error, and is overwritten on save.
func loadSettings(path string) (*settings, error) {
	s := &settings{path: path}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err == nil {
		err = json.Unmarshal(data, s)
	}
	if err != nil {
		*s = settings{path: path}
		return s, err
	}
	return s, nil
}

func (s *settings) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
