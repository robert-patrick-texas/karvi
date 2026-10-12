package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// statusView is issue 2's example: a daemon of another build beside the
// client (EXAMPLES chapter 57).
var statusView = daemonStatusView{
	Status: "running", PID: 2267268, UID: 1000, Socket: "/home/netops/.local/share/karvi/socket/d.sock",
	DaemonVersion: "0.29.0", ClientVersion: "0.29.0",
	DaemonCommit: "karvi-v0.29.0-43-gdeac902", ClientCommit: "karvi-v0.29.0-43-gdeac902-dirty",
	DaemonIPCSchema: 11, ClientIPCSchema: 11,
	DaemonBuild: "9Au3WHU839F5xE-67H93", ClientBuild: "XfTtoeK1qq9HkGakcVu7",
	StartedAt:  time.Date(2026, 10, 12, 1, 34, 55, 610075038, time.FixedZone("", -4*3600)),
	ActiveJobs: 1, AcceptedJobs: 3,
}

// TestDaemonStatusStacksEachPair: the daemon's value over the client's, keys
// of one length per pair, compatible and the remediation after the last
// pair, and the lines without a client's value under their own names.
func TestDaemonStatusStacksEachPair(t *testing.T) {
	var out bytes.Buffer
	statusView.render(&out, display.LineStyle{}, "karvi daemon restart")
	want := `status: running
pid: 2267268
uid: 1000
socket: /home/netops/.local/share/karvi/socket/d.sock
daemon_version: 0.29.0
client_version: 0.29.0
daemon_commit: karvi-v0.29.0-43-gdeac902
client_commit: karvi-v0.29.0-43-gdeac902-dirty
daemon_ipc_schema: 11
client_ipc_schema: 11
daemon_build: 9Au3WHU839F5xE-67H93
client_build: XfTtoeK1qq9HkGakcVu7
compatible: false
remediation: karvi daemon restart
started_at: 2026-10-12T01:34:55.610075038-04:00
active_jobs: 1
accepted_jobs: 3
`
	if out.String() != want {
		t.Fatalf("status:\n%s\nwant:\n%s", out.String(), want)
	}
}

// TestDaemonStatusColoursEachPair: with colour on, a pair's two values are
// in the success colour when equal and in the error colour when not, and
// compatible's value alike; keys and the other lines are plain.
func TestDaemonStatusColoursEachPair(t *testing.T) {
	style := display.LineStyle{Enabled: true, Success: "green", Error: "red"}
	ok := func(v string) string { return display.ANSIStyle(v, "green", true, false) }
	bad := func(v string) string { return display.ANSIStyle(v, "red", true, false) }
	var out bytes.Buffer
	statusView.render(&out, style, "karvi daemon restart")
	for _, line := range []string{
		"status: running\n",
		"daemon_version: " + ok("0.29.0") + "\n", "client_version: " + ok("0.29.0") + "\n",
		"daemon_commit: " + bad("karvi-v0.29.0-43-gdeac902") + "\n", "client_commit: " + bad("karvi-v0.29.0-43-gdeac902-dirty") + "\n",
		"daemon_ipc_schema: " + ok("11") + "\n", "client_ipc_schema: " + ok("11") + "\n",
		"daemon_build: " + bad("9Au3WHU839F5xE-67H93") + "\n", "client_build: " + bad("XfTtoeK1qq9HkGakcVu7") + "\n",
		"compatible: " + bad("false") + "\n",
		"remediation: karvi daemon restart\n",
	} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("status %q missing %q", out.String(), line)
		}
	}
	same := statusView
	same.DaemonCommit, same.DaemonBuild, same.Compatible = same.ClientCommit, same.ClientBuild, true
	out.Reset()
	same.render(&out, style, "")
	for _, line := range []string{"daemon_build: " + ok("XfTtoeK1qq9HkGakcVu7") + "\n", "compatible: " + ok("true") + "\n"} {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("status %q missing %q", out.String(), line)
		}
	}
	if strings.Contains(out.String(), "remediation") {
		t.Fatalf("a compatible daemon's status names a remediation: %q", out.String())
	}
}

// TestDaemonStatusJSONTakesTheSameKeys: the JSON names each value as the
// text does, and no longer the daemon's version or schema alone.
func TestDaemonStatusJSONTakesTheSameKeys(t *testing.T) {
	raw, err := json.Marshal(statusView)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"status", "pid", "uid", "socket", "daemon_version", "client_version", "daemon_commit", "client_commit", "daemon_ipc_schema", "client_ipc_schema", "daemon_build", "client_build", "compatible", "started_at", "active_jobs", "accepted_jobs"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("JSON %s missing %q", raw, key)
		}
		delete(got, key)
	}
	if len(got) != 0 {
		t.Fatalf("JSON keys beyond the text's: %v", got)
	}
}

// TestEnsureDaemonRefusesWithoutItsOwnBuild: a client that cannot read its
// own build submits nothing through a daemon and launches none.
func TestEnsureDaemonRefusesWithoutItsOwnBuild(t *testing.T) {
	saved := ownBuild
	ownBuild = func() (string, error) {
		return "", errors.New("/proc/self/exe carries no Go build ID (.note.go.buildid)")
	}
	t.Cleanup(func() { ownBuild = saved })
	base, g, _ := daemonTestRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := ensureTestDaemon(ctx, g, &bytes.Buffer{})
	if errorcodes.Of(err) != "build_unreadable" || errorcodes.ExitAt(err, "daemon_start_failed") != exitcode.ExitDependencyError {
		t.Fatalf("err=%v, want build_unreadable", err)
	}
	if !strings.Contains(err.Error(), "--no-daemon") {
		t.Fatalf("err=%q names no --no-daemon", err)
	}
	if _, statErr := os.Stat(filepath.Join(base, "logs", "daemon.log")); !os.IsNotExist(statErr) {
		t.Fatalf("a daemon was launched; daemon.log stat=%v", statErr)
	}
}

// TestVersionNamesTheBuild: karvi version's build: line follows commit:, and
// the JSON names it.
func TestVersionNamesTheBuild(t *testing.T) {
	own := buildinfo.BuildOrNone()
	var stdout, stderr bytes.Buffer
	if got := Main([]string{"version"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("version exit=%d stderr=%q", got, stderr.String())
	}
	if want := "\ncommit: " + buildinfo.Commit + "\nbuild: " + own + "\n"; !strings.Contains(stdout.String(), want) {
		t.Fatalf("version %q missing %q", stdout.String(), want)
	}
	stdout.Reset()
	if got := Main([]string{"version", "--format", "json"}, strings.NewReader(""), &stdout, &stderr); got != 0 {
		t.Fatalf("version --format json exit=%d stderr=%q", got, stderr.String())
	}
	var info buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &info); err != nil || info.Build != own {
		t.Fatalf("version JSON build %q err=%v, want %q", info.Build, err, own)
	}
}
