package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// TestStatusNamesTheDaemonsBuildAndCommit: the ping and status answer names
// the daemon's build and commit (EXAMPLES chapter 57, issue 2), and a client
// of the same program finds it compatible.
func TestStatusNamesTheDaemonsBuildAndCommit(t *testing.T) {
	_, socket, _ := startTestServer(t)
	own, err := buildinfo.Build()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	probe, err := Probe(ctx, socket, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if probe.Status.Build != own || probe.Status.Commit != buildinfo.Commit || !probe.Compatible {
		t.Fatalf("probe=%+v, want build %s commit %s compatible", probe, own, buildinfo.Commit)
	}
}

// TestServeRefusesWithoutItsOwnBuild: a daemon that cannot read its build
// does not start, build_unreadable, and binds nothing.
func TestServeRefusesWithoutItsOwnBuild(t *testing.T) {
	saved := ownBuild
	ownBuild = func() (string, error) { return "", errors.New("no .note.go.buildid") }
	t.Cleanup(func() { ownBuild = saved })
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "d.sock")
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid()}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.Serve(ctx)
	if errorcodes.Of(err) != "build_unreadable" {
		t.Fatalf("Serve: %v, want build_unreadable", err)
	}
	if _, statErr := os.Stat(socket); !os.IsNotExist(statErr) {
		t.Fatalf("socket stat=%v, want none", statErr)
	}
	if _, statErr := os.Stat(s.StatePath); !os.IsNotExist(statErr) {
		t.Fatalf("state stat=%v, want none", statErr)
	}
}
