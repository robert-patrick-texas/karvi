package matching

import (
	"errors"
	"fmt"
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
		{"cisco_*", "CISCO_IOS", true},
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
	f := Fields{Name: "sw-nyc-01.example.gov", Address: netip.MustParseAddr("10.1.2.3"), Platform: "cisco_ios", Site: "NYC", Groups: []string{"core", "lab"}}
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
		{"earlier ordinary beats cidr", []map[string]any{rule("platform", "cisco_ios"), rule("address-cidr", "10.1.2.0/24"), rule("name", "*")}, 0},
		{"earlier cidr beats ordinary", []map[string]any{rule("address-cidr", "10.1.2.0/24"), rule("platform", "cisco_ios")}, 0},
		{"cidr negation", []map[string]any{rule("address-cidr", []any{"10.0.0.0/8", "!10.1.0.0/16"}), rule("name", "*")}, 1},
		{"a spelling selects the built-in", []map[string]any{rule("platform", "cisco_iosxe"), rule("name", "*")}, 0},
		{"a negated spelling removes the built-in", []map[string]any{rule("platform", []any{"cisco_*", "!CISCO_XE"}), rule("name", "*")}, 1},
		{"a glob matches built-in names alone", []map[string]any{rule("platform", "cisco_iosx*"), rule("name", "*")}, 1},
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

// TestPlatformValue: a platform selector's literal is read as a platform
// name, its "!" kept; a glob, a blank value, and a value whose folding would
// begin a negation are left as written.
func TestPlatformValue(t *testing.T) {
	for in, want := range map[string]string{
		"cisco_xe": "cisco_ios", "!Cisco_IOSXE": "!cisco_ios", " cisco_iosxr ": "cisco_xr", "C9300": "c9300",
		"cisco_x*": "cisco_x*", "!cisco_iosx?": "!cisco_iosx?", "": "", "!": "!", " !cisco_xe": " !cisco_xe",
	} {
		if got := PlatformValue(in); got != want {
			t.Errorf("PlatformValue(%q) = %q, want %q", in, got, want)
		}
	}
}

// spelled is the names a one-bracket numeric range spells, written out by
// formatting each number in lo..hi at width (fmt's zero padding), so the
// test's list is built apart from the matcher.
func spelled(prefix string, lo, hi, width int, suffix string) []string {
	var out []string
	for n := lo; n <= hi; n++ {
		out = append(out, fmt.Sprintf("%s%0*d%s", prefix, width, n, suffix))
	}
	return out
}

// TestNumericRangeMatchesTheNamesItSpells: a bracket of only digits,
// hyphens, and commas is a numeric range, and a pattern holding one matches
// a value exactly when the value is one of the names the pattern spells.
// Every value "r" followed by one to four digits, and a few others, is
// tried against the spelled list.
func TestNumericRangeMatchesTheNamesItSpells(t *testing.T) {
	cat := func(lists ...[]string) []string {
		var out []string
		for _, l := range lists {
			out = append(out, l...)
		}
		return out
	}
	for _, tc := range []struct {
		pattern string
		want    []string
	}{
		{"r[1-12]", spelled("r", 1, 12, 1, "")},
		{"r[01-12]", spelled("r", 1, 12, 2, "")},
		{"r[8-100]", spelled("r", 8, 100, 1, "")},
		{"r[008-100]", spelled("r", 8, 100, 3, "")},
		{"r[1-012]", spelled("r", 1, 12, 1, "")}, // the lower bound's width
		{"r[1,4,7-9]", []string{"r1", "r4", "r7", "r8", "r9"}},
		{"r[05]", []string{"r05"}},
		{"r[123]", []string{"r123"}},
		{"r[1,3]", []string{"r1", "r3"}},
		{"r[01,5]", []string{"r01", "r5"}},
		{"r[0-9]", spelled("r", 0, 9, 1, "")},
		{"r[1-3]", spelled("r", 1, 3, 1, "")},
		{"r[0-0]", []string{"r0"}},
		{"r[000-002]", spelled("r", 0, 2, 3, "")},
		{"r[1-3]0", spelled("r", 1, 3, 1, "0")},
		{"r[1-12]5", spelled("r", 1, 12, 1, "5")},
		{"r[1-2][1-12]", cat(spelled("r1", 1, 12, 1, ""), spelled("r2", 1, 12, 1, ""))},
		{"r[1-3]?", cat(spelled("r1", 0, 9, 1, ""), spelled("r2", 0, 9, 1, ""), spelled("r3", 0, 9, 1, ""), []string{"r1a"})},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Errorf("Compile(%q): %v", tc.pattern, err)
			continue
		}
		want := map[string]bool{}
		for _, w := range tc.want {
			want[w] = true
		}
		values := []string{"r", "R1", "r-", "r,", "r1a", "x1", "r1-3", "r1,3"}
		for width := 1; width <= 4; width++ {
			values = append(values, spelled("r", 0, 9999, width, "")...)
		}
		for _, v := range values {
			if got := p.Match(v); got != want[strings.ToLower(v)] {
				t.Errorf("%q matching %q = %v, want %v", tc.pattern, v, got, !got)
			}
		}
	}
}

