package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
)

// claimFakeSSH answers an interactive session the way the k03 fake device
// does: the prompt, then each line echoed with "ok".
const claimFakeSSH = `#!/bin/sh
case " $* " in
  *" -O check "*) exit 1 ;;
  *" BatchMode=no "*)
    printf '127.0.0.1#'
    while IFS= read -r line; do
      [ "$line" = exit ] && exit 0
      printf '%s\r\nok\r\n127.0.0.1#' "$line"
    done
    exit 0 ;;
esac
printf 'ok\n'
`

// TestEveryActivityClaimsItsID: a cmd that writes files, a cmd under --nof,
// a dry run, and a cmd under watch.enabled false each take an ID of their
// own by a claim in the scoreboards folder (EXAMPLES chapter 68), and the
// cmd job's summary, records, and scoreboard name its ID as job_id.
func TestEveryActivityClaimsItsID(t *testing.T) {
	state := testdir.Short(t)
	for _, d := range []string{"home", "base", "sb", "cap", "spool", "tmp", "trust"} {
		if err := os.MkdirAll(filepath.Join(state, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	fake := filepath.Join(state, "fake-ssh")
	if err := os.WriteFile(fake, []byte(claimFakeSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	// The system transport locates karvi-askpass on PATH; a stub is enough
	// for the fake device.
	if err := os.WriteFile(filepath.Join(state, "karvi-askpass"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", state+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	base, sb := filepath.Join(state, "base"), filepath.Join(state, "sb")
	sets := []string{}
	for _, kv := range []string{
		fmt.Sprintf("basedir=%q", base), `sharedroot="none"`, fmt.Sprintf("scoreboards=%q", sb),
		fmt.Sprintf("spooldir=%q", filepath.Join(state, "spool")), fmt.Sprintf("tempdir=%q", filepath.Join(state, "tmp")),
		fmt.Sprintf("sessions.shared-capacity-root=%q", filepath.Join(state, "cap")),
		fmt.Sprintf("ssh.known-hosts-file=%q", filepath.Join(state, "trust", "known_hosts")),
		fmt.Sprintf("ssh.transports.system=%q", fake), "audit.journald-required=false",
		fmt.Sprintf("audit.file=%q", filepath.Join(state, "audit.jsonl")), "output.min-free-bytes-after-job=0", "display.color=never",
	} {
		sets = append(sets, "--set", kv)
	}
	run := func(args ...string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if got := Main(append(append([]string{}, sets...), args...), strings.NewReader(""), &stdout, &stderr); got != 0 {
			t.Fatalf("%v: exit=%d stdout=%q stderr=%q", args, got, stdout.String(), stderr.String())
		}
	}
	run("command", "--transport", "system", "127.0.0.1", "show", "clock")
	run("command", "--nof", "--transport", "system", "127.0.0.1", "show", "clock")
	run("run", "--dry-run", "--no-daemon", "--transport", "system", "--target", "127.0.0.1", "show", "clock")
	run("--set", "watch.enabled=false", "command", "--nof", "--transport", "system", "127.0.0.1", "show", "clock")

	// One claim per activity, each its own ID, in the order they ran.
	claims, err := filepath.Glob(filepath.Join(sb, "*"+osutil.ClaimExtension))
	if err != nil || len(claims) != 4 {
		t.Fatalf("claims %v %v, want four", claims, err)
	}
	sort.Strings(claims)
	ids := make([]string, len(claims))
	for i, c := range claims {
		ids[i] = strings.TrimSuffix(filepath.Base(c), osutil.ClaimExtension)
		if fi, err := os.Stat(c); err != nil || fi.Size() != 0 {
			t.Errorf("%s: %v %v", c, fi, err)
		}
	}
	files, nof, dry, unwatched := ids[0], ids[1], ids[2], ids[3]

	// The cmd that wrote files: its folder, summary, records, and
	// scoreboard all name its ID as job_id.
	dir := filepath.Join(base, "jobs", files[:len(osutil.DayFolderLayout)], files)
	jobID := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var v struct {
			JobID string `json:"job_id"`
		}
		if err := json.Unmarshal(data, &v); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		return v.JobID
	}
	for path, want := range map[string]string{
		filepath.Join(dir, "summary.json"): files,
		filepath.Join(sb, files+".json"):   files,
		filepath.Join(sb, nof+".json"):     nof,
	} {
		if got := jobID(path); got != want {
			t.Errorf("%s: job_id %q, want %s", path, got, want)
		}
	}
	f, err := os.Open(filepath.Join(dir, "commands.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	records := 0
	for scan := bufio.NewScanner(f); scan.Scan(); records++ {
		var r struct {
			JobID      string `json:"job_id"`
			ActivityID string `json:"activity_id"`
		}
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil || r.JobID != files || r.ActivityID != files {
			t.Errorf("record %s: %+v %v", scan.Bytes(), r, err)
		}
	}
	if records == 0 {
		t.Error("no command record")
	}

	// The others have no job folder; the dry run and the unwatched cmd have
	// no scoreboard either.
	for _, id := range []string{nof, dry, unwatched} {
		if _, err := os.Stat(filepath.Join(base, "jobs", id[:len(osutil.DayFolderLayout)], id)); !os.IsNotExist(err) {
			t.Errorf("%s has a job folder: %v", id, err)
		}
	}
	for _, id := range []string{dry, unwatched} {
		if _, err := os.Stat(filepath.Join(sb, id+".json")); !os.IsNotExist(err) {
			t.Errorf("%s has a scoreboard: %v", id, err)
		}
	}
}
