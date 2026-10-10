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

// Stream mode: karvi stream, or karvi -, reads a run line by line from standard
// input and executes it as run would. A line is skipped blank or when its first
// character is ! or #; a line beginning with a dash and one more character is
// one run option, one dash or two alike as on run's command line, the word and
// then its value as the rest of the line after a space or =, a value wholly
// wrapped in one pair of quotes losing them as a shell would remove them; any
// other line, a - alone among them, is one command, sent as written, quotes and
// all, \r read as --cmd reads it. The draft has two parts: the options (targets
// among them), which stay from one job to the next, and the commands, which
// every job clears. An option line whose word is --cmd, --command, or --cf, or
// one of the five declarations (--expect, --blind, --blind-return, --timeout,
// --maxbytes), belongs to the commands, so a command typed the way run takes
// it is sent once, like a bare line. The directives are whole lines, one dash or two alike: --go or
// --sendit executes the draft and clears the commands (with nothing to send, a
// notice and no job); --clear empties the commands alone; --reset empties the
// draft; --restart empties it and reads the configuration again, as the
// stream's start does; --end, --quit, EOF, or Ctrl-C leave without executing. A
// line the parser refuses is reported with its number and dropped, as is an
// option whose value attaches with = alone given text after a space. Standard
// input is the stream, so --cf, --tf, and --tfr may not name - in any spelling.
// The exit is the last executed job's, 0 when none ran; a read failure or a
// line over the scanner's limit ends the stream with stream_input_read_failed,
// and a --restart whose reading fails ends it with the reading's code.
//
// The reader turns the draft into a run invocation (Parse over a built
// argument list) and hands it to run's handler, so the job, its records,
// its display, and its exit are a run's, and no rule lives twice; the
// option words are resolved through run's own table, so an abbreviation or
// an = spelling means what it means on a command line.

// streamDirectives maps a directive word, without its dashes, to its act.
var streamDirectives = map[string]string{"go": "go", "sendit": "go", "clear": "clear", "reset": "reset", "restart": "restart", "end": "end", "quit": "end", "exit": "end"}

// streamPurges are the two directives taken by prefix: a word from least to
// the whole word, without its dashes, names the act. "purge" and "purge-"
// name both and are refused.
var streamPurges = []struct{ word, least, act string }{
	{"purge-commands", "purge-c", "purge-commands"},
	{"purge-targets", "purge-t", "purge-targets"},
}

// streamPurge resolves a directive word given by prefix: the act, or the
// refusal of a word naming both.
func streamPurge(word string) (string, error) {
	if word == "purge" || word == "purge-" {
		return "", errorcodes.Errorf("cli_option_ambiguous", "--%s is ambiguous in stream: --purge-commands, --purge-targets", word)
	}
	for _, p := range streamPurges {
		if strings.HasPrefix(word, p.least) && strings.HasPrefix(p.word, word) {
			return p.act, nil
		}
	}
	return "", nil
}

// streamLineLimit is the longest line the reader accepts.
const streamLineLimit = 1 << 20

// streamEntry is one line's arguments in the draft. An option's entry
// holds the option's role, so --purge-targets finds the target inputs; a
// command's entry holds the command or commands file and the declarations
// that follow it, and kept says it was given in option form (--cmd and its
// aliases, --cf), which --go and --clear leave in the draft.
type streamEntry struct {
	args []string
	role optRole
	kept bool
}

// streamDraft is the run being composed: the options and the commands, each
// in the order typed, so Parse sees them as a command line would give them.
type streamDraft struct {
	options  []streamEntry
	commands []streamEntry
}

// argv is the run invocation the draft makes.
func (d *streamDraft) argv() []string {
	out := []string{"run"}
	for _, e := range d.options {
		out = append(out, e.args...)
	}
	for _, e := range d.commands {
		out = append(out, e.args...)
	}
	return out
}

