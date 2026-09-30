package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// Stream mode: karvi stream, or karvi -, reads a run line by line from
// standard input and executes it as run would. A line is skipped blank or
// when its first character is ! or #; a line beginning with -- is one run
// option, the word and then its value as the rest of the line after a space
// or =; any other line is one command, sent as written, \r read as --cmd
// reads it. The draft has two parts: the options (targets among them),
// which stay from one job to the next, and the commands, which every job
// clears. An option line whose word is --cmd, --command, or --cf, or one of
// the three declarations (--expect, --blind, --blind-return), belongs to the
// commands, so a command typed the way run takes it is sent once, like a
// bare line. The directives are whole lines: --go or --sendit executes the
// draft and clears the commands (with nothing to send, a notice and no
// job); --clear empties the commands alone; --reset empties the draft;
// --end, --quit, EOF, or Ctrl-C leave without executing. A line the parser
// refuses is reported with its number and dropped. Standard input is the
// stream, so --cf, --tf, and --tfr may not name - in any spelling. The exit
// is the last executed job's, 0 when none ran; a read failure or a line
// over the scanner's limit ends the stream with stream_input_read_failed.
//
// The reader turns the draft into a run invocation (Parse over a built
// argument list) and hands it to run's handler, so the job, its records,
// its display, and its exit are a run's, and no rule lives twice; the
// option words are resolved through run's own table, so an abbreviation or
// an = spelling means what it means on a command line.

// streamDirectives maps a directive word, without its dashes, to its act.
var streamDirectives = map[string]string{"go": "go", "sendit": "go", "clear": "clear", "reset": "reset", "end": "end", "quit": "end"}

// streamLineLimit is the longest line the reader accepts.
const streamLineLimit = 1 << 20

// streamDraft is the run being composed: the option arguments and the
// command arguments (commands, commands files, and the declarations that
// follow them), each in the order typed, so Parse sees them as a command
// line would give them.
type streamDraft struct {
	options  []string
	commands []string
}

// argv is the run invocation the draft makes.
func (d *streamDraft) argv() []string {
	out := []string{"run"}
	out = append(out, d.options...)
	return append(out, d.commands...)
}

// commandStream is the stream word's handler.
func commandStream(ctx context.Context, inv *Invocation, streams app.IO) int {
	if f, ok := streams.Stdin.(*os.File); ok && osutil.IsTerminal(f) && !inv.Global.quiet {
		fmt.Fprintln(streams.Stderr, "stream: one run option (--target NAME) or one command per line; --go sends, --clear drops the commands, --reset clears, --end quits")
	}
	execute := func(line int, argv []string) int {
		sub, err := Parse(argv)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "stream line %d: ", line)
			return reportError(streams.Stderr, errorcodes.Of(err), err)
		}
		sub.Global = inv.Global
		return commandRun(ctx, sub, streams)
	}
	return streamLoop(ctx, streams.Stdin, streams.Stderr, execute)
}

// streamRead is one line of the input, or the reader's failure after the
// last line it delivered.
type streamRead struct {
	text string
	err  error
}

