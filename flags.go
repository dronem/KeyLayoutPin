//go:build windows

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"
)

// options are the command-line settings.
type options struct {
	db       string
	def      string
	interval time.Duration
	verbose  bool
	chromium bool
	list     bool
	reset    bool
	watch    time.Duration
	probe    time.Duration
	version  bool
	log      logFlag
}

// diagnostic reports a one-shot or observation mode: no tray, no switching.
func (o *options) diagnostic() bool { return o.watch > 0 || o.probe > 0 || o.list || o.reset }

// logFile is the file to write to ("" — none). Diagnostic modes print field
// labels and window titles, so they use a file only if -log was explicit.
func (o *options) logFile() string {
	if o.log.path == "off" || (o.diagnostic() && !flagSet("log")) {
		return ""
	}
	return o.log.path
}

// fullLog: everything goes to the file, not just errors.
func (o *options) fullLog() bool { return flagSet("log") && o.logFile() != "" }

func parseOptions() *options {
	o := &options{log: logFlag{path: defaultLogPath()}}
	flag.StringVar(&o.db, "db", defaultStorePath(), "file with remembered layouts")
	flag.StringVar(&o.def, "default", "system", "layout for new fields: system (from Windows settings), keep (leave as is) or a language, e.g. en-US")
	flag.DurationVar(&o.interval, "interval", 100*time.Millisecond, "how often to check the current layout")
	flag.BoolVar(&o.verbose, "v", false, "verbose log: field labels and window titles (personal data!)")
	flag.BoolVar(&o.chromium, "chromium", true, "enable accessibility in Chrome/Edge/VS Code etc. to tell apart fields inside pages")
	flag.BoolVar(&o.list, "list", false, "list installed layouts and exit")
	flag.BoolVar(&o.reset, "reset", false, "forget all remembered layouts and exit")
	flag.DurationVar(&o.watch, "watch", 0, "diagnostics: log focus events and layout changes without switching anything (e.g. 3m)")
	flag.DurationVar(&o.probe, "probe", 0, "diagnostics: print what has focus for the given duration (e.g. 30s)")
	flag.BoolVar(&o.version, "version", false, "print the version and exit")
	flag.Var(&o.log, "log", "full log to a file (up to 1 MB, then log.old.txt); -log alone means the default file. Without -log only errors go to the default file; -log=off disables the file")
	parseFlags(&o.log)

	if err := o.validate(); err != nil {
		fatalf("%v (see -h)", err)
	}
	if o.log.path != "off" {
		resolvePathFlag("log")
	}
	resolvePathFlag("db")
	return o
}

// Bounds for -interval: Windows timers don't go below ~10 ms, and checking
// less often than every few seconds would miss the user's switches.
const (
	minInterval = 10 * time.Millisecond
	maxInterval = 5 * time.Second
)

// validate checks values the flag package accepts but the app can't use.
func (o *options) validate() error {
	if o.interval < minInterval || o.interval > maxInterval {
		return fmt.Errorf("-interval %v is out of range %v to %v", o.interval, minInterval, maxInterval)
	}
	if o.watch < 0 || o.probe < 0 {
		return fmt.Errorf("-watch and -probe need a positive duration")
	}
	return nil
}

// flagSet reports whether a flag was given on the command line.
func flagSet(name string) bool {
	set := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// logFlag is the -log flag. It works bare (-log → the default file) and with
// a value (-log=path, -log path, -log=off). Go's flag package allows bare
// flags only for booleans, so it reports itself as one; a path given after
// a space is picked up from the remaining arguments (see parseFlags).
type logFlag struct {
	path string
	bare bool
}

func (l *logFlag) String() string {
	if l == nil {
		return ""
	}
	return l.path
}

func (l *logFlag) Set(v string) error {
	switch v {
	case "true":
		l.path, l.bare = defaultLogPath(), true
	case "false":
		l.path, l.bare = "off", false
	default:
		l.path, l.bare = v, false
	}
	return nil
}

func (l *logFlag) IsBoolFlag() bool { return true }

// parseFlags parses the command line. Errors are reported with fatalf, not
// by the flag package itself: it would print to stderr and exit, which the
// GUI build (no console) does silently.
func parseFlags(lf *logFlag) {
	fs := flag.CommandLine
	fs.Init(fs.Name(), flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported below, help is printed below
	err := parseArgs(fs, lf, os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fs.SetOutput(os.Stderr)
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", fs.Name())
		fs.PrintDefaults()
		os.Exit(0)
	}
	if err != nil {
		fatalf("%v (see -h)", err)
	}
}

// parseArgs parses args into fs, taking the argument after a bare -log as
// its path: "-log C:\x\log.txt -v" works like "-log=C:\x\log.txt -v".
func parseArgs(fs *flag.FlagSet, lf *logFlag, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	for lf.bare && fs.NArg() > 0 {
		lf.path, lf.bare = fs.Arg(0), false
		if err := fs.Parse(fs.Args()[1:]); err != nil {
			return err
		}
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %q", fs.Args())
	}
	return nil
}
