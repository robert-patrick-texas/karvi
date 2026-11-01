package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestDaemonRunReleasesItsJobDirectoryOnAClientPlanningFailure: a run
// through the daemon reserves its job directory before the draft, and a
// draft the client refuses (a daemon-authority target where the site's
// gate is closed) leaves no directory under output.root; the daemon is
// never reached.
func TestDaemonRunReleasesItsJobDirectoryOnAClientPlanningFailure(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "jobs")
	if err := os.Mkdir(filepath.Join(dir, "base"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{
		`basedir="` + filepath.Join(dir, "base") + `"`, `sharedroot="none"`, `output.root="` + root + `"`, `scoreboards="` + filepath.Join(dir, "sb") + `"`,
		`timezone="UTC"`, "name.allow-daemon-resolution=false", "audit.journald-required=false", `audit.file="` + filepath.Join(dir, "audit.jsonl") + `"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	opts := RunOptions{
		CommonOptions:      CommonOptions{Config: cfg, Operator: credentials.Operator{Username: "jsmith", UID: os.Getuid(), Home: dir}},
		Targets:            []records.TargetInput{{Kind: "target", Value: "127.0.0.1"}},
		AddressAuthorities: []string{"127.0.0.1=daemon"},
		Commands:           []string{"show clock"},
		Transport:          "system",
	}
	ensure := func(context.Context) error {
		t.Error("the daemon was reached after a client-planning failure")
		return nil
	}
	var stdout, stderr bytes.Buffer
	result := RunViaDaemon(context.Background(), opts, filepath.Join(dir, "d.sock"), 1<<20, ensure, IO{Stdout: &stdout, Stderr: &stderr})
	if !strings.HasPrefix(result.Error, "daemon_resolution_not_allowed: client planning: ") {
		t.Fatalf("result: exit=%d error=%q stderr=%q", result.ExitCode, result.Error, stderr.String())
	}
	left, _ := filepath.Glob(filepath.Join(root, "*", "*"))
	days, _ := filepath.Glob(filepath.Join(root, "*"))
	if len(left) != 0 || len(days) != 0 {
		t.Fatalf("the refused run left job directories %v and day folders %v", left, days)
	}
}