// streamLoop reads the lines and drives the draft; execute runs one
// invocation, named by the line that sent it, and gives its exit. It returns
// when the input ends, a leaving directive is read, the input fails, or ctx
// is cancelled (Ctrl-C), with the last exit. The reader goroutine blocks in
// the input's Read, which takes no deadline, so it lives until the input
// ends or the process does; after the loop returns it delivers nowhere.
func streamLoop(ctx context.Context, in io.Reader, stderr io.Writer, execute func(line int, argv []string) int) int {
	lines := make(chan streamRead, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64*1024), streamLineLimit)
		for scanner.Scan() {
			lines <- streamRead{text: scanner.Text()}
		}
		if err := scanner.Err(); err != nil {
			lines <- streamRead{err: err}
		}
	}()
	draft := &streamDraft{}
	last := exitcode.ExitSuccess
	n := 0
	for {
		// A cancellation outranks a line already read.
		select {
		case <-ctx.Done():
			return last
		default:
		}
		var r streamRead
		var ok bool
		select {
		case <-ctx.Done():
			return last
		case r, ok = <-lines:
			if !ok {
				return last
			}
		}
		if r.err != nil {
			var err error
			if errors.Is(r.err, bufio.ErrTooLong) {
				err = errorcodes.Errorf("stream_input_read_failed", "stream line %d is longer than the %d-byte limit", n+1, streamLineLimit)
			} else {
				err = errorcodes.Errorf("stream_input_read_failed", "standard input failed after stream line %d: %v", n, r.err)
			}
			return reportError(stderr, "stream_input_read_failed", err)
		}
		n++
		text := strings.TrimSpace(r.text)
		if text == "" || text[0] == '!' || text[0] == '#' {
			continue
		}
		if strings.HasPrefix(text, "--") {
			if act, ok := streamDirectives[text[2:]]; ok {
				switch act {
				case "go":
					if len(draft.commands) == 0 {
						fmt.Fprintf(stderr, "stream line %d: nothing to send\n", n)
						continue
					}
					last = execute(n, draft.argv())
					draft.commands = nil
				case "clear":
					draft.commands = nil
				case "reset":
					draft = &streamDraft{}
				case "end":
					return last
				}
				continue
			}
			args, commandPart, stdinOption := streamOptionArgs(text)
			if stdinOption != "" {
				fmt.Fprintf(stderr, "stream line %d: standard input is the stream; %s - is not accepted\n", n, stdinOption)
				continue
			}
			candidate := &streamDraft{options: draft.options, commands: draft.commands}
			if commandPart {
				candidate.commands = append(append([]string{}, draft.commands...), args...)
			} else {
				candidate.options = append(append([]string{}, draft.options...), args...)
			}
			// The parser wants a command on a run; while the draft has
			// none, a placeholder stands in for the check alone.
			probe := candidate.argv()
			if len(candidate.commands) == 0 {
				probe = append(probe, "--cmd", "probe")
			}
			if _, err := Parse(probe); err != nil {
				fmt.Fprintf(stderr, "stream line %d dropped: %s\n", n, errorcodes.Message(err))
				continue
			}
			draft = candidate
			continue
		}
		// A command, as written: leading spaces are the operator's, and
		// \r sequences are --cmd's (declarationLists); trailing blanks are
		// the line's, not the command's.
		draft.commands = append(draft.commands, "--cmd", strings.TrimRight(r.text, " \t\r\n"))
	}
}

// streamOptionArgs splits an option line into the arguments Parse takes:
// the word alone (a flag, or a word=value whose value runs to the end of
// the line), or the word and the rest of the line after the first space,
// trimmed, as its value. The word is
// resolved through run's option table as Parse resolves it (a full name, an
// alias, or a prefix naming one option): commandPart says the option
// belongs to the draft's commands (--cmd and its aliases, --cf, and the
// three declarations), and stdinOption names the option when a --cf, --tf,
// or --tfr line gives - as its value, which the stream cannot serve. A word
// the table does not resolve is an option line for the probe to refuse.
func streamOptionArgs(text string) (args []string, commandPart bool, stdinOption string) {
	word, rest, spaced := strings.Cut(text, " ")
	if spaced && strings.IndexByte(word, '=') >= 0 {
		// An = value runs to the end of the line, spaces included.
		word, rest, spaced = text, "", false
	}
	if spaced {
		rest = strings.TrimSpace(rest)
	}
	_, name, inline, hasInline := splitOptionArg(word)
	canon, _ := resolveWord(name, "--", optionWords(runOptions))
	for _, o := range runOptions {
		if o.name != canon {
			continue
		}
		switch o.role {
		case roleCmd, roleCf, roleDeclaration:
			commandPart = true
		}
		value := rest
		if hasInline {
			value = inline
		}
		if value == "-" && (o == optCf || o == optTf || o == optTfr) {
			stdinOption = "--" + o.name
		}
		break
	}
	if spaced && rest != "" {
		return []string{word, rest}, commandPart, stdinOption
	}
	return []string{word}, commandPart, stdinOption
}
