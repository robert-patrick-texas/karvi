package auditview

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/flagspec"
)

// TestSpec holds the spec to the defined flags: every long flag in Order
// once, each one-letter spelling on the long flag's value, a placeholder for
// every flag that takes a value and none for a switch.
func TestSpec(t *testing.T) {
	fs := Spec.FlagSet()
	seen := map[string]bool{}
	for _, name := range Spec.Order {
		f := fs.Lookup(name)
		if f == nil || seen[name] {
			t.Fatalf("Spec.Order names %q: undefined or twice", name)
		}
		seen[name] = true
		if switched := flagspec.IsBool(f); switched == (Spec.Placeholder(name) != "") {
			t.Errorf("--%s: switch %v, placeholder %q", name, switched, Spec.Placeholder(name))
		}
		if s := Spec.Short[name]; s != "" && fs.Lookup(s) == nil {
			t.Errorf("-%s, --%s's spelling, is not defined", s, name)
		}
	}
	fs.VisitAll(func(f *flag.Flag) {
		if !seen[Spec.Long(f.Name)] {
			t.Errorf("-%s is in no flag of Spec.Order", f.Name)
		}
	})
	const want = "usage: karvi-audit [-f] [-r] [-g PATTERN] [-o NAME]... [-d NAME]... [-n N] [-S TIME] [-U TIME] [--nocolor]"
	if got := Spec.Usage(); got != want {
		t.Errorf("Usage()\n got %q\nwant %q", got, want)
	}
}

// parse is the flags of args.
func parse(t *testing.T, args ...string) *Flags {
	t.Helper()
	fs := flag.NewFlagSet("karvi-audit", flag.ContinueOnError)
	f := DefineFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("%q: %v", args, err)
	}
	return f
}

// TestArgs: journalctl's arguments for each option, one or two dashes, = or
// a space; the devices lowercased; the operators as two terms, the names as
// the operator in one and as the login user in the other.
func TestArgs(t *testing.T) {
	base := []string{"--no-pager", "--output=json", "--output-fields=KARVI_AUDIT_JSON"}
	match := []string{"SYSLOG_IDENTIFIER=karvi", "MESSAGE_ID=9cb838d10f074d8a928e9f7c2f98450a"}
	join := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	cases := []struct {
		args []string
		want []string
	}{
		{nil, join(base, match)},
		{[]string{"-f", "--reverse", "-g=show", "-n", "5", "-S", "yesterday", "--until=-1h"}, join(base, []string{"--follow", "--reverse", "--grep=show", "--lines=5", "--since=yesterday", "--until=-1h"}, match)},
		{[]string{"-n", "0"}, join(base, []string{"--lines=0"}, match)},
		{[]string{"-d", "Core-RTR-1", "--device", "edge"}, join(base, match, []string{"KARVI_DEVICE=core-rtr-1", "KARVI_DEVICE=edge"})},
		{[]string{"-o", "noc", "-d", "edge", "-o", "jsmith"}, join(base,
			match, []string{"KARVI_DEVICE=edge", "KARVI_OPERATOR=noc", "KARVI_OPERATOR=jsmith"}, []string{"+"},
			match, []string{"KARVI_DEVICE=edge", "KARVI_LOGINUSER=noc", "KARVI_LOGINUSER=jsmith"})},
	}
	for _, c := range cases {
		if got := Args(parse(t, c.args...)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %q\nwant %q", c.args, got, c.want)
		}
	}
	fs := flag.NewFlagSet("karvi-audit", flag.ContinueOnError)
	fs.SetOutput(&strings.Builder{})
	DefineFlags(fs)
	if err := fs.Parse([]string{"-n", "x"}); err == nil || err.Error() != `invalid value "x" for flag -n: parse error` {
		t.Errorf("-n x: %v", err)
	}
}

// entry is journalctl's JSON line for an event's JSON.
func entry(event string) string {
	b, _ := json.Marshal(map[string]string{"__CURSOR": "s=1", "KARVI_AUDIT_JSON": event})
	return string(b)
}

const (
	commandEvent = `{"schema_version":2,"event_name":"run.command","outcome":"device_error","operator":{"username":"netops.test"},"loginuser":"netops","job_id":"261017-143818-00","device":{"id":"d","canonical_name":"fake-iosxe","platform":"cisco_iosxe"},"action":{"command":"snmp-server community <removed> RO"}}`
	loginEvent   = `{"schema_version":2,"event_name":"login.start","operator":{"username":"netops"},"login_id":"261017-143825-00","device":{"id":"d","canonical_name":"fake-iosxe","platform":"cisco_iosxe"},"action":{}}`
)

