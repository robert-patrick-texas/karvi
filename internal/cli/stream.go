package cli

import (
	"bufio"
	"context"
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
// standard input
// and executes it as run would. A line is skipped blank or when its first
// character is ! or #; a line beginning with -- is one run option, the
// word and then its value as the rest of the line after a space or =; any
// other line is one command, sent as written, \r read as --cmd reads it.
// The directives are whole lines: --go or --sendit executes the draft,
// after which the targets and options stay and the commands clear;
// --reset empties the draft; --end, --quit, EOF, or Ctrl-C leave without
// executing. A line the parser refuses is reported with its number and
// dropped. The exit is the last executed job's, 0 when none ran.
//
// The reader turns the draft into a run invocation (Parse over a built
// argument list) and hands it to run's handler, so the job, its records,
// its display, and its exit are a run's, and no rule lives twice.

// streamDirectives maps a directive word, without its dashes, to its act.
var streamDirectives = map[string]string{"go": "go", "sendit": "go", "reset": "reset", "end": "end", "quit": "end"}

// streamDraft is the run being composed: the option arguments and the
// command arguments (commands and the declarations that follow them), in
// the order typed, so Parse sees them as a command line would give them.
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
		fmt.Fprintln(streams.Stderr, "stream: one run option (--target NAME) or one command per line; --go sends, --reset clears, --end quits")
	}
	execute := func(argv []string) int {
		sub, err := Parse(argv)
		if err != nil {
			return reportError(streams.Stderr, errorcodes.Of(err), err)
		}
		sub.Global = inv.Global
		return commandRun(ctx, sub, streams)
	}
	return streamLoop(ctx, streams.Stdin, streams.Stderr, execute)
}

// streamLoop reads the lines and drives the draft; execute runs one
// invocation and gives its exit. It returns when the input ends, a leaving
// directive is read, or ctx is cancelled (Ctrl-C), with the last exit.
func streamLoop(ctx context.Context, in io.Reader, stderr io.Writer, execute func(argv []string) int) int {
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 64*1024), 1<<20)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	draft := &streamDraft{}
	last := exitcode.ExitSuccess
	n := 0
	for {
		var line string
		var ok bool
		select {
		case <-ctx.Done():
			return last
		case line, ok = <-lines:
			if !ok {
				return last
			}
		}
		n++
		text := strings.TrimSpace(line)
		if text == "" || text[0] == '!' || text[0] == '#' {
			continue
		}
		if strings.HasPrefix(text, "--") {
			if act, ok := streamDirectives[text[2:]]; ok {
				switch act {
				case "go":
					last = execute(draft.argv())
					draft.commands = nil
				case "reset":
					draft = &streamDraft{}
				case "end":
					return last
				}
				continue
			}
			args, declaration := streamOptionArgs(text)
			if len(args) == 2 && args[1] == "-" && (args[0] == "--cf" || args[0] == "--tf") {
				fmt.Fprintf(stderr, "stream line %d: standard input is the stream; %s - is not accepted\n", n, args[0])
				continue
			}
			candidate := &streamDraft{options: draft.options, commands: draft.commands}
			if declaration {
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
		// \r sequences are --cmd's (declarationLists).
		draft.commands = append(draft.commands, "--cmd", strings.TrimRight(line, "\r\n"))
	}
}

// streamOptionArgs splits an option line into the arguments Parse takes:
// the word alone (a flag, or a word=value), or the word and the rest of
// the line after the first space, trimmed, as its value. declaration says
// whether the word is one of the three that attach to the command before
// them (--expect, --blind, --blind-return, as their abbreviations too),
// which the draft keeps among the commands.
func streamOptionArgs(text string) (args []string, declaration bool) {
	word, rest, spaced := strings.Cut(text, " ")
	if spaced {
		rest = strings.TrimSpace(rest)
	}
	name := strings.TrimPrefix(word, "--")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	for _, o := range []*option{optExpect, optBlind, optBlindReturn} {
		if strings.HasPrefix(o.name, name) && name != "" {
			declaration = true
		}
	}
	if spaced && rest != "" {
		return []string{word, rest}, declaration
	}
	return []string{word}, declaration
}
