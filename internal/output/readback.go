package output

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"hash"
	"io"
	"math"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// A record's line read back from commands.jsonl, as the gathered display
// reads each group's first record at the job's end: the lines found by
// their numbers in one pass (FindLines), then each record decoded with its
// output and an exec record's stderr left in the file when large
// (ReadRecordAt). Such a stream's source is a region of the line: its
// passes read the JSON string's content from the file, the escapes undone
// and, under base64, decoded as they stream, so the device's bytes come out
// (WriteRaw), checked against the record's digest at the pass's end, and
// the line written again from the record and its source is the line's own
// bytes (WriteRecordLine). No response is read whole into memory, as the
// store never holds one to write it.

// readbackBuffer is the read-back's buffer: FindLines's chunk and each
// line's and region's reader. A variable so a test can make every escape
// and character cross a buffer's border (bufio's least is 16).
var readbackBuffer = 32 << 10

// LineSpan is a line's place in commands.jsonl: the offset of its first
// byte and its length, its LF included.
type LineSpan struct {
	Offset, Length int64
}

// FindLines reads commands.jsonl from r once, from the file's first byte,
// and returns the span of each of lines by number, a record's sequence
// being its line's number (Store.appendLine); it stops at the last of
// them. name is the file's path for the messages. A line the file does not
// hold whole, LF and all, is run_output_records_unreadable, as is a read
// that fails.
func FindLines(r io.ReaderAt, name string, lines []int64) (map[int64]LineSpan, error) {
	want := make(map[int64]bool, len(lines))
	for _, l := range lines {
		want[l] = true
	}
	spans := make(map[int64]LineSpan, len(want))
	if len(want) == 0 {
		return spans, nil
	}
	file := io.NewSectionReader(r, 0, math.MaxInt64)
	buf := make([]byte, readbackBuffer)
	// pos is the offset of the chunk's first byte, start the current
	// line's, line its number.
	var pos, start int64
	line := int64(1)
	for {
		n, err := file.Read(buf)
		chunk := buf[:n]
		for {
			i := bytes.IndexByte(chunk, '\n')
			if i < 0 {
				break
			}
			end := pos + int64(i) + 1
			if want[line] {
				spans[line] = LineSpan{Offset: start, Length: end - start}
				if len(spans) == len(want) {
					return spans, nil
				}
			}
			line++
			start, pos, chunk = end, end, chunk[i+1:]
		}
		pos += int64(len(chunk))
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", name, err)
		}
	}
	missing := int64(math.MaxInt64)
	for l := range want {
		if _, ok := spans[l]; !ok && l < missing {
			missing = l
		}
	}
	return nil, errorcodes.Errorf("run_output_records_unreadable", "%s ends before a record's line\n  lines   %d\n  record  %d", name, line-1, missing)
}

// ReadRecordAt decodes the record whose line is at span in r: the
// record's JSON object and its LF, as the store writes it. name is the
// file's path and line the line's number, for the messages. The output,
// and an exec record's stderr, whose escaped text is longer than
// streamedLineThreshold (the store's bound for a line in pieces, measured
// there on the string before its escapes) stays in r: the record carries
// it empty and the source streams it from there, its digest the record's;
// a shorter one is in the record, its source FromRecord's, its bytes
// checked against the record's digest here. A field's name
// matches as encoding/json matches it, case folded. A line that ends early
// or cannot be read is run_output_records_unreadable; one that is not a
// record's JSON object is run_output_record_decode_failed.
func ReadRecordAt(r io.ReaderAt, name string, line int64, span LineSpan) (records.CommandRecord, Source, error) {
	var rec records.CommandRecord
	sc := &lineScanner{br: bufio.NewReaderSize(io.NewSectionReader(r, span.Offset, span.Length), readbackBuffer), pos: span.Offset, name: name, line: line}
	small, regions, err := sc.object()
	if err != nil {
		return rec, Source{}, err
	}
	if err := json.Unmarshal(small, &rec); err != nil {
		return records.CommandRecord{}, Source{}, sc.malformed("%v", err)
	}
	src := FromRecord(&rec)
	if at, ok := regions["output"]; ok {
		stderr := src.stderr
		src = Source{path: name, region: &lineRegion{r: r, off: at.off, n: at.n, line: line, field: "output"}, encoding: rec.OutputEncoding, digest: rec.OutputSHA256, stderr: stderr}
	} else if err := checkRecordDigest(src, rec.OutputSHA256, name, line, "output"); err != nil {
		return records.CommandRecord{}, Source{}, err
	}
	if at, ok := regions["stderr"]; ok {
		stderr := Source{path: name, region: &lineRegion{r: r, off: at.off, n: at.n, line: line, field: "stderr"}, encoding: "utf-8"}
		if rec.StderrEncoding != nil {
			stderr.encoding = *rec.StderrEncoding
		}
		if rec.StderrSHA256 != nil {
			stderr.digest = *rec.StderrSHA256
		}
		src = src.WithStderr(stderr)
	} else if stderr, ok := src.Stderr(); ok && rec.StderrSHA256 != nil {
		if err := checkRecordDigest(stderr, *rec.StderrSHA256, name, line, "stderr"); err != nil {
			return records.CommandRecord{}, Source{}, err
		}
	}
	return rec, src, nil
}

