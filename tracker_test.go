//go:build windows

package main

import (
	"testing"
	"time"

	"github.com/dronem/keylayoutpin/internal/engine"
)

type fakeWorker struct {
	submitted []resolveReq
	abandoned bool
}

func (w *fakeWorker) submit(r resolveReq) { w.submitted = append(w.submitted, r) }
func (w *fakeWorker) abandon() int32      { w.abandoned = true; return 1 }

type fakeSink struct {
	focused []engine.Field
	ticks   int
}

func (s *fakeSink) Focus(f focus) { s.focused = append(s.focused, f.field) }
func (s *fakeSink) Tick()         { s.ticks++ }

type trackerRig struct {
	t       *tracker
	sink    *fakeSink
	workers []*fakeWorker
	posts   int
	now     time.Time
	stuck   int32
	pids    map[uintptr]uint32
}

func newRig() *trackerRig {
	r := &trackerRig{sink: &fakeSink{}, now: time.Unix(1000, 0), pids: map[uintptr]uint32{}}
	r.t = &tracker{
		sink:    r.sink,
		box:     &inbox{notify: func() {}},
		pidOf:   func(h uintptr) uint32 { return r.pids[h] },
		appName: func(uint32) string { return "app.exe" },
		stuck:   func() int32 { return r.stuck },
		now:     func() time.Time { return r.now },
		post:    func() { r.posts++ },
		hung:    map[uint32]time.Time{},
	}
	r.t.spawn = func() worker {
		w := &fakeWorker{}
		r.workers = append(r.workers, w)
		return w
	}
	r.t.res = r.t.spawn()
	return r
}

func (r *trackerRig) worker() *fakeWorker { return r.workers[len(r.workers)-1] }

// reply delivers the resolver's answer for the last submitted request.
func (r *trackerRig) reply(key string) {
	w := r.worker()
	req := w.submitted[len(w.submitted)-1]
	r.t.box.put(resolved{gen: req.gen, focus: focus{ev: req.ev, ok: true, field: engine.Field{Key: key, Kind: engine.Input}}})
	r.t.handleResolved()
}

func ev(h uintptr) focusEvent { return focusEvent{hwnd: h} }

func TestTrackerCoalescesEvents(t *testing.T) {
	r := newRig()
	r.t.onFocus(ev(1))
	r.t.onFocus(ev(2)) // before the main loop got to dispatch
	if r.posts != 1 {
		t.Fatalf("posts = %d, want 1: one wake-up is enough", r.posts)
	}
	r.t.dispatch()
	if got := r.worker().submitted; len(got) != 1 || got[0].ev.hwnd != 2 {
		t.Fatalf("submitted %+v, want only the latest event", got)
	}

	// While busy, newer events wait and overwrite each other.
	r.t.onFocus(ev(3))
	r.t.dispatch()
	r.t.onFocus(ev(4))
	r.t.dispatch()
	if n := len(r.worker().submitted); n != 1 {
		t.Fatalf("submitted %d requests while busy", n)
	}
	r.reply("two")
	if got := r.worker().submitted; len(got) != 2 || got[1].ev.hwnd != 4 {
		t.Fatalf("after the reply the latest event must follow: %+v", got)
	}
	if len(r.sink.focused) != 1 || r.sink.focused[0].Key != "two" {
		t.Fatalf("sink got %+v", r.sink.focused)
	}
}

func TestTrackerDropsLateReplies(t *testing.T) {
	r := newRig()
	r.t.onFocus(ev(1))
	r.t.dispatch()
	r.t.box.put(resolved{gen: 99, focus: focus{ok: true}})
	r.t.handleResolved()
	if len(r.sink.focused) != 0 || !r.t.busy {
		t.Fatal("a reply with a stale generation must be ignored")
	}
}

func TestTrackerSkipsHungApp(t *testing.T) {
	r := newRig()
	r.pids[1], r.pids[2] = 100, 200
	r.t.onFocus(ev(1))
	r.t.dispatch()
	hung := r.worker()

	r.now = r.now.Add(resolveTimeout + time.Millisecond)
	r.t.tick()
	if !hung.abandoned || len(r.workers) != 2 {
		t.Fatal("a stuck resolver must be abandoned and replaced")
	}
	if r.sink.ticks != 1 {
		t.Fatal("the sink must still tick")
	}

	r.t.onFocus(ev(1)) // the hung app again
	r.t.dispatch()
	if n := len(r.worker().submitted); n != 0 {
		t.Fatal("events of a hung app must be skipped")
	}
	r.t.onFocus(ev(2)) // another app
	r.t.dispatch()
	if n := len(r.worker().submitted); n != 1 {
		t.Fatal("other apps must keep working")
	}
	r.reply("other")

	r.now = r.now.Add(hungSkip)
	r.t.onFocus(ev(1))
	r.t.dispatch()
	if n := len(r.worker().submitted); n != 2 {
		t.Fatal("after hungSkip the app must be asked again")
	}
}

