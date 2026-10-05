package devsession

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/termtext"
	"github.com/robert-patrick-texas/karvi/platform"
)

// The streaming reader. A command's
// response is cleaned as it arrives into settled bytes, the recorded bytes
// in the order and values the whole-buffer cleanResponse of v0.12.1 gives
// over the rendered text (the reference kept in clean_response_test.go): the
// bytes rendered as the terminal showed them, chunk by chunk (termtext);
// the first line held until its newline and dropped when it
// is the echoed command, alone or after the previous prompt; the unsettled
// tail, the last line and the trailing whitespace before it, the only place
// a prompt, an escalate prompt, or a declaration is matched, held up to
// tailMax; at the end the tail gives up the returned prompt and the trailing
// blanks and the closing newline is added. What settles goes to memory up
// to output.spool-threshold-bytes and from the byte that would cross it to
// the command's spool file under spooldir; the running digest, the
// UTF-8 check, and the failure-pattern search run on the bytes as they
// settle; the limit counts settled bytes; and every ending hands
// the caller what settled and where it is.

// tailMax bounds the unsettled tail: one read chunk. A last
// line longer than this settles from its front and is no prompt candidate
// until its newline: the prompt patterns allow 240 characters at most, and
// a declared pattern matches a device's question, not a page.
const tailMax = 4096

// Spool is where a command's settled bytes go once they pass the threshold:
// the file <activity>.<device>.<index>.<pid>.spool,
// 0600 under Dir, named by output.SpoolName so the sweep at admission knows
// its owner. An empty Dir settles into memory only, whatever
// the threshold: the login, enable, and paging reads and a session
// opened without a spool directory. Threshold is output.spool-threshold-
// bytes; 0 spools every response from its first byte, and a
// value at or above the limit spools nothing, since the limit ends a
// response first.
type Spool struct {
	Dir       string
	Threshold int64
	Activity  string // the activity ID, the name's first field
	Device    string // the device's canonical name, the name's second field
	// Stderr names an exec command's stderr spool beside its stdout's
	// (output.StderrSpoolName).
	Stderr bool
}

// ForRequest is the session's spool for one device: the transport fills
// the device's name from the open request's canonical name, its address
// when the request carries none (a test's).
func (sp Spool) ForRequest(req platform.OpenRequest) Spool {
	sp.Device = req.Metadata["canonical_name"]
	if sp.Device == "" {
		sp.Device = req.Address
	}
	return sp
}

// limitError ends a read whose settled bytes would pass the limit:
// observed is the settled count at the cut, the bytes of
// the settling piece that did not fit included; the store holds exactly
// the first limit bytes.
type limitError struct {
	observed int64
	limit    int64
}

func (e *limitError) Error() string {
	return "output_limit_exceeded: device session response exceeded " + strconv.FormatInt(e.limit, 10) + " bytes before a prompt returned"
}

// settled is where one read's settled bytes go and what is known of them:
// memory up to the threshold, then the spool file
// with the settled head written first and the memory let go; the
// running SHA-256, the UTF-8 check with its carry, and the failure-pattern
// search with its carry, each over the bytes in the order they settle; the
// count, stopped at the limit. After the limit or a spool failure nothing
// more settles: stopped holds the error every later write returns.
type settled struct {
	limit    int64
	spool    Spool
	index    int
	opened   func(path string) // the debug line when the spool opens
	progress *atomic.Int64     // the scoreboard's count of settled bytes, nil when nobody watches
	mem      []byte
	file     *os.File
	path     string
	n        int64 // bytes stored, in memory or in the file
	observed int64 // bytes settled, those past the limit counted and not stored
	// shared, when set, is the count stored across an exec command's two
	// sinks, which the limit bounds together; the sinks are written from
	// one goroutine.
	shared   *int64
	hash     hash.Hash
	utf8     utf8Check
	failures failureScan
	stopped  error
}

