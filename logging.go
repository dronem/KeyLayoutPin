//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// maxLogSize keeps the log from growing forever and piling up history.
const maxLogSize = 1 << 20

// rotatingFile is a log file capped at max bytes: when a write would exceed
// it, the file is renamed to .old (replacing the previous .old) and a new one
// is started. So at most two files' worth of history stay on disk, however
// long the app runs. Safe for concurrent use: the info and error writers
// share one file.
type rotatingFile struct {
	mu   sync.Mutex
	path string
	max  int64
	f    *os.File
	size int64
}

func openRotating(path string, max int64) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, max: max}
	if st, err := os.Stat(path); err == nil && st.Size() >= max {
		r.rotate()
	}
	return r, r.open()
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	r.f, r.size = f, 0
	if st, err := f.Stat(); err == nil {
		r.size = st.Size()
	}
	return nil
}

func (r *rotatingFile) oldPath() string {
	ext := filepath.Ext(r.path)
	return strings.TrimSuffix(r.path, ext) + ".old" + ext
}

func (r *rotatingFile) rotate() {
	os.Remove(r.oldPath())
	os.Rename(r.path, r.oldPath())
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return 0, os.ErrClosed
	}
	if r.size > 0 && r.size+int64(len(p)) > r.max {
		r.f.Close()
		r.rotate()
		if err := r.open(); err != nil {
			r.f = nil
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
