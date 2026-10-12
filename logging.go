//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// errLog receives errors: they go to the log file even without -log.
// Logging is process-wide by nature, like the standard logger.
var errLog = log.New(os.Stderr, "ERROR ", log.Ltime)

func errorf(format string, args ...any) { errLog.Printf(format, args...) }

// setupLogging sends informational messages to the console and, if full,
// to the file; errors always go to both (file == "" means no file). The
// returned func flushes and closes everything.
func setupLogging(file string, full bool) func() {
	log.SetFlags(log.Ltime)
	infoOut, errOut := teeWriter{os.Stderr}, teeWriter{os.Stderr}
	var f *rotatingFile
	if file != "" {
		var err error
		if f, err = openRotating(file, maxLogSize); err != nil {
			fatalf("cannot open log: %v", err)
		}
		errOut = append(errOut, f)
		if full {
			infoOut = append(infoOut, f)
		}
	}
	infoW, errW := newAsyncWriter(infoOut), newAsyncWriter(errOut)
	log.SetOutput(infoW)
	errLog.SetOutput(errW)
	return func() {
		infoW.Close()
		errW.Close()
		if f != nil {
			f.Close()
		}
	}
}

// asyncWriter writes the log in the background. Console writes block while
// text is selected in the console (QuickEdit) — that must not stall the app.
type asyncWriter struct {
	ch   chan []byte
	done chan struct{}
}

func newAsyncWriter(w io.Writer) *asyncWriter {
	a := &asyncWriter{ch: make(chan []byte, 256), done: make(chan struct{})}
	go func() {
		for b := range a.ch {
			w.Write(b)
		}
		close(a.done)
	}()
	return a
}

func (a *asyncWriter) Write(p []byte) (int, error) {
	select {
	case a.ch <- append([]byte(nil), p...):
	default: // queue is full — drop the log line rather than block
	}
	return len(p), nil
}

// Close flushes the queued lines; the writer must not be used afterwards.
func (a *asyncWriter) Close() {
	close(a.ch)
	select {
	case <-a.done:
	case <-time.After(time.Second):
	}
}

// teeWriter writes to every destination regardless of errors. Unlike
// io.MultiWriter it doesn't stop at the first failure: a GUI build has no
// valid stderr, and that must not prevent writing the log file.
type teeWriter []io.Writer

func (t teeWriter) Write(p []byte) (int, error) {
	for _, w := range t {
		w.Write(p)
	}
	return len(p), nil
}

// guiBuild reports whether this exe is the GUI build. Only that one shows
// message boxes: it has no stderr anyone could read. The console build
// always writes to stderr — it may run without a console window (scripts,
// redirected output), where a modal box would just hang it.
var guiBuild = sync.OnceValue(func() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	s, _ := peSubsystem(exe)
	return s == subsystemGUI
})

// notify tells the user something important: a message box in the GUI
// build, stderr in the console build.
func notify(msg string) {
	if guiBuild() {
		messageBox(msg)
		return
	}
	fmt.Fprintln(os.Stderr, msg)
}

// exitHooks run on fatalf, which exits without running deferred calls.
var exitHooks []func()

func atExit(f func()) { exitHooks = append(exitHooks, f) }

// fatalf reports a startup error and exits: to stderr and the log file
// (errorf), and in the GUI build also in a message box.
func fatalf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	errorf("%s", msg)
	for i := len(exitHooks) - 1; i >= 0; i-- {
		exitHooks[i]()
	}
	time.Sleep(100 * time.Millisecond) // let the async log writers flush
	if guiBuild() {
		messageBox("KeyLayoutPin cannot start:\n\n" + msg)
	}
	os.Exit(1)
}

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