// newSettled starts a sink: limit is output.max-command-bytes (0 or less
// takes the 64 MiB default the session applies), spool the command's spool
// (Dir "" for memory only), index the command's ordinal in the session for
// the spool's name, patterns the platform's failure patterns.
func newSettled(limit int64, spool Spool, index int, patterns []string, opened func(string)) *settled {
	if limit <= 0 {
		limit = 64 << 20
	}
	return &settled{limit: limit, spool: spool, index: index, opened: opened, hash: sha256.New(), failures: newFailureScan(patterns)}
}

// write settles p: counted, hashed, checked, scanned, and stored up to the
// limit. The piece that would pass the limit is stored up to it and ends
// the read with a limitError; a spool that cannot be opened or written
// ends it with output_spool_write_failed, the file removed
// and the memory let go, so the record holds nothing of the output.
func (st *settled) write(p []byte) error {
	if st.stopped != nil {
		return st.stopped
	}
	if len(p) == 0 {
		return nil
	}
	st.observed += int64(len(p))
	stored := st.n
	if st.shared != nil {
		stored = *st.shared
	}
	room := st.limit - stored
	over := int64(len(p)) > room
	if over {
		p = p[:max(room, 0)]
	}
	if len(p) > 0 {
		st.hash.Write(p)
		st.utf8.write(p)
		st.failures.scan(p)
		if err := st.store(p); err != nil {
			return err
		}
	}
	if over {
		st.stopped = &limitError{observed: st.observed, limit: st.limit}
		return st.stopped
	}
	return nil
}

// store puts p in memory while the threshold holds, else in the spool:
// the byte that would cross the threshold opens the file,
// the settled head goes in first, and every settled byte from there
// follows it. A response whose spool has no directory stays in
// memory.
func (st *settled) store(p []byte) error {
	if st.file == nil {
		if st.spool.Dir == "" || st.n+int64(len(p)) <= st.spool.Threshold {
			st.mem = append(st.mem, p...)
			st.stored(len(p))
			return nil
		}
		name := output.SpoolName(st.spool.Activity, st.spool.Device, st.index, os.Getpid())
		if st.spool.Stderr {
			name = output.StderrSpoolName(st.spool.Activity, st.spool.Device, st.index, os.Getpid())
		}
		path := filepath.Join(st.spool.Dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return st.failed(path, err)
		}
		st.file, st.path = f, path
		if st.opened != nil {
			st.opened(path)
		}
		if _, err := f.Write(st.mem); err != nil {
			return st.failed(path, err)
		}
		st.mem = nil
	}
	if _, err := st.file.Write(p); err != nil {
		return st.failed(st.path, err)
	}
	st.stored(len(p))
	return nil
}

// stored counts n bytes stored and publishes the settled count for the
// scoreboard: both of an exec command's streams when they share a count.
func (st *settled) stored(n int) {
	st.n += int64(n)
	count := st.n
	if st.shared != nil {
		*st.shared += int64(n)
		count = *st.shared
	}
	if st.progress != nil {
		st.progress.Store(count)
	}
}

// failed ends the sink on a spool error: the broken file is closed and
// removed here, the one removal the executor does not make,
// since nothing of it is handed over; the memory is let go.
func (st *settled) failed(path string, err error) error {
	if st.file != nil {
		_ = st.file.Close()
		_ = os.Remove(st.path)
		st.file = nil
	}
	st.mem, st.n = nil, 0
	st.stopped = errorcodes.Errorf("output_spool_write_failed", "the output spool %s could not be written: %v", path, err)
	return st.stopped
}

// output hands the settled bytes to the result: the memory
// as Output, or the spool closed and described as Spool with its count,
// digest, and encoding. After a spool failure nothing is handed over.
func (st *settled) output(res *platform.Result) {
	if _, failed := st.stopped.(*limitError); st.stopped != nil && !failed {
		return
	}
	if st.file != nil {
		_ = st.file.Close()
		st.file = nil
		res.Spool = &platform.Spool{Path: st.path, Bytes: st.n, SHA256: hex.EncodeToString(st.hash.Sum(nil)), UTF8: st.utf8.valid()}
		return
	}
	res.Output = st.mem
}

// discard drops what settled: the memory, and a spool file that was opened,
// for a read whose bytes reach nothing (the read after the enable secret).
func (st *settled) discard() {
	if st.file != nil {
		_ = st.file.Close()
		_ = os.Remove(st.path)
		st.file = nil
	}
	st.mem = nil
}