// clone is a copy the next line can change without changing the draft.
func (d *streamDraft) clone() *streamDraft {
	c := &streamDraft{options: append([]streamEntry{}, d.options...), commands: append([]streamEntry{}, d.commands...)}
	if n := len(c.commands); n > 0 {
		c.commands[n-1].args = append([]string{}, c.commands[n-1].args...)
	}
	return c
}

// clear removes the commands given as bare lines; those given in option
// form stay, in their order.
func (d *streamDraft) clear() {
	var kept []streamEntry
	for _, e := range d.commands {
		if e.kept {
			kept = append(kept, e)
		}
	}
	d.commands = kept
}

// purgeTargets removes every target input: --target, --tl, and the options
// run's table marks as target inputs (--tf, --tfr, --site, --device-group,
// --all, --select-platform).
func (d *streamDraft) purgeTargets() {
	var rest []streamEntry
	for _, e := range d.options {
		switch e.role {
		case roleTarget, roleTargetList, roleTargetInput:
		default:
			rest = append(rest, e)
		}
	}
	d.options = rest
}

// streamCommandRole says an option belongs to the draft's commands.
func streamCommandRole(r optRole) bool {
	return r == roleCmd || r == roleCf || r == roleDeclaration
}

// commandStream is the stream word's handler. A terminal's lines come
// through the editing reader (stream_terminal.go); any other input through
// the scanner. The stream reads its configuration as it starts and at each
// --restart, and every job's snapshot is made from the reading before it
// with the job's own option lines: a configuration that does not load is
// refused before a line is read, or ends the stream at the --restart that
// read it, and an edit reaches the next stream or the jobs after a
// --restart, not the next job.
func commandStream(ctx context.Context, inv *Invocation, streams app.IO) int {
	if code, ok := readStreamConfig(inv, streams.Stderr, 0); !ok {
		return code
	}
	next := streamScanner(streams.Stdin)
	terminal := false
	if f, ok := streams.Stdin.(*os.File); ok && osutil.IsTerminal(f) {
		terminal = true
		if t, err := newStreamTerminal(f); err == nil {
			defer t.close()
			next = t.next
		}
	}
	opening := func() {
		if terminal && !inv.Global.quiet {
			fmt.Fprintln(streams.Stderr, "stream: one run option (--target NAME) or one command per line; --go sends, --clear drops the commands, --reset clears, --end quits")
		}
	}
	opening()
	execute := func(line int, argv []string) int {
		sub, err := Parse(argv)
		if err != nil {
			fmt.Fprintf(streams.Stderr, "stream line %d: ", line)
			return reportError(streams.Stderr, errorcodes.Of(err), err)
		}
		sub.Global = inv.Global
		return commandRun(ctx, sub, streams)
	}
	restart := func(line int) (int, bool) {
		code, ok := readStreamConfig(inv, streams.Stderr, line)
		if ok {
			opening()
		}
		return code, ok
	}
	return streamLoop(ctx, next, streams.Stderr, execute, restart)
}

// readStreamConfig is the stream's reading of its configuration, at its
// start (line 0) and at the --restart on line: the files and the
// environment under the stream's command-line options and --set, its
// snapshot validated and its warnings said, the reading every later job's
// snapshot is made from. A failure is reported, after "stream line N: "
// for a --restart's, and its exit returned with false.
func readStreamConfig(inv *Invocation, stderr io.Writer, line int) (int, bool) {
	reading, err := app.ReadConfigFiles(app.ConfigRequest{Roots: inv.Global.configs, Sets: inv.Global.sets})
	if err == nil {
		_, _, err = reading.Snapshot(inv.flags(), inv.Global.say)
	}
	if err != nil {
		if line > 0 {
			fmt.Fprintf(stderr, "stream line %d: ", line)
		}
		return reportError(stderr, "config_load_failed", err), false
	}
	inv.Global.reading = &reading
	return exitcode.ExitSuccess, true
}

