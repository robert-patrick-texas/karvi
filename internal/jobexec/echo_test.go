package jobexec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

func TestRecordRendererEchoesObservedPromptAndCommand(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, true, false, "activity-1", "/tmp/artifacts", "command", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	observed := true
	now := time.Now()
	renderer.OnRecord(records.CommandRecord{
		SchemaVersion:  records.CommandSchemaVersion,
		ActivityType:   "command",
		Command:        "show clock",
		Status:         "succeeded",
		Output:         "01:20:19.849 EDT Thu Sep 10 2026\n",
		OutputEncoding: "utf-8",
		Prompt:         "router1#",
		PromptSource:   "observed",
		PromptObserved: &observed,
		Timing:         records.Timing{EndedAt: now},
	})
	got := out.String()
	if !strings.HasPrefix(got, "router1#show clock\n") {
		t.Fatalf("echo output did not start with prompt and command: %q", got)
	}
	if !strings.Contains(got, "01:20:19.849 EDT Thu Sep 10 2026") {
		t.Fatalf("device output missing: %q", got)
	}
}

func TestRecordRendererDoesNotEchoByDefault(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, true, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	observed := true
	renderer.OnRecord(records.CommandRecord{Command: "show clock", Status: "succeeded", Output: "clock-output\n", OutputEncoding: "utf-8", Prompt: "router1#", PromptSource: "observed", PromptObserved: &observed, Timing: records.Timing{EndedAt: time.Now()}})
	if strings.Contains(out.String(), "router1#show clock") {
		t.Fatalf("prompt unexpectedly echoed: %q", out.String())
	}
}

func TestRecordRendererColorsHeaderFooterAndBorderBySemanticRole(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.color=always",
		"display.theme=dark",
		"display.command.border=<repeat:-:4>",
		"display.command.last-border=true",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	renderer.OnRecord(records.CommandRecord{
		SchemaVersion: records.CommandSchemaVersion, ActivityType: "command",
		Device:          records.DeviceProjection{CanonicalName: "router1"},
		InputTarget:     "router1",
		SelectedAddress: "192.0.2.10",
		Platform:        "cisco_iosxe",
		Credential:      &records.CredentialProjection{DeviceUsername: "svc.operator", Backend: "builtin-env-fallback"},
		Transport:       "system",
		Command:         "show clock", Status: "succeeded", Output: "clock-output\n", OutputEncoding: "utf-8",
		Timing: records.Timing{EndedAt: now},
	})
	if err := renderer.WriteFooter(now, 0, time.Second); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{
		"\x1b[1;33mrouter1\x1b[0m",
		"\x1b[1;35m192.0.2.10\x1b[0m",
		"\x1b[34m [\x1b[0m",
		"\x1b[34m] platform=\x1b[0m",
		"\x1b[90m----\x1b[0m",
		"\x1b[37m0\x1b[0m",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("decorated output missing %q: %q", want, got)
		}
	}
}

func TestJSONLRendererNeverAddsDisplayANSI(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "jsonl", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, Command: "show clock", Status: "succeeded", Output: "ok\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("JSONL contains ANSI escapes: %q", out.String())
	}
}

func TestDynamicBorderUsesHeaderWidthAndOverridesConfiguredBorder(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.command.header=target=<target>",
		"display.command.border=<repeat:=:4>",
		"display.command.dynamic-border-length=7",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{
		Device: records.DeviceProjection{CanonicalName: "r1"}, InputTarget: "r1",
		Status: "succeeded", Output: "ok\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()},
	})
	renderer.OnRecord(records.CommandRecord{
		Device: records.DeviceProjection{CanonicalName: "r1"}, InputTarget: "r1",
		Status: "succeeded", Output: "ok2\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()},
	})
	got := out.String()
	if !strings.Contains(got, "target=r1\nok\n! -------\nok2\n") {
		t.Fatalf("dynamic border did not match nine-column header: %q", got)
	}
	if strings.Contains(got, "====") {
		t.Fatalf("configured static border was not replaced: %q", got)
	}
}

