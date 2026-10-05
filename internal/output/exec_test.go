package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// execRecord is an exec record carrying stdout and stderr as strings, and
// the same record with each stream that spool names left empty, as the
// executor hands a spooled stream to the store, with its source.
func execRecord(t *testing.T, stdout, stderr []byte, spoolOut, spoolErr bool) (whole, spooled records.CommandRecord, src Source) {
	t.Helper()
	out, outEnc, outDigest := EncodeOutput(stdout)
	errText, errEnc, errDigest := EncodeOutput(stderr)
	status, size := 2, int64(len(stderr))
	whole = records.CommandRecord{SchemaVersion: 3, RecordID: "r", Channel: records.ChannelExec, Command: "ls /nonexistent", Output: out, OutputEncoding: outEnc, OutputBytes: int64(len(stdout)), OutputSHA256: outDigest, ExitStatus: &status, Stderr: &errText, StderrEncoding: &errEnc, StderrBytes: &size, StderrSHA256: &errDigest, PromptSource: records.PromptSourceNone}
	spooled = whole
	src = FromRecord(&whole)
	if spoolOut {
		spooled.Output = ""
		src, _ = spoolOf(t, stdout)
	}
	errSrc := FromEncoded(errText, errEnc)
	if spoolErr {
		empty := ""
		spooled.Stderr = &empty
		errSrc, _ = spoolOf(t, stderr)
	}
	return whole, spooled, src.WithStderr(errSrc)
}

// TestExecLineEqualsTheMarshalledRecord: an exec record's line, its output
// and its stderr each in a spool, a large string, or a small one, in every
// combination, equals json.Marshal of the record carrying both as strings;
// the measuring pass verifies each spool, and so does the indented form.
func TestExecLineEqualsTheMarshalledRecord(t *testing.T) {
	big := []byte(strings.Repeat("line \"quoted\" \\ é€😀 <html>\n", 4000))
	binary := append(append([]byte{}, big...), 0xff, '\n')
	small := []byte("ls: cannot access '/nonexistent'\n")
	cases := []struct {
		name               string
		stdout, stderr     []byte
		spoolOut, spoolErr bool
	}{
		{"both spooled", big, binary, true, true},
		{"stdout spooled", big, small, true, false},
		{"stderr spooled", small, big, false, true},
		{"both large strings", big, big, false, false},
		{"stderr a large string", small, big, false, false},
		{"both small", small, small, false, false},
		{"no stderr", big, nil, true, false},
	}
	for _, c := range cases {
		whole, spooled, src := execRecord(t, c.stdout, c.stderr, c.spoolOut, c.spoolErr)
		want, err := json.Marshal(&whole)
		if err != nil {
			t.Fatal(err)
		}
		line, err := newRecordLine(&spooled, src)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		n, err := line.measure()
		if err != nil {
			t.Fatalf("%s: measure: %v", c.name, err)
		}
		var got bytes.Buffer
		if _, err := line.WriteTo(&got); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), append(want, '\n')) || n != int64(got.Len()) {
			t.Fatalf("%s: the line differs from the marshalled record (%d and %d bytes, measured %d)", c.name, got.Len(), len(want)+1, n)
		}
		wantIndent, _ := json.MarshalIndent(&whole, "  ", "  ")
		got.Reset()
		if err := WriteRecordJSONIndent(&got, &spooled, src, "  ", "  "); err != nil || !bytes.Equal(got.Bytes(), append([]byte("  "), wantIndent...)) {
			t.Fatalf("%s: the indented form differs from MarshalIndent: %v", c.name, err)
		}
	}
}

// TestExecLineRefusesAStderrSpoolThatChanged: the measuring pass checks the
// stderr's spool against the reader's digest as it checks the output's.
func TestExecLineRefusesAStderrSpoolThatChanged(t *testing.T) {
	big := []byte(strings.Repeat("e", streamedLineThreshold))
	_, spooled, src := execRecord(t, []byte("ok\n"), big, false, true)
	stderr, _ := src.Stderr()
	stderr.digest = strings.Repeat("0", 64)
	line, err := newRecordLine(&spooled, src.WithStderr(stderr))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := line.measure(); errorcodes.Of(err) != "output_spool_mismatch" {
		t.Fatalf("measure: %v, want output_spool_mismatch", err)
	}
}

// TestTextBlockOfAnExecCommand: the inferred prompt and the command, stdout,
// stderr, the `!` line; one that did not run is `! not sent:`.
func TestTextBlockOfAnExecCommand(t *testing.T) {
	_, spooled, src := execRecord(t, []byte("partial"), []byte("ls: cannot access '/nonexistent'\n"), false, true)
	spooled.Device.CanonicalName = "srv1"
	spooled.Status = "device_error"
	spooled.Error = &records.StructuredError{Code: "command_exit_nonzero", Message: "exited 2"}
	var got bytes.Buffer
	if err := WriteTextBlock(&got, &spooled, src); err != nil {
		t.Fatal(err)
	}
	want := "srv1$ ls /nonexistent\npartial\nls: cannot access '/nonexistent'\n! command_exit_nonzero: exited 2\n"
	if got.String() != want {
		t.Fatalf("block %q, want %q", got.String(), want)
	}
	notRun := records.CommandRecord{Channel: records.ChannelExec, Device: records.DeviceProjection{CanonicalName: "srv1"}, Command: "uname -s", Status: "not_attempted_prior_command_failure", OutputEncoding: "utf-8"}
	got.Reset()
	if err := WriteTextBlock(&got, &notRun, FromRecord(&notRun)); err != nil {
		t.Fatal(err)
	}
	if want := "! not sent: uname -s\n! not_attempted_prior_command_failure\n"; got.String() != want {
		t.Fatalf("block %q, want %q", got.String(), want)
	}
}
