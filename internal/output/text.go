package output

import (
	"io"
	"net/netip"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// output.TARGET.txt is one device's session as an operator reads it in a
// terminal: a header line, then for
// each statement the prompt and the statement on one line and what the
// device answered. Everything here is written from a record's own fields,
// so every recorded statement's block is derivable from commands.jsonl.
// The one part with no record behind it is karvi's own set-up (enable and
// the paging commands), written by WriteTextSetup from what the session saw.
//
// The functions are pure: a record and a writer. Which file, when it is
// opened, and who else feeds it (the session's set-up lines) are the
// store's and the executor's concern.
//
// Nothing here builds a block in memory. The record's output is already a
// string and goes to the writer as it is, so the text file adds no copy of
// a response.

// FileName is the device's name in a file name, one function for the job
// folder's output.NAME.txt and the collection's crun/NAME. target is the
// device that was accessed: the record's
// device.canonical_name, which is the name for a named target and the
// address for a target given as an address. An address is written whole
// with every dot and colon a hyphen (10-1-2-3, 2001-db8--10), never cropped.
// A device name is lowercased, and under crop (output.crop-to-dot, carried
// in the plan) it is its first label: core-nyc-01.example.net and
// Core-NYC-01 both write core-nyc-01. A path separator cannot come from an
// inventory name or an address; it is replaced all the same, so that the
// name stays one file in its folder.
func FileName(target string, crop bool) string {
	if addr, err := netip.ParseAddr(target); err == nil {
		return addressReplacer.Replace(addr.String())
	}
	name := strings.ToLower(target)
	if i := strings.IndexByte(name, '.'); crop && i > 0 {
		name = name[:i]
	}
	return separatorReplacer.Replace(name)
}

// TextFileName is the name of a device's text file in the job folder.
func TextFileName(target string, crop bool) string {
	return "output." + FileName(target, crop) + ".txt"
}

var (
	separatorReplacer = strings.NewReplacer("/", "-", "\\", "-", "\x00", "-")
	addressReplacer   = strings.NewReplacer(".", "-", ":", "-", "%", "-")
)

// Timestamp formats a header's time for the operator: display.Formatter's
// Timestamp method, which applies display.timestamp in the effective
// timezone, the one format every human-facing time of a job has (the
// display's header and footer, the login transcript's banner). The record
// keeps the instant itself, so the header stays derivable from
// commands.jsonl by whoever holds the same two settings (tools/textfile).
type Timestamp func(time.Time) string

// DefaultTimestamp is the registered default pattern in the host's zone,
// what a caller that names no formatter gets.
func DefaultTimestamp() Timestamp {
	f, err := display.NewFormatter(display.DefaultTimestampPattern, "auto")
	if err != nil {
		panic("the default display.timestamp does not compile: " + err.Error())
	}
	return f.Timestamp
}

// WriteTextHeader writes the file's first line from a device's first
// record: `! ### name (address) timestamp ###`, the timestamp the device's
// start, which is about the time of the connection, written by stamp
// (display.timestamp). A device whose address
// was never selected has no parenthesis. The line opens with `!` as every
// line of karvi's own does, so each line of the file is the device's, a
// prompt and a statement, or an IOS comment.
func WriteTextHeader(w io.Writer, r *records.CommandRecord, stamp Timestamp) error {
	line := "! ### " + r.Device.CanonicalName
	if r.SelectedAddress != "" {
		line += " (" + r.SelectedAddress + ")"
	}
	if at := r.Timing.DeviceStartedAt; at != nil {
		line += " " + stamp(*at)
	}
	_, err := io.WriteString(w, line+" ###\n")
	return err
}

// WriteTextBlock writes one record's block:
//
//	r1#show clock                      the prompt it was sent at, and the statement
//	*10:00:00.000 UTC Tue Sep 15 2026  what the device answered, if anything
//	! command_timeout: ...             only when the record is not a success
//
// A statement that answers with nothing but the next prompt (`end`, an
// accepted configuration line) is the first line alone. A statement that
// was not sent has no prompt (promptbefore is empty: nothing was typed), so
// it is an IOS comment line, `! not sent: show version`. The closing `!`
// line is the record's error code and message, or its status when it has
// no error (not_attempted_prior_command_failure).
//
// An exec command was typed at no prompt: its first line is the inferred
// one (srv1$ uname -s), then stdout, then stderr, their interleaving lost;
// one that did not run is `! not sent:` as on a shell.
func WriteTextBlock(w io.Writer, r *records.CommandRecord, src Source) error {
	first := r.PromptBefore + r.Command
	sent := r.PromptBefore != ""
	if r.Channel == records.ChannelExec {
		first, sent = records.ExecPrompt(r.Device.CanonicalName)+r.Command, r.Ran()
	}
	if !sent {
		first = "! not sent: " + r.Command
	}
	if _, err := io.WriteString(w, first+"\n"); err != nil {
		return err
	}
	if err := writeTextAnswer(w, src); err != nil {
		return err
	}
	if stderr, ok := src.Stderr(); ok {
		if err := writeTextAnswer(w, stderr); err != nil {
			return err
		}
	}
	if r.Status == "succeeded" {
		return nil
	}
	why := r.Status
	if r.Error != nil {
		// One line: a message's own line breaks would read as device output.
		why = r.Error.Code + ": " + strings.Join(strings.Fields(r.Error.Message), " ")
	}
	_, err := io.WriteString(w, "! "+why+"\n")
	return err
}

// WriteTextSetup writes karvi's own set-up statements, which go between the
// header and the device's first block: each is the prompt it was sent at
// and the statement on one line, then what the device answered, the same
// shape as a recorded statement's block.
//
//	r1>enable
//	Password:
//	r1#terminal length 0
//	r1#terminal width 512
//
// Nothing follows `Password:`: the secret is not a statement, and what the
// device sent between it and the next prompt is not kept
// (platform.SetupLine). A set-up statement that failed is the last line
// here; the reason is on the device's first record, whose block follows.
func WriteTextSetup(w io.Writer, lines []platform.SetupLine) error {
	for _, l := range lines {
		if _, err := io.WriteString(w, l.PromptBefore+l.Statement+"\n"); err != nil {
			return err
		}
		if err := writeTextAnswer(w, FromText(l.Output)); err != nil {
			return err
		}
	}
	return nil
}

// writeTextAnswer writes what the device answered, ending it with a line
// break if it has none. The output goes to the writer as it is from its
// source, with no copy of a large response: the
// record's text, base64 decoded as it streams, or the spool file copied;
// the text file holds the device's own bytes either way.
func writeTextAnswer(w io.Writer, src Source) error {
	if src.Empty() {
		return nil
	}
	end := LastByteWriter{W: w}
	if err := src.WriteRaw(&end); err != nil {
		return err
	}
	if end.Last == '\n' {
		return nil
	}
	_, err := io.WriteString(w, "\n")
	return err
}

// LastByteWriter remembers the last byte written through it: whether a
// streamed output ended its last line is not known until it has streamed.
// The text block and the renderer's text format end the block by it.
type LastByteWriter struct {
	W    io.Writer
	Last byte
}

func (l *LastByteWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		l.Last = p[len(p)-1]
	}
	return l.W.Write(p)
}

// WriteString passes a string on without copying it to a byte slice: a
// record's text is written through here whole.
func (l *LastByteWriter) WriteString(s string) (int, error) {
	if len(s) > 0 {
		l.Last = s[len(s)-1]
	}
	return io.WriteString(l.W, s)
}
