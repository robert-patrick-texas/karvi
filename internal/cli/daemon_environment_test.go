package cli

import (
	"bytes"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/osutil/osutiltest"
)

// TestMain lets the test binary serve as the daemon the launcher spawns:
// ensureDaemon starts os.Executable() with "daemon serve" last on the
// command line, and this binary then runs the real command.
func TestMain(m *testing.M) {
	// Off the host first (osutiltest.Isolate): no test consults the host's
	// shared roots, and every activity a test starts writes its scoreboard
	// under a directory of this run; the daemon this binary serves
	// inherits the variable and the
	// same guard, since it runs this TestMain too, and removes the scratch
	// root its guard made when it ends.
	done := osutiltest.Isolate()
	if n := len(os.Args); n >= 3 && os.Args[n-2] == "daemon" && os.Args[n-1] == "serve" {
		code := Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
		done()
		os.Exit(code)
	}
	code := m.Run()
	done()
	os.Exit(code)
}

// TestEnsureDaemonPassesOnlyTheAllowList: the
// real launcher spawns the daemon with a canary in this process's
// environment, and the child's process carries no canary while a KARVI__
// variable and an allow-listed name survive.
func TestEnsureDaemonPassesOnlyTheAllowList(t *testing.T) {
	base, g, socket := daemonTestRuntime(t)
	_ = base
	seed := canarytest.Seed(t)
	t.Setenv("NETPASS", seed.Raw)
	t.Setenv("NETENABLE", seed.Raw)
	t.Setenv("KARVI__DISPLAY__COLOR", "never")
	t.Setenv("TERM", "xterm-canary")
	t.Setenv("TZ", "UTC")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var stderr bytes.Buffer
	rt, err := ensureTestDaemon(ctx, g, &stderr)
	if err != nil {
		t.Fatalf("ensureDaemon: %v (stderr %q)", err, stderr.String())
	}
	defer func() {
		sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer scancel()
		_, _ = daemon.StopCompatible(sctx, socket, rt.MaxFrame, daemon.StopForce)
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(socket); os.IsNotExist(err) {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()
	probe, err := daemon.Probe(ctx, socket, rt.MaxFrame)
	if err != nil {
		t.Fatal(err)
	}
	pid := probe.Status.PID
	if pid <= 0 || pid == os.Getpid() {
		t.Fatalf("daemon pid %d", pid)
	}
	hits, err := canary.ScanProcess(pid, seed)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("the daemon process carries the canary: %v", hits)
	}
	environ, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, kv := range strings.Split(strings.TrimRight(string(environ), "\x00"), "\x00") {
		name, value, _ := strings.Cut(kv, "=")
		names[name] = value
	}
	for _, absent := range []string{"NETPASS", "NETENABLE"} {
		if _, ok := names[absent]; ok {
			t.Errorf("%s reached the daemon", absent)
		}
	}
	for name, want := range map[string]string{"KARVI__DISPLAY__COLOR": "never", "TERM": "xterm-canary", "TZ": "UTC"} {
		if names[name] != want {
			t.Errorf("%s=%q in the daemon, want %q", name, names[name], want)
		}
	}
	if _, ok := names["PATH"]; !ok {
		t.Error("PATH did not reach the daemon")
	}
	t.Logf("daemon pid %d: %d variables, no canary; KARVI__DISPLAY__COLOR, TERM, TZ, PATH present", pid, len(names))
}
