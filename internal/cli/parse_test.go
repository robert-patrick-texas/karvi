package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
)

func mustParse(t *testing.T, args ...string) *Invocation {
	t.Helper()
	inv, err := Parse(args)
	if err != nil {
		t.Fatalf("Parse(%q): %v", args, err)
	}
	return inv
}

func parseCode(t *testing.T, args ...string) (string, string) {
	t.Helper()
	_, err := Parse(args)
	if err == nil {
		t.Fatalf("Parse(%q): expected an error", args)
	}
	return errorcodes.Of(err), err.Error()
}

func targetValues(inv *Invocation) []string {
	var out []string
	for _, tgt := range inv.Targets {
		out = append(out, tgt.Kind+"="+tgt.Value)
	}
	return out
}

// TestParseADR0002Rows covers the normative acceptance cases for the
// table-driven parser, row by row.
func TestParseADR0002Rows(t *testing.T) {
	type row struct {
		n        int
		args     []string
		commands []string
		targets  []string
		echo     bool
		code     string
		mentions []string
	}
	rows := []row{
		{1, []string{"com", "--ech", "router01", "show", "ip", "route"}, []string{"show ip route"}, []string{"target=router01"}, true, "", nil},
		{2, []string{"command", "router01", "--echo", "show", "ip", "route"}, []string{"show ip route"}, []string{"target=router01"}, true, "", nil},
		{3, []string{"command", "router01", "show", "ip", "route", "--echo"}, []string{"show ip route --echo"}, []string{"target=router01"}, false, "", nil},
		{4, []string{"command", "router01", "traceroute", "-4", "192.0.2.1"}, []string{"traceroute -4 192.0.2.1"}, []string{"target=router01"}, false, "", nil},
		{5, []string{"command", "router01", "show", "run", "|", "grep", "-e", "bgp"}, []string{"show run | grep -e bgp"}, []string{"target=router01"}, false, "", nil},
		{6, []string{"command", "router01", "show", "interfaces", "|", "grep", "-h", "up"}, []string{"show interfaces | grep -h up"}, []string{"target=router01"}, false, "", nil},
		{7, []string{"command", "--echo", "router01", "show ip route", "show clock"}, []string{"show ip route show clock"}, []string{"target=router01"}, true, "", nil},
		{8, []string{"command", "--echo", "router01", "--cmd", "show ip route", "--cmd", "show clock"}, []string{"show ip route", "show clock"}, []string{"target=router01"}, true, "", nil},
		{9, []string{"command", "--targ", "router01", "--command", "show ip route", "--ech"}, []string{"show ip route"}, []string{"target=router01"}, true, "", nil},
		{10, []string{"command", "--tar", "router01", "--co", "show ip route"}, nil, nil, false, "cli_option_ambiguous", []string{"--cmd (as --command)", "--continue-device-on-error"}},
		{11, []string{"co", "router01", "show", "clock"}, nil, nil, false, "cli_command_ambiguous", []string{"command", "config"}},
		{12, []string{"command", "router01", "show", "clock", "--cmd", "show version"}, []string{"show clock --cmd show version"}, []string{"target=router01"}, false, "", nil},
		{13, []string{"command", "--echo", "router01", "--", "-example", "device", "text"}, []string{"-example device text"}, []string{"target=router01"}, true, "", nil},
		{14, []string{"run", "--host", "router01", "show", "clock", "--echo"}, []string{"show clock --echo"}, []string{"target=router01"}, false, "", nil},
		{15, []string{"run", "--targ", "router01", "show", "clock", "--echo"}, []string{"show clock --echo"}, []string{"target=router01"}, false, "", nil},
	}
	for _, r := range rows {
		inv, err := Parse(r.args)
		if r.code != "" {
			if err == nil {
				t.Errorf("row %d: expected %s, parsed %+v", r.n, r.code, inv)
				continue
			}
			if got := errorcodes.Of(err); got != r.code {
				t.Errorf("row %d: code %s, want %s (%v)", r.n, got, r.code, err)
			}
			for _, m := range r.mentions {
				if !strings.Contains(err.Error(), m) {
					t.Errorf("row %d: message %q does not name %q", r.n, err.Error(), m)
				}
			}
			continue
		}
		if err != nil {
			t.Errorf("row %d: %v", r.n, err)
			continue
		}
		if inv.Help {
			t.Errorf("row %d: help must not be recognized", r.n)
		}
		if !reflect.DeepEqual(inv.Commands, r.commands) {
			t.Errorf("row %d: commands %q, want %q", r.n, inv.Commands, r.commands)
		}
		if got := targetValues(inv); !reflect.DeepEqual(got, r.targets) {
			t.Errorf("row %d: targets %q, want %q", r.n, got, r.targets)
		}
		if inv.Flag(optEcho) != r.echo {
			t.Errorf("row %d: echo=%v, want %v", r.n, inv.Flag(optEcho), r.echo)
		}
		if r.n == 4 && inv.Flag(optIPv4) {
			t.Errorf("row 4: -4 inside device text must not set --ipv4")
		}
	}
}

// sampleValue returns a value that fits the option's type.
func sampleValue(o *option) string {
	if o == optExpect {
		return "x=" // the --expect grammar: a nonempty PATTERN before the first =
	}
	switch o.typ {
	case typeInt:
		return "1"
	case typeDuration:
		return "1s"
	case typeEnum:
		return o.enum[0]
	}
	return "x"
}

// baseArgs returns an invocation of cmd that is valid on its own, with a
// marker for where an extra option is inserted.
func baseArgs(path string) (before, after []string) {
	switch path {
	case "login":
		return []string{"login", "r1"}, nil
	case "command":
		return []string{"command", "--target", "r1", "--cmd", "show clock"}, nil
	case "run":
		return []string{"run", "--target", "r1", "--cmd", "show clock"}, nil
	case "crun":
		return []string{"crun", "--target", "r1", "--cmd", "show clock"}, nil
	case "job cancel":
		return []string{"job", "cancel", "260915-170000-00"}, nil
	case "job follow":
		return []string{"job", "follow", "260915-170000-00"}, nil
	}
	return strings.Fields(path), nil
}

// expectedResolution is the independent oracle for word resolution: an exact name or
// alias wins; otherwise every option with a name or alias having the prefix.
func expectedResolution(prefix string, opts []*option) (exact *option, matches []*option) {
	for _, o := range opts {
		if o.name == prefix {
			return o, nil
		}
		for _, a := range o.aliases {
			if a == prefix {
				return o, nil
			}
		}
	}
	for _, o := range opts {
		hit := strings.HasPrefix(o.name, prefix)
		for _, a := range o.aliases {
			hit = hit || strings.HasPrefix(a, prefix)
		}
		if hit {
			matches = append(matches, o)
		}
	}
	return nil, matches
}

func allCommands() []*command {
	var out []*command
	for _, c := range commandTable {
		out = append(out, c)
		out = append(out, c.subs...)
	}
	return out
}

