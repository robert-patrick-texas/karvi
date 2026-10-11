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
		{"EMPTY", lookup, "", "EMPTY is empty"},
		{"UNSET", lookup, "", "UNSET is not set"},
		{"SET", nil, "", "SET is not set"},
	} {
		if v, m := Read(tc.name, tc.lookup); v != tc.value || m != tc.missing {
			t.Errorf("%s: %q %q, want %q %q", tc.name, v, m, tc.value, tc.missing)
		}
	}
}