// streamRead is one line of the input, or the reader's failure after the
// last line it delivered.
type streamRead struct {
	text string
	err  error
}

// streamScanner reads the lines of a pipe or a file: next gives each line
// without its ending, then io.EOF, or the scanner's failure (a line over
// streamLineLimit among them).
func streamScanner(in io.Reader) func() (string, error) {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 64*1024), streamLineLimit)
	return func() (string, error) {
		if scanner.Scan() {
			return scanner.Text(), nil
		}
		if err := scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
}

// streamLoop reads the lines and drives the draft; next gives one line, or
// io.EOF at the input's end, or the input's failure; execute runs one
// invocation, named by the line that sent it, and gives its exit; restart
// reads the configuration again for the --restart on a line, giving false
// and the failure's exit when the reading fails. It returns when the input
// ends, a leaving directive is read, the input fails, a restart's reading
// fails, or ctx is cancelled (Ctrl-C), with the last exit or the failure's.
// A restart starts the draft and the reading again: the line count and the
// last exit go on. The reader goroutine reads one
// line when the loop asks for it and none ahead: at a terminal a read is
// the editor's, in raw mode, and one running beside a job would hold the
// terminal in raw mode through it, taking the job's Ctrl-C as a key and its
// credential prompt's answer as the next line. It blocks in the input's
// read, which takes no deadline, so it lives until the input ends or the
// process does; after the loop returns it delivers nowhere.
func streamLoop(ctx context.Context, next func() (string, error), stderr io.Writer, execute func(line int, argv []string) int, restart func(line int) (int, bool)) int {
	want := make(chan struct{})
	defer close(want)
	lines := make(chan streamRead, 1)
	go func() {
		defer close(lines)
		for range want {
			text, err := next()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				lines <- streamRead{err: err}
				return
			}
			lines <- streamRead{text: text}
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
		select {
		case <-ctx.Done():
			return last
		case want <- struct{}{}:
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
		if len(text) > 1 && text[0] == '-' {
			word := strings.TrimPrefix(strings.TrimPrefix(text, "-"), "-")
			act, ok := streamDirectives[word]
			if !ok {
				purge, err := streamPurge(word)
				if err != nil {
					fmt.Fprintf(stderr, "stream line %d dropped: %s\n", n, errorcodes.Message(err))
					continue
				}
				act, ok = purge, purge != ""
			}
			if ok {
				switch act {
				case "go":
					if len(draft.commands) == 0 {
						fmt.Fprintf(stderr, "stream line %d: nothing to send\n", n)
						continue
					}
					last = execute(n, draft.argv())
					draft.clear()
				case "clear":
					draft.clear()
				case "purge-commands":
					draft.commands = nil
				case "purge-targets":
					draft.purgeTargets()
				case "reset":
					draft = &streamDraft{}
				case "restart":
					draft = &streamDraft{}
					if code, ok := restart(n); !ok {
						return code
					}
				case "end":
					return last
				}
				continue
			}
			args, role, stdinOption, detached := streamOptionArgs(text)
			if stdinOption != "" {
				fmt.Fprintf(stderr, "stream line %d: standard input is the stream; %s - is not accepted\n", n, stdinOption)
				continue
			}
			if detached != nil {
				fmt.Fprintf(stderr, "stream line %d dropped: %s\n", n, errorcodes.Message(detached))
				continue
			}
			// A declaration joins the command before it; with none, it
			// stands alone for the probe to refuse.
			candidate := draft.clone()
			switch {
			case role == roleDeclaration && len(candidate.commands) > 0:
				prev := &candidate.commands[len(candidate.commands)-1]
				prev.args = append(prev.args, args...)
			case streamCommandRole(role):
				candidate.commands = append(candidate.commands, streamEntry{args: args, role: role, kept: role != roleDeclaration})
			default:
				candidate.options = append(candidate.options, streamEntry{args: args, role: role})
			}
			// The parser wants a command on a run; while the draft has
			// none, a placeholder stands in for the check alone.
			probe := candidate.argv()
			if len(candidate.commands) == 0 {
				probe = append(probe, "--cmd", "probe")
			}
			probed, err := Parse(probe)
			if err == nil && probed.Freeform {
				// Text no option on the line takes would begin run's
				// freeform command text and take every command after it.
				err = errorcodes.Errorf("cli_positional_unexpected", "%q leaves text no option takes, which run would read as command text; a stream line is one option, its value after the word, and a command is a line of its own", text)
			}
			if err == nil {
				// The collection options' values are checked as the line
				// is read, so a bad one never stays in the draft.
				err = collectionOptionsError(probed)
			}
			if err != nil {
				fmt.Fprintf(stderr, "stream line %d dropped: %s\n", n, errorcodes.Message(err))
				continue
			}
			draft = candidate
			continue
		}
		// A command, as written: leading spaces are the operator's, and
		// \r sequences are --cmd's (declarationLists); trailing blanks are
		// the line's, not the command's.
		draft.commands = append(draft.commands, streamEntry{args: []string{"--cmd", strings.TrimRight(r.text, " \t\r\n")}, role: roleCmd})
	}
}

// streamOptionArgs splits an option line into the arguments Parse takes:
// the word alone (a flag, or a word=value whose value runs to the end of
// the line), or the word and the rest of the line after the first space,
// trimmed, as its value; either value loses one pair of quotes wrapping it
// whole (streamUnquote). The word is
// resolved through run's option table as Parse resolves it (a full name, an
// alias, or a prefix naming one option): role is the option's, which says
// whether it belongs to the draft's commands (--cmd and its aliases, --cf,
// and the five declarations; streamCommandRole) or is a target input, and stdinOption names the option when a --cf, --tf,
// or --tfr line gives - as its value, which the stream cannot serve;
// detached is the refusal of an option whose value attaches with = alone
// (--of) given text after a space. A word the table does not resolve is an
// option line for the probe to refuse.
func streamOptionArgs(text string) (args []string, role optRole, stdinOption string, detached error) {
	word, rest, spaced := strings.Cut(text, " ")
	if spaced && strings.IndexByte(word, '=') >= 0 {
		// An = value runs to the end of the line, spaces included.
		word, rest, spaced = text, "", false
	}
	if spaced {
		rest = strings.TrimSpace(rest)
	}
	valued := spaced && rest != ""
	rest = streamUnquote(rest)
	_, name, inline, hasInline := splitOptionArg(word)
	if hasInline {
		inline = streamUnquote(inline)
		word = word[:strings.IndexByte(word, '=')+1] + inline
	}
	canon, _ := resolveWord(name, "--", optionWords(runOptions))
	for _, o := range runOptions {
		if o.name != canon {
			continue
		}
		role = o.role
		value := rest
		if hasInline {
			value = inline
		}
		if value == "-" && (o == optCf || o == optTf || o == optTfr) {
			stdinOption = "--" + o.name
		}
		if o.kind == kindOptional && valued {
			// A stream line is one option, so the text after the space
			// can only have been meant as the value, which attaches
			// with = alone.
			detached = detachedValue(o, rest)
		}
		break
	}
	if valued {
		return []string{word, rest}, role, stdinOption, detached
	}
	return []string{word}, role, stdinOption, detached
}

// streamUnquote removes the quotes a shell would have removed from a value
// wholly wrapped in one pair: a value that begins and ends with the same
// quote, double or single, and holds no other of it. Nothing inside is
// read: no escapes, and quotes of the other kind stay. Any other value is
// returned as written, so a device's own quoting reaches it.
func streamUnquote(v string) string {
	if len(v) < 2 || (v[0] != '"' && v[0] != '\'') || v[len(v)-1] != v[0] {
		return v
	}
	if inner := v[1 : len(v)-1]; strings.IndexByte(inner, v[0]) < 0 {
		return inner
	}
	return v
}