// TestParseEveryPrefixOfEveryOption parses every prefix of every option name
// and alias in every mode and compares the outcome with the oracle.
func TestParseEveryPrefixOfEveryOption(t *testing.T) {
	type region struct {
		name string
		opts []*option
		make func(optArgs []string) []string
	}
	var regions []region
	regions = append(regions, region{"global", globalOptionTable, func(a []string) []string { return append(a, "version") }})
	for _, c := range allCommands() {
		if c.hidden || c.subs != nil {
			continue
		}
		c := c
		before, after := baseArgs(c.path)
		regions = append(regions, region{c.path, c.options, func(a []string) []string {
			out := append(append([]string(nil), before...), a...)
			return append(out, after...)
		}})
	}
	cases := 0
	for _, r := range regions {
		for _, o := range r.opts {
			words := append([]string{o.name}, o.aliases...)
			for _, w := range words {
				for n := 1; n <= len(w); n++ {
					prefix := w[:n]
					exact, matches := expectedResolution(prefix, r.opts)
					want := exact
					if want == nil && len(matches) == 1 {
						want = matches[0]
					}
					optArgs := []string{"--" + prefix}
					if want != nil && want.kind == kindValue {
						optArgs = append(optArgs, sampleValue(want))
					}
					args := r.make(optArgs)
					inv, err := Parse(args)
					cases++
					switch {
					case want != nil:
						if err != nil {
							t.Errorf("%s: %q: %v; want --%s", r.name, args, err, want.name)
							continue
						}
						if want.role == roleHelp {
							if !inv.Help {
								t.Errorf("%s: %q: help not recognized", r.name, args)
							}
						} else if !inv.Set(want) {
							t.Errorf("%s: %q: --%s not set", r.name, args, want.name)
						}
					default:
						if err == nil {
							t.Errorf("%s: %q: parsed, want ambiguity among %d options", r.name, args, len(matches))
							continue
						}
						if errorcodes.Of(err) != "cli_option_ambiguous" {
							t.Errorf("%s: %q: %v, want cli_option_ambiguous", r.name, args, err)
							continue
						}
						for _, m := range matches {
							if !strings.Contains(err.Error(), "--"+m.name) {
								t.Errorf("%s: %q: candidates %q do not name --%s", r.name, args, err.Error(), m.name)
							}
						}
					}
				}
			}
		}
	}
	if cases < 1000 {
		t.Fatalf("only %d prefix cases generated", cases)
	}
}

// TestParseEveryPrefixOfEveryWord does the same for command and subcommand
// words.
func TestParseEveryPrefixOfEveryWord(t *testing.T) {
	suffix := map[string][]string{
		"login": {"r1"}, "command": {"r1", "show", "clock"}, "run": {"--target", "r1", "show", "clock"},
		"daemon": {"status"}, "config": {"show"}, "job": {"cancel", "260915-170000-00"}, "cancel": {"260915-170000-00"}, "follow": {"260915-170000-00"},
		"setup": {"shared"},
	}
	check := func(parent []string, cmds []*command, code string) {
		words := commandWords(cmds)
		for _, c := range cmds {
			for _, w := range append([]string{c.word}, c.aliases...) {
				for n := 1; n <= len(w); n++ {
					prefix := w[:n]
					wantWord, cands := resolveWord(prefix, "", words)
					// Independent oracle.
					var oracle []string
					exactHit := false
					for _, e := range words {
						if e.word == prefix {
							exactHit = true
							oracle = []string{e.canon}
							break
						}
					}
					if !exactHit {
						seen := map[string]bool{}
						for _, e := range words {
							if strings.HasPrefix(e.word, prefix) && !seen[e.canon] {
								seen[e.canon] = true
								oracle = append(oracle, e.canon)
							}
						}
					}
					args := append(append([]string(nil), parent...), prefix)
					if len(oracle) == 1 {
						args = append(args, suffix[oracle[0]]...)
					}
					inv, err := Parse(args)
					if len(oracle) == 1 {
						if wantWord != oracle[0] {
							t.Fatalf("resolveWord(%q) = %q, oracle %q", prefix, wantWord, oracle)
						}
						if err != nil {
							t.Errorf("%q: %v", args, err)
							continue
						}
						wantPath := strings.TrimSpace(strings.Join(parent, " ") + " " + oracle[0])
						if oracle[0] == "daemon" {
							wantPath = "daemon status"
						} else if oracle[0] == "config" {
							wantPath = "config show"
						} else if oracle[0] == "job" {
							wantPath = "job cancel"
						} else if oracle[0] == "setup" {
							wantPath = "setup shared"
						}
						if inv.Path != wantPath {
							t.Errorf("%q: path %q, want %q", args, inv.Path, wantPath)
						}
						continue
					}
					if err == nil || errorcodes.Of(err) != code {
						t.Errorf("%q: %v, want %s among %q (candidates %q)", args, err, code, oracle, cands)
						continue
					}
					for _, o := range oracle {
						if !strings.Contains(err.Error(), o) {
							t.Errorf("%q: %q does not name %s", args, err.Error(), o)
						}
					}
				}
			}
		}
	}
	check(nil, commandTable, "cli_command_ambiguous")
	check([]string{"daemon"}, lookupCommand("daemon").subs, "cli_subcommand_ambiguous")
	check([]string{"config"}, lookupCommand("config").subs, "cli_subcommand_ambiguous")
}

func TestParseAliases(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want func(*Invocation) bool
	}{
		{[]string{"-h"}, func(i *Invocation) bool { return i.Help && i.Path == "" }},
		{[]string{"--h"}, func(i *Invocation) bool { return i.Help }},
		{[]string{"command", "-h"}, func(i *Invocation) bool { return i.Help && i.Path == "command" }},
		{[]string{"login", "router01", "-h"}, func(i *Invocation) bool { return i.Help && i.Path == "login" }},
		{[]string{"run", "--h"}, func(i *Invocation) bool { return i.Help && i.Path == "run" }},
		{[]string{"daemon", "stop", "-h"}, func(i *Invocation) bool { return i.Help && i.Path == "daemon stop" }},
		{[]string{"command", "-t", "router01", "-c", "show clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(targetValues(i), []string{"target=router01"}) && reflect.DeepEqual(i.Commands, []string{"show clock"})
		}},
		{[]string{"command", "--host", "router01", "--command", "show clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(targetValues(i), []string{"target=router01"}) && reflect.DeepEqual(i.Commands, []string{"show clock"})
		}},
		{[]string{"run", "-t", "r1", "-t", "r2", "--c", "show clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(targetValues(i), []string{"target=r1", "target=r2"})
		}},
		{[]string{"command", "-a", "192.0.2.10", "-t", "router01", "-c", "show clock"}, func(i *Invocation) bool {
			return i.String(optAddress) == "192.0.2.10" && len(i.Targets) == 1
		}},
		{[]string{"login", "--address", "192.0.2.10", "router01"}, func(i *Invocation) bool {
			return i.String(optAddress) == "192.0.2.10" && targetValues(i)[0] == "target=router01"
		}},
		{[]string{"--cfg", "/etc/karvi", "cmd", "router01", "show", "clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(i.Global.configs, []string{"/etc/karvi"}) && i.Path == "command"
		}},
		{[]string{"-cfg", "/etc/karvi", "cmd", "router01", "show", "clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(i.Global.configs, []string{"/etc/karvi"})
		}},
		{[]string{"-c", "site.toml", "cmd", "router01", "show", "clock"}, func(i *Invocation) bool {
			return reflect.DeepEqual(i.Global.configs, []string{"site.toml"})
		}},
		{[]string{"--4", "login", "r1"}, func(i *Invocation) bool { return i.Global.ipv4 }},
		{[]string{"login", "r1", "-6"}, func(i *Invocation) bool { return i.Flag(optIPv6) }},
		{[]string{"login", "--rec", "router01"}, func(i *Invocation) bool {
			return i.Record != nil && *i.Record == "" && targetValues(i)[0] == "target=router01"
		}},
		{[]string{"login", "router01", "--record=/tmp/x"}, func(i *Invocation) bool { return i.Record != nil && *i.Record == "/tmp/x" }},
		{[]string{"login", "router01", "--rec=session.log"}, func(i *Invocation) bool { return *i.Record == "session.log" }},
	} {
		inv := mustParse(t, tc.args...)
		if !tc.want(inv) {
			t.Errorf("%q: unexpected result %+v (targets %q)", tc.args, inv, targetValues(inv))
		}
	}
	// Aliases are valid only where defined.
	if code, _ := parseCode(t, "command", "--cfg", "x", "r1", "show", "clock"); code != "cli_option_unknown" {
		t.Errorf("--cfg after the command word: %s", code)
	}
	// After the command word, -c is --cmd; before it, -c is --config.
	if inv := mustParse(t, "command", "-c", "show clock", "-t", "r1"); inv.Commands[0] != "show clock" {
		t.Errorf("-c after command word: %+v", inv)
	}
	// --h is never --host.
	if inv := mustParse(t, "command", "--h"); !inv.Help {
		t.Errorf("--h must be --help")
	}
}

