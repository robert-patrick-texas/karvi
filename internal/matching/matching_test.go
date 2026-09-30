package matching

import (
	"errors"
	"net/netip"
	"reflect"
	"strings"
	"testing"
)

// TestPatternGrammar covers the SELECTOR profile: whole-field,
// case-insensitive, "*" across "/" and ".", classes
// with "!", backslash escapes.
func TestPatternGrammar(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"sw-*", "sw-nyc-01.example.gov", true},
		{"nyc*", "nyc/dc1", true},
		{"nyc", "nyc/dc1", false},
		{"*dc1", "nyc/dc1", true},
		{"sw-0?", "sw-01", true},
		{"sw-0?", "sw-012", false},
		{"sw-?", "sw-é", true},
		{"sw-[a-c]", "sw-b", true},
		{"sw-[a-c]", "sw-B", true},
		{"sw-[A-C]", "sw-b", true},
		{"sw-[!a-c]", "sw-b", false},
		{"sw-[!a-c]", "sw-B", false},
		{"sw-[!a-c]", "sw-x", true},
		{"nyc[!/]*", "nyc-1", true},
		{"nyc[!/]*", "nyc/dc1", false},
		{"sw[-_]1", "sw_1", true},
		{"sw[_-]1", "sw-1", true},
		{`sw[\]]1`, "sw]1", true},
		{`sw\*`, "sw*", true},
		{`sw\*`, "sw1", false},
		{`\!lab`, "!lab", true},
		{`\^x\$`, "^x$", true},
		{"SW-*", "sw-1", true},
		{"cisco_*", "CISCO_IOSXE", true},
		{"nyc", "NYC", true},
		{"core", "Core", true},
		{"*", "", true},
		{"?", "", false},
		{"a*b*c", "aXbYbZc", true},
		{"a*b*c", "aXbYbZ", false},
		{"**x", "x", true},
		{"", "", true},
		{"", "a", false},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		if got := p.Match(tc.value); got != tc.want {
			t.Errorf("%q matching %q = %v, want %v", tc.pattern, tc.value, got, tc.want)
		}
	}
}

// TestMalformedPatternsRefused: a malformed pattern and an
// unescaped "^" or "$" are refused, never an empty selection.
func TestMalformedPatternsRefused(t *testing.T) {
	for _, tc := range []struct{ value, reason string }{
		{"ny[c", "not closed"},
		{"sw-[a-", "not closed"},
		{`sw\`, "trailing backslash"},
		{`sw[a\`, "trailing backslash"},
		{"sw[]", "is empty"},
		{"sw[!]", "is empty"},
		{"sw[c-a]", "reversed"},
		{"^nyc", "unescaped \"^\""},
		{"nyc$", "unescaped \"$\""},
		{"sw-[^a-c]", "unescaped \"^\""},
		{"!", "needs a pattern"},
		{"!^x", "unescaped \"^\""},
	} {
		_, err := ParseSelector(tc.value)
		var pe *PatternError
		if !errors.As(err, &pe) || pe.Value != tc.value || !strings.Contains(pe.Reason, tc.reason) {
			t.Errorf("ParseSelector(%q) = %v, want a PatternError containing %q", tc.value, err, tc.reason)
		}
	}
	if _, err := ParseSelector("^x"); err == nil || !strings.Contains(err.Error(), `\^ and \$`) {
		t.Errorf("the reserved-character message names the escapes: %v", err)
	}
}

func TestParseSelectorNegation(t *testing.T) {
	s, err := ParseSelector("!lab*")
	if err != nil || !s.Negated || !s.Match("LAB-1") {
		t.Fatalf("!lab*: %+v %v", s, err)
	}
	s, err = ParseSelector(`\!lab`)
	if err != nil || s.Negated || !s.Match("!lab") {
		t.Fatalf(`\!lab: %+v %v`, s, err)
	}
	if !HasMeta(`a\b`) || !HasMeta("a[b]") || HasMeta("sw-01.example.gov") {
		t.Fatal("HasMeta")
	}
}

// TestSelectRule covers rule selection over the shared matcher: negation
// removes the rule, groups match any, the earlier of the first ordinary rule
// and the longest CIDR wins, equal prefixes are ambiguous, no match is -1.
func TestSelectRule(t *testing.T) {
	f := Fields{Name: "sw-nyc-01.example.gov", Address: netip.MustParseAddr("10.1.2.3"), Platform: "cisco_iosxe", Site: "NYC", Groups: []string{"core", "lab"}}
	rule := func(kv ...any) map[string]any {
		m := map[string]any{}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}
	for _, tc := range []struct {
		name  string
		rules []map[string]any
		want  int
	}{
		{"site folds case", []map[string]any{rule("site", "nyc"), rule("name", "*")}, 0},
		{"group any", []map[string]any{rule("device-group", []any{"edge", "LAB"}), rule("name", "*")}, 0},
		{"group negation removes", []map[string]any{rule("device-group", []any{"core", "!lab"}), rule("name", "*")}, 1},
		{"all keys AND", []map[string]any{rule("platform", "cisco_*", "site", "bos"), rule("name", "*")}, 1},
		{"longest cidr", []map[string]any{rule("address-cidr", "10.0.0.0/8"), rule("address-cidr", "10.1.0.0/16"), rule("name", "*")}, 1},
		{"earlier ordinary beats cidr", []map[string]any{rule("platform", "cisco_iosxe"), rule("address-cidr", "10.1.2.0/24"), rule("name", "*")}, 0},
		{"earlier cidr beats ordinary", []map[string]any{rule("address-cidr", "10.1.2.0/24"), rule("platform", "cisco_iosxe")}, 0},
		{"cidr negation", []map[string]any{rule("address-cidr", []any{"10.0.0.0/8", "!10.1.0.0/16"}), rule("name", "*")}, 1},
		{"none", []map[string]any{rule("site", "bos")}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Select(tc.rules, f)
			if err != nil || got != tc.want {
				t.Fatalf("Select = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
	_, err := Select([]map[string]any{rule("platform", "x"), rule("address-cidr", "10.1.0.0/16", "site", "nyc"), rule("address-cidr", "10.1.0.0/16")}, f)
	var amb *AmbiguousError
	if !errors.As(err, &amb) || amb.Prefix != 16 || !reflect.DeepEqual(amb.Indices, []int{1, 2}) {
		t.Fatalf("ambiguous: %v", err)
	}
	_, err = Select([]map[string]any{rule("site", "ny[c")}, f)
	var re *RuleError
	if !errors.As(err, &re) || re.Key != "site" || re.Value != "ny[c" {
		t.Fatalf("rule error: %v", err)
	}
	if err := CheckRule(rule("name", "sw*", "address-cidr", "10.0.0.0/33")); !errors.As(err, &re) || !re.CIDR {
		t.Fatalf("CheckRule cidr: %v", err)
	}
	if err := CheckRule(rule("device-group", []any{"core", "!"})); !errors.As(err, &re) || re.Key != "device-group" {
		t.Fatalf("CheckRule bare negation: %v", err)
	}
}
