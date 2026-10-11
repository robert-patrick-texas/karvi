package envindirect

import "testing"

// TestReference: a reference is a cell whose text, its surrounding white
// space trimmed, begins with env: in any case; its name is valid when it is a
// portable variable name; any other cell is a literal.
func TestReference(t *testing.T) {
	type want struct {
		name       string
		ref, valid bool
	}
	for cell, w := range map[string]want{
		"env:NETPASS":         {"NETPASS", true, true},
		" ENV:NETPASS_NYC ":   {"NETPASS_NYC", true, true},
		"Env:_x9":             {"_x9", true, true},
		"env:":                {"", true, false},
		"env:NETPASS-NYC":     {"NETPASS-NYC", true, false},
		"env:9X":              {"9X", true, false},
		"env: X":              {" X", true, false},
		"env:p$ss":            {"p$ss", true, false},
		"NETPASS":             {"", false, false},
		"en:X":                {"", false, false},
		"pw env:X":            {"", false, false},
		"":                    {"", false, false},
		"environment:NETPASS": {"", false, false},
	} {
		name, ref, valid := Reference(cell)
		if (want{name, ref, valid}) != w {
			t.Errorf("%q: %q %v %v, want %+v", cell, name, ref, valid, w)
		}
	}
}

// TestRead: a set variable gives its value; an unset or empty one gives the
// reason naming it; a nil lookup reads no variable.
func TestRead(t *testing.T) {
	env := map[string]string{"SET": "v", "EMPTY": ""}
	lookup := func(name string) (string, bool) { v, ok := env[name]; return v, ok }
	for _, tc := range []struct {
		name           string
		lookup         func(string) (string, bool)
		value, missing string
	}{
		{"SET", lookup, "v", ""},
		{"EMPTY", lookup, "", "EMPTY empty"},
		{"UNSET", lookup, "", "UNSET not set"},
		{"SET", nil, "", "SET not set"},
	} {
		if v, m := Read(tc.name, tc.lookup); v != tc.value || m != tc.missing {
			t.Errorf("%s: %q %q, want %q %q", tc.name, v, m, tc.value, tc.missing)
		}
	}
}

// TestRecord: a store record's three fields, each a literal as written, a
// variable's name included, or a reference read through the lookup; absent
// fields are unset, and none present is not found; an unset or empty
// variable is credential_env_missing naming the record and the field; a
// reference to no variable name is the store's malformed code, never
// quoting the field.
func TestRecord(t *testing.T) {
	env := map[string]string{"SET": "v", "EMPTY": "", "USER": "not-read"}
	lookup := func(name string) (string, bool) { v, ok := env[name]; return v, ok }
	names := [3]string{"username", "password", "enable_password"}
	record := func(m map[string]string) func(string) (string, bool) {
		return func(f string) (string, bool) { v, ok := m[f]; return v, ok }
	}
	values, found, failed := Record("r", "k:netops", "redis_malformed", names, record(map[string]string{"username": "USER", "password": " ENV:SET ", "enable_password": ""}), lookup)
	if values != [3]string{"USER", "v", ""} || !found || failed != nil {
		t.Errorf("literal and reference: %q %v %+v", values, found, failed)
	}
	if _, found, failed := Record("r", "k:netops", "redis_malformed", names, record(map[string]string{"other": "x"}), lookup); found || failed != nil {
		t.Errorf("none of the three: %v %+v", found, failed)
	}
	for field, want := range map[string]string{"env:EMPTY": "credential backend r: k:netops password: EMPTY empty", "env:UNSET": "credential backend r: k:netops password: UNSET not set"} {
		_, _, failed := Record("r", "k:netops", "redis_malformed", names, record(map[string]string{"password": field}), lookup)
		if failed == nil || failed.ErrorCode != "credential_env_missing" || failed.Message != want {
			t.Errorf("%s: %+v, want %q", field, failed, want)
		}
	}
	for _, field := range []string{"env:p$ss", "env:"} {
		_, _, failed := Record("v", "v1/secret/data/karvi/netops", "vault_response_malformed", names, record(map[string]string{"password": field}), lookup)
		if failed == nil || failed.ErrorCode != "vault_response_malformed" || failed.Message != "credential backend v: v1/secret/data/karvi/netops password: env: not followed by a variable name (letters, digits, underscores; no leading digit)" {
			t.Errorf("%q: %+v", field, failed)
		}
	}
	if _, _, failed := Record("r", "k", "redis_malformed", names, record(map[string]string{"password": "env:SET"}), nil); failed == nil || failed.Message != "credential backend r: k password: SET not set" {
		t.Errorf("no lookup: %+v", failed)
	}
}
