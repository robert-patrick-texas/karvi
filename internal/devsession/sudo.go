package devsession

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
)

// A root command (--sudo) on an exec channel (EXAMPLES chapter 96). With a
// password the command is sent as
//
//	sudo -S -p 'karvi-sudo-<16 hex>:' -- TEXT
//
// its standard input left open, the prompt a token of the command's own;
// without one as sudo -n -- TEXT, the input at its end as for any command.
// No sh -c: the server's sudoers rules match the command and its log names
// it, and a pipe or a redirect after the first command runs as the login
// user, as sudo TEXT typed at a prompt does. sudo writes the prompt on
// stderr; the session removes it there (sudoFilter), writes the password
// and a line end once, at the first prompt, and closes the input; a second
// prompt says sudo refused the password, and the command is
// privilege_failed.

// sudoPromptPrefix begins the prompt a root command's sudo is given.
const sudoPromptPrefix = "karvi-sudo-"

// sudoPrompt is a fresh prompt for one root command: the prefix, 16 hex
// digits from crypto/rand (whose Read never fails), and a colon.
func sudoPrompt() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return sudoPromptPrefix + hex.EncodeToString(b[:]) + ":"
}

// sudoWire is the text a root command is sent as: with prompt, sudo reads
// the password on its input; without one, sudo -n asks for none.
func sudoWire(text, prompt string) string {
	if prompt == "" {
		return "sudo -n -- " + text
	}
	return "sudo -S -p '" + prompt + "' -- " + text
}

// sudo-rs's form of the prompt it is given: "[sudo: PROMPT] Password: ".
const (
	sudoRSOpen  = "[sudo: "
	sudoRSClose = "] Password: "
)

// sudoFilter takes sudo's prompt out of a root command's stderr, in either
// form sudo writes it: classic sudo's, the prompt as given with no line end
// after it; sudo-rs's, "[sudo: PROMPT] Password: " and the line end it
// writes once its read ends. A line end directly after a prompt is removed
// with it: after sudo-rs's form always, after classic sudo's on a second
// prompt alone, which it ends with one when the read finds the input
// closed; after a classic first prompt, answered, the command's own stderr
// follows, so a blank first line of the command's is kept. A tail of a
// chunk that may begin a prompt is held until the next chunk shows what it
// is, or the stream's end (EXAMPLES chapter 96, ex96d-stderr).
type sudoFilter struct {
	prompt []byte // the -p argument
	rs     []byte // sudo-rs's form of it
	held   []byte
	// lineEnd: the next byte, if a line end, is the last prompt's.
	lineEnd bool
	prompts int
}

func newSudoFilter(prompt string) *sudoFilter {
	return &sudoFilter{prompt: []byte(prompt), rs: []byte(sudoRSOpen + prompt + sudoRSClose)}
}

// write filters one chunk: the bytes to pass on, and how many prompts it
// removed. The chunk is not kept; what is held is a copy.
func (f *sudoFilter) write(chunk []byte) (out []byte, prompts int) {
	buf := append(f.held, chunk...)
	f.held = nil
	for {
		if f.lineEnd && len(buf) > 0 {
			if buf[0] == '\n' {
				buf = buf[1:]
			}
			f.lineEnd = false
		}
		i := bytes.Index(buf, f.prompt)
		if i < 0 {
			hold := max(partialSuffix(buf, f.rs), partialSuffix(buf, f.prompt))
			out = append(out, buf[:len(buf)-hold]...)
			f.held = append([]byte(nil), buf[len(buf)-hold:]...)
			return out, prompts
		}
		start, end := i, i+len(f.prompt)
		open := i - len(sudoRSOpen)
		if open >= 0 && string(buf[open:i]) == sudoRSOpen {
			after := buf[end:]
			if len(after) < len(sudoRSClose) && bytes.HasPrefix([]byte(sudoRSClose), after) {
				// sudo-rs's form, its close not yet read.
				out = append(out, buf[:open]...)
				f.held = append([]byte(nil), buf[open:]...)
				return out, prompts
			}
			if bytes.HasPrefix(after, []byte(sudoRSClose)) {
				start, end = open, end+len(sudoRSClose)
				f.lineEnd = true
			}
		}
		f.prompts++
		prompts++
		if start == i && f.prompts > 1 {
			f.lineEnd = true
		}
		out = append(out, buf[:start]...)
		buf = buf[end:]
	}
}

// end is the held tail at the stream's end: it was no prompt.
func (f *sudoFilter) end() []byte {
	out := f.held
	f.held = nil
	return out
}

// partialSuffix is the length of the longest proper prefix of form that
// buf ends with: bytes that may begin form in the next chunk.
func partialSuffix(buf, form []byte) int {
	for n := min(len(buf), len(form)-1); n > 0; n-- {
		if bytes.HasSuffix(buf, form[:n]) {
			return n
		}
	}
	return 0
}
