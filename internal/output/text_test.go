package output

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestTextFileName covers the file name of a device: a device name is
// its first label, lowercased, under the crop, and the whole name lowercased
// without; an address is whole with hyphens for its dots and colons under
// both; a separator is a hyphen.
func TestTextFileName(t *testing.T) {
	for target, want := range map[string]string{
		"r1":                      "output.r1.txt",
		"core-sw.site":            "output.core-sw.txt",
		"Core-NYC-01.Example.NET": "output.core-nyc-01.txt",
		"192.0.2.10":              "output.192-0-2-10.txt",
		"2001:db8::10":            "output.2001-db8--10.txt",
		"a/b":                     "output.a-b.txt",
		".hidden":                 "output..hidden.txt",
	} {
		if got := TextFileName(target, true); got != want {
			t.Errorf("TextFileName(%q, crop) = %q, want %q", target, got, want)
		}
	}
	for target, want := range map[string]string{
		"core-sw.site":            "output.core-sw.site.txt",
		"Core-NYC-01.Example.NET": "output.core-nyc-01.example.net.txt",
		"192.0.2.10":              "output.192-0-2-10.txt",
		"2001:db8::10":            "output.2001-db8--10.txt",
	} {
		if got := TextFileName(target, false); got != want {
			t.Errorf("TextFileName(%q, whole) = %q, want %q", target, got, want)
		}
	}
	if got := FileName("core-nyc-01.example.net", true); got != "core-nyc-01" {
		t.Errorf("the collection's name: %q", got)
	}
}

// A session with each kind of block: a mode change, a statement
// with no answer, a rejected statement, a timeout after partial output, and
// a statement not sent.
func TestTextFileOfASession(t *testing.T) {
	at := time.Date(2026, 9, 21, 10, 0, 0, 0, time.FixedZone("", -4*3600))
	rec := func(before, command, status, output string, e *records.StructuredError) *records.CommandRecord {
		r := &records.CommandRecord{PromptBefore: before, Command: command, Status: status, Output: output, OutputEncoding: "utf-8", Error: e, SelectedAddress: "192.0.2.1"}
		r.Device.CanonicalName = "r1"
		r.Timing.DeviceStartedAt = &at
		return r
	}
	session := []*records.CommandRecord{
		rec("r1#", "show clock", "succeeded", "*10:00:00.000 UTC Tue Sep 15 2026\n", nil),
		rec("r1#", "configure terminal", "succeeded", "Enter configuration commands, one per line.  End with CNTL/Z.\n", nil),
		rec("r1(config)#", "end", "succeeded", "", nil),
		rec("r1#", "show bogus", "device_error", "     ^\n% Invalid input detected at '^' marker.\n", &records.StructuredError{Code: "device_command_error", Message: "device reported a command error"}),
		rec("r1#", "show slow", "timeout", "partial", &records.StructuredError{Code: "command_timeout", Message: "command timed out after 2s\nwhile waiting"}),
		rec("", "show version", "not_attempted_prior_command_failure", "", nil),
	}
	// The header's time is display.timestamp in the effective zone, here the
	// default pattern in the zone the record's offset belongs to.
	formatter, err := display.NewFormatter(display.DefaultTimestampPattern, "America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := WriteTextHeader(&b, session[0], formatter.Timestamp); err != nil {
		t.Fatal(err)
	}
	for _, r := range session {
		if err := WriteTextBlock(&b, r, FromRecord(r)); err != nil {
			t.Fatal(err)
		}
	}
	want := `! ### r1 (192.0.2.1) 10:00:00 2026-09-21 ###
r1#show clock
*10:00:00.000 UTC Tue Sep 15 2026
r1#configure terminal
Enter configuration commands, one per line.  End with CNTL/Z.
r1(config)#end
r1#show bogus
     ^
% Invalid input detected at '^' marker.
! device_command_error: device reported a command error
r1#show slow
partial
! command_timeout: command timed out after 2s while waiting
! not sent: show version
! not_attempted_prior_command_failure
`
	if b.String() != want {
		t.Fatalf("text file:\n%s\nwant:\n%s", b.String(), want)
	}
}

