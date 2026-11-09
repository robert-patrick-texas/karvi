package jobexec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// gatheringRenderer is a run's text renderer gathering from path, colour
// off, under sets after the footer left out.
func gatheringRenderer(t *testing.T, out *bytes.Buffer, path string, quiet bool, sets ...string) *recordRenderer {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: append([]string{"display.color=never", "display.run.footer="}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRecordRenderer(out, "text", cfg, quiet, false, "", "/srv/jobs/261108-130024-00", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.gatherFrom(func() (*os.File, error) { return os.Open(path) }) {
		t.Fatal("the renderer does not gather")
	}
	return r
}

// feed gives the renderer each record of the job's commands.jsonl as the
// job's stream carries it.
func feed(t *testing.T, r *recordRenderer, path string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	lines := bufio.NewScanner(f)
	lines.Buffer(make([]byte, 1<<20), 1<<26)
	for lines.Scan() {
		var rec records.CommandRecord
		if err := json.Unmarshal(lines.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		r.OnRecord(rec)
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
}

func gathered(t *testing.T, path string, quiet bool, sets ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, quiet, sets...)
	feed(t, r, path)
	err := r.WriteFooter(time.Now(), 0, time.Second, nil)
	return out.String(), err
}

// TestGatheredDisplayOfTheLabJobs: the lab jobs' records (testdata/gather,
// the design session's jobs, the operator made fictional) give the layout
// the agreed prototype prints from them (S3b/expected.sh): one list for
// all, two commands with a failing device and its not-attempted command, a
// --cf folder's lists (one block of three by text and occurrence, the two
// show clock apart), and a device error's output before its failure line.
func TestGatheredDisplayOfTheLabJobs(t *testing.T) {
	for _, job := range []string{"show-version", "two-commands", "cf-dir", "device-error"} {
		want, err := os.ReadFile(filepath.Join("testdata", "gather", job+".gathered"))
		if err != nil {
			t.Fatal(err)
		}
		got, err := gathered(t, filepath.Join("testdata", "gather", job+".jsonl"), false)
		if err != nil {
			t.Fatalf("%s: %v", job, err)
		}
		if got != string(want) {
			t.Errorf("%s:\n%s\nwant:\n%s", job, got, want)
		}
	}
}

// TestGatheredDisplayEnds: the footer follows the last group as today's
// follows the last record, the border before it only under last-border;
// --quiet leaves out the markers, headers, borders, and footer, never an
// output or a failure line; an empty group header leaves the groups apart
// by the border alone; and a follow whose stream was lost (no summary)
// shows nothing gathered.
func TestGatheredDisplayEnds(t *testing.T) {
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	got, err := gathered(t, path, false, "display.run.footer=! exit=<exit-code>")
	if err != nil || !strings.HasSuffix(got, "connect: connection refused\n! exit=0\n") {
		t.Errorf("the footer: %v\n%s", err, got)
	}
	got, err = gathered(t, path, false, "display.run.footer=! exit=<exit-code>", "display.run.last-border=true")
	if err != nil || !strings.HasSuffix(got, "connect: connection refused\n\n! exit=0\n") {
		t.Errorf("the last border: %v\n%s", err, got)
	}
	got, err = gathered(t, filepath.Join("testdata", "gather", "two-commands.jsonl"), true, "display.run.footer=! exit=<exit-code>")
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"! show version\n", "! show running-config\n", " (7)\n", " (5)\n", "exit="} {
		if strings.Contains(got, absent) {
			t.Errorf("quiet: %q shown:\n%s", absent, got)
		}
	}
	for _, present := range []string{"fake-iosxe uptime is 1 day\nkarvi: status=connection_error", "hostname edge\n", "karvi: status=not_attempted_prior_command_failure\n"} {
		if !strings.Contains(got, present) {
			t.Errorf("quiet: %q not shown:\n%s", present, got)
		}
	}
	got, err = gathered(t, path, false, "display.gather.header=")
	if want := "! show version\nCisco IOS XE Software, Version 17.09.04a\nfake-iosxe uptime is 1 day\n\nkarvi: status=connection_error error=native_session_open_failed: dial tcp 127.0.0.9:38537: connect: connection refused\n"; err != nil || got != want {
		t.Errorf("no header: %v\n%q\nwant\n%q", err, got, want)
	}

	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false)
	feed(t, r, path)
	if err := r.finish(nil); err != nil || out.Len() != 0 {
		t.Errorf("a lost stream: %v %q", err, out.String())
	}
}

// TestGatheredHeaderIsOneLine: a group's header is one line at any width,
// never split at its spaces as a run's header is (DECISIONS point 5).
func TestGatheredHeaderIsOneLine(t *testing.T) {
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false, "display.gather.header=! <targets> (<count>) devices answered alike")
	r.terminalWidth = 20
	feed(t, r, path)
	if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\n! edge[1-2],router[01-04,10] (7) devices answered alike\n") {
		t.Errorf("at 20 columns:\n%s", out.String())
	}
}