func TestDynamicBorderUsesConfiguredFallbackWithoutHeader(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.command.header=",
		"display.command.dynamic-border-length=7",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "ok\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "ok2\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	if got := out.String(); !strings.Contains(got, "ok\n! -----\nok2\n") {
		t.Fatalf("fallback dynamic border missing: %q", got)
	}
}

func TestPrettyJSONRendererProducesIndentedArray(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.json.indent=4", "display.color=always"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "json", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		renderer.OnRecord(records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: fmt.Sprintf("r%d", i), ActivityID: "activity-1", ActivityType: "command", Sequence: int64(i), Device: records.DeviceProjection{Groups: []string{}}, AddressCandidates: []string{}, Dispatch: records.DispatchContext{}, CommandIndex: i, CommandCount: 2, CommandKind: "requested", Command: "show clock", Status: "succeeded", Output: "ok\n", OutputEncoding: "utf-8", Notices: []records.Notice{}, Timing: records.Timing{EndedAt: time.Now()}})
	}
	if err := renderer.WriteFooter(time.Now(), 0, time.Second); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Fatalf("pretty JSON contains ANSI: %q", out.String())
	}
	if !strings.Contains(out.String(), "\n    {\n        \"schema_version\"") {
		t.Fatalf("configured four-space indentation missing: %q", out.String())
	}
	var decoded []records.CommandRecord
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatalf("pretty output is not valid JSON: %v\n%s", err, out.String())
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded %d records, want 2", len(decoded))
	}
}

func TestRunTextRendererMakesOutputlessFailureVisible(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "run-1", "/tmp/artifacts", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{
		Device: records.DeviceProjection{CanonicalName: "vzn-ftc"}, InputTarget: "vzn-ftc", SelectedAddress: "10.0.2.10",
		Platform: "generic", Transport: "system", Status: "connection_error", OutputEncoding: "utf-8",
		Error:  &records.StructuredError{Code: "connection_refused", Category: "connection", Message: "connection refused"},
		Timing: records.Timing{EndedAt: time.Now()},
	})
	got := out.String()
	if !strings.Contains(got, "vzn-ftc [10.0.2.10]") {
		t.Fatalf("per-target run header missing: %q", got)
	}
	if !strings.Contains(got, "error=connection_refused: connection refused") {
		t.Fatalf("run failure remained invisible: %q", got)
	}
}

// TestRunTextRendererPrintsOmittedOutputNotice is the omitted-output rule
// at the terminal: a record the follow stream sent without its
// output shows the notice's message where the output would stand, as its
// own line, without --debug.
func TestRunTextRendererPrintsOmittedOutputNotice(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "run-1", "/tmp/artifacts", "run", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{
		Device: records.DeviceProjection{CanonicalName: "r1"}, InputTarget: "r1", SelectedAddress: "10.0.2.10",
		Platform: "cisco_iosxe", Transport: "system", Status: "succeeded", OutputEncoding: "utf-8", OutputBytes: 73000, OutputSHA256: "abc",
		Command: "show big", PromptBefore: "r1#", Prompt: "r1#", PromptSource: "observed",
		Notices: []records.Notice{{Code: "follow_output_omitted", Message: "output of 73000 bytes left out of the follow stream: the record's 74107-byte line is more than daemon.max-ipc-frame-bytes (65536) allows in a frame; the output is in commands.jsonl"}},
		Timing:  records.Timing{EndedAt: time.Now()},
	})
	got := out.String()
	if !strings.Contains(got, "r1#show big\nkarvi: output of 73000 bytes left out of the follow stream: the record's 74107-byte line is more than daemon.max-ipc-frame-bytes (65536) allows in a frame; the output is in commands.jsonl\n") {
		t.Fatalf("the omission line is not where the output would stand: %q", got)
	}
	if strings.Contains(got, "error=") {
		t.Fatalf("an omitted output is not a failure: %q", got)
	}
}

