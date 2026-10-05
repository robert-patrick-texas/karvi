package output

import (
	"bufio"
	"bytes"
	"encoding/json"
	"hash"
	"io"
	"sort"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// A record's line is what json.Marshal gives for the record, then '\n'.
// For a small record that is how it is made. A record whose output is
// large is written in pieces instead: json.Marshal of the whole record
// grows a buffer by doubling, copies it out, and the caller's newline
// copied it again, which at production widths was the largest
// share of the daemon's memory. The pieces
// are the record marshalled with a short stand-in for its output, cut at
// the stand-in, and between the two halves the output escaped a chunk at
// a time by json.Marshal itself, so the escaping cannot differ from the
// whole record's. The bytes are the same either way; line_test.go holds
// the two forms equal.
//
// This is the streaming escape. The output comes from its Source: the
// record's string, or the command's spool file, which feeds the same
// writer a chunk at a time and is hashed on the measuring pass against
// the reader's digest.

// streamedLineThreshold is the output size from which a line is written in
// pieces. Below it the whole line is a few tens of kilobytes at most.
const streamedLineThreshold = 64 << 10

// lineChunk is how much of the output is escaped at once.
const lineChunk = 32 << 10

// The stand-ins replace a streamed field's value while the rest of the
// record is marshalled: the output, and an exec record's stderr. The cut
// is at the field's own key and value, so a record that holds the same
// text elsewhere (a command could) still cuts unambiguously; a record
// that somehow does not is written whole.
const (
	outputStandIn = "karvi-output-stand-in-5d1c7e0b9a3f"
	stderrStandIn = "karvi-stderr-stand-in-8e2f4a6c1b7d"
)

// recordLine is one record's line, whole or in pieces: parts are the
// marshalled record around its streamed fields in the line's order, one
// more part than fields, the last ending in '\n'.
type recordLine struct {
	whole  []byte // the line with its newline, when not streamed
	parts  [][]byte
	fields []Source
}

// streamedField is one field written in pieces: its key, its stand-in,
// and where its bytes are.
type streamedField struct {
	key, standIn string
	src          Source
}

// streamedFields are r's fields written in pieces: the output when it is
// spooled or its string large, and an exec record's stderr by the same
// rule.
func streamedFields(r *records.CommandRecord, src Source) []streamedField {
	var fields []streamedField
	if src.Spooled() || len(r.Output) >= streamedLineThreshold {
		fields = append(fields, streamedField{"output", outputStandIn, src})
	}
	if stderr, ok := src.Stderr(); ok && r.Stderr != nil && (stderr.Spooled() || len(*r.Stderr) >= streamedLineThreshold) {
		fields = append(fields, streamedField{"stderr", stderrStandIn, stderr})
	}
	return fields
}

// withStandIns is a shallow copy of r with each streamed field's value
// its stand-in.
func withStandIns(r *records.CommandRecord, fields []streamedField) *records.CommandRecord {
	shallow := *r
	for _, f := range fields {
		switch f.key {
		case "output":
			shallow.Output = f.standIn
		case "stderr":
			standIn := f.standIn
			shallow.Stderr = &standIn
		}
	}
	return &shallow
}

// newRecordLine makes r's line from src, r's output where it is: in pieces
// when a stream is spooled or its string large, whole otherwise.
func newRecordLine(r *records.CommandRecord, src Source) (recordLine, error) {
	if fields := streamedFields(r, src); len(fields) > 0 {
		small, err := json.Marshal(withStandIns(r, fields))
		if err != nil {
			return recordLine{}, err
		}
		if parts, sources, ok := cutAtFields(small, fields); ok {
			parts[len(parts)-1] = append(parts[len(parts)-1], '\n')
			return recordLine{parts: parts, fields: sources}, nil
		}
		// Unreachable for the record's fixed fields; a spooled stream is
		// read whole rather than the record lost.
		if r, err = inlined(r, fields); err != nil {
			return recordLine{}, err
		}
	}
	// The encoder writes the record and its newline in one piece; appending
	// the newline to json.Marshal's exact slice would copy the line again.
	var whole bytes.Buffer
	if err := json.NewEncoder(&whole).Encode(r); err != nil {
		return recordLine{}, err
	}
	return recordLine{whole: whole.Bytes()}, nil
}

// inlined is a shallow copy of r with each spooled field's bytes read into
// its string, under the encoding the record already names.
func inlined(r *records.CommandRecord, fields []streamedField) (*records.CommandRecord, error) {
	shallow := *r
	for _, f := range fields {
		if !f.src.Spooled() {
			continue
		}
		var text bytes.Buffer
		if err := f.src.WriteRaw(&text); err != nil {
			return nil, err
		}
		encoded, _, _ := EncodeOutput(text.Bytes())
		switch f.key {
		case "output":
			shallow.Output = encoded
		case "stderr":
			shallow.Stderr = &encoded
		}
	}
	return &shallow, nil
}

// cutAtFields finds each field's stand-in value in the marshalled record,
// compact (`"output":"…"`) or indented (`"output": "…"`), and returns the
// parts around them in the line's order with the fields' sources in that
// order: a part before a field ends with its value's opening quote, the
// next begins with its closing quote. The key and the value together make
// the cut unambiguous; a field that does not cut once is reported.
func cutAtFields(small []byte, fields []streamedField) ([][]byte, []Source, bool) {
	type cut struct {
		at, end int
		src     Source
	}
	cuts := make([]cut, 0, len(fields))
	for _, f := range fields {
		found := false
		for _, sep := range []string{`"` + f.key + `":"`, `"` + f.key + `": "`} {
			key := []byte(sep + f.standIn + `"`)
			if bytes.Count(small, key) == 1 {
				at := bytes.Index(small, key) + len(sep)
				cuts = append(cuts, cut{at: at, end: at + len(f.standIn), src: f.src})
				found = true
				break
			}
		}
		if !found {
			return nil, nil, false
		}
	}
	sort.Slice(cuts, func(i, j int) bool { return cuts[i].at < cuts[j].at })
	parts := make([][]byte, 0, len(cuts)+1)
	sources := make([]Source, 0, len(cuts))
	from := 0
	for _, c := range cuts {
		parts = append(parts, small[from:c.at:c.at])
		sources = append(sources, c.src)
		from = c.end
	}
	return append(parts, small[from:]), sources, true
}

// WriteRecordJSONIndent writes r as json.MarshalIndent gives it, with each
// stream escaped in pieces from its source between the parts when it is
// spooled or large (the renderer's json format). A JSON string holds no
// newline, so the indent is unaffected; the bytes equal MarshalIndent of
// the record with its streams in place. prefix begins the first line too,
// as the renderer's array element wants.
func WriteRecordJSONIndent(w io.Writer, r *records.CommandRecord, src Source, prefix, indent string) error {
	fields := streamedFields(r, src)
	if len(fields) == 0 {
		encoded, err := json.MarshalIndent(r, prefix, indent)
		if err != nil {
			return err
		}
		_, err = w.Write(append([]byte(prefix), encoded...))
		return err
	}
	small, err := json.MarshalIndent(withStandIns(r, fields), prefix, indent)
	if err != nil {
		return err
	}
	parts, sources, ok := cutAtFields(small, fields)
	if !ok {
		return errorcodes.Errorf("record_encode_failed", "the record's streamed fields could not be found for the indented form")
	}
	if _, err := w.Write([]byte(prefix)); err != nil {
		return err
	}
	for i, part := range parts {
		if _, err := w.Write(part); err != nil {
			return err
		}
		if i < len(sources) {
			if err := sources[i].writeEscaped(w, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// measure is the pass to io.Discard before any byte reaches a file: the
// line's length for the job limit and the follow frame's bound, and for a
// spooled stream the digest of the file's bytes, which must be the
// reader's (output_spool_mismatch when it is not).
func (l recordLine) measure() (int64, error) {
	verifiers := make([]*verifier, len(l.fields))
	for i, f := range l.fields {
		verifiers[i] = f.verifier()
	}
	n, err := l.writeJSON(io.Discard, verifiers)
	if err != nil {
		return 0, err
	}
	for i, f := range l.fields {
		if err := verifiers[i].check(f); err != nil {
			return 0, err
		}
	}
	return n + 1, nil
}

// WriteTo writes the line and reports its length. Writing to io.Discard is
// how the length is known before anything reaches a file.
func (l recordLine) WriteTo(w io.Writer) (int64, error) {
	n, err := l.writeJSON(w, nil)
	if err != nil {
		return n, err
	}
	m, err := w.Write([]byte{'\n'})
	return n + int64(m), err
}

// writeJSON writes the record's JSON without the line's LF: the line as
// the follow stream embeds it in a frame (ipc.WriteRecordFrame), the same
// bytes and the same pieces. verifiers, when given (the measuring pass),
// hash each spooled field's bytes as they are read.
func (l recordLine) writeJSON(w io.Writer, verifiers []*verifier) (int64, error) {
	if l.whole != nil {
		n, err := w.Write(l.whole[:len(l.whole)-1])
		return int64(n), err
	}
	counted := &countingWriter{}
	tee := io.MultiWriter(w, counted)
	for i, part := range l.parts {
		if i == len(l.parts)-1 {
			part = part[:len(part)-1]
		}
		if _, err := tee.Write(part); err != nil {
			return counted.n, err
		}
		if i < len(l.fields) {
			var h hash.Hash
			if verifiers != nil {
				h = verifiers[i].hash()
			}
			if err := l.fields[i].writeEscaped(tee, h); err != nil {
				return counted.n, err
			}
		}
	}
	return counted.n, nil
}

// WriteRecordLine writes r's line to w from its source, in pieces when
// the output is spooled or large, and reports the bytes written. The
// renderer's jsonl format is this line.
func WriteRecordLine(w io.Writer, r *records.CommandRecord, src Source) (int64, error) {
	line, err := newRecordLine(r, src)
	if err != nil {
		return 0, err
	}
	return writeLine(w, line)
}

// WriteRecordJSON writes r's line without its LF, in pieces when its
// output is large: the record as the daemon puts it in a follow frame
// through one buffer as the file's line goes. The bytes are the line's, so a follower's jsonl output equals
// commands.jsonl byte for byte.
func WriteRecordJSON(w io.Writer, r *records.CommandRecord) (int64, error) {
	line, err := newRecordLine(r, FromRecord(r))
	if err != nil {
		return 0, err
	}
	buffered := bufio.NewWriterSize(w, 256<<10)
	n, err := line.writeJSON(buffered, nil)
	if err != nil {
		return n, err
	}
	return n, buffered.Flush()
}
