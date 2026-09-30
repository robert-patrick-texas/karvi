package output

import (
	"bufio"
	"bytes"
	"encoding/json"
	"hash"
	"io"

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

// outputStandIn replaces the output while the rest of the record is
// marshalled. The cut is at the output field's own key and value, so a
// record that holds the same text elsewhere (a command could) still cuts
// unambiguously; a record that somehow does not is written whole.
const outputStandIn = "karvi-output-stand-in-5d1c7e0b9a3f"

// recordLine is one record's line, whole or in pieces.
type recordLine struct {
	whole          []byte // the line with its newline, when not streamed
	prefix, suffix []byte // around the escaped output, the suffix ending in '\n'
	output         Source
}

// newRecordLine makes r's line from src, r's output where it is: in pieces
// when the output is spooled or the string is large, whole otherwise.
func newRecordLine(r *records.CommandRecord, src Source) (recordLine, error) {
	if streamed(r, src) {
		shallow := *r
		shallow.Output = outputStandIn
		small, err := json.Marshal(&shallow)
		if err != nil {
			return recordLine{}, err
		}
		if before, after, ok := cutAtOutput(small); ok {
			return recordLine{prefix: before, suffix: append(after, '\n'), output: src}, nil
		}
		if src.Spooled() {
			// Unreachable for the record's fixed fields; the file is read
			// whole rather than the record lost.
			var text bytes.Buffer
			if err := src.WriteRaw(&text); err != nil {
				return recordLine{}, err
			}
			shallow.Output, _, _ = EncodeOutput(text.Bytes())
			r = &shallow
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

// streamed says whether r's output is written in pieces: when it is in a
// spool, or the string is large.
func streamed(r *records.CommandRecord, src Source) bool {
	return src.Spooled() || len(r.Output) >= streamedLineThreshold
}

// cutAtOutput finds the output field's stand-in value in the marshalled
// record, compact (`"output":"…"`) or indented (`"output": "…"`), and
// returns the halves around it: before ends with the value's opening
// quote, after begins with its closing quote. The key and the value
// together make the cut unambiguous; a record that somehow does not cut
// once is reported.
func cutAtOutput(small []byte) (before, after []byte, ok bool) {
	for _, sep := range []string{`"output":"`, `"output": "`} {
		key := []byte(sep + outputStandIn + `"`)
		if bytes.Count(small, key) == 1 {
			at := bytes.Index(small, key) + len(sep)
			return small[:at:at], small[at+len(outputStandIn):], true
		}
	}
	return nil, nil, false
}

// WriteRecordJSONIndent writes r as json.MarshalIndent gives it, with the
// output escaped in pieces from its source between the two halves when
// it is spooled or large (the renderer's json format). A JSON string holds no newline, so the indent is unaffected;
// the bytes equal MarshalIndent of the record with its output in place.
// prefix begins the first line too, as the renderer's array element wants.
func WriteRecordJSONIndent(w io.Writer, r *records.CommandRecord, src Source, prefix, indent string) error {
	if !streamed(r, src) {
		encoded, err := json.MarshalIndent(r, prefix, indent)
		if err != nil {
			return err
		}
		_, err = w.Write(append([]byte(prefix), encoded...))
		return err
	}
	shallow := *r
	shallow.Output = outputStandIn
	small, err := json.MarshalIndent(&shallow, prefix, indent)
	if err != nil {
		return err
	}
	before, after, ok := cutAtOutput(small)
	if !ok {
		return errorcodes.Errorf("record_encode_failed", "the record's output field could not be found for the indented form")
	}
	if _, err := w.Write(append([]byte(prefix), before...)); err != nil {
		return err
	}
	if err := src.writeEscaped(w, nil); err != nil {
		return err
	}
	_, err = w.Write(after)
	return err
}

// measure is the pass to io.Discard before any byte reaches a file: the
// line's length for the job limit and the follow frame's bound, and for a
// spooled output the digest of the file's bytes, which must be the
// reader's (output_spool_mismatch when it is not).
func (l recordLine) measure() (int64, error) {
	v := l.output.verifier()
	n, err := l.writeJSON(io.Discard, v.hash())
	if err != nil {
		return 0, err
	}
	if err := v.check(l.output); err != nil {
		return 0, err
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
// bytes and the same pieces. h, when given, hashes a spool's bytes as
// they are read (the measuring pass).
func (l recordLine) writeJSON(w io.Writer, h hash.Hash) (int64, error) {
	if l.whole != nil {
		n, err := w.Write(l.whole[:len(l.whole)-1])
		return int64(n), err
	}
	counted := &countingWriter{}
	tee := io.MultiWriter(w, counted)
	if _, err := tee.Write(l.prefix); err != nil {
		return counted.n, err
	}
	if err := l.output.writeEscaped(tee, h); err != nil {
		return counted.n, err
	}
	_, err := tee.Write(l.suffix[:len(l.suffix)-1])
	return counted.n, err
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
