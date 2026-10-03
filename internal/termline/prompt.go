package termline

import (
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// prompter is the controlling terminal's editor for the credential prompts,
// opened at the first and kept for the process, so a line typed or pasted
// ahead of the next prompt (a password after the username's Enter) is that
// prompt's answer and not lost with an editor of its own.
var prompter struct {
	sync.Mutex
	tty  *os.File
	line *Line
}

// Prompt asks on the controlling terminal and reads the answer with the
// editor, without echo when masked. Ctrl-C ends it with ErrInterrupt,
// Ctrl-D on an empty line and the terminal's end with io.EOF, and the
// context's end (a signal: in raw mode Ctrl-C is a key, not a signal) with
// the context's error; the cursor is then put on a new line, so what the
// caller prints next starts there.
func Prompt(ctx context.Context, prompt string, masked bool) (string, error) {
	prompter.Lock()
	defer prompter.Unlock()
	if prompter.tty == nil {
		tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
		if err != nil {
			// The caller attaches credential_prompt_unavailable.
			return "", fmt.Errorf("no controlling terminal: %w", err)
		}
		prompter.tty, prompter.line = tty, New(tty, tty)
	}
	tty, l := prompter.tty, prompter.line
	_ = tty.SetReadDeadline(time.Time{})
	stop := context.AfterFunc(ctx, func() { _ = tty.SetReadDeadline(time.Now()) })
	defer stop()
	read := l.ReadLine
	if masked {
		read = l.ReadPassword
	}
	answer, err := read(prompt)
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		_, _ = io.WriteString(tty, "\r\n")
		return "", err
	}
	return answer, nil
}