// TestParseOneOrTwoDashes spells every option of every mode with one and two
// dashes, with and without an inline value.
func TestParseOneOrTwoDashes(t *testing.T) {
	for _, c := range allCommands() {
		if c.hidden || c.subs != nil {
			continue
		}
		before, _ := baseArgs(c.path)
		for _, o := range c.options {
			if o.role == roleHelp {
				continue
			}
			var forms [][]string
			switch o.kind {
			case kindFlag:
				forms = [][]string{{"-" + o.name}, {"--" + o.name}, {"-" + o.name + "=true"}, {"--" + o.name + "=false"}}
			case kindOptional:
				forms = [][]string{{"-" + o.name}, {"--" + o.name}, {"-" + o.name + "=p"}, {"--" + o.name + "=p"}}
			default:
				v := sampleValue(o)
				forms = [][]string{{"-" + o.name, v}, {"--" + o.name, v}, {"-" + o.name + "=" + v}, {"--" + o.name + "=" + v}}
			}
			for _, f := range forms {
				args := append(append([]string(nil), before...), f...)
				inv, err := Parse(args)
				if err != nil {
					t.Errorf("%q: %v", args, err)
					continue
				}
				if !inv.Set(o) {
					t.Errorf("%q: --%s not set", args, o.name)
				}
				if o.kind == kindFlag && strings.HasSuffix(f[0], "=false") && inv.Flag(o) {
					t.Errorf("%q: inline false must turn the option off", args)
				}
			}
		}
	}
}

func TestParseRemovedSpellingsAreUnknown(t *testing.T) {
	for _, tc := range []struct {
		mode []string
		old  string
	}{
		{[]string{"command", "r1"}, "--commands-file"},
		{[]string{"run"}, "--commands-file"},
		{[]string{"run"}, "--targets-file"},
		{[]string{"run"}, "--targets"},
		{[]string{"command", "r1"}, "--host-key-policy"},
		{[]string{"login", "r1"}, "--host-key-policy"},
		{[]string{"run"}, "--host-key-policy"},
		{[]string{"command", "r1"}, "--known-hosts-file"},
		{[]string{"login", "r1"}, "--known-hosts-file"},
		{[]string{"run"}, "--known-hosts-file"},
		{[]string{"login", "r1"}, "--record-file"},
		{[]string{"login", "r1"}, "--record-path"},
	} {
		args := append(append([]string(nil), tc.mode...), tc.old, "x")
		code, msg := parseCode(t, args...)
		if code != "cli_option_unknown" {
			t.Errorf("%q: %s (%s), want cli_option_unknown", args, code, msg)
		}
	}
}

