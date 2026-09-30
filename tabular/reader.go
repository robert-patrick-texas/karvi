// Package tabular provides the single explicit CSV reader used by inventory
// and by the credential CSV backend. It never sniffs header
// presence. A caller chooses whether cells are trimmed: inventory trims
// every cell; a credential file keeps its cells verbatim, since a secret
// may begin or end with a space, and trims its own non-secret cells.
package tabular

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

type Spec struct {
	Delimiter            rune
	Mode                 string // header or numeric
	CommentPrefix        byte
	Requested            []string
	Aliases              map[string][]string
	Mandatory            []string
	NumericMappings      map[string]int // one-based columns
	Trim                 bool           // strip leading and trailing white space from every cell; false keeps cells verbatim
	MaxPhysicalLineBytes int
	ShortRowPolicy       string // warn-skip or error
	Warn                 func(string)
	// CheckHeader, when set, is asked about every header of a header-mode
	// file, mapped or not, in column order (one-based), before the columns
	// are mapped; the first error it returns is the read's error, unchanged,
	// so its code is the caller's. The reader knows nothing of what a caller
	// refuses: the inventory loader uses the hook to refuse a secret column
	// Numeric mode has no headers and never calls it.
	CheckHeader func(column int, header string) error
}

type Record struct {
	Values map[string]string
	Line   int
}

type Iterator interface {
	Next() bool
	Record() Record
	Err() error
}
type Reader interface {
	Read(ctx context.Context, r io.Reader, spec Spec) (Iterator, error)
}
type CSVReader struct{}

type iterator struct {
	ctx      context.Context
	csv      *csv.Reader
	mapping  map[string]int
	maxIndex int
	spec     Spec
	current  Record
	err      error
}

func (CSVReader) Read(ctx context.Context, r io.Reader, spec Spec) (Iterator, error) {
	if spec.Delimiter == 0 {
		spec.Delimiter = ','
	}
	if !utf8.ValidRune(spec.Delimiter) {
		return nil, errorcodes.Errorf("tabular_delimiter_invalid", "delimiter must be one valid rune")
	}
	if spec.Mode == "" {
		spec.Mode = "header"
	}
	if spec.Mode != "header" && spec.Mode != "numeric" {
		return nil, errorcodes.Errorf("tabular_mode_invalid", "mode must be header or numeric")
	}
	if spec.CommentPrefix == 0 {
		spec.CommentPrefix = '#'
	}
	if spec.CommentPrefix != '#' {
		return nil, errorcodes.Errorf("tabular_comment_prefix_invalid", "v1 comment prefix must be #")
	}
	if spec.MaxPhysicalLineBytes == 0 {
		spec.MaxPhysicalLineBytes = 1 << 20
	}
	if spec.ShortRowPolicy == "" {
		spec.ShortRowPolicy = "warn-skip"
	}
	lr := newLineLimitReader(r, spec.MaxPhysicalLineBytes)
	cr := csv.NewReader(lr)
	cr.Comma = spec.Delimiter
	cr.Comment = rune(spec.CommentPrefix)
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = false
	mapping := map[string]int{}
	if spec.Mode == "header" {
		header, err := cr.Read()
		if err != nil {
			return nil, errorcodes.Errorf("tabular_header_unreadable", "read header: %w", err)
		}
		index := map[string]int{}
		for i, h := range header {
			key := canonicalHeader(h)
			if key == "" {
				return nil, errorcodes.Errorf("tabular_header_blank", "blank header at column %d", i+1)
			}
			if spec.CheckHeader != nil {
				if err := spec.CheckHeader(i+1, h); err != nil {
					return nil, err
				}
			}
			if _, exists := index[key]; exists {
				return nil, errorcodes.Errorf("tabular_header_duplicate", "duplicate header %q", h)
			}
			index[key] = i
		}
		for _, logical := range spec.Requested {
			candidates := append([]string{logical}, spec.Aliases[logical]...)
			found := -1
			for _, candidate := range candidates {
				if i, ok := index[canonicalHeader(candidate)]; ok {
					found = i
					break
				}
			}
			if found >= 0 {
				mapping[logical] = found
			}
		}
	} else {
		for _, logical := range spec.Requested {
			if col, ok := spec.NumericMappings[logical]; ok && col > 0 {
				mapping[logical] = col - 1
			}
		}
	}
	for _, m := range spec.Mandatory {
		if _, ok := mapping[m]; !ok {
			return nil, errorcodes.Errorf("tabular_mapping_missing", "mandatory field %q has no mapping", m)
		}
	}
	max := -1
	for _, i := range mapping {
		if i > max {
			max = i
		}
	}
	return &iterator{ctx: ctx, csv: cr, mapping: mapping, maxIndex: max, spec: spec}, nil
}