// utf8Check is utf8.Valid over bytes that arrive in pieces:
// a rune cut by a chunk border is carried, at most three bytes, until its
// remaining bytes arrive; an incomplete rune at the end is invalid, as
// utf8.Valid holds it.
type utf8Check struct {
	carry   [utf8.UTFMax]byte
	ncarry  int
	invalid bool
}

func (u *utf8Check) write(p []byte) {
	if u.invalid {
		return
	}
	// Complete the carried rune from the front of p.
	for u.ncarry > 0 && len(p) > 0 && !utf8.FullRune(u.carry[:u.ncarry]) {
		u.carry[u.ncarry] = p[0]
		u.ncarry++
		p = p[1:]
	}
	if u.ncarry > 0 {
		if !utf8.FullRune(u.carry[:u.ncarry]) {
			return // p is exhausted; the carry waits
		}
		if r, size := utf8.DecodeRune(u.carry[:u.ncarry]); r == utf8.RuneError && size == 1 {
			u.invalid = true
			return
		}
		u.ncarry = 0
	}
	if len(p) == 0 {
		return
	}
	// The last rune start within the final three bytes: when it begins an
	// incomplete rune, those bytes are carried and the rest is checked now.
	i := len(p)
	for k := len(p) - 1; k >= 0 && k >= len(p)-utf8.UTFMax+1; k-- {
		if utf8.RuneStart(p[k]) {
			if !utf8.FullRune(p[k:]) {
				i = k
			}
			break
		}
	}
	if !utf8.Valid(p[:i]) {
		u.invalid = true
		return
	}
	u.ncarry = copy(u.carry[:], p[i:])
}

// valid is utf8.Valid's answer for everything written.
func (u *utf8Check) valid() bool { return !u.invalid && u.ncarry == 0 }

// failureScan is deviceFailure over bytes that arrive in pieces: each
// piece is searched on its own and, with the carry of the longest pattern
// less one byte before it, across the border it shares
// with the previous piece, so a pattern cut by a chunk border is found
// without the whole response.
type failureScan struct {
	patterns []string
	keep     int // the longest pattern less one byte
	carry    []byte
	found    bool
}

func newFailureScan(patterns []string) failureScan {
	f := failureScan{}
	for _, p := range patterns {
		if p != "" {
			f.patterns = append(f.patterns, p)
			f.keep = max(f.keep, len(p)-1)
		}
	}
	return f
}

func (f *failureScan) scan(p []byte) {
	if f.found || len(f.patterns) == 0 {
		return
	}
	if len(f.carry) > 0 {
		// The border: the carry and as much of p as a pattern could reach.
		joint := append(f.carry, p[:min(len(p), f.keep)]...)
		if deviceFailure(f.patterns, joint) {
			f.found = true
			return
		}
		f.carry = joint[:len(f.carry)]
	}
	if deviceFailure(f.patterns, p) {
		f.found = true
		return
	}
	if len(p) >= f.keep {
		f.carry = append(f.carry[:0], p[len(p)-f.keep:]...)
	} else {
		f.carry = append(f.carry, p...)
		if len(f.carry) > f.keep {
			f.carry = append(f.carry[:0], f.carry[len(f.carry)-f.keep:]...)
		}
	}
}

// response is one read's streaming cleaner. The device's bytes are rendered
// as the terminal showed them (termtext, at no width, the spaces the device
// wrote kept), the prompt the read starts after seeded on the first line so
// the device's cursor moves land where they did; each finished line goes
// through the cleaner, and the unfinished line stays in the renderer, where a
// prompt or a declared question is matched. The tail holds what the ending
// may still trim; everything before it has settled into the sink.
type response struct {
	echo, promptEcho []byte // the echoed command, alone and after the previous prompt, trimmed
	sink             *settled
	render           *termtext.Renderer
	// tail is the unsettled finished lines: while first, the held first
	// line; after it, the trailing whitespace and the front of a last line
	// that gave it up (Break), never longer than tailMax (plus the echo's
	// own length while first).
	tail  []byte
	first bool
	// dropNL drops leading newlines while the tail is empty: the reference's
	// TrimLeft before the first line and again after the echo is removed.
	dropNL bool
	// overflow says the last line gave up its front: it is no prompt
	// candidate until its newline arrives.
	overflow bool
	// lines counts the finished lines; markLines and markCol are the
	// expecter's mark, the line and the offset in it a declaration is
	// matched after.
	lines, markLines, markCol int
	// rendered collects what the renderer finishes during one feed, taken
	// as one piece when the feed's bytes are rendered.
	rendered []byte
	err      error
}

