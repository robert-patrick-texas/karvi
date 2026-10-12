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
	"github.com/robert-patrick-texas/karvi/internal/ipc"
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

// TestPrepareJobRefusesAnotherBuildsPreparation: a prepare answer from a
// daemon of another build than the client's is refused before any
// credential, daemon_incompatible naming both builds (EXAMPLES chapter 57,
// issue 3); from the client's own build it is accepted and names the
// daemon's build and commit.
func TestPrepareJobRefusesAnotherBuildsPreparation(t *testing.T) {
	f := newV5Fixture(t)
	own, err := buildinfo.Build()
	if err != nil {
		t.Fatal(err)
	}
	draft, header, cp, _ := f.draft(mustID(t), []string{"show clock"})
	defer cp.Destroy()
	saved := clientBuild
	clientBuild = func() (string, error) { return "XfTtoeK1qq9HkGakcVu7", nil }
	_, err = PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft})
	clientBuild = saved
	var other *OtherBuildError
	if !errors.As(err, &other) || errorcodes.Of(err) != "daemon_incompatible" || other.Daemon != own || other.Client != "XfTtoeK1qq9HkGakcVu7" {
		t.Fatalf("PrepareJob: %v, want daemon_incompatible from build %s", err, own)
	}
	if want := "the daemon that prepared the job is build " + own + ", this karvi build XfTtoeK1qq9HkGakcVu7; none submitted"; errorcodes.Message(err) != "daemon_incompatible: "+want {
		t.Fatalf("message %q, want %q", errorcodes.Message(err), want)
	}
	prepared, err := PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft})
	if err != nil {
		t.Fatal(err)
	}
	if d := prepared.Preparation.Daemon; d.Build != own || d.Commit != buildinfo.Commit {
		t.Fatalf("prepare answer's daemon block %+v, want build %s commit %s", d, own, buildinfo.Commit)
	}
}
