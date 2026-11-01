package tomlmini

import (
	"math"
	"testing"
)

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

func TestFloatsAreTOMLDecimalFloats(t *testing.T) {
	good := map[string]float64{
		"1.0": 1, "+1.0": 1, "-1.0": -1, "0.0": 0, "3.1415": 3.1415,
		"-0.01": -0.01, "0.5": 0.5, "5e+22": 5e22, "1e06": 1e6, "-2E-2": -0.02,
		"6.626e-34": 6.626e-34, "1E5": 1e5, "+1e5": 1e5, "0e0": 0, "0E+0": 0,
		"1e-0": 1, "1.0e05": 1e5, "1_0.5": 10.5, "1.0_5": 1.05,
		"224_617.445_991_228": 224617.445991228, "0.000_1": 0.0001,
		"1e1_0": 1e10, "1.0e5_0": 1e50, "1.7976931348623157e308": 1.7976931348623157e308,
		"4.9e-324": 4.9e-324,
	}
	for text, want := range good {
		got, err := ParseValue(text)
		if err != nil || got != want {
			t.Errorf("ParseValue(%q) = %v, %v; want %g", text, got, err, want)
		}
		d, err := Parse([]byte("x = " + text + "\n"))
		if err != nil || d.Root["x"] != want {
			t.Errorf("x = %s parsed as %v, %v; want %g", text, d.Root["x"], err, want)
		}
	}
	bad := []string{
		"010.5", "00.5", "-01.5", "+00.0", "0_1.5", "01e5", "00e5",
		"1__0.5", "1_.5", "_1.5", "1.5_", "1._5", "1.5__5", "1_e5",
		"1e_5", "1e5_", "1e1__0", "1.5e+_5",
		".5", "+.5", "-.5", "-.0", "5.", "0.", "1.e5", "0.e1",
		"1e", "1e+", "1.5E", "1e+-5", "e5", "1.5e5.5", "1e5.0", "1.0.0", "1E5E5",
		"++1.5", "+-1.5", "1.5f", "0x1.8p1", "0x1p-2",
		"inf", "+inf", "-inf", "nan", "+nan", "-nan", "Inf", "NaN", "infinity",
		"1e500", "-1e500", "1.8e308",
	}
	for _, text := range bad {
		if v, err := ParseValue(text); err == nil {
			t.Errorf("ParseValue(%q) = %v, want an error", text, v)
		}
		if d, err := Parse([]byte("x = " + text + "\n")); err == nil {
			t.Errorf("x = %s parsed as %v, want a syntax error", text, d.Root["x"])
		}
	}
	if v, err := ParseValue("-0.0"); err != nil || v != 0.0 || !math.Signbit(v.(float64)) {
		t.Errorf("ParseValue(%q) = %v, %v; want negative zero", "-0.0", v, err)
	}
	if v, err := ParseValue("0x1e5"); err != nil || v != int64(0x1e5) {
		t.Errorf("ParseValue(%q) = %v, %v; want the integer %d", "0x1e5", v, err, 0x1e5)
	}
}

func TestMultilineStringsKeepQuotesBeforeTheClosingDelimiter(t *testing.T) {
	good := map[string]any{
		`x = """x""""`:             `x"`,
		`x = """x"""""`:            `x""`,
		`x = """"x"""`:             `"x`,
		`x = """""x"""`:            `""x`,
		`x = """"""`:               ``,
		`x = """""""`:              `"`,
		`x = """"""""`:             `""`,
		"x = \"\"\"\nx\"\"\"\"":    `x"`,
		"x = \"\"\"a\n\"b\"\"\"\"": "a\n\"b\"",
		`x = """x\""""`:            `x"`,
		`x = """x\"""""`:           `x""`,
		`x = """x\\""""`:           `x\"`,
		`x = """x"""" # c`:         `x"`,
		`x = """"This," she said, "is just a pointless statement.""""`: `"This," she said, "is just a pointless statement."`,
		`x = '''x''''`:   `x'`,
		`x = '''x'''''`:  `x''`,
		`x = ''''x'''`:   `'x`,
		`x = '''''x'''`:  `''x`,
		`x = ''''''`:     ``,
		`x = '''''''`:    `'`,
		`x = ''''''''`:   `''`,
		"x = '''\nx''''": `x'`,
		`x = ''''That,' she said, 'is still pointless.''''`: `'That,' she said, 'is still pointless.'`,
	}
	for text, want := range good {
		if got, err := stringValue(text + "\n"); err != nil || got != want {
			t.Errorf("%q = %q, %v; want %q", text, got, err, want)
		}
	}
	d, err := Parse([]byte("x = [\"\"\"a\"\"\"\", '''b''''']\ny = {z = \"\"\"c\"\"\"\"\"}\n"))
	if err != nil {
		t.Error(err)
	} else {
		if got := d.Root["x"].([]any); len(got) != 2 || got[0] != `a"` || got[1] != `b''` {
			t.Errorf("x = %q, want [%q %q]", got, `a"`, `b''`)
		}
		if got := d.Root["y"].(map[string]any)["z"]; got != `c""` {
			t.Errorf("y.z = %q, want %q", got, `c""`)
		}
	}
	bad := []string{
		`x = """x""""""`, `x = """x"""""""`, `x = """""""""`, `x = """a"""" "`,
		`x = """Here are three quotation marks: """."""`,
		`x = '''x''''''`, `x = '''x'''''''`, `x = '''''''''`, `x = '''a'''' '`,
	}
	for _, text := range bad {
		if d, err := Parse([]byte(text + "\n")); err == nil {
			t.Errorf("%q parsed as %q, want a syntax error", text, d.Root["x"])
		}
	}
}
