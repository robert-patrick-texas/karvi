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

// TestLineFilterMasks covers the mask at the writer: a line kept is masked
// without its terminator and a trailing CR, both kept in the bytes written,
// whatever the chunks that carried it; a dropped line is not masked but
// left out; an unterminated last line is masked at the flush.
func TestLineFilterMasks(t *testing.T) {
	var out bytes.Buffer
	f := &lineFilter{w: &out, res: []*regexp.Regexp{regexp.MustCompile(` uptime is `)}, masks: []*regexp.Regexp{regexp.MustCompile(`(?i)^\s*snmp-s\S*\s+comm\S*\s+(\S+)`), regexp.MustCompile(`key (\S+)`)}}
	for _, chunk := range []string{"snmp-server comm", "unity Publ1c RO\r\nhostname r1\nr1 uptime is key K1 1 day\n ke", "y K2"} {
		if n, err := f.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("write %q: %d %v", chunk, n, err)
		}
	}
	if err := f.flush(); err != nil {
		t.Fatal(err)
	}
	if want := "snmp-server community <removed> RO\r\nhostname r1\n key <removed>"; out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

// TestCollectionMasks covers the mask through the store: under crun.mask
// (Masks given) each device's file is masked by its own platform's list,
// the marker's statement and the output lines, on the plain and the base64
// path, after the drop list; a platform absent from the map is written
// whole; without Masks the file is as the device sent it; a list that does
// not compile or holds no group refuses the store as the plan's fault.
func TestCollectionMasks(t *testing.T) {
	dir := t.TempDir()
	coll := filepath.Join(dir, "crun")
	if err := os.MkdirAll(coll, 0o770); err != nil {
		t.Fatal(err)
	}
	rec := func(device, platform, command, out, encoding string, index int) *records.CommandRecord {
		return &records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "261017-020000-00", JobID: "261017-020000-00", ActivityType: "run", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "name:" + device, Name: device, CanonicalName: device, Groups: []string{}}, InputTarget: device, TransformedName: device, DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: platform, Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: index, CommandCount: 2, CommandKind: "requested", Command: command, CommandSHA256: "x", Status: "success", Output: out, OutputEncoding: encoding, OutputSHA256: "x", Notices: []records.Notice{}, Timing: records.Timing{QueuedAt: time.Now(), EndedAt: time.Now()}}
	}
	iosxe := []string{`(?i)^\s*(?:no\s+)?snmp-s\S*\s+comm\S*\s+(\S+)`, `(?i)\bkey-string\s+(?:\d\s+)?(\S+)`}
	junos := []string{`(?i)\bsnmp\s+community\s+("[^"]*"|\S+)`}
	config := "Building configuration...\nsnmp-server community Publ1c RO\nkey chain K\n key 1\n  key-string 0 ChainKey\nset snmp community Publ1c\nend\n"
	write := func(id string, masks map[string][]string, device, platform string) string {
		t.Helper()
		s, err := Create(Options{Root: filepath.Join(dir, id), ID: id, Skip: FileSet{OutputTxt: true},
			Collection: &CollectionOptions{Directory: coll, FileMode: 0o660, Filters: map[string][]string{"cisco_iosxe": {`^Building configuration\.\.\.$`}}, Masks: masks}, Warn: func(string) {}})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range []*records.CommandRecord{
			rec(device, platform, "snmp-server community Publ1c RO", "", "utf-8", 1),
			rec(device, platform, "show running-config", base64.StdEncoding.EncodeToString([]byte(config)), "base64", 2),
		} {
			if _, err := appendRecord(s, r); err != nil {
				t.Fatal(err)
			}
		}
		s.EndDevice(device)
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(coll, device))
		return string(got)
	}
	lists := map[string][]string{"cisco_iosxe": iosxe, "juniper_junos": junos}
	want := "! snmp-server community <removed> RO\n\n! show running-config\nsnmp-server community <removed> RO\nkey chain K\n key 1\n  key-string 0 <removed>\nset snmp community Publ1c\nend\n"
	if got := write("261017-020000-00", lists, "r1", "cisco_iosxe"); got != want {
		t.Fatalf("r1 masked by its own list:\n%s\nwant:\n%s", got, want)
	}
	want = "! snmp-server community Publ1c RO\n\n! show running-config\n" + strings.TrimPrefix(config, "Building configuration...\n")
	if got := write("261017-020001-00", nil, "r2", "cisco_iosxe"); got != want {
		t.Fatalf("r2 without crun.mask:\n%s\nwant:\n%s", got, want)
	}
	want = "! snmp-server community Publ1c RO\n\n! show running-config\n" + config
	if got := write("261017-020002-00", map[string][]string{"cisco_iosxe": iosxe}, "g1", "generic"); got != want {
		t.Fatalf("g1, a platform with no list:\n%s\nwant:\n%s", got, want)
	}
	for _, bad := range []string{"(", `^key \S+`} {
		_, err := Create(Options{Root: filepath.Join(dir, "job2"), ID: "261017-020003-00", Skip: FileSet{OutputTxt: true},
			Collection: &CollectionOptions{Directory: coll, FileMode: 0o660, Masks: map[string][]string{"cisco_iosxe": {bad}}}, Warn: func(string) {}})
		if err == nil || !strings.Contains(err.Error(), "execution_plan_invalid") || !strings.Contains(err.Error(), "platform_masks") || !strings.Contains(err.Error(), "pattern 1") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}
