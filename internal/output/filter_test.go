package output

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestLineFilter covers the collection file's line filter at the writer:
// a line is matched without its terminator and without a trailing CR,
// whatever the chunks that carried it; a last line with no terminator is
// judged at the flush; nothing else is touched.
func TestLineFilter(t *testing.T) {
	res := []*regexp.Regexp{regexp.MustCompile(`^Building configuration\.\.\.$`), regexp.MustCompile(` uptime is `)}
	var out bytes.Buffer
	f := &lineFilter{w: &out, res: res}
	for _, chunk := range []string{"Building conf", "iguration...\r\nhostname r1\nr1 upt", "ime is 1 day\n!\nend"} {
		if n, err := f.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %q: %d %v", chunk, n, err)
		}
	}
	if out.String() != "hostname r1\n!\n" {
		t.Fatalf("before the flush: %q", out.String())
	}
	if err := f.flush(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "hostname r1\n!\nend" {
		t.Fatalf("after the flush: %q", out.String())
	}
	out.Reset()
	f = &lineFilter{w: &out, res: res}
	f.Write([]byte("r1 uptime is 2 days"))
	f.flush()
	if out.Len() != 0 {
		t.Fatalf("an unterminated last line is judged too: %q", out.String())
	}
}

// TestCollectionFilters covers 12.19 rules 2 and 3 through the store: the
// plan's list for the record's platform drops matching output lines from
// the collection file, on the plain and the base64 path, a platform absent
// from the map is written whole, an emptied block keeps its marker, and a
// list that does not compile refuses the store as the plan's fault.
func TestCollectionFilters(t *testing.T) {
	dir := t.TempDir()
	coll := filepath.Join(dir, "crun")
	if err := os.MkdirAll(coll, 0o770); err != nil {
		t.Fatal(err)
	}
	filters := map[string][]string{"cisco_iosxe": {`^Building configuration\.\.\.$`, `^Current configuration : \d+ bytes$`, `^ntp clock-period \d+$`, ` uptime is `}, "cisco_nxos": {`^!Time: `}}
	s, err := Create(Options{Root: filepath.Join(dir, "job"), ID: "260926-020000-00", Skip: FileSet{OutputTxt: true}, CropNames: true,
		Collection: &CollectionOptions{Directory: coll, FileMode: 0o660, Filters: filters}, Warn: func(string) {}})
	if err != nil {
		t.Fatal(err)
	}
	rec := func(device, platform, command, out, encoding string, index int) *records.CommandRecord {
		return &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "260926-020000-00", JobID: "260926-020000-00", ActivityType: "run", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "name:" + device, Name: device, CanonicalName: device, Groups: []string{}}, InputTarget: device, TransformedName: device, DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: platform, Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: index, CommandCount: 2, CommandKind: "requested", Command: command, CommandSHA256: "x", Status: "succeeded", Output: out, OutputEncoding: encoding, OutputSHA256: "x", Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: time.Now(), EndedAt: time.Now()}}
	}
	config := "Building configuration...\n\nCurrent configuration : 512 bytes\n!\n! Last configuration change at 10:00:00 UTC Tue Sep 15 2026\n!\nhostname r1\n!\nntp clock-period 17179869\nend\n"
	version := "Cisco IOS XE Software, Version 17.09.04a\nr1 uptime is 1 day\n"
	for _, r := range []*records.CommandRecord{
		rec("r1", "cisco_iosxe", "show running-config", config, "utf-8", 1),
		rec("r1", "cisco_iosxe", "show version", base64.StdEncoding.EncodeToString([]byte(version)), "base64", 2),
	} {
		if _, err := appendRecord(s, r); err != nil {
			t.Fatal(err)
		}
	}
	s.EndDevice("r1")
	got, _ := os.ReadFile(filepath.Join(coll, "r1"))
	want := "! show running-config\n\n!\n! Last configuration change at 10:00:00 UTC Tue Sep 15 2026\n!\nhostname r1\n!\nend\n\n! show version\nCisco IOS XE Software, Version 17.09.04a\n"
	if string(got) != want {
		t.Fatalf("r1:\n%s\nwant:\n%s", got, want)
	}
	// generic: no list in the map, written whole.
	if _, err := appendRecord(s, rec("g1", "generic", "show version", version, "utf-8", 1)); err != nil {
		t.Fatal(err)
	}
	s.EndDevice("g1")
	if got, _ := os.ReadFile(filepath.Join(coll, "g1")); string(got) != "! show version\n"+version {
		t.Fatalf("g1: %q", got)
	}
	// nxos: a block whose every line is dropped keeps its marker.
	if _, err := appendRecord(s, rec("n1", "cisco_nxos", "show clock", "!Time: Fri Sep 26 10:00:00 2026\n", "utf-8", 1)); err != nil {
		t.Fatal(err)
	}
	s.EndDevice("n1")
	if got, _ := os.ReadFile(filepath.Join(coll, "n1")); string(got) != "! show clock\n" {
		t.Fatalf("n1: %q", got)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Create(Options{Root: filepath.Join(dir, "job2"), ID: "260926-020001-00", Skip: FileSet{OutputTxt: true},
		Collection: &CollectionOptions{Directory: coll, FileMode: 0o660, Filters: map[string][]string{"cisco_iosxe": {"("}}}, Warn: func(string) {}})
	if err == nil || !strings.Contains(err.Error(), "execution_plan_invalid") || !strings.Contains(err.Error(), "pattern 1") {
		t.Fatalf("a list that does not compile: %v", err)
	}
}
