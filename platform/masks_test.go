package platform

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// maskedPlatforms are the built-ins with a mask list.
var maskedPlatforms = []string{"cisco_iosxe", "cisco_iosxr", "cisco_nxos", "arista_eos", "juniper_junos"}

// TestBuiltinMasksCompile: every built-in list compiles with a group in
// each pattern, the five network platforms carry one, and generic, linux,
// and linux_shell none.
func TestBuiltinMasksCompile(t *testing.T) {
	sizes := map[string]int{"cisco_iosxe": 19, "cisco_iosxr": 15, "cisco_nxos": 13, "arista_eos": 13, "juniper_junos": 10}
	for name, def := range builtins {
		if _, i, err := CompileMasks(def.Masks); err != nil {
			t.Fatalf("%s: pattern %d: %v", name, i+1, err)
		}
		if got := len(def.Masks); got != sizes[name] {
			t.Fatalf("%s ships %d patterns, want %d", name, got, sizes[name])
		}
	}
}

// TestCompileMasks: a pattern that does not compile and one that holds no
// capturing group are each refused by their index; no list is none.
func TestCompileMasks(t *testing.T) {
	if _, i, err := CompileMasks([]string{`^a (\S+)`, "(", `^b (\S+)`}); err == nil || i != 1 {
		t.Fatalf("the pattern that does not compile: %d %v", i, err)
	}
	if _, i, err := CompileMasks([]string{`^a (\S+)`, `^b (?:\S+)`}); err == nil || i != 1 || err.Error() != "holds no capturing group" {
		t.Fatalf("the pattern with no group: %d %v", i, err)
	}
	if res, i, err := CompileMasks(nil); res != nil || i != -1 || err != nil {
		t.Fatalf("no list: %v %d %v", res, i, err)
	}
}

// TestMask: every group of every match of every pattern is replaced, text
// that two groups cover is replaced once, a group that matched nothing is
// left alone, and a statement no pattern matches is unchanged.
func TestMask(t *testing.T) {
	res, _, err := CompileMasks([]string{
		`key (\S+)`,
		`auth (\S+) priv (\S+)`,
		`(?i)secret (\S+ \S+)`,
		`secret \S+ (\S+)`,
		`opt(\d*)`,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range [][2]string{
		{"key K1 and key K2", "key <removed> and key <removed>"},
		{"user u auth A1 priv P1", "user u auth <removed> priv <removed>"},
		{"secret 0 S3cret tail", "secret <removed> tail"},
		{"opt", "opt"},
		{"opt12", "opt<removed>"},
		{"show clock", "show clock"},
		{"", ""},
	} {
		if got := Mask(res, c[0]); got != c[1] {
			t.Fatalf("Mask(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	if got := Mask(nil, "key K1"); got != "key K1" {
		t.Fatalf("no list masks nothing: %q", got)
	}
}

// TestResolveMasks at the resolver: a table's array replaces the built-in
// list whole, an empty array turns the mask off, an alias inherits its
// driver's built-in list, and linux has none.
func TestResolveMasks(t *testing.T) {
	tables := map[string]map[string]any{
		"cisco_iosxe": {"masks": []any{`^hostname (\S+)`}},
		"c9300":       {"driver": "cisco_iosxe"},
		"quiet":       {"driver": "cisco_nxos", "masks": []any{}},
		"linux":       {"masks": []any{`--password=(\S+)`}},
	}
	if got := Resolve("cisco_iosxe", tables).Masks; len(got) != 1 || got[0] != `^hostname (\S+)` {
		t.Fatalf("the table's list replaces the built-in: %v", got)
	}
	if got := Resolve("c9300", tables).Masks; len(got) != len(builtins["cisco_iosxe"].Masks) {
		t.Fatalf("an alias inherits the built-in list, not the site's base table: %v", got)
	}
	if got := Resolve("quiet", tables).Masks; len(got) != 0 {
		t.Fatalf("an empty array turns the mask off: %v", got)
	}
	if got := Resolve("linux", nil).Masks; len(got) != 0 {
		t.Fatalf("linux has no built-in list: %v", got)
	}
	if got := Resolve("linux", tables).Masks; len(got) != 1 {
		t.Fatalf("a site's linux list: %v", got)
	}
}

// corpusLine is a line of testdata/masks: a statement as sent or a line of
// a configuration as the device prints it, and the secrets it holds.
type corpusLine struct {
	kind, text string
	secrets    []string
}

func readCorpus(t *testing.T, name string) []corpusLine {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "masks", name+".txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []corpusLine
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "#") {
			continue
		}
		fields := strings.Split(sc.Text(), "\t")
		out = append(out, corpusLine{kind: fields[0], text: fields[1], secrets: fields[2:]})
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestBuiltinMasksCorpus runs each built-in list over its platform's
// statements as sent and its printed configuration (docs/EXAMPLES.md,
// chapter 63, issue 8): no secret is left in place, and the only text
// removed that is no secret is the over-masks the chapter accepts.
func TestBuiltinMasksCorpus(t *testing.T) {
	accepted := map[string][]string{
		"cisco_iosxe": {"sent description uplink to core, password reset by noc", "line  key 1"},
		"cisco_iosxr": {"line  key 1"},
	}
	for _, name := range maskedPlatforms {
		res, _, err := CompileMasks(builtins[name].Masks)
		if err != nil {
			t.Fatal(err)
		}
		var over []string
		lines := readCorpus(t, name)
		for _, l := range lines {
			out := Mask(res, l.text)
			for _, s := range l.secrets {
				if strings.Contains(out, s) {
					t.Errorf("%s leaks %q: %s %q -> %q", name, s, l.kind, l.text, out)
				}
			}
			for _, sp := range maskSpans(res, l.text) {
				if removed := l.text[sp[0]:sp[1]]; !isSecret(removed, l.secrets) {
					over = append(over, l.kind+" "+l.text)
				}
			}
		}
		if strings.Join(over, "\n") != strings.Join(accepted[name], "\n") {
			t.Errorf("%s removes words of no secret:\n%s\nwant:\n%s", name, strings.Join(over, "\n"), strings.Join(accepted[name], "\n"))
		}
		if len(lines) == 0 {
			t.Fatalf("%s: an empty corpus", name)
		}
	}
}

// TestAllMasksCorpus: a job across platforms masks every event by the
// lists of all its platforms together, so that each platform's statements
// and printed lines under all five lists leave no secret in place.
func TestAllMasksCorpus(t *testing.T) {
	var all []*regexp.Regexp
	for _, name := range maskedPlatforms {
		res, _, err := CompileMasks(builtins[name].Masks)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, res...)
	}
	for _, name := range maskedPlatforms {
		for _, l := range readCorpus(t, name) {
			out := Mask(all, l.text)
			for _, s := range l.secrets {
				if strings.Contains(out, s) {
					t.Errorf("all lists leak %q of %s: %s %q -> %q", s, name, l.kind, l.text, out)
				}
			}
		}
	}
}

func isSecret(text string, secrets []string) bool {
	for _, s := range secrets {
		if text == s {
			return true
		}
	}
	return false
}
