package output

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// withReadbackBuffer runs the test with the read-back's buffer at size.
func withReadbackBuffer(t *testing.T, size int) {
	t.Helper()
	was := readbackBuffer
	readbackBuffer = size
	t.Cleanup(func() { readbackBuffer = was })
}

// esc is text with each "%" a backslash: a test's JSON escapes are built
// here, never written in the source as they are, so nothing that writes
// the file can turn one into its character.
func esc(text string) string { return strings.ReplaceAll(text, "%", "\x5c") }

// readbackCase is one record of a test commands.jsonl: the record with its
// streams as strings (whole), the record and source its line is written
// from (as the executor hands them to the store), and the device's bytes.
type readbackCase struct {
	name           string
	whole, rec     records.CommandRecord
	src            Source
	stdout, stderr []byte
	exec           bool
}

func readbackCases(t *testing.T) []readbackCase {
	t.Helper()
	every := make([]byte, 0, 0x80)
	for b := 0; b < 0x80; b++ {
		every = append(every, byte(b))
	}
	every = append(every, "<html> &     é€😀 \"output\":\"x\" \\u0041 \\"...)
	filler := strings.Repeat("0123456789 line of output\n", 4000)
	big := []byte(filler + string(every) + filler)
	shell := []struct {
		name string
		raw  []byte
	}{
		{"empty", nil},
		{"small", []byte("ok\n")},
		{"every-byte", every},
		{"big", big},
		{"multi-byte", []byte(strings.Repeat("😀é€", 30000))},
		{"binary-small", []byte{0xff, 0xfe, '\n'}},
		{"binary-big", bytes.Repeat([]byte{0xe2, 0x82, 0xac, 0xff}, 40000)},
		{"at-the-bound", []byte(strings.Repeat("a", streamedLineThreshold))},
		{"past-the-bound", []byte(strings.Repeat("a", streamedLineThreshold+1))},
	}
	var cases []readbackCase
	for _, s := range shell {
		text, enc, digest := EncodeOutput(s.raw)
		whole := records.CommandRecord{SchemaVersion: 2, RecordID: "r-" + s.name, Command: `show "output":"x" <big> & more`, Output: text, OutputEncoding: enc, OutputBytes: int64(len(s.raw)), OutputSHA256: digest, Prompt: "router#",
			// Brackets, quotes, and backslashes inside nested strings, and
			// nested fields named output and stderr, which stay in the record.
			Notices: []records.Notice{{Code: "x", Message: `}]{[ "quoted" \ "}"`, Details: map[string]any{"output": "nested ]}", "stderr": []any{"[", map[string]any{"k": `"}`}}}}},
			Error:   &records.StructuredError{Code: "c", Category: "device", Message: `a "message" with ]} and \`, Details: map[string]any{"list": []any{1, "]", 2.5, nil, true}}}}
		cases = append(cases, readbackCase{name: "string/" + s.name, whole: whole, rec: whole, src: FromRecord(&whole), stdout: s.raw})
		if len(s.raw) > 0 {
			spooled := whole
			spooled.Output = ""
			src, _ := spoolOf(t, s.raw)
			cases = append(cases, readbackCase{name: "spool/" + s.name, whole: whole, rec: spooled, src: src, stdout: s.raw})
		}
	}
	smallErr := []byte("ls: cannot access '/nonexistent'\n")
	binaryErr := append(append([]byte{}, big...), 0xff, '\n')
	for _, e := range []struct {
		name               string
		stdout, stderr     []byte
		spoolOut, spoolErr bool
	}{
		{"small-small", []byte("x\n"), smallErr, false, false},
		{"big-small", big, smallErr, false, false},
		{"small-big", []byte("x\n"), big, false, false},
		{"big-binary-spooled", big, binaryErr, true, true},
		{"small-binary-spooled", []byte("x\n"), binaryErr, false, true},
		{"empty-empty", nil, nil, false, false},
	} {
		whole, spooled, src := execRecord(t, e.stdout, e.stderr, e.spoolOut, e.spoolErr)
		cases = append(cases, readbackCase{name: "exec/" + e.name, whole: whole, rec: spooled, src: src, stdout: e.stdout, stderr: e.stderr, exec: true})
	}
	return cases
}

// TestReadRecordAtGivesBackTheLine: each line of a commands.jsonl the
// store's writer made (a shell record's output as a string or in a spool,
// utf-8 or base64, small or past the 64 KiB bound; an exec record's stdout
// and stderr in every form) is found by its number, read back with its
// device's bytes as they were, the record its own but a stream past the
// bound (left in the file, the source streaming it), and written again
// from the record and the source to the line's own bytes; with a buffer of
// 16 bytes, so every escape and character crosses its border, and the
// default.
func TestReadRecordAtGivesBackTheLine(t *testing.T) {
	for _, size := range []int{16, 32 << 10} {
		withReadbackBuffer(t, size)
		cases := readbackCases(t)
		path := filepath.Join(t.TempDir(), "commands.jsonl")
		var file bytes.Buffer
		lines := make([][]byte, len(cases))
		var want []int64
		for i, c := range cases {
			var line bytes.Buffer
			if _, err := WriteRecordLine(&line, &c.rec, c.src); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			lines[i] = line.Bytes()
			file.Write(lines[i])
			want = append(want, int64(i+1))
		}
		if err := os.WriteFile(path, file.Bytes(), 0o640); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		spans, err := FindLines(f, path, want)
		if err != nil {
			t.Fatal(err)
		}
		var off int64
		for i, c := range cases {
			seq := int64(i + 1)
			if got, wantSpan := spans[seq], (LineSpan{Offset: off, Length: int64(len(lines[i]))}); got != wantSpan {
				t.Fatalf("%s: span %+v, want %+v", c.name, got, wantSpan)
			}
			off += int64(len(lines[i]))
			rec, src, err := ReadRecordAt(f, path, seq, spans[seq])
			if err != nil {
				t.Fatalf("%s (buffer %d): %v", c.name, size, err)
			}
			var out bytes.Buffer
			if err := src.WriteRaw(&out); err != nil {
				t.Fatalf("%s: WriteRaw: %v", c.name, err)
			}
			if !bytes.Equal(out.Bytes(), c.stdout) {
				t.Fatalf("%s (buffer %d): the output read back differs (%d bytes, want %d)", c.name, size, out.Len(), len(c.stdout))
			}
			stderr, ok := src.Stderr()
			if ok != c.exec {
				t.Fatalf("%s: stderr %v, want %v", c.name, ok, c.exec)
			}
			if ok {
				var errOut bytes.Buffer
				if err := stderr.WriteRaw(&errOut); err != nil {
					t.Fatalf("%s: stderr WriteRaw: %v", c.name, err)
				}
				if !bytes.Equal(errOut.Bytes(), c.stderr) {
					t.Fatalf("%s: the stderr read back differs (%d bytes, want %d)", c.name, errOut.Len(), len(c.stderr))
				}
			}
			var again bytes.Buffer
			if _, err := WriteRecordLine(&again, &rec, src); err != nil {
				t.Fatalf("%s: WriteRecordLine: %v", c.name, err)
			}
			if !bytes.Equal(again.Bytes(), lines[i]) {
				t.Fatalf("%s (buffer %d): the line written again differs", c.name, size)
			}
			// A stream is left in the file exactly when its escaped text is
			// past the bound; the record with its streams put back is the
			// line's.
			if streamed := escapedLen(t, c.whole.Output) > streamedLineThreshold; streamed != (rec.Output == "" && c.whole.Output != "") {
				t.Fatalf("%s: output streamed %v, the record's output %d bytes", c.name, streamed, len(rec.Output))
			}
			rec.Output = c.whole.Output
			if c.whole.Stderr != nil {
				if streamed := escapedLen(t, *c.whole.Stderr) > streamedLineThreshold; streamed != (*rec.Stderr == "" && *c.whole.Stderr != "") {
					t.Fatalf("%s: stderr streamed %v, the record's stderr %d bytes", c.name, streamed, len(*rec.Stderr))
				}
				s := *c.whole.Stderr
				rec.Stderr = &s
			}
			marshalled, err := json.Marshal(&rec)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(append(marshalled, '\n'), lines[i]) {
				t.Fatalf("%s: the record read back is not the line's", c.name)
			}
		}
	}
}

func escapedLen(t *testing.T, s string) int {
	t.Helper()
	q, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return len(q) - 2
}

// TestUnescaperDecodesAsEncodingJSON: a string's content read through the
// unescaper is what json.Unmarshal gives for the quoted string: the
// encoder's own escapes of random bytes (control bytes, quotes,
// backslashes, HTML characters, bytes that are not UTF-8), and what a line
// karvi wrote never holds but a line read back may (surrogate escapes
// paired, lone, and out of order, upper-case digits, raw bytes that are
// not UTF-8, a character cut at the end), at buffers of 16 and 64 bytes
// and the default, read as io.Copy reads and one byte at a time.
func TestUnescaperDecodesAsEncodingJSON(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	var contents []string
	for i := 0; i < 400; i++ {
		b := make([]byte, rng.Intn(300))
		for j := range b {
			switch rng.Intn(5) {
			case 0:
				b[j] = byte(rng.Intn(256))
			case 1:
				b[j] = byte(rng.Intn(0x20))
			case 2:
				b[j] = "\"\\/<>&"[rng.Intn(6)]
			case 3:
				copy(b[j:], "é€😀 ")
			default:
				b[j] = 'a' + byte(rng.Intn(26))
			}
		}
		q, err := json.Marshal(string(b))
		if err != nil {
			t.Fatal(err)
		}
		contents = append(contents, string(q[1:len(q)-1]))
	}
	for _, c := range []string{
		"%ud83d%ude00", "%uD83D%uDE00", "%ud83d", "%ud83dx", "%ude00%ud83d", "%ud83d%u0041", "%ud800%udbff%udc00",
		"%u00e9%u00E9%u00Ea", "%/%b%f%n%r%t%\"%%", "%u2028 %u003c%u003e%u0026",
	} {
		contents = append(contents, esc(c))
	}
	contents = append(contents, "\xff\xfe raw", "trailing \xe2\x82", "\xe2\x82\xac whole", "\xed\xa0\x80 a surrogate in UTF-8", "\xef\xbf\xbd", "")
	if contents[len(contents)-16] != "\x5cud83d\x5cude00" {
		t.Fatalf("the escapes were not built: %q", contents[len(contents)-16])
	}
	for _, c := range contents {
		var want string
		if err := json.Unmarshal([]byte(`"`+c+`"`), &want); err != nil {
			t.Fatalf("%q: %v", c, err)
		}
		for _, size := range []int{16, 64, 32 << 10} {
			for _, oneByte := range []bool{false, true} {
				var r io.Reader = &unescaper{br: bufio.NewReaderSize(strings.NewReader(c), size), name: "commands.jsonl", line: 1}
				if oneByte {
					r = iotest.OneByteReader(r)
				}
				got, err := io.ReadAll(r)
				if err != nil {
					t.Fatalf("%q (buffer %d, one byte %v): %v", c, size, oneByte, err)
				}
				if string(got) != want {
					t.Fatalf("%q (buffer %d, one byte %v): %q, want %q", c, size, oneByte, got, want)
				}
			}
		}
	}
}

// TestReadRecordAtRefusals: a line that is not a record's JSON object and
// its LF is run_output_record_decode_failed, one that ends early
// run_output_records_unreadable; and a streamed region changed after its
// line was read is refused when it is read.
func TestReadRecordAtRefusals(t *testing.T) {
	big := strings.Repeat("a", streamedLineThreshold+10)
	const decode, unreadable = "run_output_record_decode_failed", "run_output_records_unreadable"
	cases := []struct{ name, line, code string }{
		{"not an object", "not json\n", decode},
		{"no LF", `{"output":"x"}`, unreadable},
		{"cut in a string", `{"command":"abc`, unreadable},
		{"cut in a streamed string", `{"output":"` + big, unreadable},
		{"two objects", "{}{}\n", decode},
		{"after the LF", "{}\n\n", decode},
		{"a control byte", "{\"output\":\"a\x01b\"}\n", decode},
		{"a control byte streamed", "{\"output\":\"" + big + "\x01\"}\n", decode},
		{"an escape", esc(`{"output":"a%qb"}`) + "\n", decode},
		{"a \\u escape", esc(`{"output":"%u12g4"}`) + "\n", decode},
		{"an escape streamed", esc(`{"output":"`+big+`%qb"}`) + "\n", decode},
		{"a \\u escape streamed", esc(`{"output":"`+big+`%u12g4"}`) + "\n", decode},
		{"a streamed string ending in its backslash", esc(`{"output":"` + big + `%`), unreadable},
		{"a raw LF in a value", "{\"notices\":[\n]}\n", decode},
		{"output a number", `{"output":5}` + "\n", decode},
		{"two outputs", `{"output":"` + big + `","output":"` + big + `"}` + "\n", decode},
		{"not a record's", `{"schema_version":"two"}` + "\n", decode},
	}
	for _, c := range cases {
		r := strings.NewReader(c.line)
		_, _, err := ReadRecordAt(r, "commands.jsonl", 7, LineSpan{Offset: 0, Length: int64(len(c.line))})
		if got := errorcodes.Of(err); got != c.code {
			t.Errorf("%s: %v (code %q), want %s", c.name, err, got, c.code)
		}
	}

	// The file shorter than the span FindLines found.
	line := `{"output":"x"}` + "\n"
	_, _, err := ReadRecordAt(strings.NewReader(line[:8]), "commands.jsonl", 7, LineSpan{Offset: 0, Length: int64(len(line))})
	if errorcodes.Of(err) != unreadable || !strings.Contains(err.Error(), "line 7 ends before its record does") {
		t.Errorf("a short file: %v", err)
	}

	// A region changed between its line's read and its own: a control byte
	// in a utf-8 output, a character outside base64's in a base64 one.
	for _, raw := range [][]byte{[]byte(big), append([]byte(big), 0xff)} {
		text, enc, digest := EncodeOutput(raw)
		rec := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show big", Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest}
		var buf bytes.Buffer
		if _, err := WriteRecordLine(&buf, &rec, FromRecord(&rec)); err != nil {
			t.Fatal(err)
		}
		data := buf.Bytes()
		_, src, err := ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 3, LineSpan{Offset: 0, Length: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		at := bytes.Index(data, []byte(`"output":"`)) + len(`"output":"`) + 100
		data[at] = 0x01
		if enc == "base64" {
			data[at] = '!'
		}
		if err := src.WriteRaw(io.Discard); errorcodes.Of(err) != decode {
			t.Errorf("%s output changed after its read: %v", enc, err)
		}
	}
}

// TestFindLines: the spans of the wanted lines among lines of every length
// across the chunk's borders; the pass stops at the last wanted line (a
// read failing after it is never made); and a wanted line the file does
// not hold whole, or a failed read, is run_output_records_unreadable, the
// file's lines and the record's stacked.
func TestFindLines(t *testing.T) {
	withReadbackBuffer(t, 16)
	rng := rand.New(rand.NewSource(2))
	var file bytes.Buffer
	var spans []LineSpan
	for i := 0; i < 300; i++ {
		n := 1 + rng.Intn(70)
		spans = append(spans, LineSpan{Offset: int64(file.Len()), Length: int64(n)})
		file.WriteString(strings.Repeat("x", n-1) + "\n")
	}
	want := []int64{300, 1, 7, 7}
	for l := int64(14); l < 300; l += 7 {
		want = append(want, l)
	}
	got, err := FindLines(bytes.NewReader(file.Bytes()), "commands.jsonl", want)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want)-1 {
		t.Fatalf("%d spans, want %d", len(got), len(want)-1)
	}
	for _, l := range want {
		if got[l] != spans[l-1] {
			t.Fatalf("line %d: %+v, want %+v", l, got[l], spans[l-1])
		}
	}

	// The wanted line ends in the first 16-byte read; the second read fails.
	stop := &failAt{data: []byte("a\nb\n" + strings.Repeat("x", 30) + "\n"), at: 20, armed: true, all: true}
	if _, err := FindLines(stop, "commands.jsonl", []int64{2}); err != nil {
		t.Errorf("the pass did not stop at the wanted line: %v", err)
	}
	if got, err := FindLines(&failAt{at: 0, armed: true, all: true}, "commands.jsonl", nil); err != nil || len(got) != 0 {
		t.Errorf("no wanted line: %v %v", got, err)
	}

	for _, c := range []struct {
		name, file string
		want       int64
		message    string
	}{
		{"fewer lines", "a\nb\nc\n", 5, "commands.jsonl ends before a record's line\n  lines   3\n  record  5"},
		{"the last without its LF", "a\nb", 2, "commands.jsonl ends before a record's line\n  lines   1\n  record  2"},
	} {
		_, err := FindLines(strings.NewReader(c.file), "commands.jsonl", []int64{1, c.want})
		if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), c.message) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	_, err = FindLines(&failAt{data: []byte("a\n"), at: 0, armed: true, all: true}, "commands.jsonl", []int64{1})
	if errorcodes.Of(err) != "run_output_records_unreadable" {
		t.Errorf("a failed read: %v", err)
	}
}

// TestReadRecordAtHoldsNoResponseWhole: a record whose output is 16 MiB
// (twice the follow's frame) is found, read back, and its device's bytes
// streamed to a digest with what FindLines, ReadRecordAt, and WriteRaw
// allocate staying under 1 MiB in all: no copy of the response, escaped or
// decoded.
func TestReadRecordAtHoldsNoResponseWhole(t *testing.T) {
	unit := []byte("0123456789 \"quoted\" back\\slash é€😀 <html> & line\n")
	raw := bytes.Repeat(unit, (16<<20)/len(unit))
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	src, enc := spoolOf(t, raw)
	rec := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show big", OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest, Prompt: "router#"}
	path := filepath.Join(t.TempDir(), "commands.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := WriteRecordLine(f, &rec, src); err != nil {
		t.Fatal(err)
	}
	raw = nil
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	spans, err := FindLines(f, path, []int64{1})
	if err != nil {
		t.Fatal(err)
	}
	got, gotSrc, err := ReadRecordAt(f, path, 1, spans[1])
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	if err := gotSrc.WriteRaw(h); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if got.Output != "" || !gotSrc.Spooled() {
		t.Fatalf("the output was not left in the file: %d bytes in the record", len(got.Output))
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		t.Fatal("the bytes read back are not the output's")
	}
	alloc := after.TotalAlloc - before.TotalAlloc
	t.Logf("a %d-byte line read back with %d bytes allocated", spans[1].Length, alloc)
	if alloc > 1<<20 {
		t.Fatalf("the read-back allocated %d bytes for a %d-byte line", alloc, spans[1].Length)
	}
}

// failAt is a file whose read of the byte at fails, once or (all) every
// time, while armed.
type failAt struct {
	data       []byte
	at         int64
	armed, all bool
}

func (f *failAt) ReadAt(p []byte, off int64) (int, error) {
	if f.armed && off <= f.at && f.at < off+int64(len(p)) {
		f.armed = f.all
		return 0, errors.New("input/output error")
	}
	return bytes.NewReader(f.data).ReadAt(p, off)
}

// bigLine is the store's line for a shell record whose output is raw.
func bigLine(t *testing.T, raw []byte) []byte {
	t.Helper()
	text, enc, digest := EncodeOutput(raw)
	rec := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show big", Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest}
	var buf bytes.Buffer
	if _, err := WriteRecordLine(&buf, &rec, FromRecord(&rec)); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestRegionRefusesAFileChangedSinceItsRead: a streamed output read after
// its line was read, the file changed since, is refused, and what was
// given before the refusal is the device's: the file cut short in the
// region, utf-8 and base64, is run_output_records_unreadable; a byte
// changed to another valid one is run_output_digest_mismatch, the record's
// and the read digest stacked, in an exec record's stderr by its own digest; and a read failing inside the region, once or
// every time, at a character, an escape, or a \u escape cut by the
// buffer, is run_output_records_unreadable, never other bytes.
func TestRegionRefusesAFileChangedSinceItsRead(t *testing.T) {
	big := strings.Repeat("a", streamedLineThreshold+1000)
	for _, raw := range [][]byte{[]byte(big), append([]byte(big), 0xff)} {
		line := bigLine(t, raw)
		path := filepath.Join(t.TempDir(), "commands.jsonl")
		if err := os.WriteFile(path, line, 0o600); err != nil {
			t.Fatal(err)
		}
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		_, src, err := ReadRecordAt(f, path, 1, LineSpan{Offset: 0, Length: int64(len(line))})
		if err != nil {
			t.Fatal(err)
		}
		// A cut at a multiple of four into the region, so base64 ends on a
		// quantum.
		if err := os.Truncate(path, src.region.off+40000); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		err = src.WriteRaw(&out)
		if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), "line 1 ends before its record does") || !bytes.HasPrefix(raw, out.Bytes()) {
			t.Errorf("%s, the file cut: %v, %d bytes given", src.encoding, err, out.Len())
		}
	}

	line := bigLine(t, []byte(big))
	_, src, err := ReadRecordAt(bytes.NewReader(line), "commands.jsonl", 4, LineSpan{Offset: 0, Length: int64(len(line))})
	if err != nil {
		t.Fatal(err)
	}
	line[src.region.off+100] = 'b'
	err = src.WriteRaw(io.Discard)
	if errorcodes.Of(err) != "run_output_digest_mismatch" || !strings.Contains(err.Error(), "commands.jsonl line 4's output does not match the record's digest\n  record  "+src.digest+"\n  read    ") {
		t.Errorf("a byte changed: %v", err)
	}

	// An exec record's streamed stderr is checked against its own digest.
	whole, _, execSrc := execRecord(t, []byte("x\n"), []byte(big), false, false)
	var execLine bytes.Buffer
	if _, err := WriteRecordLine(&execLine, &whole, execSrc); err != nil {
		t.Fatal(err)
	}
	data := execLine.Bytes()
	_, src, err = ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 5, LineSpan{Offset: 0, Length: int64(len(data))})
	if err != nil {
		t.Fatal(err)
	}
	stderr, ok := src.Stderr()
	if !ok || stderr.region == nil {
		t.Fatalf("the stderr was not streamed")
	}
	data[stderr.region.off+100] = 'b'
	if err := stderr.WriteRaw(io.Discard); errorcodes.Of(err) != "run_output_digest_mismatch" || !strings.Contains(err.Error(), "line 5's stderr does not match the record's digest") {
		t.Errorf("a stderr byte changed: %v", err)
	}

	// A short output and a short stderr are in the record, checked when
	// the line is read: a byte changed, the digest left, is the same
	// refusal, the two digests stacked; base64 and utf-8 alike.
	for _, c := range []struct {
		name           string
		stdout, stderr []byte
		field          string
		from, to       string
	}{
		{"output utf-8", []byte("ok\n"), []byte("e\n"), "output", `"output":"ok\n"`, `"output":"no\n"`},
		{"output base64", []byte{0xff, 'o', 'k'}, []byte("e\n"), "output", `"output":"/29r"`, `"output":"/29s"`},
		{"stderr utf-8", []byte("ok\n"), []byte("e\n"), "stderr", `"stderr":"e\n"`, `"stderr":"f\n"`},
	} {
		whole, _, src := execRecord(t, c.stdout, c.stderr, false, false)
		var buf bytes.Buffer
		if _, err := WriteRecordLine(&buf, &whole, src); err != nil {
			t.Fatal(err)
		}
		good := buf.Bytes()
		if _, _, err := ReadRecordAt(bytes.NewReader(good), "commands.jsonl", 6, LineSpan{Offset: 0, Length: int64(len(good))}); err != nil {
			t.Fatalf("%s: the line as written: %v", c.name, err)
		}
		if bytes.Count(good, []byte(c.from)) != 1 {
			t.Fatalf("%s: %q not once in %s", c.name, c.from, good)
		}
		bad := bytes.Replace(good, []byte(c.from), []byte(c.to), 1)
		want := whole.OutputSHA256
		if c.field == "stderr" {
			want = *whole.StderrSHA256
		}
		_, _, err := ReadRecordAt(bytes.NewReader(bad), "commands.jsonl", 6, LineSpan{Offset: 0, Length: int64(len(bad))})
		if errorcodes.Of(err) != "run_output_digest_mismatch" || !strings.Contains(err.Error(), "commands.jsonl line 6's "+c.field+" does not match the record's digest\n  record  "+want+"\n  read    ") {
			t.Errorf("%s, a byte changed: %v", c.name, err)
		}
	}

	withReadbackBuffer(t, 16)
	for _, c := range []struct {
		name string
		raw  string
		all  bool
	}{
		{"a character cut, the failure once", strings.Repeat("a", 15) + "é" + big, false},
		{"a character cut, the failure stays", strings.Repeat("a", 15) + "é" + big, true},
		{"an escape cut, the failure once", strings.Repeat("a", 15) + "\n" + big, false},
		{"an escape cut, the failure stays", strings.Repeat("a", 15) + "\n" + big, true},
		{"a \\u escape cut, the failure stays", strings.Repeat("a", 15) + "\x01" + big, true},
	} {
		line := bigLine(t, []byte(c.raw))
		f := &failAt{data: line}
		_, src, err := ReadRecordAt(f, "commands.jsonl", 1, LineSpan{Offset: 0, Length: int64(len(line))})
		if err != nil || src.region == nil {
			t.Fatalf("%s: %v, streamed %v", c.name, err, src.region != nil)
		}
		f.at, f.armed, f.all = src.region.off+16, true, c.all
		var out bytes.Buffer
		err = src.WriteRaw(&out)
		if errorcodes.Of(err) != "run_output_records_unreadable" || !strings.Contains(err.Error(), "input/output error") || !strings.HasPrefix(c.raw, out.String()) {
			t.Errorf("%s: %v, %d bytes given", c.name, err, out.Len())
		}
	}
}

