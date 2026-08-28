//go:build windows

package main

import (
	"io"
	"runtime"
	"sync"
	"time"
)

// resolveTimeout is how long to wait for MSAA. A call into a hung
// app may never return; then the thread is abandoned and a new one started.
const resolveTimeout = 2 * time.Second

const wmFieldResolved = wmApp + 2

type focusEvent struct {
	hwnd       uintptr
	obj, child int32
}

type resolved struct {
	gen   uint64
	field Field
	ok    bool
}

// resolver identifies the field for a focus event on a separate thread,
// so the main thread (hook, timer, layout switching) never waits on
// other applications.
type resolver struct {
	reqs chan resolveReq
}

type resolveReq struct {
	gen uint64
	ev  focusEvent
}

var (
	resultsMu sync.Mutex
	results   []resolved
)

func newResolver(mainThread uint32) *resolver {
	r := &resolver{reqs: make(chan resolveReq, 1)}
	go func() {
		runtime.LockOSThread()
		coInitialize()
		defer coUninitialize()
		for req := range r.reqs {
			f, ok := describeFocus(req.ev.hwnd, req.ev.obj, req.ev.child)
			resultsMu.Lock()
			results = append(results, resolved{gen: req.gen, field: f, ok: ok})
			resultsMu.Unlock()
			procPostThreadMessageW.Call(uintptr(mainThread), wmFieldResolved, 0, 0)
		}
	}()
	return r
}

func takeResults() []resolved {
	resultsMu.Lock()
	defer resultsMu.Unlock()
	r := results
	results = nil
	return r
}

// asyncWriter writes the log in the background. Console writes block while
// text is selected in the console (QuickEdit) — that must not stall the app.
type asyncWriter struct{ ch chan []byte }

func newAsyncWriter(w io.Writer) *asyncWriter {
	a := &asyncWriter{ch: make(chan []byte, 256)}
	go func() {
		for b := range a.ch {
			w.Write(b)
		}
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