// checkRecordDigest compares the digest of a string the record carries,
// field (output or stderr) of line, with the record's, digest, as a
// region's pass does at its end: a line whose string is not the bytes its
// digest names is run_output_records_unreadable, the two stacked; "" is a
// record without one.
func checkRecordDigest(src Source, digest, name string, line int64, field string) error {
	if digest == "" {
		return nil
	}
	h := sha256.New()
	if err := src.WriteRaw(h); err != nil {
		return decodeFailed(name, line, "the %s's base64: %v", field, err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != digest {
		return notTheRecords(name, line, field, digest, got)
	}
	return nil
}

// notTheRecords is a string read back whose digest is not the record's.
func notTheRecords(name string, line int64, field, want, got string) error {
	return errorcodes.Errorf("run_output_records_unreadable", "%s line %d's %s is not the record's\n  record  %s\n  read    %s", name, line, field, want, got)
}

// stringAt is a JSON string's content in the file: the offset of its first
// byte after the opening quote, and its length to the closing quote.
type stringAt struct{ off, n int64 }

// lineScanner reads one line's record object, copying it into a small
// buffer but for the large output and stderr strings, whose places it
// notes instead (object).
type lineScanner struct {
	br   *bufio.Reader
	pos  int64 // the offset in the file of the next byte
	name string
	line int64
}

// object reads the line: the record's object, then its LF and the line's
// end. small is the object with each streamed string empty; regions holds
// those strings' places, by key.
func (s *lineScanner) object() (small []byte, regions map[string]stringAt, err error) {
	small = make([]byte, 0, 4096)
	regions = map[string]stringAt{}
	s.skipSpace()
	if c, err := s.next(); err != nil {
		return nil, nil, err
	} else if c != '{' {
		return nil, nil, s.malformed("the line does not begin a JSON object")
	}
	small = append(small, '{')
	s.skipSpace()
	if c, err := s.peek(); err != nil {
		return nil, nil, err
	} else if c == '}' {
		s.discard(1)
		small = append(small, '}')
	} else {
		for {
			s.skipSpace()
			if c, err := s.next(); err != nil {
				return nil, nil, err
			} else if c != '"' {
				return nil, nil, s.malformed("a field's name is not a string")
			}
			keyAt := len(small)
			small = append(small, '"')
			if small, _, _, err = s.str(small, math.MaxInt64); err != nil {
				return nil, nil, err
			}
			var key string
			_ = json.Unmarshal(small[keyAt:], &key)
			for _, streamed := range []string{"output", "stderr"} {
				if strings.EqualFold(key, streamed) {
					key = streamed
				}
			}
			s.skipSpace()
			if c, err := s.next(); err != nil {
				return nil, nil, err
			} else if c != ':' {
				return nil, nil, s.malformed("a field's name is not followed by its value")
			}
			small = append(small, ':')
			s.skipSpace()
			c, err := s.peek()
			if err != nil {
				return nil, nil, err
			}
			if (key == "output" || key == "stderr") && c == '"' {
				if _, twice := regions[key]; twice {
					return nil, nil, s.malformed("the record has two %q fields", key)
				}
				s.discard(1)
				small = append(small, '"')
				head, off := len(small), s.pos
				var n int64
				var streamed bool
				if small, n, streamed, err = s.str(small, streamedLineThreshold); err != nil {
					return nil, nil, err
				}
				if streamed {
					small = append(small[:head], '"')
					regions[key] = stringAt{off: off, n: n}
				}
			} else if small, err = s.value(small); err != nil {
				return nil, nil, err
			}
			s.skipSpace()
			c, err = s.next()
			if err != nil {
				return nil, nil, err
			}
			small = append(small, c)
			if c == '}' {
				break
			}
			if c != ',' {
				return nil, nil, s.malformed("a field is not followed by a comma or the object's end")
			}
		}
	}
	if c, err := s.next(); err != nil {
		return nil, nil, err
	} else if c != '\n' {
		return nil, nil, s.malformed("the record's object is not followed by the line's end")
	}
	if _, err := s.br.ReadByte(); err == nil {
		return nil, nil, s.malformed("the line goes on after its LF")
	} else if !errors.Is(err, io.EOF) {
		return nil, nil, s.unreadable(err)
	}
	return small, regions, nil
}

// str reads a string's content after its opening quote, through its
// closing quote, checking each escape and refusing a control byte as JSON
// does, and appends it, the closing quote too, to small while its length
// stays at most limit; past it, streamed is set and small keeps what it
// had been given and what was appended before (the caller cuts it back).
// n is the content's length, without the quotes.
func (s *lineScanner) str(small []byte, limit int64) (out []byte, n int64, streamed bool, err error) {
	keep := func(b []byte) {
		n += int64(len(b))
		if !streamed && n > limit {
			streamed = true
		}
		if !streamed {
			small = append(small, b...)
		}
	}
	for {
		if _, err := s.br.Peek(1); err != nil {
			return small, n, streamed, s.ended(err)
		}
		data, _ := s.br.Peek(s.br.Buffered())
		i := 0
		for i < len(data) && data[i] != '"' && data[i] != '\\' && data[i] >= 0x20 {
			i++
		}
		if i > 0 {
			keep(data[:i])
			s.discard(i)
			continue
		}
		switch c := data[0]; {
		case c == '"':
			s.discard(1)
			if !streamed {
				small = append(small, '"')
			}
			return small, n, streamed, nil
		case c < 0x20:
			return small, n, streamed, s.malformed("a string holds the control byte %#02x", c)
		}
		// A backslash: its escape whole, a \u with its four digits.
		b, err := s.br.Peek(2)
		if len(b) < 2 {
			return small, n, streamed, s.ended(err)
		}
		switch b[1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			keep(b[:2])
			s.discard(2)
		case 'u':
			b, err = s.br.Peek(6)
			if len(b) < 6 {
				return small, n, streamed, s.ended(err)
			}
			if _, ok := hex4(b[2:6]); !ok {
				return small, n, streamed, s.malformed("a string holds the escape %q", b)
			}
			keep(b[:6])
			s.discard(6)
		default:
			return small, n, streamed, s.malformed("a string holds the escape %q", b)
		}
	}
}

// value copies a value that is not streamed into small: a string, an
// object or an array to its closing bracket (its strings read by str, so
// a bracket inside one is text), or a number or literal to the next
// delimiter. json.Unmarshal of the whole checks the rest.
func (s *lineScanner) value(small []byte) ([]byte, error) {
	c, err := s.peek()
	if err != nil {
		return small, err
	}
	switch c {
	case '"':
		s.discard(1)
		small, _, _, err = s.str(append(small, '"'), math.MaxInt64)
		return small, err
	case '{', '[':
		depth := 0
		for {
			c, err := s.peek()
			if err != nil {
				return small, err
			}
			s.discard(1)
			switch c {
			case '"':
				if small, _, _, err = s.str(append(small, '"'), math.MaxInt64); err != nil {
					return small, err
				}
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			case '\n':
				return small, s.malformed("the line ends inside a value")
			}
			small = append(small, c)
			if depth == 0 {
				return small, nil
			}
		}
	}
	for {
		c, err := s.peek()
		if err != nil {
			return small, err
		}
		switch c {
		case ',', '}', ']', ' ', '\t', '\r', '\n':
			return small, nil
		}
		s.discard(1)
		small = append(small, c)
	}
}

func (s *lineScanner) next() (byte, error) {
	c, err := s.br.ReadByte()
	if err != nil {
		return 0, s.ended(err)
	}
	s.pos++
	return c, nil
}

func (s *lineScanner) peek() (byte, error) {
	b, err := s.br.Peek(1)
	if len(b) == 0 {
		return 0, s.ended(err)
	}
	return b[0], nil
}

func (s *lineScanner) discard(n int) {
	d, _ := s.br.Discard(n)
	s.pos += int64(d)
}

// skipSpace passes JSON's white space but the LF, which ends the line.
func (s *lineScanner) skipSpace() {
	for {
		b, _ := s.br.Peek(1)
		if len(b) == 0 || (b[0] != ' ' && b[0] != '\t' && b[0] != '\r') {
			return
		}
		s.discard(1)
	}
}

// ended is a read that stopped: the line's end before its record's (the
// file shorter than the line FindLines found), or a failed read.
func (s *lineScanner) ended(err error) error {
	if err == nil || errors.Is(err, io.EOF) {
		return errorcodes.Errorf("run_output_records_unreadable", "%s line %d ends before its record does", s.name, s.line)
	}
	return s.unreadable(err)
}

func (s *lineScanner) unreadable(err error) error {
	return errorcodes.Errorf("run_output_records_unreadable", "read %s line %d: %w", s.name, s.line, err)
}

func (s *lineScanner) malformed(format string, args ...any) error {
	return decodeFailed(s.name, s.line, format, args...)
}

func decodeFailed(name string, line int64, format string, args ...any) error {
	return errorcodes.Errorf("run_output_record_decode_failed", "decode %s line %d: "+format, append([]any{name, line}, args...)...)
}

// hex4 is the value of four hex digits, as a \u escape writes them.
func hex4(b []byte) (rune, bool) {
	var r rune
	for _, c := range b {
		switch {
		case '0' <= c && c <= '9':
			c -= '0'
		case 'a' <= c && c <= 'f':
			c = c - 'a' + 10
		case 'A' <= c && c <= 'F':
			c = c - 'A' + 10
		default:
			return 0, false
		}
		r = r*16 + rune(c)
	}
	return r, true
}

// lineRegion is a streamed string's place in a line read back: the file
// (the caller's, which a source's Close leaves open), the content's offset
// and length, and the line's number and the field's name (output, stderr)
// for the messages.
type lineRegion struct {
	r      io.ReaderAt
	off, n int64
	line   int64
	field  string
}

// open is one pass over the region: the device's bytes, the string's
// escapes undone and, under base64, the text decoded, from exactly the
// region's bytes (a file cut short since its line was read is
// run_output_records_unreadable), and at the pass's end their digest
// compared with the record's, digest (the bytes then given are not the
// record's output: the file changed since the job wrote it).
func (g *lineRegion) open(name, encoding, digest string) *bufio.Reader {
	exact := &exactReader{r: io.NewSectionReader(g.r, g.off, g.n), left: g.n, name: name, line: g.line}
	var r io.Reader = &unescaper{br: bufio.NewReaderSize(exact, readbackBuffer), name: name, line: g.line}
	if encoding == "base64" {
		r = base64Reader{r: base64.NewDecoder(base64.StdEncoding, r), name: name, line: g.line}
	}
	if digest != "" {
		r = &digestReader{r: r, h: sha256.New(), want: digest, name: name, line: g.line, field: g.field}
	}
	return bufio.NewReaderSize(r, lineChunk)
}

// exactReader reads a region's bytes, every one: the file's end before the
// region's is a file cut short, run_output_records_unreadable.
type exactReader struct {
	r    io.Reader
	left int64
	name string
	line int64
}

func (e *exactReader) Read(p []byte) (int, error) {
	n, err := e.r.Read(p)
	e.left -= int64(n)
	if errors.Is(err, io.EOF) && e.left > 0 {
		err = errorcodes.Errorf("run_output_records_unreadable", "%s line %d ends before its record does", e.name, e.line)
	}
	return n, err
}

// digestReader hashes the bytes it gives and, at their end, compares the
// digest with the record's.
type digestReader struct {
	r     io.Reader
	h     hash.Hash
	want  string
	name  string
	line  int64
	field string
}

func (d *digestReader) Read(p []byte) (int, error) {
	n, err := d.r.Read(p)
	d.h.Write(p[:n])
	if errors.Is(err, io.EOF) {
		if got := hex.EncodeToString(d.h.Sum(nil)); got != d.want {
			err = notTheRecords(d.name, d.line, d.field, d.want, got)
		}
	}
	return n, err
}

// base64Reader codes the decoder's refusal of its text.
type base64Reader struct {
	r    io.Reader
	name string
	line int64
}

func (b base64Reader) Read(p []byte) (int, error) {
	n, err := b.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && errorcodes.Of(err) == "" {
		err = decodeFailed(b.name, b.line, "the output's base64: %v", err)
	}
	return n, err
}

// unescaper reads a JSON string's content, its quotes outside, and gives
// its bytes as encoding/json decodes them: each escape undone, a UTF-16
// surrogate pair joined and a lone surrogate U+FFFD, and each byte that
// does not begin a UTF-8 character U+FFFD, a control byte refused. A line
// the store wrote holds no invalid UTF-8 and no surrogate escape, but a
// line read back decodes as json.Unmarshal would decode it.
type unescaper struct {
	br      *bufio.Reader
	pending [utf8.UTFMax]byte // a decoded character not yet given
	ip, np  int
	name    string
	line    int64
}

func (u *unescaper) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		if u.ip < u.np {
			c := copy(p[n:], u.pending[u.ip:u.np])
			u.ip += c
			n += c
			continue
		}
		if b, err := u.peek(1); err != nil {
			return n, err
		} else if len(b) == 0 {
			if n > 0 {
				return n, nil
			}
			return 0, io.EOF
		}
		data, _ := u.br.Peek(u.br.Buffered())
		budget := len(p) - n
		i := 0
		for i < len(data) && i < budget {
			c := data[i]
			if c == '\\' || c < 0x20 {
				break
			}
			if c < utf8.RuneSelf {
				i++
				continue
			}
			// A character cut by the buffer's end is (RuneError, 1) too:
			// special reads it whole.
			r, size := utf8.DecodeRune(data[i:])
			if (r == utf8.RuneError && size == 1) || i+size > budget {
				break
			}
			i += size
		}
		if i > 0 {
			copy(p[n:], data[:i])
			u.br.Discard(i)
			n += i
			continue
		}
		if err := u.special(data[0]); err != nil {
			return n, err
		}
	}
	return n, nil
}