// writeJob writes recs as a job's commands.jsonl, each record's sequence
// its line, and returns its path.
func writeJob(t *testing.T, recs []records.CommandRecord) (string, []records.CommandRecord) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for i := range recs {
		recs[i].Sequence = int64(i + 1)
		if _, err := output.WriteRecordLine(f, &recs[i], output.FromRecord(&recs[i])); err != nil {
			t.Fatal(err)
		}
	}
	return path, recs
}

// execAnswer is an exec record of device's answer to command.
func execAnswer(device string, position, index, count int, command, stdout, stderr string, exit int) records.CommandRecord {
	out, outEnc, outDigest := output.EncodeOutput([]byte(stdout))
	errText, errEnc, errDigest := output.EncodeOutput([]byte(stderr))
	size := int64(len(stderr))
	return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: device + command, ActivityType: "run", CommandKind: "requested",
		Device: records.DeviceProjection{ID: device, CanonicalName: device}, InputTarget: device, Channel: records.ChannelExec,
		Dispatch: records.DispatchContext{ScopePosition: position}, CommandIndex: index, CommandCount: count, Command: command, Privilege: records.PrivilegeSudo,
		Status: "success", Output: out, OutputEncoding: outEnc, OutputBytes: int64(len(stdout)), OutputSHA256: outDigest,
		ExitStatus: &exit, Stderr: &errText, StderrEncoding: &errEnc, StderrBytes: &size, StderrSHA256: &errDigest}
}

