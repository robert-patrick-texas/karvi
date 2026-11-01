package tomlmini

import "testing"

func TestParseCore(t *testing.T) {
	s := `[config]
schema-version = 1
[[inventory-source]]
name = "a"
[inventory-source.mappings]
name = ["hostname", "name"]
[config-lock]
"ssh.*" = true
[name-transform.default]
operations = [{op="lowercase"}, {op="add-suffix", suffix=".example"}]
`
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if d.Root["config"].(map[string]any)["schema-version"].(int64) != 1 {
		t.Fatal("bad int")
	}
	arr := d.Root["inventory-source"].([]any)
	if len(arr) != 1 {
		t.Fatal("bad aot")
	}
	if !d.Root["config-lock"].(map[string]any)["ssh.*"].(bool) {
		t.Fatal("bad quoted key")
	}
}
func TestDuplicateKey(t *testing.T) {
	if _, err := Parse([]byte("a=1\na=2\n")); err == nil {
		t.Fatal("expected duplicate")
	}
}
func TestMultilineIncludeText(t *testing.T) {
	d, err := Parse([]byte("x=\"\"\"a\n@include nope\nb\"\"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Root["x"].(string) == "" {
		t.Fatal("empty")
	}
}

// stringValue parses one document holding x and returns x, or the parse
// error.
func stringValue(text string) (any, error) {
	d, err := Parse([]byte(text))
	if err != nil {
		return nil, err
	}
	return d.Root["x"], nil
}

func TestMultilineBasicLineEndingBackslashJoinsLines(t *testing.T) {
	tests := map[string]string{
		"x = \"\"\"\\\n  ! <target> \\\n  <address>\"\"\"\n": "! <target> <address>",
		"x = \"\"\"abc\\\n\"\"\"\n":                          "abc",
		"x = \"\"\"abc\\\n   \"\"\"\n":                       "abc",
		"x = \"\"\"a \\   \n\n\t  b\"\"\"\n":                 "a b",
		"x = \"\"\"a\\\\\nb\"\"\"\n":                         "a\\\nb",
		"x = \"\"\"\none\ntwo\"\"\"\n":                       "one\ntwo",
	}
	for text, want := range tests {
		if got, err := stringValue(text); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", text, got, err, want)
		}
	}
}

func TestBasicStringsDecodeTOMLEscapes(t *testing.T) {
	const escapes = `\b\t\n\f\r\"\\é\U0001F600`
	const want = "\b\t\n\f\r\"\\é\U0001F600"
	for _, text := range []string{
		"x = \"" + escapes + "\"\n",
		"x = \"\"\"" + escapes + "\"\"\"\n",
	} {
		if got, err := stringValue(text); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", text, got, err, want)
		}
	}
	quotes := "x = \"\"\"say \"hi\" and \"\"twice\"\" now\"\"\"\n"
	if got, err := stringValue(quotes); err != nil || got != `say "hi" and ""twice"" now` {
		t.Errorf("%q = %q, %v", quotes, got, err)
	}
}

func TestBasicStringsRefuseEscapesTOMLDoesNotDefine(t *testing.T) {
	for _, escape := range []string{`\x41`, `\101`, `\a`, `\v`, `\'`, `\e`, `\ a`, `\uD800`, `\U00110000`} {
		for _, text := range []string{
			"x = \"a" + escape + "b\"\n",
			"x = \"\"\"a" + escape + "b\"\"\"\n",
		} {
			if d, err := Parse([]byte(text)); err == nil {
				t.Errorf("%q parsed as %q, want a syntax error", text, d.Root["x"])
			}
		}
	}
}

func TestIntegersAreTOMLIntegers(t *testing.T) {
	good := map[string]int64{
		"0": 0, "-0": 0, "+0": 0, "7": 7, "10": 10, "-17": -17, "+99": 99,
		"1_000": 1000, "5_349_221": 5349221,
		"0x10": 16, "0xDEAD_beef": 0xdeadbeef, "0x0010": 16,
		"0o755": 493, "0o0755": 493, "0b1010": 10, "0b1_0": 2,
	}
	for text, want := range good {
		got, err := ParseValue(text)
		if err != nil || got != want {
			t.Errorf("ParseValue(%q) = %v, %v; want %d", text, got, err, want)
		}
		d, err := Parse([]byte("x = " + text + "\n"))
		if err != nil || d.Root["x"] != want {
			t.Errorf("x = %s parsed as %v, %v; want %d", text, d.Root["x"], err, want)
		}
	}
	bad := []string{
		"010", "0755", "00", "0_0", "-010", "+010", "060", "0022",
		"_1", "1_", "1__2", "-_1", "0x_1", "0x1_", "0x1__0", "0x",
		"-0x10", "+0x10", "0X10", "0O17", "0B1", "0o8", "0b2", "0xg",
	}
	for _, text := range bad {
		if v, err := ParseValue(text); err == nil {
			t.Errorf("ParseValue(%q) = %v, want an error", text, v)
		}
		if d, err := Parse([]byte("x = " + text + "\n")); err == nil {
			t.Errorf("x = %s parsed as %v, want a syntax error", text, d.Root["x"])
		}
	}
}