// TestNumericRangeByNumber: a range is tested by number and width, so a
// wide range selects without a list and a bound past 64 bits is read
// exactly; a glob beside a range takes what the range leaves.
func TestNumericRangeByNumber(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"box[78000-99999]", "box78000", true},
		{"box[78000-99999]", "box99999", true},
		{"box[78000-99999]", "box77999", false},
		{"box[78000-99999]", "box100000", false},
		{"box[00501-09999]", "box00501", true},
		{"box[00501-09999]", "box501", false},
		{"box[00501-09999]", "box09999", true},
		{"r[1-99999999999999999999999]", "r12345678901234567890", true},
		{"r[1-99999999999999999999999]", "r0", false},
		{"r[0-18446744073709551616]", "r18446744073709551616", true},
		{"r[0-18446744073709551616]", "r18446744073709551617", false},
		{"*[10-12]", "router11", true},
		{"*[10-12]", "router13", false}, // every name *[10-12] spells ends in 10, 11, or 12
		{"router*[10-12]", "router-a10", true},
		{"[1-3]*", "2-core", true},
		{"[1-3]*", "4-core", false},
		{"sw[1-2]-[10-11]", "sw2-11", true},
		{"sw[1-2]-[10-11]", "sw2-1", false},
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

// TestNumericRangeMalformedRefused: a bracket of only digits, hyphens, and
// commas that is not a list of numbers and number-number items is refused,
// as a reversed range is; a backslash keeps the class reading.
func TestNumericRangeMalformedRefused(t *testing.T) {
	const malformed = `the numeric range "[1-]" is malformed: each item is a number or two numbers joined by "-", separated by ","`
	_, err := Compile("router[1-]")
	var pe *PatternError
	if !errors.As(err, &pe) || pe.Reason != malformed {
		t.Fatalf("router[1-]: %v, want the reason %q", err, malformed)
	}
	for _, v := range []string{"r[1-]", "r[-5]", "r[1,,3]", "r[1-3-5]", "r[-]", "r[,]", "r[1,]", "r[,1]", "r[--]", "!r[1-]"} {
		_, err := ParseSelector(v)
		if !errors.As(err, &pe) || !strings.Contains(pe.Reason, "is malformed") || strings.Contains(pe.Unquoted, "[") || pe.Unquoted == "" {
			t.Errorf("ParseSelector(%q) = %v (unquoted %q), want a malformed numeric range", v, err, pe.Unquoted)
		}
	}
	for _, v := range []string{"r[12-1]", "r[10-09]"} {
		_, err := ParseSelector(v)
		if !errors.As(err, &pe) || !strings.Contains(pe.Reason, "reversed") || pe.Unquoted != "a numeric range is reversed" {
			t.Errorf("ParseSelector(%q) = %v, want a reversed numeric range", v, err)
		}
	}
	for _, tc := range []struct {
		pattern string
		match   []string
		miss    []string
	}{
		{`r[1\-]`, []string{"r1", "r-"}, []string{"r10"}},
		{`r[\-5]`, []string{"r-", "r5"}, nil},
		{`r[1\,3]`, []string{"r1", "r,", "r3"}, []string{"r13"}},
		{"r[!0-3]", []string{"r5"}, []string{"r1", "r10"}},
		{"r[0-9a-f]", []string{"rb", "r7"}, []string{"r10"}},
	} {
		p, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", tc.pattern, err)
		}
		for _, v := range tc.match {
			if !p.Match(v) {
				t.Errorf("class %q misses %q", tc.pattern, v)
			}
		}
		for _, v := range tc.miss {
			if p.Match(v) {
				t.Errorf("class %q matches %q", tc.pattern, v)
			}
		}
	}
}

// TestMatchWaysAgree: the two ways Match takes give one answer on every
// pattern without a numeric range, so a range's way is the grammar's own.
func TestMatchWaysAgree(t *testing.T) {
	values := []string{"", "a", "sw-1", "sw-b", "SW-B", "sw-x", "sw_1", "nyc/dc1", "aXbYbZc", "aXbYbZ", "sw]1", "sw*", "!lab", "x", "r-", "r1", "r10", "é"}
	for _, pattern := range []string{"*", "?", "", "sw-*", "sw-?", "sw-[a-c]", "sw-[!a-c]", "nyc[!/]*", "sw[-_]1", `sw[\]]1`, `sw\*`, `\!lab`, "a*b*c", "**x", "*?", "?*?", "*[!a]*", "r[a-z0-9]*", "*b*"} {
		p, err := Compile(pattern)
		if err != nil {
			t.Fatalf("Compile(%q): %v", pattern, err)
		}
		for _, v := range values {
			r := []rune(v)
			if a, b := p.matchRunes(r), p.matchPositions(r); a != b {
				t.Errorf("%q matching %q: by runes %v, by positions %v", pattern, v, a, b)
			}
		}
	}
}
