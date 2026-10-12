//go:build windows

package main

import (
	"sync/atomic"
	"syscall"
	"time"
)

const wmFieldResolved = wmApp + 2

// hungSkip is how long focus events of a hung app are skipped.
const hungSkip = 30 * time.Second

// sink receives resolved focus events and timer ticks: the engine in normal
// mode, the observer in -watch mode.
type sink interface {
	Focus(f focus)
	Tick()
}

// tracker moves focus events from the WinEvent hook to a resolver thread and
// the results to the sink, all on the main thread. Only the latest focus
// matters: events that arrive while the resolver is busy overwrite each other.
// A resolver stuck in a hung app is abandoned, and that app is skipped for
// a while so it can't swallow more threads.
type tracker struct {
	sink    sink
	box     *inbox
	spawn   func() worker
	pidOf   func(hwnd uintptr) uint32
	appName func(pid uint32) string
	stuck   func() int32 // abandoned resolvers still running
	now     func() time.Time
	post    func() // asks the main thread to call dispatch

	ev       focusEvent // latest event, not yet handed to the resolver
	evNew    bool
	evQueued bool

	res       worker
	gen       uint64 // id of the last event handed to the resolver
	busy      bool
	busySince time.Time
	sentPID   uint32               // process of the event being resolved
	hung      map[uint32]time.Time // hung processes → skipped until then
}

// newTracker wires a tracker to the message window and real resolver threads.
func newTracker(s sink, win *msgWindow, describe func(focusEvent) focus) *tracker {
	var stuck atomic.Int32
	box := &inbox{notify: func() { win.post(wmFieldResolved) }}
	t := &tracker{
		sink: s,
		box:  box,
		pidOf: func(hwnd uintptr) uint32 {
			_, pid := windowThreadProcess(hwnd)
			return pid
		},
		appName: processName,
		stuck:   stuck.Load,
		now:     time.Now,
		post:    func() { win.post(wmFocusChanged) },
		hung:    map[uint32]time.Time{},
	}
	t.spawn = func() worker { return startResolver(describe, box, &stuck) }
	t.res = t.spawn()
	win.on(wmFocusChanged, func(_, _ uintptr) { t.dispatch() })
	win.on(wmFieldResolved, func(_, _ uintptr) { t.handleResolved() })
	return t
}

// hookFocus subscribes to focus changes system-wide (except our own process).
func (t *tracker) hookFocus() (unhook func(), err error) {
	h, _, err := procSetWinEventHook.Call(eventObjectFocus, eventObjectFocus, 0,
		syscall.NewCallback(t.onWinEvent), 0, 0, wineventOutOfContext|wineventSkipOwnProcess)
	if h == 0 {
		return nil, err
	}
	return func() { procUnhookWinEvent.Call(h) }, nil
}

// onWinEvent is called by Windows from GetMessage on the main thread.
// It only records the event; resolving happens on a resolver thread.
func (t *tracker) onWinEvent(hook, event, hwnd, idObject, idChild, idThread, eventTime uintptr) uintptr {
	if hwnd != 0 {
		t.onFocus(focusEvent{hwnd: hwnd, obj: int32(idObject), child: int32(idChild)})
	}
	return 0
}

func (t *tracker) onFocus(ev focusEvent) {
	t.ev, t.evNew = ev, true
	if !t.evQueued {
		t.evQueued = true
		t.post()
	}
}

// dispatch hands the latest event to the resolver if it is idle.
func (t *tracker) dispatch() {
	t.evQueued = false
	if t.busy || !t.evNew {
		return
	}
	t.evNew = false
	pid := t.pidOf(t.ev.hwnd)
	if until, ok := t.hung[pid]; ok {
		if t.now().Before(until) {
			return // the app is hung: asking it again would hang another thread
		}
		delete(t.hung, pid)
	}
	if t.stuck() >= maxStuck {
		return // too many threads stuck in hung apps; wait for some to return
	}
	t.gen++
	t.busy, t.busySince, t.sentPID = true, t.now(), pid
	t.res.submit(resolveReq{gen: t.gen, ev: t.ev})
}

func (t *tracker) handleResolved() {
	for _, r := range t.box.take() {
		if r.gen != t.gen {
			continue // late reply from an abandoned thread
		}
		t.busy = false
		if r.ok {
			t.sink.Focus(r.focus)
		}
	}
	t.dispatch()
}

// tick runs on the timer: watches for a stuck resolver, then lets the sink
// check the layout.
func (t *tracker) tick() {
	if t.busy && t.now().Sub(t.busySince) > resolveTimeout {
		stuck := t.res.abandon()
		errorf("app %s is not responding — skipping it for %v (stuck threads: %d)",
			t.appName(t.sentPID), hungSkip, stuck)
		t.hung[t.sentPID] = t.now().Add(hungSkip)
		t.res = t.spawn()
		t.busy = false
		t.dispatch() // a newer event may be waiting
	}
	for pid, until := range t.hung {
		if !t.now().Before(until) {
			delete(t.hung, pid) // also covers apps that were closed meanwhile
		}
	}
	t.sink.Tick()
}
