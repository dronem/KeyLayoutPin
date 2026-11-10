//go:build windows

package main

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExpandPath(t *testing.T) {
	t.Setenv("CL_TEST_DIR", `C:\Some Dir`)
	cwd, _ := os.Getwd()
	cases := map[string]string{
		`%CL_TEST_DIR%\log.txt`:      `C:\Some Dir\log.txt`,
		`$env:CL_TEST_DIR\log.txt`:   `C:\Some Dir\log.txt`,
		`$Env:cl_test_dir\a\log.txt`: `C:\Some Dir\a\log.txt`,
		`log.txt`:                    filepath.Join(cwd, "log.txt"),
		``:                           ``,
	}
	for in, want := range cases {
		got, err := expandPath(in)
		if err != nil || got != want {
			t.Errorf("expandPath(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestLogFlagForms(t *testing.T) {
	cases := []struct {
		args    []string
		path    string
		verbose bool
	}{
		{[]string{"-log"}, defaultLogPath(), false},
		{[]string{"-log", "-v"}, defaultLogPath(), true},
		{[]string{"-log", `C:\x\l.txt`, "-v"}, `C:\x\l.txt`, true},
		{[]string{`-log=C:\y.txt`}, `C:\y.txt`, false},
		{[]string{"-log=off"}, "off", false},
		{[]string{"-log=false"}, "off", false},
		{[]string{"-v"}, defaultLogPath(), true},
		{nil, defaultLogPath(), false},
	}
	for _, c := range cases {
		fs, lf, v := testFlags()
		if err := parseArgs(fs, lf, c.args); err != nil {
			t.Errorf("%q: %v", c.args, err)
			continue
		}
		if lf.path != c.path || *v != c.verbose {
			t.Errorf("%q: path=%q v=%v, want path=%q v=%v", c.args, lf.path, *v, c.path, c.verbose)
		}
	}
}

func TestBadArgsAreErrors(t *testing.T) {
	for _, args := range [][]string{{"-nosuchflag"}, {"-v", "stray"}, {"-v=maybe"}} {
		fs, lf, _ := testFlags()
		if err := parseArgs(fs, lf, args); err == nil {
			t.Errorf("%q must be an error", args)
		}
	}
}

func testFlags() (*flag.FlagSet, *logFlag, *bool) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	lf := &logFlag{path: defaultLogPath()}
	fs.Var(lf, "log", "")
	return fs, lf, fs.Bool("v", false, "")
}

func TestValidateInterval(t *testing.T) {
	for _, c := range []struct {
		interval time.Duration
		ok       bool
	}{
		{100 * time.Millisecond, true},
		{minInterval, true},
		{maxInterval, true},
		{0, false},
		{-time.Second, false},
		{time.Minute, false},
	} {
		o := &options{interval: c.interval}
		if err := o.validate(); (err == nil) != c.ok {
			t.Errorf("-interval %v: err=%v, want ok=%v", c.interval, err, c.ok)
		}
	}
}

// Diagnostic modes print field names, so they write a file only when -log
// is given explicitly; the app itself always keeps an errors-only log.
func TestLogFileChoice(t *testing.T) {
	saved := flag.CommandLine
	defer func() { flag.CommandLine = saved }()
	cases := []struct {
		args []string
		file string
		full bool
	}{
		{nil, defaultLogPath(), false},
		{[]string{"-log"}, defaultLogPath(), true},
		{[]string{"-log=off"}, "", false},
		{[]string{"-watch=1m"}, "", false},
		{[]string{"-watch=1m", "-log", `C:\w.txt`}, `C:\w.txt`, true},
		{[]string{"-probe=5s", "-log"}, defaultLogPath(), true},
	}
	for _, c := range cases {
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		o := &options{log: logFlag{path: defaultLogPath()}}
		flag.Var(&o.log, "log", "")
		flag.DurationVar(&o.watch, "watch", 0, "")
		flag.DurationVar(&o.probe, "probe", 0, "")
		if err := parseArgs(flag.CommandLine, &o.log, c.args); err != nil {
			t.Fatalf("%q: %v", c.args, err)
		}
		if o.logFile() != c.file || o.fullLog() != c.full {
			t.Errorf("%q: file=%q full=%v, want file=%q full=%v", c.args, o.logFile(), o.fullLog(), c.file, c.full)
		}
	}
}
