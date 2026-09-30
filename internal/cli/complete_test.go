package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestCompleteReachesTheTable keeps completion and the parser table in
// step: every visible command word is offered at
// the command position, every visible subcommand word after its command, and
// every built option of every command at its option position, so a word
// added to the table is completed by construction.
func TestCompleteReachesTheTable(t *testing.T) {
	has := func(cands []string, want string) bool {
		for _, c := range cands {
			if c == want {
				return true
			}
		}
		return false
	}
	top, _ := complete(nil, "")
	for _, o := range globalOptionTable {
		if !has(top, "--"+o.name) {
			t.Errorf("global --%s is not offered before the command", o.name)
		}
	}
	for _, c := range commandTable {
		if c.hidden {
			continue
		}
		if !has(top, c.word) {
			t.Errorf("command %s is not offered", c.word)
		}
		cmds := []*command{c}
		if c.subs != nil {
			subs, _ := complete([]string{c.word}, "")
			for _, s := range c.subs {
				if s.hidden {
					continue
				}
				if !has(subs, s.word) {
					t.Errorf("%s is not offered after %s", s.path, c.word)
				}
				cmds = append(cmds, s)
			}
		}
		for _, x := range cmds {
			line := strings.Fields(x.path)
			if x.subs != nil && x != c {
				continue
			}
			cands, _ := complete(line, "--")
			for _, o := range x.options {
				if o.pending {
					continue
				}
				if !has(cands, "--"+o.name) {
					t.Errorf("%s: --%s is not offered", x.path, o.name)
				}
			}
		}
	}
}

