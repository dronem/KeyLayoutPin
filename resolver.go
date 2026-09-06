//go:build windows

package main

import (
	"io"
	"runtime"
	"sync"
	"sync/atomic"
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
	reqs      chan resolveReq
	abandoned atomic.Bool
}

type resolveReq struct {
	gen uint64
	ev  focusEvent
}

// maxStuck caps how many abandoned (hung) resolver threads may exist at
// once; beyond that new focus events are skipped until some come back.
const maxStuck = 8

var (
	resultsMu sync.Mutex
	results   []resolved

	stuckResolvers atomic.Int32
)

func newResolver(window uintptr) *resolver {
	r := &resolver{reqs: make(chan resolveReq, 1)}
	go func() {
		runtime.LockOSThread()
		// MTA rather than STA: an STA thread must keep pumping messages,
		// while this one waits on a Go channel between requests.
		if hr := coInitializeMTA(); int32(hr) < 0 {
			errorf("COM init failed: 0x%08X", uint32(hr))
		}
		defer coUninitialize()
		defer func() {
			if r.abandoned.Load() {
				stuckResolvers.Add(-1)
			}
		}()
		for req := range r.reqs {
			f, ok := describeFocus(req.ev.hwnd, req.ev.obj, req.ev.child)
			resultsMu.Lock()
			results = append(results, resolved{gen: req.gen, field: f, ok: ok})
			resultsMu.Unlock()
			procPostMessageW.Call(window, wmFieldResolved, 0, 0)
		}
	}()
	return r
}

// abandon gives up on a resolver stuck in a hung app. Its thread exits
// when (if) the call returns; its late result is ignored by generation.
func (r *resolver) abandon() int32 {
	r.abandoned.Store(true)
	close(r.reqs)
	return stuckResolvers.Add(1)
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

func (a *asyncWriter) Write(p []byte) (int, error) {
	select {
	case a.ch <- append([]byte(nil), p...):
	default: // queue is full — drop the log line rather than block
	}
	return len(p), nil
}