// TestReadRecordAtChecksALongAnswerFirst: a streamed output and a streamed
// stderr are read through once and checked against the record's digest
// before ReadRecordAt gives their source, as a short one is: a byte
// changed, the digest kept, is run_output_digest_mismatch, the two
// stacked, and no source is given to show; so is the record's digest
// changed, the bytes kept. A streamed stderr's base64 refused names the
// stderr. A read that fails on that pass with a coded error (the gathered
// display's interrupt) gives that error as it is.
func TestReadRecordAtChecksALongAnswerFirst(t *testing.T) {
	big := strings.Repeat("a", streamedLineThreshold+1000)
	good := bigLine(t, []byte(big))
	_, src, err := ReadRecordAt(bytes.NewReader(good), "commands.jsonl", 4, LineSpan{Offset: 0, Length: int64(len(good))})
	if err != nil || src.region == nil {
		t.Fatalf("the line as written: %v, streamed %v", err, src.region != nil)
	}
	off := src.region.off
	changed := bytes.Clone(good)
	changed[off+100] = 'b'
	flipped := bytes.Replace(good, []byte(src.digest), []byte(strings.Repeat("0", len(src.digest))), 1)
	for _, c := range []struct {
		name string
		line []byte
		want string
	}{
		{"a byte changed", changed, src.digest},
		{"the digest changed", flipped, strings.Repeat("0", len(src.digest))},
	} {
		_, got, err := ReadRecordAt(bytes.NewReader(c.line), "commands.jsonl", 4, LineSpan{Offset: 0, Length: int64(len(c.line))})
		if errorcodes.Of(err) != "run_output_digest_mismatch" || !strings.Contains(err.Error(), "commands.jsonl line 4's output does not match the record's digest\n  record  "+c.want+"\n  read    ") || got.region != nil {
			t.Errorf("%s: %v, a source given %v", c.name, err, got.region != nil)
		}
	}

	whole, _, execSrc := execRecord(t, []byte("x\n"), []byte(big), false, false)
	var execLine bytes.Buffer
	if _, err := WriteRecordLine(&execLine, &whole, execSrc); err != nil {
		t.Fatal(err)
	}
	data := execLine.Bytes()
	_, src, err = ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 5, LineSpan{Offset: 0, Length: int64(len(data))})
	stderr, ok := src.Stderr()
	if err != nil || !ok || stderr.region == nil {
		t.Fatalf("the exec line as written: %v, the stderr streamed %v", err, ok && stderr.region != nil)
	}
	data[stderr.region.off+100] = 'b'
	if _, _, err := ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 5, LineSpan{Offset: 0, Length: int64(len(data))}); errorcodes.Of(err) != "run_output_digest_mismatch" || !strings.Contains(err.Error(), "line 5's stderr does not match the record's digest\n  record  "+*whole.StderrSHA256+"\n  read    ") {
		t.Errorf("a stderr byte changed: %v", err)
	}

	// A streamed stderr under base64 whose text the decoder refuses names
	// the stderr, not the output.
	whole, _, execSrc = execRecord(t, []byte("x\n"), append([]byte{0xff}, big...), false, false)
	execLine.Reset()
	if _, err := WriteRecordLine(&execLine, &whole, execSrc); err != nil {
		t.Fatal(err)
	}
	data = execLine.Bytes()
	_, src, err = ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 7, LineSpan{Offset: 0, Length: int64(len(data))})
	stderr, ok = src.Stderr()
	if err != nil || !ok || stderr.region == nil || stderr.encoding != "base64" {
		t.Fatalf("the base64 stderr as written: %v, streamed %v", err, ok && stderr.region != nil)
	}
	data[stderr.region.off+100] = '!'
	if _, _, err := ReadRecordAt(bytes.NewReader(data), "commands.jsonl", 7, LineSpan{Offset: 0, Length: int64(len(data))}); errorcodes.Of(err) != "run_output_record_decode_failed" || !strings.Contains(err.Error(), "line 7: the stderr's base64") {
		t.Errorf("a base64 stderr refused: %v", err)
	}

	// The line's scan reads the byte at once; the check's pass is the
	// second read of it.
	stop := errorcodes.Errorf("cancelled", "the gathered display was interrupted")
	f := &stopAt{data: good, at: off + 100, nth: 2, err: stop}
	if _, _, err := ReadRecordAt(f, "commands.jsonl", 4, LineSpan{Offset: 0, Length: int64(len(good))}); !errors.Is(err, stop) || errorcodes.Of(err) != "cancelled" {
		t.Errorf("stopped on the check's pass: %v (%s), the byte read %d times", err, errorcodes.Of(err), f.reads)
	}
}

