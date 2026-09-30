package inventoryload

import (
	"context"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// loadCSV writes one inventory source with the given CSV and source keys and
// loads it through the configured loader.
func loadCSV(t *testing.T, csv string, sourceKeys ...string) ([]inventory.Device, error) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "inv.csv"), []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	toml := "[[inventory-source]]\nname = \"t\"\ntype = \"csv\"\npath = \"" + filepath.Join(dir, "inv.csv") + "\"\nrequired = true\n" + strings.Join(sourceKeys, "\n") + "\n"
	cfgPath := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(cfgPath, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: []string{cfgPath}, HomeDir: dir, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	devices, _, err := Loader{Config: cfg, Home: dir}.Load(context.Background())
	return devices, err
}

func addrs(list ...string) []netip.Addr {
	out := []netip.Addr{}
	for _, s := range list {
		out = append(out, netip.MustParseAddr(s))
	}
	return out
}

// TestV0100AddressAuthorityAndAlternatesColumns covers the address
// authority and alternates columns: the column under its alias, the source
// default beside the
// platform default, and the alternates column parsed in row order.
func TestV0100AddressAuthorityAndAlternatesColumns(t *testing.T) {
	devices, err := loadCSV(t,
		"name,management_address,alternate_ips,dns_authority,site\n"+
			"Core-A,127.0.0.1,\"10.0.0.3;10.0.0.2;127.0.0.1;10.0.0.3\",Daemon,hq\n"+
			"edge-b,127.0.0.1,,,branch\n",
		`defaults.address-authority = "client"`, `defaults.platform = "generic"`)
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 2 {
		t.Fatalf("devices=%d", len(devices))
	}
	core, edge := devices[0], devices[1]
	if core.AddressAuthority != inventory.AuthorityDaemon || edge.AddressAuthority != inventory.AuthorityClient {
		t.Fatalf("authority core=%q edge=%q", core.AddressAuthority, edge.AddressAuthority)
	}
	if got, want := core.Alternates(), addrs("10.0.0.3", "10.0.0.2"); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("alternates=%v, want %v (row order, management and duplicates dropped)", got, want)
	}
	if len(edge.Alternates()) != 0 || edge.ManagementAddress != netip.MustParseAddr("127.0.0.1") {
		t.Fatalf("edge-b: %+v", edge)
	}
	if core.Platform != "generic" {
		t.Fatalf("platform default not applied: %q", core.Platform)
	}
	// The underscore spellings load too, and an explicit column beats the default.
	devices, err = loadCSV(t, "name,management_address,addresses,address_authority\nr1,192.0.2.1,192.0.2.9,client\nr2,192.0.2.2,,\n", `defaults.address-authority = "daemon"`)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].AddressAuthority != "client" || devices[1].AddressAuthority != "daemon" || len(devices[0].Alternates()) != 1 {
		t.Fatalf("%+v %+v", devices[0], devices[1])
	}
}

func TestV0100AlternatesAndAuthorityRefusals(t *testing.T) {
	for _, tc := range []struct{ name, csv, code string }{
		{"alternates without management", "name,addresses\nr1,10.0.0.2\n", "inventory_alternate_without_management"},
		{"bad alternate", "name,management_address,addresses\nr1,10.0.0.1,not-an-ip\n", "inventory_alternate_address_invalid"},
		{"bad authority", "name,address_authority\nr1,executor\n", "inventory_address_authority_invalid"},
	} {
		_, err := loadCSV(t, tc.csv)
		if errorcodes.Of(err) != tc.code {
			t.Errorf("%s: code=%q err=%v, want %s", tc.name, errorcodes.Of(err), err, tc.code)
		}
	}
	// A bad source default is refused the same way.
	if _, err := loadCSV(t, "name\nr1\n", `defaults.address-authority = "executor"`); errorcodes.Of(err) != "inventory_address_authority_invalid" {
		t.Errorf("bad default: %v", err)
	}
}

// TestV0100DuplicateRowsCompareAuthorityAndAlternates: two rows for one device
// that differ only in authority or alternates are an inventory_conflict;
// identical rows coalesce as before.
func TestV0100DuplicateRowsCompareAuthorityAndAlternates(t *testing.T) {
	for _, tc := range []struct{ name, rows, code string }{
		{"same", "r1,10.0.0.1,10.0.0.2,client\nr1,10.0.0.1,10.0.0.2,client\n", ""},
		{"authority differs", "r1,10.0.0.1,10.0.0.2,client\nr1,10.0.0.1,10.0.0.2,daemon\n", "inventory_conflict"},
		{"alternates differ", "r1,10.0.0.1,10.0.0.2,client\nr1,10.0.0.1,10.0.0.3,client\n", "inventory_conflict"},
	} {
		devices, err := loadCSV(t, "name,management_address,addresses,address_authority\n"+tc.rows)
		if errorcodes.Of(err) != tc.code {
			t.Errorf("%s: code=%q err=%v, want %q", tc.name, errorcodes.Of(err), err, tc.code)
		}
		if tc.code == "" && len(devices) != 1 {
			t.Errorf("%s: devices=%d, want 1", tc.name, len(devices))
		}
	}
}

