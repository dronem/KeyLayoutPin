//go:build windows

package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// resolveTimeout is how long to wait for MSAA. A call into a hung
// app may never return; then the thread is abandoned and a new one started.
const resolveTimeout = 2 * time.Second

// maxStuck caps how many abandoned (hung) resolver threads may exist at
// once; beyond that new focus events are skipped until some come back.
const maxStuck = 8

type focusEvent struct {
	hwnd       uintptr
	obj, child int32
}

type resolveReq struct {
	gen uint64
	ev  focusEvent
}

// resolved is what a resolver found out about a focus event.
type resolved struct {
	gen uint64
	focus
}

// inbox collects results from resolver threads (including late ones from
// abandoned threads, which the tracker drops by generation).
type inbox struct {
	mu     sync.Mutex
	items  []resolved
	notify func() // wakes up the main thread
}

func (b *inbox) put(r resolved) {
	b.mu.Lock()
	b.items = append(b.items, r)
	b.mu.Unlock()
	b.notify()
}

func (b *inbox) take() []resolved {
	b.mu.Lock()
	defer b.mu.Unlock()
	r := b.items
	b.items = nil
	return r
}

// worker resolves focus events off the main thread.
type worker interface {
	submit(resolveReq)
	abandon() int32 // gives up on a stuck worker; returns how many are stuck
}

// resolver is a worker on its own OS thread, so the main thread (hook,
// timer, layout switching) never waits on other applications.
type resolver struct {
	reqs      chan resolveReq
	abandoned atomic.Bool
	stuck     *atomic.Int32
}

func startResolver(describe func(focusEvent) focus, box *inbox, stuck *atomic.Int32) *resolver {
	r := &resolver{reqs: make(chan resolveReq, 1), stuck: stuck}
	go func() {
		runtime.LockOSThread()
		// MTA: this thread waits on a Go channel between requests and
		// doesn't pump messages, which an STA thread would have to do.
		doneCOM, err := initCOM()
		if err != nil {
			errorf("%v", err)
		}
		defer doneCOM()
		defer func() {
			if r.abandoned.Load() {
				stuck.Add(-1)
			}
		}()
		for req := range r.reqs {
			box.put(resolved{gen: req.gen, focus: describe(req.ev)})
		}
	}()
	return r
}

func (r *resolver) submit(req resolveReq) { r.reqs <- req }

// abandon gives up on a resolver stuck in a hung app. Its thread exits
// when (if) the call returns; its late result is ignored by generation.
func (r *resolver) abandon() int32 {
	r.abandoned.Store(true)
	close(r.reqs)
	return r.stuck.Add(1)
}
