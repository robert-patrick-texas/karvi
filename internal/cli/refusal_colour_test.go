package cli

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
)

// terminalBuffer is standard error at a terminal as the display tells one:
// a writer that names its terminal, as osutil.RawTerminalLines does,
// holding what is written.
type terminalBuffer struct{ bytes.Buffer }

func (*terminalBuffer) Terminal() *os.File { return os.Stderr }

// Where a case's standard error goes: a terminal, a pipe (a buffer, which
// names no file), or a regular file.
const (
	toTerminal = "terminal"
	toPipe     = "pipe"
	toFile     = "file"
)

// mainTo runs Main with standard error going to, and returns its exit and
// what it wrote there.
func mainTo(t *testing.T, to string, args []string, stdin string) (int, string) {
	t.Helper()
	var stdout bytes.Buffer
	switch to {
	case toTerminal:
		var term terminalBuffer
		got := Main(args, strings.NewReader(stdin), &stdout, &term)
		return got, term.String()
	case toFile:
		f, err := os.CreateTemp(t.TempDir(), "stderr")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		got := Main(args, strings.NewReader(stdin), &stdout, f)
		b, err := os.ReadFile(f.Name())
		if err != nil {
			t.Fatal(err)
		}
		return got, string(b)
	}
	var pipe bytes.Buffer
	got := Main(args, strings.NewReader(stdin), &stdout, &pipe)
	return got, pipe.String()
}

// The escapes a refusal and a dropped line open with under the dark theme,
// and the one that closes both.
const (
	refusalRed     = "\x1b[1;31m"
	refusalMagenta = "\x1b[1;35m"
	droppedOrange  = "\x1b[1;38;5;208m"
	colourEnd      = "\x1b[0m"
)

// TestRefusalColours (EXAMPLES chapter 56, issue 13): a refusal that ends
// the invocation, its code and message, is one line in the error colour and
// bold at a terminal, in the built-in defaults before the configuration is
// read (an unknown option, a configuration that does not load) and in the
// read's display keys after it (a target file that cannot be read, through
// reportError; a credential refused at planning, through the activity's
// result); NO_COLOR, display.color = never, the nocolor theme, and
// --nocolor, before the read and after it, turn it off;
// display.colors.error names its colour; a pipe carries no escape unless
// display.color = always, and a file none whatever the policy.
func TestRefusalColours(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	// A Vault backend whose token variable is unset refuses the device at
	// planning, before any connection.
	t.Setenv("KARVI_TEST_VTOK", "")
	os.Unsetenv("KARVI_TEST_VTOK")
	vault := []string{
		"--set", `credential-backend.v.type="vault"`,
		"--set", `credential-backend.v.address="http://127.0.0.1:9"`,
		"--set", `credential-backend.v.token-env="KARVI_TEST_VTOK"`,
		"--set", `credential-policy.default.backend-sequence=["v"]`,
	}
	run := func(display ...string) []string {
		a := append(append(append([]string{}, sets...), display...), vault...)
		return append(a, "run", "--dry-run", "--no-daemon", "--transport", "native", "--target", "127.0.0.1", "show version")
	}
	missing := func(display ...string) []string {
		a := append(append([]string{}, sets...), display...)
		return append(a, "run", "--dry-run", "--no-daemon", "--tf", base+"/none.txt", "show version")
	}
	color := func(v string) []string { return []string{"--set", "display.color=" + strconv.Quote(v)} }
	noColour := func(args []string) []string { return append([]string{"--nocolor"}, args...) }
	for _, tc := range []struct {
		name    string
		args    []string
		noColor string
		to      string
		code    string
		open    string
	}{
		{"an unknown option", []string{"run", "--bogus"}, "", toTerminal, "cli_option_unknown", refusalRed},
		{"an unknown option, NO_COLOR", []string{"run", "--bogus"}, "1", toTerminal, "cli_option_unknown", ""},
		{"an unknown option, --nocolor", noColour([]string{"run", "--bogus"}), "", toTerminal, "cli_option_unknown", ""},
		{"an unknown global option after --nocolor", []string{"--nocolor", "--bogus", "run"}, "", toTerminal, "cli_option_unknown", ""},
		{"an unknown option, a pipe", []string{"run", "--bogus"}, "", toPipe, "cli_option_unknown", ""},
		{"a configuration that does not load", append(color("sometimes"), "run", "--dry-run", "--target", "r1", "show version"), "", toTerminal, "config_enum_value_invalid", refusalRed},
		{"a configuration that does not load, --nocolor", noColour(append(color("sometimes"), "run", "--dry-run", "--target", "r1", "show version")), "", toTerminal, "config_enum_value_invalid", ""},
		{"a target file that cannot be read", missing(color("auto")...), "", toTerminal, "target_source_missing", refusalRed},
		{"a target file, display.color = never", missing(color("never")...), "", toTerminal, "target_source_missing", ""},
		{"a target file, the nocolor theme", missing(append(color("auto"), "--set", `display.theme="nocolor"`)...), "", toTerminal, "target_source_missing", ""},
		{"a target file, --nocolor", noColour(missing(color("auto")...)), "", toTerminal, "target_source_missing", ""},
		{"a credential refused at planning", run(color("auto")...), "", toTerminal, "credential_env_missing", refusalRed},
		{"a credential, display.colors.error = magenta", run(append(color("auto"), "--set", `display.colors.error="magenta"`)...), "", toTerminal, "credential_env_missing", refusalMagenta},
		{"a credential, NO_COLOR", run(color("auto")...), "1", toTerminal, "credential_env_missing", ""},
		{"a credential, --nocolor", noColour(run(color("auto")...)), "", toTerminal, "credential_env_missing", ""},
		{"a credential, a pipe", run(color("auto")...), "", toPipe, "credential_env_missing", ""},
		{"a credential, a pipe under always", run(color("always")...), "", toPipe, "credential_env_missing", refusalRed},
		{"a credential, a file under always", run(color("always")...), "", toFile, "credential_env_missing", ""},
		{"a credential, a file", run(color("auto")...), "", toFile, "credential_env_missing", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NO_COLOR", tc.noColor)
			got, stderr := mainTo(t, tc.to, tc.args, "")
			if got == 0 {
				t.Fatalf("exit 0; stderr=%q", stderr)
			}
			want := tc.open + tc.code + ": "
			if !strings.HasPrefix(stderr, want) {
				t.Fatalf("stderr=%q, want the line to open %q", stderr, want)
			}
			line := strings.TrimSuffix(stderr, "\n")
			if strings.Contains(line, "\n") {
				t.Fatalf("stderr=%q, want one line", stderr)
			}
			if tc.open != "" && !strings.HasSuffix(line, colourEnd) {
				t.Fatalf("stderr=%q, want the line closed by %q", stderr, colourEnd)
			}
			if tc.open == "" && strings.Contains(line, "\x1b") {
				t.Fatalf("stderr=%q, want no escape", stderr)
			}
		})
	}
}

