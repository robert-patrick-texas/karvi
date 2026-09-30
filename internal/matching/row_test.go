package matching

import (
	"net/netip"
	"strings"
	"testing"
)

func mustRow(t *testing.T, cells map[string]string) Row {
	t.Helper()
	r, err := CompileRow(cells)
	if err != nil {
		t.Fatalf("%v: %v", cells, err)
	}
	return r
}

func view(name, address, platform, site string, groups ...string) RowFields {
	f := RowFields{Fields: Fields{Name: name, Platform: platform, Site: site, Groups: groups}}
	if address != "" {
		f.Address = netip.MustParseAddr(address)
	}
	return f
}

// TestFirstRowIsFileOrder runs the row evaluator over a sample of six rows
// and six devices. Two results differ from what the map evaluator would
// give, as the row rules set out to make them: fw-nyc-01 now takes line 5
// (a negated cell stands beside a positive cell in another column), and
// the /8 on line 3 still beats the /16 on line 4, since order is the
// file's and there is no longest-prefix rule.
func TestFirstRowIsFileOrder(t *testing.T) {
	lines := []int{2, 3, 4, 5, 6, 7}
	rows := []Row{
		mustRow(t, map[string]string{"device_name": "sw-nyc-01"}),
		mustRow(t, map[string]string{"address_cidr": "10.0.0.0/8"}),
		mustRow(t, map[string]string{"address_cidr": "10.1.0.0/16"}),
		mustRow(t, map[string]string{"site": "NYC", "device_group": "!lab"}),
		mustRow(t, map[string]string{"platform": "cisco_*", "site": "bos"}),
		mustRow(t, map[string]string{"device_name": "*"}),
	}
	for _, tc := range []struct {
		device RowFields
		line   int
	}{
		{view("sw-nyc-01", "10.1.2.3", "cisco_iosxe", "nyc", "core", "lab"), 2},
		{view("sw-nyc-02", "10.1.2.4", "cisco_iosxe", "nyc", "core", "lab"), 3},
		{view("sw-bos-01", "192.0.2.1", "cisco_iosxe", "BOS", "edge"), 6},
		{view("fw-nyc-01", "192.0.2.9", "linux", "Nyc", "edge"), 5},
		{view("fw-nyc-02", "192.0.2.10", "linux", "nyc", "LAB"), 7},
		{view("sw-lon-01", "", "", "lon"), 7},
	} {
		i := FirstRow(rows, tc.device)
		if i < 0 || lines[i] != tc.line {
			t.Errorf("%s: row index %d, want line %d", tc.device.Name, i, tc.line)
		}
	}
	if i := FirstRow(rows[:5], view("fw-lon-01", "", "linux", "lon")); i != -1 {
		t.Errorf("no row should match, got index %d", i)
	}
}

// TestRowCells covers the row's cell rules cell by cell.
func TestRowCells(t *testing.T) {
	dev := view("SW-NYC-01", "10.1.2.3", "Cisco_IOSXE", "NYC", "Core", "Lab")
	noAddr := view("sw-nyc-01", "", "cisco_iosxe", "nyc", "core")
	for _, tc := range []struct {
		name  string
		cells map[string]string
		f     RowFields
		want  bool
	}{
		{"case folds in every column", map[string]string{"device_name": "sw-nyc-*", "platform": "cisco_iosxe", "site": "nyc", "device_group": "core"}, dev, true},
		{"every filled cell must agree", map[string]string{"device_name": "sw-nyc-*", "site": "bos"}, dev, false},
		{"any group matches a positive cell", map[string]string{"device_group": "lab"}, dev, true},
		{"a negated group excludes on any group", map[string]string{"site": "nyc", "device_group": "!lab"}, dev, false},
		{"a negated cell that does not match agrees", map[string]string{"site": "nyc", "device_group": "!edge"}, dev, true},
		{"a negated name", map[string]string{"site": "nyc", "device_name": "!sw-nyc-01"}, dev, false},
		{"cells are trimmed", map[string]string{"device_name": "  sw-nyc-01 ", "site": " "}, dev, true},
		{"a prefix contains the address", map[string]string{"address_cidr": "10.1.0.0/16"}, dev, true},
		{"a prefix that does not", map[string]string{"address_cidr": "10.2.0.0/16"}, dev, false},
		{"a negated prefix excludes", map[string]string{"site": "nyc", "address_cidr": "!10.1.0.0/16"}, dev, false},
		{"no address, no match for a positive prefix", map[string]string{"address_cidr": "0.0.0.0/0"}, noAddr, false},
		{"no address, a negated prefix excludes nothing", map[string]string{"site": "nyc", "address_cidr": "!10.1.0.0/16"}, noAddr, true},
		{"an escaped ! is a literal", map[string]string{"device_name": `\!odd`}, view("!odd", "", "", ""), true},
		{"a blank field is matched by a lone *, as in the maps", map[string]string{"platform": "*"}, view("x", "", "", ""), true},
		{"and by no name", map[string]string{"platform": "generic"}, view("x", "", "", ""), false},
	} {
		if got := mustRow(t, tc.cells).Match(tc.f); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestRowKeyIsAPin: a pinned device matches only
// the row whose key equals its reference, the row's other cells still
// applying; an unpinned device ignores the key column, and a row whose
// only positive cell is its key matches no unpinned device.
func TestRowKeyIsAPin(t *testing.T) {
	keyOnly := mustRow(t, map[string]string{"credkey": "Core-Admin"})
	keyAndSite := mustRow(t, map[string]string{"credkey": "nyc-ro", "site": "nyc"})
	keyAndNegation := mustRow(t, map[string]string{"credkey": "not-lab", "device_group": "!lab"})
	general := mustRow(t, map[string]string{"device_name": "*"})

	pinned := func(ref, site string, groups ...string) RowFields {
		f := view("sw-01", "10.0.0.1", "cisco_iosxe", site, groups...)
		f.CredKeyRef = ref
		return f
	}
	for _, tc := range []struct {
		name string
		row  Row
		f    RowFields
		want bool
	}{
		{"the key matches, folding case", keyOnly, pinned("core-admin", "nyc"), true},
		{"the reference is trimmed", keyOnly, pinned(" core-admin ", "nyc"), true},
		{"another key", keyOnly, pinned("edge-admin", "nyc"), false},
		{"a pinned device never takes a keyless row", general, pinned("core-admin", "nyc"), false},
		{"the key row's other cells still apply", keyAndSite, pinned("nyc-ro", "bos"), false},
		{"and agree", keyAndSite, pinned("nyc-ro", "NYC"), true},
		{"a key beside only a negation is a legal row", keyAndNegation, pinned("not-lab", "nyc", "core"), true},
		{"whose negation excludes a pinned device", keyAndNegation, pinned("not-lab", "nyc", "lab"), false},
		{"an unpinned device ignores the key column", keyAndSite, view("sw-01", "", "", "nyc"), true},
		{"a key-only row is no catch-all for unpinned devices", keyOnly, view("sw-01", "", "", "nyc"), false},
		{"nor is a key beside only a negation", keyAndNegation, view("sw-01", "", "", "nyc", "core"), false},
	} {
		if got := tc.row.Match(tc.f); got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, got, tc.want)
		}
	}
	if keyOnly.Key() != "Core-Admin" || general.Key() != "" {
		t.Errorf("keys: %q %q", keyOnly.Key(), general.Key())
	}
	if FoldKey("Core-Admin") != FoldKey("CORE-admin") || FoldKey("a") == FoldKey("b") {
		t.Error("FoldKey does not fold case alone")
	}
}

