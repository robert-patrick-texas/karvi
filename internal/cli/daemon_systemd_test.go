package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/records"
)

// systemdSet is the setting under which the account's daemon is the unit's
// (EXAMPLES chapter 57, issue 15).
const systemdSet = `daemon.launch="systemd"`

// systemdTestRuntime is daemonTestRuntime under daemon.launch = "systemd",
// with the unit its runtime names.
func systemdTestRuntime(t *testing.T) (string, globalOptions, string, string) {
	t.Helper()
	base, g, socket := daemonTestRuntime(t)
	g.sets = append(g.sets, systemdSet)
	rt, err := testRuntime(g)
	if err != nil {
		t.Fatal(err)
	}
	return base, g, socket, rt.Unit()
}

// TestSystemdLaunchEnsureDaemon (EXAMPLES chapter 57, issue 15): under
// daemon.launch = "systemd" a daemon of this build, not draining, is used;
// a draining daemon, one of another build, one of a newer schema, and none
// refuse the submission, naming the unit's systemctl; nothing is drained,
// stopped, or launched.
func TestSystemdLaunchEnsureDaemon(t *testing.T) {
	own := buildinfo.BuildOrNone()
	for _, tc := range []struct {
		name     string
		build    string
		newer    bool
		none     bool
		draining bool
		jobs     int64
		code     string
		exit     int
		want     string
	}{
		{name: "this build", build: own},
		{name: "this build, draining", build: own, draining: true, jobs: 1, code: "daemon_draining", exit: exitcode.ExitJobRejected,
			want: fmt.Sprintf("daemon_draining: 1 job running; none submitted; retry when it exits, or --no-daemon\n  daemon build %s %s\n  client build %[1]s %[2]s", own, buildinfo.Version)},
		{name: "another build", build: fixtureBuild, code: "daemon_incompatible", exit: exitcode.ExitJobRejected,
			want: fmt.Sprintf("daemon_incompatible: the daemon runs another build; none submitted; systemctl restart %%s, or --no-daemon\n  daemon build %s %s\n  client build %s %[2]s", fixtureBuild, buildinfo.Version, own)},
		{name: "a newer schema", newer: true, code: "daemon_incompatible", exit: exitcode.ExitJobRejected,
			want: fmt.Sprintf("daemon_incompatible: the daemon speaks a newer IPC schema; none submitted; systemctl restart %%s, or --no-daemon\n  daemon ipc_schema %d\n  client ipc_schema %d", ipc.SchemaVersion+1, ipc.SchemaVersion)},
		{name: "none", none: true, code: "daemon_unreachable", exit: exitcode.ExitConnectionFailure,
			want: "daemon_unreachable: no daemon running; none submitted; systemctl start %s, or --no-daemon"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, g, socket, unit := systemdTestRuntime(t)
			var f *stopFixture
			var done <-chan struct{}
			switch {
			case tc.newer:
				done = startCLILifecycleFixture(t, socket, ipc.SchemaVersion+1, "0.31.0", 0)
			case !tc.none:
				f = startStopFixtureBuild(t, socket, tc.jobs, tc.build, nil)
				f.draining.Store(tc.draining)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var stderr bytes.Buffer
			_, err := ensureTestDaemon(ctx, g, &stderr)
			if tc.code == "" {
				if err != nil {
					t.Fatalf("ensureDaemon: %v", err)
				}
			} else {
				want := strings.Replace(tc.want, "%s", unit, 1)
				if got := errorcodes.Message(err); got != want || errorcodes.ExitAt(err, "daemon_start_failed") != tc.exit {
					t.Fatalf("refusal=%q exit=%d, want %q exit %d", got, errorcodes.ExitAt(err, "daemon_start_failed"), want, tc.exit)
				}
			}
			if stderr.Len() != 0 {
				t.Fatalf("stderr=%q", stderr.String())
			}
			if f != nil {
				if ops := f.operations(); ops != "" {
					t.Fatalf("the client reached the unit's daemon: %q", ops)
				}
			}
			if done != nil {
				select {
				case <-done:
					t.Fatal("the newer daemon was stopped")
				default:
				}
			}
			if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
				t.Fatalf("a daemon was launched; daemon.log stat=%v", statErr)
			}
		})
	}
}

