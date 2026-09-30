package platform

import (
	"regexp"
	"strings"
	"testing"
)

// TestBuiltinCrunFiltersCompile: every
// built-in drop list compiles, the platforms with a configuration and a
// volatile line carry one, and no built-in pattern names the last-change
// stamp.
func TestBuiltinCrunFiltersCompile(t *testing.T) {
	for name, def := range builtins {
		if _, i, err := CompileFilters(def.CrunFilters); err != nil {
			t.Fatalf("%s: pattern %d: %v", name, i+1, err)
		}
		for _, p := range def.CrunFilters {
			if containsAny(p, "Last configuration change", "Last commit", "last done") {
				t.Fatalf("%s drops a change stamp: %q", name, p)
			}
		}
	}
	for _, name := range []string{"cisco_iosxe", "cisco_iosxr", "cisco_nxos", "arista_eos"} {
		if len(builtins[name].CrunFilters) == 0 {
			t.Fatalf("%s ships no drop list", name)
		}
	}
	for _, name := range []string{"juniper_junos", "generic", "linux"} {
		if len(builtins[name].CrunFilters) != 0 {
			t.Fatalf("%s ships a drop list: %v", name, builtins[name].CrunFilters)
		}
	}
	res, _, _ := CompileFilters(builtins["cisco_iosxe"].CrunFilters)
	for _, line := range []string{"Building configuration...", "Current configuration : 512 bytes", "ntp clock-period 17179869", "fake-iosxe uptime is 1 day", "Load for five secs: 1%/0%; one minute: 1%; five minutes: 1%", "Time source is NTP, 10:00:00.000 UTC Tue Sep 15 2026"} {
		if !matchesAny(res, line) {
			t.Fatalf("cisco_iosxe keeps %q", line)
		}
	}
	for _, line := range []string{"! Last configuration change at 10:00:00 UTC Tue Sep 15 2026", "hostname core-nyc-01", "Building configurationXXX", "end"} {
		if matchesAny(res, line) {
			t.Fatalf("cisco_iosxe drops %q", line)
		}
	}
}

// TestResolveCrunFilters at the resolver: a table's
// array replaces the built-in list whole, an empty array turns the filter
// off, an alias inherits its driver's built-in list, and a bad pattern is
// named by its index.
func TestResolveCrunFilters(t *testing.T) {
	tables := map[string]map[string]any{
		"cisco_iosxe": {"crun-filters": []any{"^hostname "}},
		"c9300":       {"driver": "cisco_iosxe"},
		"quiet":       {"driver": "cisco_nxos", "crun-filters": []any{}},
	}
	if got := Resolve("cisco_iosxe", tables).CrunFilters; len(got) != 1 || got[0] != "^hostname " {
		t.Fatalf("the table's list replaces the built-in: %v", got)
	}
	if got := Resolve("c9300", tables).CrunFilters; len(got) != len(builtins["cisco_iosxe"].CrunFilters) {
		t.Fatalf("an alias inherits the built-in list, not the site's base table: %v", got)
	}
	if got := Resolve("quiet", tables).CrunFilters; len(got) != 0 {
		t.Fatalf("an empty array turns the filter off: %v", got)
	}
	if _, i, err := CompileFilters([]string{"^ok$", "(", "^ok$"}); err == nil || i != 1 {
		t.Fatalf("the bad pattern's index: %d %v", i, err)
	}
	if res, i, err := CompileFilters(nil); res != nil || i != -1 || err != nil {
		t.Fatalf("no list: %v %d %v", res, i, err)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func matchesAny(res []*regexp.Regexp, line string) bool {
	for _, re := range res {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}
