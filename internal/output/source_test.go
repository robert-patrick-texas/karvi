package output

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// spoolOf writes raw to a spool file and returns its source with the
// reader's digest and the encoding the record takes.
func spoolOf(t *testing.T, raw []byte) (Source, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "a.d.1.1.spool")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, enc, digest := EncodeOutput(raw)
	return FromSpool(path, int64(len(raw)), digest, enc), enc
}

// TestStreamedLineFromASpoolEqualsTheLineFromTheString: a record whose
// output is in its spool file writes
// the same line as the record carrying the output as its string, utf-8
// (escaped a chunk at a time, characters across every chunk border) and
// base64 (the file streamed through the encoder) alike; the measuring pass
// verifies the file against the reader's digest.
func TestStreamedLineFromASpoolEqualsTheLineFromTheString(t *testing.T) {
	filler := strings.Repeat("0123456789 line of output\n", 5000)
	raws := map[string][]byte{
		"plain":      []byte(filler),
		"escapes":    []byte(filler + "\"quoted\" back\\slash \t tab \b \f \x01 <html> & \x7f" + filler),
		"multi-byte": []byte(strings.Repeat("é", lineChunk) + strings.Repeat("€", lineChunk) + strings.Repeat("😀", lineChunk)),
		"small":      []byte("ok\n"),
		"binary":     append([]byte(filler), 0xff, 0xfe, '\n'),
		"binary-big": bytes.Repeat([]byte{0xe2, 0x82, 0xac, 0xff}, lineChunk),
	}
	for shift := 0; shift < 5; shift++ {
		raws["border"+string(rune('0'+shift))] = []byte(strings.Repeat("a", lineChunk-shift) + strings.Repeat("😀é€", 20000))
	}
	for name, raw := range raws {
		text, enc, digest := EncodeOutput(raw)
		fromString := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show <big> & \"more\"", Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest, Prompt: "router#"}
		want, err := json.Marshal(&fromString)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')

		src, srcEnc := spoolOf(t, raw)
		if srcEnc != enc {
			t.Fatalf("%s: the spool's encoding %s, the record's %s", name, srcEnc, enc)
		}
		fromSpool := fromString
		fromSpool.Output = ""
		line, err := newRecordLine(&fromSpool, src)
		if err != nil {
			t.Fatal(err)
		}
		if line.whole != nil {
			t.Fatalf("%s: a spooled output was not written in pieces", name)
		}
		length, err := line.measure()
		if err != nil {
			t.Fatalf("%s: measure: %v", name, err)
		}
		var got bytes.Buffer
		n, err := line.WriteTo(&got)
		if err != nil || n != int64(got.Len()) || n != length {
			t.Fatalf("%s: WriteTo = %d, %v; wrote %d; measured %d", name, n, err, got.Len(), length)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("%s: the line from the spool differs from the line from the string (%d and %d bytes)", name, got.Len(), len(want))
		}
		// The raw bytes reach the text block as the device sent them.
		var raw2 bytes.Buffer
		if err := src.WriteRaw(&raw2); err != nil || !bytes.Equal(raw2.Bytes(), raw) {
			t.Fatalf("%s: WriteRaw gave %d bytes (%v), want %d", name, raw2.Len(), err, len(raw))
		}
		var fromText bytes.Buffer
		if err := FromRecord(&fromString).WriteRaw(&fromText); err != nil || !bytes.Equal(fromText.Bytes(), raw) {
			t.Fatalf("%s: the string's WriteRaw gave %d bytes (%v), want %d", name, fromText.Len(), err, len(raw))
		}
	}
}

