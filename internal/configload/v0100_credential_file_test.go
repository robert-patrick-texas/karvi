package configload

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestV0100CredentialFileBackendKeys: scope
// and required on a file backend, the three rules with their codes, the
// generic enum and type codes, and rejection on a non-file type.
func TestV0100CredentialFileBackendKeys(t *testing.T) {
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	rancid := func(sets ...string) []string {
		return append([]string{`credential-backend.rancid.type="cloginrc"`}, sets...)
	}
	for _, sets := range [][]string{
		rancid(),
		rancid(`credential-backend.rancid.scope="user"`, `credential-backend.rancid.required=false`),
		rancid(`credential-backend.rancid.scope="user"`, `credential-backend.rancid.required=true`, `credential-backend.rancid.path="~/.cloginrc"`),
		rancid(`credential-backend.rancid.scope="shared"`, `credential-backend.rancid.path="/etc/karvi/credentials/cloginrc"`),
		rancid(`credential-backend.rancid.scope="shared"`, `credential-backend.rancid.required=true`, `credential-backend.rancid.path="/opt/karvi/cloginrc"`),
	} {
		if _, err := load(sets...); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
	for _, tc := range []struct {
		sets []string
		code string
	}{
		{rancid(`credential-backend.rancid.scope="shared"`, `credential-backend.rancid.required=false`, `credential-backend.rancid.path="/etc/karvi/cloginrc"`), "config_credential_backend_shared_optional"},
		{rancid(`credential-backend.rancid.scope="shared"`), "config_credential_backend_shared_path_relative"},
		{rancid(`credential-backend.rancid.scope="shared"`, `credential-backend.rancid.path="~/.cloginrc"`), "config_credential_backend_shared_path_relative"},
		{rancid(`credential-backend.rancid.scope="shared"`, `credential-backend.rancid.path="credentials/cloginrc"`), "config_credential_backend_shared_path_relative"},
		{rancid(`credential-backend.rancid.scope="bogus"`), "config_enum_value_invalid"},
		{rancid(`credential-backend.rancid.required="yes"`), "config_type_error"},
		{[]string{`credential-backend.e.type="env"`, `credential-backend.e.scope="user"`}, "config_credential_backend_scope_unsupported"},
		{[]string{`credential-backend.e.type="env"`, `credential-backend.e.required=true`}, "config_credential_backend_scope_unsupported"},
	} {
		_, err := load(tc.sets...)
		if got := errorcodes.Of(err); got != tc.code {
			t.Errorf("%v: code %q, want %s (%v)", tc.sets, got, tc.code, err)
		}
	}
}

// TestCredentialCSVBackendKeys: the csv type is a
// file backend with an explicit scope and a path, the reader keys are typed
// for the mode, mandatory-fields names logical fields, the csv-only keys are
// refused on another type, and validation never opens the file (every path
// here names a file that does not exist).
func TestCredentialCSVBackendKeys(t *testing.T) {
	load := func(sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
	}
	csv := func(sets ...string) []string {
		return append([]string{`credential-backend.creds.type="csv"`}, sets...)
	}
	user := func(sets ...string) []string {
		return csv(append([]string{`credential-backend.creds.scope="user"`, `credential-backend.creds.path="~/.karvi/credentials.csv"`}, sets...)...)
	}
	for _, sets := range [][]string{
		user(),
		user(`credential-backend.creds.required=true`),
		csv(`credential-backend.creds.scope="shared"`, `credential-backend.creds.path="/etc/karvi/credentials/credentials.csv"`),
		csv(`credential-backend.creds.scope="shared"`, `credential-backend.creds.required=true`, `credential-backend.creds.path="/etc/karvi/credentials/credentials.csv"`),
		user(`credential-backend.creds.mode="header"`, `credential-backend.creds.delimiter=";"`,
			`credential-backend.creds.mappings.device_name="Host"`,
			`credential-backend.creds.mappings.username=["login", "user"]`,
			`credential-backend.creds.mandatory-fields=["username", "password"]`),
		user(`credential-backend.creds.mode="numeric"`,
			`credential-backend.creds.mappings.device_name=1`,
			`credential-backend.creds.mappings.username=2`,
			`credential-backend.creds.mappings.password=3`),
		// Every logical field is a legal mapping key and a legal mandatory field.
		user(`credential-backend.creds.mappings.address_cidr="net"`, `credential-backend.creds.mappings.platform="os"`,
			`credential-backend.creds.mappings.site="location"`, `credential-backend.creds.mappings.device_group="grp"`,
			`credential-backend.creds.mappings.credkey="key"`, `credential-backend.creds.mappings.enable_password="enable"`,
			`credential-backend.creds.mandatory-fields=["device_name", "address_cidr", "platform", "site", "device_group", "credkey", "username", "password", "enable_password"]`),
		user(`credential-backend.creds.env-indirection.password=true`, `credential-backend.creds.transform="default"`),
	} {
		if _, err := load(sets...); err != nil {
			t.Errorf("%v: %v", sets, err)
		}
	}
	for _, tc := range []struct {
		sets []string
		code string
	}{
		// Scope is declared, never defaulted; path has no default.
		{csv(), "config_credential_backend_scope_missing"},
		{csv(`credential-backend.creds.path="~/credentials.csv"`), "config_credential_backend_scope_missing"},
		{csv(`credential-backend.creds.scope="user"`), "config_credential_backend_path_missing"},
		{csv(`credential-backend.creds.scope="user"`, `credential-backend.creds.path=" "`), "config_credential_backend_path_missing"},
		{csv(`credential-backend.creds.scope="shared"`), "config_credential_backend_path_missing"},
		{csv(`credential-backend.creds.scope="user"`, `credential-backend.creds.path=7`), "config_type_error"},
		// The file backends' rules hold unchanged for csv.
		{csv(`credential-backend.creds.scope="shared"`, `credential-backend.creds.required=false`, `credential-backend.creds.path="/etc/karvi/credentials.csv"`), "config_credential_backend_shared_optional"},
		{csv(`credential-backend.creds.scope="shared"`, `credential-backend.creds.path="~/credentials.csv"`), "config_credential_backend_shared_path_relative"},
		{csv(`credential-backend.creds.scope="bogus"`, `credential-backend.creds.path="/x"`), "config_enum_value_invalid"},
		{user(`credential-backend.creds.required="yes"`), "config_type_error"},
		// The reader keys.
		{user(`credential-backend.creds.mode="sniff"`), "config_enum_value_invalid"},
		{user(`credential-backend.creds.mode=1`), "config_type_error"},
		{user(`credential-backend.creds.delimiter=";;"`), "config_credential_backend_delimiter_invalid"},
		{user(`credential-backend.creds.delimiter=""`), "config_credential_backend_delimiter_invalid"},
		{user(`credential-backend.creds.delimiter=9`), "config_type_error"},
		{user(`credential-backend.creds.mappings.username=2`), "config_type_error"},
		{user(`credential-backend.creds.mappings.username=""`), "config_type_error"},
		{user(`credential-backend.creds.mappings.username=[]`), "config_type_error"},
		{user(`credential-backend.creds.mappings.username=["login", 2]`), "config_type_error"},
		{user(`credential-backend.creds.mode="numeric"`, `credential-backend.creds.mappings.username="login"`), "config_type_error"},
		{user(`credential-backend.creds.mode="numeric"`, `credential-backend.creds.mappings.username=0`), "config_type_error"},
		{user(`credential-backend.creds.mandatory-fields="username"`), "config_type_error"},
		{user(`credential-backend.creds.mandatory-fields=["username", 3]`), "config_type_error"},
		{user(`credential-backend.creds.mandatory-fields=["name"]`), "config_enum_value_invalid"},
		// The schema's allow-list admits a mapping for a logical field only.
		{user(`credential-backend.creds.mappings.name="Host"`), "config_unknown_key"},
		{user(`credential-backend.creds.mappings.attributes.owner="owner"`), "config_unknown_key"},
		{user(`credential-backend.creds.comment-prefix=";"`), "config_unknown_key"},
		{user(`credential-backend.creds.include="other.csv"`), "config_unknown_key"},
		// The csv-only keys on another type.
		{[]string{`credential-backend.rancid.type="cloginrc"`, `credential-backend.rancid.delimiter=","`}, "config_credential_backend_key_unsupported"},
		{[]string{`credential-backend.e.type="env"`, `credential-backend.e.mode="header"`}, "config_credential_backend_key_unsupported"},
		{[]string{`credential-backend.e.type="env"`, `credential-backend.e.mandatory-fields=["username"]`}, "config_credential_backend_key_unsupported"},
		{[]string{`credential-backend.e.type="env"`, `credential-backend.e.mappings.username="login"`}, "config_credential_backend_key_unsupported"},
	} {
		_, err := load(tc.sets...)
		if got := errorcodes.Of(err); got != tc.code {
			t.Errorf("%v: code %q, want %s (%v)", tc.sets, got, tc.code, err)
		}
	}
}

// TestCredentialCSVKeyUnsupportedNamesOneKey pins the report for a table
// with several csv-only keys to the first in sorted order, so the same
// configuration always fails the same way.
func TestCredentialCSVKeyUnsupportedNamesOneKey(t *testing.T) {
	for i := 0; i < 20; i++ {
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{
			`credential-backend.e.type="env"`, `credential-backend.e.mode="header"`, `credential-backend.e.delimiter=","`, `credential-backend.e.mappings.username="u"`,
		}})
		if got := errorcodes.Of(err); got != "config_credential_backend_key_unsupported" {
			t.Fatalf("code %q (%v)", got, err)
		}
		if !strings.Contains(err.Error(), "credential-backend.e.delimiter") {
			t.Fatalf("the report names %v, want credential-backend.e.delimiter", err)
		}
	}
}