func TestStaticBorderIsCroppedToTerminalWidth(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.command.header=",
		"display.command.border=\"<repeat:-:10>\\n\"",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.terminalWidth = 6
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "ok\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "ok2\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	if got := out.String(); !strings.Contains(got, "ok\n------\nok2\n") {
		t.Fatalf("border not cropped: %q", got)
	}
}

func TestDefaultBlankBorderSeparatesRecordsButDoesNotTrail(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.command.header=",
		"display.command.footer=",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "two\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	if err := renderer.WriteFooter(time.Now(), 0, time.Second); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), "one\n\ntwo\n"; got != want {
		t.Fatalf("output=%q, want %q", got, want)
	}
}

func TestLastBorderOptInAndNoBorderOverride(t *testing.T) {
	for _, tc := range []struct {
		name     string
		noBorder bool
		want     string
	}{
		{name: "last border enabled", want: "one\n----\n"},
		{name: "noborder wins", noBorder: true, want: "one\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
				"display.command.header=",
				"display.command.footer=",
				"display.command.border=\"<repeat:-:4>\\n\"",
				"display.command.last-border=true",
			}})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, tc.noBorder)
			if err != nil {
				t.Fatal(err)
			}
			renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
			if err := renderer.WriteFooter(time.Now(), 0, time.Second); err != nil {
				t.Fatal(err)
			}
			if got := out.String(); got != tc.want {
				t.Fatalf("output=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestEchoIncludesCommandOnDeviceErrorWithoutPromptMetadata(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.command.header=",
		"display.command.footer=",
		"display.command.border=",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{
		Device:  records.DeviceProjection{CanonicalName: "router1"},
		Command: "show clock show clock", Status: "device_error",
		Output: "                    ^\n% Invalid input detected at '^' marker.\n", OutputEncoding: "utf-8",
		Error:  &records.StructuredError{Code: "device_command_error", Category: "device", Message: "device reported a command error"},
		Timing: records.Timing{EndedAt: time.Now()},
	})
	got := out.String()
	if !strings.HasPrefix(got, "router1#show clock show clock\n") {
		t.Fatalf("errored command was not echoed: %q", got)
	}
	if !strings.Contains(got, "% Invalid input detected") {
		t.Fatalf("device error output missing: %q", got)
	}
}

func TestRunBorderDefaultsMatchCommandSemantics(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{
		"display.run.header=",
	}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	renderer, err := newRecordRenderer(&out, "text", cfg, false, false, "run-1", "/tmp/artifacts", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "one\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	renderer.OnRecord(records.CommandRecord{Status: "succeeded", Output: "two\n", OutputEncoding: "utf-8", Timing: records.Timing{EndedAt: time.Now()}})
	if err := renderer.WriteFooter(time.Now(), 0, time.Second); err != nil {
		t.Fatal(err)
	}
	// The run's footer (display.run.footer, the command footer's default)
	// ends the display of a run rendered where it executes, after the last
	// record and with no border before it, as command's does.
	if got, want := out.String(), "one\n\ntwo\n! exit=0 elapsed=1s artifacts=/tmp/artifacts\n"; got != want {
		t.Fatalf("run output=%q, want %q", got, want)
	}
}

// TestReplayedRunHasNoFooter: a run read back from commands.jsonl (a job
// followed through the daemon, a job directory shown later) ends without a
// footer, whatever display.run.footer says: the exit and the elapsed time
// are the executing process's.
func TestReplayedRunHasNoFooter(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.run.header=", `display.run.footer="FOOTER <exit-code>"`}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	rr, err := NewRunRenderer(cfg, false, false, "/tmp/artifacts", "text", false, false, false, &out)
	if err != nil {
		t.Fatal(err)
	}
	if err := rr.Line([]byte(`{"status":"succeeded","output":"one\n","output_encoding":"utf-8"}` + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := rr.Finish(nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "one\n" {
		t.Fatalf("followed run output=%q, want no footer", got)
	}
}
