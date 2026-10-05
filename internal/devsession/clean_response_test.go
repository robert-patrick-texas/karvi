package devsession

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/termtext"
)

// rendered is raw as the terminal showed it after previousPrompt, the
// whole response at once: no width, the spaces the device wrote kept, the
// prompt holding its columns and never part of the text.
func rendered(raw []byte, previousPrompt string) string {
	var out bytes.Buffer
	r := termtext.New(&out, 0)
	r.KeepSpaces = true
	r.Seed(previousPrompt)
	r.Write(raw)
	r.Close()
	return out.String()
}

// cleanResponseReference is cleanResponse as it stood through v0.12.1, the
// whole response cleaned in one pass at the end, over the rendered text in
// place of the bytes with their carriage returns dropped. It is kept as the
// statement of the recorded bytes: the streaming cleaner must answer byte
// for byte what this does, however the response is cut into chunks.
func cleanResponseReference(raw []byte, command, previousPrompt, returnedPrompt string) []byte {
	text := rendered(raw, previousPrompt)
	text = strings.TrimLeft(text, "\n")
	trimmedRight := strings.TrimRight(text, " \t\n")
	if returnedPrompt != "" && strings.HasSuffix(trimmedRight, returnedPrompt) {
		trimmedRight = strings.TrimSuffix(trimmedRight, returnedPrompt)
	}
	text = strings.TrimRight(trimmedRight, " \t\n")
	first, rest, found := strings.Cut(text, "\n")
	if !found {
		rest = ""
	}
	firstTrimmed := strings.TrimSpace(first)
	commandTrimmed := strings.TrimSpace(command)
	promptCommand := strings.TrimSpace(previousPrompt + command)
	if firstTrimmed == commandTrimmed || (previousPrompt != "" && firstTrimmed == promptCommand) {
		text = rest
	}
	text = strings.TrimLeft(text, "\n")
	if text == "" {
		return nil
	}
	return []byte(text + "\n")
}

// settleChunks runs the streaming cleaner over raw cut into the given
// chunks, memory only, and returns the settled bytes with the sink.
func settleChunks(t *testing.T, chunks [][]byte, command, previous, returned string) ([]byte, *settled) {
	t.Helper()
	sink := newSettled(1<<30, Spool{}, 1, nil, nil)
	r := newResponse(command, previous, sink)
	for _, c := range chunks {
		if err := r.feed(append([]byte(nil), c...)); err != nil {
			t.Fatalf("feed %q: %v", c, err)
		}
	}
	if err := r.finish(returned); err != nil {
		t.Fatalf("finish: %v", err)
	}
	return sink.mem, sink
}

// cuts is raw as one chunk, as one chunk per byte (every border at once,
// mid-line and mid-character included), and, when every is set, as two
// chunks split at each position; otherwise at one random position.
func cuts(raw []byte, every bool, rng *rand.Rand) [][][]byte {
	out := [][][]byte{{raw}}
	if len(raw) > 1 {
		single := make([][]byte, len(raw))
		for i := range raw {
			single[i] = raw[i : i+1]
		}
		out = append(out, single)
		if every {
			for i := 1; i < len(raw); i++ {
				out = append(out, [][]byte{raw[:i], raw[i:]})
			}
		} else {
			i := 1 + rng.Intn(len(raw)-1)
			out = append(out, [][]byte{raw[:i], raw[i:]})
		}
	}
	return out
}

