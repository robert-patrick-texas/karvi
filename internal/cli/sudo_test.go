package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestSudoIsASourceInTheOrder: --sudo TEXT is a command source where it is
// written, between --cmd, --cf, and --sleep alike, its command run as root
// (EXAMPLES chapter 96); a --cf line --sudo TEXT or --sudo=TEXT is the same
// statement, a word that only begins as --sudo is device text; --timeout
// attaches to the --sudo before it as to a --cmd; the list carries no flags
// when no command is root; --literal sends a file's --sudo line as written,
// the option still a root command.
func TestSudoIsASourceInTheOrder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "post.txt")
	if err := os.WriteFile(file, []byte("  --sudo  journalctl -n 5  \n--sudo=id -u\n--sudoers\nwhoami\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv := mustParse(t, "run", "--target", "r1", "--cmd", "uptime", "--sudo", "systemctl restart nginx", "--timeout", "60s", "--sleep", "5s", "--sudo=iptables-save", "--cf", file, "--cmd", "show clock")
	want := []CommandSource{{Value: "uptime"}, {Sudo: true, Value: "systemctl restart nginx"}, {Sleep: true, Value: "5s"}, {Sudo: true, Value: "iptables-save"}, {File: true, Value: file}, {Value: "show clock"}}
	if !reflect.DeepEqual(inv.Sources, want) {
		t.Fatalf("sources %+v", inv.Sources)
	}
	var stderr bytes.Buffer
	set, code, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr})
	if !ok {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	if want := []string{"uptime", "systemctl restart nginx", "iptables-save", "journalctl -n 5", "id -u", "--sudoers", "whoami", "show clock"}; !reflect.DeepEqual(set.commands, want) {
		t.Fatalf("commands %q, want %q", set.commands, want)
	}
	if want := []bool{false, true, true, true, true, false, false, false}; !reflect.DeepEqual(set.decl.sudo, want) {
		t.Fatalf("sudo %v, want %v", set.decl.sudo, want)
	}
	if want := []int64{0, 0, int64(5 * time.Second), 0, 0, 0, 0, 0}; !reflect.DeepEqual(set.decl.sleeps, want) {
		t.Fatalf("sleeps %v, want %v", set.decl.sleeps, want)
	}
	if len(set.decl.timeouts) != 8 || set.decl.timeouts[1] != int64(60*time.Second) {
		t.Fatalf("the --timeout belongs to the --sudo before it, the second command: %v", set.decl.timeouts)
	}
	// No root command: the list is empty, as the other declaration lists are.
	inv = mustParse(t, "command", "r1", "--cmd", "show clock", "--cmd", "show users")
	if set, _, ok = commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); !ok || set.decl.sudo == nil || len(set.decl.sudo) != 0 {
		t.Fatalf("no root command: %#v", set.decl.sudo)
	}
	// --literal: the file's lines as written, the option still root.
	inv = mustParse(t, "command", "r1", "--literal", "--sudo", "id -u", "--cf", file)
	if set, _, ok = commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); !ok {
		t.Fatalf("literal: %s", stderr.String())
	}
	if want := []string{"id -u", "  --sudo  journalctl -n 5  ", "--sudo=id -u", "--sudoers", "whoami"}; !reflect.DeepEqual(set.commands, want) || !reflect.DeepEqual(set.decl.sudo, []bool{true, false, false, false, false}) {
		t.Fatalf("literal: commands %q sudo %v", set.commands, set.decl.sudo)
	}
}