func (it *iterator) Next() bool {
	if it.err != nil {
		return false
	}
	for {
		select {
		case <-it.ctx.Done():
			it.err = it.ctx.Err()
			return false
		default:
		}
		row, err := it.csv.Read()
		if err == io.EOF {
			return false
		}
		if err != nil {
			it.err = errorcodes.Ensure(err, "tabular_row_malformed")
			return false
		}
		line := 0
		if len(row) > 0 {
			line, _ = it.csv.FieldPos(0)
		}
		if it.maxIndex >= len(row) {
			msg := fmt.Sprintf("short CSV row at line %d: need column %d, got %d", line, it.maxIndex+1, len(row))
			if it.spec.ShortRowPolicy == "error" {
				it.err = errorcodes.Errorf("tabular_row_short", "%s", msg)
				return false
			}
			if it.spec.Warn != nil {
				it.spec.Warn(msg)
			}
			continue
		}
		values := make(map[string]string, len(it.mapping))
		for k, i := range it.mapping {
			v := row[i]
			if it.spec.Trim {
				v = strings.TrimSpace(v)
			}
			values[k] = v
		}
		it.current = Record{Values: values, Line: line}
		return true
	}
}
func (it *iterator) Record() Record { return it.current }
func (it *iterator) Err() error     { return it.err }
func canonicalHeader(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '-' {
			return '_'
		}
		return unicode.ToLower(r)
	}, strings.TrimSpace(s))
}

// lineLimitReader feeds the CSV parser one physical line at a time,
// refusing an overlong line, and strips a UTF-8 byte-order mark from the
// start of the stream. The mark is stripped here, before the parser, so
// that both modes are served (numeric mode has no header row to clean, and
// the mark would otherwise sit at the front of the first row's first
// cell), and so that a first line that is a comment or begins with a quote
// parses as written.
type lineLimitReader struct {
	r       *bufio.Reader
	max     int
	pending []byte
	err     error
	started bool
}

var byteOrderMark = []byte{0xEF, 0xBB, 0xBF}

func newLineLimitReader(r io.Reader, max int) *lineLimitReader {
	return &lineLimitReader{r: bufio.NewReaderSize(r, min(max+1, 64*1024)), max: max}
}
func (l *lineLimitReader) Read(p []byte) (int, error) {
	if len(l.pending) == 0 && l.err == nil {
		line, err := l.r.ReadBytes('\n')
		if !l.started {
			l.started = true
			line = bytes.TrimPrefix(line, byteOrderMark)
		}
		if len(line) > l.max {
			l.err = errorcodes.Errorf("tabular_line_too_long", "physical line exceeds %d bytes", l.max)
			return 0, l.err
		}
		l.pending = line
		if err != nil {
			l.err = err
		}
	}
	if len(l.pending) > 0 {
		n := copy(p, l.pending)
		l.pending = l.pending[n:]
		return n, nil
	}
	if l.err != nil {
		err := l.err
		l.err = nil
		return 0, err
	}
	return 0, io.EOF
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
