package daemon

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// TestServerSweepsSpoolsAtStart: the spool sweep's daemon half, a
// spool a dead process left under spooldir is removed at the daemon's start
// and logged as spool_abandoned_removed by path; a spooldir that cannot be
// resolved is logged and does not stop the daemon.
func TestServerSweepsSpoolsAtStart(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spool")
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`spooldir="` + dir + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Skip("no `true`:", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(dir, output.SpoolName("260927-101500-00", "dev", 1, child.Process.Pid))
	if err := os.WriteFile(gone, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	s := &Server{Config: cfg, Operator: credentials.Operator{UID: os.Geteuid(), Home: t.TempDir()}, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	s.sweepSpools()
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the abandoned spool is still there: %v", err)
	}
	if !strings.Contains(log.String(), "code=spool_abandoned_removed") || !strings.Contains(log.String(), gone) {
		t.Fatalf("log: %s", log.String())
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`spooldir="` + filepath.Join(file, "spool") + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	log.Reset()
	s.Config = cfg
	s.sweepSpools()
	if !strings.Contains(log.String(), "code=spool_directory_unavailable") {
		t.Fatalf("an unavailable directory is logged: %s", log.String())
	}
}

// TestServerSweepsControlSocketsAtStart: the control socket sweep's daemon
// half, a socket a master killed outright left in the control-path root is
// removed at the daemon's start and logged as
// control_socket_abandoned_removed by path; nothing is made.
func TestServerSweepsControlSocketsAtStart(t *testing.T) {
	root, base := testsocket.Dir(t), t.TempDir()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`basedir="` + base + `"`, `ssh.control-path-root="` + root + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(root, "0123456789abcdef")
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: gone, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	var log bytes.Buffer
	s := &Server{Config: cfg, Operator: credentials.Operator{UID: os.Geteuid(), Username: "u", Home: t.TempDir()}, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	s.sweepControlSockets()
	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Fatalf("the abandoned socket is still there: %v", err)
	}
	if !strings.Contains(log.String(), "code=control_socket_abandoned_removed") || !strings.Contains(log.String(), gone) {
		t.Fatalf("log: %s", log.String())
	}
	// A root not yet made holds no socket, and the sweep makes nothing:
	// neither the private root nor the control-path root.
	absent := filepath.Join(t.TempDir(), "base")
	if s.Config, err = configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`basedir="` + absent + `"`}}); err != nil {
		t.Fatal(err)
	}
	s.sweepControlSockets()
	if _, err := os.Lstat(absent); !os.IsNotExist(err) {
		t.Fatalf("the sweep made the private root: %v", err)
	}
}

// TestServerSweepsScratchAtStart: the scratch sweep's daemon half, a
// generated configuration a dead karvi left in the scratch is removed at the
// daemon's start and logged as scratch_abandoned_removed by path; a scratch
// not yet made is not made.
func TestServerSweepsScratchAtStart(t *testing.T) {
	scratch, base := testdir.Short(t), t.TempDir()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`basedir="` + base + `"`, `tempdir="` + scratch + `"`}})
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("true")
	if err := child.Run(); err != nil {
		t.Skip("no `true`:", err)
	}
	gone := filepath.Join(scratch, "karvi-ssh-"+strconv.Itoa(child.Process.Pid)+"-1.conf")
	if err := os.WriteFile(gone, []byte("Host *\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	s := &Server{Config: cfg, Operator: credentials.Operator{UID: os.Geteuid(), Username: "u", Home: t.TempDir()}, Logger: slog.New(slog.NewTextHandler(&log, nil))}
	s.sweepScratch()
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Fatalf("the abandoned configuration is still there: %v", err)
	}
	if !strings.Contains(log.String(), "code=scratch_abandoned_removed") || !strings.Contains(log.String(), gone) {
		t.Fatalf("log: %s", log.String())
	}
	absent := filepath.Join(t.TempDir(), "scratch")
	if s.Config, err = configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`basedir="` + base + `"`, `tempdir="` + absent + `"`}}); err != nil {
		t.Fatal(err)
	}
	s.sweepScratch()
	if _, err := os.Lstat(absent); !os.IsNotExist(err) {
		t.Fatalf("the sweep made the scratch: %v", err)
	}
}
