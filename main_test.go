//go:build windows

package main

import (
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/dronem/keylayoutpin/internal/engine"
)

type stubSystem struct{}

func (stubSystem) Layout(uint32) HKL         { return 0x04090409 }
func (stubSystem) Request(uintptr, HKL) bool { return true }
func (stubSystem) Foreground() uintptr       { return 1 }
func (stubSystem) LastHotkey() time.Time     { return time.Time{} }
func (stubSystem) Now() time.Time            { return time.Unix(100, 0) }

// stubMemory remembers ja-JP for every field, so entering one is logged.
type stubMemory struct{}

func (stubMemory) Get(string) (HKL, bool) { return 0x04110411, true }
func (stubMemory) Set(string, HKL) error  { return nil }
func (stubMemory) Forget(string) error    { return nil }

// Field labels can be personal: without -v they must not reach the log.
func TestSinkHidesFieldNamesWithoutVerbose(t *testing.T) {
	saved := log.Writer()
	log.SetOutput(io.Discard) // -v also logs every focus through the standard logger
	defer log.SetOutput(saved)
	secret := focus{field: engine.Field{Key: "k", App: "chat.exe", Name: "Message to Jane Doe", Root: 1, Kind: engine.Input}}
	for _, verbose := range []bool{false, true} {
		var logged []string
		e := engine.New(engine.Config{System: stubSystem{}, Memory: stubMemory{},
			Logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }})
		engineSink{e, verbose}.Focus(secret)
		all := strings.Join(logged, "\n")
		if leaked := strings.Contains(all, "Jane Doe"); leaked != verbose {
			t.Errorf("verbose=%v: label in log = %v, log: %q", verbose, leaked, all)
		}
	}
}
