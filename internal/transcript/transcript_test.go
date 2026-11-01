package transcript

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/termtext"
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
	if err != nil || d.Root != filepath.Join(base, "transcripts") || d.Folder != filepath.Join(base, "transcripts", "260914") || d.Chosen {
		t.Fatalf("configured root: %+v %v", d, err)
	}
	ny, _ := time.LoadLocation("America/New_York")
	if d, err := Resolve("", "auto", "none", base, home, false, started, ny); err != nil || filepath.Base(d.Folder) != "260914" {
		t.Fatalf("configured root in the timezone: %+v %v", d, err)
	}
	// PATH is the folder itself, with no day folder.
	d, err = Resolve("~/rec", "auto", "none", base, home, false, started, ny)
	if err != nil || d.Root != filepath.Join(home, "rec") || d.Folder != d.Root || !d.Chosen {
		t.Fatalf("PATH with home: %+v %v", d, err)
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
	if d, err := Resolve(filepath.Join(home, "new"), "auto", "none", base, home, false, started, time.UTC); err != nil || d.Folder != filepath.Join(home, "new") {
		t.Fatalf("missing PATH: %+v %v", d, err)
	}
}

func TestClaimBumpsBothNames(t *testing.T) {
	root := t.TempDir()
	dest := Destination{Root: root, Folder: filepath.Join(root, "260914")}
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
	if fi, _ := os.Stat(dest.Folder); fi.Mode().Perm() != 0o750 {
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
	if err := os.WriteFile(filepath.Join(dest.Folder, "edge-143005.meta.jsonl"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	p4, err := Claim(dest, "edge", started, time.UTC, ".log", ".meta.jsonl", 0o750)
	if err != nil || filepath.Base(p4.Transcript) != "edge-143005.1.log" {
		t.Fatalf("metadata taken: %+v %v", p4, err)
	}
	if _, err := os.Stat(filepath.Join(dest.Folder, "edge-143005.log")); !os.IsNotExist(err) {
		t.Fatal("the unpaired transcript must be removed")
	}
}

// TestClaimInChosenFolder: --record=PATH records into PATH under the
// device's name, then the name with the session's start, then refuses.
func TestClaimInChosenFolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "rec")
	dest := Destination{Root: path, Folder: path, Chosen: true}
	ny, _ := time.LoadLocation("America/New_York")
	started := time.Date(2026, 10, 21, 2, 46, 52, 0, time.UTC) // 2026-10-20 22:46:52 in New York
	p1, err := Claim(dest, "Router1", started, ny, ".log", ".meta.jsonl", 0o750)
	if err != nil || p1.Transcript != filepath.Join(path, "router1.log") || p1.Metadata != filepath.Join(path, "router1.meta.jsonl") {
		t.Fatalf("first: %+v %v", p1, err)
	}
	for _, d := range []string{path, filepath.Dir(path)} {
		if fi, err := os.Stat(d); err != nil || fi.Mode().Perm() != 0o750 {
			t.Fatalf("%s: %v %v", d, fi, err)
		}
	}
	p2, err := Claim(dest, "router1", started, ny, ".log", ".meta.jsonl", 0o750)
	if err != nil || filepath.Base(p2.Transcript) != "router1-261020-224652.log" || filepath.Base(p2.Metadata) != "router1-261020-224652.meta.jsonl" {
		t.Fatalf("second: %+v %v", p2, err)
	}
	_, err = Claim(dest, "router1", started, ny, ".log", ".meta.jsonl", 0o750)
	if errorcodes.Of(err) != "transcript_name_taken" || !strings.Contains(err.Error(), path+"; retry\n  router1.log\n  router1-261020-224652.log") {
		t.Fatalf("third: %v", err)
	}
	// An operator's own file of the device's name moves the session to the
	// second name, the file untouched; a taken metadata name alone does too.
	own := t.TempDir()
	dest = Destination{Root: own, Folder: own, Chosen: true}
	if err := os.WriteFile(filepath.Join(own, "edge.log"), []byte("collected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p, err := Claim(dest, "edge", started, ny, ".log", ".meta.jsonl", 0o750); err != nil || filepath.Base(p.Transcript) != "edge-261020-224652.log" {
		t.Fatalf("own file: %+v %v", p, err)
	}
	if b, _ := os.ReadFile(filepath.Join(own, "edge.log")); string(b) != "collected\n" {
		t.Fatalf("the operator's file changed: %q", b)
	}
	if err := os.WriteFile(filepath.Join(own, "core.meta.jsonl"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if p, err := Claim(dest, "core", started, ny, ".log", ".meta.jsonl", 0o750); err != nil || filepath.Base(p.Metadata) != "core-261020-224652.meta.jsonl" {
		t.Fatalf("metadata taken: %+v %v", p, err)
	}
	if _, err := os.Stat(filepath.Join(own, "core.log")); !os.IsNotExist(err) {
		t.Fatal("the unpaired transcript must be removed")
	}
	// A taken metadata name at both names is named as taken.
	if err := os.WriteFile(filepath.Join(own, "core-261020-224653.meta.jsonl"), nil, 0o640); err != nil {
		t.Fatal(err)
	}
	_, err = Claim(dest, "core", started.Add(time.Second), ny, ".log", ".meta.jsonl", 0o750)
	if errorcodes.Of(err) != "transcript_name_taken" || !strings.HasSuffix(err.Error(), "\n  core.meta.jsonl\n  core-261020-224653.meta.jsonl") {
		t.Fatalf("both metadata names taken: %v", err)
	}
}

// claimed is a pair claimed for device in a folder of its own.
func claimed(t *testing.T, device, metaExt string) Pair {
	t.Helper()
	dir := t.TempDir()
	p, err := Claim(Destination{Root: dir, Folder: dir, Chosen: true}, device, time.Date(2026, 9, 14, 14, 30, 5, 0, time.UTC), time.UTC, ".log", metaExt, 0o750)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func sample() Metadata {
	key := "k"
	return Metadata{SessionID: "s1", OperatorUsername: "u", OperatorUID: 1000, InputTarget: "Router01", DeviceName: "router01", CanonicalName: "router01", Platform: "generic", Transport: "system", DispatchOrder: "shuffle", ShuffleKey: &key, CandidateCount: 2, TranscriptFile: "router01-143005.log", TranscriptFormat: "text", Rows: 40, Columns: 120, StartedAt: time.Date(2026, 9, 14, 14, 30, 5, 0, time.UTC), EndedAt: time.Date(2026, 9, 14, 14, 31, 0, 0, time.UTC), ExitClassification: "ExitSuccess", TranscriptSHA256: "ab", RecordingFailed: false}
}

func TestMetadataJSONL(t *testing.T) {
	p := claimed(t, "router01", ".meta.jsonl")
	path := p.Metadata
	m := sample()
	if err := p.WriteStart(FormatJSONL, m); err != nil {
		t.Fatal(err)
	}
	if IsEnd(path) {
		t.Fatal("start only is not ended")
	}
	if err := p.WriteEnd(FormatJSONL, m); err != nil {
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
	m := sample()
	m.ShuffleKey = nil
	jp := claimed(t, "router01", ".meta.json")
	jsonPath := jp.Metadata
	if err := jp.WriteStart(FormatJSON, m); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(jsonPath); len(data) != 0 || IsEnd(jsonPath) {
		t.Fatal("json metadata is written only at the end")
	}
	if err := jp.WriteEnd(FormatJSON, m); err != nil {
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
	tp := claimed(t, "router01", ".meta.txt")
	textPath := tp.Metadata
	if err := tp.WriteStart(FormatText, m); err != nil {
		t.Fatal(err)
	}
	if err := tp.WriteEnd(FormatText, m); err != nil {
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

func TestRender(t *testing.T) {
	for name, tc := range map[string]struct {
		in      string
		columns int
		resizes []termtext.Resize
		want    string
	}{
		"both":      {in: "Script started on 2026-09-14 [COMMAND=\"x\"]\nrouter01#\r\nok\r\n\nScript done on 2026-09-14 [COMMAND_EXIT_CODE=\"0\"]\n", want: "router01#\nok\n"},
		"header":    {in: "Script started on x\ndevice\n", want: "device\n"},
		"none":      {in: "device\n", want: "device\n"},
		"onlystart": {in: "Script started on x\n", want: ""},
		"nonewline": {in: "Script started on x\nprompt#\nScript done on y\n", want: "prompt#\n"},
		"crlf":      {in: "Script started on x\r\nout\r\n\nScript done on y\r\n", want: "out\n"},
		// the line editor's correction applied, the colours dropped
		"rendered": {in: "Script started on x\n\x1b[01;32m$\x1b[0m echo helo\b\x1b[K\b\x1b[Klo\r\nhelo\r\n\nScript done on y\n", columns: 80, want: "$ echo helo\nhelo\n"},
		// up a row in a line wrapped at 4, read at the width after the resize
		"resized": {in: "Script started on x\nabcdefghij\x1b[A\x1b[DX\r\n\nScript done on y\n", columns: 4, resizes: []termtext.Resize{{Offset: 10, Columns: 8}}, want: "aXcdefghij\n"},
	} {
		p := claimed(t, name, ".meta.jsonl")
		if err := os.WriteFile(p.Transcript, []byte(tc.in), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := p.Render(tc.columns, tc.resizes); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(p.Transcript)
		if string(got) != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
		if fi, _ := os.Stat(p.Transcript); fi.Mode().Perm() != 0o640 {
			t.Errorf("%s: mode %o", name, fi.Mode().Perm())
		}
		// The rendered file is the one the pair holds from here.
		if sum, err := p.TranscriptSHA256(); err != nil || len(sum) != 64 {
			t.Errorf("%s: digest after rendering: %q %v", name, sum, err)
		}
	}
	p := claimed(t, "missing", ".meta.jsonl")
	if err := os.Remove(p.Transcript); err != nil {
		t.Fatal(err)
	}
	if err := p.Render(80, nil); err == nil {
		t.Error("a missing transcript rendered")
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
	p := claimed(t, "r1", ".meta.jsonl")
	if err := os.WriteFile(p.Transcript, []byte("abc"), 0o640); err != nil {
		t.Fatal(err)
	}
	sum, err := p.TranscriptSHA256()
	if err != nil || sum != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%s %v", sum, err)
	}
}

// plantLink is a member of the tree's group at work in a day folder: path
// renamed aside when it exists, and a symbolic link to target in its place.
func plantLink(t *testing.T, path, target string) {
	t.Helper()
	if _, err := os.Lstat(path); err == nil {
		if err := os.Rename(path, path+".moved"); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// unchanged fails the test when a file of the operator's no longer holds
// want or a planted link is no longer a link.
func unchanged(t *testing.T, file, want, link string) {
	t.Helper()
	if b, err := os.ReadFile(file); err != nil || string(b) != want {
		t.Errorf("%s was written through the link: %q %v", file, b, err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is no longer the planted link: %v %v", link, fi, err)
	}
}

// secretKey is a file of the operator's that a planted link names.
const secretKey = "-----BEGIN OPENSSH PRIVATE KEY-----\r\nb3BlbnNzaC1rZXktdjE=\r\n"

// TestClaimPassesByALinkAtItsName: a link planted in the day folder at a
// session's name before the claim is a taken name, the link and its
// target untouched.
func TestClaimPassesByALinkAtItsName(t *testing.T) {
	root, private := t.TempDir(), t.TempDir()
	dest := Destination{Root: root, Folder: filepath.Join(root, "261101")}
	if err := os.Mkdir(dest.Folder, 0o770); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(private, "id_ed25519")
	if err := os.WriteFile(key, []byte(secretKey), 0o600); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	plantLink(t, filepath.Join(dest.Folder, "router1-120000.log"), key)
	plantLink(t, filepath.Join(dest.Folder, "router1-120000.1.meta.jsonl"), key)
	p, err := Claim(dest, "router1", started, time.UTC, ".log", ".meta.jsonl", 0o770)
	if err != nil || filepath.Base(p.Transcript) != "router1-120000.2.log" {
		t.Fatalf("claim: %+v %v", p, err)
	}
	unchanged(t, key, secretKey, filepath.Join(dest.Folder, "router1-120000.log"))
	unchanged(t, key, secretKey, filepath.Join(dest.Folder, "router1-120000.1.meta.jsonl"))
	if _, err := os.Lstat(filepath.Join(dest.Folder, "router1-120000.1.log")); !os.IsNotExist(err) {
		t.Fatalf("the unpaired transcript must be removed: %v", err)
	}
}

// TestALinkSwappedInAfterTheClaimIsRefused: after the claim another member
// renames the session's files aside and plants links to the operator's
// own files. The recorder's path still reaches the claimed transcript;
// the start record, the end's rendering, digest, and end record refuse the
// links with transcript_create_failed: nothing is read through them,
// nothing written, and the links stay links.
func TestALinkSwappedInAfterTheClaimIsRefused(t *testing.T) {
	for _, format := range []string{FormatJSONL, FormatText, FormatJSON} {
		t.Run(format, func(t *testing.T) {
			root, private := t.TempDir(), t.TempDir()
			dest := Destination{Root: root, Folder: filepath.Join(root, "261101")}
			started := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
			p, err := Claim(dest, "router1", started, time.UTC, ".log", MetadataExtension(format), 0o770)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close()
			key, profile := filepath.Join(private, "id_ed25519"), filepath.Join(private, ".profile")
			// The key's line ends are ones rendering changes, so a
			// rendering through the link would republish it.
			for f, b := range map[string]string{key: secretKey, profile: "umask 077\n"} {
				if err := os.WriteFile(f, []byte(b), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			plantLink(t, p.Transcript, key)
			plantLink(t, p.Metadata, profile)
			// script(1) opens the recorder's path for append, as here.
			rec, err := os.OpenFile(p.RecorderPath(), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := rec.WriteString("router1#show clock\r\n"); err != nil {
				t.Fatal(err)
			}
			rec.Close()
			if b, err := os.ReadFile(p.Transcript + ".moved"); err != nil || string(b) != "router1#show clock\r\n" {
				t.Errorf("the recorder's path: %q %v", b, err)
			}
			refused := func(what string, err error) {
				t.Helper()
				if errorcodes.Of(err) != "transcript_create_failed" || !errors.Is(err, ErrReplaced) {
					t.Errorf("%s: %v", what, err)
				}
			}
			if format != FormatJSON {
				refused("start record", p.WriteStart(format, sample()))
			}
			refused("render", p.Render(80, nil))
			_, err = p.TranscriptSHA256()
			refused("digest", err)
			refused("end record", p.WriteEnd(format, sample()))
			unchanged(t, key, secretKey, p.Transcript)
			unchanged(t, profile, "umask 077\n", p.Metadata)
		})
	}
}

// TestAFileSwappedInAfterTheClaimIsRefused: a regular file or a FIFO of
// another's in the session's place is not the session's file: refused, the
// file unchanged, and the FIFO's open never waits for a writer.
func TestAFileSwappedInAfterTheClaimIsRefused(t *testing.T) {
	p := claimed(t, "router1", ".meta.jsonl")
	if err := os.Rename(p.Transcript, p.Transcript+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(p.Transcript, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := p.Render(80, nil); !errors.Is(err, ErrReplaced) {
		t.Errorf("render of a FIFO: %v", err)
	}
	if err := os.Rename(p.Metadata, p.Metadata+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Metadata, []byte("planted\n"), 0o660); err != nil {
		t.Fatal(err)
	}
	if err := p.WriteEnd(FormatJSONL, sample()); !errors.Is(err, ErrReplaced) {
		t.Errorf("end record into another file: %v", err)
	}
	if b, _ := os.ReadFile(p.Metadata); string(b) != "planted\n" {
		t.Errorf("the planted file changed: %q", b)
	}
}

// TestTranscriptForADeviceNamedWithMeta: a device name holding ".meta."
// pairs its own transcript, and its transcript is not a metadata file.
func TestTranscriptForADeviceNamedWithMeta(t *testing.T) {
	dir := t.TempDir()
	stem := filepath.Join(dir, SafeName("Router1.meta.example.net")+"-010203")
	log := stem + ".log"
	if err := os.WriteFile(log, []byte("abc"), 0o640); err != nil {
		t.Fatal(err)
	}
	for _, ext := range []string{".meta.jsonl", ".meta.json", ".meta.txt"} {
		if got, ok := TranscriptFor(stem + ext); !ok || got != log {
			t.Errorf("%s: %q %v", ext, got, ok)
		}
	}
	if got, ok := TranscriptFor(log); ok {
		t.Errorf("the transcript is taken for metadata, paired with %q", got)
	}
}
