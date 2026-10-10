package jobexec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// ansiRE is a terminal escape sequence the strip setting removes: a CSI
// sequence, or an OSC sequence up to its first terminator (BEL or ESC \).
// The OSC body is matched lazily: through v0.19.0 it was greedy and one
// match swallowed the text between two ESC-\-terminated sequences
// (found by the streaming stripper's test).
var ansiRE = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*?(?:\x07|\x1b\\))`)

type recordRenderer struct {
	mu  sync.Mutex
	out io.Writer
	// warnOut, when set (sayTo), takes each record's host-key lines
	// (HostKeyLines): the client's standard error.
	warnOut   io.Writer
	warnStyle display.LineStyle
	// daemon says the daemon runs the job: nothing reads the format, so
	// OnRecord keeps the counts the summary needs and formats nothing
	// the follower is fed from the file.
	daemon               bool
	format               string
	stripANSI            bool
	quiet                bool
	debug                bool
	pingTemplate         string // display.ping.header; empty prints no ping line
	echo                 bool
	activityType         string
	colorEnabled         bool
	lineStyle            display.LineStyle
	borderColor          string
	dynamicBorderColor   string
	formatter            display.Formatter
	headerTemplate       string
	footerTemplate       string
	collectionTemplate   string // display.collection.footer, after the footer for a job with a collection
	border               string
	activityID           string
	artifact             string
	terminalWidth        int
	dynamicBorder        bool
	noBorder             bool
	lastBorder           bool
	dynamicBorderDefault int
	currentHeaderWidth   int
	headerWritten        bool
	lastHeaderKey        string
	lastValues           display.Values
	pendingBorder        string
	pendingBorderColor   string
	jsonIndent           int
	jsonRecords          int
	err                  error
	requested            map[string]int // requested records by status
	sessionInit          map[string]int // session-init records by status
	seenDevices          map[string]bool
	errors               map[string]int
	outputByteSize       int64
}

func newRecordRenderer(out io.Writer, format string, cfg configload.Snapshot, quiet, debug bool, activityID, artifact, activityType string, echo, dynamicBorder, noBorder bool) (*recordRenderer, error) {
	if format == "" {
		format = "text"
	}
	strip := cfg.String("output.ansi") == "strip"
	terminal := false
	terminalWidth := 0
	if f, ok := out.(*os.File); ok {
		terminal = osutil.IsTerminal(f)
		terminalWidth = osutil.TerminalWidth(f)
	}
	if cfg.String("output.ansi") == "auto" && !terminal {
		strip = true
	}
	formatter, err := display.NewFormatter(cfg.String("display.timestamp"), cfg.String("timezone"))
	if err != nil {
		return nil, err
	}

	headerTemplate := ""
	footerTemplate := ""
	borderTemplate := ""
	dynamicDefault := 72
	lastBorder := false
	switch activityType {
	case "command":
		headerTemplate = cfg.String("display.command.header")
		footerTemplate = cfg.String("display.command.footer")
		borderTemplate = cfg.String("display.command.border")
		dynamicDefault = cfg.Int("display.command.dynamic-border-length")
		lastBorder = cfg.Bool("display.command.last-border")
	case "run":
		headerTemplate = cfg.String("display.run.header")
		// The footer of a run rendered where it executes (run --no-daemon),
		// which knows the exit and the elapsed time. A replayed or followed
		// run clears it (newRunReplayRenderer).
		footerTemplate = cfg.String("display.run.footer")
		borderTemplate = cfg.String("display.run.border")
		dynamicDefault = cfg.Int("display.run.dynamic-border-length")
		lastBorder = cfg.Bool("display.run.last-border")
	}
	border, err := display.RenderBorder(borderTemplate)
	if err != nil {
		return nil, err
	}
	if dynamicDefault < 1 {
		dynamicDefault = 72
	}
	style := DisplayLineStyle(cfg, terminal)
	return &recordRenderer{
		out: out, format: format, stripANSI: strip, quiet: quiet, debug: debug, pingTemplate: cfg.String("display.ping.header"), echo: echo, activityType: activityType,
		colorEnabled: style.Enabled, lineStyle: style, borderColor: DisplayBorderColor(cfg),
		dynamicBorderColor: DisplayDynamicBorderColor(cfg),
		formatter:          formatter, headerTemplate: headerTemplate, footerTemplate: footerTemplate, collectionTemplate: cfg.String("display.collection.footer"), border: border,
		activityID: activityID, artifact: artifact, terminalWidth: terminalWidth,
		dynamicBorder: dynamicBorder, noBorder: noBorder, lastBorder: lastBorder, dynamicBorderDefault: dynamicDefault,
		jsonIndent: cfg.Int("display.json.indent"), requested: map[string]int{}, sessionInit: map[string]int{}, seenDevices: map[string]bool{}, errors: map[string]int{},
	}, nil
}

// sayTo has the renderer write each record's host-key lines to w,
// the client's standard error, before the record, in every format: the
// in-process job's renderer and a followed job's, not a past job's
// directory shown again.
func (r *recordRenderer) sayTo(w io.Writer, cfg configload.Snapshot) {
	r.warnOut, r.warnStyle = w, DisplayLineStyle(cfg, DisplayTerminal(w))
}

// HostKeyLines are the client's lines for a record's host-key notices on
// device: "! " and the notice's phrase (hostkey) around the device's name,
// the name in the target colour as the headers draw it and the rest in the
// warning colour, a key accepted though it differs from the stored one in
// the error colour. They are shown under --quiet, which suppresses the
// display's other "!" lines: a key trusted unchecked is not narration.
func HostKeyLines(device string, notices []records.Notice, style display.LineStyle) []string {
	var lines []string
	for _, n := range notices {
		detail := func(key string) string { v, _ := n.Details[key].(string); return v }
		colour := style.Warning
		var phrase hostkey.Phrase
		switch n.Code {
		case "host_key_enrolled":
			phrase = hostkey.EnrolledPhrase(detail("key_type"))
		case "host_key_mismatch_accepted":
			phrase, colour = hostkey.MismatchPhrase(), style.Error
		case "host_key_not_compared":
			phrase = hostkey.NotComparedPhrase(detail("cause"))
		default:
			continue
		}
		lines = append(lines, display.ANSIStyle("! "+phrase.Before, colour, style.Enabled, true)+
			display.ANSIStyle(device, style.Target, style.Enabled, display.RoleBold("target"))+
			display.ANSIStyle(phrase.After, colour, style.Enabled, true))
	}
	return lines
}

// WarningLine is a message after "! ", in the display's warning colour when
// colour is on: the insecure policy's lines, shown under --quiet as the
// host-key lines are.
func WarningLine(message string, style display.LineStyle) string {
	return display.ANSIStyle("! "+message, style.Warning, style.Enabled, display.RoleBold("warning"))
}

// WarningText is "warning: " and message, a warning's diagnostic line, the
// whole line in the display's warning colour and bold when colour is on, as
// WarningLine is.
func WarningText(message string, style display.LineStyle) string {
	return display.ANSIStyle("warning: "+message, style.Warning, style.Enabled, display.RoleBold("warning"))
}

// WriteWarning writes message's warning line on w in cfg's style for w, by
// the display's one colour rule; a nil w writes nothing. Every warning line
// karvi writes is written by it.
func WriteWarning(w io.Writer, cfg configload.Snapshot, message string) {
	if w != nil {
		fmt.Fprintln(w, WarningText(message, DisplayLineStyle(cfg, DisplayTerminal(w))))
	}
}

// The insecure policy's admission warning: its code, the message the
// daemon's receipt and log carry, and the two lines a client shows for it,
// once per job.
const PolicyInsecureCode = "host_key_policy_insecure"

var policyInsecureLines = []string{
	"ssh host-key policy insecure: unknown and changed keys accepted;",
	" connecting to devices with wrong keys and MITM attacks allowed",
}

// PolicyInsecureWarning is the admission warning of a job under insecure,
// "code: message" as every admission warning.
func PolicyInsecureWarning() string {
	return fmt.Sprintf("host_key_policy_insecure: unknown and changed keys accepted; connecting to devices with wrong keys and MITM attacks allowed")
}

// AdmissionWarningLines are a client's lines for one admission warning: the
// insecure policy's two lines, each a WarningLine, or the warning's
// WarningText.
func AdmissionWarningLines(warning string, style display.LineStyle) []string {
	if strings.HasPrefix(warning, PolicyInsecureCode+":") {
		lines := make([]string, len(policyInsecureLines))
		for i, l := range policyInsecureLines {
			lines[i] = WarningLine(l, style)
		}
		return lines
	}
	return []string{WarningText(warning, style)}
}

// WriteAdmissionWarnings writes a job's admission warnings to w, a client's
// standard error, as AdmissionWarningLines in w's colours.
func WriteAdmissionWarnings(w io.Writer, cfg configload.Snapshot, warnings []string) {
	style := DisplayLineStyle(cfg, DisplayTerminal(w))
	for _, warning := range warnings {
		for _, line := range AdmissionWarningLines(warning, style) {
			fmt.Fprintln(w, line)
		}
	}
}

// stoppedStatuses are the statuses a device's requested records repeat when
// dispatch, a cancel, or a shutdown stopped it: a session-init record with
// one is hidden.
var stoppedStatuses = map[string]bool{"not_started_halt": true, "not_started_wave_gate": true, "cancelled": true, "incomplete_shutdown": true}

// OnRecord renders a record whose output is in the record: a line read
// back from commands.jsonl (a replay, a followed job).
func (r *recordRenderer) OnRecord(rec records.CommandRecord) {
	r.OnRecordFrom(rec, output.FromRecord(&rec))
}

// OnRecordFrom counts the record for the summary and renders it in the
// chosen format from its output's source: the
// jsonl format is the store's line, the json format the same pieces with
// the indent, the text format the block with the output streamed from the
// source, so a spooled response is never held whole here. Under the
// daemon nothing is rendered (6.1).
func (r *recordRenderer) OnRecordFrom(rec records.CommandRecord, src output.Source) {
	r.mu.Lock()
	defer r.mu.Unlock()
	sessionInit := rec.CommandKind == "session_init"
	if sessionInit {
		r.sessionInit[rec.Status]++
	} else {
		r.requested[rec.Status]++
	}
	if rec.Error != nil {
		r.errors[rec.Error.Code]++
	}
	if r.err != nil || r.daemon {
		return
	}
	if r.warnOut != nil {
		for _, line := range HostKeyLines(rec.Device.CanonicalName, rec.Notices, r.warnStyle) {
			fmt.Fprintln(r.warnOut, line)
		}
	}

	switch r.format {
	case "jsonl":
		// The record's line as the store writes it, in pieces when the
		// output is large; a write failure is already r.err.
		if _, err := output.WriteRecordLine(rendererWriter{r}, &rec, src); err != nil && r.err == nil {
			r.setErrorLocked("record_encode_failed", err)
		}
		return
	case "json":
		r.writePrettyJSONRecord(rec, src)
		return
	}

	values := displayValuesFromRecord(rec)
	values.ReferenceID = r.activityID
	values.Artifacts = r.artifact
	var data []byte
	target := values.Target
	if target == "" {
		target = rec.Device.CanonicalName
	}

	// A separator belongs between records, not intrinsically to the record that
	// precedes it. Deferring the write until the next shown record lets
	// last-border=false suppress only the final separator without buffering
	// device output.
	takeBorder := func() {
		if r.pendingBorder != "" {
			data = append(data, []byte(r.styledBorder(r.pendingBorder, r.pendingBorderColor))...)
			r.pendingBorder = ""
			r.pendingBorderColor = ""
		}
	}

	// The ICMP gate's one line for the device, before its first record of
	// either kind even when that record is hidden: routine narration, so --quiet
	// suppresses it, and an empty display.ping.header switches it off. The
	// line is the template rendered as the headers are, styled by role;
	// error details and the packet-loss notice
	// follow only under --debug.
	deviceKey := rec.Device.ID
	if deviceKey == "" {
		deviceKey = target
	}
	firstOfDevice := !r.seenDevices[deviceKey]
	r.seenDevices[deviceKey] = true
	if rec.Ping != nil && firstOfDevice && !r.quiet && r.pingTemplate != "" {
		takeBorder()
		lines, err := r.formatter.RenderStyledLines(r.pingTemplate, PingValues(values.Target, values.Address, rec.Ping), r.lineStyle, r.terminalWidth)
		if err != nil {
			r.setErrorLocked("config_display_template_invalid", err)
			return
		}
		for _, line := range lines {
			data = appendLine(data, line)
		}
		if r.debug {
			for _, line := range PingDebugLines(rec.Ping, rec.Notices) {
				data = appendLine(data, line)
			}
		}
	}

	// A record without an error object that did not succeed was not sent, a
	// succeeded session-init record is setup, and a stopped session-init
	// record repeats its device's requested status: none takes a header,
	// border, echo, or output. A requested one leaves one line, shown under
	// --quiet as a failure line is, so a device's not-attempted commands
	// stack under its failure.
	if (rec.Error == nil && (sessionInit || rec.Status != "succeeded")) || (sessionInit && stoppedStatuses[rec.Status]) {
		if !sessionInit {
			line := fmt.Sprintf("karvi: target=%s status=%s command=%d/%d", target, rec.Status, rec.CommandIndex, rec.CommandCount)
			data = appendLine(data, display.ANSIStyle(line, r.lineStyle.Error, r.colorEnabled, true))
		}
		r.write(data)
		return
	}
	r.lastValues = values
	takeBorder()
	headerKey := values.Target + "\x00" + values.Address
	writeHeader := !r.quiet && r.headerTemplate != ""
	if r.activityType == "command" {
		writeHeader = writeHeader && !r.headerWritten
	} else {
		writeHeader = writeHeader && r.lastHeaderKey != headerKey
	}
	if writeHeader {
		lines, err := r.formatter.RenderStyledLines(r.headerTemplate, values, r.lineStyle, r.terminalWidth)
		if err != nil {
			r.setErrorLocked("config_display_template_invalid", err)
			return
		}
		r.currentHeaderWidth = 0
		for _, line := range lines {
			data = appendLine(data, line)
			if width := display.VisibleWidth(line); width > r.currentHeaderWidth {
				r.currentHeaderWidth = width
			}
		}
		r.headerWritten = true
		r.lastHeaderKey = headerKey
	} else if r.headerTemplate == "" {
		r.currentHeaderWidth = 0
	}

	if r.echo && echoRecord(rec) {
		prompt := rec.Prompt
		if rec.Channel == records.ChannelExec {
			prompt = records.ExecPrompt(target)
		} else if prompt == "" {
			promptTarget := values.Target
			if promptTarget == "" {
				promptTarget = "device"
			}
			prompt = promptTarget + "#"
		}
		data = appendLine(data, prompt+rec.Command)
	}
	// The output streams from its source between what precedes it and what
	// follows, the device's bytes as they were recorded (base64 decoded as
	// it streams, a spool copied), through the escape-sequence stripper
	// when output.ansi asks for it: no copy of a response, whatever its
	// size. The
	// bytes written are the same as before the spool.
	last, wrote := byte(0), len(data) > 0
	if wrote {
		last = data[len(data)-1]
	}
	r.write(data)
	data = nil
	if omitted := rec.OutputOmitted(); omitted != nil {
		// The follow stream left the output out: the notice's message
		// stands where
		// the output would, as its own line, whatever --debug says about
		// the other record notices. The record's output_bytes and digest
		// still describe the output, which commands.jsonl holds when kept.
		r.writeString("karvi: " + omitted.Message + "\n")
		last, wrote = '\n', true
	} else {
		// An exec record's stderr follows its stdout, each ending its last
		// line, their interleaving lost.
		streams := []output.Source{src}
		if stderr, ok := src.Stderr(); ok {
			streams = append(streams, stderr)
		}
		for _, stream := range streams {
			if stream.Empty() {
				continue
			}
			if wrote && last != '\n' {
				r.write([]byte{'\n'})
				last = '\n'
			}
			// The last byte is read below the stripper: the block's closing
			// newline follows what the terminal saw.
			end := &output.LastByteWriter{W: rendererWriter{r}, Last: last}
			var w io.Writer = end
			var strip *ansiStripper
			if r.stripANSI {
				strip = &ansiStripper{w: end}
				w = strip
			}
			err := stream.WriteRaw(w)
			if err == nil && strip != nil {
				err = strip.flush()
			}
			if err != nil {
				r.setErrorLocked("record_output_decode_failed", err)
				return
			}
			if strip == nil || strip.wrote {
				last, wrote = end.Last, true
			}
		}
	}
	if wrote && last != '\n' {
		data = append(data, '\n')
	}

	// Text output must make failed records visible. Earlier releases persisted
	// a second target's failure but rendered no line when that record contained
	// no device output, which made a partial run appear to stop after target one.
	if rec.Error != nil {
		failure := fmt.Sprintf("karvi: target=%s status=%s error=%s: %s", target, rec.Status, rec.Error.Code, rec.Error.Message)
		if sessionInit {
			failure = fmt.Sprintf("karvi: target=%s session_init=%s command=%d/%d status=%s error=%s: %s", target, rec.SessionInitProfile, rec.CommandIndex, rec.CommandCount, rec.Status, rec.Error.Code, rec.Error.Message)
		}
		data = appendLine(data, display.ANSIStyle(failure, r.lineStyle.Error, r.colorEnabled, true))
	}

	r.write(data)
	if r.err == nil {
		r.pendingBorder, r.pendingBorderColor = r.nextBorder()
	}
}

func echoRecord(rec records.CommandRecord) bool {
	if rec.Channel == records.ChannelExec {
		return rec.Ran()
	}
	if rec.PromptSource != "" {
		return true
	}
	// A device-reported command error proves that the command was sent even if
	// an adapter could not return prompt metadata. Echo it with the deterministic
	// inferred prompt rather than hiding the command that triggered the error.
	return rec.Status == "device_error"
}

func (r *recordRenderer) nextBorder() (string, string) {
	if r.quiet || r.noBorder {
		return "", ""
	}
	border := r.border
	color := r.borderColor
	if r.dynamicBorder {
		length := r.currentHeaderWidth
		if length < 1 {
			length = r.dynamicBorderDefault
		}
		if r.terminalWidth > 0 && length > r.terminalWidth {
			length = r.terminalWidth
		}
		border = dynamicBorderLine(length)
		color = r.dynamicBorderColor
	}
	if border == "" {
		return "", ""
	}
	return display.CropLines(border, r.terminalWidth), color
}

// dynamicBorderLine is the --border line at a visible width: the display's
// comment prefix and dashes to the width, so that the border stands under
// the header it measures as one more line of karvi's own (the header and
// footer defaults begin the same way). A width too
// small for the prefix and one dash is one dash.
const displayCommentPrefix = "! "

func dynamicBorderLine(width int) string {
	dashes := width - len(displayCommentPrefix)
	if dashes < 1 {
		dashes = 1
	}
	return displayCommentPrefix + strings.Repeat("-", dashes) + "\n"
}

func (r *recordRenderer) styledBorder(border, color string) string {
	if strings.TrimSpace(border) == "" {
		return border
	}
	return display.ANSI(border, color, r.colorEnabled)
}

// writePrettyJSONRecord writes one element of the json format's array: the
// record as json.MarshalIndent gives it under display.json.indent, every
// line indented once more as an element, the output escaped in pieces
// from its source when it is spooled or large.
func (r *recordRenderer) writePrettyJSONRecord(rec records.CommandRecord, src output.Source) {
	if r.jsonRecords == 0 {
		r.write([]byte{'[', '\n'})
	} else {
		r.write([]byte{',', '\n'})
	}
	indent := strings.Repeat(" ", r.jsonIndent)
	if err := output.WriteRecordJSONIndent(rendererWriter{r}, &rec, src, indent, indent); err != nil && r.err == nil {
		r.setErrorLocked("record_encode_failed", err)
		return
	}
	r.jsonRecords++
}

// ansiStripper removes terminal escape sequences from a stream (output.ansi
// strip, or auto without a terminal), as ansiRE.ReplaceAll did on the whole
// output: a sequence cut by a chunk border is carried until it is whole,
// or until the carry passes ansiCarryMax, when it is text after all. wrote
// says whether any byte reached the writer.
type ansiStripper struct {
	w     io.Writer
	carry []byte
	wrote bool
}

// ansiCarryMax bounds an unfinished escape sequence: a real one is a few
// bytes, an OSC title at most a line.
const ansiCarryMax = 4096

func (a *ansiStripper) Write(p []byte) (int, error) {
	buf := p
	if len(a.carry) > 0 {
		buf = append(a.carry, p...)
	}
	// Everything up to the first escape after the last whole sequence can
	// be stripped and written; that escape and what follows it wait, since
	// the rest of its sequence may be in the next chunk.
	keep := len(buf)
	end := 0
	if matches := ansiRE.FindAllIndex(buf, -1); len(matches) > 0 {
		end = matches[len(matches)-1][1]
	}
	if i := bytes.IndexByte(buf[end:], 0x1b); i >= 0 {
		keep = end + i
	}
	if len(buf)-keep > ansiCarryMax {
		keep = len(buf) // not a sequence: text
	}
	if err := a.emit(buf[:keep]); err != nil {
		return 0, err
	}
	a.carry = append(a.carry[:0], buf[keep:]...)
	return len(p), nil
}

// emit writes chunk with its whole escape sequences removed.
func (a *ansiStripper) emit(chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	if ansiRE.Match(chunk) {
		chunk = ansiRE.ReplaceAll(chunk, nil)
	}
	if len(chunk) == 0 {
		return nil
	}
	a.wrote = true
	_, err := a.w.Write(chunk)
	return err
}

// flush writes what was carried: an unfinished sequence at the end of the
// output is text.
func (a *ansiStripper) flush() error {
	carry := a.carry
	a.carry = nil
	if len(carry) == 0 {
		return nil
	}
	a.wrote = true
	_, err := a.w.Write(carry)
	return err
}

// WriteFooter renders the invocation-level command footer after all command
// records have been emitted, then, for a job with a collection, the
// collection's line (display.collection.footer) directly after it, on the
// same stream. JSONL output is never decorated; JSON output is a valid
// indented array for human inspection.
func (r *recordRenderer) WriteFooter(ended time.Time, code int, elapsed time.Duration, collection *records.CollectionSummary) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	if r.format == "json" {
		if r.jsonRecords == 0 {
			r.write([]byte("[]\n"))
		} else {
			r.write([]byte("\n]\n"))
		}
		return r.err
	}
	if r.format == "jsonl" {
		return nil
	}

	// The pending separator is the border after the final record. Emit it only
	// when the mode's explicit last-border setting requests one.
	if r.pendingBorder != "" {
		if r.lastBorder && !r.quiet && !r.noBorder {
			r.write([]byte(r.styledBorder(r.pendingBorder, r.pendingBorderColor)))
		}
		r.pendingBorder = ""
		r.pendingBorderColor = ""
	}
	if r.err != nil || r.quiet {
		return r.err
	}
	values := r.lastValues
	values.Timestamp = ended
	values.ReferenceID = r.activityID
	values.ExitStatus = fmt.Sprintf("%s(%d)", exitcode.ExitName(code), code)
	values.ExitCode = code
	values.Artifacts = r.artifact
	values.Status = strings.ToLower(strings.TrimPrefix(exitcode.ExitName(code), "Exit"))
	values.Elapsed = elapsed
	var data []byte
	templates := []string{r.footerTemplate}
	if collection != nil {
		values.Collection = collection.Directory
		values.Replaced = collection.Replaced
		values.Kept = collection.Kept
		templates = append(templates, r.collectionTemplate)
	}
	for _, template := range templates {
		if template == "" {
			continue
		}
		lines, err := r.formatter.RenderStyledLines(template, values, r.lineStyle, r.terminalWidth)
		if err != nil {
			return err
		}
		for _, line := range lines {
			data = appendLine(data, line)
		}
	}
	r.write(data)
	return r.err
}

// rendererWriter passes a record's line to write, which counts the bytes
// and keeps the first failure.
type rendererWriter struct{ r *recordRenderer }

func (w rendererWriter) Write(p []byte) (int, error) {
	w.r.write(p)
	if w.r.err != nil {
		return 0, w.r.err
	}
	return len(p), nil
}

// writeString is write for a string, without the copy a conversion makes.
func (r *recordRenderer) writeString(text string) {
	if r.err != nil || len(text) == 0 {
		return
	}
	n, err := io.WriteString(r.out, text)
	r.outputByteSize += int64(n)
	if err != nil {
		r.setErrorLocked("terminal_write_failed", err)
		return
	}
	if n != len(text) {
		r.setErrorLocked("terminal_write_failed", io.ErrShortWrite)
	}
}

func (r *recordRenderer) write(data []byte) {
	if r.err != nil || len(data) == 0 {
		return
	}
	n, err := r.out.Write(data)
	r.outputByteSize += int64(n)
	if err != nil {
		r.setErrorLocked("terminal_write_failed", err)
		return
	}
	if n != len(data) {
		r.setErrorLocked("terminal_write_failed", io.ErrShortWrite)
	}
}

func appendLine(dst []byte, line string) []byte {
	if line == "" {
		return dst
	}
	dst = append(dst, line...)
	if dst[len(dst)-1] != '\n' {
		dst = append(dst, '\n')
	}
	return dst
}

func displayValuesFromRecord(rec records.CommandRecord) display.Values {
	// Targets are lowercase on input; the record keeps the token
	// as supplied in input_target, the display shows the device's name.
	target := rec.Device.CanonicalName
	if target == "" {
		target = rec.InputTarget
	}
	values := display.Values{
		Timestamp: rec.Timing.EndedAt, Target: target, Address: rec.SelectedAddress,
		Platform: rec.Platform, Transport: rec.Transport, Status: rec.Status,
	}
	if rec.Credential != nil {
		values.User = rec.Credential.DeviceUsername
		values.AuthBackend = rec.Credential.Backend
	}
	if rec.Timing.CommandStartedAt != nil {
		values.Elapsed = rec.Timing.EndedAt.Sub(*rec.Timing.CommandStartedAt)
	}
	return values
}

// SetError records the first error, coded with the cause named by code unless
// err carries a more specific code.
func (r *recordRenderer) SetError(code string, err error) {
	r.mu.Lock()
	r.setErrorLocked(code, err)
	r.mu.Unlock()
}

func (r *recordRenderer) setErrorLocked(code string, err error) {
	if r.err == nil && err != nil {
		r.err = errorcodes.Ensure(err, code)
	}
}
func (r *recordRenderer) Error() error { r.mu.Lock(); defer r.mu.Unlock(); return r.err }

// RequestedCounts is the summary's requested_command_counts: requested
// records by status.
func (r *recordRenderer) RequestedCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneIntMap(r.requested)
}

// SessionInitCounts is the summary's session_init_counts: session-init
// records by status, empty when none were written.
func (r *recordRenderer) SessionInitCounts() map[string]int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return cloneIntMap(r.sessionInit)
}
func (r *recordRenderer) ErrorCounts() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := map[string]any{}
	for k, v := range r.errors {
		out[k] = v
	}
	return out
}
func (r *recordRenderer) OutputBytes() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.outputByteSize
}

// RenderRunOutput renders a completed run's durable command records. The daemon
// always persists canonical JSONL; the client chooses JSONL passthrough or the
// human text projection after completion without changing stored evidence.
// RenderRunOutput renders a job directory's command records to out in the
// requested format, as the follow step of run does after completion.
func RenderRunOutput(cfg configload.Snapshot, quiet, debug bool, artifactDir, format string, echo, dynamicBorder, noBorder bool, summary *records.Summary, out io.Writer) error {
	if format == "" {
		format = "text"
	}
	path := filepath.Join(artifactDir, "commands.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return errorcodes.Errorf("run_output_records_unreadable", "open %s: %w", path, err)
	}
	defer file.Close()
	if format == "jsonl" {
		// The records byte for byte, then the summary as the last line (28.4).
		if _, err = io.Copy(out, file); err != nil {
			return errorcodes.Ensure(err, "terminal_write_failed")
		}
		if summary == nil {
			return nil
		}
		line, err := SummaryLine(summary)
		if err != nil {
			return err
		}
		_, err = out.Write(line)
		return errorcodes.Ensure(err, "terminal_write_failed")
	}
	if format != "text" && format != "json" {
		return errorcodes.Errorf("run_output_format_unsupported", "unsupported run output format %q", format)
	}
	renderer, err := newRunReplayRenderer(out, format, cfg, quiet, debug, artifactDir, echo, dynamicBorder, noBorder)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 256<<20)
	for scanner.Scan() {
		var record records.CommandRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return errorcodes.Errorf("run_output_record_decode_failed", "decode %s: %w", path, err)
		}
		renderer.OnRecord(record)
		if err := renderer.Error(); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return errorcodes.Errorf("run_output_records_unreadable", "read %s: %w", path, err)
	}
	if err := renderer.finish(summary); err != nil {
		return err
	}
	return renderer.Error()
}

// newRunReplayRenderer is the renderer of a run's records read back from
// commands.jsonl: a job's directory shown later, or a daemon's job followed
// by the client. Its display ends as the in-process run's does, with the
// footer from the job's summary (finish), since
// the summary travels with the records: the follow's terminal frame
// carries it, and a directory holds summary.json.
func newRunReplayRenderer(out io.Writer, format string, cfg configload.Snapshot, quiet, debug bool, artifactDir string, echo, dynamicBorder, noBorder bool) (*recordRenderer, error) {
	return newRecordRenderer(out, format, cfg, quiet, debug, "", artifactDir, "run", echo || cfg.Bool("display.run.echo"), dynamicBorder, noBorder)
}

// finish ends a run's display from the job's summary (28.4): in text the
// footer with the job's exit, duration, and artifacts (display.run.footer,
// as run --no-daemon ends); in jsonl the summary document as the stream's
// last line, the same document as summary.json, so a script reads the
// records and then the summary from one stream; in json the array closed.
// A nil summary (a follow whose stream was lost) closes the format alone.
func (r *recordRenderer) finish(summary *records.Summary) error {
	if r.format == "jsonl" {
		if summary == nil {
			return nil
		}
		line, err := SummaryLine(summary)
		if err != nil {
			return err
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		r.write(line)
		return r.err
	}
	if summary == nil {
		// No summary, no footer: the format is closed and no more.
		r.mu.Lock()
		r.footerTemplate = ""
		r.mu.Unlock()
		return r.WriteFooter(time.Time{}, 0, 0, nil)
	}
	return r.WriteFooter(summary.EndedAt, summary.ExitCode, time.Duration(summary.DurationNS), summary.Collection)
}

// SummaryLine is the summary as one JSONL line, the stream's last under
// --format jsonl.
func SummaryLine(summary *records.Summary) ([]byte, error) {
	line, err := json.Marshal(summary)
	if err != nil {
		return nil, errorcodes.Errorf("record_encode_failed", "encode the summary: %w", err)
	}
	return append(line, '\n'), nil
}

// RunRenderer renders a daemon-backed run's records as the client verifies
// them from the canonical file: the record renderer
// for text and json, the verified line bytes for jsonl. It replaces the
// post-hoc RenderRunOutput on the daemon path.
type RunRenderer struct {
	out       io.Writer
	format    string
	r         *recordRenderer
	warnOut   io.Writer
	warnStyle display.LineStyle
}

// NewRunRenderer builds the renderer under the invocation's configuration;
// errOut, the client's standard error, takes each record's
// host_key_enrolled line.
func NewRunRenderer(cfg configload.Snapshot, quiet, debug bool, artifactDir, format string, echo, dynamicBorder, noBorder bool, out, errOut io.Writer) (*RunRenderer, error) {
	if format == "" {
		format = "text"
	}
	rr := &RunRenderer{out: out, format: format, warnOut: errOut, warnStyle: DisplayLineStyle(cfg, DisplayTerminal(errOut))}
	if format == "jsonl" {
		return rr, nil
	}
	if format != "text" && format != "json" {
		return nil, errorcodes.Errorf("run_output_format_unsupported", "unsupported run output format %q", format)
	}
	r, err := newRunReplayRenderer(out, format, cfg, quiet, debug, artifactDir, echo, dynamicBorder, noBorder)
	if err != nil {
		return nil, err
	}
	r.sayTo(errOut, cfg)
	rr.r = r
	return rr, nil
}

// Line renders one verified LF-terminated record line.
func (rr *RunRenderer) Line(line []byte) error {
	if rr.format == "jsonl" {
		// The line passes through undecoded; one naming a host-key notice
		// is decoded for its notices alone, the output (megabytes in a large
		// response) skipped rather than copied.
		if rr.warnOut != nil && bytes.Contains(line, []byte(`"code":"host_key_`)) {
			var record struct {
				Device  records.DeviceProjection `json:"device"`
				Notices []records.Notice         `json:"notices"`
			}
			if json.Unmarshal(line, &record) == nil {
				for _, l := range HostKeyLines(record.Device.CanonicalName, record.Notices, rr.warnStyle) {
					fmt.Fprintln(rr.warnOut, l)
				}
			}
		}
		_, err := rr.out.Write(line)
		return errorcodes.Ensure(err, "terminal_write_failed")
	}
	var record records.CommandRecord
	if err := json.Unmarshal(line, &record); err != nil {
		return errorcodes.Errorf("run_output_record_decode_failed", "decode record: %w", err)
	}
	rr.r.OnRecord(record)
	return rr.r.Error()
}

