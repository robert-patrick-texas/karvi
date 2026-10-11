package credcsv

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// The sample file: a device row, a /8 above a /16
// (file order, not the longest prefix), a site row with a negated group, a
// keyed row, and a catch-all with no password.
const sample = `# credentials for the lab
device_name,address_cidr,platform,site,device_group,credkey,username,password,enable_password
sw-nyc-01,,,,,,svc.core,  core pass  ,core-enable

,10.0.0.0/8,,,,,svc.ten,ten-pass,
,10.1.0.0/16,,,,,svc.sixteen,sixteen-pass,
,,,NYC,!lab,nyc-ro,,nyc-pass,
,,,,,core-admin,svc.admin,admin-pass,admin-enable
*,,,,,,svc.default,,
`

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// newBackend builds a user-scope backend over body through New, as the
// resolver does, with the table's keys given as a map.
func newBackend(t *testing.T, body string, table map[string]any) *Backend {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "credentials.csv")
	write(t, path, body, 0o600)
	d := map[string]any{"path": path}
	for k, v := range table {
		d[k] = v
	}
	return New(credfile.Rules{BackendName: "creds", Home: home, UID: os.Getuid(), Scope: credfile.ScopeUser}, d, 1<<20)
}

type device struct {
	name, address, platform, site string
	groups                        []string
	credkeyref                    string // the inventory's pin; blank for an unpinned device
}

func resolve(b *Backend, d device) credentials.BackendResult { return resolveWith(b, d, nil) }

// resolveWith resolves d with lookup as the request's lookup of a variable.
func resolveWith(b *Backend, d device, lookup func(string) (string, bool)) credentials.BackendResult {
	dev := inventory.Direct(d.name, d.platform, "system", 0)
	dev.Site, dev.Groups, dev.CredKeyRef = d.site, d.groups, d.credkeyref
	if d.address != "" {
		dev.ManagementAddress = netip.MustParseAddr(d.address)
	}
	return b.Resolve(context.Background(), credentials.ResolveRequest{Operator: credentials.Operator{Username: "me"}, Device: dev, Policy: "default", LookupEnv: lookup})
}

func material(t *testing.T, r credentials.BackendResult) (username, password, enable string) {
	t.Helper()
	if r.Outcome != credentials.Success {
		t.Fatalf("%+v", r)
	}
	m := r.Credential.Material
	_ = m.WithUsername(func(v []byte) error { username = string(v); return nil })
	if m.PasswordSet() {
		_ = m.WithPassword(func(v []byte) error { password = string(v); return nil })
	}
	if m.EnablePasswordSet() {
		_ = m.WithEnablePassword(func(v []byte) error { enable = string(v); return nil })
	}
	return
}

// TestFirstMatchingRowIsTheAnswer covers the selected row's result: file
// order, the operator's name for a blank username, blank secrets allowed,
// the secret kept verbatim, and the evidence.
func TestFirstMatchingRowIsTheAnswer(t *testing.T) {
	b := newBackend(t, sample, nil)
	path := b.Path
	for _, tc := range []struct {
		device                     device
		line                       int
		username, password, enable string
		pattern, credkey           string
	}{
		{device{name: "sw-nyc-01", address: "10.1.2.3"}, 3, "svc.core", "  core pass  ", "core-enable", "device_name=sw-nyc-01", "creds:3"},
		// The /8 on line 5 is above the /16 on line 6: order, not length.
		{device{name: "sw-nyc-02", address: "10.1.2.4"}, 5, "svc.ten", "ten-pass", "", "address_cidr=10.0.0.0/8", "creds:5"},
		// Site folds case; the negated group stands beside it; a blank
		// username is the operator's own name.
		{device{name: "fw-nyc-01", site: "nyc", groups: []string{"edge"}}, 7, "me", "nyc-pass", "", "site=NYC device_group=!lab credkey=nyc-ro", "nyc-ro"},
		// The lab device is excluded from line 7; the key-only row on line
		// 8 serves pinned devices alone, so the catch-all answers, with no
		// password: a legal answer where public-key login needs none.
		{device{name: "fw-nyc-02", site: "nyc", groups: []string{"LAB"}}, 9, "svc.default", "", "", "device_name=*", "creds:9"},
	} {
		r := resolve(b, tc.device)
		u, p, e := material(t, r)
		m := r.Credential.MatchedOn
		if u != tc.username || p != tc.password || e != tc.enable || m.Line != tc.line {
			t.Errorf("%s: line %d username %q password %q enable %q, want line %d %q %q %q", tc.device.name, m.Line, u, p, e, tc.line, tc.username, tc.password, tc.enable)
		}
		if m.Category != "csv_row" || m.Pattern != tc.pattern || m.Source != path || m.CredKey != tc.credkey || m.SafeValue != "" {
			t.Errorf("%s: evidence %+v, want pattern %q credkey %q source %s", tc.device.name, m, tc.pattern, tc.credkey, path)
		}
		if r.Credential.Backend != "creds" || r.Credential.Policy != "default" {
			t.Errorf("%s: backend %q policy %q", tc.device.name, r.Credential.Backend, r.Credential.Policy)
		}
	}
}