// TestStreamLinesColoured (EXAMPLES chapter 56, issue 13): a line a stream
// drops and goes on from is said in the warning colour and bold at a
// terminal, its words unchanged, and a job it refuses in the error colour;
// under display.color = never, under --nocolor, on a pipe, and in a file
// under always, neither carries an escape.
func TestStreamLinesColoured(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	base, _, _ := daemonTestRuntime(t)
	sets := dryRunSets(t, base)
	t.Setenv("KARVI_TEST_VTOK", "")
	os.Unsetenv("KARVI_TEST_VTOK")
	vault := []string{
		"--set", `credential-backend.v.type="vault"`,
		"--set", `credential-backend.v.address="http://127.0.0.1:9"`,
		"--set", `credential-backend.v.token-env="KARVI_TEST_VTOK"`,
		"--set", `credential-policy.default.backend-sequence=["v"]`,
	}
	in := "--bogus\n--cf -\n--of /x\n--go\n--dry-run\n--no-daemon\n--transport native\n--target 127.0.0.1\nshow version\n--go\n--end\n"
	dropped := []string{
		"stream line 1 dropped: cli_option_unknown: ",
		"stream line 2: standard input is the stream; --cf - is not accepted",
		"stream line 3 dropped: cli_option_value_detached: ",
		"stream line 4: nothing to send",
	}
	for _, tc := range []struct {
		name     string
		nocolor  bool
		color    string
		to       string
		coloured bool
	}{
		{"at a terminal", false, "auto", toTerminal, true},
		{"display.color = never", false, "never", toTerminal, false},
		{"--nocolor", true, "auto", toTerminal, false},
		{"a pipe", false, "auto", toPipe, false},
		{"a file under always", false, "always", toFile, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var args []string
			if tc.nocolor {
				args = append(args, "--nocolor")
			}
			args = append(append(append(args, sets...), "--set", "display.color="+strconv.Quote(tc.color)), vault...)
			args = append(args, "stream")
			_, stderr := mainTo(t, tc.to, args, in)
			lines := strings.Split(strings.TrimSuffix(stderr, "\n"), "\n")
			if len(lines) != 5 {
				t.Fatalf("stderr=%q, want four dropped lines and the refusal", stderr)
			}
			for i, want := range dropped {
				open := ""
				if tc.coloured {
					open = droppedOrange
				}
				if !strings.HasPrefix(lines[i], open+want) || (tc.coloured && !strings.HasSuffix(lines[i], colourEnd)) || (!tc.coloured && strings.Contains(lines[i], "\x1b")) {
					t.Errorf("line %d=%q, want %q in %q", i+1, lines[i], want, open)
				}
			}
			open := ""
			if tc.coloured {
				open = refusalRed
			}
			if !strings.HasPrefix(lines[4], open+"credential_env_missing: ") || (!tc.coloured && strings.Contains(lines[4], "\x1b")) {
				t.Errorf("the refused job=%q, want it in %q", lines[4], open)
			}
		})
	}
}

// TestRefusalBuiltinStyle: the zero snapshot a refusal before the read is
// coloured by gives the style of the registry's defaults for the display
// keys it reads, wherever it goes, so the built-in defaults need no
// reading.
func TestRefusalBuiltinStyle(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	defaults := configload.Snapshot{Values: map[string]configload.Value{}}
	for _, key := range []string{"display.color", "display.theme", "display.colors.error", "display.colors.warning"} {
		e, ok := configschema.Lookup(key)
		if !ok {
			t.Fatalf("%s is not in the registry", key)
		}
		v, err := strconv.Unquote(e.DefaultLiteral)
		if err != nil {
			t.Fatalf("%s: default %q: %v", key, e.DefaultLiteral, err)
		}
		defaults.Values[key] = configload.Value{Data: v}
	}
	for _, to := range []display.Destination{display.Terminal, display.Elsewhere, display.File} {
		zero, builtin := jobexec.DisplayLineStyle(configload.Snapshot{}, to), jobexec.DisplayLineStyle(defaults, to)
		if zero.Enabled != builtin.Enabled || zero.Error != builtin.Error || zero.Warning != builtin.Warning {
			t.Fatalf("destination %d: the zero snapshot's style %+v, the defaults' %+v", to, zero, builtin)
		}
	}
}