// TestSudoRefusals: an unquoted --sudo command beside another command
// leaves freeform text, a blank --sudo has no command, a declaration before the first --cmd or
// --sudo has none to attach to, and a file line --sudo with no command is
// refused naming the file and the line, each before any connection
// (EXAMPLES chapter 96).
func TestSudoRefusals(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code string
		text string
	}{
		{[]string{"command", "srv1", "--cmd", "whoami", "--sudo", "systemctl", "restart", "nginx"}, "cli_command_text_mixed", `freeform text "restart nginx" joins only a line's one --cmd or --sudo, right after its value; quote each command`},
		{[]string{"run", "--target", "srv1", "--sudo", "whoami", "--sudo", "systemctl", "restart", "nginx"}, "cli_command_text_mixed", `freeform text "restart nginx"`},
		{[]string{"command", "srv1", "--sudo", "systemctl", "--echo", "restart", "nginx"}, "cli_command_text_mixed", `freeform text "restart nginx"`},
		{[]string{"command", "srv1", "--sudo", ""}, "cli_option_value_invalid", "--sudo has no command; it takes the command to run as root, --sudo TEXT"},
		{[]string{"run", "--target", "srv1", "--sudo=  "}, "cli_option_value_invalid", "--sudo has no command"},
		{[]string{"run", "--target", "srv1", "--timeout", "5s", "--sudo", "id -u"}, "declaration_before_command", "--timeout is given before the first --cmd or --sudo"},
	} {
		_, err := Parse(tc.args)
		if errorcodes.Of(err) != tc.code || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%q: %v, want %s containing %q", tc.args, err, tc.code, tc.text)
		}
	}
	dir := t.TempDir()
	for name, content := range map[string]string{"bare": "uptime\n--sudo\n", "equals": "uptime\n--sudo=  \n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"bare", "equals"} {
		path := filepath.Join(dir, name)
		inv := mustParse(t, "command", "srv1", "--cf", path)
		var stderr bytes.Buffer
		want := "commands_file_sudo_invalid: commands file " + path + " line 2: --sudo has no command; the line is --sudo TEXT\n"
		if _, _, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr}); ok || stderr.String() != want {
			t.Errorf("%s: %q, want %q", name, stderr.String(), want)
		}
	}
}

// TestFreeformJoinsALoneCommand: the words right after the value of a
// line's one --cmd or --sudo are that command's rest, as the freeform
// command's words are: karvi's options before it are its own, and every word
// after its first is the command's, an option's name among them.
func TestFreeformJoinsALoneCommand(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want CommandSource
		decl []Declaration
	}{
		{[]string{"command", "srv1", "--sudo", "systemctl", "restart", "nginx"}, CommandSource{Sudo: true, Value: "systemctl restart nginx"}, nil},
		{[]string{"run", "--target", "srv1", "--sudo=systemctl", "restart", "nginx"}, CommandSource{Sudo: true, Value: "systemctl restart nginx"}, nil},
		{[]string{"command", "srv1", "--sudo", "systemctl restart", "nginx"}, CommandSource{Sudo: true, Value: "systemctl restart nginx"}, nil},
		{[]string{"command", "r1", "--cmd", "show", "ip", "interface", "brief"}, CommandSource{Value: "show ip interface brief"}, nil},
		{[]string{"command", "srv1", "--timeout", "60s", "--sudo", "systemctl", "restart", "nginx"}, CommandSource{Sudo: true, Value: "systemctl restart nginx"}, []Declaration{{Option: "timeout", Value: "60s", Command: 1}}},
		{[]string{"command", "srv1", "--sudo", "systemctl", "restart", "nginx", "--timeout", "60s"}, CommandSource{Sudo: true, Value: "systemctl restart nginx --timeout 60s"}, nil},
		{[]string{"srv1", "--sudo", "systemctl", "restart", "nginx"}, CommandSource{Sudo: true, Value: "systemctl restart nginx"}, nil},
		{[]string{"command", "srv1", "--sudo", "--", "journalctl", "-u", "nginx"}, CommandSource{Sudo: true, Value: "journalctl -u nginx"}, nil},
		{[]string{"srv1", "--sudo", "--", "ss", "-tlnp"}, CommandSource{Sudo: true, Value: "ss -tlnp"}, nil},
		{[]string{"command", "r1", "--cmd", "--", "show", "running-config", "|", "include", "--echo"}, CommandSource{Value: "show running-config | include --echo"}, nil},
		{[]string{"command", "srv1", "--timeout", "60s", "--sudo", "--", "journalctl", "-u", "nginx"}, CommandSource{Sudo: true, Value: "journalctl -u nginx"}, []Declaration{{Option: "timeout", Value: "60s", Command: 1}}},
	} {
		inv, err := Parse(tc.args)
		if err != nil {
			t.Errorf("%q: %v", tc.args, err)
			continue
		}
		if !inv.Freeform || !reflect.DeepEqual(inv.Sources, []CommandSource{tc.want}) || !reflect.DeepEqual(inv.Declarations, tc.decl) {
			t.Errorf("%q: freeform %v sources %+v declarations %+v, want %+v %+v", tc.args, inv.Freeform, inv.Sources, inv.Declarations, tc.want, tc.decl)
		}
	}
}

