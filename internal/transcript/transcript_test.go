package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{"Router01": "router01", "core-1.example.net": "core-1.example.net", "sw 1/2": "sw_1_2", "10.0.0.1": "10.0.0.1", "": "device", "ü": "_"} {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q)=%q, want %q", in, got, want)
		}
	}
}

func TestResolveDestination(t *testing.T) {
	base, home := t.TempDir(), t.TempDir()
	started := time.Date(2026, 9, 14, 23, 30, 5, 0, time.UTC)
	d, err := Resolve("", "auto", "none", base, home, true, started, time.UTC)
	if err != nil || d.Root != filepath.Join(base, "transcripts") || d.Day != filepath.Join(base, "transcripts", "260914") {
		t.Fatalf("configured root: %+v %v", d, err)
	}
	ny, _ := time.LoadLocation("America/New_York")
	d, err = Resolve("~/rec", "auto", "none", base, home, false, started, ny)
	if err != nil || d.Root != filepath.Join(home, "rec") || filepath.Base(d.Day) != "260914" {
		t.Fatalf("PATH with home and timezone: %+v %v", d, err)
	}
	if _, err := Resolve("/x", "auto", "none", base, home, true, started, time.UTC); errorcodes.Of(err) != "transcript_root_locked" {
		t.Fatalf("locked root: %v", err)
	}
	file := filepath.Join(home, "session.log")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(file, "auto", "none", base, home, false, started, time.UTC); errorcodes.Of(err) != "transcript_path_not_directory" {
		t.Fatalf("PATH is a file: %v", err)
	}
	// A missing PATH is accepted; it is created at claim time.
	if d, err := Resolve(filepath.Join(home, "new"), "auto", "none", base, home, false, started, time.UTC); err != nil || d.Root != filepath.Join(home, "new") {
		t.Fatalf("missing PATH: %+v %v", d, err)
	}
}

func TestClaimBumpsBothNames(t *testing.T) {
	root := t.TempDir()
	dest := Destination{Root: root, Day: filepath.Join(root, "260914")}
	started := time.Date(2026, 9, 14, 14, 30, 5, 0, time.UTC)
	p1, err := Claim(dest, "Router01", started, time.UTC, ".log", ".meta.jsonl", 0o750)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p1.Transcript) != "router01-143005.log" || filepath.Base(p1.Metadata) != "router01-143005.meta.jsonl" {
		t.Fatalf("names: %+v", p1)
	}
	for _, f := range []string{p1.Transcript, p1.Metadata} {
		fi, err := os.Stat(f)
		if err != nil || fi.Mode().Perm() != 0o640 || fi.Size() != 0 {
			t.Fatalf("%s: %v %v", f, fi, err)
		}
	}
	if fi, _ := os.Stat(dest.Day); fi.Mode().Perm() != 0o750 {
		t.Fatalf("day folder mode %o", fi.Mode().Perm())
	}
	// The same second again, and a device whose name collapses to the same
	// safe name, get .1 and .2 for both files.
	p2, err := Claim(dest, "router01", started, time.UTC, ".log", ".meta.jsonl", 0o750)
	if err != nil || filepath.Base(p2.Transcript) != "router01-143005.1.log" || filepath.Base(p2.Metadata) != "router01-143005.1.meta.jsonl" {
		t.Fatalf("bump: %+v %v", p2, err)
	}
	p3, err := Claim(dest, "ROUTER01", started, time.UTC, ".log", ".meta.jsonl", 0o750)
	if err != nil || filepath.Base(p3.Transcript) != "router01-143005.2.log" {
		t.Fatalf("bump 2: %+v %v", p3, err)
	}
	// A taken metadata name alone bumps both.
	if err := os.WriteFile(filepath.Join(dest.Day, "edge-143005.meta.jsonl"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	p4, err := Claim(dest, "edge", started, time.UTC, ".log", ".meta.jsonl", 0o750)
	if err != nil || filepath.Base(p4.Transcript) != "edge-143005.1.log" {
		t.Fatalf("metadata taken: %+v %v", p4, err)
	}
	if _, err := os.Stat(filepath.Join(dest.Day, "edge-143005.log")); !os.IsNotExist(err) {
		t.Fatal("the unpaired transcript must be removed")
	}
}

func sample() Metadata {
	key := "k"
	return Metadata{SessionID: "s1", OperatorUsername: "u", OperatorUID: 1000, InputTarget: "Router01", DeviceName: "router01", CanonicalName: "router01", Platform: "generic", Transport: "system", DispatchOrder: "shuffle", ShuffleKey: &key, CandidateCount: 2, TranscriptFile: "router01-143005.log", TranscriptFormat: "text", Rows: 40, Columns: 120, StartedAt: time.Date(2026, 9, 14, 14, 30, 5, 0, time.UTC), EndedAt: time.Date(2026, 9, 14, 14, 31, 0, 0, time.UTC), ExitClassification: "ExitSuccess", TranscriptSHA256: "ab", RecordingFailed: false}
}

func TestMetadataJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.meta.jsonl")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	m := sample()
	if err := WriteStart(path, FormatJSONL, m); err != nil {
		t.Fatal(err)
	}
	if IsEnd(path) {
		t.Fatal("start only is not ended")
	}
	if err := WriteEnd(path, FormatJSONL, m); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines: %q", lines)
	}
	var start, end map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &start); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &end); err != nil {
		t.Fatal(err)
	}
	if start["record"] != "start" || start["schema_version"].(float64) != 1 || start["session_id"] != "s1" || start["candidate_count"].(float64) != 2 || start["shuffle_key"] != "k" || start["terminal"].(map[string]any)["columns"].(float64) != 120 {
		t.Fatalf("start: %v", start)
	}
	if _, ok := start["ended_at"]; ok {
		t.Fatal("start must not carry end fields")
	}
	if end["record"] != "end" || end["ended_at"] != "2026-09-14T14:31:00Z" || end["exit_classification"] != "ExitSuccess" || end["transcript_sha256"] != "ab" || end["recording_failed"] != false {
		t.Fatalf("end: %v", end)
	}
	if !strings.HasPrefix(lines[0], `{"schema_version":1,"record":"start",`) {
		t.Fatalf("field order: %s", lines[0])
	}
	if !IsEnd(path) {
		t.Fatal("IsEnd")
	}
	if at, ok := EndedAt(path); !ok || !at.Equal(m.EndedAt) {
		t.Fatalf("EndedAt: %v %v", at, ok)
	}
}