// TestGatheredExecAnswers: on the exec channel the exit status and stderr
// are in the key, a root command's marker says --sudo, a group's stdout
// is followed by its stderr, a session-init record is not shown, groups of
// one size are ordered by their first names in the fold's natural order
// (case folded, numbers by value), and a device's second run of the same
// command is a block of its own.
func TestGatheredExecAnswers(t *testing.T) {
	recs := []records.CommandRecord{
		{SchemaVersion: records.CommandSchemaVersion, RecordID: "init", ActivityType: "run", CommandKind: "session_init", Device: records.DeviceProjection{ID: "edge10", CanonicalName: "edge10"}, CommandIndex: 1, CommandCount: 1, Command: "terminal length 0", Status: "success", OutputEncoding: "utf-8"},
		execAnswer("edge10", 3, 1, 2, "ls /srv", "a\n", "", 0),
		execAnswer("edge9", 2, 1, 2, "ls /srv", "a\n", "", 0),
		execAnswer("Edge2", 1, 1, 2, "ls /srv", "", "ls: cannot access '/srv'\n", 2),
		execAnswer("edge10", 3, 2, 2, "ls /srv", "b\n", "", 0),
		execAnswer("edge9", 2, 2, 2, "ls /srv", "c\n", "", 0),
		execAnswer("Edge2", 1, 2, 2, "ls /srv", "d", "e\n", 1),
	}
	path, _ := writeJob(t, recs)
	got, err := gathered(t, path, false)
	if err != nil {
		t.Fatal(err)
	}
	want := `! --sudo ls /srv
! edge[9-10] (2)
a

! Edge2 (1)
ls: cannot access '/srv'

! --sudo ls /srv
! Edge2 (1)
d
e

! edge9 (1)
c

! edge10 (1)
b
`
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestGatheredDisplayRefusals: at the end, a commands.jsonl that cannot be
// opened, or whose line is not the record the job's stream carried, is
// run_output_records_unreadable (the two stacked), and no footer follows.
func TestGatheredDisplayRefusals(t *testing.T) {
	src := filepath.Join("testdata", "gather", "show-version.jsonl")
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	// The job's first record moved: its line holds the second's.
	lines[0], lines[1] = lines[1], lines[0]
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	if err := os.WriteFile(path, bytes.Join(lines, nil), 0o640); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false, "display.run.footer=! exit=<exit-code>")
	feed(t, r, src)
	err = r.WriteFooter(time.Now(), 0, 0, nil)
	if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), " is not the record the job's stream carried\n  carried  record ") || !strings.Contains(err.Error(), "\n  read     record ") {
		t.Errorf("another record: %v", err)
	}
	if strings.Contains(out.String(), "exit=") {
		t.Errorf("a footer after the refusal:\n%s", out.String())
	}

	out.Reset()
	r = gatheringRenderer(t, &out, src, false, "display.run.footer=! exit=<exit-code>")
	r.gather.open = func() (*os.File, error) {
		return nil, errorcodes.Errorf("run_output_records_unreadable", "%s has mode 0644; the canonical file is 0640 or stricter", src)
	}
	feed(t, r, src)
	err = r.WriteFooter(time.Now(), 0, 0, nil)
	if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), "mode 0644") || out.Len() != 0 {
		t.Errorf("an unreadable file: %v %q", err, out.String())
	}
}

// TestGatheringHoldsNoOutput: while the job runs the renderer keeps no
// record's output: 400 records of 128 KiB, each its own, leave the heap
// within a few MiB of where it began once collected.
func TestGatheringHoldsNoOutput(t *testing.T) {
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, filepath.Join(t.TempDir(), "unused"), false)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := 0; i < 400; i++ {
		raw := bytes.Repeat([]byte{byte('a' + i%26)}, 128<<10)
		raw = append(raw, []byte(strings.Repeat("x", i))...)
		text, enc, digest := output.EncodeOutput(raw)
		device := "router" + string(rune('a'+i%26)) + strings.Repeat("z", i/26)
		r.OnRecord(records.CommandRecord{ActivityType: "run", CommandKind: "requested", Device: records.DeviceProjection{ID: device, CanonicalName: device}, Sequence: int64(i + 1),
			Dispatch: records.DispatchContext{ScopePosition: i + 1}, CommandIndex: 1, CommandCount: 1, Command: "show running-config", Status: "success",
			Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest})
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if grew := int64(after.HeapAlloc) - int64(before.HeapAlloc); grew > 4<<20 {
		t.Fatalf("the heap grew %d bytes over 400 records of 128 KiB", grew)
	}
	if out.Len() != 0 || len(r.gather.blocks) != 1 || len(r.gather.blocks[blockKey{command: "show running-config", occurrence: 1}].groups) != 400 {
		t.Fatalf("%d bytes written, %d blocks", out.Len(), len(r.gather.blocks))
	}
	if len(r.gather.inFlight) != 0 {
		t.Fatalf("%d devices' counts kept after their last records", len(r.gather.inFlight))
	}
}

// TestGatheredDisplayReadsOnlyWhatItShows: commands.jsonl is opened only
// when a group has an answer to read back; a job whose groups all failed
// without output shows them whole with no file to read.
func TestGatheredDisplayReadsOnlyWhatItShows(t *testing.T) {
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, filepath.Join(t.TempDir(), "absent.jsonl"), false)
	r.OnRecord(withError(shellAnswer("r1", 1, 1, 2, "show a", "connection_error", ""), "native_session_open_failed", "refused"))
	r.OnRecord(shellAnswer("r1", 1, 2, 2, "show b", "not_attempted_prior_command_failure", ""))
	if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if want := "! show a\n! r1 (1)\nkarvi: status=connection_error error=native_session_open_failed: refused\n\n! show b\n! r1 (1)\nkarvi: status=not_attempted_prior_command_failure\n"; out.String() != want {
		t.Errorf("got\n%q\nwant\n%q", out.String(), want)
	}
}

