package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestStreamedLineEqualsTheMarshalledRecord holds a line written in pieces
// to json.Marshal of the whole record and a newline, over the outputs whose
// escaping differs from their bytes: quotes and backslashes, control
// characters, the HTML characters encoding/json escapes, U+2028 and U+2029,
// and multi-byte characters placed across every chunk border.
func TestStreamedLineEqualsTheMarshalledRecord(t *testing.T) {
	filler := strings.Repeat("0123456789 line of output\n", 5000) // beyond the threshold and several chunks
	outputs := map[string]string{
		"plain":      filler,
		"escapes":    filler + "\"quoted\" back\\slash \t tab \b \f \x01 <html> &     \x7f" + filler,
		"multi-byte": strings.Repeat("é", lineChunk) + strings.Repeat("€", lineChunk) + strings.Repeat("😀", lineChunk),
	}
	// A multi-byte character at each offset around the first chunk border.
	for shift := 0; shift < 5; shift++ {
		outputs["border"+string(rune('0'+shift))] = strings.Repeat("a", lineChunk-shift) + strings.Repeat("😀é€", 20000)
	}
	for name, out := range outputs {
		r := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show <big> & \"more\"", Output: out, OutputEncoding: "utf-8", OutputBytes: int64(len(out)), Prompt: "router#"}
		want, err := json.Marshal(&r)
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, '\n')
		line, err := newRecordLine(&r, FromRecord(&r))
		if err != nil {
			t.Fatal(err)
		}
		if line.whole != nil {
			t.Fatalf("%s: a %d-byte output was not written in pieces", name, len(out))
		}
		var got bytes.Buffer
		n, err := line.WriteTo(&got)
		if err != nil || n != int64(got.Len()) {
			t.Fatalf("%s: WriteTo = %d, %v; wrote %d", name, n, err, got.Len())
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("%s: the streamed line differs from the marshalled record (%d and %d bytes)", name, got.Len(), len(want))
		}
	}
}

// A record that holds the stand-in's text outside its output (a command
// could) is still cut at the output field itself, the key and the value,
// and written in pieces, the same bytes as the whole record.
func TestStreamedLineCutsAtTheOutputField(t *testing.T) {
	r := records.CommandRecord{Command: outputStandIn, Output: strings.Repeat("x", streamedLineThreshold), OutputEncoding: "utf-8"}
	line, err := newRecordLine(&r, FromRecord(&r))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(&r)
	var got bytes.Buffer
	if _, err := line.WriteTo(&got); err != nil {
		t.Fatal(err)
	}
	if line.whole != nil || !bytes.Equal(got.Bytes(), append(want, '\n')) {
		t.Fatal("a record naming the stand-in in its command must still be cut at its output field and equal json.Marshal")
	}
}

// TestIndentedRecordFromASpoolEqualsMarshalIndent is the streamed record
// for the json format: the record written in pieces from its spool, the
// output escaped between the halves of the indented form, equals
// json.MarshalIndent of the record carrying the output as its string, with
// the element's prefix on every line as the renderer puts it.
func TestIndentedRecordFromASpoolEqualsMarshalIndent(t *testing.T) {
	filler := strings.Repeat("line \"quoted\" \\ é€😀 <html>\n", 4000)
	for name, raw := range map[string][]byte{"utf-8": []byte(filler), "binary": append([]byte(filler), 0xff, '\n')} {
		text, enc, digest := EncodeOutput(raw)
		r := records.CommandRecord{SchemaVersion: 2, RecordID: "r", Command: "show", Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest}
		want, err := json.MarshalIndent(&r, "  ", "  ")
		if err != nil {
			t.Fatal(err)
		}
		want = append([]byte("  "), want...)
		src, _ := spoolOf(t, raw)
		spooled := r
		spooled.Output = ""
		var got bytes.Buffer
		if err := WriteRecordJSONIndent(&got, &spooled, src, "  ", "  "); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("%s: the indented form from the spool differs from MarshalIndent (%d and %d bytes)", name, got.Len(), len(want))
		}
		// The string form small enough to be marshalled whole takes the
		// same path's other branch and equals too.
		small := records.CommandRecord{RecordID: "s", Output: "ok\n", OutputEncoding: "utf-8"}
		wantSmall, _ := json.MarshalIndent(&small, "", " ")
		got.Reset()
		if err := WriteRecordJSONIndent(&got, &small, FromRecord(&small), "", " "); err != nil || !bytes.Equal(got.Bytes(), wantSmall) {
			t.Fatalf("small record: %v\n%s\n%s", err, got.Bytes(), wantSmall)
		}
	}
}
