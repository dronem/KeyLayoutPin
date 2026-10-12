//go:build windows

package main

import (
	"runtime/debug"
	"strings"
)

// version can be set for a release: go build -ldflags "-X main.version=1.0.0".
// Otherwise it is taken from the git commit Go records at build time.
var version string

// versionString describes this build: the release version if set,
// otherwise the commit, e.g. "431a020 (2026-10-12)" or "431a020-dirty (…)"
// for a build with uncommitted changes; "dev" if nothing is known
// (go run, go test, building outside git).
func versionString() string {
	if version != "" {
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return describeVCS(info.Settings)
}

func describeVCS(settings []debug.BuildSetting) string {
	var rev, when string
	var dirty bool
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.time":
			when = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	v := rev[:min(7, len(rev))]
	if dirty {
		v += "-dirty"
	}
	if day, _, ok := strings.Cut(when, "T"); ok {
		v += " (" + day + ")"
	}
	return v
}
