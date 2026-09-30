package output

import (
	"fmt"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAppend(t *testing.T) {
	s, err := Create(Options{Root: t.TempDir(), ID: "id", Fsync: true, MaxJobBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", ActivityType: "command", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "d", Name: "d", CanonicalName: "d", Groups: []string{}}, InputTarget: "d", TransformedName: "d", DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "generic", Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: 1, CommandCount: 1, CommandKind: "requested", Command: "x", CommandSHA256: "x", Status: "succeeded", Output: "ok", OutputEncoding: "utf-8", OutputSHA256: "x", Prompt: "", PromptSource: "", Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: time.Now(), EndedAt: time.Now()}, Error: nil}
	n, err := appendRecord(s, &r)
	if err != nil {
		t.Fatal(err)
	}
	if n.Sequence != 1 {
		t.Fatal(n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(s.Paths().CommandsJSONL)
	if len(b) == 0 {
		t.Fatal("empty")
	}
}

// TestFailedDevicesFollowRequestedRecords: failures.jsonl takes every
// non-succeeded record of either kind;
// failed-devices.txt lists a device only for a non-succeeded requested one.
func TestFailedDevicesFollowRequestedRecords(t *testing.T) {
	s, err := Create(Options{Root: t.TempDir(), ID: "id", MaxJobBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	record := func(device, kind, status string) records.CommandRecord {
		r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", JobID: "a", ActivityType: "run", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "name:" + device, Name: device, CanonicalName: device, Groups: []string{}}, InputTarget: device, TransformedName: device, DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "cisco_iosxe", Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: 1, CommandCount: 1, CommandKind: kind, SessionInitProfile: "p", Command: "x", CommandSHA256: "x", Status: status, OutputEncoding: "utf-8", OutputSHA256: "x", Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: time.Now(), EndedAt: time.Now()}}
		if status != "succeeded" {
			r.Error = &records.StructuredError{Code: "device_command_error", Category: "device", Message: "m", Operation: "execute_command"}
		}
		return r
	}
	for _, r := range []records.CommandRecord{
		record("continued", "session_init", "device_error"), record("continued", "requested", "succeeded"),
		record("stopped", "session_init", "device_error"), record("stopped", "requested", "device_error"),
	} {
		if _, err := appendRecord(s, &r); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	failed, _ := os.ReadFile(s.Paths().FailedDevices)
	if string(failed) != "stopped\n" {
		t.Errorf("failed-devices.txt %q, want only the device with a failed requested record", failed)
	}
	failures, _ := os.ReadFile(s.Paths().FailuresJSONL)
	if n := len(strings.Split(strings.TrimSpace(string(failures)), "\n")); n != 3 {
		t.Errorf("failures.jsonl has %d lines, want 3", n)
	}
}

// TestTextFilePerDevice: each device's
// records go to its own output.TARGET.txt, the header once, a block per
// record in the device's order, whatever the order the devices' records
// reach the store in; the file is closed after each block, 0640 like the
// job's other files, and never written through a symbolic link. A block
// that cannot be written is a notice, once for the device, and not a
// failed append: the record is the authority and the job goes on.
//
// A device's set-up lines, given before its first record, are written once
// between the header and the first block; lines given for a device whose
// file exists are dropped.
func TestTextFilePerDevice(t *testing.T) {
	root := t.TempDir()
	var warnings []string
	s, err := Create(Options{Root: root, ID: "id", MaxJobBytes: 1 << 20, Warn: func(m string) { warnings = append(warnings, m) }})
	if err != nil {
		t.Fatal(err)
	}
	record := func(device, before, command, output string) records.CommandRecord {
		return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", JobID: "a", ActivityType: "run", Device: records.DeviceProjection{CanonicalName: device}, CommandKind: "requested", PromptBefore: before, Command: command, Status: "succeeded", Output: output, OutputEncoding: "utf-8"}
	}
	s.SetTextSetup("r1", []platform.SetupLine{{PromptBefore: "r1>", Statement: "enable", Output: "Password:\n"}, {PromptBefore: "r1#", Statement: "terminal length 0"}})
	s.SetTextSetup("r7", nil) // a driver with no set-up holds nothing
	for _, r := range []records.CommandRecord{
		record("r1", "r1#", "show clock", "10:00\n"),
		record("2001:db8::10", "sw#", "show clock", "10:01\n"),
		record("r1", "r1#", "configure terminal", "Enter configuration commands.\n"),
		record("r1", "r1(config)#", "end", ""),
	} {
		if _, err := appendRecord(s, &r); err != nil {
			t.Fatal(err)
		}
	}
	for name, want := range map[string]string{
		"output.r1.txt":           "! ### r1 ###\nr1>enable\nPassword:\nr1#terminal length 0\nr1#show clock\n10:00\nr1#configure terminal\nEnter configuration commands.\nr1(config)#end\n",
		"output.2001-db8--10.txt": "! ### 2001:db8::10 ###\nsw#show clock\n10:01\n",
	} {
		got, err := os.ReadFile(root + "/" + name)
		if err != nil || string(got) != want {
			t.Fatalf("%s: %q (%v), want %q", name, got, err, want)
		}
		if info, _ := os.Stat(root + "/" + name); info.Mode().Perm() != 0o640 {
			t.Fatalf("%s: mode %v", name, info.Mode().Perm())
		}
	}
	if len(s.setups) != 0 {
		t.Fatalf("set-up lines still held after the first records: %v", s.setups)
	}
	s.SetTextSetup("2001:db8::10", []platform.SetupLine{{PromptBefore: "sw#", Statement: "late"}})
	// A link put at a device's path after its first block is not followed.
	if err := os.Remove(root + "/output.r1.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root+"/elsewhere", root+"/output.r1.txt"); err != nil {
		t.Fatal(err)
	}
	for _, r := range []records.CommandRecord{
		record("r1", "r1#", "show version", "v\n"),
		record("r1", "r1#", "show users", "u\n"),
		record("2001:db8::10", "sw#", "show version", "v\n"),
	} {
		if n, err := appendRecord(s, &r); err != nil || n.Sequence == 0 {
			t.Fatalf("the append failed with its text file: %v", err)
		}
	}
	if _, err := os.Stat(root + "/elsewhere"); !os.IsNotExist(err) {
		t.Fatalf("the link's target exists: %v", err)
	}
	if len(warnings) != 1 || !strings.HasPrefix(warnings[0], "output_text_write_failed: output.r1.txt is not written from record 5 on: ") {
		t.Fatalf("warnings: %q", warnings)
	}
	// The other device's file goes on, and all seven lines are in commands.jsonl.
	if got, _ := os.ReadFile(root + "/output.2001-db8--10.txt"); !strings.HasSuffix(string(got), "sw#show version\nv\n") || strings.Contains(string(got), "late") {
		t.Fatalf("the other device's file: %q", got)
	}
	if lines, _ := os.ReadFile(s.Paths().CommandsJSONL); strings.Count(string(lines), "\n") != 7 {
		t.Fatalf("commands.jsonl has %d lines", strings.Count(string(lines), "\n"))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestSkippedFiles: a skipped file is not created and the store is
// otherwise the same. With every file
// skipped (`cmd --nof`) no root is needed and no folder is created, and a
// record is still validated and given its sequence; its notice locates no
// line. With some skipped, the others are written as ever.
func TestSkippedFiles(t *testing.T) {
	record := func(status string) records.CommandRecord {
		return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", JobID: "a", ActivityType: "command", Device: records.DeviceProjection{CanonicalName: "r1"}, CommandKind: "requested", PromptBefore: "r1#", Command: "show clock", Status: status, Output: "10:00\n", OutputEncoding: "utf-8"}
	}
	writeAll := func(s *Store) {
		t.Helper()
		if err := s.WriteCommands([]string{"show clock"}); err != nil {
			t.Fatal(err)
		}
		for _, write := range []func(any) error{s.WriteManifest, s.WriteMetrics, s.WriteSummary} {
			if err := write(map[string]any{"k": "v"}); err != nil {
				t.Fatal(err)
			}
		}
		for i, status := range []string{"succeeded", "device_error"} {
			r := record(status)
			n, err := appendRecord(s, &r)
			if err != nil || n.Sequence != int64(i+1) || r.Sequence != int64(i+1) {
				t.Fatalf("append %d: notice %+v, record sequence %d, %v", i+1, n, r.Sequence, err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
	}
	names := func(dir string) string {
		entries, _ := os.ReadDir(dir)
		var got []string
		for _, e := range entries {
			got = append(got, e.Name())
		}
		return strings.Join(got, " ")
	}

	parent := t.TempDir()
	root := parent + "/job"
	s, err := Create(Options{Skip: AllFiles, Root: root, ID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	writeAll(s)
	if got := names(parent); got != "" || s.Paths() != (Paths{}) || s.Bytes() != 0 {
		t.Fatalf("every file skipped: created %q, paths %+v, bytes %d", got, s.Paths(), s.Bytes())
	}
	bad := record("succeeded")
	bad.CommandKind = "other"
	if s2, _ := Create(Options{Skip: AllFiles, ID: "id"}); s2 != nil {
		if _, err := appendRecord(s2, &bad); err == nil {
			t.Fatal("a record that is not valid was accepted with every file skipped")
		}
	}

	s, err = Create(Options{Skip: FileSet{FailuresJSONL: true, OutputTxt: true, MetricsJSON: true}, Root: root, ID: "id", MaxJobBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	writeAll(s)
	if got, want := names(root), "commands.jsonl commands.txt failed-devices.txt manifest.json summary.json"; got != want {
		t.Fatalf("three files skipped: the folder holds %q, want %q", got, want)
	}
	if got, _ := os.ReadFile(root + "/failed-devices.txt"); string(got) != "r1\n" {
		t.Fatalf("failed-devices.txt %q: a device is listed whether or not failures.jsonl is written", got)
	}
	// Paths names the files that are written and no other: the summary's
	// "paths" is made from it.
	p := s.Paths()
	if p.FailuresJSONL != "" || p.Metrics != "" {
		t.Fatalf("paths %+v name a skipped file", p)
	}
	for _, written := range []string{p.CommandsJSONL, p.CommandsText, p.FailedDevices, p.Manifest, p.Summary} {
		if _, err := os.Stat(written); written == "" || err != nil {
			t.Fatalf("paths %+v: %q is not a written file (%v)", p, written, err)
		}
	}
}

// TestJobLimitCountsTheText: the
// limit is on what the store appends of the devices' output, the lines and
// the text blocks together. The record decides: a text block that would pass
// the limit is left out with the notice and the job goes on; a line that
// would pass it fails the append, and the store keeps that error and lists
// the device, since the device's result reaches no operator by itself.
func TestJobLimitCountsTheText(t *testing.T) {
	record := func() records.CommandRecord {
		return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", JobID: "a", ActivityType: "command", Device: records.DeviceProjection{CanonicalName: "r1"}, CommandKind: "requested", PromptBefore: "r1#", Command: "show big", Status: "succeeded", Output: strings.Repeat("x", 1000) + "\n", OutputEncoding: "utf-8"}
	}
	appendOne := func(s *Store) error { r := record(); _, err := appendRecord(s, &r); return err }
	create := func(skip FileSet, limit int64, warn func(string)) (*Store, string) {
		t.Helper()
		root := t.TempDir() + "/job"
		s, err := Create(Options{Skip: skip, Root: root, ID: "id", MaxJobBytes: limit, Warn: warn})
		if err != nil {
			t.Fatal(err)
		}
		return s, root
	}
	unlimited, _ := create(FileSet{}, 0, nil)
	if err := appendOne(unlimited); err != nil {
		t.Fatal(err)
	}
	line := unlimited.Bytes() // one record's line; its text block is over 1000 bytes

	// The line fits and the text does not: the record is kept, the text is
	// not written, one notice; the next line does not fit either.
	var notices []string
	s, root := create(FileSet{}, line+500, func(m string) { notices = append(notices, m) })
	if err := appendOne(s); err != nil || s.Err() != nil {
		t.Fatalf("the line fits the limit: %v, %v", err, s.Err())
	}
	if _, err := os.Stat(root + "/output.r1.txt"); err == nil {
		t.Fatal("a text block that passes the limit was written")
	}
	if len(notices) != 1 || !strings.Contains(notices[0], "output_text_write_failed") || !strings.Contains(notices[0], "output_job_limit_exceeded") || !strings.Contains(notices[0], "derived from commands.jsonl") {
		t.Fatalf("notices %q", notices)
	}
	err := appendOne(s)
	if errorcodes.Of(err) != "output_job_limit_exceeded" {
		t.Fatalf("a line past the limit: %v", err)
	}
	if got := s.Err(); got == nil || errorcodes.Of(got) != "output_job_limit_exceeded" || !strings.Contains(got.Error(), "device r1") {
		t.Fatalf("the store's error %v does not carry the code and the device", got)
	}
	s.Close()
	if got, _ := os.ReadFile(root + "/failed-devices.txt"); string(got) != "r1\n" {
		t.Fatalf("failed-devices.txt %q: the device whose record was not written is not listed", got)
	}

	// With the text switched off the old arithmetic is back: two lines fit
	// a limit that, with the text, holds one.
	for _, c := range []struct {
		skip FileSet
		fits bool
	}{{FileSet{OutputTxt: true}, true}, {FileSet{}, false}} {
		s, _ := create(c.skip, 2*line+10, nil)
		first, second := appendOne(s), appendOne(s)
		if first != nil || (second == nil) != c.fits {
			t.Fatalf("skip %+v, limit of two lines: %v, %v", c.skip, first, second)
		}
		s.Close()
	}

	// Text only: the text is what counts, and a notice cannot point at a
	// commands.jsonl that is not written.
	notices = nil
	s, _ = create(FileSet{CommandsJSONL: true}, 500, func(m string) { notices = append(notices, m) })
	if err := appendOne(s); err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 || strings.Contains(notices[0], "derived") || !strings.Contains(notices[0], "is not kept") {
		t.Fatalf("notices %q", notices)
	}
	s.Close()
}

// TestFinishedSizeCountsTheCopies: the job limit's estimate counts the
// copies, and the output root's copies are the files it writes.
func TestFinishedSizeCountsTheCopies(t *testing.T) {
	for _, c := range []struct {
		skip FileSet
		want int64
	}{{FileSet{}, 30000}, {FileSet{OutputTxt: true}, 15000}, {FileSet{CommandsJSONL: true}, 15000}, {FileSet{CommandsJSONL: true, OutputTxt: true}, 0}} {
		if got := FinishedSize(1000, 10, c.skip.OutputCopies(), 1.5); got != c.want {
			t.Errorf("skip %+v: %d bytes, want %d", c.skip, got, c.want)
		}
	}
	if got := FinishedSize(262144, 10, 2, 0); got != 6553600 {
		t.Errorf("the default multiplier: %d", got)
	}
}

// TestNoticesLeaveInSequence: the daemon's followers refuse a stream whose
// notice does not follow the one before (ipc_result_malformed), so the
// store hands each notice over under its lock, before the record's text
// block is written. Handed over by the caller after AppendRecord returned,
// a device with a large text block was overtaken by one with a small one:
// at width 32 every output-scale run failed.
func TestNoticesLeaveInSequence(t *testing.T) {
	var got []int64 // written under the store's lock, which is the point
	s, err := Create(Options{Root: t.TempDir() + "/job", ID: "id", OnDurable: func(_ *records.CommandRecord, n Notice) { got = append(got, n.Sequence) }})
	if err != nil {
		t.Fatal(err)
	}
	const devices = 16
	var wg sync.WaitGroup
	for i := 0; i < devices; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Every other device has a text block of 4 MiB, the rest of a line.
			size := 10
			if i%2 == 0 {
				size = 4 << 20
			}
			name := fmt.Sprintf("r%02d", i)
			r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: name, ActivityID: "a", JobID: "a", ActivityType: "run", Device: records.DeviceProjection{CanonicalName: name}, CommandKind: "requested", PromptBefore: name + "#", Command: "show big", Status: "succeeded", Output: strings.Repeat("x", size) + "\n", OutputEncoding: "utf-8"}
			if _, err := appendRecord(s, &r); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	s.Close()
	if len(got) != devices {
		t.Fatalf("%d notices, want %d", len(got), devices)
	}
	for i, sequence := range got {
		if sequence != int64(i+1) {
			t.Fatalf("notices left in the order %v", got)
		}
	}
}

// TestWriteCommandLists: a plan's list
// per platform is commands.PLATFORM.txt beside commands.txt, one statement
// per line, and none when commands.txt is switched off or there is no list.
func TestWriteCommandLists(t *testing.T) {
	dir := t.TempDir()
	s, err := Create(Options{Root: filepath.Join(dir, "job"), ID: "260924-020000-00"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.WriteCommands(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "job", "commands.txt")); !os.IsNotExist(err) {
		t.Fatalf("no commands, no commands.txt: %v", err)
	}
	if err := s.WriteCommandLists(map[string][]string{"cisco_iosxe": {"show running-config", "show version"}, "juniper_junos": {"show configuration"}}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "job", "commands.cisco_iosxe.txt"))
	if err != nil || string(got) != "show running-config\nshow version\n" {
		t.Fatalf("cisco_iosxe: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "job", "commands.juniper_junos.txt")); err != nil {
		t.Fatal(err)
	}
	off, err := Create(Options{Root: filepath.Join(dir, "job2"), ID: "260924-020001-00", Skip: FileSet{CommandsTxt: true}})
	if err != nil {
		t.Fatal(err)
	}
	defer off.Close()
	if err := off.WriteCommandLists(map[string][]string{"cisco_iosxe": {"show version"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "job2", "commands.cisco_iosxe.txt")); !os.IsNotExist(err) {
		t.Fatalf("commands.txt off, no list file: %v", err)
	}
}

// TestCollectionFiles covers the collection files in the store: a
// device whose records all passed (a rejected statement among them) has
// its file renamed into place at the mode, its blocks under markers with a
// blank line before every marker but the first and no error line; a
// device not reached keeps its previous file and leaves no temporary; a
// stale temporary of a device is swept; a device still open at the close
// is kept; the summary counts both; no output.NAME.txt is involved.
func TestCollectionFiles(t *testing.T) {
	dir := t.TempDir()
	coll := filepath.Join(dir, "crun")
	if err := os.MkdirAll(coll, 0o770); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coll, "r2"), []byte("old r2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(coll, ".r1.260924-000000-00"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	var warnings []string
	s, err := Create(Options{Root: filepath.Join(dir, "job"), ID: "260924-020000-00", Skip: FileSet{OutputTxt: true}, CropNames: true,
		Collection: &CollectionOptions{Directory: coll, FileMode: 0o660}, Warn: func(m string) { warnings = append(warnings, m) }})
	if err != nil {
		t.Fatal(err)
	}
	rec := func(device, command, status, code, out string, index int) *records.CommandRecord {
		r := &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "260924-020000-00", JobID: "260924-020000-00", ActivityType: "run", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "name:" + device, Name: device, CanonicalName: device, Groups: []string{}}, InputTarget: device, TransformedName: device, DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "cisco_iosxe", Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: index, CommandCount: 2, CommandKind: "requested", Command: command, CommandSHA256: "x", Status: status, Output: out, OutputEncoding: "utf-8", OutputSHA256: "x", Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: time.Now(), EndedAt: time.Now()}}
		if status != "succeeded" {
			r.Error = &records.StructuredError{Code: code, Category: "device", Message: "m", Operation: "command"}
		}
		return r
	}
	// r1.example.net: a rejected statement, then a success: replaced.
	for _, r := range []*records.CommandRecord{rec("r1.example.net", "show running-config", "device_error", "device_command_error", "     ^\n% Invalid input detected at '^' marker.\n", 1), rec("r1.example.net", "show clock", "succeeded", "", "*10:00:00.000 UTC Tue Sep 15 2026\n", 2)} {
		if _, err := appendRecord(s, r); err != nil {
			t.Fatal(err)
		}
	}
	s.EndDevice("r1.example.net")
	got, err := os.ReadFile(filepath.Join(coll, "r1"))
	if err != nil || string(got) != "! show running-config\n     ^\n% Invalid input detected at '^' marker.\n\n! show clock\n*10:00:00.000 UTC Tue Sep 15 2026\n" {
		t.Fatalf("r1: %q %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(coll, "r1")); fi.Mode().Perm() != 0o660 {
		t.Fatalf("r1 mode %o", fi.Mode().Perm())
	}
	if stale, _ := filepath.Glob(filepath.Join(coll, ".r1.*")); len(stale) != 0 {
		t.Fatalf("temporaries left: %v", stale)
	}
	// r2: not reached, then not attempted: kept, nothing left behind.
	for _, r := range []*records.CommandRecord{rec("r2", "show running-config", "connection_error", "native_session_open_failed", "", 1), rec("r2", "show clock", "not_attempted_prior_command_failure", "", "", 2)} {
		if _, err := appendRecord(s, r); err != nil {
			t.Fatal(err)
		}
	}
	s.EndDevice("r2")
	if got, _ := os.ReadFile(filepath.Join(coll, "r2")); string(got) != "old r2\n" {
		t.Fatalf("r2 was touched: %q", got)
	}
	// r3: one success and no end before the close: kept.
	if _, err := appendRecord(s, rec("r3", "show clock", "succeeded", "", "x\n", 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(coll, "r3")); !os.IsNotExist(err) {
		t.Fatalf("r3 written without its end: %v", err)
	}
	entries, _ := os.ReadDir(coll)
	names := []string{}
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != "r1" || names[1] != "r2" {
		t.Fatalf("directory holds %v", names)
	}
	c := s.CollectionSummary()
	if c == nil || c.Directory != coll || c.Replaced != 1 || c.Kept != 2 || c.Devices["r1.example.net"] != (records.CollectionDevice{File: "r1", Outcome: "replaced"}) || c.Devices["r2"].Outcome != "kept" || c.Devices["r3"].Outcome != "kept" {
		t.Fatalf("summary %+v", c)
	}
	if got := CollectionLabel(c); got != " collection="+coll+" replaced=1 kept=2" {
		t.Fatalf("label %q", got)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if _, err := os.Stat(filepath.Join(dir, "job", "output.r1.txt")); !os.IsNotExist(err) {
		t.Fatal("a text file was written")
	}
}

// appendRecord appends r with its output where the record carries it, the
// form every test here needs.
func appendRecord(s *Store, r *records.CommandRecord) (Notice, error) {
	return s.AppendRecord(r, FromRecord(r))
}