// TestPlatformNamesNormalise: a
// row's platform and a source's defaults.platform are trimmed and lowercased
// on loading, so the record's platform is the one spelling every check
// compares.
func TestPlatformNamesNormalise(t *testing.T) {
	devices, err := loadCSV(t,
		"name,management_address,platform\n"+
			"a,192.0.2.1, Cisco_IOSXE \n"+
			"b,192.0.2.2,\n",
		`defaults.platform = " Juniper_JUNOS "`)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Platform != "cisco_iosxe" || devices[1].Platform != "juniper_junos" {
		t.Fatalf("platforms %q, %q", devices[0].Platform, devices[1].Platform)
	}
}

// TestPlatformNotSetStaysBlank: a row
// without a platform and without a source default loads blank (not set), a
// source default sets it, and a row's own value beats the default.
func TestPlatformNotSetStaysBlank(t *testing.T) {
	devices, err := loadCSV(t, "name,management_address,platform\nr1,192.0.2.1,\nr2,192.0.2.2,cisco_iosxe\n")
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Platform != "" || devices[1].Platform != "cisco_iosxe" {
		t.Fatalf("platforms %q, %q", devices[0].Platform, devices[1].Platform)
	}
	devices, err = loadCSV(t, "name,management_address,platform\nr1,192.0.2.1,\nr2,192.0.2.2,cisco_iosxe\n", `defaults.platform = "Generic"`)
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].Platform != "generic" || devices[1].Platform != "cisco_iosxe" {
		t.Fatalf("with a default: %q, %q", devices[0].Platform, devices[1].Platform)
	}
}

// TestCredKeyRefColumn: credkeyref is a read
// inventory field, found under its own name without a mapping or under a
// mapped header, trimmed, kept as written (the comparison folds case, the
// device does not), and blank for a row that pins nothing.
func TestCredKeyRefColumn(t *testing.T) {
	devices, err := loadCSV(t, "name,management_address,credkeyref\nsw-nyc-01,10.1.2.3, NYC-Core \nsw-nyc-02,10.1.2.4,\n")
	if err != nil {
		t.Fatal(err)
	}
	if devices[0].CredKeyRef != "NYC-Core" || devices[1].CredKeyRef != "" {
		t.Fatalf("credkeyref %q, %q", devices[0].CredKeyRef, devices[1].CredKeyRef)
	}
	// A mapped header, and numeric mode with a column number.
	devices, err = loadCSV(t, "name,management_address,Cred Pin\nsw-nyc-01,10.1.2.3,nyc-core\n", `mappings.credkeyref = "Cred Pin"`)
	if err != nil || devices[0].CredKeyRef != "nyc-core" {
		t.Fatalf("mapped header: %v %+v", err, devices)
	}
	devices, err = loadCSV(t, "sw-nyc-01,10.1.2.3,nyc-core\n", `mode = "numeric"`, `mappings.name = 1`, `mappings.management_address = 2`, `mappings.credkeyref = 3`)
	if err != nil || devices[0].CredKeyRef != "nyc-core" {
		t.Fatalf("numeric mode: %v %+v", err, devices)
	}
}

