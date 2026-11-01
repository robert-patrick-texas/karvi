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