// TestDecode: an event's cells, a login's ID in place of a job's, and every
// entry the reader cannot read.
func TestDecode(t *testing.T) {
	row, ok := Decode([]byte(entry(commandEvent)))
	want := Row{ID: "261017-143818-00", Operator: "netops.test", LoginUser: "netops", Mode: "run", Event: "command", Outcome: "device_error", Device: "fake-iosxe", Command: "snmp-server community <removed> RO"}
	if !ok || row != want {
		t.Errorf("command: %v %+v", ok, row)
	}
	row, ok = Decode([]byte(entry(loginEvent)))
	if !ok || row != (Row{ID: "261017-143825-00", Operator: "netops", Mode: "login", Event: "start", Device: "fake-iosxe"}) {
		t.Errorf("login: %v %+v", ok, row)
	}
	for name, line := range map[string]string{
		"not JSON":         `{"KARVI_AUDIT_JSON":`,
		"no event":         `{"MESSAGE":"karvi audit run.started outcome=started"}`,
		"a field array":    `{"KARVI_AUDIT_JSON":[123,125]}`,
		"event not JSON":   `{"KARVI_AUDIT_JSON":"{"}`,
		"audit schema 1":   entry(`{"schema_version":1,"event_name":"run.started"}`),
		"audit schema 7":   entry(`{"schema_version":7,"event_name":"test"}`),
		"a field mistyped": entry(`{"schema_version":2,"event_name":"run.start","operator":"netops"}`),
	} {
		if _, ok := Decode([]byte(line)); ok {
			t.Errorf("%s: read", name)
		}
	}
}

// rows are the lab's events of chapter 64 (c0-lab.sh) the layout's tests
// lay out.
var rows = []Row{
	{ID: "261017-143817-00", Operator: "netops", Mode: "cmd", Event: "command", Outcome: "success", Device: "fake-iosxe", Command: "show version"},
	{ID: "261017-143818-00", Operator: "netops", Mode: "run", Event: "command", Outcome: "connection_error", Device: "dead-iosxe", Command: "show clock"},
	{ID: "261017-143818-00", Operator: "netops", Mode: "run", Event: "command", Outcome: "not_attempted_prior_command_failure", Device: "fake-iosxe", Command: "show running-config | section interface GigabitEthernet0/0/1"},
	{ID: "261017-143825-00", Operator: "netops", Mode: "login", Event: "start", Device: "fake-iosxe"},
	{ID: "261017-143828-00", Operator: "netops.test", LoginUser: "netops", Mode: "cmd", Event: "command", Outcome: "device_error", Device: "fake-iosxe", Command: "show ip interface brief"},
}

// TestLayout pins the lines at each width to chapter 64's agreed layouts
// (c3-mockup.out, the operator as issue 3 set it), DEVICE at chapter 66's
// 30: at a pipe nothing cut, OPERATOR and DEVICE centred; at 110 COMMAND cut
// with one cell free; at 99 DEVICE still centred, the line's … in the cell
// after it; at 98 and 80 DEVICE at its left.
func TestLayout(t *testing.T) {
	want := map[int][]string{
		0: {
			"JOB-ID                OPERATOR      MODE  EVENT   OUTCOME                      DEVICE             COMMAND",
			"261017-143817-00       netops       cmd   command success                    fake-iosxe           show version",
			"261017-143818-00       netops       run   command connection_error           dead-iosxe           show clock",
			"261017-143818-00       netops       run   command not_attempted_p…           fake-iosxe           show running-config | section interface GigabitEthernet0/0/1",
			"261017-143825-00       netops       login start                              fake-iosxe",
			"261017-143828-00 netops.test/netops cmd   command device_error               fake-iosxe           show ip interface brief",
		},
		110: {
			"JOB-ID                OPERATOR      MODE  EVENT   OUTCOME                      DEVICE             COMMAND",
			"261017-143817-00       netops       cmd   command success                    fake-iosxe           show versi…",
			"261017-143818-00       netops       run   command connection_error           dead-iosxe           show clock",
			"261017-143818-00       netops       run   command not_attempted_p…           fake-iosxe           show runni…",
			"261017-143825-00       netops       login start                              fake-iosxe",
			"261017-143828-00 netops.test/netops cmd   command device_error               fake-iosxe           show ip in…",
		},
		99: {
			"JOB-ID                OPERATOR      MODE  EVENT   OUTCOME                      DEVICE            …",
			"261017-143817-00       netops       cmd   command success                    fake-iosxe          …",
			"261017-143818-00       netops       run   command connection_error           dead-iosxe          …",
			"261017-143818-00       netops       run   command not_attempted_p…           fake-iosxe          …",
			"261017-143825-00       netops       login start                              fake-iosxe",
			"261017-143828-00 netops.test/netops cmd   command device_error               fake-iosxe          …",
		},
		98: {
			"JOB-ID                OPERATOR      MODE  EVENT   OUTCOME          DEVICE                       …",
			"261017-143817-00       netops       cmd   command success          fake-iosxe                   …",
			"261017-143818-00       netops       run   command connection_error dead-iosxe                   …",
			"261017-143818-00       netops       run   command not_attempted_p… fake-iosxe                   …",
			"261017-143825-00       netops       login start                    fake-iosxe",
			"261017-143828-00 netops.test/netops cmd   command device_error     fake-iosxe                   …",
		},
		80: {
			"JOB-ID                OPERATOR      MODE  EVENT   OUTCOME          DEVICE     …",
			"261017-143817-00       netops       cmd   command success          fake-iosxe …",
			"261017-143818-00       netops       run   command connection_error dead-iosxe …",
			"261017-143818-00       netops       run   command not_attempted_p… fake-iosxe …",
			"261017-143825-00       netops       login start                    fake-iosxe",
			"261017-143828-00 netops.test/netops cmd   command device_error     fake-iosxe …",
		},
	}
	for width, lines := range want {
		tb := Table{Width: width}
		got := []string{tb.Heading()}
		for _, r := range rows {
			got = append(got, tb.Line(r))
		}
		for i := range lines {
			if got[i] != lines[i] {
				t.Errorf("width %d, line %d:\n got %q\nwant %q", width, i, got[i], lines[i])
			}
			if n := runes(got[i]); width > 0 && n > width-1 {
				t.Errorf("width %d, line %d: %d cells", width, i, n)
			}
		}
	}
}