// TestCredKeyRefLeavesAnUnpinnedDeviceUnchanged:
// a blank credkeyref is not encoded on the device, and a set one is.
func TestCredKeyRefLeavesAnUnpinnedDeviceUnchanged(t *testing.T) {
	blank, err := loadCSV(t, "name,management_address,credkeyref\nr1,192.0.2.1,\n")
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := loadCSV(t, "name,management_address,credkeyref\nr1,192.0.2.1,lab\n")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(blank[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "credkeyref") {
		t.Fatalf("a blank pin is encoded: %s", encoded)
	}
	if encoded, _ = json.Marshal(pinned[0]); !strings.Contains(string(encoded), `"credkeyref":"lab"`) {
		t.Fatalf("a pin is not encoded: %s", encoded)
	}
}

// TestCredKeyRefInvalid: a value that is not a legal key
// (matching.CheckKey, the credential CSV's own rule for credkey) fails the
// load with inventory_credkeyref_invalid, naming the file, the line, and
// the column, and never the cell.
func TestCredKeyRefInvalid(t *testing.T) {
	for _, cell := range []string{"nyc-*", "lab?", "[ab]", `a\b`, "!lab"} {
		_, err := loadCSV(t, "name,management_address,credkeyref\nr1,192.0.2.1,ok\nr2,192.0.2.2,"+cell+"\n")
		if errorcodes.Of(err) != "inventory_credkeyref_invalid" {
			t.Errorf("%q: code=%q err=%v", cell, errorcodes.Of(err), err)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, "inv.csv:3:") || !strings.Contains(msg, "column credkeyref") {
			t.Errorf("%q: the message does not name the file, the line, and the column: %s", cell, msg)
		}
		if strings.Contains(msg, cell) {
			t.Errorf("%q: the message quotes the cell: %s", cell, msg)
		}
	}
}

// TestCredKeyRefDuplicateRows: two rows for one device coalesce when their
// pins are one key (keys compare without regard to case) and
// conflict when they differ, a pin against no pin included.
func TestCredKeyRefDuplicateRows(t *testing.T) {
	for _, tc := range []struct{ name, rows, code string }{
		{"same key, case differs", "r1,10.0.0.1,Lab\nr1,10.0.0.1,lab\n", ""},
		{"keys differ", "r1,10.0.0.1,lab\nr1,10.0.0.1,core\n", "inventory_conflict"},
		{"a pin and no pin", "r1,10.0.0.1,lab\nr1,10.0.0.1,\n", "inventory_conflict"},
	} {
		devices, err := loadCSV(t, "name,management_address,credkeyref\n"+tc.rows)
		if errorcodes.Of(err) != tc.code {
			t.Errorf("%s: code=%q err=%v, want %q", tc.name, errorcodes.Of(err), err, tc.code)
		}
		if tc.code == "" && len(devices) != 1 {
			t.Errorf("%s: devices=%d, want 1", tc.name, len(devices))
		}
	}
}

// TestSecretColumnRefused: an inventory file with a header that names a
// secret, mapped or
// not, fails the load with inventory_secret_column. The message names the
// source, the file, the column's number, and the word, and never the
// header's text or a cell.
func TestSecretColumnRefused(t *testing.T) {
	for _, tc := range []struct{ name, csv, column, word string }{
		{"an unmapped password column", "name,management_address,password\nr1,192.0.2.1,Hunter2-cell\n", "column 3", `"password"`},
		{"a suffix, hyphen spelling, any case", "name,Enable-Password,site\nr1,Hunter2-cell,nyc\n", "column 2", `"password"`},
		{"a token", "name,site,api_token\nr1,nyc,Hunter2-cell\n", "column 3", `"token"`},
		{"a private key", "ssh_private_key,name\nHunter2-cell,r1\n", "column 1", `"private_key"`},
		// No header row: the first device row is read as the header, and a
		// password that ends with a listed word must not be printed.
		{"no header row", "r1,192.0.2.1,admin,TopSecret\nr2,192.0.2.2,admin,TopSecret\n", "column 4", `"secret"`},
	} {
		_, err := loadCSV(t, tc.csv)
		if errorcodes.Of(err) != "inventory_secret_column" {
			t.Errorf("%s: code=%q err=%v", tc.name, errorcodes.Of(err), err)
			continue
		}
		msg := err.Error()
		for _, want := range []string{"inventory source t", "inv.csv", tc.column, tc.word, "credential CSV"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s: the message lacks %q: %s", tc.name, want, msg)
			}
		}
		for _, never := range []string{"Hunter2-cell", "TopSecret", "Enable-Password", "api_token"} {
			if strings.Contains(msg, never) {
				t.Errorf("%s: the message quotes %q: %s", tc.name, never, msg)
			}
		}
	}
	// A mapped header is refused like any other: mapping a secret column to
	// an innocent field does not make the file acceptable.
	if _, err := loadCSV(t, "name,password\nr1,nyc\n", `mappings.site = "password"`); errorcodes.Of(err) != "inventory_secret_column" {
		t.Errorf("a mapped secret header: %v", err)
	}
	// Names that hold a listed word elsewhere than at the end, and the pin
	// column, load.
	devices, err := loadCSV(t, "name,token_ring,password_age,credkeyref\nr1,yes,30,lab\n")
	if err != nil || len(devices) != 1 || devices[0].CredKeyRef != "lab" {
		t.Errorf("innocent headers: %v %+v", err, devices)
	}
	// Numeric mode has no headers to check; a first row that looks like
	// one is data.
	if _, err := loadCSV(t, "r1,192.0.2.1,password\n", `mode = "numeric"`, `mappings.name = 1`, `mappings.management_address = 2`); err != nil {
		t.Errorf("numeric mode: %v", err)
	}
}
