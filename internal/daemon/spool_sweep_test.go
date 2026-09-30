package daemon

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/output"
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
