//go:build windows

package main

import (
	"flag"
	"strings"
	"testing"
)

func TestHelpListsEveryFlag(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("db", "", "file with remembered layouts")
	fs.Bool("v", false, "verbose log")
	help := helpText(fs)
	for _, want := range []string{"How it works", "-db — file with remembered layouts", "-v — verbose log"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q:\n%s", want, help)
		}
	}
}

func TestAboutHasVersionAndLink(t *testing.T) {
	about := aboutText()
	if !strings.Contains(about, versionString()) || !strings.Contains(about, projectURL) {
		t.Errorf("about must show the version and the link:\n%s", about)
	}
}
