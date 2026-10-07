package jobexec

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
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
	notice := records.Notice{Code: "host_key_enrolled", Message: "ssh accepted new host key for r1 (ED25519)", Details: map[string]any{"key_type": "ED25519"}}
	first, err := json.Marshal(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Notices: []records.Notice{notice}})
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
			if got := errOut.String(); got != "! ssh accepted new host key for r1 (ED25519)\n" {
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
	r.OnRecord(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Notices: []records.Notice{{Code: "host_key_enrolled", Message: "ssh accepted new host key for r1 (ED25519)"}}})
	if strings.Contains(out.String(), "ssh accepted") {
		t.Fatalf("a past job's display %q", out.String())
	}
}

// TestEnrolledLineColour: the line takes the display's warning colour when
// colour is on.
func TestEnrolledLineColour(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always"}})
	if err != nil {
		t.Fatal(err)
	}
	style := DisplayLineStyle(cfg, false)
	got := EnrolledLine("ssh accepted new host key for r1 (ED25519)", style)
	if !strings.HasPrefix(got, "\x1b[") || !strings.Contains(got, "! ssh accepted new host key for r1 (ED25519)") || !strings.HasSuffix(got, "\x1b[0m") {
		t.Fatalf("coloured line %q", got)
	}
}
