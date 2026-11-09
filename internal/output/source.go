package output

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"math"
	"os"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// Source is where a record's output bytes are: in
// the record's Output string, encoded as the record carries it (the text,
// or base64 of bytes that were not valid UTF-8), or in the command's spool
// file, the device's recorded bytes as the reader settled them (a spooled
// response is whole in its file and the record's Output is empty). Every consumer of a record's output streams from it through the
// two methods here: the line's escaped pieces (writeEscaped) and the
// device's own bytes for the text and collection blocks (WriteRaw), so a
// response is never read whole into memory by the store, whatever its
// size. The string form is the common small response and its path is as
// it was before the spool.
type Source struct {
	text     string     // the record's Output, when the output is in the record
	encoding string     // utf-8 or base64, the record's output_encoding
	path     string     // the spool file, when the output is there
	spool    *spoolFile // the spool opened once, read by every pass
	bytes    int64      // the spool's settled count
	digest   string     // the reader's running SHA-256 of the spool's bytes, hex
	// stderr is an exec record's standard error where it is, by the same
	// rules; nil on a shell record and for an exec command that did not
	// run. The record's line streams it as it streams the output.
	stderr *Source
	// region, with path the file's, is the output inside a commands.jsonl
	// line read back (ReadRecordAt): a spool to every pass, which reads the
	// device's bytes from it, digest being the record's own.
	region *lineRegion
}

// FromRecord is the output as the record carries it: its Output string
// under its encoding.
func FromRecord(r *records.CommandRecord) Source {
	s := Source{text: r.Output, encoding: r.OutputEncoding}
	if r.Stderr != nil {
		encoding := "utf-8"
		if r.StderrEncoding != nil {
			encoding = *r.StderrEncoding
		}
		s.stderr = &Source{text: *r.Stderr, encoding: encoding}
	}
	return s
}

// WithStderr is s with an exec command's standard error where it is: the
// record's string (FromText of a record's stderr, under its encoding) or
// its spool.
func (s Source) WithStderr(stderr Source) Source {
	s.stderr = &stderr
	return s
}

// Stderr is the standard error beside the output, when the record is an
// exec command's that ran.
func (s Source) Stderr() (Source, bool) {
	if s.stderr == nil {
		return Source{}, false
	}
	return *s.stderr, true
}

// FromEncoded is a stream as a record carries it: its string under its
// encoding.
func FromEncoded(text, encoding string) Source { return Source{text: text, encoding: encoding} }

// FromText is plain text that is not a record's (a set-up line's answer).
func FromText(text string) Source { return Source{text: text, encoding: "utf-8"} }

// spoolFile is a spool opened once, without following a link: every pass
// reads it from its start through this descriptor, so each reads the file
// the measuring pass verified, whatever is put at its path later. err is
// the open's failure, which every pass reports.
type spoolFile struct {
	f   *os.File
	err error
}

// FromSpool is the output in its spool file: the settled bytes, their
// count, the reader's digest of them, and the encoding the record takes
// (utf-8 when they are valid UTF-8, else base64). The file is opened here,
// once, and stays open until Close.
func FromSpool(path string, bytes int64, digest, encoding string) Source {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	return Source{path: path, spool: &spoolFile{f: f, err: err}, bytes: bytes, digest: digest, encoding: encoding}
}

