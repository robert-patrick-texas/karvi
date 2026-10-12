package credhelper

import (
	"bytes"
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// shells are the shells a line karvi accepts must mean the same to, those
// present on the host; sh is dash on Debian.
func shells(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, sh := range []string{"dash", "bash", "sh"} {
		if p, err := exec.LookPath(sh); err == nil {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		t.Skip("no shell")
	}
	return out
}

// evaluated is what shell gives each name after eval of output, in an
// empty environment.
func evaluated(t *testing.T, shell, output string, names []string) map[string]string {
	t.Helper()
	script := `eval "$1" || exit 9; shift; for n do eval "v=\${$n-UNSET}"; printf '%s\0' "$v"; done`
	cmd := exec.Command(shell, append([]string{"-c", script, "sh", output}, names...)...)
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s: eval %q: %v", shell, output, err)
	}
	got := map[string]string{}
	parts := strings.Split(string(out), "\x00")
	for i, n := range names {
		got[n] = parts[i]
	}
	return got
}

func values(set []assignment) map[string]string {
	m := map[string]string{}
	for _, a := range set {
		m[a.name] = string(a.value)
	}
	return m
}

// TestParseAccepted is issue 10's accepted forms, each read as dash and
// bash read it under eval.
func TestParseAccepted(t *testing.T) {
	for _, tc := range []struct {
		output string
		want   map[string]string
	}{
		{"NETPASS=S3cret\n", map[string]string{"NETPASS": "S3cret"}},
		{"export NETPASS=S3cret", map[string]string{"NETPASS": "S3cret"}},
		{"export NETUSER=netops NETPASS=x\n", map[string]string{"NETUSER": "netops", "NETPASS": "x"}},
		{"  export\tA=1 \t B=2  \n", map[string]string{"A": "1", "B": "2"}},
		{"A=a.b,c:d/e@f%g+h=i-j_k\n", map[string]string{"A": "a.b,c:d/e@f%g+h=i-j_k"}},
		{`A="p a;s|s&w#o'r!d"`, map[string]string{"A": "p a;s|s&w#o'r!d"}},
		{`A="q\"b\\s\$d\` + "`" + `e"`, map[string]string{"A": `q"b\s$d` + "`" + `e`}},
		{`A="p\w\n"`, map[string]string{"A": `p\w\n`}},
		{`A='pa$word "x" \ ` + "`" + `'`, map[string]string{"A": `pa$word "x" \ ` + "`"}},
		{"A=\nB=\"\"\nC=''\n", map[string]string{"A": "", "B": "", "C": ""}},
		{"A= B=x\n", map[string]string{"A": "", "B": "x"}},
		{"A=x # the user\n", map[string]string{"A": "x"}},
		{"A=\"x\" #c\n", map[string]string{"A": "x"}},
		{"# a comment\n\n   \n\t# indented\nA=1\n", map[string]string{"A": "1"}},
		{"export=1\n", map[string]string{"export": "1"}},
		{"exportA=1\n", map[string]string{"exportA": "1"}},
		{"A=\"p\xc3\xa4ss\"\n", map[string]string{"A": "p\xc3\xa4ss"}},
		{"A='\xff'", map[string]string{"A": "\xff"}},
		{"_x9=1\n", map[string]string{"_x9": "1"}},
		{"", map[string]string{}},
		{"\n\n", map[string]string{}},
	} {
		set, err := parse([]byte(tc.output))
		if err != nil {
			t.Errorf("%q: %v", tc.output, err)
			continue
		}
		got := values(set)
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q: %q, want %q", tc.output, got, tc.want)
		}
		var names []string
		for n := range tc.want {
			names = append(names, n)
		}
		for _, sh := range shells(t) {
			if shell := evaluated(t, sh, tc.output, names); !reflect.DeepEqual(shell, tc.want) {
				t.Errorf("%q: %s gives %q, karvi %q", tc.output, sh, shell, tc.want)
			}
		}
	}
}

// TestParseRefused is issue 10's refusals: the line and the reason, and
// nothing of the line in the message.
func TestParseRefused(t *testing.T) {
	for _, tc := range []struct {
		output, want string
	}{
		{"NETPASS = s3cretX\n", "line 1: a blank before ="},
		{"NETPASS =s3cretX\n", "line 1: a blank before ="},
		{"A=1\nNETPASS= s3cretX\n", "line 2: a blank after ="},
		{"NETPASS=s3cretX echo x\n", "line 1: a word that is no assignment"},
		{"export NETPASS=s3cretX more\n", "line 1: a word that is no assignment"},
		{"NETPASS\n", "line 1: a word that is no assignment"},
		{"1A=s3cretX\n", "line 1: a word that is no assignment"},
		{"A-B=s3cretX\n", "line 1: a word that is no assignment"},
		{"=s3cretX\n", "line 1: a word that is no assignment"},
		{"export -n A=s3cretX\n", "line 1: a word that is no assignment"},
		{"export\n", "line 1: export without an assignment"},
		{"export # s3cretX\n", "line 1: export without an assignment"},
		{"A=s3cretX;\n", "line 1: an unquoted special character"},
		{"A=~/s3cretX\n", "line 1: an unquoted special character"},
		{"A=s3cretX#x\n", "line 1: an unquoted special character"},
		{"A=$s3cretX\n", "line 1: an unquoted special character"},
		{"A=s3\"cretX\"\n", "line 1: an unquoted special character"},
		{"A=p\xc3\xa4ssX\n", "line 1: an unquoted special character"},
		{`A="s3cretX$HOME"`, "line 1: a bare $ or ` in double quotes"},
		{"A=\"s3cretX`id`\"", "line 1: a bare $ or ` in double quotes"},
		{"A=\"s3cretX", "line 1: an unterminated double quote"},
		{"A=\"s3cretX\\", "line 1: an unterminated double quote"},
		{"A='s3cretX", "line 1: an unterminated single quote"},
		{"A=\"s3cretX\"y", "line 1: text after a closing quote"},
		{"A='s3cretX'#c", "line 1: text after a closing quote"},
		{"A=s3cretX\r\n", "line 1: a control character"},
		{"A=\"s3cretX\"\r\n", "line 1: a control character"},
		{"A=\"s3\tcretX\"", "line 1: a control character"},
		{"A='s3\x00cretX'", "line 1: a control character"},
		{"A=s3cretX \r", "line 1: a control character"},
		{"A=\"s3\x7fcretX\"", "line 1: a control character"},
		{"A=1\nB=2\nA=s3cretX\n", "A set on lines 1 and 3"},
		{"A=1 A=s3cretX\n", "line 1: A set twice"},
		{"A=1\n" + strings.Repeat("x", maxLine+1) + "\n", "line 2: line past 4 KiB"},
	} {
		set, err := parse([]byte(tc.output))
		if err == nil {
			t.Errorf("%q: accepted %q", tc.output, values(set))
			continue
		}
		if err.Error() != tc.want {
			t.Errorf("%q: %q, want %q", tc.output, err, tc.want)
		}
		if strings.Contains(err.Error(), "s3cretX") {
			t.Errorf("%q: the message quotes the line: %v", tc.output, err)
		}
	}
}

// TestParseBounds: an output of 64 KiB is read, one byte more refused; a
// line of 4 KiB is read.
func TestParseBounds(t *testing.T) {
	line := "A=" + strings.Repeat("x", maxLine-2)
	if _, err := parse([]byte(line)); err != nil {
		t.Errorf("a 4 KiB line: %v", err)
	}
	var b bytes.Buffer
	for i := 0; b.Len() < maxOutput; i++ {
		b.WriteString("# filler\n")
	}
	full := b.Bytes()[:maxOutput]
	full[maxOutput-1] = '\n'
	if _, err := parse(full); err != nil {
		t.Errorf("64 KiB: %v", err)
	}
	if _, err := parse(append(full, '\n')); err == nil || err.Error() != "output past 64 KiB" {
		t.Errorf("64 KiB and a byte: %v", err)
	}
}

// TestExampleQuoting is issue 14's test: the example helper's quoting
// function, run by each shell over hard values, prints a line karvi reads
// as the value, and so does each shell's eval of that line.
func TestExampleQuoting(t *testing.T) {
	src, err := os.ReadFile("../../../examples/karvi-credh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "\ncredh_quote() {\n")
	end := strings.Index(text[start+1:], "\n}\n")
	if start < 0 || end < 0 {
		t.Fatal("examples/karvi-credh has no credh_quote function")
	}
	function := text[start+1 : start+1+end+3]
	hard := []string{
		"p a s s", `"double"`, `'single'`, "$HOME", "${HOME}", `\back\slash\`, "`id`", "$(id)",
		"#hash", ";semi", "a&b|c", "*", "~/x", "!bang", `p"a'$` + "`" + `\#;`, "", "pässwörd",
		`trailing\`, " lead and trail ", `\"`, `\\$`, "a=b", "--opt",
	}
	for _, sh := range shells(t) {
		for _, v := range hard {
			out, err := exec.Command(sh, "-c", function+`printf 'export V=%s\n' "$(credh_quote "$1")"`, "sh", v).Output()
			if err != nil {
				t.Fatalf("%s: credh_quote %q: %v", sh, v, err)
			}
			set, err := parse(out)
			if err != nil {
				t.Errorf("%s: %q quoted as %q: %v", sh, v, out, err)
				continue
			}
			if got := values(set)["V"]; got != v {
				t.Errorf("%s: %q quoted as %q reads %q", sh, v, out, got)
			}
			for _, other := range shells(t) {
				if got := evaluated(t, other, string(out), []string{"V"})["V"]; got != v {
					t.Errorf("%s's %q: %s's eval gives %q", sh, out, other, got)
				}
			}
		}
	}
}