// Finish ends the display from the job's summary, the follow's terminal
// outcome (28.4): the footer in text, the summary line in jsonl (written
// here, since jsonl passes the verified bytes through without a record
// renderer), the array closed in json.
func (rr *RunRenderer) Finish(summary *records.Summary) error {
	if rr.r == nil {
		if summary == nil {
			return nil
		}
		line, err := SummaryLine(summary)
		if err != nil {
			return err
		}
		_, err = rr.out.Write(line)
		return errorcodes.Ensure(err, "terminal_write_failed")
	}
	if err := rr.r.finish(summary); err != nil {
		return err
	}
	return rr.r.Error()
}

// PingValues are the ICMP gate's values for display.ping.header: the target
// and the
// address as the headers carry them, <rtt1> and <rtt2> each probe's
// round-trip time, "timeout", or "error" when an ICMP error answer
// arrived, and <result> "proceeding" on one or two replies and "skipped"
// otherwise. The default template renders "! core-nyc-01 [2001:db8:10::1]
// ping(1) 2.1ms, ping(2) timeout, proceeding". login renders the same
// template to stderr.
func PingValues(target, address string, p *records.PingReport) display.Values {
	if target == "" {
		target = p.Address
	}
	if address == "" {
		address = p.Address
	}
	v := display.Values{Target: target, Address: address, Result: "skipped"}
	if p.Replies > 0 {
		v.Result = "proceeding"
	}
	for _, o := range p.Outcomes {
		switch o.Sequence {
		case 1:
			v.RTT1 = pingValue(o)
		case 2:
			v.RTT2 = pingValue(o)
		}
	}
	return v
}

// PingDebugLines are the lines --debug adds after PingLine: one per ICMP
// error answer with its detail and source, then each notice on the record
// (the packet-loss notice), all prefixed "karvi:".
func PingDebugLines(p *records.PingReport, notices []records.Notice) []string {
	lines := []string{}
	for _, o := range p.Outcomes {
		if o.Status != records.PingError {
			continue
		}
		line := fmt.Sprintf("karvi: ping(%d) error: %s", o.Sequence, o.Detail)
		if o.From != "" {
			line += ", from " + o.From
		}
		lines = append(lines, line)
	}
	for _, n := range notices {
		lines = append(lines, "karvi: "+n.Message)
	}
	return lines
}

func pingValue(o records.PingOutcome) string {
	switch o.Status {
	case records.PingReply:
		if o.RTTNS == nil {
			return "reply"
		}
		ms := float64(*o.RTTNS) / float64(time.Millisecond)
		if ms < 0.1 {
			return "<0.1ms"
		}
		return fmt.Sprintf("%.1fms", ms)
	case records.PingError:
		return "error"
	default:
		return "timeout"
	}
}
