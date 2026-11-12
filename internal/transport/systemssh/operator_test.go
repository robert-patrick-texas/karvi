package systemssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/termline"
)

// TestOperatorAnswer: what termline.Prompt returned becomes the broker's
// answer: the line typed; an empty answer at Ctrl-D (io.EOF); ErrNoOperator
// with no controlling terminal; credential_prompt_interrupted at Ctrl-C or a
// signal (the context's end); and credential_prompt_unavailable at any other
// failure, as at a credential prompt.
func TestOperatorAnswer(t *testing.T) {
	for _, c := range []struct {
		name   string
		answer string
		err    error
		want   string // the answer, or the error's message
		isErr  bool
	}{
		{"a line", "731904", nil, "731904", false},
		{"Ctrl-D", "", io.EOF, "", false},
		{"no terminal", "", fmt.Errorf("%w: %w", termline.ErrNoTerminal, errors.New("open /dev/tty: no such device or address")), askpass.ErrNoOperator.Error(), true},
		{"Ctrl-C", "", termline.ErrInterrupt, "credential_prompt_interrupted: interrupted at the device's prompt", true},
		{"a signal", "", context.Canceled, "credential_prompt_interrupted: interrupted at the device's prompt", true},
		{"another failure", "", errors.New("set raw mode: input/output error"), "credential_prompt_unavailable: the device's prompt: set raw mode: input/output error", true},
	} {
		got, err := operatorAnswer(c.answer, c.err)
		switch {
		case !c.isErr && (err != nil || string(got) != c.want || got == nil):
			t.Errorf("%s: %q %v, want %q", c.name, got, err, c.want)
		case c.isErr && (err == nil || got != nil):
			t.Errorf("%s: %q %v, want the error %q", c.name, got, err, c.want)
		case c.isErr && c.name == "no terminal" && !errors.Is(err, askpass.ErrNoOperator):
			t.Errorf("%s: %v, want askpass.ErrNoOperator", c.name, err)
		case c.isErr && c.name != "no terminal" && errorcodes.Message(err) != c.want:
			t.Errorf("%s: %q, want %q", c.name, errorcodes.Message(err), c.want)
		}
	}
}
