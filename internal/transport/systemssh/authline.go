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

// enrolledLine is the line OpenSSH writes at LogLevel INFO and above when it
// stores an unknown host's key under StrictHostKeyChecking accept-new; the
// label is its name for the key type (ED25519, ECDSA, RSA). A process that
// finds the key stored meanwhile writes none.
var enrolledLine = regexp.MustCompile(`^Warning: Permanently added '.*' \(([A-Z0-9-]+)\) to the list of known hosts\.\r?$`)

// enrolledLabel is the key type's label when text is OpenSSH's enrollment
// line.
func enrolledLabel(text string) (string, bool) {
	if g := enrolledLine.FindStringSubmatch(text); g != nil {
		return g[1], true
	}
	return "", false
}

// authFilter stands between OpenSSH's stderr and the diagnostics: it takes
// the line naming the method that authenticated, and the line saying the
// host's key was stored (to enrolled), out of the stream and passes every
// other line on whole, so the diagnostics classify a failure as they did at
// LogLevel INFO. A line is held until its newline; Flush passes on what is
// left when the stream ends.
type authFilter struct {
	next     io.Writer
	enrolled func(label string) // the request's host_key_enrolled notice, or nil (Driver.hostKeyEnrolled)

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
		text := bytes.TrimSuffix(line, []byte("\n"))
		if m := authenticatedLine.FindSubmatch(text); m != nil {
			f.method = string(m[1])
		} else if label, ok := enrolledLabel(string(text)); ok {
			if f.enrolled != nil {
				f.enrolled(label)
			}
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