// TestSpoolMismatchRefusesTheRecord: a spool whose bytes
// are not the reader's is output_spool_mismatch on the measuring pass, and
// nothing of the line reaches commands.jsonl; the device is listed for a
// rerun.
func TestSpoolMismatchRefusesTheRecord(t *testing.T) {
	raw := []byte("the device said this\n")
	src, enc := spoolOf(t, raw)
	other := sha256.Sum256([]byte("the session read that\n"))
	src.digest = hex.EncodeToString(other[:])
	s, err := Create(Options{Root: t.TempDir(), ID: "id"})
	if err != nil {
		t.Fatal(err)
	}
	r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", ActivityType: "command", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "d", Name: "d", CanonicalName: "d", Groups: []string{}}, InputTarget: "d", TransformedName: "d", DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "generic", Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: 1, CommandCount: 1, CommandKind: "requested", Command: "x", CommandSHA256: "x", Status: "succeeded", OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: src.digest, Notices: []records.Notice{}}
	_, err = s.AppendRecord(&r, src)
	if errorcodes.Of(err) != "output_spool_mismatch" || !strings.Contains(err.Error(), src.path) {
		t.Fatalf("append = %v", err)
	}
	if s.Err() == nil {
		t.Fatal("the store does not report the failed append")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(s.Paths().CommandsJSONL); len(data) != 0 {
		t.Fatalf("commands.jsonl holds %d bytes after the refusal", len(data))
	}
	if data, _ := os.ReadFile(s.Paths().FailedDevices); string(data) != "d\n" {
		t.Fatalf("failed-devices.txt %q", data)
	}
	// The spool is the executor's to remove: it is still there.
	if _, err := os.Stat(src.path); err != nil {
		t.Fatalf("the store removed the spool: %v", err)
	}
}

// TestSpooledRecordFilesEqualTheStringForm is the spooled record at the store:
// the commands.jsonl line, the failures.jsonl line, the text block, and
// the collection block of a spooled record are those of the same record
// carrying its output as a string; the record leaves the append with its
// Output still empty (the renderer and the follower take the
// source and the file), and the notices carry each line's offset.
func TestSpooledRecordFilesEqualTheStringForm(t *testing.T) {
	raw := []byte("Building configuration...\n\nCurrent configuration : 12 bytes\nhostname r1\nend\n")
	base := func() records.CommandRecord {
		return records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r", ActivityID: "a", ActivityType: "command", Operator: records.Operator{Username: "u"}, Device: records.DeviceProjection{ID: "d", Name: "d", CanonicalName: "d", Groups: []string{}}, InputTarget: "d", TransformedName: "d", DNSSuffixAction: "add-suffix:none", AddressCandidates: []string{}, Platform: "generic", Transport: "system", Port: 22, Dispatch: records.DispatchContext{Mode: "serial"}, CommandIndex: 1, CommandCount: 1, CommandKind: "requested", Command: "show running-config", CommandSHA256: "x", Status: "timeout", PromptBefore: "r1#", OutputEncoding: "utf-8", OutputBytes: int64(len(raw)), Notices: []records.Notice{}, Error: &records.StructuredError{Code: "command_timeout", Category: "timeout", Message: "cut"}}
	}
	run := func(spooled bool) (jsonl, failures, text, collected string, filled string) {
		dir := t.TempDir()
		coll := filepath.Join(dir, "coll")
		os.MkdirAll(coll, 0o700)
		s, err := Create(Options{Root: filepath.Join(dir, "job"), ID: "id", Collection: &CollectionOptions{Directory: coll, FileMode: 0o640}})
		if err != nil {
			t.Fatal(err)
		}
		r := base()
		var src Source
		if spooled {
			src, _ = spoolOf(t, raw)
			r.OutputSHA256 = src.digest
		} else {
			r.Output, _, r.OutputSHA256 = EncodeOutput(raw)
			src = FromRecord(&r)
		}
		first, err := s.AppendRecord(&r, src)
		if err != nil {
			t.Fatal(err)
		}
		if first.LineOffset != 0 {
			t.Fatalf("the first line's offset is %d", first.LineOffset)
		}
		// The collection keeps a device whose every record passed; the
		// timeout's device is a failure, so a second device's success gives
		// the collection block.
		ok := base()
		ok.RecordID, ok.Status, ok.Error, ok.OutputSHA256 = "r2", "succeeded", nil, r.OutputSHA256
		ok.Device = records.DeviceProjection{ID: "e", Name: "e", CanonicalName: "e", Groups: []string{}}
		ok.InputTarget, ok.TransformedName = "e", "e"
		var second Notice
		if spooled {
			okSrc, _ := spoolOf(t, raw)
			if second, err = s.AppendRecord(&ok, okSrc); err != nil {
				t.Fatal(err)
			}
		} else {
			ok.Output = r.Output
			if second, err = s.AppendRecord(&ok, FromRecord(&ok)); err != nil {
				t.Fatal(err)
			}
		}
		if second.LineOffset != first.LineLength || second.LineLength <= 0 {
			t.Fatalf("the second line's offset is %d, the first's length %d", second.LineOffset, first.LineLength)
		}
		s.EndDevice("d")
		s.EndDevice("e")
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
		return read(s.Paths().CommandsJSONL), read(s.Paths().FailuresJSONL), read(filepath.Join(s.Paths().Root, TextFileName("d", false))), read(filepath.Join(coll, "e")), r.Output
	}
	j1, f1, t1, c1, _ := run(false)
	j2, f2, t2, c2, filled := run(true)
	// The header's time differs between the two runs; compare from the
	// first block on.
	after := func(s string) string { i := strings.Index(s, "r1#show"); return s[i:] }
	if j1 != j2 || f1 != f2 || after(t1) != after(t2) || c1 != c2 {
		t.Fatalf("the spooled record's files differ from the string form's:\ncommands equal %v\nfailures equal %v\ntext equal %v\ncollection equal %v", j1 == j2, f1 == f2, after(t1) == after(t2), c1 == c2)
	}
	if filled != "" {
		t.Fatalf("after the append the spooled record's Output is %q, want it empty", filled)
	}
	if !strings.Contains(c2, "hostname r1") || !strings.Contains(j2, `"output":"Building`) {
		t.Fatalf("the files do not hold the output: collection %q", c2)
	}
}