func TestMetadataJSONAndText(t *testing.T) {
	dir := t.TempDir()
	m := sample()
	m.ShuffleKey = nil
	jsonPath := filepath.Join(dir, "m.meta.json")
	if err := os.WriteFile(jsonPath, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteStart(jsonPath, FormatJSON, m); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(jsonPath); len(data) != 0 || IsEnd(jsonPath) {
		t.Fatal("json metadata is written only at the end")
	}
	if err := WriteEnd(jsonPath, FormatJSON, m); err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	data, _ := os.ReadFile(jsonPath)
	if err := json.Unmarshal(data, &doc); err != nil || doc["record"] != "end" || !IsEnd(jsonPath) {
		t.Fatalf("json end: %v %v", err, doc)
	}
	if _, ok := doc["shuffle_key"]; ok {
		t.Fatal("shuffle_key must be absent for default order")
	}
	if fi, _ := os.Stat(jsonPath); fi.Mode().Perm() != 0o640 {
		t.Fatalf("json mode %o", fi.Mode().Perm())
	}
	textPath := filepath.Join(dir, "m.meta.txt")
	if err := os.WriteFile(textPath, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := WriteStart(textPath, FormatText, m); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnd(textPath, FormatText, m); err != nil {
		t.Fatal(err)
	}
	text, _ := os.ReadFile(textPath)
	for _, want := range []string{"record: start\n", "session_id: s1\n", "device.canonical_name: router01\n", "terminal.rows: 40\n", "started_at: 2026-09-14T14:30:05Z\n", "ended_at: 2026-09-14T14:31:00Z\n", "recording_failed: false\n"} {
		if !strings.Contains(string(text), want) {
			t.Fatalf("text lacks %q:\n%s", want, text)
		}
	}
	if strings.Count(string(text), "session_id:") != 1 {
		t.Fatalf("end appends only end fields:\n%s", text)
	}
	if !IsEnd(textPath) {
		t.Fatal("IsEnd text")
	}
	if at, ok := EndedAt(textPath); !ok || !at.Equal(m.EndedAt) {
		t.Fatalf("EndedAt text: %v %v", at, ok)
	}
}

func TestStripScriptMarkers(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct{ in, want string }{
		"both":      {"Script started on 2026-09-14 [COMMAND=\"x\"]\nrouter01#\r\nok\r\n\nScript done on 2026-09-14 [COMMAND_EXIT_CODE=\"0\"]\n", "router01#\r\nok\r\n"},
		"header":    {"Script started on x\ndevice\n", "device\n"},
		"none":      {"device\n", "device\n"},
		"onlystart": {"Script started on x\n", ""},
		"nonewline": {"Script started on x\nprompt#\nScript done on y\n", "prompt#"},
		"crlf":      {"Script started on x\r\nout\r\n\nScript done on y\r\n", "out\r\n"},
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(tc.in), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := StripScriptMarkers(p); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p)
		if string(got) != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o640 {
			t.Errorf("%s: mode %o", name, fi.Mode().Perm())
		}
	}
}

func TestTranscriptForAndSHA(t *testing.T) {
	dir := t.TempDir()
	meta := filepath.Join(dir, "r1-010203.1.meta.jsonl")
	if _, ok := TranscriptFor(meta); ok {
		t.Fatal("no transcript yet")
	}
	log := filepath.Join(dir, "r1-010203.1.log")
	if err := os.WriteFile(log, []byte("abc"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, ok := TranscriptFor(meta); !ok || got != log {
		t.Fatalf("%q %v", got, ok)
	}
	sum, err := FileSHA256(log)
	if err != nil || sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%s %v", sum, err)
	}
}
