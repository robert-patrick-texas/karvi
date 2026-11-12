package systemssh

import (
	"context"
	"errors"
	"io"

	"github.com/robert-patrick-texas/karvi/internal/askpass"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/termline"
)

// TerminalOperator asks a login's operator a device's prompt at the
// controlling terminal, with the credential prompts' editor
// (termline.Prompt), echo off, and returns the line typed: Ctrl-D on an
// empty line is an empty answer, as OpenSSH's own prompt sends; Ctrl-C or a
// signal (the context's end) ends the login, credential_prompt_interrupted,
// and any other failure credential_prompt_unavailable, as at a credential
// prompt; no controlling terminal is askpass.ErrNoOperator, the prompt
// answered as a command session does.
func TerminalOperator(ctx context.Context, prompt string) ([]byte, error) {
	return operatorAnswer(termline.Prompt(ctx, prompt, true))
}

// operatorAnswer is TerminalOperator's answer for what termline.Prompt
// returned.
func operatorAnswer(answer string, err error) ([]byte, error) {
	switch {
	case err == nil:
		return []byte(answer), nil
	case errors.Is(err, termline.ErrNoTerminal):
		return nil, askpass.ErrNoOperator
	case errors.Is(err, io.EOF):
		return []byte{}, nil
	case errors.Is(err, termline.ErrInterrupt) || errors.Is(err, context.Canceled):
		return nil, errorcodes.Errorf("credential_prompt_interrupted", "interrupted at the device's prompt")
	}
	return nil, errorcodes.Errorf("credential_prompt_unavailable", "the device's prompt: %s", err)
}
