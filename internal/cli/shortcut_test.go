package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestNearCommand: the command word one slip from a shortcut's word, ignoring
// case (EXAMPLES chapter 71, rule 4): a letter added, dropped, or changed, or
// two neighbours swapped, from a command word of four letters or more.
func TestNearCommand(t *testing.T) {
	for word, want := range map[string]string{
		"lgoin": "login", "logn": "login", "loginn": "login", "xlogin": "login", "logim": "login", "Lgoin": "login", "LOGIN": "login",
		"comand": "command", "commadn": "command", "crnu": "crun", "strem": "stream", "deamon": "daemon", "cnofig": "config",
		"stup": "setup", "wacth": "watch", "verison": "version", "hepl": "help", "hel": "help",
		"lgoinn": "", "router1": "", "r1": "", "rnu": "", "jbo": "", "192.0.2.1": "", "comandd": "",
	} {
		if got := nearCommand(word); got != want {
			t.Errorf("nearCommand(%q) = %q, want %q", word, got, want)
		}
	}
}

// TestShortcutGuard: a first word one slip from a command word that is no
// inventory name is refused as cli_command_near_miss (exit 4) by login, by
// command, and by the recording wrapper, before any claim (rule 4); the
// same word after a command word is a device as before.
func TestShortcutGuard(t *testing.T) {
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv("PATH", t.TempDir())
	scoreboards := filepath.Join(t.TempDir(), "scoreboards")
	sets = append(sets, "--set", `scoreboards="`+scoreboards+`"`)
	for _, tc := range []struct {
		args []string
		near string
	}{
		{[]string{"lgoin"}, "login"},
		{[]string{"lgoin", "--record"}, "login"},
		{[]string{"lgoin", "--address", "127.0.0.1"}, "login"},
		{[]string{"Login", "r1"}, "login"},
		{[]string{"comand", "r1", "show", "clock"}, "command"},
		{[]string{"verison"}, "version"},
		{[]string{"stup", "--transport", "system", "shared"}, "setup"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Main(append(append([]string{}, sets...), tc.args...), strings.NewReader(""), &stdout, &stderr)
			want := "cli_command_near_miss: \"" + tc.args[0] + "\" is not a command; did you mean " + tc.near + "?\n"
			if got != exitcode.ExitUsageError || stderr.String() != want {
				t.Fatalf("exit=%d stderr=%q, want %q", got, stderr.String(), want)
			}
			if entries, _ := os.ReadDir(scoreboards); len(entries) != 0 {
				t.Fatalf("a claim before the guard: %v", entries)
			}
		})
	}
	// After a command word the word is a device: login goes ahead to the
	// missing askpass helper, its address given so DNS is not asked.
	var stdout, stderr bytes.Buffer
	if got := Main(append(append([]string{}, sets...), "login", "lgoin", "--address", "127.0.0.1"), strings.NewReader(""), &stdout, &stderr); got != exitcode.ExitDependencyError || !strings.HasPrefix(stderr.String(), "dependency_askpass_unavailable: ") {
		t.Fatalf("login lgoin: exit=%d stderr=%q", got, stderr.String())
	}
}