// TestInventorySourceSecretMapping: an attribute
// mapping whose name marks a secret is config_inventory_source_secret_mapping
// at config validate, in numeric mode (the one name such a file has) and in
// header mode (the one way a secret could arrive under an innocent header);
// several such keys report the first in sorted order.
func TestInventorySourceSecretMapping(t *testing.T) {
	source := []string{`inventory-source.0.name="inv"`, `inventory-source.0.type="csv"`, `inventory-source.0.path="/tmp/inv.csv"`}
	for _, tc := range []struct {
		name string
		sets []string
		key  string // "" when the configuration is valid
	}{
		{"numeric mode", []string{`inventory-source.0.mode="numeric"`, `inventory-source.0.mappings.name=1`, `inventory-source.0.mappings.attributes.password=4`}, "inventory-source.0.mappings.attributes.password"},
		{"header mode", []string{`inventory-source.0.mappings.attributes.Enable-Password="pw"`}, "inventory-source.0.mappings.attributes.Enable-Password"},
		{"the first in sorted order", []string{`inventory-source.0.mappings.attributes.snmp_secret="s"`, `inventory-source.0.mappings.attributes.api_token="t"`}, "inventory-source.0.mappings.attributes.api_token"},
		{"innocent attributes", []string{`inventory-source.0.mappings.attributes.owner="owner"`, `inventory-source.0.mappings.attributes.token_ring="tr"`, `inventory-source.0.mappings.credkeyref="pin"`}, ""},
	} {
		for i := 0; i < 5; i++ {
			_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: append(append([]string(nil), source...), tc.sets...)})
			if tc.key == "" {
				if err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
				continue
			}
			if got := errorcodes.Of(err); got != "config_inventory_source_secret_mapping" {
				t.Fatalf("%s: code %q (%v)", tc.name, got, err)
			}
			if !strings.Contains(err.Error(), tc.key) || !strings.Contains(err.Error(), "credential CSV") {
				t.Fatalf("%s: the report lacks the key or the remedy: %v", tc.name, err)
			}
		}
	}
}