// completionLab writes a configuration with a two-device inventory and a
// scoreboard directory holding a running and a finished job, and returns the
// --config argument the line carries.
func completionLab(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	inv := filepath.Join(dir, "inv.csv")
	if err := os.WriteFile(inv, []byte("name,management_address,platform\ncore-nyc-01,192.0.2.1,cisco_iosxe\nedge-sfo-02,192.0.2.2,generic\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	score := filepath.Join(dir, "score")
	if err := os.MkdirAll(score, 0o700); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for id, status := range map[string]string{"260925-101010-00": "running", "260925-090909-00": "completed"} {
		snap := records.ScoreboardSnapshot{SchemaVersion: 2, ActivityID: id, JobID: id, Mode: "run", Status: status, StartedAt: now, LastUpdatedAt: now}
		data, _ := json.Marshal(snap)
		if err := os.WriteFile(filepath.Join(score, id+".json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(dir, "karvi.toml")
	text := "basedir = \"" + filepath.Join(dir, "base") + "\"\nsharedroot = \"none\"\n[watch]\ndirectory = \"" + score + "\"\n[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \"" + inv + "\"\nrequired = true\nmode = \"header\"\ndelimiter = \",\"\nmandatory-fields = [\"name\", \"platform\"]\n[inventory-source.mappings]\nname = [\"name\"]\nmanagement_address = [\"management_address\"]\nplatform = [\"platform\"]\n[platform.lab_switch]\ndriver = \"cisco_iosxe\"\n"
	if err := os.WriteFile(cfg, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestCompleteCases covers each kind of candidate and the boundaries: the
// command position with a prefix, an ambiguous and an unknown command, a
// subcommand, an option prefix, an enum value, --platform's names with the
// site's table, --set's keys with =, a device name for --target and for
// command's device, login's device, job follow's and job cancel's IDs, a
// path option's directive, the inline --opt=prefix form, the options still
// offered after command's device (the parser reads them until text begins),
// and nothing once device text has begun or a positional is taken.
func TestCompleteCases(t *testing.T) {
	cfg := completionLab(t)
	// The test binary's Isolate points KARVI__WATCH__DIRECTORY at a directory
	// of the run, which beats the file; the line's --set beats both.
	g := []string{"--config", cfg, "--set", "watch.directory=\"" + filepath.Join(filepath.Dir(cfg), "score") + "\""}
	for _, tc := range []struct {
		name    string
		line    []string
		current string
		want    []string // every one present
		absent  []string // none present
		files   bool
		exact   []string // the whole list, when set
	}{
		{name: "command prefix", line: nil, current: "c", exact: []string{"command", "config", "crun"}},
		{name: "global option", line: nil, current: "--se", exact: []string{"--set"}},
		{name: "unknown command", line: []string{"nosuch"}, current: "", exact: nil},
		{name: "ambiguous command", line: []string{"c"}, current: "", exact: nil},
		{name: "subcommand", line: []string{"daemon"}, current: "st", exact: []string{"start", "status", "stop"}},
		{name: "subcommand by prefix", line: []string{"dae"}, current: "", want: []string{"serve", "--help"}},
		{name: "option prefix", line: []string{"run"}, current: "--tar", exact: []string{"--target"}},
		{name: "alias not offered", line: []string{"run"}, current: "--hos", exact: nil},
		{name: "pending option not offered", line: []string{"login"}, current: "--ssh-o", exact: nil},
		{name: "enum value", line: []string{"run", "--order"}, current: "", exact: []string{"default", "random", "shuffle", "sorted"}},
		{name: "enum value prefix", line: []string{"watch", "--format"}, current: "t", exact: []string{"table", "tui"}},
		{name: "platform names", line: append(g, "run", "--platform"), current: "", want: []string{"cisco_iosxe", "generic", "lab_switch"}},
		{name: "set keys", line: []string{"--set"}, current: "output.ro", exact: []string{"output.root="}},
		{name: "set keys inline", line: nil, current: "--set=output.ro", exact: []string{"--set=output.root="}},
		{name: "target device", line: append(g, "run", "--target"), current: "", exact: []string{"core-nyc-01", "edge-sfo-02"}},
		{name: "target device prefix", line: append(g, "run", "--t"), current: "e", exact: []string{"edge-sfo-02"}},
		{name: "command's device", line: append(g, "cmd"), current: "c", want: []string{"core-nyc-01"}, absent: []string{"edge-sfo-02"}},
		{name: "command's device given", line: append(g, "cmd", "core-nyc-01"), current: "", want: []string{"--echo"}, absent: []string{"core-nyc-01", "edge-sfo-02"}},
		{name: "run's text begun", line: append(g, "run", "--target", "core-nyc-01", "show"), current: "", exact: nil},
		{name: "run before text", line: append(g, "run", "--target", "core-nyc-01"), current: "--e", exact: []string{"--echo", "--exclude", "--exercise", "--expect"}},
		{name: "after --", line: append(g, "run", "--"), current: "", exact: nil},
		{name: "command's device after --", line: append(g, "cmd", "--"), current: "", exact: []string{"core-nyc-01", "edge-sfo-02"}},
		{name: "login device", line: append(g, "login"), current: "core", exact: []string{"core-nyc-01"}},
		{name: "job follow ids", line: append(g, "job", "follow"), current: "", want: []string{"260925-101010-00", "260925-090909-00", "--format"}},
		{name: "job cancel ids", line: append(g, "job", "cancel"), current: "2609", exact: []string{"260925-090909-00", "260925-101010-00"}},
		{name: "job follow id taken", line: append(g, "job", "follow", "260925-101010-00"), current: "", absent: []string{"260925-090909-00"}, want: []string{"--echo"}},
		{name: "config show keys", line: []string{"config", "show"}, current: "watch.", want: []string{"watch.directory", "watch.refresh"}},
		{name: "config generate file", line: []string{"config", "generate"}, current: "", files: true, exact: []string{"--force", "--full", "--help", "--minimal"}},
		{name: "path option", line: []string{"--config"}, current: "", files: true, exact: nil},
		{name: "path option inline", line: []string{"run"}, current: "--cf=", files: true, exact: nil},
		{name: "setup words", line: []string{"setup"}, current: "", exact: []string{"--help", "shared", "tab"}},
		{name: "setup tab options", line: []string{"setup", "tab"}, current: "", exact: []string{"--help"}},
		{name: "hidden word not offered", line: nil, current: "__", exact: nil},
	} {
		got, files := complete(tc.line, tc.current)
		if files != tc.files {
			t.Errorf("%s: files=%v, want %v", tc.name, files, tc.files)
		}
		if tc.exact != nil || (tc.want == nil && tc.absent == nil) {
			if strings.Join(got, " ") != strings.Join(tc.exact, " ") {
				t.Errorf("%s: %q, want %q", tc.name, got, tc.exact)
			}
			continue
		}
		set := map[string]bool{}
		for _, c := range got {
			set[c] = true
		}
		for _, w := range tc.want {
			if !set[w] {
				t.Errorf("%s: %q lacks %q", tc.name, got, w)
			}
		}
		for _, a := range tc.absent {
			if set[a] {
				t.Errorf("%s: %q holds %q", tc.name, got, a)
			}
		}
	}
}

// TestCompleteWord drives the hidden word through Main as the script does:
// the cursor index and the words, the candidates one per line, the :files
// directive last, exit 0 and silence on a bad cursor, a cursor past the
// last word as an empty current word, and no state created under a
// configuration that does not load.
func TestCompleteWord(t *testing.T) {
	cfg := completionLab(t)
	run := func(args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Main(append([]string{completion.Word}, args...), strings.NewReader(""), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	if code, out, errText := run("2", "karvi", "run", "--tar"); code != exitcode.ExitSuccess || out != "--target\n" || errText != "" {
		t.Errorf("--tar: %d %q %q", code, out, errText)
	}
	want, _ := complete([]string{"--config", cfg, "login"}, "")
	if code, out, _ := run("4", "karvi", "--config", cfg, "login"); code != 0 || out != strings.Join(want, "\n")+"\n" || !strings.Contains(out, "\ncore-nyc-01\nedge-sfo-02\n") || !strings.Contains(out, "--target\n") {
		t.Errorf("login position: %d\n%s", code, out)
	}
	if code, out, _ := run("2", "karvi", "--config"); code != 0 || out != ":files\n" {
		t.Errorf("--config: %d %q", code, out)
	}
	for _, bad := range [][]string{{}, {"x", "karvi"}, {"0", "karvi"}, {"3", "karvi"}, {"1"}} {
		if code, out, errText := run(bad...); code != 0 || out != "" || errText != "" {
			t.Errorf("%q: %d %q %q", bad, code, out, errText)
		}
	}
	if code, out, _ := run("5", "karvi", "--config", "/nonexistent/karvi.toml", "run", "--target"); code != 0 || out != "" {
		t.Errorf("a configuration that does not load: %d %q", code, out)
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(cfg), "base")); !os.IsNotExist(err) {
		t.Errorf("completion created the base directory: %v", err)
	}
	// The word is not in the table: it abbreviates to nothing and has no help.
	var errBuf bytes.Buffer
	if code := Main([]string{"__c"}, strings.NewReader(""), &bytes.Buffer{}, &errBuf); code != exitcode.ExitUsageError || !strings.Contains(errBuf.String(), "cli_command_unknown") {
		t.Errorf("__c: %d %q", code, errBuf.String())
	}
	if strings.Contains(topHelp, completion.Word) {
		t.Error("the hidden word is in the top help")
	}
}