// TestDashedCommandEndsTheLine: --sudo -- takes every word after it as the
// last command of a line with others before it, each declaration attaching
// backwards as written; with no word after -- it is refused, and a
// command's own option right after its first word says how to send it.
func TestDashedCommandEndsTheLine(t *testing.T) {
	inv := mustParse(t, "command", "srv1", "--cmd", "systemctl is-active nginx", "--timeout", "5s", "--sudo", "--", "systemctl", "restart", "nginx", "--no-block")
	want := []CommandSource{{Value: "systemctl is-active nginx"}, {Sudo: true, Value: "systemctl restart nginx --no-block"}}
	if inv.Freeform || !reflect.DeepEqual(inv.Sources, want) || !reflect.DeepEqual(inv.Declarations, []Declaration{{Option: "timeout", Value: "5s", Command: 1}}) {
		t.Fatalf("freeform %v sources %+v declarations %+v", inv.Freeform, inv.Sources, inv.Declarations)
	}
	for _, tc := range []struct {
		args []string
		code string
		text string
	}{
		{[]string{"command", "srv1", "--sudo", "--"}, "cli_option_value_invalid", "--sudo -- has no command after it; the words after -- are its command"},
		{[]string{"command", "srv1", "--sudo", "id", "-u"}, "cli_option_unknown", "unknown option -u in command; for the command's own option write --sudo -- TEXT, or quote the command"},
		{[]string{"run", "--target", "srv1", "--cmd", "ls", "-la"}, "cli_option_unknown", "write --cmd -- TEXT, or quote the command"},
	} {
		_, err := Parse(tc.args)
		if errorcodes.Of(err) != tc.code || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%q: %v, want %s containing %q", tc.args, err, tc.code, tc.text)
		}
	}
}

// TestSudoInAFolderList: a device's own file carries its root commands into
// its list beside the line's.
func TestSudoInAFolderList(t *testing.T) {
	lists := filepath.Join(t.TempDir(), "lists")
	if err := os.MkdirAll(lists, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lists, "srv1"), []byte("uptime\n--sudo systemctl restart nginx\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	inv := mustParse(t, "run", "--target", "srv1", "--sudo", "iptables-save", "--cf", lists)
	var stderr bytes.Buffer
	set, code, ok := commandPlan(inv, app.IO{Stdin: strings.NewReader(""), Stderr: &stderr})
	if !ok || set.perTarget == nil {
		t.Fatalf("exit %d %s", code, stderr.String())
	}
	list, err := set.perTarget("srv1")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"iptables-save", "uptime", "systemctl restart nginx"}; !reflect.DeepEqual(list.Commands, want) || !reflect.DeepEqual(list.Sudo, []bool{true, false, true}) {
		t.Fatalf("srv1 %q sudo %v", list.Commands, list.Sudo)
	}
}

// TestStreamKeepsASudoLine: in stream mode a --sudo line is one root
// command, the rest of the line, quotes wrapping it whole removed; it is
// command material in the order typed, a declaration after it attaching to
// it, and it stays from one job to the next as --cmd's option form does.
func TestStreamKeepsASudoLine(t *testing.T) {
	args, role, stdinOption, refused := streamOptionArgs("--sudo systemctl restart nginx")
	if !reflect.DeepEqual(args, []string{"--sudo", "systemctl restart nginx"}) || role != roleSudo || stdinOption != "" || refused != nil || !streamCommandRole(role) {
		t.Fatalf("args %q role %v stdin %q refused %v", args, role, stdinOption, refused)
	}
	var runs [][]string
	execute := func(_ int, argv []string) int {
		runs = append(runs, append([]string{}, argv...))
		return 0
	}
	var stderr bytes.Buffer
	in := strings.Join([]string{
		"--target srv1",
		"--sudo systemctl restart nginx",
		"--timeout 60s",
		"uptime",
		"--go",
		`--sudo "id -u"`,
		"whoami",
		"--go",
	}, "\n") + "\n"
	streamLoop(context.Background(), streamScanner(strings.NewReader(in)), globalOptions{}, &stderr, execute, restartNone)
	want := [][]string{
		{"run", "--target", "srv1", "--sudo", "systemctl restart nginx", "--timeout", "60s", "--cmd", "uptime"},
		{"run", "--target", "srv1", "--sudo", "systemctl restart nginx", "--timeout", "60s", "--sudo", "id -u", "--cmd", "whoami"},
	}
	if !reflect.DeepEqual(runs, want) || stderr.Len() != 0 {
		t.Errorf("runs:\n%q\nwant:\n%q\nstderr: %s", runs, want, stderr.String())
	}
}
