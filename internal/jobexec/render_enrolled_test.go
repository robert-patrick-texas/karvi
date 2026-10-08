package jobexec

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestEnrolledLineOnStandardError: a followed record carrying
// host_key_enrolled puts "! ssh accepted new host key for DEVICE (TYPE)" on
// the client's standard error before the record, in each format and under
// --quiet, and nothing of it on standard output; a record without the
// notice puts nothing there.
func TestEnrolledLineOnStandardError(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never"}})
	if err != nil {
		t.Fatal(err)
	}
	notice := records.Notice{Code: "host_key_enrolled", Message: "ssh accepted new host-key r1 (ED25519)", Details: map[string]any{"key_type": "ED25519"}}
	first, err := json.Marshal(records.CommandRecord{Device: records.DeviceProjection{CanonicalName: "r1"}, Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Notices: []records.Notice{notice}})
	if err != nil {
		t.Fatal(err)
	}
	second := []byte(`{"status":"succeeded","output":"two\n","output_encoding":"utf-8"}`)
	for _, format := range []string{"text", "json", "jsonl"} {
		for _, quiet := range []bool{false, true} {
			var out, errOut bytes.Buffer
			rr, err := NewRunRenderer(cfg, quiet, false, "/tmp/artifacts", format, false, false, false, &out, &errOut)
			if err != nil {
				t.Fatal(err)
			}
			for _, line := range [][]byte{first, second} {
				if err := rr.Line(append(append([]byte(nil), line...), '\n')); err != nil {
					t.Fatal(err)
				}
			}
			if got := errOut.String(); got != "! ssh accepted new host-key r1 (ED25519)\n" {
				t.Errorf("%s quiet=%t: standard error %q", format, quiet, got)
			}
			if strings.Contains(out.String(), "! ssh accepted") {
				t.Errorf("%s quiet=%t: the line on standard output %q", format, quiet, out.String())
			}
		}
	}
}

// TestEnrolledLineNotFromAPastJob: a job's directory shown again (no
// standard error given) says nothing of an enrollment; the record keeps
// the notice.
func TestEnrolledLineNotFromAPastJob(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRunReplayRenderer(&out, "text", cfg, false, false, "/tmp/artifacts", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	r.OnRecord(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Notices: []records.Notice{{Code: "host_key_enrolled", Message: "ssh accepted new host-key r1 (ED25519)", Details: map[string]any{"key_type": "ED25519"}}}})
	if strings.Contains(out.String(), "ssh accepted") {
		t.Fatalf("a past job's display %q", out.String())
	}
}

// TestHostKeyLinesColours: with colour on, the device's name takes the
// target colour, the rest of the line the warning colour, and the mismatch
// line the error colour.
func TestHostKeyLinesColours(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always"}})
	if err != nil {
		t.Fatal(err)
	}
	style := DisplayLineStyle(cfg, false)
	notices := []records.Notice{
		{Code: "host_key_enrolled", Details: map[string]any{"key_type": "ED25519"}},
		{Code: "host_key_mismatch_accepted"},
	}
	lines := HostKeyLines("r1", notices, style)
	target := display.ANSIStyle("r1", style.Target, true, true)
	want := []string{
		display.ANSIStyle("! ssh accepted new host-key ", style.Warning, true, true) + target + display.ANSIStyle(" (ED25519)", style.Warning, true, true),
		display.ANSIStyle("! ssh host-key mismatch ", style.Error, true, true) + target + display.ANSIStyle(" proceeding at risk", style.Error, true, true),
	}
	if strings.Join(lines, "|") != strings.Join(want, "|") || style.Target == style.Warning || style.Error == style.Warning {
		t.Fatalf("lines %q", lines)
	}
}

// TestHostKeyLinesOfEachCode: the three host-key notices each make a line,
// any other notice none, and a followed jsonl record naming one is said
// with its device's name.
func TestHostKeyLinesOfEachCode(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never"}})
	if err != nil {
		t.Fatal(err)
	}
	notices := []records.Notice{
		{Code: "host_key_mismatch_accepted", Message: "ssh host-key mismatch r1 proceeding at risk"},
		{Code: "platform_not_set", Message: "no platform"},
		{Code: "host_key_not_compared", Message: "ssh host-key r1 not compared: ssh-keyscan timed out", Details: map[string]any{"cause": "ssh-keyscan timed out", "reason": "ssh-keyscan timed out: no key within 5s"}},
	}
	want := "! ssh host-key mismatch r1 proceeding at risk\n! ssh host-key r1 not compared: ssh-keyscan timed out"
	if got := strings.Join(HostKeyLines("r1", notices, DisplayLineStyle(cfg, false)), "\n"); got != want {
		t.Fatalf("lines %q", got)
	}
	line, err := json.Marshal(records.CommandRecord{Device: records.DeviceProjection{CanonicalName: "r1"}, Status: "succeeded", Notices: notices})
	if err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	rr, err := NewRunRenderer(cfg, true, false, "/tmp/artifacts", "jsonl", false, false, false, &out, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	if err := rr.Line(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	if errOut.String() != want+"\n" {
		t.Fatalf("followed jsonl: %q", errOut.String())
	}
}

// TestAdmissionWarningLines: the insecure policy's warning is two lines, the
// second's two spaces marking a continuation, each under 80 columns and in
// the warning colour when colour is on; any other admission warning is
// "warning: " and the warning.
func TestAdmissionWarningLines(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	WriteAdmissionWarnings(&out, cfg, []string{PolicyInsecureWarning(), "spool_width_narrowed: /tmp has 1 bytes free"})
	want := "! ssh host-key policy insecure: unknown and changed keys accepted;\n!  connecting to devices with wrong keys and MITM attacks allowed\nwarning: spool_width_narrowed: /tmp has 1 bytes free\n"
	if out.String() != want {
		t.Fatalf("lines %q", out.String())
	}
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if len(l) > 80 {
			t.Fatalf("%d columns: %q", len(l), l)
		}
	}
	coloured, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range AdmissionWarningLines(PolicyInsecureWarning(), DisplayLineStyle(coloured, false)) {
		if !strings.HasPrefix(l, "\x1b[") || !strings.HasSuffix(l, "\x1b[0m") {
			t.Fatalf("uncoloured %q", l)
		}
	}
}