// shellAnswer is a shell record of device's answer to command.
func shellAnswer(device string, position, index, count int, command, status, out string) records.CommandRecord {
	text, enc, digest := output.EncodeOutput([]byte(out))
	return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: device + command, ActivityType: "run", CommandKind: "requested",
		Device: records.DeviceProjection{ID: "name:" + device, CanonicalName: device}, InputTarget: device, Channel: records.ChannelShell,
		Dispatch: records.DispatchContext{ScopePosition: position}, CommandIndex: index, CommandCount: count, Command: command,
		Status: status, Output: text, OutputEncoding: enc, OutputBytes: int64(len(out)), OutputSHA256: digest}
}

func withError(r records.CommandRecord, code, message string) records.CommandRecord {
	r.Error = &records.StructuredError{Code: code, Message: message}
	return r
}

// gatherRecords writes recs as the job's file, feeds them as its stream
// carries them, and ends the display.
func gatherRecords(t *testing.T, recs []records.CommandRecord, sets ...string) (string, error) {
	t.Helper()
	path, recs := writeJob(t, recs)
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false, sets...)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	err := r.WriteFooter(time.Now(), 0, 0, nil)
	return out.String(), err
}

// TestGatheredGroupsAndKeys (from S3b's review, its mutants M10 to M15):
// the succeeded group comes first though a failure group is larger; on the
// exec channel the same stdout with another stderr, another exit status,
// or no exit status at all against 0 is a group of its own; the
// occurrence counts text and privilege together (a device's `ls` then
// `--sudo ls` and another's `--sudo ls` share the --sudo block); and a
// record whose output is in a spool (in process, Output "") or left out of
// the follow stream is shown whole from the file.
func TestGatheredGroupsAndKeys(t *testing.T) {
	recs := []records.CommandRecord{shellAnswer("r1", 1, 1, 1, "show version", "success", "v\n")}
	for i, name := range []string{"r2", "r3", "r4"} {
		recs = append(recs, withError(shellAnswer(name, 2+i, 1, 1, "show version", "timeout", ""), "command_timeout", "timed out"))
	}
	if got, err := gatherRecords(t, recs); err != nil || got != "! show version\n! r1 (1)\nv\n\n! r[2-4] (3)\nkarvi: status=timeout error=command_timeout\n" {
		t.Errorf("succeeded first: %v\n%q", err, got)
	}

	a := execAnswer("h1", 1, 1, 1, "id", "uid=0\n", "", 0)
	b := execAnswer("h2", 2, 1, 1, "id", "uid=0\n", "warn\n", 0)
	c := execAnswer("h3", 3, 1, 1, "id", "uid=0\n", "", 1)
	d := execAnswer("h4", 4, 1, 1, "id", "uid=0\n", "", 0)
	d.ExitStatus = nil
	if got, err := gatherRecords(t, []records.CommandRecord{a, b, c, d}); err != nil || strings.Count(got, " (1)\n") != 4 {
		t.Errorf("the exec key: %v\n%s", err, got)
	}

	plain := execAnswer("h1", 1, 1, 2, "ls", "a\n", "", 0)
	plain.Privilege = ""
	if got, err := gatherRecords(t, []records.CommandRecord{plain, execAnswer("h1", 1, 2, 2, "ls", "b\n", "", 0), execAnswer("h2", 2, 1, 1, "ls", "b\n", "", 0)}); err != nil || got != "! ls\n! h1 (1)\na\n\n! --sudo ls\n! h[1-2] (2)\nb\n" {
		t.Errorf("the occurrence by privilege: %v\n%q", err, got)
	}

	path, written := writeJob(t, []records.CommandRecord{
		shellAnswer("r1", 1, 1, 1, "show run", "success", "hostname r1\n"),
		shellAnswer("r2", 2, 1, 1, "show run", "success", "hostname r2\n"),
	})
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false)
	spooled := written[0]
	spooled.Output = ""
	r.OnRecordFrom(spooled, output.FromSpool(path+".gone", spooled.OutputSHA256, spooled.OutputEncoding))
	omitted := written[1]
	omitted.Output = ""
	omitted.Notices = []records.Notice{{Code: "follow_output_omitted", Message: "output left out"}}
	r.OnRecord(omitted)
	if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil || !strings.Contains(out.String(), "hostname r1\n") || !strings.Contains(out.String(), "hostname r2\n") || strings.Contains(out.String(), "left out") {
		t.Errorf("spooled and left out: %v\n%s", err, out.String())
	}
}