// TestDeviceWidth: a name of DEVICE's 30 cells is whole, a longer one cut
// with … in the column's last cell.
func TestDeviceWidth(t *testing.T) {
	for device, want := range map[string]string{
		"core-router-01.nyc.example.net":  " core-router-01.nyc.example.net show clock",
		"core-router-01.nyc1.example.net": " core-router-01.nyc1.example.n… show clock",
	} {
		r := Row{ID: "261017-143817-00", Operator: "netops", Mode: "cmd", Event: "command", Outcome: "success", Device: device, Command: "show clock"}
		if got := (Table{}).Line(r); !strings.HasSuffix(got, want) {
			t.Errorf("%s:\n got %q\nwant the end %q", device, got, want)
		}
	}
}

// TestColours pins a row's codes to chapter 64's c7-colour.ansi: OPERATOR
// in the accent, OUTCOME by its word, DEVICE in the target role, bold.
func TestColours(t *testing.T) {
	tb := Table{Width: 132, Colour: true}
	want := "261017-143828-00 \x1b[36mnetops.test/netops\x1b[0m cmd   command \x1b[31mdevice_error\x1b[0m               \x1b[1;33mfake-iosxe\x1b[0m           show ip interface brief"
	if got := tb.Line(rows[4]); got != want {
		t.Errorf("\n got %q\nwant %q", got, want)
	}
	if got := tb.Heading(); !strings.HasPrefix(got, "\x1b[34mJOB-ID\x1b[0m") || !strings.HasSuffix(got, "\x1b[34mCOMMAND\x1b[0m") {
		t.Errorf("heading %q", got)
	}
}

// TestOperatorCell: the operator, the pair, the operator's part cut so that
// the login user stays whole, and a login user too long for that.
func TestOperatorCell(t *testing.T) {
	for _, c := range []struct{ op, lu, want string }{
		{"netops", "", "netops"},
		{"netops.test", "netops", "netops.test/netops"},
		{"svc-karvi-collector", "jsmith", "svc-karvi-…/jsmith"},
		{"a-very-long-service-account", "", "a-very-long-servi…"},
		{"noc", "a-very-long-login-user", "noc/a-very-long-l…"},
	} {
		if got := operatorCell(c.op, c.lu); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.op, c.lu, got, c.want)
		}
	}
}

// TestOutcomeRole: the words of chapter 64's c6-roles.out.
func TestOutcomeRole(t *testing.T) {
	want := map[string]string{
		"success": "success", "complete": "success", "exercise": "success",
		"error": "error", "fail": "error", "device_error": "error", "connection_error": "error",
		"authentication_error": "error", "privilege_error": "error", "timeout": "error",
		"output_limit_exceeded": "error", "output_error": "error", "icmp_unreachable": "error",
		"icmp_capability_unavailable": "error",
		"cancel":                      "warning", "halt": "warning", "incomplete": "warning",
		"not_attempted_prior_command_failure": "warning", "not_attempted_session_init_failure": "warning",
		"not_started_halt": "warning", "not_started_wave_gate": "warning",
		"incomplete_shutdown": "warning", "incomplete_daemon_recovery": "warning",
		"": "",
	}
	for word, role := range want {
		if got := outcomeRole(word); got != role {
			t.Errorf("%q: %q, want %q", word, got, role)
		}
	}
}

