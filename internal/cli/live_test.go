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
		"! 127.0.0.1 [127.0.0.1] platform=cisco_iosxe user=u backend=builtin-env-fallback transport=native\n\r\nRouter>",
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