// TestGatheredBorderAndFooterValues (from S3b's review, its mutant M17):
// under --border the border is as wide as the group header above it; and
// the footer's record values are the last shown record's, as today's are,
// not a not-attempted record's.
func TestGatheredBorderAndFooterValues(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{shellAnswer("edge1", 1, 1, 1, "show clock", "success", "x\n"), shellAnswer("edge2", 2, 1, 1, "show clock", "success", "y\n")})
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never", "display.run.footer="}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", cfg, false, false, "", "/srv/jobs/x", "run", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	r.gatherFrom(func() (*os.File, error) { return os.Open(path) })
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	if want := "! show clock\n! edge1 (1)\nx\n! " + strings.Repeat("-", len("! edge1 (1)")-2) + "\n! edge2 (1)\ny\n"; out.String() != want {
		t.Errorf("--border:\n%q\nwant\n%q", out.String(), want)
	}

	got, err := gatherRecords(t, []records.CommandRecord{
		withError(shellAnswer("alpha", 1, 1, 2, "show a", "connection_error", ""), "native_session_open_failed", "refused"),
		shellAnswer("bravo", 2, 1, 1, "show a", "success", "A\n"),
		shellAnswer("alpha", 1, 2, 2, "show b", "not_attempted_prior_command_failure", ""),
	}, "display.run.footer=! last=<target>")
	if err != nil || !strings.HasSuffix(got, "\n! last=bravo\n") {
		t.Errorf("the footer's values: %v\n%s", err, got)
	}
}

// TestGatheredReadBackChecked (from S3b's review, its mutant M06): a line
// with the group's sequence and device but another command index, or
// another output, is not the group's record; and a later block's refusal
// leaves the groups written before it, its own border and marker unwritten.
func TestGatheredReadBackChecked(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(*records.CommandRecord)
	}{
		{"index", func(r *records.CommandRecord) { r.CommandIndex, r.CommandCount = 2, 2 }},
		{"output", func(r *records.CommandRecord) {
			r.Output, r.OutputEncoding, r.OutputSHA256 = output.EncodeOutput([]byte("another answer\n"))
		}},
	} {
		stream := shellAnswer("r1", 1, 1, 1, "show version", "success", "v\n")
		file := stream
		c.edit(&file)
		path, _ := writeJob(t, []records.CommandRecord{file})
		stream.Sequence = 1
		var out bytes.Buffer
		r := gatheringRenderer(t, &out, path, false)
		r.OnRecord(stream)
		if err := r.WriteFooter(time.Now(), 0, 0, nil); errorcodes.Of(err) != "run_output_records_unreadable" {
			t.Errorf("another %s: %v\n%s", c.name, err, out.String())
		}
	}

	path, recs := writeJob(t, []records.CommandRecord{shellAnswer("r1", 1, 1, 2, "show a", "success", "A\n"), shellAnswer("r1", 1, 2, 2, "show b", "success", "B\n")})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	lines[1] = lines[0]
	if err := os.WriteFile(path, bytes.Join(lines, nil), 0o640); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false, "display.run.footer=! exit=<exit-code>")
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, 0, nil); errorcodes.Of(err) != "run_output_records_unreadable" || out.String() != "! show a\n! r1 (1)\nA\n" {
		t.Errorf("a later block refused: %v\n%q", err, out.String())
	}
}