// TestNoMatchingRowIsNotFound: no row, a file
// of a header alone, and a file of comments and blank lines.
// TestAPinnedDeviceTakesItsKeysRowOrNothing covers the pin inside the
// backend, which declares the keyed capability: a device with a credkeyref
// matches only the row whose key equals it (case folded, the row's other
// cells still applying), whatever rows stand above it, and with no such row
// the answer is not found, never the catch-all. The resolver's walk over
// the sequence is TestCredKeyRefPin's.
func TestAPinnedDeviceTakesItsKeysRowOrNothing(t *testing.T) {
	b := newBackend(t, sample, nil)
	var _ credentials.Keyed = b
	if !b.HonoursCredKey() {
		t.Fatal("the credential CSV does not declare the keyed capability")
	}
	for _, tc := range []struct {
		device device
		line   int // 0: not found
	}{
		// Line 3 names this device, and the pin outranks it.
		{device{name: "sw-nyc-01", address: "10.1.2.3", credkeyref: "Core-Admin"}, 8},
		{device{name: "fw-nyc-01", site: "nyc", credkeyref: "nyc-ro"}, 7},
		// Line 7's other cells still apply: wrong site, then the negated group.
		{device{name: "fw-bos-01", site: "bos", credkeyref: "nyc-ro"}, 0},
		{device{name: "fw-nyc-02", site: "nyc", groups: []string{"lab"}, credkeyref: "nyc-ro"}, 0},
		// No such key: not the catch-all on line 9.
		{device{name: "sw-nyc-01", address: "10.1.2.3", credkeyref: "edge-admin"}, 0},
		// A generated label (creds:9) is evidence, not a key to pin.
		{device{name: "sw-nyc-01", credkeyref: "creds:9"}, 0},
	} {
		r := resolve(b, tc.device)
		if tc.line == 0 {
			if r.Outcome != credentials.NotFound {
				t.Errorf("%s pinned to %s: outcome %s line %d, want not found", tc.device.name, tc.device.credkeyref, r.Outcome, r.Credential.MatchedOn.Line)
			}
			continue
		}
		if r.Outcome != credentials.Success || r.Credential.MatchedOn.Line != tc.line {
			t.Errorf("%s pinned to %s: outcome %s line %d, want line %d", tc.device.name, tc.device.credkeyref, r.Outcome, r.Credential.MatchedOn.Line, tc.line)
		}
		if r.Credential.Material != nil {
			r.Credential.Material.Destroy()
		}
	}
}

