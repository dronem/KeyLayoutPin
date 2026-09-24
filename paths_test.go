//go:build windows

package main

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
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
	oldArgs, oldSet := os.Args, flag.CommandLine
	t.Cleanup(func() { os.Args, flag.CommandLine = oldArgs, oldSet })

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
		flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
		os.Args = append([]string{"keylayoutpin"}, c.args...)
		lf := &logFlag{path: defaultLogPath()}
		flag.Var(lf, "log", "")
		v := flag.Bool("v", false, "")
		parseFlags(lf)
		if lf.path != c.path || *v != c.verbose {
			t.Errorf("%q: path=%q v=%v, want path=%q v=%v", c.args, lf.path, *v, c.path, c.verbose)
		}
	}
}