// TestTextDisplayEndsAnAnswersLastLine: today's display (not gathered)
// ends an output without its last newline before the failure line that
// follows it, and before the next record's border and header (found
// uncovered by S3b's review when the answer's writing moved to
// writeAnswerLocked).
func TestTextDisplayEndsAnAnswersLastLine(t *testing.T) {
	got := renderAll(t, []string{"display.color=never", "display.run.header=! <target>"}, false, false,
		withError(shellAnswer("r1", 1, 1, 1, "show a", "device_error", "% Invalid input"), "device_command_error", "device reported a command error"),
		shellAnswer("r2", 2, 1, 1, "show a", "success", "A"))
	if want := "! r1\n% Invalid input\nkarvi: target=r1 status=device_error error=device_command_error: device reported a command error\n\n! r2\nA\n"; got != want {
		t.Errorf("got\n%q\nwant\n%q", got, want)
	}
}

// sessionInit is a session-init record of device's profile command.
func sessionInit(device string, position, index, count int, profile, command, status string) records.CommandRecord {
	r := shellAnswer(device, position, index, count, command, status, "")
	r.CommandKind, r.SessionInitProfile = "session_init", profile
	return r
}

// TestGatheredSessionInitFailure: a failed session-init record, which
// today's display shows with its cause, is gathered in a block of its own
// ahead of the requested blocks, its marker naming the profile and the
// command, its groups as any block's (the message for a group of one);
// succeeded and stopped session-init records stay hidden, as today.
func TestGatheredSessionInitFailure(t *testing.T) {
	got, err := gatherRecords(t, []records.CommandRecord{
		withError(sessionInit("r1", 1, 1, 1, "iosxe", "terminal length 0", "timeout"), "command_timeout", "session-init command timed out after 30s"),
		shellAnswer("r1", 1, 1, 1, "show version", "not_attempted_session_init_failure", ""),
		sessionInit("r2", 2, 1, 1, "iosxe", "terminal length 0", "success"),
		shellAnswer("r2", 2, 1, 1, "show version", "success", "v\n"),
		sessionInit("r3", 3, 1, 1, "iosxe", "terminal length 0", "cancel"),
		shellAnswer("r3", 3, 1, 1, "show version", "cancel", ""),
	})
	want := `! session_init=iosxe: terminal length 0
! r1 (1)
karvi: status=timeout error=command_timeout: session-init command timed out after 30s

! show version
! r2 (1)
v

! r1 (1)
karvi: status=not_attempted_session_init_failure

! r3 (1)
karvi: status=cancel
`
	if err != nil || got != want {
		t.Errorf("%v\ngot\n%s\nwant\n%s", err, got, want)
	}

	// Two devices failing the profile alike are one group, no message.
	got, err = gatherRecords(t, []records.CommandRecord{
		sessionInit("r1", 1, 1, 2, "iosxe", "terminal length 0", "success"),
		withError(sessionInit("r1", 1, 2, 2, "iosxe", "terminal width 0", "timeout"), "command_timeout", "timed out on r1"),
		withError(sessionInit("r2", 2, 2, 2, "iosxe", "terminal width 0", "timeout"), "command_timeout", "timed out on r2"),
		shellAnswer("r1", 1, 1, 1, "show version", "not_attempted_session_init_failure", ""),
		shellAnswer("r2", 2, 1, 1, "show version", "not_attempted_session_init_failure", ""),
	})
	want = `! session_init=iosxe: terminal width 0
! r[1-2] (2)
karvi: status=timeout error=command_timeout

! show version
! r[1-2] (2)
karvi: status=not_attempted_session_init_failure
`
	if err != nil || got != want {
		t.Errorf("%v\ngot\n%s\nwant\n%s", err, got, want)
	}
}