func TestTrackerStopsWhenTooManyStuck(t *testing.T) {
	r := newRig()
	r.stuck = maxStuck
	r.t.onFocus(ev(1))
	r.t.dispatch()
	if len(r.worker().submitted) != 0 {
		t.Fatal("no new requests while too many threads are stuck")
	}
	r.stuck = 0
	r.t.onFocus(ev(1))
	r.t.dispatch()
	if len(r.worker().submitted) != 1 {
		t.Fatal("requests must resume")
	}
}

func TestTrackerForgetsExpiredHungApps(t *testing.T) {
	r := newRig()
	r.pids[1] = 100
	r.t.onFocus(ev(1))
	r.t.dispatch()
	r.now = r.now.Add(resolveTimeout + time.Millisecond)
	r.t.tick() // app 100 is marked hung
	if len(r.t.hung) != 1 {
		t.Fatalf("hung = %v", r.t.hung)
	}
	r.now = r.now.Add(hungSkip)
	r.t.tick() // the app was closed meanwhile and never sends events again
	if len(r.t.hung) != 0 {
		t.Fatal("expired entries must be dropped even without new events")
	}
}

// Chromium turns accessibility off when nothing uses it for a while, and then
// focus moves inside the window send no events. The foreground Chromium
// window is pinged when nothing was asked of the resolver for keepAliveEvery.
func TestTrackerKeepsChromiumAwake(t *testing.T) {
	r := newRig()
	fg := uintptr(0)
	r.t.keepAlive = func() uintptr { return fg }

	r.t.tick()
	if n := len(r.worker().submitted); n != 0 {
		t.Fatal("no Chromium window in front: nothing to ping")
	}

	fg = 7
	r.t.tick()
	reqs := r.worker().submitted
	if len(reqs) != 1 || !reqs[0].ev.ping || reqs[0].ev.hwnd != 7 {
		t.Fatalf("submitted %+v, want a ping of window 7", reqs)
	}
	r.t.box.put(resolved{gen: reqs[0].gen}) // a ping's result has ok=false
	r.t.handleResolved()
	if len(r.sink.focused) != 0 || r.t.busy {
		t.Fatal("a ping must not reach the sink and must free the resolver")
	}

	r.now = r.now.Add(keepAliveEvery - time.Millisecond)
	r.t.tick()
	if n := len(r.worker().submitted); n != 1 {
		t.Fatal("pinged again too soon")
	}

	// A real focus event asks the window anyway and postpones the ping.
	r.t.onFocus(ev(7))
	r.t.dispatch()
	r.reply("field")
	r.now = r.now.Add(keepAliveEvery - time.Millisecond)
	r.t.tick()
	if n := len(r.worker().submitted); n != 2 {
		t.Fatal("a focus event must postpone the ping")
	}
	r.now = r.now.Add(time.Millisecond)
	r.t.tick()
	if reqs := r.worker().submitted; len(reqs) != 3 || !reqs[2].ev.ping {
		t.Fatalf("submitted %+v, want a ping after keepAliveEvery", reqs)
	}
}

// A ping never takes the place of a pending focus event, and a hung app is
// not pinged.
func TestTrackerPingYields(t *testing.T) {
	r := newRig()
	r.t.keepAlive = func() uintptr { return 7 }
	r.pids[7] = 100
	r.now = r.now.Add(keepAliveEvery)

	r.t.onFocus(ev(7)) // not dispatched yet
	r.t.tick()
	if n := len(r.worker().submitted); n != 0 {
		t.Fatal("a ping must not jump ahead of a pending focus event")
	}
	r.t.dispatch()
	if reqs := r.worker().submitted; len(reqs) != 1 || reqs[0].ev.ping {
		t.Fatalf("submitted %+v, want the focus event", reqs)
	}

	r.now = r.now.Add(resolveTimeout + time.Millisecond)
	r.t.tick() // the app hangs: marked hung, resolver replaced
	r.now = r.now.Add(keepAliveEvery)
	r.t.tick()
	if n := len(r.worker().submitted); n != 0 {
		t.Fatal("a hung app must not be pinged")
	}
}