// TestSystemdLaunchDaemonWords: under daemon.launch = "systemd" daemon stop
// and daemon restart, with an option or none, are refused daemon_managed,
// naming the unit's systemctl, the daemon untouched; daemon start finding
// none is refused naming systemctl start; a submission finding none is
// refused alike; and status's remediation names the unit's restart.
func TestSystemdLaunchDaemonWords(t *testing.T) {
	main := func(sets []string, args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		got := Main(append(append([]string{}, sets...), args...), strings.NewReader(""), &stdout, &stderr)
		return got, stdout.String(), stderr.String()
	}
	t.Run("stop and restart", func(t *testing.T) {
		_, g, socket, unit := systemdTestRuntime(t)
		f := startStopFixture(t, socket, 1, nil)
		sets := []string{"--set", g.sets[0], "--set", systemdSet}
		for _, args := range [][]string{{"stop"}, {"stop", "--force"}, {"stop", "--grace"}, {"restart"}, {"restart", "--after=5s"}} {
			got, stdout, stderr := main(sets, append([]string{"daemon"}, args...)...)
			want := fmt.Sprintf("daemon_managed: daemon.launch is systemd; systemctl %s %s\n", args[0], unit)
			if got != exitcode.ExitJobRejected || stderr != want || stdout != "" {
				t.Fatalf("daemon %v: exit=%d stdout=%q stderr=%q, want %q", args, got, stdout, stderr, want)
			}
		}
		if ops := f.operations(); ops != "" {
			t.Fatalf("the client reached the unit's daemon: %q", ops)
		}
	})
	t.Run("start and a submission, none running", func(t *testing.T) {
		base, _, socket, unit := systemdTestRuntime(t)
		sets := append(dryRunSets(t, base), "--set", systemdSet)
		t.Setenv("NETUSER", "u")
		t.Setenv("NETPASS", "p")
		want := fmt.Sprintf("daemon_unreachable: no daemon running; none submitted; systemctl start %s, or --no-daemon\n", unit)
		for _, args := range [][]string{{"daemon", "start"}, {"run", "--target", "127.0.0.1", "--transport", "system", "show", "clock"}} {
			got, _, stderr := main(sets, args...)
			if got != exitcode.ExitConnectionFailure || !strings.HasSuffix(stderr, want) {
				t.Fatalf("%v: exit=%d stderr=%q, want %q", args, got, stderr, want)
			}
		}
		if _, err := os.Stat(socket); !os.IsNotExist(err) {
			t.Fatalf("a daemon was launched; socket stat=%v", err)
		}
		if _, err := os.Stat(filepath.Join(base, "fake-ssh.log")); !os.IsNotExist(err) {
			t.Fatalf("the transport ran; stat=%v", err)
		}
	})
	t.Run("status beside another build", func(t *testing.T) {
		_, g, socket, unit := systemdTestRuntime(t)
		startStopFixtureBuild(t, socket, 0, fixtureBuild, nil)
		got, stdout, stderr := main([]string{"--set", g.sets[0], "--set", systemdSet}, "daemon", "status")
		if want := "compatible: false\nremediation: systemctl restart " + unit + "\n"; got != 0 || !strings.Contains(stdout, want) {
			t.Fatalf("exit=%d stdout=%q stderr=%q, missing %q", got, stdout, stderr, want)
		}
	})
}

// TestRunDryRunUnderSystemd (EXAMPLES chapter 57, issue 15): the dry run
// says what a live run under daemon.launch = "systemd" would do, every
// refusal an error finding naming the unit's systemctl: none answering,
// another build, a newer schema, and a daemon draining with no job.
func TestRunDryRunUnderSystemd(t *testing.T) {
	own := buildinfo.BuildOrNone()
	for _, tc := range []struct {
		name         string
		none, newer  bool
		build        string
		draining     bool
		status, code string
		exit         int
		message      string
	}{
		{name: "none", none: true, status: executionplan.DaemonAbsent, code: "daemon_unreachable", exit: exitcode.ExitConnectionFailure,
			message: "no daemon answers at the socket; a live run would be refused; systemctl start %s"},
		{name: "another build", build: fixtureBuild, status: executionplan.DaemonIncompatible, code: "daemon_incompatible", exit: exitcode.ExitJobRejected,
			message: "the daemon is another build's; a live run would be refused; systemctl restart %s"},
		{name: "a newer schema", newer: true, status: executionplan.DaemonIncompatible, code: "daemon_incompatible", exit: exitcode.ExitJobRejected,
			message: fmt.Sprintf("the daemon speaks a newer IPC schema, %d, than this client's %d; a live run would be refused; systemctl restart %%s", ipc.SchemaVersion+1, ipc.SchemaVersion)},
		{name: "this build draining, no job", build: own, draining: true, status: executionplan.DaemonDraining, code: "daemon_draining", exit: exitcode.ExitJobRejected,
			message: "the daemon is draining, 0 jobs running; a live run would be refused until it exits"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, _, socket, unit := systemdTestRuntime(t)
			sets := append(dryRunSets(t, base), "--set", systemdSet)
			t.Setenv("NETUSER", "u")
			t.Setenv("NETPASS", "p")
			var f *stopFixture
			switch {
			case tc.newer:
				startCLILifecycleFixture(t, socket, ipc.SchemaVersion+1, "0.31.0", 0)
			case !tc.none:
				f = startStopFixtureBuild(t, socket, 0, tc.build, nil)
				f.draining.Store(tc.draining)
			}
			message := strings.Replace(tc.message, "%s", unit, 1)
			var stdout, stderr bytes.Buffer
			got := Main(dryRunArgs(sets, "--format", "json"), strings.NewReader(""), &stdout, &stderr)
			if got != tc.exit || stderr.String() != tc.code+": "+message+"\n" {
				t.Fatalf("exit=%d stderr=%q, want %d %q", got, stderr.String(), tc.exit, message)
			}
			var report records.PlanReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			var finding *executionplan.Finding
			for i := range report.Findings {
				if report.Findings[i].Stage == "daemon_probe" {
					finding = &report.Findings[i]
				}
			}
			if report.Outcome != records.OutcomeInvalid || report.Daemons[0].Status != tc.status || finding == nil || finding.Severity != executionplan.SeverityError || finding.Code != tc.code {
				t.Fatalf("report: outcome=%s daemon=%+v finding=%+v", report.Outcome, report.Daemons[0], finding)
			}
			if f != nil {
				if ops := f.operations(); ops != "" {
					t.Fatalf("the dry run reached the daemon: %q", ops)
				}
			}
			if tc.none {
				if _, err := os.Stat(socket); !os.IsNotExist(err) {
					t.Fatalf("a daemon was launched; socket stat=%v", err)
				}
			}
		})
	}
}

// The unit is the client's own account's instance of the template.
func TestSystemdUnitNamesTheAccount(t *testing.T) {
	rt := app.DaemonRuntime{}
	rt.Operator.Username = "karvi"
	if got := rt.Unit(); got != "karvi-daemon@karvi.service" {
		t.Fatalf("unit %q", got)
	}
}
