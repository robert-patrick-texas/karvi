package output

import (
	"bytes"
	"io"
	"regexp"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The collection file's line filter: a platform's
// crun-filters, carried in the plan per platform, drop the output lines
// that match from the collection file and nowhere else. The filter sits in
// the block's stream between writeTextAnswer and the file, so a response is
// still written without a copy; it holds at most one partial line.

// compileCollectionFilters compiles the plan's lists per platform for the
// store. The plan validated them already; a pattern that does not compile
// here is the plan's fault, named as such.
func compileCollectionFilters(lists map[string][]string) (map[string][]*regexp.Regexp, error) {
	if len(lists) == 0 {
		return nil, nil
	}
	out := make(map[string][]*regexp.Regexp, len(lists))
	for name, list := range lists {
		res, i, err := platform.CompileFilters(list)
		if err != nil {
			return nil, errorcodes.Errorf("execution_plan_invalid", "platform_filters: platform %s pattern %d does not compile: %v", name, i+1, err)
		}
		if len(res) > 0 {
			out[name] = res
		}
	}
	return out, nil
}

// lineFilter writes complete lines through to w unless one matches a
// pattern; a line is matched without its terminator (a trailing CR is left
// out of the match too, and kept in the bytes written). The bytes of a line
// still open at the end of a Write wait for the next one or for flush.
type lineFilter struct {
	w   io.Writer
	res []*regexp.Regexp
	buf []byte
}

func (f *lineFilter) Write(p []byte) (int, error) {
	f.buf = append(f.buf, p...)
	for {
		i := bytes.IndexByte(f.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := f.buf[:i+1]
		if !f.drop(line[:i]) {
			if _, err := f.w.Write(line); err != nil {
				return 0, err
			}
		}
		f.buf = f.buf[i+1:]
	}
}

// flush writes a last line that had no terminator, or drops it.
func (f *lineFilter) flush() error {
	if len(f.buf) == 0 {
		return nil
	}
	line := f.buf
	f.buf = nil
	if f.drop(line) {
		return nil
	}
	_, err := f.w.Write(line)
	return err
}

func (f *lineFilter) drop(line []byte) bool {
	line = bytes.TrimSuffix(line, []byte("\r"))
	for _, re := range f.res {
		if re.Match(line) {
			return true
		}
	}
	return false
}
