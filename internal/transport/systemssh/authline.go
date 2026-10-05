package systemssh

import (
	"bytes"
	"io"
	"regexp"
	"sync"
)

// authenticatedLine is the line OpenSSH writes at LogLevel VERBOSE once the
// server accepts the client: the method is OpenSSH's own name for it.
var authenticatedLine = regexp.MustCompile(`^Authenticated to .* using "([a-z-]+)"\.\r?$`)

// authFilter stands between OpenSSH's stderr and the diagnostics: it takes
// the line naming the method that authenticated out of the stream and
// passes every other line on whole, so the diagnostics classify a failure
// as they did at LogLevel INFO. A line is held until its newline; Flush
// passes on what is left when the stream ends.
type authFilter struct {
	next io.Writer

	mu      sync.Mutex
	pending []byte
	method  string
}

func (f *authFilter) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending = append(f.pending, p...)
	for {
		i := bytes.IndexByte(f.pending, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := f.pending[:i+1]
		if m := authenticatedLine.FindSubmatch(bytes.TrimSuffix(line, []byte("\n"))); m != nil {
			f.method = string(m[1])
		} else if _, err := f.next.Write(line); err != nil {
			return len(p), err
		}
		f.pending = f.pending[i+1:]
	}
}

// Flush passes on a last line without a newline.
func (f *authFilter) Flush() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) > 0 {
		_, _ = f.next.Write(f.pending)
		f.pending = nil
	}
}

// Method is the method OpenSSH said authenticated, "" before it said so.
func (f *authFilter) Method() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.method
}
