package configload

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestIncludeScanLineEndingBackslashEndsAtTheLine: a backslash ending a line
// inside a multi-line basic string joins it to the next; it escapes nothing
// on the next line, so a closing """ there closes the string and a directive
// after it is read.
func TestIncludeScanLineEndingBackslashEndsAtTheLine(t *testing.T) {
	text := "x = \"\"\"abc\\\n\"\"\"\n@include \"other.toml\"\n"
	scanned, err := scanIncludes([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(scanned.Directives) != 1 || scanned.Directives[0].Target != "other.toml" || scanned.Directives[0].Line != 3 {
		t.Fatalf("directives = %+v, want other.toml at line 3", scanned.Directives)
	}
	inside := "x = \"\"\"abc\\\n@include \"other.toml\"\n\"\"\"\n"
	scanned, err = scanIncludes([]byte(inside))
	if err != nil {
		t.Fatal(err)
	}
	if len(scanned.Directives) != 0 {
		t.Fatalf("directives inside a string = %+v, want none", scanned.Directives)
	}
}

// TestConfigTOMLStringsAndIntegers: a configuration file's multi-line basic
// string joins a line ending in a backslash to the next, and a file using an
// escape TOML does not define or an integer with a leading zero is
// config_toml_syntax.
func TestConfigTOMLStringsAndIntegers(t *testing.T) {
	body := "[display.command]\nheader = \"\"\"! <target> \\\n    [<address>]\"\"\"\n"
	snap, err := loadAs(t, "jsmith", body)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("display.command.header"); got != "! <target> [<address>]" {
		t.Errorf("header = %q", got)
	}
	for _, body := range []string{
		"[display.command]\nheader = \"\"\"! <target>\\x21\"\"\"\n",
		"[display.command]\nheader = \"! <target>\\a\"\n",
		"[dispatch]\nparallel-workers = 010\n",
		"[dispatch]\nparallel-workers = 1__0\n",
	} {
		_, err := loadAs(t, "jsmith", body)
		if got := errorcodes.Of(err); got != "config_toml_syntax" {
			t.Errorf("%q: code %q, want config_toml_syntax (%v)", body, got, err)
		}
	}
}

// TestIncludeScanReadsQuotesBeforeAClosingDelimiter: one or two quotes just
// before a multi-line string's closing delimiter belong to the string, so
// the string ends there and a directive on the next line is read.
func TestIncludeScanReadsQuotesBeforeAClosingDelimiter(t *testing.T) {
	for _, text := range []string{
		"x = \"\"\"a\"\"\"\"\n@include \"other.toml\"\n",
		"x = \"\"\"a\"\"\"\"\"\n@include \"other.toml\"\n",
		"x = '''a''''\n@include \"other.toml\"\n",
		"x = '''a'''''\n@include \"other.toml\"\n",
	} {
		scanned, err := scanIncludes([]byte(text))
		if err != nil {
			t.Fatal(err)
		}
		if len(scanned.Directives) != 1 || scanned.Directives[0].Target != "other.toml" || scanned.Directives[0].Line != 2 {
			t.Errorf("%q: directives = %+v, want other.toml at line 2", text, scanned.Directives)
		}
	}
}

// TestConfigTOMLFloatsAndClosingQuotes: a configuration file's number is a
// TOML integer or decimal float, any other form (inf and nan among them, and
// a float beyond float64) config_toml_syntax; quotes just before a closing
// """ belong to the string.
func TestConfigTOMLFloatsAndClosingQuotes(t *testing.T) {
	snap, err := loadAs(t, "jsmith", "[output]\nreserve-multiplier = 1_2.5e-1\n[display.command]\nheader = \"\"\"! \"<target>\"\"\"\"\n")
	if err != nil {
		t.Error(err)
	} else {
		if got := snap.Float("output.reserve-multiplier"); got != 1.25 {
			t.Errorf("reserve-multiplier = %v, want 1.25", got)
		}
		if got := snap.String("display.command.header"); got != `! "<target>"` {
			t.Errorf("header = %q, want %q", got, `! "<target>"`)
		}
	}
	for _, value := range []string{"01.5", "1._5", "1.5_", ".5e1", "2.", "inf", "+inf", "nan", "-nan", "1e500"} {
		body := "[output]\nreserve-multiplier = " + value + "\n"
		_, err := loadAs(t, "jsmith", body)
		if got := errorcodes.Of(err); got != "config_toml_syntax" {
			t.Errorf("%q: code %q, want config_toml_syntax (%v)", body, got, err)
		}
	}
}
