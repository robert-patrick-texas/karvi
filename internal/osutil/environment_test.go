package osutil

import (
	"slices"
	"testing"
)

func TestChildEnvironmentKeepsOnlyTheAllowList(t *testing.T) {
	t.Setenv("HOME", "/h")
	t.Setenv("PATH", "/p")
	t.Setenv("USER", "u")
	t.Setenv("LOGNAME", "u")
	t.Setenv("TERM", "xterm")
	t.Setenv("NETPASS", "secret")
	t.Setenv("KARVI__DISPLAY__COLOR", "never")
	t.Setenv("SSH_AUTH_SOCK", "/s")
	got := ChildEnvironment([]string{"TERM", "SSH_AUTH_SOCK"}, "DISPLAY=karvi:0")
	for _, want := range []string{"HOME=/h", "PATH=/p", "USER=u", "LOGNAME=u", "TERM=xterm", "SSH_AUTH_SOCK=/s", "DISPLAY=karvi:0"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %s in %v", want, got)
		}
	}
	for _, entry := range got {
		if entry == "NETPASS=secret" || entry == "KARVI__DISPLAY__COLOR=never" {
			t.Errorf("leaked %s", entry)
		}
	}
	if len(got) != 7 {
		t.Errorf("got %d entries: %v", len(got), got)
	}
}