// special decodes what the fast path stops at, c being its first byte,
// into pending: an escape, or a byte of 0x80 or more that is not a whole
// character within the buffer, too large for the reader's room, or not
// UTF-8 at all.
func (u *unescaper) special(c byte) error {
	u.ip, u.np = 0, 0
	switch {
	case c == '\\':
		return u.escape()
	case c < 0x20:
		return decodeFailed(u.name, u.line, "a string holds the control byte %#02x", c)
	}
	b, err := u.peek(utf8.UTFMax)
	if err != nil {
		return err
	}
	r, size := utf8.DecodeRune(b)
	if r == utf8.RuneError && size == 1 {
		u.np = utf8.EncodeRune(u.pending[:], unicode.ReplacementChar)
	} else {
		u.np = copy(u.pending[:], b[:size])
	}
	u.br.Discard(size)
	return nil
}

// peek is the next n bytes, fewer at the string's end; a read that failed
// is returned, since bufio reports it once.
func (u *unescaper) peek(n int) ([]byte, error) {
	b, err := u.br.Peek(n)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		if errorcodes.Of(err) == "" {
			err = errorcodes.Errorf("run_output_records_unreadable", "read %s line %d: %w", u.name, u.line, err)
		}
		return nil, err
	}
	return b, nil
}

