package helplayout

import "testing"

// TestRoffEscape: a hyphen is \-, a backslash \e, and a line beginning with
// a dot or an apostrophe is protected, so help text cannot make a request.
func TestRoffEscape(t *testing.T) {
	if got := RoffEscape(`--a-b \x`); got != `\-\-a\-b \ex` {
		t.Errorf("RoffEscape: %q", got)
	}
	for in, want := range map[string]string{".so /etc/x": `\&.so /etc/x` + "\n", "'br": `\&'br` + "\n", "plain": "plain\n"} {
		if got := RoffLine(in); got != want {
			t.Errorf("RoffLine(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestRoff: a text of every shape a help text has, as the page's SYNOPSIS
// and DESCRIPTION: the title line left to NAME; each invocation on a line,
// karvi and the action words bold, <command> italic, sudo plain, a line
// without karvi continuing the one above; a heading as a subsection (a
// two-line heading joined, its colon dropped); an entry's option words
// bold and values italic, a command entry bold, a placeholder entry
// italic, a long entry with its description under it; prose as a
// paragraph, a line beginning with a dot protected.
func TestRoff(t *testing.T) {
	text := `karvi - a title line

Usage:
  karvi thing [options]
  karvi [global-options] <command> [options]
  karvi thing [--one VALUE] [--two VALUE]
              [--three VALUE]
  sudo karvi thing sub [--group NAME]

Two-line heading (with a
parenthesis):
  DEVICE                         A placeholder entry
  --one VALUE, --o               A one-line entry
  --two                          A two-line entry, whose second line
                                 is here
  daemon start|stop|restart|status|serve
                                 A command entry too long for its column
  A paragraph of prose,
  .two lines long.

An unindented paragraph.
`
	syn, desc := Roff(text)
	wantSyn := `\fBkarvi\fR \fBthing\fR [options]
.br
\fBkarvi\fR [global\-options] \fI<command>\fR [options]
.br
\fBkarvi\fR \fBthing\fR [\-\-one VALUE] [\-\-two VALUE]
[\-\-three VALUE]
.br
sudo \fBkarvi\fR \fBthing sub\fR [\-\-group NAME]
`
	wantDesc := `.SS "Two\-line heading (with a parenthesis)"
.TP
\fIDEVICE\fR
A placeholder entry
.TP
\fB\-\-one\fR \fIVALUE\fR, \fB\-\-o\fR
A one\-line entry
.TP
\fB\-\-two\fR
A two\-line entry, whose second line
is here
.TP
\fBdaemon\fR \fBstart|stop|restart|status|serve\fR
A command entry too long for its column
.PP
A paragraph of prose,
\&.two lines long.
.PP
An unindented paragraph.
`
	if syn != wantSyn {
		t.Errorf("synopsis:\n%s\nwant:\n%s", syn, wantSyn)
	}
	if desc != wantDesc {
		t.Errorf("description:\n%s\nwant:\n%s", desc, wantDesc)
	}
}