func TestNoMatchingRowIsNotFound(t *testing.T) {
	header := "device_name,username,password\n"
	for name, body := range map[string]string{
		"no row matches":   header + "sw-bos-*,svc,pw\n",
		"a header alone":   header,
		"comments, blanks": "# nothing yet\n\n" + header + "\n# sw-*,svc,pw\n",
	} {
		if r := resolve(newBackend(t, body, nil), device{name: "sw-nyc-01"}); r.Outcome != credentials.NotFound {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

// TestEvidenceNeverEchoesTheDevice for the CSV: two
// devices that take one row have equal evidence, so the planner can merge
// their grants.
func TestEvidenceNeverEchoesTheDevice(t *testing.T) {
	b := newBackend(t, "device_name,username,password\nsw-*,svc,pw\n", nil)
	first, second := resolve(b, device{name: "sw-nyc-01"}), resolve(b, device{name: "sw-nyc-02"})
	if first.Outcome != credentials.Success || first.Credential.MatchedOn != second.Credential.MatchedOn {
		t.Fatalf("evidence differs: %+v and %+v", first.Credential.MatchedOn, second.Credential.MatchedOn)
	}
}

// TestTheAnswerIsACopy: destroying one answer's
// material leaves the cached row whole for the next device.
func TestTheAnswerIsACopy(t *testing.T) {
	b := newBackend(t, "device_name,username,password,enable_password\nsw-*,svc,pw,en\n", nil)
	first := resolve(b, device{name: "sw-nyc-01"})
	material(t, first)
	first.Credential.Material.Destroy()
	if u, p, e := material(t, resolve(b, device{name: "sw-nyc-02"})); u != "svc" || p != "pw" || e != "en" {
		t.Fatalf("after a destroy: %q %q %q", u, p, e)
	}
}

// TestLoadedSecretsAreRedacted: a stray print of the
// loaded table shows no secret.
func TestLoadedSecretsAreRedacted(t *testing.T) {
	b := newBackend(t, "device_name,username,password,enable_password\nsw-*,svc,Hunter2pw,Hunter2en\n", nil)
	loaded, failed := b.load(context.Background())
	if failed != nil {
		t.Fatalf("%+v", failed)
	}
	for _, format := range []string{"%v", "%+v", "%#v", "%s"} {
		if out := fmt.Sprintf(format, loaded.rows); strings.Contains(out, "Hunter2") {
			t.Errorf("%s prints a secret: %s", format, out)
		}
	}
}

// TestReaderKeys covers the reader keys through New: the header name for
// device_name, hyphen spellings and case, mappings as a name and as a
// list, a delimiter, numeric mode with no header row, and a byte-order
// mark.
func TestReaderKeys(t *testing.T) {
	for name, tc := range map[string]struct {
		body  string
		table map[string]any
	}{
		"the header name":             {"name,username,password\nsw-*,svc,pw\n", nil},
		"hyphens and case":            {"Device-Name,USERNAME,Password,Enable-Password\nsw-*,svc,pw,\n", nil},
		"a mapped header":             {"host,login,pass\nsw-*,svc,pw\n", map[string]any{"mappings.device_name": "host", "mappings.username": "login", "mappings.password": "pass"}},
		"a list of headers":           {"hostname,login,password\nsw-*,svc,pw\n", map[string]any{"mappings.device_name": []any{"device", "hostname"}, "mappings.username": []any{"user", "login"}}},
		"a semicolon":                 {"device_name;username;password\nsw-*;svc;pw\n", map[string]any{"delimiter": ";"}},
		"numeric mode, no header":     {"sw-*,ignored,svc,pw\n", map[string]any{"mode": "numeric", "mappings.device_name": int64(1), "mappings.username": int64(3), "mappings.password": int64(4)}},
		"a byte-order mark":           {"\xef\xbb\xbfdevice_name,username,password\nsw-*,svc,pw\n", nil},
		"a mark in numeric mode":      {"\xef\xbb\xbfsw-*,svc,pw\n", map[string]any{"mode": "numeric", "mappings.device_name": int64(1), "mappings.username": int64(2), "mappings.password": int64(3)}},
		"an unmapped extra column":    {"device_name,owner,username,password\nsw-*,team-a,svc,pw\n", nil},
		"a quoted cell":               {"device_name,username,password\n\"sw-*\",\"svc\",\"pw\"\n", nil},
		"mandatory password, present": {"device_name,username,password\nsw-*,svc,pw\n", map[string]any{"mandatory-fields": []any{"username", "password"}}},
	} {
		if u, p, _ := material(t, resolve(newBackend(t, tc.body, tc.table), device{name: "sw-nyc-01"})); u != "svc" || p != "pw" {
			t.Errorf("%s: %q %q", name, u, p)
		}
	}
}

// TestQuotedCells covers RFC 4180 quoting through the shared reader, as
// for inventory: quotes are removed, a quoted cell may hold the
// delimiter, a doubled quote is one quote, and spaces inside the quotes of a
// secret cell are kept. Under another delimiter a comma needs no quotes. A
// quote is only legal at the start of a cell: a space before it, or a quote
// inside an unquoted cell, is invalid CSV (the reader does not guess).
func TestQuotedCells(t *testing.T) {
	for name, tc := range map[string]struct {
		body     string
		table    map[string]any
		site     string
		password string
	}{
		"quotes removed":          {"site,username,password\n\"New York\",svc,pw\n", nil, "new york", "pw"},
		"the delimiter in quotes": {"site,username,password\n\"New York, NY\",svc,\" p,w \"\"x\"\" \"\n", nil, "New York, NY", " p,w \"x\" "},
		"a semicolon file":        {"site;username;password\nNew York, NY;svc;p,w\n", map[string]any{"delimiter": ";"}, "New York, NY", "p,w"},
	} {
		r := resolve(newBackend(t, tc.body, tc.table), device{name: "sw-nyc-01", site: tc.site})
		if _, p, _ := material(t, r); p != tc.password {
			t.Errorf("%s: password %q, want %q", name, p, tc.password)
		}
	}
	for name, body := range map[string]string{
		"a space before the quote":        "site,username,password\n \"New York\",svc,pw\n",
		"a quote inside an unquoted cell": "site,username,password\nNew \"York\",svc,pw\n",
	} {
		if r := resolve(newBackend(t, body, nil), device{name: "sw-nyc-01", site: "New York"}); r.ErrorCode != "credential_csv_malformed" || !strings.Contains(r.Message, "line 2") {
			t.Errorf("%s: %+v", name, r)
		}
	}
}

// TestLoadFailures covers the load failures: the three
// codes, each message naming the backend, the file, and the line, and none
// quoting a cell.
func TestLoadFailures(t *testing.T) {
	header := "device_name,address_cidr,site,credkey,username,password,enable_password\n"
	for name, tc := range map[string]struct {
		body  string
		table map[string]any
		code  string
		says  []string
	}{
		"a duplicate header": {"device_name,username,username\nsw-*,svc,pw\n", nil, "credential_csv_malformed", []string{"a header appears twice"}},
		// A file with no header row read under header mode: the first
		// credential row is the "header", and the reader's own message
		// would quote the repeated cell, here the password.
		"no header row, equal cells":   {"sw-*,Hunter2,Hunter2\n", nil, "credential_csv_malformed", []string{"mode = \"numeric\""}},
		"no username column":           {"device_name,password\nsw-*,pw\n", nil, "credential_csv_malformed", []string{"username", "no mapping"}},
		"a mandatory column missing":   {"device_name,username\nsw-*,svc\n", map[string]any{"mandatory-fields": []any{"username", "password"}}, "credential_csv_malformed", []string{"password"}},
		"numeric, mandatory unmapped":  {"sw-*,svc,pw\n", map[string]any{"mode": "numeric", "mappings.device_name": int64(1)}, "credential_csv_malformed", []string{"username"}},
		"invalid CSV":                  {header + "sw-*,,,,svc,\"Hunter2,\n", nil, "credential_csv_malformed", nil},
		"a short row":                  {header + "sw-a,,,,svc,Hunter2,en\nsw-b,,,,svc\n", nil, "credential_csv_malformed", []string{"line 3"}},
		"an overlong line":             {header + "sw-*,,,,svc," + strings.Repeat("Hunter2", 200000) + ",\n", nil, "credential_csv_malformed", []string{"physical line"}},
		"no selector":                  {header + ",,,,svc,Hunter2,\n", nil, "credential_csv_row_invalid", []string{":2:", "no selector"}},
		"only a negation":              {header + "sw-a,,,,svc,Hunter2,\n,,!lab,,svc,Hunter2,\n", nil, "credential_csv_row_invalid", []string{":3:", "only negated"}},
		"a malformed pattern":          {header + "Hunter2[x,,,,svc,pw,\n", nil, "credential_csv_row_invalid", []string{":2:", "column device_name"}},
		"a bad prefix":                 {header + ",Hunter2,,,svc,pw,\n", nil, "credential_csv_row_invalid", []string{":2:", "column address_cidr", "CIDR"}},
		"a glob key":                   {header + ",,,Hunter2*,svc,pw,\n", nil, "credential_csv_row_invalid", []string{":2:", "column credkey"}},
		"a negated key":                {header + "sw-*,,,!Hunter2,svc,pw,\n", nil, "credential_csv_row_invalid", []string{":2:", "column credkey"}},
		"no result":                    {header + "sw-a,,,,svc,Hunter2,\nsw-b,,,,  ,  ,\n", nil, "credential_csv_row_invalid", []string{":3:", "no result"}},
		"a duplicate key":              {header + "sw-a,,,Hunter2,svc,pw,\n\nsw-b,,,hunter2,svc,pw,\n", nil, "credential_csv_credkey_duplicate", []string{"lines 2 and 4"}},
		"a shifted row, secret in key": {header + "sw-a,,,,svc,pw,\nsw-b,,,Hunter2[,svc,pw,\n", nil, "credential_csv_row_invalid", []string{":3:", "column credkey"}},
		// An env: cell whose rest is no portable variable name, which may
		// be a password beginning with env:, is refused naming the column.
		"an env: cell without a name":   {header + "sw-a,,,,svc,env:,\n", nil, "credential_csv_row_invalid", []string{":2:", "column password", "variable name"}},
		"an env: cell with a hyphen":    {header + "sw-a,,,,svc,ENV:Hunter2-x,\n", nil, "credential_csv_row_invalid", []string{":2:", "column password"}},
		"an env: cell, a leading digit": {header + "sw-a,,,,env:2Hunter2,pw,\n", nil, "credential_csv_row_invalid", []string{":2:", "column username"}},
		"an env: cell with a space":     {header + "sw-a,,,,svc,pw,env:Hunter2 x\n", nil, "credential_csv_row_invalid", []string{":2:", "column enable_password"}},
	} {
		b := newBackend(t, tc.body, tc.table)
		r := resolve(b, device{name: "sw-nyc-01"})
		if r.Outcome != credentials.Malformed || r.ErrorCode != tc.code {
			t.Errorf("%s: %+v, want %s", name, r, tc.code)
			continue
		}
		for _, want := range append([]string{"credential backend creds", b.Path}, tc.says...) {
			if !strings.Contains(r.Message, want) {
				t.Errorf("%s: the message lacks %q: %s", name, want, r.Message)
			}
		}
		if strings.Contains(r.Message, "tabular_") {
			t.Errorf("%s: the message carries the reader's code: %s", name, r.Message)
		}
		if strings.Contains(strings.ToLower(r.Message), "hunter2") {
			t.Errorf("%s: the message quotes a cell: %s", name, r.Message)
		}
	}
}

// TestABadRowFailsEveryDevice: every row is checked at
// load, so a bad last row fails a device that the first row would serve,
// and the failure is read once and cached.
func TestABadRowFailsEveryDevice(t *testing.T) {
	b := newBackend(t, "device_name,username,password\nsw-nyc-01,svc,pw\nsw-[bos,svc,pw\n", nil)
	first := resolve(b, device{name: "sw-nyc-01"})
	if first.ErrorCode != "credential_csv_row_invalid" {
		t.Fatalf("%+v", first)
	}
	if err := os.Remove(b.Path); err != nil {
		t.Fatal(err)
	}
	if again := resolve(b, device{name: "sw-nyc-01"}); again.ErrorCode != first.ErrorCode || again.Message != first.Message {
		t.Fatalf("the failure was not cached: %+v", again)
	}
}

// TestReadOnce: one read per backend; a later change to
// the file is not seen.
func TestReadOnce(t *testing.T) {
	b := newBackend(t, "device_name,username,password\nsw-*,svc,pw\n", nil)
	material(t, resolve(b, device{name: "sw-nyc-01"}))
	write(t, b.Path, "device_name,username,password\nsw-*,other,pw\n", 0o600)
	if u, _, _ := material(t, resolve(b, device{name: "sw-nyc-02"})); u != "svc" {
		t.Fatalf("the file was read again: %q", u)
	}
}

// TestFileRules covers the file rules for the CSV: the shared
// rules run before parsing; an optional user file that is absent is a
// silent not-found, a required one is credential_file_unavailable, and a
// present file with an unsafe mode fails whatever required says, before a
// byte of it is parsed.
func TestFileRules(t *testing.T) {
	home := t.TempDir()
	rules := func(required bool) credfile.Rules {
		return credfile.Rules{BackendName: "creds", Home: home, UID: os.Getuid(), Scope: credfile.ScopeUser, Required: required}
	}
	absent := map[string]any{"path": "~/absent.csv"}
	if r := resolve(New(rules(false), absent, 0), device{name: "sw-nyc-01"}); r.Outcome != credentials.NotFound {
		t.Errorf("optional and absent: %+v", r)
	}
	if r := resolve(New(rules(true), absent, 0), device{name: "sw-nyc-01"}); r.ErrorCode != "credential_file_unavailable" || !strings.Contains(r.Message, filepath.Join(home, "absent.csv")) {
		t.Errorf("required and absent: %+v", r)
	}
	// Not CSV at all: the mode check answers first.
	write(t, filepath.Join(home, "open.csv"), "\"unclosed\n", 0o644)
	if r := resolve(New(rules(false), map[string]any{"path": "~/open.csv"}, 0), device{name: "sw-nyc-01"}); r.ErrorCode != "credential_file_mode_unsafe" {
		t.Errorf("mode 0644: %+v", r)
	}
}

// TestEnvReference is EXAMPLES chapter 55, issues 1 and 3: a result cell
// env:NAME, its surrounding white space trimmed and the prefix's case
// ignored, takes the variable's value through the request's lookup; a plain
// cell naming a set variable is a literal; an unset or empty variable is
// credential_env_missing, naming the file, the line, the column, and the
// variable; a request without a lookup reads no variable; a row no device
// took is not read, and a row whose results are all references loads.
func TestEnvReference(t *testing.T) {
	body := "device_name,username,password,enable_password\n" +
		"sw-a,env:CORE_USER, ENV:CORE_PASS ,Env:CORE_EN\n" +
		"sw-b,CORE_USER,CORE_PASS,\n" +
		"sw-c,svc,env:UNSET,\n" +
		"sw-d,svc,pw,env:EMPTY\n" +
		"sw-e,env:UNREAD,env:UNREAD,env:UNREAD\n"
	env := map[string]string{"CORE_USER": "svc.core", "CORE_PASS": "from-env", "CORE_EN": "en-env", "EMPTY": ""}
	var asked []string
	lookup := func(name string) (string, bool) {
		asked = append(asked, name)
		v, ok := env[name]
		return v, ok
	}
	b := newBackend(t, body, nil)
	if u, p, e := material(t, resolveWith(b, device{name: "sw-a"}, lookup)); u != "svc.core" || p != "from-env" || e != "en-env" {
		t.Errorf("references: %q %q %q", u, p, e)
	}
	if u, p, e := material(t, resolveWith(b, device{name: "sw-b"}, lookup)); u != "CORE_USER" || p != "CORE_PASS" || e != "" {
		t.Errorf("plain cells: %q %q %q", u, p, e)
	}
	for _, tc := range []struct {
		device string
		lookup func(string) (string, bool)
		says   string
	}{
		{"sw-c", lookup, ":4 password: UNSET not set"},
		{"sw-d", lookup, ":5 enable_password: EMPTY empty"},
		{"sw-a", nil, ":2 username: CORE_USER not set"},
	} {
		r := resolveWith(b, device{name: tc.device}, tc.lookup)
		if r.Outcome != credentials.Incomplete || r.ErrorCode != "credential_env_missing" || r.Message != "credential backend creds: "+b.Path+tc.says {
			t.Errorf("%s: %+v, want %q", tc.device, r, tc.says)
		}
	}
	for _, name := range asked {
		if name == "UNREAD" {
			t.Errorf("a row no device took was read: %v", asked)
		}
	}
}