// newResponse starts a cleaner for one read of command sent at previous
// (both "" for the login read and the read after the enable secret, which
// have no echo to remove and start a line of their own).
func newResponse(command, previous string, sink *settled) *response {
	r := &response{sink: sink, first: true, dropNL: true}
	if t := strings.TrimSpace(command); t != "" {
		r.echo = []byte(t)
		if previous != "" {
			r.promptEcho = []byte(strings.TrimSpace(previous + command))
		}
	}
	r.render = termtext.New(lineTaker{r}, 0)
	r.render.KeepSpaces = true
	r.render.Seed(previous)
	return r
}

// lineTaker collects the renderer's finished lines for the cleaner.
type lineTaker struct{ r *response }

func (t lineTaker) Write(p []byte) (int, error) {
	t.r.rendered = append(t.r.rendered, p...)
	return len(p), nil
}

// isEcho says whether the first line, trimmed, is the echoed command alone
// or after the previous prompt: an exact match, never a substring, so
// legitimate output is not discarded. A read without a command has none.
func (r *response) isEcho(line []byte) bool {
	if r.echo == nil {
		return false
	}
	t := bytes.TrimSpace(line)
	return bytes.Equal(t, r.echo) || (r.promptEcho != nil && bytes.Equal(t, r.promptEcho))
}

// feed takes one chunk of the device's bytes: rendered, the lines it
// finished taken as one piece, and an unfinished line past the bound made
// to give up its front.
func (r *response) feed(c []byte) error {
	if r.err != nil {
		return r.err
	}
	r.render.Write(c)
	limit := tailMax
	if r.first {
		limit += len(r.promptEcho) + len(r.echo)
	}
	broke := len(r.render.Pending()) > limit
	if broke {
		r.render.Break()
	}
	err := r.take(r.rendered)
	r.rendered = r.rendered[:0]
	if broke {
		r.overflow = true
	}
	return err
}

// take is the rendered text of one feed: finished lines, and the front a
// long line gave up (no newline). The first line is decided at its newline,
// and what the ending can no longer trim settles.
func (r *response) take(c []byte) error {
	if r.err != nil {
		return r.err
	}
	if len(c) == 0 {
		return nil
	}
	r.lines += bytes.Count(c, []byte{'\n'})
	if r.dropNL && len(r.tail) == 0 {
		c = bytes.TrimLeft(c, "\n")
		if len(c) == 0 {
			return nil
		}
		r.dropNL = false
	}
	r.tail = append(r.tail, c...)
	if r.first {
		i := bytes.IndexByte(r.tail, '\n')
		if i < 0 {
			return r.bound()
		}
		r.first = false
		if r.isEcho(r.tail[:i]) {
			r.dropFront(i + 1)
			// TrimLeft after the echo: the newlines that follow it go too.
			if trimmed := bytes.TrimLeft(r.tail, "\n"); len(trimmed) < len(r.tail) {
				r.dropFront(len(r.tail) - len(trimmed))
			}
			if len(r.tail) == 0 {
				r.dropNL = true
				return nil
			}
		}
	}
	return r.settle()
}

// settle moves to the sink what the ending can no longer trim: everything
// up to the last non-blank byte that a newline follows. What remains is
// the trailing whitespace and the front of a last line that gave it up,
// bounded by tailMax.
func (r *response) settle() error {
	nl := bytes.LastIndexByte(r.tail, '\n')
	if nl >= 0 {
		r.overflow = false
		if k := lastNonBlank(r.tail[:nl]); k >= 0 {
			if err := r.settleFront(k + 1); err != nil {
				return err
			}
		}
	}
	return r.bound()
}