// TestParseEveryCode triggers each parser code and the parse-time
// commands-file cardinality rule.
func TestParseEveryCode(t *testing.T) {
	cases := map[string][][]string{
		"cli_command_unknown":       {{"bogus"}, {"lgoin", "r1"}},
		"cli_command_ambiguous":     {{"co"}, {"c", "r1", "show", "clock"}},
		"cli_subcommand_missing":    {{"daemon"}, {"config"}},
		"cli_subcommand_unknown":    {{"daemon", "bounce"}, {"config", "edit"}},
		"cli_subcommand_ambiguous":  {{"daemon", "s"}, {"daemon", "st"}, {"daemon", "sta"}},
		"cli_option_unknown":        {{"--bogus", "version"}, {"command", "--bogus", "r1", "show", "clock"}, {"daemon", "--grace", "stop"}, {"run", "--quiet", "--target", "r1", "show", "clock"}},
		"cli_option_ambiguous":      {{"--d", "version"}, {"command", "--p", "x", "r1", "show", "clock"}, {"run", "--fo", "json", "--target", "r1", "show", "clock"}, {"login", "--ad", "x", "r1"}},
		"cli_option_value_missing":  {{"command", "r1", "--port"}, {"run", "--target"}, {"login", "r1", "--platform"}, {"--config"}},
		"cli_option_value_detached": {{"run", "--target", "r1", "--of", "/tmp/x", "--cmd", "show clock"}, {"run", "--target", "r1", "--of", "./x", "show", "clock"}, {"command", "--of", "~/x", "r1", "show", "clock"}, {"command", "--of", "..", "r1", "show", "clock"}, {"login", "--record", "/tmp/y", "r1"}, {"login", "--rec", ".", "r1"}, {"crun", "--all", "--cd", "/tmp/c"}},
		"cli_option_value_invalid":  {{"command", "r1", "--echo=maybe", "show", "clock"}, {"command", "r1", "--echo=1", "show", "clock"}, {"command", "r1", "--port", "abc", "show", "clock"}, {"command", "r1", "--blind-wait", "5x", "show", "clock"}, {"command", "r1", "--format", "xml", "show", "clock"}, {"run", "--dispatch", "fast", "--target", "r1", "show", "clock"}, {"--ansi", "never", "version"}, {"daemon", "stop", "--after=soon"}, {"version", "--format", "yaml"}},
		"cli_device_missing":        {{"command", "--cmd", "show clock"}, {"command", "--address", "192.0.2.10", "--cmd", "show clock"}, {"login"}, {"login", "--address", "192.0.2.10"}, {"login", "--quiet"}},
		"cli_device_empty":          {{"command", "", "show", "clock"}, {"command", " ", "show", "clock"}, {"command", "--target", "", "--cmd", "x"}, {"login", ""}, {"login", "--host", " "}, {"command", "--", "", "show"}},
		"cli_command_text_missing":  {{"command", "r1"}, {"command", "--target", "r1"}, {"run", "--target", "r1"}, {"run"}},
		"cli_command_text_mixed":    {{"command", "r1", "--cmd", "show clock", "show", "version"}, {"command", "--target", "r1", "--cf", "f", "show", "clock"}, {"run", "--target", "r1", "--cmd", "x", "show", "clock"}, {"command", "r1", "--cmd", "x", "--", "y"}},
		"cli_positional_missing":    {{"job", "cancel"}, {"job", "cancel", "--reason", "x"}, {"j", "c"}, {"job", "follow"}, {"j", "f", "--format", "jsonl"}},
		"cli_positional_unexpected": {{"version", "extra"}, {"watch", "extra"}, {"daemon", "status", "extra"}, {"daemon", "stop", "now"}, {"job", "cancel", "a", "b"}, {"job", "follow", "a", "b"}, {"config", "generate", "a", "b"}, {"config", "show", "a", "b"}, {"config", "validate", "--", "a", "b"}},
		"commands_file_repeated":    {{"command", "r1", "--cf", "a", "--cf", "b"}, {"run", "--target", "r1", "--cf", "a", "--cf", "a"}},
		// Malformed selectors, reserved ^ and $, a bare !, and a ! in
		// --exclude are refused at parse time.
		"target_selector_pattern_invalid": {{"run", "--site", "ny[c", "--all", "show", "clock"}, {"run", "--target", "^r1", "show", "clock"}, {"command", "r1$", "show", "clock"}, {"command", "--target", "r[!]", "--cmd", "x"}, {"run", "--all", "--exclude", "!r1", "show", "clock"}, {"run", "--device-group", "!", "--all", "show", "clock"}, {"run", "--select-platform", `cisco\`, "--all", "show", "clock"}, {"login", "--exclude", "r[c-a]", "r1"}},
	}
	for want, argsList := range cases {
		for _, args := range argsList {
			code, msg := parseCode(t, args...)
			if code != want {
				t.Errorf("%q: %s (%s), want %s", args, code, msg, want)
			}
		}
	}
	// Ambiguity messages name every candidate.
	_, msg := parseCode(t, "daemon", "s")
	for _, w := range []string{"start", "stop", "restart", "status", "serve"} {
		if w != "restart" && !strings.Contains(msg, w) {
			t.Errorf("daemon s: %q lacks %s", msg, w)
		}
	}
	if strings.Contains(msg, "restart") {
		t.Errorf("daemon s: %q must not name restart", msg)
	}
	_, msg = parseCode(t, "--d", "version")
	if !strings.Contains(msg, "--debug") || !strings.Contains(msg, "--debug-show-secrets") {
		t.Errorf("--d: %q", msg)
	}
}

func TestParsePlainCommands(t *testing.T) {
	inv := mustParse(t, "config", "validate", "site.toml", "--format", "json")
	if inv.Path != "config validate" || inv.String(optFormatTJ) != "json" || !reflect.DeepEqual(inv.Positional, []string{"site.toml"}) {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "config", "validate", "--", "-weird.toml")
	if !reflect.DeepEqual(inv.Positional, []string{"-weird.toml"}) {
		t.Fatalf("-- must make the next argument positional: %+v", inv)
	}
	inv = mustParse(t, "config", "show", "--explain", "ssh.host-key-policy")
	if !inv.Flag(optExplain) || inv.Positional[0] != "ssh.host-key-policy" {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "daemon", "stop", "--after", "90s")
	if inv.Duration(optAfter).Seconds() != 90 {
		t.Fatalf("--after 90s: %+v", inv)
	}
	inv = mustParse(t, "daemon", "restart", "--after=5m")
	if inv.Path != "daemon restart" || inv.Duration(optAfter).Minutes() != 5 {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "daemon", "star")
	if inv.Path != "daemon start" {
		t.Fatalf("daemon star: %q", inv.Path)
	}
	inv = mustParse(t, "login", "--quiet", "--", "-r1")
	if targetValues(inv)[0] != "target=-r1" || !inv.Flag(optQuiet) {
		t.Fatalf("login -- -r1: %+v", inv)
	}
	inv = mustParse(t, "login", "r1", "--port=2222", "--ssh-host-key-policy", "secure")
	if inv.Int(optPort) != 2222 || inv.String(optHostKeyPolicy) != "secure" {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "daemon", "--help")
	if !inv.Help || inv.Path != "daemon" {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "daemon", "help")
	if !inv.Help {
		t.Fatalf("daemon help: %+v", inv)
	}
	inv = mustParse(t, "help")
	if !inv.Help || inv.helpText() != topHelp {
		t.Fatalf("help: %+v", inv)
	}
	inv = mustParse(t, "--version")
	if !inv.Global.version || inv.Path != "" {
		t.Fatalf("--version: %+v", inv)
	}
	inv = mustParse(t)
	if inv.Path != "" || inv.Help {
		t.Fatalf("no arguments: %+v", inv)
	}
}

// TestParseFreeformBoundary covers where freeform text begins in command.
func TestParseFreeformBoundary(t *testing.T) {
	inv := mustParse(t, "command", "--tf", "list.txt", "show", "clock")
	if !reflect.DeepEqual(targetValues(inv), []string{"tf=list.txt"}) || inv.Commands[0] != "show clock" || !inv.Freeform {
		t.Fatalf("a target input before the first word makes it freeform: %+v %q", inv, targetValues(inv))
	}
	if len(inv.Pending) != 0 {
		t.Fatalf("--tf in command is built: %+v", inv.Pending)
	}
	// Every target input of run is accepted by login and command, and a
	// selector before the first word makes that word freeform.
	inv = mustParse(t, "command", "--site", "core", "--device-group", "edge", "--all", "--exclude", "r9", "show", "clock")
	if !reflect.DeepEqual(targetValues(inv), []string{"site=core", "device-group=edge", "all=true"}) || inv.Commands[0] != "show clock" || inv.Strings(optExclude)[0] != "r9" || len(inv.Pending) != 0 {
		t.Fatalf("%q %+v", targetValues(inv), inv)
	}
	inv = mustParse(t, "login", "r1", "r2", "--tf", "list.txt", "--site", "core")
	if !reflect.DeepEqual(targetValues(inv), []string{"target=r1", "target=r2", "tf=list.txt", "site=core"}) || len(inv.Pending) != 0 {
		t.Fatalf("login target set: %q", targetValues(inv))
	}
	// --select-platform is a selector at its position, recorded
	// under the kind "platform"; --platform names the platform definition in
	// all three modes and is not a target input, and a shortcut is --platform
	// with its value, the last spelling winning.
	for _, mode := range []string{"run", "command", "login"} {
		args := []string{mode, "--target", "r1", "--select-platform", "cisco_*", "--target", "r2", "--platform", "generic", "--pn"}
		if mode != "login" {
			args = append(args, "show", "clock")
		}
		inv = mustParse(t, args...)
		if !reflect.DeepEqual(targetValues(inv), []string{"target=r1", "platform=cisco_*", "target=r2"}) || inv.String(optPlatform) != "cisco_nxos" {
			t.Fatalf("%s: targets %q platform %q", mode, targetValues(inv), inv.String(optPlatform))
		}
	}
	// --pi is the IOS XE shortcut, a whole word; --pin is still --ping.
	if inv = mustParse(t, "run", "--all", "--pin", "show", "clock"); !inv.Flag(optPing) || inv.Set(optPlatform) {
		t.Fatalf("--pin is not --ping")
	}
	if inv = mustParse(t, "run", "--all", "--pi", "show", "clock"); inv.Flag(optPing) {
		t.Fatalf("--pi is still --ping")
	}
	for short, want := range map[string]string{"--pi": "cisco_iosxe", "--pn": "cisco_nxos", "--pr": "cisco_iosxr", "--pj": "juniper_junos", "-pa": "arista_eos", "--pg": "generic"} {
		if inv = mustParse(t, "run", "--all", short, "show", "clock"); inv.String(optPlatform) != want {
			t.Fatalf("%s is --platform %q, want %q", short, inv.String(optPlatform), want)
		}
	}
	inv = mustParse(t, "command", "--platform", "cisco_iosxe", "r1", "show", "clock")
	if !reflect.DeepEqual(targetValues(inv), []string{"target=r1"}) || inv.String(optPlatform) != "cisco_iosxe" {
		t.Fatalf("command --platform is not a target input: %q", targetValues(inv))
	}
	// --order sets dispatch.order through the lock-aware flag layer in
	// every connection mode; its value is checked by the parser.
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"login", "r1", "--order", "sorted"}, "sorted"},
		{[]string{"command", "--order", "shuffle", "r1", "show", "clock"}, "shuffle"},
		{[]string{"run", "--target", "r1", "--order=random", "show", "clock"}, "random"},
		{[]string{"run", "--target", "r1", "show", "clock"}, ""},
	} {
		inv = mustParse(t, tc.args...)
		got, _ := inv.common().ConfigFlags["dispatch.order"].Value.(string)
		if got != tc.want {
			t.Fatalf("%v: dispatch.order flag=%q, want %q", tc.args, got, tc.want)
		}
	}
	if code, _ := parseCode(t, "login", "r1", "--order", "inventory"); code != "cli_option_value_invalid" {
		t.Fatalf("--order inventory: %s", code)
	}
	// --all=false contributes nothing and is not a target input.
	if code, _ := parseCode(t, "login", "--all=false"); code != "cli_device_missing" {
		t.Fatalf("login --all=false: %s", code)
	}
	inv = mustParse(t, "run", "--all=false", "--target", "r1", "show", "clock")
	if !reflect.DeepEqual(targetValues(inv), []string{"target=r1"}) {
		t.Fatalf("run --all=false: %q", targetValues(inv))
	}
	inv = mustParse(t, "command", "--target", "r1", "--", "-x", "--echo")
	if inv.Commands[0] != "-x --echo" || len(inv.Targets) != 1 {
		t.Fatalf("-- after a target input begins freeform: %+v", inv)
	}
	inv = mustParse(t, "command", "--", "r1", "-x")
	if targetValues(inv)[0] != "target=r1" || inv.Commands[0] != "-x" {
		t.Fatalf("-- without a target input: next argument is the device: %+v", inv)
	}
	inv = mustParse(t, "command", "--", "-r1", "show", "clock")
	if targetValues(inv)[0] != "target=-r1" {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "run", "--target", "r1", "--", "--echo", "-h")
	if inv.Commands[0] != "--echo -h" || inv.Help {
		t.Fatalf("%+v", inv)
	}
	inv = mustParse(t, "command", "r1", "-h")
	if !inv.Help {
		t.Fatal("help between the device and the first word is help")
	}
	inv = mustParse(t, "command", "r1", "show", "-h")
	if inv.Help || inv.Commands[0] != "show -h" {
		t.Fatalf("help after freeform is device text: %+v", inv)
	}
	inv = mustParse(t, "command", "r1", "show", "clock", "--", "x")
	if inv.Commands[0] != "show clock -- x" {
		t.Fatalf("-- after freeform is device text: %+v", inv)
	}
	inv = mustParse(t, "command", "r1", "  show   clock  ")
	if inv.Commands[0] != "  show   clock  " {
		t.Fatalf("spacing inside an argument is preserved: %q", inv.Commands[0])
	}
	inv = mustParse(t, "command", "--echo", "r1", "--border", "show", "clock")
	if !inv.Flag(optEcho) || !inv.Flag(optBorder) || inv.Commands[0] != "show clock" {
		t.Fatalf("%+v", inv)
	}
	// Explicit form: options in any order.
	inv = mustParse(t, "command", "--cmd", "show clock", "--address", "192.0.2.10", "--target", "router1", "--command", "show version", "--echo")
	if !reflect.DeepEqual(inv.Commands, []string{"show clock", "show version"}) || targetValues(inv)[0] != "target=router1" || !inv.Flag(optEcho) {
		t.Fatalf("%+v", inv)
	}
	// Positional device then --target: both are targets, in order.
	inv = mustParse(t, "command", "r1", "--target", "r2", "--cmd", "x")
	if !reflect.DeepEqual(targetValues(inv), []string{"target=r1", "target=r2"}) || !inv.Targets[0].Positional || inv.Targets[1].Positional {
		t.Fatalf("%q", targetValues(inv))
	}
	// run: the first positional word begins freeform even when it looks like a device.
	inv = mustParse(t, "run", "--all", "r1", "--echo")
	if inv.Commands[0] != "r1 --echo" || targetValues(inv)[0] != "all=true" {
		t.Fatalf("%+v %q", inv, targetValues(inv))
	}
}

func TestParseGlobalRegion(t *testing.T) {
	inv := mustParse(t, "--set", "a=1", "--quiet", "--debug", "--timezone", "UTC", "--ansi=strip", "-6", "--", "login", "r1")
	if !reflect.DeepEqual(inv.Global.sets, []string{"a=1"}) || !inv.Global.quiet || !inv.Global.debug || inv.Global.timezone != "UTC" || inv.Global.ansi != "strip" || !inv.Global.ipv6 || inv.Path != "login" {
		t.Fatalf("%+v", inv.Global)
	}
	inv = mustParse(t, "--debug", "--debug-show-secrets", "version")
	if !inv.Global.debugShow {
		t.Fatalf("%+v", inv.Global)
	}
	// --debug after the command word is merged by common().
	inv = mustParse(t, "command", "r1", "--debug", "--quiet", "show", "clock")
	if c := inv.common(); !c.Debug || !c.Quiet {
		t.Fatalf("%+v", c)
	}
	inv = mustParse(t, "command", "r1", "--ssh-host-key-policy", "insecure", "--ssh-known-hosts-file", "auto", "show", "clock")
	if c := inv.common(); c.ConfigFlags["ssh.host-key-policy"] != (configload.FlagValue{Value: "insecure", Option: "--ssh-host-key-policy"}) || c.ConfigFlags["ssh.known-hosts-file"].Value != "auto" {
		t.Fatalf("%+v", c.ConfigFlags)
	}
}

// TestMainParserDiagnostics checks the parser's diagnostics at the process
// boundary: usage errors go to stderr with the code first and
// leave stdout empty; help and version exit 0 and create nothing.
func TestMainParserDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"co", "r1", "show", "clock"}, "cli_command_ambiguous"},
		{[]string{"command", "--tar", "r1", "--co", "x"}, "cli_option_ambiguous"},
		{[]string{"command", "r1"}, "cli_command_text_missing"},
		{[]string{"login"}, "cli_device_missing"},
		{[]string{"version", "x"}, "cli_positional_unexpected"},
		{[]string{"--debug-show-secrets", "version"}, "debug_show_secrets_requires_debug"},
		{[]string{"command", "r1", "--tfr", "-", "show", "clock"}, "target_source_stdin_invalid"},
		{[]string{"run", "--noping", "--ping", "--target", "r1", "show", "clock"}, "ping_flag_conflict"},
		{[]string{"login", "r1", "--ssh-option", "a=b"}, "cli_option_unavailable"},
		{[]string{"command", "--tf", "-", "--cf", "-"}, "stdin_source_repeated"},
		{[]string{"login", "r1", "--tf", "-", "--tf", "-"}, "stdin_source_repeated"},
		{[]string{"login", "r1", "--port", "70000"}, "port_out_of_range"},
		{[]string{"command", "r1", "--port", "0", "show", "clock"}, "port_out_of_range"},
		{[]string{"command", "r1", "--blind-return", "-1", "show", "clock"}, "blind_return_out_of_range"},
		{[]string{"command", "r1", "--blind-return", "21", "reload"}, "blind_return_out_of_range"},
		{[]string{"run", "--target", "r1", "--cmd", `reload` + strings.Repeat(`\r`, 21)}, "blind_return_out_of_range"},
		{[]string{"command", "r1", "--blind-return", "1", "--cmd", "clear counters", "--cmd", "show clock"}, "declaration_before_command"},
		{[]string{"run", "--target", "r1", "--cf", "-", "--expect", "confirm="}, "declaration_with_commands_file"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "confirm"}, "expect_malformed"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "confirm(=y"}, "expect_pattern_invalid"},
		{append([]string{"run", "--target", "r1", "--cmd", "ping"}, strings.Split(strings.Repeat("--expect p= ", 21), " ")[:42]...), "expect_too_many"},
		{[]string{"command", "r1", "--cmd", `clear counters\r`, "--expect", `confirm\]=`}, "expect_with_blind_return"},
		{[]string{"command", "r1", "--cmd", `reload\r`, "--blind-return", "1"}, "blind_return_conflict"},
		{[]string{"run", "--target", "r1", "--cf", "-", "--tf", "-"}, "stdin_source_repeated"},
		{[]string{"run", "--no-daemon", "show", "clock"}, "inventory_positive_selector_missing"},
		{[]string{"--debug", "--debug-show-secrets", "login", "--record", "r1"}, "record_debug_show_secrets_conflict"},
		{[]string{"daemon", "stop", "--grace", "--force"}, "daemon_stop_options_conflict"},
		{[]string{"daemon", "stop", "--after=0s"}, "daemon_stop_after_not_positive"},
		{[]string{"config", "generate", "--minimal", "--full"}, "config_generate_mode_conflict"},
		{[]string{"watch"}, "watch_tui_requires_terminal"},
		{[]string{"watch", "--format", "json", "--filter", "lee"}, "watch_json_filter_unsupported"},
		{[]string{"watch", "--format", "json", "--sort", "fail"}, "watch_json_filter_unsupported"},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			got := Main(tc.args, strings.NewReader(""), &stdout, &stderr)
			wantExit := exitcode.ExitUsageError
			if tc.code == "inventory_positive_selector_missing" {
				wantExit = exitcode.ExitInventoryError
			}
			if got != wantExit {
				t.Fatalf("exit=%d, want %d; stderr=%q", got, wantExit, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout must stay empty, got %q", stdout.String())
			}
			if !strings.HasPrefix(stderr.String(), tc.code+": ") {
				t.Fatalf("stderr=%q, want prefix %q", stderr.String(), tc.code+": ")
			}
			if strings.Count(stderr.String(), tc.code) != 1 {
				t.Fatalf("code must appear once: %q", stderr.String())
			}
		})
	}
	for _, args := range [][]string{{"--help"}, {"-h"}, {}, {"help"}, {"command", "--help"}, {"cmd", "-h"}, {"run", "--he"}, {"login", "r1", "-h"}, {"daemon", "-h"}, {"daemon", "stop", "--help"}, {"config", "sh", "--h"}, {"watch", "-h"}, {"version", "-h"}, {"version"}, {"--version"}, {"version", "--format", "json"}} {
		var stdout, stderr bytes.Buffer
		if got := Main(args, strings.NewReader(""), &stdout, &stderr); got != 0 || stdout.Len() == 0 || stderr.Len() != 0 {
			t.Fatalf("%q: exit=%d stdout=%d bytes stderr=%q", args, got, stdout.Len(), stderr.String())
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("help and version created files: %v", entries)
	}
	var stdout, stderr bytes.Buffer
	// The command help, laid out for the terminal (help_layout.go): on a
	// pipe under display.color=auto, plain.
	if got := Main([]string{"cmd", "--help"}, strings.NewReader(""), &stdout, &stderr); got != 0 || stdout.String() != helplayout.Layout(commandHelp, helplayout.Style{}) {
		t.Fatalf("cmd --help must print the command help")
	}
}

func TestLoadCommandsFileSendsLinesAsWritten(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	got, err := loadCommandsFile(write("a", "show clock\r\n\n# comment\n  indented \nshow version\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"show clock", "", "# comment", "  indented ", "show version"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	got, err = loadCommandsFile(write("b", "no final newline"), nil)
	if err != nil || !reflect.DeepEqual(got, []string{"no final newline"}) {
		t.Fatalf("got %q, %v", got, err)
	}
	got, err = loadCommandsFile(write("c", "\n"), nil)
	if err != nil || !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("one empty line is one empty command: got %q, %v", got, err)
	}
	got, err = loadCommandsFile("-", strings.NewReader("from stdin\n"))
	if err != nil || !reflect.DeepEqual(got, []string{"from stdin"}) {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err = loadCommandsFile(write("empty", ""), nil); errorcodes.Of(err) != "commands_file_empty" {
		t.Fatalf("empty file: %v", err)
	}
	if _, err = loadCommandsFile("-", strings.NewReader("")); errorcodes.Of(err) != "commands_file_empty" {
		t.Fatalf("empty stdin: %v", err)
	}
	if _, err = loadCommandsFile(dir, nil); errorcodes.Of(err) != "commands_file_is_directory" {
		t.Fatalf("folder: %v", err)
	}
	if _, err = loadCommandsFile(filepath.Join(dir, "missing"), nil); errorcodes.Of(err) != "commands_file_unreadable" {
		t.Fatalf("missing: %v", err)
	}
}

// decl is a test shorthand for a parsed declaration on one-based command n.
func decl(option, value string, n int) Declaration {
	return Declaration{Option: option, Value: value, Command: n}
}

// TestDeclarationLists covers the escape rule and the per-command
// declaration rules: trailing \r sequences only, --literal, the flag
// beside every count, --blind alone, the expectations in declared order, and
// each usage code with the command named.
func TestDeclarationLists(t *testing.T) {
	ex := func(pattern, response string) executionplan.Expectation {
		return executionplan.Expectation{Pattern: pattern, Response: response}
	}
	none := [][]executionplan.Expectation{}
	for _, tc := range []struct {
		name         string
		in           []string
		decls        []Declaration
		literal      bool
		wantCommands []string
		wantCounts   []int
		wantFlags    []bool
		wantExpect   [][]executionplan.Expectation
		wantCode     string
	}{
		{"no escapes", []string{"show clock", "show version"}, nil, false, []string{"show clock", "show version"}, []int{}, []bool{}, none, ""},
		{"one trailing", []string{`clear counters\r`, "show clock"}, nil, false, []string{"clear counters", "show clock"}, []int{1, 0}, []bool{true, false}, none, ""},
		{"several trailing", []string{`reload\r\r\r`}, nil, false, []string{"reload"}, []int{3}, []bool{true}, none, ""},
		{"nothing else interpreted", []string{`show run | include \.\r`, `a\rb`, `x\n`}, nil, false, []string{`show run | include \.`, `a\rb`, `x\n`}, []int{1, 0, 0}, []bool{true, false, false}, none, ""},
		{"a trailing r after an escaped backslash", []string{`x\\r`}, nil, false, []string{`x\`}, []int{1}, []bool{true}, none, ""},
		{"literal", []string{`clear counters\r`}, nil, true, []string{`clear counters\r`}, []int{}, []bool{}, none, ""},
		{"literal with the count", []string{`clear counters\r`}, []Declaration{decl("blind-return", "2", 1)}, true, []string{`clear counters\r`}, []int{2}, []bool{true}, none, ""},
		{"blind-return alone", []string{"clear counters"}, []Declaration{decl("blind-return", "1", 1)}, false, []string{"clear counters"}, []int{1}, []bool{true}, none, ""},
		{"blind-return zero declares nothing", []string{"clear counters"}, []Declaration{decl("blind-return", "0", 1)}, false, []string{"clear counters"}, []int{}, []bool{}, none, ""},
		{"blind-return on the second of two", []string{"clear counters", "reload"}, []Declaration{decl("blind-return", "1", 2)}, false, []string{"clear counters", "reload"}, []int{0, 1}, []bool{false, true}, none, ""},
		{"blind-return on each of two", []string{"clear counters", "reload"}, []Declaration{decl("blind-return", "1", 1), decl("blind-return", "2", 2)}, false, []string{"clear counters", "reload"}, []int{1, 2}, []bool{true, true}, none, ""},
		{"blind alone", []string{"reload", "show clock"}, []Declaration{decl("blind", "true", 1)}, false, []string{"reload", "show clock"}, []int{0, 0}, []bool{true, false}, none, ""},
		{"blind beside the count is harmless", []string{`reload\r`}, []Declaration{decl("blind", "true", 1)}, false, []string{"reload"}, []int{1}, []bool{true}, none, ""},
		{"expectations in declared order", []string{"copy running-config startup-config", "show clock"}, []Declaration{decl("expect", `filename \[startup-config\]\?=`, 1), decl("expect", `confirm\]=y`, 1)}, false, []string{"copy running-config startup-config", "show clock"}, []int{}, []bool{}, [][]executionplan.Expectation{{ex(`filename \[startup-config\]\?`, ""), ex(`confirm\]`, "y")}, {}}, ""},
		{"a response holding =", []string{"ping"}, []Declaration{decl("expect", `Target IP address:=a=b`, 1)}, false, []string{"ping"}, []int{}, []bool{}, [][]executionplan.Expectation{{ex(`Target IP address:`, "a=b")}}, ""},
		{"blind with expectations", []string{"reload"}, []Declaration{decl("blind", "true", 1), decl("expect", `Save\? \[yes/no\]:=y`, 1), decl("expect", `confirm\]=`, 1)}, false, []string{"reload"}, []int{0}, []bool{true}, [][]executionplan.Expectation{{ex(`Save\? \[yes/no\]:`, "y"), ex(`confirm\]`, "")}}, ""},
		{"twenty expectations", []string{"ping"}, repeatDecl("expect", "p=", 1, 20), false, []string{"ping"}, []int{}, []bool{}, [][]executionplan.Expectation{repeatExpect("p", "", 20)}, ""},
		{"twenty escapes", []string{`reload` + strings.Repeat(`\r`, 20)}, nil, false, []string{"reload"}, []int{20}, []bool{true}, none, ""},
		{"negative", []string{"reload"}, []Declaration{decl("blind-return", "-1", 1)}, false, nil, nil, nil, nil, "blind_return_out_of_range"},
		{"twenty-one", []string{"reload"}, []Declaration{decl("blind-return", "21", 1)}, false, nil, nil, nil, nil, "blind_return_out_of_range"},
		{"twenty-one escapes", []string{`reload` + strings.Repeat(`\r`, 21)}, nil, false, nil, nil, nil, nil, "blind_return_out_of_range"},
		{"twenty-one expectations", []string{"ping"}, repeatDecl("expect", "p=", 1, 21), false, nil, nil, nil, nil, "expect_too_many"},
		{"the escape and the option on one command", []string{`reload\r`}, []Declaration{decl("blind-return", "1", 1)}, false, nil, nil, nil, nil, "blind_return_conflict"},
		{"the escape and blind-return zero", []string{`reload\r`}, []Declaration{decl("blind-return", "0", 1)}, false, nil, nil, nil, nil, "blind_return_conflict"},
		{"the option twice on one command", []string{"reload"}, []Declaration{decl("blind-return", "1", 1), decl("blind-return", "1", 1)}, false, nil, nil, nil, nil, "blind_return_conflict"},
		{"the escape and an expectation", []string{`clear counters\r`}, []Declaration{decl("expect", `confirm\]=`, 1)}, false, nil, nil, nil, nil, "expect_with_blind_return"},
		{"the option and an expectation", []string{"clear counters"}, []Declaration{decl("expect", `confirm\]=`, 1), decl("blind-return", "1", 1)}, false, nil, nil, nil, nil, "expect_with_blind_return"},
		{"the option at zero and an expectation", []string{"clear counters"}, []Declaration{decl("blind-return", "0", 1), decl("expect", `confirm\]=`, 1)}, false, nil, nil, nil, nil, "expect_with_blind_return"},
		{"literal: the escape is text, the expectation stands", []string{`show run | include \r`}, []Declaration{decl("expect", `confirm\]=`, 1)}, true, []string{`show run | include \r`}, []int{}, []bool{}, [][]executionplan.Expectation{{ex(`confirm\]`, "")}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			commands := append([]string{}, tc.in...)
			got, err := declarationLists(commands, tc.decls, tc.literal)
			if errorcodes.Of(err) != tc.wantCode {
				t.Fatalf("code %q, want %q (%v)", errorcodes.Of(err), tc.wantCode, err)
			}
			if tc.wantCode != "" {
				return
			}
			if !reflect.DeepEqual(commands, tc.wantCommands) {
				t.Errorf("commands %q, want %q", commands, tc.wantCommands)
			}
			if !reflect.DeepEqual(got.returns, tc.wantCounts) {
				t.Errorf("counts %v, want %v", got.returns, tc.wantCounts)
			}
			// The flag follows the count: the plan refuses a count without it.
			if !reflect.DeepEqual(got.blind, tc.wantFlags) {
				t.Errorf("flags %v, want %v", got.blind, tc.wantFlags)
			}
			// No null list: every command has a list once any command has one.
			if !reflect.DeepEqual(got.expect, tc.wantExpect) {
				t.Errorf("expectations %v, want %v", got.expect, tc.wantExpect)
			}
		})
	}
}

func repeatDecl(option, value string, n, count int) []Declaration {
	var out []Declaration
	for i := 0; i < count; i++ {
		out = append(out, decl(option, value, n))
	}
	return out
}

func repeatExpect(pattern, response string, count int) []executionplan.Expectation {
	var out []executionplan.Expectation
	for i := 0; i < count; i++ {
		out = append(out, executionplan.Expectation{Pattern: pattern, Response: response})
	}
	return out
}

// TestDeclarationsAttachToTheirCommand covers the parser's positional
// record: each --expect, --blind, and
// --blind-return is recorded beside the --cmd it follows, in freeform on the
// one command, and --blind=false declares nothing. The record is one-based
// so a handler never sees a declaration without a command.
func TestDeclarationsAttachToTheirCommand(t *testing.T) {
	inv := mustParse(t, "command", "r1", "--cmd", "copy running-config startup-config", "--expect", `filename \[startup-config\]\?=`, "--cmd", "clear counters", "--blind-return", "1", "--cmd", "reload", "--blind", `--expect=Save\? \[yes/no\]:=y`, "--exp", `confirm\]=`, "--cmd", "show clock")
	want := []Declaration{
		decl("expect", `filename \[startup-config\]\?=`, 1),
		decl("blind-return", "1", 2),
		decl("blind", "true", 3),
		decl("expect", `Save\? \[yes/no\]:=y`, 3),
		decl("expect", `confirm\]=`, 3),
	}
	if !reflect.DeepEqual(inv.Declarations, want) {
		t.Fatalf("declarations %+v, want %+v", inv.Declarations, want)
	}
	if !reflect.DeepEqual(inv.Commands, []string{"copy running-config startup-config", "clear counters", "reload", "show clock"}) {
		t.Fatalf("commands %q", inv.Commands)
	}
	// Freeform: the options precede the text and belong to the one command.
	inv = mustParse(t, "run", "--target", "r1", "--blind", "--expect", `confirm\]=`, "reload")
	if want := []Declaration{decl("blind", "true", 1), decl("expect", `confirm\]=`, 1)}; !reflect.DeepEqual(inv.Declarations, want) || !inv.Freeform {
		t.Fatalf("freeform declarations %+v (freeform %v), want %+v", inv.Declarations, inv.Freeform, want)
	}
	// --cf with --cmd: the declaration attaches to the --cmd; the file lines
	// carry none (commandPlan appends them after the --cmd values).
	inv = mustParse(t, "command", "r1", "--cf", "-", "--cmd", "reload", "--blind")
	if want := []Declaration{decl("blind", "true", 1)}; !reflect.DeepEqual(inv.Declarations, want) {
		t.Fatalf("declarations with --cf and --cmd %+v, want %+v", inv.Declarations, want)
	}
	inv = mustParse(t, "command", "r1", "--cmd", "reload", "--blind=false")
	if len(inv.Declarations) != 0 || !inv.Set(optBlind) {
		t.Fatalf("--blind=false must declare nothing: %+v", inv.Declarations)
	}
	// Placement and grammar are the parser's rules: the code before any
	// handler runs, with the option named.
	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"command", "r1", "--expect", "a=", "--cmd", "reload"}, "declaration_before_command"},
		{[]string{"command", "r1", "--blind", "--cmd", "reload"}, "declaration_before_command"},
		{[]string{"run", "--target", "r1", "--blind-return", "1", "--cmd", "clear counters", "--cmd", "show clock"}, "declaration_before_command"},
		{[]string{"command", "r1", "--cf", "-", "--expect", "a="}, "declaration_with_commands_file"},
		{[]string{"command", "r1", "--blind-return", "1", "--cf", "-"}, "declaration_with_commands_file"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "confirm"}, "expect_malformed"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "=y"}, "expect_malformed"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "Save? (yes/no=y"}, "expect_pattern_invalid"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect", "Save? [yes/no=y"}, "expect_malformed"}, // the unclosed class swallows the =
		{[]string{"command", "r1", "--cmd", "reload", "--blind-return", "x"}, "cli_option_value_invalid"},
		{[]string{"command", "r1", "--cmd", "reload", "--blind=maybe"}, "cli_option_value_invalid"},
		{[]string{"command", "r1", "--cmd", "reload", "--expect"}, "cli_option_value_missing"},
		{[]string{"command", "r1", "--ex", "a=", "show", "clock"}, "cli_option_ambiguous"},
		{[]string{"command", "r1", "--bli", "show", "clock"}, "cli_option_ambiguous"},
	} {
		_, err := Parse(tc.args)
		if errorcodes.Of(err) != tc.code {
			t.Errorf("%q: code %q, want %q (%v)", tc.args, errorcodes.Of(err), tc.code, err)
		}
	}
	// A literal = in a pattern is written [=]; the value splits at its
	// first =, so the response keeps any later one.
	for value, want := range map[string]executionplan.Expectation{
		"a[=]b=c=d":  {Pattern: "a[=]b", Response: "c=d"},
		`a\=b=c`:     {Pattern: `a\=b`, Response: "c"},
		"[]=]x=y":    {Pattern: "[]=]x", Response: "y"},
		"[^]=]x=":    {Pattern: "[^]=]x", Response: ""},
		"p=[=]":      {Pattern: "p", Response: "[=]"},
		"Target:=a=": {Pattern: "Target:", Response: "a="},
	} {
		inv = mustParse(t, "command", "r1", "--cmd", "x", "--expect", value)
		if got, err := declarationLists(inv.Commands, inv.Declarations, false); err != nil || got.expect[0][0] != want {
			t.Errorf("%q: %+v, %v; want %+v", value, got.expect, err, want)
		}
	}
}

// TestParseTargetListAndDispatchShortcuts covers the target list and the
// dispatch shortcuts: --tl LIST is each of its names as a
// --target at the list's position, split at commas and whitespace in any
// mix (the shell's quotes group a whitespace list; the = form is the
// parser's for every value option), an empty list refused as an empty
// --target is, a name the selector grammar refuses named with --tl; and
// --dp, --dw, --ds stand for --dispatch parallel, wave, serial, the last on
// the line winning, as --pi stands for --platform.
func TestParseTargetListAndDispatchShortcuts(t *testing.T) {
	rows := []struct {
		args     []string
		targets  []string
		dispatch string
		code     string
		mention  string
	}{
		{[]string{"run", "--tl", "router01,router02,switch03", "show clock"}, []string{"target=router01", "target=router02", "target=switch03"}, "", "", ""},
		{[]string{"run", "--tl=router01,router02", "show clock"}, []string{"target=router01", "target=router02"}, "", "", ""},
		{[]string{"run", "--tl", "router01 router02 switch03", "show clock"}, []string{"target=router01", "target=router02", "target=switch03"}, "", "", ""},
		{[]string{"run", "--tl", " router01, router02 ,,\tswitch03\n", "show clock"}, []string{"target=router01", "target=router02", "target=switch03"}, "", "", ""},
		{[]string{"run", "--target", "a", "--tl", "b c", "--tf", "f.txt", "--tl", "d", "show clock"}, []string{"target=a", "target=b", "target=c", "tf=f.txt", "target=d"}, "", "", ""},
		{[]string{"command", "--tl", "router01,router02", "show clock"}, []string{"target=router01", "target=router02"}, "", "", ""},
		{[]string{"command", "router00", "--tl", "router01", "show clock"}, []string{"target=router00", "target=router01"}, "", "", ""},
		{[]string{"run", "--tl", " , ", "show clock"}, nil, "", "cli_device_empty", "--tl"},
		{[]string{"run", "--tl", "", "show clock"}, nil, "", "cli_device_empty", "--tl"},
		{[]string{"run", "--tl", "router01,^bad", "show clock"}, nil, "", "", "--tl"},
		{[]string{"run", "--dp", "--target", "r1", "show clock"}, []string{"target=r1"}, "parallel", "", ""},
		{[]string{"run", "--dw", "--target", "r1", "show clock"}, []string{"target=r1"}, "wave", "", ""},
		{[]string{"run", "--ds", "--target", "r1", "show clock"}, []string{"target=r1"}, "serial", "", ""},
		{[]string{"run", "--dispatch", "wave", "--ds", "--target", "r1", "show clock"}, []string{"target=r1"}, "serial", "", ""},
		{[]string{"run", "--dp", "--dispatch", "wave", "--target", "r1", "show clock"}, []string{"target=r1"}, "wave", "", ""},
		{[]string{"run", "--dp=false", "--target", "r1", "show clock"}, []string{"target=r1"}, "", "", ""},
		{[]string{"command", "--dp", "r1", "show clock"}, nil, "", "cli_option_unknown", "--dp"},
		{[]string{"login", "--tl", "r1"}, nil, "", "cli_option_unknown", "--tl"},
	}
	for i, r := range rows {
		inv, err := Parse(r.args)
		if r.code != "" || r.mention != "" && r.code == "" {
			if err == nil {
				t.Errorf("row %d %q: parsed, want an error", i, r.args)
				continue
			}
			if r.code != "" && errorcodes.Of(err) != r.code {
				t.Errorf("row %d %q: code %s, want %s (%v)", i, r.args, errorcodes.Of(err), r.code, err)
			}
			if !strings.Contains(err.Error(), r.mention) {
				t.Errorf("row %d %q: message %q does not name %q", i, r.args, err.Error(), r.mention)
			}
			continue
		}
		if err != nil {
			t.Errorf("row %d %q: %v", i, r.args, err)
			continue
		}
		if got := targetValues(inv); !reflect.DeepEqual(got, r.targets) {
			t.Errorf("row %d %q: targets %q, want %q", i, r.args, got, r.targets)
		}
		if got := inv.String(optDispatch); got != r.dispatch {
			t.Errorf("row %d %q: dispatch %q, want %q", i, r.args, got, r.dispatch)
		}
	}
}