func (u *unescaper) escape() error {
	b, err := u.peek(2)
	if err != nil {
		return err
	}
	if len(b) < 2 {
		return decodeFailed(u.name, u.line, "a string ends in a backslash")
	}
	var r rune
	switch b[1] {
	case '"', '\\', '/':
		r = rune(b[1])
	case 'b':
		r = '\b'
	case 'f':
		r = '\f'
	case 'n':
		r = '\n'
	case 'r':
		r = '\r'
	case 't':
		r = '\t'
	case 'u':
		if b, err = u.peek(6); err != nil {
			return err
		}
		var ok bool
		if len(b) == 6 {
			r, ok = hex4(b[2:6])
		}
		if !ok {
			return decodeFailed(u.name, u.line, "a string holds the escape %q", b)
		}
		u.br.Discard(6)
		if utf16.IsSurrogate(r) {
			// The pair's second half when it follows, else U+FFFD and the
			// next escape read on its own, as encoding/json does.
			next, err := u.peek(6)
			if err != nil {
				return err
			}
			r2, ok := rune(0), false
			if len(next) == 6 && next[0] == '\\' && next[1] == 'u' {
				r2, ok = hex4(next[2:6])
			}
			if dec := utf16.DecodeRune(r, r2); ok && dec != unicode.ReplacementChar {
				u.br.Discard(6)
				r = dec
			} else {
				r = unicode.ReplacementChar
			}
		}
		u.np = utf8.EncodeRune(u.pending[:], r)
		return nil
	default:
		return decodeFailed(u.name, u.line, "a string holds the escape %q", b)
	}
	u.br.Discard(2)
	u.np = utf8.EncodeRune(u.pending[:], r)
	return nil
}
