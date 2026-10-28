package jobexec

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

func liveRenderer(t *testing.T, activity string, quiet bool, sets ...string) (*recordRenderer, *bytes.Buffer) {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", cfg, quiet, false, "activity-1", "/tmp/artifacts", activity, false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	r.live = true
	return r, &out
}

func liveRecord(device string) records.CommandRecord {
	return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, ActivityType: "command", Device: records.DeviceProjection{CanonicalName: device}, SelectedAddress: "192.0.2.1", Platform: "cisco_ios", Transport: "native",
		Credential: &records.CredentialProjection{DeviceUsername: "jsmith", Backend: "env"}, Timing: records.Timing{EndedAt: time.Now()}}
}

// liveCommand is a record of one command as the executor appends it after
// the command has run.
func liveCommand(device, command, status, output string, failure *records.StructuredError) records.CommandRecord {
	rec := liveRecord(device)
	rec.CommandKind, rec.CommandIndex, rec.CommandCount = "requested", 1, 1
	rec.Command, rec.Status, rec.Output, rec.OutputEncoding = command, status, output, "utf-8"
	rec.PromptBefore, rec.Prompt, rec.PromptSource = "Router#", "Router#", "observed"
	rec.Error = failure
	return rec
}

// TestLiveTakesTheTextDisplaysPlace: the header when the session opens, the
// session's bytes as written with a secret removed, no record's echo or
// output a second time, karvi's failure line on a line of its own, and the
// footer after the last line's end.
func TestLiveTakesTheTextDisplaysPlace(t *testing.T) {
	r, out := liveRenderer(t, "command", false)
	v := r.LiveDevice(liveRecord("router1"))
	if got := out.String(); got != "! router1 [192.0.2.1] platform=cisco_ios user=jsmith backend=env transport=native\n" {
		t.Fatalf("at the open %q", got)
	}
	v.Write([]byte("Router>"))
	v.Secret([]byte("Zq7-en"))
	v.Write([]byte("enable\r\nPassword: Zq"))
	v.Write([]byte("7-en\r\nRouter#show clock\r\n*10:00\r\nRouter#"))
	r.OnRecord(liveCommand("router1", "show clock", "success", "*10:00\n", nil))
	v.Write([]byte("show bogus\r\n% Invalid input\r\nRouter#"))
	r.OnRecord(liveCommand("router1", "show bogus", "device_error", "% Invalid input\n", &records.StructuredError{Code: "device_command_error", Message: "the device reported an error"}))
	v.Write([]byte("exit\r\n"))
	v.End()
	v.Write([]byte("after the end"))
	if err := r.WriteFooter(time.Now(), 6, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	want := "! router1 [192.0.2.1] platform=cisco_ios user=jsmith backend=env transport=native\n" +
		"Router>enable\r\nPassword: <removed>\r\nRouter#show clock\r\n*10:00\r\nRouter#show bogus\r\n% Invalid input\r\nRouter#\n" +
		"karvi: target=router1 status=device_error error=device_command_error: the device reported an error\n" +
		"exit\r\n! exit=6 elapsed=1s artifacts=/tmp/artifacts\n"
	if got := out.String(); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

// TestLiveEachDeviceOfARunInTurn: a run's devices each open with their own
// header, and a device whose session never opened has its header and its
// failure from the record, as the text display writes them.
func TestLiveEachDeviceOfARunInTurn(t *testing.T) {
	r, out := liveRenderer(t, "run", false)
	v := r.LiveDevice(liveRecord("router1"))
	v.Write([]byte("Router#show clock\r\n*10:00\r\nRouter#"))
	r.OnRecord(liveCommand("router1", "show clock", "success", "*10:00\n", nil))
	v.End()
	v = r.LiveDevice(liveRecord("router2"))
	v.Write([]byte("Router#show clock\r\n*10:01\r\nRouter#"))
	r.OnRecord(liveCommand("router2", "show clock", "success", "*10:01\n", nil))
	v.End()
	r.OnRecord(liveCommand("router3", "show clock", "connection_failed", "", &records.StructuredError{Code: "connection_refused", Message: "refused"}))
	want := "! router1 [192.0.2.1] platform=cisco_ios user=jsmith backend=env transport=native\nRouter#show clock\r\n*10:00\r\nRouter#\n" +
		"! router2 [192.0.2.1] platform=cisco_ios user=jsmith backend=env transport=native\nRouter#show clock\r\n*10:01\r\nRouter#\n" +
		"! router3 [192.0.2.1] platform=cisco_ios user=jsmith backend=env transport=native\nkarvi: target=router3 status=connection_failed error=connection_refused: refused\n"
	if got := out.String(); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
}

// TestLiveQuietAndStripped: --quiet drops the header and the footer but not
// the bytes or a failure line; output.ansi strip takes the escape
// sequences out of the bytes, one cut by a write's edge too.
func TestLiveQuietAndStripped(t *testing.T) {
	r, out := liveRenderer(t, "command", true, `output.ansi="strip"`)
	v := r.LiveDevice(liveRecord("router1"))
	v.Write([]byte("Router#show clock\r\n\x1b[1m*10"))
	v.Write([]byte(":00\x1b[0"))
	v.Write([]byte("m\r\nRouter#"))
	r.OnRecord(liveCommand("router1", "show clock", "timeout", "", &records.StructuredError{Code: "command_timeout", Message: "timed out"}))
	v.End()
	if err := r.WriteFooter(time.Now(), 7, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	want := "Router#show clock\r\n*10:00\r\nRouter#\nkarvi: target=router1 status=timeout error=command_timeout: timed out\n"
	if got := out.String(); got != want {
		t.Fatalf("\n got %q\nwant %q", got, want)
	}
	if strings.Contains(out.String(), "\x1b") {
		t.Fatalf("an escape sequence was written: %q", out.String())
	}
}
