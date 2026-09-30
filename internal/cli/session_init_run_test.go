package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestRunNoDaemonRunsTheSessionInitProfile: run --no-daemon over a fake
// device with a
// fail-device profile whose first command times out. The plan's
// table reaches the executor; the records come profile first; the summary
// counts each kind apart; the device is listed for a rerun; run exits 101.
func TestRunNoDaemonRunsTheSessionInitProfile(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	log := filepath.Join(base, "device.log")
	device := filepath.Join(base, "fake-device")
	body := "#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\nprintf 'dev#'\nwhile IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >>\"" + log + "\"\n" +
		"  case \"$line\" in\n" +
		"    exit) exit 0;;\n" +
		"    'show slow') sleep 3; printf '%s\\r\\nslow\\r\\ndev#' \"$line\";;\n" +
		"    *) printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"  esac\ndone\n"
	if err := os.WriteFile(device, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	sets = append(sets,
		"--set", fmt.Sprintf("ssh.transports.system=%q", device),
		"--set", `ssh.host-key-policy="insecure"`,
		"--set", `execution.command-timeout="1s"`,
		"--set", `session-init.init.commands=["show slow", "show clock"]`,
		"--set", `session-init.init.on-error="fail-device"`,
		"--set", `session-init-map.0.profile="init"`,
		"--set", `session-init-map.0.name="*"`,
	)
	// The fake device never asks for a password; the helper only has to
	// exist.
	helper := filepath.Join(base, "karvi-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KARVI_ASKPASS_PATH", helper)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "run", "--no-daemon", "--format", "jsonl", "--target", "127.0.0.1", "--transport", "system", "--cmd", "show version")
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitPartialFailure {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	var got []string
	for _, line := range recordLines(stdout.String()) {
		var r records.CommandRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%v: %q", err, line)
		}
		got = append(got, fmt.Sprintf("%s:%d/%d:%s:%s", r.CommandKind, r.CommandIndex, r.CommandCount, r.Status, r.SessionInitProfile))
	}
	if want := "session_init:1/2:timeout:init session_init:2/2:not_attempted_prior_command_failure:init requested:1/1:not_attempted_session_init_failure:init"; strings.Join(got, " ") != want {
		t.Fatalf("records:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	data, _ := os.ReadFile(log)
	if sent := strings.Split(strings.TrimSpace(string(data)), "\n"); !strings.Contains(string(data), "show slow") || strings.Contains(string(data), "show clock") || strings.Contains(string(data), "show version") {
		t.Fatalf("device received %q", sent)
	}
	summaries, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "summary.json"))
	if len(summaries) != 1 {
		t.Fatalf("summaries %v", summaries)
	}
	var summary records.Summary
	raw, err := os.ReadFile(summaries[0])
	if err != nil || json.Unmarshal(raw, &summary) != nil {
		t.Fatalf("summary: %v", err)
	}
	if fmt.Sprint(summary.RequestedCommandCounts) != "map[not_attempted_session_init_failure:1]" || fmt.Sprint(summary.SessionInitCounts) != "map[not_attempted_prior_command_failure:1 timeout:1]" {
		t.Fatalf("counts requested=%v session_init=%v", summary.RequestedCommandCounts, summary.SessionInitCounts)
	}
	failed, err := os.ReadFile(filepath.Join(filepath.Dir(summaries[0]), "failed-devices.txt"))
	if err != nil || strings.TrimSpace(string(failed)) != "127.0.0.1" {
		t.Fatalf("failed-devices.txt %q %v", failed, err)
	}
}
