package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestLiveRefusesTheTextDisplaysOptions: beside --live, each option that
// shapes the text display it replaces is refused before any configuration
// loads.
func TestLiveRefusesTheTextDisplaysOptions(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"--format", "jsonl"}, "--live and --format jsonl are mutually exclusive"},
		{[]string{"--format", "json"}, "--live and --format json are mutually exclusive"},
		{[]string{"--echo"}, "--live and --echo are mutually exclusive"},
		{[]string{"--border"}, "--live and --border are mutually exclusive"},
		{[]string{"--noborder"}, "--live and --noborder are mutually exclusive"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			args := append([]string{"command", "--host", "router1", "--live"}, tc.args...)
			args = append(args, "--cmd", "show clock")
			var stdout, stderr bytes.Buffer
			got := Main(args, strings.NewReader(""), &stdout, &stderr)
			if got != exitcode.ExitUsageError || stderr.String() != "live_option_conflict: "+tc.want+"\n" || stdout.Len() != 0 {
				t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
			}
		})
	}
}

// lockedBuffer is standard output written by the job while the test reads
// it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// TestCommandLiveShowsTheSessionAsItArrives: command --live over the native
// transport prints the header, then the device's bytes from the first (the
// enable step with the echoed secret removed, the paging commands, the
// command) while the command runs, each once, and the footer; the job's
// files are those of the same command without it.
func TestCommandLiveShowsTheSessionAsItArrives(t *testing.T) {
	const enable = "Zq7-cli-SECRET"
	gate := make(chan struct{})
	srv, err := fakedevice.Start(fakedevice.Options{Username: "u", Password: "p", Enable: enable, EchoSecret: true, DripGate: gate})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	base, _, _ := daemonTestRuntime(t)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv("NETENABLE", enable)
	args := append(dryRunSets(t, base), "command", "--live", "--pi", "--transport", "native", "--port", fmt.Sprint(srv.Port()), "--host", "127.0.0.1", "--cmd", "show drip")
	var stdout lockedBuffer
	var stderr bytes.Buffer
	seen := make(chan bool, 1)
	go func() {
		ok := false
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && !ok; time.Sleep(10 * time.Millisecond) {
			ok = strings.Contains(stdout.String(), "drip 1")
		}
		seen <- ok
		gate <- struct{}{}
		gate <- struct{}{}
	}()
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitSuccess {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !<-seen {
		t.Fatalf("drip 1 was not shown while the command ran: %q", out)
	}
	for _, want := range []string{
		"! 127.0.0.1 [127.0.0.1] platform=cisco_ios user=u backend=builtin-env-fallback transport=native\n\r\nRouter>",
		"enable\r\nPassword: <removed>",
		"terminal length 0\r\n",
		"show drip\r\ndrip 1\r\ndrip 2\r\ndrip 3\r\nRouter#",
		"\n! exit=0 elapsed=",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, enable) || strings.Count(out, "drip 3") != 1 || strings.Count(out, "! 127.0.0.1") != 1 {
		t.Fatalf("stdout:\n%s", out)
	}
	files, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "commands.jsonl"))
	if len(files) != 1 {
		t.Fatalf("job files %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var rec records.CommandRecord
	if err := json.Unmarshal(bytes.TrimSpace(data), &rec); err != nil || rec.Output != "drip 1\ndrip 2\ndrip 3\n" || rec.Command != "show drip" {
		t.Fatalf("record %+v, %v", rec, err)
	}
}

// TestLiveRunRefusals: a run's --live is refused without a display in this
// process, and beside a dispatch other than serial, on the line or from
// --set.
func TestLiveRunRefusals(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	setIndex := len(sets)/2 + 1
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--live"}, "--live without --no-daemon; add --no-daemon"},
		{[]string{"run", "--no-daemon", "--live", "--dry-run"}, "--live and --dry-run are mutually exclusive"},
		{[]string{"run", "--live", "--exercise"}, "--live and --exercise are mutually exclusive"},
		{[]string{"run", "--no-daemon", "--live", "--follow=false"}, "--live and --follow=false are mutually exclusive"},
		{[]string{"run", "--no-daemon", "--live", "--dp"}, "--live and --dispatch parallel are mutually exclusive"},
		{[]string{"crun", "--no-daemon", "--live", "--dispatch", "wave"}, "--live and --dispatch wave are mutually exclusive"},
		{[]string{"crun", "--no-daemon", "--live", "--format", "json"}, "--live and --format json are mutually exclusive"},
		{[]string{"--set", `dispatch.default="parallel"`, "run", "--no-daemon", "--live"}, fmt.Sprintf(`--live and dispatch.default = "parallel" at --set[%d] are mutually exclusive`, setIndex)},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			args := append(append(append([]string{}, sets...), tc.args...), "--target", "router1", "--cmd", "show clock")
			var stdout, stderr bytes.Buffer
			got := Main(args, strings.NewReader(""), &stdout, &stderr)
			if got != exitcode.ExitUsageError || stderr.String() != "live_option_conflict: "+tc.want+"\n" || stdout.Len() != 0 {
				t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
			}
		})
	}
}