// TestVisible: a statement's control characters as Go's escapes, so a row
// stays one line.
func TestVisible(t *testing.T) {
	if got := visible("show\tclock\nend\x1b"); got != `show\tclock\nend\x1b` {
		t.Errorf("%q", got)
	}
	if got := visible("show clock"); got != "show clock" {
		t.Errorf("%q", got)
	}
}

// fakeJournalctl puts a journalctl on PATH that records its arguments,
// writes lines, writes msg to standard error, waits wait (a sleep(1)
// argument, "" none), and exits with code.
func fakeJournalctl(t *testing.T, lines []string, msg string, code int, wait string) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	data := filepath.Join(dir, "lines")
	if err := os.WriteFile(data, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(lines) == 0 {
		if err := os.WriteFile(data, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	argsFile = filepath.Join(dir, "args")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" >'" + argsFile + "'\ncat '" + data + "'\n"
	if msg != "" {
		script += "echo '" + msg + "' >&2\n"
	}
	if wait != "" {
		script += "sleep " + wait + "\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	// The fake first, and the system's tools its script runs after it.
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("NO_COLOR", "")
	return argsFile
}

// runFiles runs the reader for args with standard output and standard error
// into files, and returns the exit and both texts.
func runFiles(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	errf, err := os.Create(filepath.Join(dir, "err"))
	if err != nil {
		t.Fatal(err)
	}
	code := Run(parse(t, args...), out, errf)
	out.Close()
	errf.Close()
	o, _ := os.ReadFile(out.Name())
	e, _ := os.ReadFile(errf.Name())
	return code, string(o), string(e)
}

// TestRun: the rows into a file, plain; journalctl's arguments; the count
// of the entries not read; nothing matched; journalctl's own refusal; and
// journalctl missing.
func TestRun(t *testing.T) {
	argsFile := fakeJournalctl(t, []string{entry(commandEvent), `{"MESSAGE":"old"}`, entry(loginEvent)}, "", 0, "")
	code, out, errs := runFiles(t, "-o", "netops")
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 3 || !strings.HasPrefix(lines[0], "JOB-ID") || !strings.Contains(lines[1], "netops.test/netops") || strings.Contains(out, "\x1b") {
		t.Errorf("rows: %d %q", code, out)
	}
	if errs != "karvi-audit: 1 event not shown: not audit schema 2\n" {
		t.Errorf("count: %q", errs)
	}
	got, _ := os.ReadFile(argsFile)
	if !strings.Contains(string(got), "KARVI_OPERATOR=netops\n+\n") || !strings.HasSuffix(string(got), "KARVI_LOGINUSER=netops\n") {
		t.Errorf("args: %q", got)
	}

	fakeJournalctl(t, nil, "", 0, "")
	if code, out, errs := runFiles(t); code != 0 || out != "" || errs != "karvi-audit: no matches\n" {
		t.Errorf("no matches: %d %q %q", code, out, errs)
	}
	fakeJournalctl(t, nil, "", 0, "1")
	if code, out, errs := runFiles(t, "-f"); code != 0 || !strings.HasPrefix(out, "JOB-ID") || errs != "" {
		t.Errorf("following, nothing yet: %d %q %q", code, out, errs)
	}
	fakeJournalctl(t, nil, "Please specify either --reverse or --follow, not both.", 1, "")
	if code, out, errs := runFiles(t, "-f", "-r"); code != 1 || out != "" || errs != "Please specify either --reverse or --follow, not both.\n" {
		t.Errorf("following, refused: %d %q %q", code, out, errs)
	}

	fakeJournalctl(t, []string{`{"MESSAGE":"old"}`, `{"MESSAGE":"old"}`}, "", 0, "")
	if code, out, errs := runFiles(t); code != 0 || out != "" || errs != "karvi-audit: 2 events not shown: not audit schema 2\n" {
		t.Errorf("none readable: %d %q %q", code, out, errs)
	}

	fakeJournalctl(t, nil, "Failed to parse timestamp: yesterdayx", 1, "")
	if code, out, errs := runFiles(t, "-S", "yesterdayx"); code != 1 || out != "" || errs != "Failed to parse timestamp: yesterdayx\n" {
		t.Errorf("journalctl's refusal: %d %q %q", code, out, errs)
	}

	t.Setenv("PATH", t.TempDir())
	if code, out, errs := runFiles(t); code != 1 || out != "" || errs != "karvi-audit: journalctl not found\n" {
		t.Errorf("missing: %d %q %q", code, out, errs)
	}
}
