//go:build windows

package main

import (
	"runtime/debug"
	"testing"
)

func TestDescribeVCS(t *testing.T) {
	s := func(kv ...string) []debug.BuildSetting {
		var out []debug.BuildSetting
		for i := 0; i < len(kv); i += 2 {
			out = append(out, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return out
	}
	cases := []struct {
		settings []debug.BuildSetting
		want     string
	}{
		{s("vcs.revision", "431a020b6c1f", "vcs.time", "2026-10-12T13:16:54Z", "vcs.modified", "false"), "431a020 (2026-10-12)"},
		{s("vcs.revision", "431a020b6c1f", "vcs.time", "2026-10-12T13:16:54Z", "vcs.modified", "true"), "431a020-dirty (2026-10-12)"},
		{s("vcs.revision", "abc"), "abc"},
		{nil, "dev"},
	}
	for _, c := range cases {
		if got := describeVCS(c.settings); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}

func TestReleaseVersionWins(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })
	version = "1.2.0"
	if got := versionString(); got != "1.2.0" {
		t.Fatalf("got %q", got)
	}
}
