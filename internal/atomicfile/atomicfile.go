// Package atomicfile writes small private files so that a crash or a kill
// mid-write never leaves a half-written one behind. It has no platform
// dependencies.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write replaces the file at path with data: it writes a temporary file next
// to it and renames it over the old one. Missing folders are created. The
// file and folders are readable by the current user only.
func Write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