// stopAt is a file whose nth read of the byte at, and every read after,
// fails with err, as the gathered display's file does once the interrupt
// has come.
type stopAt struct {
	data       []byte
	at         int64
	nth, reads int
	err        error
}

func (s *stopAt) ReadAt(p []byte, off int64) (int, error) {
	if off <= s.at && s.at < off+int64(len(p)) {
		s.reads++
	}
	if s.reads >= s.nth {
		return 0, s.err
	}
	return bytes.NewReader(s.data).ReadAt(p, off)
}

// TestRegionThroughTheStoresPass: a streamed region's source passes the
// store's measuring pass, which verifies a spool against its digest, the
// length the line's; and the same pass refuses it once a byte has changed.
func TestRegionThroughTheStoresPass(t *testing.T) {
	line := bigLine(t, []byte(strings.Repeat("é€", streamedLineThreshold)))
	rec, src, err := ReadRecordAt(bytes.NewReader(line), "commands.jsonl", 1, LineSpan{Offset: 0, Length: int64(len(line))})
	if err != nil || src.region == nil {
		t.Fatalf("%v, streamed %v", err, src.region != nil)
	}
	l, err := newRecordLine(&rec, src)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := l.measure(); err != nil || n != int64(len(line)) {
		t.Fatalf("measure: %d bytes, %v; the line is %d", n, err, len(line))
	}
	line[src.region.off+10] = 'x'
	if _, err := l.measure(); errorcodes.Of(err) != "run_output_digest_mismatch" {
		t.Fatalf("measure of a changed region: %v", err)
	}
}