// The set-up lines: the same shape as a
// block, a statement with no answer one line, an answer without a line
// break given one; no lines, nothing written.
func TestTextSetup(t *testing.T) {
	var b bytes.Buffer
	if err := WriteTextSetup(&b, nil); err != nil || b.Len() != 0 {
		t.Fatalf("no lines wrote %q (%v)", b.String(), err)
	}
	err := WriteTextSetup(&b, []platform.SetupLine{
		{PromptBefore: "r1>", Statement: "enable", Output: "Password:"},
		{PromptBefore: "r1#", Statement: "terminal length 0"},
		{PromptBefore: "r1#", Statement: "terminal width 512", Output: "     ^\n% Invalid input detected at '^' marker.\n"},
	})
	want := "r1>enable\nPassword:\nr1#terminal length 0\nr1#terminal width 512\n     ^\n% Invalid input detected at '^' marker.\n"
	if err != nil || b.String() != want {
		t.Fatalf("set-up text %q (%v), want %q", b.String(), err, want)
	}
}

// A device that was never reached has no address in its header and no
// prompt on any line.
func TestTextFileOfADeviceNotReached(t *testing.T) {
	r := &records.CommandRecord{Command: "show clock", Status: "connection_error", Error: &records.StructuredError{Code: "ssh_connect_failed", Message: "connection refused"}}
	r.Device.CanonicalName = "r9"
	var b bytes.Buffer
	if err := WriteTextHeader(&b, r, DefaultTimestamp()); err != nil {
		t.Fatal(err)
	}
	if err := WriteTextBlock(&b, r, FromRecord(r)); err != nil {
		t.Fatal(err)
	}
	if want := "! ### r9 ###\n! not sent: show clock\n! ssh_connect_failed: connection refused\n"; b.String() != want {
		t.Fatalf("got %q, want %q", b.String(), want)
	}
}

// A response that is not valid UTF-8 is base64 in the record and the
// device's own bytes in the text file.
func TestTextBlockDecodesABase64Output(t *testing.T) {
	raw := []byte("caf\xe9 \xff\xfe\nline two")
	text, encoding, _ := EncodeOutput(raw)
	if encoding != "base64" {
		t.Fatalf("encoding %q", encoding)
	}
	var b bytes.Buffer
	if err := WriteTextBlock(&b, &records.CommandRecord{PromptBefore: "r1#", Command: "show odd", Status: "succeeded", Output: text, OutputEncoding: encoding}, Source{text: text, encoding: encoding}); err != nil {
		t.Fatal(err)
	}
	if want := "r1#show odd\n" + string(raw) + "\n"; b.String() != want {
		t.Fatalf("got %q, want %q", b.String(), want)
	}
}

// stringWriter counts what reaches it as a string and what as bytes.
type stringWriter struct{ asString, asBytes int }

func (s *stringWriter) Write(p []byte) (int, error)       { s.asBytes += len(p); return len(p), nil }
func (s *stringWriter) WriteString(v string) (int, error) { s.asString += len(v); return len(v), nil }

// A large output reaches a writer that takes strings as the record's own
// string: the text file makes no copy of a response.
func TestTextBlockWritesALargeOutputWithoutACopy(t *testing.T) {
	big := strings.Repeat("x", 5<<20) + "\n"
	r := &records.CommandRecord{PromptBefore: "r1#", Command: "show big", Status: "succeeded", Output: big, OutputEncoding: "utf-8"}
	var w stringWriter
	allocs := testing.AllocsPerRun(5, func() {
		if err := WriteTextBlock(&w, r, FromRecord(r)); err != nil {
			t.Fatal(err)
		}
	})
	if w.asBytes != 0 {
		t.Fatalf("%d bytes reached the writer as a byte slice", w.asBytes)
	}
	// The prompt line is the one small allocation; a copy of the output
	// would be one of megabytes, which the byte count above rules out.
	if allocs > 4 {
		t.Fatalf("%v allocations for one block", allocs)
	}
}