// bound settles the tail's front past tailMax. While the first line is
// held the bound allows the echo's own length, so a long command's echo is
// still recognised whole; a line longer than that is not the echo and is
// output.
func (r *response) bound() error {
	limit := tailMax
	if r.first {
		limit += len(r.promptEcho) + len(r.echo)
	}
	if len(r.tail) <= limit {
		return nil
	}
	r.first = false
	return r.settleFront(len(r.tail) - limit)
}

// settleFront writes the tail's first k bytes to the sink and drops them.
func (r *response) settleFront(k int) error {
	if err := r.sink.write(r.tail[:k]); err != nil {
		r.err = err
		return err
	}
	r.dropFront(k)
	return nil
}

// dropFront removes the tail's first k bytes.
func (r *response) dropFront(k int) {
	n := copy(r.tail, r.tail[k:])
	r.tail = r.tail[:n]
}

// line is the last line as it stands, its trailing blanks kept: the
// unfinished line, after the front it gave up when it was too long. An
// expectation is matched against it, so a device's value prompt ending in a
// space ("Destination filename [startup-config]? ") is seen as the device
// wrote it and only a $-anchored pattern must account for the space.
func (r *response) line() string {
	front := r.tail
	if i := bytes.LastIndexByte(front, '\n'); i >= 0 {
		front = front[i+1:]
	}
	return string(front) + r.render.Pending()
}

// lastLine is the last line without its trailing blanks: the line a prompt
// is, and the line a diagnostic names.
func (r *response) lastLine() string { return strings.TrimRight(r.line(), " \t") }

// markHere moves the expecter's mark to the end of the last line.
func (r *response) markHere() {
	r.markLines, r.markCol = r.lines, len(r.line())
}

// sinceMark is the last line after the expecter's mark: the whole line once
// a newline has passed the mark.
func (r *response) sinceMark() string {
	line := r.line()
	if r.lines == r.markLines {
		line = line[min(r.markCol, len(line)):]
	}
	return line
}

// candidate says whether the last line is a prompt of the platform's, or
// the level's escalate prompt when a level is given (atSecret); a last line
// that gave up its front is none.
func (r *response) candidate(level *compiledLevel, p *prompts) (prompt, observedLevel string, atSecret bool) {
	if r.overflow {
		return "", "", false
	}
	line := r.lastLine()
	if level != nil && level.escalatePrompt(line) {
		return line, "", true
	}
	prompt, observedLevel, ok := p.match(line)
	if !ok {
		return "", "", false
	}
	return prompt, observedLevel, false
}

// finish ends the read: the unfinished line joins the tail, which gives up
// the returned prompt and the trailing blanks, a held first line that is the
// echo goes, what remains settles, and the closing newline follows any
// settled byte. The error is the sink's: a limit passed by this last
// settling is the limit after the prompt, and the store holds exactly the
// first limit bytes.
func (r *response) finish(returned string) error {
	if r.err != nil {
		return r.err
	}
	t := []byte(r.render.Pending())
	if r.dropNL && len(r.tail) == 0 {
		t = bytes.TrimLeft(t, "\n")
	}
	t = bytes.TrimRight(append(r.tail, t...), " \t\n")
	if returned != "" {
		t = bytes.TrimRight(bytes.TrimSuffix(t, []byte(returned)), " \t\n")
	}
	if r.first && r.isEcho(t) {
		t = nil
	}
	r.first = false
	if err := r.sink.write(t); err != nil {
		r.err = err
		return err
	}
	r.tail = r.tail[:0]
	if r.sink.n > 0 {
		if err := r.sink.write([]byte{'\n'}); err != nil {
			r.err = err
			return err
		}
	}
	return nil
}

// lastNonBlank is the index of the last byte of data that is not a space,
// a tab, or a newline, or -1.
func lastNonBlank(data []byte) int {
	for i := len(data) - 1; i >= 0; i-- {
		switch data[i] {
		case ' ', '\t', '\n':
		default:
			return i
		}
	}
	return -1
}
