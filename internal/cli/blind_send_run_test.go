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

// promptingDeviceRun runs `run --no-daemon` over the system transport with
// the given command arguments against a stand-in device script that asks
// the fake's prompts: `clear counters` and `reload` a [confirm] taking one
// unechoed byte (after reload the device exits, as a reloading device drops
// the connection), and `copy running-config startup-config` the value
// prompt `Destination filename [startup-config]? `, whose answer is read a
// byte at a time since the session ends a response with a return and no
// newline. It returns the requested records' index/count:status words, the
// manifest, and the manifest's bytes.
func promptingDeviceRun(t *testing.T, commandArgs ...string) (string, records.Manifest, []byte) {
	t.Helper()
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	log := filepath.Join(base, "device.log")
	device := filepath.Join(base, "fake-device")
	body := "#!/bin/sh\ncase \" $* \" in *' -O '*) exit 1;; esac\nprintf 'dev#'\nwhile IFS= read -r line; do\n" +
		"  printf '%s\\n' \"$line\" >>\"" + log + "\"\n" +
		"  case \"$line\" in\n" +
		"    exit) exit 0;;\n" +
		"    'clear counters') printf '%s\\r\\nClear counters on all interfaces [confirm]' \"$line\"; dd bs=1 count=1 2>/dev/null >>\"" + log + "\"; printf '\\r\\ndev#';;\n" +
		"    'copy running-config startup-config') printf '%s\\r\\nDestination filename [startup-config]? ' \"$line\"; dd bs=1 count=1 2>/dev/null >>\"" + log + "\"; printf '\\r\\nBuilding configuration...\\r\\n[OK]\\r\\ndev#';;\n" +
		"    reload) printf '%s\\r\\nProceed with reload? [confirm]' \"$line\"; dd bs=1 count=1 2>/dev/null >>\"" + log + "\"; printf '\\r\\n'; exit 0;;\n" +
		"    *) printf '%s\\r\\nok\\r\\ndev#' \"$line\";;\n" +
		"  esac\ndone\n"
	if err := os.WriteFile(device, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	sets = append(sets,
		"--set", fmt.Sprintf("ssh.transports.system=%q", device),
		"--set", `ssh.host-key-policy="insecure"`,
		"--set", `execution.command-timeout="2s"`,
		"--set", `execution.blind-wait="2s"`,
	)
	helper := filepath.Join(base, "karvi-askpass")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KARVI_ASKPASS_PATH", helper)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	var stdout, stderr bytes.Buffer
	args := append(append([]string{}, sets...), "run", "--no-daemon", "--format", "jsonl", "--target", "127.0.0.1", "--transport", "system")
	args = append(args, commandArgs...)
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitSuccess {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	var got []string
	for _, line := range recordLines(stdout.String()) {
		var r records.CommandRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("%v: %q", err, line)
		}
		word := fmt.Sprintf("%d/%d:%s", r.CommandIndex, r.CommandCount, r.Status)
		for _, n := range r.Notices {
			word += "+" + n.Code
		}
		got = append(got, word)
	}
	manifests, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "manifest.json"))
	if len(manifests) != 1 {
		t.Fatalf("manifests %v", manifests)
	}
	var m records.Manifest
	raw, err := os.ReadFile(manifests[0])
	if err != nil || json.Unmarshal(raw, &m) != nil {
		t.Fatalf("manifest: %v", err)
	}
	return strings.Join(got, " "), m, []byte(compactJSON(t, raw))
}

// TestRunNoDaemonCarriesTheBlindSendToThePlan: a command ending in \r
// drafts a plan whose blind_returns
// count has its blind flag beside it, which the plan's Validate requires,
// and empty expectations; the plan reaches the manifest and the executor
// with both, and both commands succeed against the stand-in device that
// asks [confirm] and takes the typed-ahead return.
func TestRunNoDaemonCarriesTheBlindSendToThePlan(t *testing.T) {
	got, m, raw := promptingDeviceRun(t, "--cmd", `clear counters\r`, "--cmd", "show clock")
	if want := "1/2:succeeded 2/2:succeeded"; got != want {
		t.Fatalf("records:\n got %s\nwant %s", got, want)
	}
	if fmt.Sprint(m.Plan.Commands, m.Plan.BlindReturns, m.Plan.Blind, m.Plan.Expectations) != "[clear counters show clock] [1 0] [true false] []" {
		t.Fatalf("plan commands=%q blind_returns=%v blind=%v expectations=%v", m.Plan.Commands, m.Plan.BlindReturns, m.Plan.Blind, m.Plan.Expectations)
	}
	if !strings.Contains(string(raw), `"blind":[true,false],"expectations":[]`) {
		t.Fatalf("manifest lacks the two fields as written:\n%s", raw)
	}
}

// TestRunNoDaemonCarriesTheDeclarationsToThePlan: --expect and --blind,
// placed by the parser on their
// commands, draft a plan with the flag on the blind command and a zero count
// beside every flag (the counts and the flags travel together), and one
// expectation list per command; the
// plan reaches the manifest as written. Against the stand-in device the
// copy's value prompt is answered with the bare return and the prompt
// returns; the blind reload's [confirm] is answered, the device drops the
// connection, and the record is a success with the notice.
func TestRunNoDaemonCarriesTheDeclarationsToThePlan(t *testing.T) {
	got, m, raw := promptingDeviceRun(t, "--cmd", "copy running-config startup-config", "--expect", `filename \[startup-config\]\?=`, "--cmd", "reload", "--blind", "--expect", `confirm\]=`)
	if want := "1/2:succeeded 2/2:succeeded+prompt_not_observed_after_blind_send"; got != want {
		t.Fatalf("records:\n got %s\nwant %s", got, want)
	}
	if fmt.Sprint(m.Plan.Commands, m.Plan.BlindReturns, m.Plan.Blind, m.Plan.Expectations) != `[copy running-config startup-config reload] [0 0] [false true] [[{filename \[startup-config\]\? }] [{confirm\] }]]` {
		t.Fatalf("plan commands=%q blind_returns=%v blind=%v expectations=%v", m.Plan.Commands, m.Plan.BlindReturns, m.Plan.Blind, m.Plan.Expectations)
	}
	if !strings.Contains(string(raw), `"blind_returns":[0,0],`) || !strings.Contains(string(raw), `"blind":[false,true],"expectations":[[{"pattern":"filename \\[startup-config\\]\\?","response":""}],[{"pattern":"confirm\\]","response":""}]]`) {
		t.Fatalf("manifest lacks the fields as written:\n%s", raw)
	}
}