// TestRowErrors: what credential_csv_row_invalid will report (decision
// 3.3): the column and the reason.
func TestRowErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cells  map[string]string
		column string
		reason string
	}{
		{"no selector at all", map[string]string{"username": "svc"}, "", "no selector"},
		{"blank cells only", map[string]string{"device_name": " ", "site": ""}, "", "no selector"},
		{"only negations", map[string]string{"device_group": "!lab", "site": "!bos"}, "", "only negated"},
		{"an unclosed class", map[string]string{"device_name": "sw-[nyc"}, "device_name", "not closed"},
		{"an anchor", map[string]string{"site": "^nyc"}, "site", "^"},
		{"a bare negation", map[string]string{"site": "!", "device_name": "x"}, "site", "negation needs a pattern"},
		{"a bad prefix", map[string]string{"address_cidr": "10.1.2.3"}, "address_cidr", "not a CIDR prefix"},
		{"a pattern for a prefix", map[string]string{"address_cidr": "10.*"}, "address_cidr", "not a CIDR prefix"},
		{"a glob key", map[string]string{"credkey": "core-*"}, "credkey", "pattern character"},
		{"a negated key", map[string]string{"credkey": "!core", "site": "nyc"}, "credkey", "cannot be negated"},
	} {
		_, err := CompileRow(tc.cells)
		re, ok := err.(*RowError)
		if !ok {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if re.Column != tc.column || !strings.Contains(re.Error(), tc.reason) {
			t.Errorf("%s: column %q, %q; want %q, %q", tc.name, re.Column, re.Error(), tc.column, tc.reason)
		}
	}
}

// TestRowString is the evidence pattern: the filled
// selector cells as written, in column order.
func TestRowString(t *testing.T) {
	r := mustRow(t, map[string]string{"site": "nyc", "device_name": "sw-nyc-*", "credkey": "nyc-ro", "username": "svc", "password": "never-shown"})
	if got, want := r.String(), "device_name=sw-nyc-* site=nyc credkey=nyc-ro"; got != want {
		t.Errorf("%q, want %q", got, want)
	}
}

func TestCheckKey(t *testing.T) {
	for _, ok := range []string{"core-admin", "NYC.ro_1", "a!b", "site:nyc"} {
		if reason := CheckKey(ok); reason != "" {
			t.Errorf("%q: %s", ok, reason)
		}
	}
	for _, bad := range []string{"", "  ", "!core", "core*", "co?e", "[core]", `co\re`} {
		if CheckKey(bad) == "" {
			t.Errorf("%q passed", bad)
		}
	}
}

// TestRowErrorNeverQuotesTheCell for the
// evaluator: a misplaced delimiter can shift a password into a selector
// column, so a row's error names the column and the fault, never the text.
func TestRowErrorNeverQuotesTheCell(t *testing.T) {
	for column, cell := range map[string]string{
		"device_name":  "Hunter2[secret",
		"site":         "Hunter2secret\\",
		"platform":     "Hunter2^secret",
		"device_group": "Hunter2[z-a]secret",
		"address_cidr": "Hunter2secret",
		"credkey":      "Hunter2*secret",
	} {
		_, err := CompileRow(map[string]string{column: cell})
		if err == nil {
			t.Errorf("%s: %q compiled", column, cell)
			continue
		}
		if msg := err.Error(); strings.Contains(msg, "Hunter2") || strings.Contains(msg, "secret") || strings.Contains(msg, `"z"`) {
			t.Errorf("%s: the error quotes the cell: %s", column, msg)
		}
		if re, ok := err.(*RowError); !ok || re.Column != column {
			t.Errorf("%s: error %v does not name the column", column, err)
		}
	}
}