// TestCleanResponseMatchesTheReference holds the streaming cleaner to the
// v0.12.1 function: named shapes first (each a case
// a device or the fake has shown), cut at every chunk border, then 20,000
// responses assembled at random from the pieces that matter to the
// trimming (returns, newlines, blanks, the prompt, the command, invalid
// UTF-8), each whole, byte by byte, and split once at random. The running
// digest and the UTF-8 check are held to the settled bytes on every run.
func TestCleanResponseMatchesTheReference(t *testing.T) {
	const command, prompt = "show clock", "router#"
	named := []string{
		"",
		"\r\n",
		"router#",
		"show clock\r\n12:00:00 UTC\r\nrouter#",
		"router#show clock\r\n12:00:00 UTC\r\nrouter#",
		"show clock\r\nshow clock\r\n12:00:00 UTC\r\nrouter#",
		"\r\n\r\nshow clock \r\n\r\n12:00:00 UTC\r\n\r\nrouter# \r\n",
		"show clock",             // the echo alone, nothing trimmed after it
		"12:00:00 UTC",           // no echo, no prompt, no spare byte for the newline
		"12:00:00 UTC\r\nrouter", // a prompt cut short is output
		"show clocks\r\nx\r\nrouter#",
		"x\r\nrouter#\r\nrouter#",
		"show clock\r\r\n\xff\xfe\r\nrouter#",
		"show clock\r\nrouter(config)#",
		"show clock\r\n\r\n\r\n  \r\nabc\r\n\r\n  \r\nrouter#", // blank lines kept inside, trimmed at the ends
		"  \r\nabc\r\nrouter#",                                 // a blank first line before output stays
		"show clockrouter#",                                    // the echo and the prompt on one line
		"show clock\r\né\r\nrouter#",
		"show clock\r\nabc\rX\r\nrouter#",                          // a return overwrites, as the terminal showed it
		"show clokc\b\b\x1b[Kck\r\n\x1b[1m12:00\x1b[0m\r\nrouter#", // a correction and a colour
		"\x1b]0;title\x07show clock\r\nrouter#",                    // a window title before the echo
		"show clock\r\ndesc \r\nrouter#",                           // a space the device wrote
	}
	pieces := []string{"\r", "\n", "\r\n", " ", "\t", prompt, command, "x", "12:00:00 UTC", "\xff", "é", "#", "router"}
	rng := rand.New(rand.NewSource(1))
	var random []string
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := rng.Intn(9); n > 0; n-- {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		random = append(random, b.String())
	}
	prompts := [][2]string{{prompt, prompt}, {"", prompt}, {prompt, ""}, {"", ""}, {prompt, "router(config)#"}}
	check := func(c string, every bool) {
		raw := []byte(c)
		for _, p := range prompts {
			// The session ends a read with a prompt only when the prompt is
			// its last line (readUntil matches lastLine and a later byte
			// stops the settle timer), so that is the returned prompt both
			// forms are given; a response whose last line is not the prompt
			// is compared as the session would record it, without one.
			returned := p[1]
			last := termtext.New(io.Discard, 0)
			last.Seed(p[0])
			last.Write(raw)
			if returned != "" && !strings.HasSuffix(strings.TrimRight(last.Pending(), " \t"), returned) {
				returned = ""
			}
			want := cleanResponseReference(raw, command, p[0], returned)
			for _, chunks := range cuts(raw, every, rng) {
				got, sink := settleChunks(t, chunks, command, p[0], returned)
				if string(got) != string(want) || (len(got) == 0) != (len(want) == 0) {
					t.Fatalf("raw %q previous %q returned %q in %d chunks:\n got %q\nwant %q", c, p[0], returned, len(chunks), got, want)
				}
				sum := sha256.Sum256(got)
				if hex.EncodeToString(sink.hash.Sum(nil)) != hex.EncodeToString(sum[:]) {
					t.Fatalf("raw %q in %d chunks: the running digest is not the digest of the settled bytes", c, len(chunks))
				}
				if sink.utf8.valid() != utf8.Valid(got) {
					t.Fatalf("raw %q in %d chunks: the UTF-8 check says %v, utf8.Valid %v", c, len(chunks), sink.utf8.valid(), utf8.Valid(got))
				}
				if sink.n != int64(len(got)) || sink.observed != sink.n {
					t.Fatalf("raw %q: counted %d stored and %d observed for %d bytes", c, sink.n, sink.observed, len(got))
				}
			}
		}
	}
	for _, c := range named {
		check(c, true)
	}
	for _, c := range random {
		check(c, false)
	}
}

// TestRenderedShellResponse: bash's decorations around a command's answer,
// as this host's shell sent them, leave the answer and a prompt without its
// window title or colours (the record kept every sequence before the shell's
// output was rendered); an inner line keeps the spaces the device wrote, and
// the last line before the prompt gives up its trailing blanks, as it always
// has.
func TestRenderedShellResponse(t *testing.T) {
	const prompt = "netops@dev:~$"
	raw := "echo hi\r\n\x1b[?2004l\rhi\r\n\x1b[?2004h\x1b]0;netops@dev: ~\x07\x1b[01;32mnetops@dev\x1b[00m:\x1b[01;34m~\x1b[00m$ "
	if got := renderedLastLine(raw); got != prompt {
		t.Fatalf("the prompt line is %q", got)
	}
	if got, _ := settleChunks(t, [][]byte{[]byte(raw)}, "echo hi", prompt, prompt); string(got) != "hi\n" {
		t.Fatalf("the answer is %q", got)
	}
	if got, _ := settleChunks(t, [][]byte{[]byte("printf 'desc \\n'\r\n\x1b[?2004l\rdesc \r\nabc\b \b\r\nend \r\n" + prompt + " ")}, "printf 'desc \\n'", prompt, prompt); string(got) != "desc \nab \nend\n" {
		t.Fatalf("the written spaces: %q", got)
	}
}