// Close closes the spools' descriptors, the output's and the standard
// error's, once every pass is done: the executor's, before it removes the
// files. A source without a spool has nothing to close, nor one read back
// from a line (ReadRecordAt), whose file is the caller's.
func (s Source) Close() error {
	var err error
	for _, src := range []*Source{&s, s.stderr} {
		if src == nil || src.spool == nil || src.spool.f == nil {
			continue
		}
		if closeErr := src.spool.f.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

// Spooled says whether the output is in a file.
func (s Source) Spooled() bool { return s.path != "" }

// AnySpooled says whether the output or the standard error is in a file.
func (s Source) AnySpooled() bool { return s.Spooled() || (s.stderr != nil && s.stderr.Spooled()) }

// Empty says whether there is no output at all; the standard error is
// asked of its own source.
func (s Source) Empty() bool { return s.path == "" && s.text == "" }

// open is the spool from its start for one pass, read through the one
// descriptor (ReadAt, so no pass moves another's offset); a pass reads the
// file once.
func (s Source) open() (*bufio.Reader, error) {
	if s.region != nil {
		return s.region.open(s.path, s.encoding, s.digest), nil
	}
	if s.spool.err != nil {
		return nil, errorcodes.Errorf("output_write_failed", "the output spool %s could not be read for the record: %v", s.path, s.spool.err)
	}
	return bufio.NewReaderSize(io.NewSectionReader(s.spool.f, 0, math.MaxInt64), lineChunk), nil
}

// WriteRaw writes the device's recorded bytes: the text as it is, base64
// text decoded as it streams, or the spool file copied. The text and the
// collection blocks take them (writeTextAnswer).
func (s Source) WriteRaw(w io.Writer) error {
	if s.path == "" {
		if s.encoding == "base64" {
			_, err := io.Copy(w, base64.NewDecoder(base64.StdEncoding, strings.NewReader(s.text)))
			return err
		}
		_, err := io.WriteString(w, s.text)
		return err
	}
	r, err := s.open()
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	return err
}

// writeEscaped writes the output as the content of the record line's JSON
// string, without its quotes: the text escaped by json.Marshal lineChunk
// bytes at a time, each chunk ending between characters, so the escaping
// cannot differ from the whole record's; a spool of bytes that were not
// valid UTF-8 streamed through base64, which needs no escape. When h is
// given, the spool's bytes are hashed as they are read (the measuring
// pass).
func (s Source) writeEscaped(w io.Writer, h hash.Hash) error {
	if s.path == "" {
		return escapeText(w, s.text)
	}
	r, err := s.open()
	if err != nil {
		return err
	}
	var src io.Reader = r
	if h != nil {
		src = io.TeeReader(r, h)
	}
	if s.encoding == "base64" {
		enc := base64.NewEncoder(base64.StdEncoding, w)
		if _, err = io.Copy(enc, src); err == nil {
			err = enc.Close()
		}
	} else {
		err = escapeStream(w, src)
	}
	return err
}

// escapeText escapes text a chunk at a time, each chunk cut between
// characters: json.Marshal would replace the halves of a split one.
func escapeText(w io.Writer, text string) error {
	for rest := text; len(rest) > 0; {
		n := min(len(rest), lineChunk)
		for n < len(rest) && !utf8.RuneStart(rest[n]) {
			n--
		}
		if err := writeEscapedChunk(w, rest[:n]); err != nil {
			return err
		}
		rest = rest[n:]
	}
	return nil
}

// escapeStream escapes a stream of valid UTF-8 a chunk at a time: a
// character cut by the buffer's end is carried to the next chunk.
func escapeStream(w io.Writer, r io.Reader) error {
	buf := make([]byte, lineChunk+utf8.UTFMax)
	carry := 0
	for {
		n, err := io.ReadFull(r, buf[carry:lineChunk])
		n += carry
		eof := err == io.EOF || err == io.ErrUnexpectedEOF
		if err != nil && !eof {
			return err
		}
		cut := n
		if !eof {
			// The last character start within the final bytes: when its
			// character is not whole, it waits for the next chunk.
			for k := n - 1; k >= 0 && k >= n-utf8.UTFMax; k-- {
				if utf8.RuneStart(buf[k]) {
					if !utf8.FullRune(buf[k:n]) {
						cut = k
					}
					break
				}
			}
		}
		if cut > 0 {
			if err := writeEscapedChunk(w, string(buf[:cut])); err != nil {
				return err
			}
		}
		carry = copy(buf, buf[cut:n])
		if eof {
			return nil
		}
	}
}

// writeEscapedChunk writes one chunk escaped by json.Marshal, its quotes
// removed.
func writeEscapedChunk(w io.Writer, chunk string) error {
	escaped, err := json.Marshal(chunk)
	if err != nil {
		return err
	}
	n, err := w.Write(escaped[1 : len(escaped)-1])
	if err == nil && n != len(escaped)-2 {
		err = io.ErrShortWrite
	}
	return err
}

// verifier hashes a spool's bytes on the measuring pass and compares them
// with the reader's digest: a difference is
// output_spool_mismatch, and nothing of the line has reached a file.
type verifier struct {
	h hash.Hash
}

func (s Source) verifier() *verifier {
	if !s.Spooled() {
		return nil
	}
	return &verifier{h: sha256.New()}
}

func (v *verifier) hash() hash.Hash {
	if v == nil {
		return nil
	}
	return v.h
}

// check compares the pass's digest with the reader's.
func (v *verifier) check(s Source) error {
	if v == nil {
		return nil
	}
	if got := hex.EncodeToString(v.h.Sum(nil)); got != s.digest {
		return errorcodes.Errorf("output_spool_mismatch", "the output spool %s does not hold the bytes the session read: digest %s, the session's %s; the record is not written", s.path, got, s.digest)
	}
	return nil
}