// TestLiveDispatchFromTheConfiguration: serial needs nothing; another mode
// from a file is set to serial; from --set it is refused.
func TestLiveDispatchFromTheConfiguration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "karvi.toml")
	if err := os.WriteFile(file, []byte("[dispatch]\ndefault = \"wave\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		opts  configload.Options
		set   bool
		error string
	}{
		{name: "built-in serial", opts: configload.Options{InternalOnly: true}},
		{name: "a file's wave", opts: configload.Options{SkipAuto: true, ExplicitRoots: []string{file}}, set: true},
		{name: "--set parallel", opts: configload.Options{InternalOnly: true, Sets: []string{`dispatch.default="parallel"`}}, error: `--live and dispatch.default = "parallel" at --set[1] are mutually exclusive`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.opts.Environment = []string{}
			cfg, err := configload.Load(tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			set, err := liveDispatch(cfg)
			if set != tc.set || (err == nil) != (tc.error == "") || (err != nil && (errorcodes.Of(err) != "live_option_conflict" || err.Error() != "live_option_conflict: "+tc.error)) {
				t.Fatalf("set=%t err=%v", set, err)
			}
		})
	}
}

// TestRunLiveFollowsEachDeviceInTurn: run --no-daemon --live under a
// configuration whose dispatch is parallel runs serial, each device's
// header and then its bytes as they arrive, the second device after the
// first's session has ended, and the footer once.
func TestRunLiveFollowsEachDeviceInTurn(t *testing.T) {
	gate := make(chan struct{})
	srv, err := fakedevice.Start(fakedevice.Options{Username: "u", Password: "p", StartPrivileged: true, DripGate: gate})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	base, _, _ := daemonTestRuntime(t)
	inventory := filepath.Join(base, "inv.csv")
	config := filepath.Join(base, "karvi.toml")
	if err := os.WriteFile(inventory, []byte("name,management_address,platform\nrouter1,127.0.0.1,cisco_ios\nrouter2,127.0.0.1,cisco_ios\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	toml := fmt.Sprintf("[dispatch]\ndefault = \"parallel\"\n[platform.cisco_ios]\nssh-port = %d\n[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = %q\nrequired = true\nmode = \"header\"\ndelimiter = \",\"\nmandatory-fields = [\"name\", \"platform\"]\nname-transform = \"default\"\n[inventory-source.mappings]\nname = [\"name\"]\nmanagement_address = [\"management_address\"]\nplatform = [\"platform\"]\n", srv.Port(), inventory)
	if err := os.WriteFile(config, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	args := append(append([]string{"--config", config}, dryRunSets(t, base)...), "run", "--no-daemon", "--live", "--transport", "native", "--target", "router1", "--target", "router2", "--cmd", "show drip")
	var stdout lockedBuffer
	var stderr bytes.Buffer
	seen := make(chan bool, 1)
	go func() {
		ok := true
		for n := 1; n <= 2 && ok; n++ {
			ok = false
			for end := time.Now().Add(10 * time.Second); time.Now().Before(end) && !ok; time.Sleep(10 * time.Millisecond) {
				ok = strings.Count(stdout.String(), "drip 1") == n
			}
			gate <- struct{}{}
			gate <- struct{}{}
		}
		seen <- ok
	}()
	if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitSuccess {
		t.Fatalf("exit=%d stderr=%q stdout=%q", got, stderr.String(), stdout.String())
	}
	out := stdout.String()
	if !<-seen {
		t.Fatalf("each device's drip 1 was not shown while its command ran: %q", out)
	}
	session := "Router#terminal length 0\r\nRouter#terminal width 512\r\nRouter#show drip\r\ndrip 1\r\ndrip 2\r\ndrip 3\r\nRouter#exit\r\n"
	header := func(device string) string {
		return "! " + device + " [127.0.0.1] platform=cisco_ios user=u backend=builtin-env-fallback transport=native\n\r\n"
	}
	if !strings.HasPrefix(out, header("router1")+session+header("router2")+session+"! exit=0 elapsed=") || strings.Count(out, "! exit=") != 1 {
		t.Fatalf("stdout:\n%q", out)
	}
	files, _ := filepath.Glob(filepath.Join(base, "jobs", "*", "*", "commands.jsonl"))
	if len(files) != 1 {
		t.Fatalf("job files %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec records.CommandRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.Dispatch.Mode != "serial" || rec.Output != "drip 1\ndrip 2\ndrip 3\n" {
			t.Fatalf("record %+v, %v", rec, err)
		}
	}
}
