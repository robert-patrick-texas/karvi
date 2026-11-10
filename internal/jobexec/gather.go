package jobexec

import (
	"cmp"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// The gathered display (display.run.gather; EXAMPLES chapter 106): a run's
// records gathered while the job runs and shown at its end. A block per
// requested command, the command identified across devices by its text,
// its privilege, and its occurrence in the device's list; in a block a
// group per answer, the devices whose records have the same status, error
// code, output digest, and on the exec channel exit status, signal, and
// stderr digest. A failed session-init record, which today's display
// shows, is gathered as a requested one in a block of its own ahead of
// them, by its profile and its command's place in the profile; a succeeded
// or stopped one is hidden, as today. While the job runs the renderer holds
// no output: per group its key and the line of its first record in
// commands.jsonl, per device its name. At the end each group with an answer to show is read
// back from the job's commands.jsonl (output.ReadRecordAt), its output
// streamed as today's display streams a record's. While it waits it counts
// the devices for the progress line (progress.go); the invocation's
// interrupt stops it at once, before the blocks or between a read-back's
// pieces (DECISIONS point 4, "Settled at S4").

// GatherFile gives the job's commands.jsonl at the gathered display's end,
// which closes it, each path its own way (in process the file the store
// holds open, on the follow paths opened by its name with the folder
// follow's checks), its failure coded.
type GatherFile func() (*os.File, error)

// blockKey is a block's command: its text, a root command (--sudo) apart
// from the same text without, and its occurrence in the device's list (the
// first `show clock`, the second); a session-init command (setup) by its
// profile and its index in the profile.
type blockKey struct {
	setup      bool
	profile    string
	command    string
	root       bool
	occurrence int
}

// answerKey is what makes two devices' answers the same: the record's
// status, its error code, its output's digest, and on the exec channel its
// exit status, signal, and stderr's digest, each "" where the record has
// none.
type answerKey struct {
	status, code, output string
	exit, signal, stderr string
}

// gatherBlock is one command's groups. position and index are the least
// (target's position in the plan, command index) of its records: the
// block's place in the plan, its commands in the order they first appear.
type gatherBlock struct {
	key             blockKey
	position, index int
	groups          map[answerKey]*gatherGroup
}

// gatherGroup is the devices that answered one way. first is the line of
// its first record in commands.jsonl, device and index that record's
// device and command, to check the line read back; answer says the record
// has output or stderr to show; message is its error's, shown for a group
// of one.
type gatherGroup struct {
	key     answerKey
	first   int64
	device  int32
	index   int
	answer  bool
	failed  bool
	message string
	devices []int32
}

// gatherer holds the groups while the job runs: each device's name once
// (the groups hold its number), and for each device in flight how often
// each command has come so far, which gives a record's occurrence; a
// device's counts go once its last command's record has come (a device's
// records come in its commands' order). done and fail are job list's DONE
// and FAIL from the records: the devices whose last requested record has
// come, and those of them with a requested record that did not succeed
// (failing holds the devices in flight that have one). interrupt is the
// invocation's: closed, the display stops.
type gatherer struct {
	open       GatherFile
	interrupt  <-chan struct{}
	names      []string
	byName     map[string]int32
	inFlight   map[string]map[[2]string]int
	failing    map[string]bool
	done, fail int
	blocks     map[blockKey]*gatherBlock
}

func newGatherer(open GatherFile, interrupt <-chan struct{}) *gatherer {
	return &gatherer{open: open, interrupt: interrupt, byName: map[string]int32{}, inFlight: map[string]map[[2]string]int{}, failing: map[string]bool{}, blocks: map[blockKey]*gatherBlock{}}
}

// errGatherInterrupted is the gathered display stopped by the
// invocation's interrupt; a follow's display ends with it (WriteFooter),
// its code cancelled (app.InterruptedDisplay).
var errGatherInterrupted = errorcodes.Errorf("cancelled", "the gathered display was interrupted")

// interrupted says whether the invocation's interrupt has come.
func (g *gatherer) interrupted() bool {
	select {
	case <-g.interrupt:
		return true
	default:
		return false
	}
}

// stopWriter is a read-back's writer that stops at the invocation's
// interrupt, checked before each piece.
type stopWriter struct {
	w io.Writer
	g *gatherer
}

func (s stopWriter) Write(p []byte) (int, error) {
	if s.g.interrupted() {
		return 0, errGatherInterrupted
	}
	return s.w.Write(p)
}

// stopReaderAt is the read-back's file stopped at the invocation's
// interrupt, checked before each read, so the one pass that finds the
// groups' lines (output.FindLines) and each group's scan of its line
// (output.ReadRecordAt), which wrap the error with %w, stop at once
// however large the file or the answer.
type stopReaderAt struct {
	r io.ReaderAt
	g *gatherer
}

func (s stopReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if s.g.interrupted() {
		return 0, errGatherInterrupted
	}
	return s.r.ReadAt(p, off)
}

// recordDevice is the name the display shows for a record's device.
func recordDevice(rec *records.CommandRecord) string {
	if rec.Device.CanonicalName != "" {
		return rec.Device.CanonicalName
	}
	return rec.InputTarget
}

// add takes a record's key and line into its block's group: a requested
// record's, and a session-init record's that today's display shows (a
// failure); a succeeded or stopped one is setup, hidden as today.
func (g *gatherer) add(rec *records.CommandRecord) {
	setup := rec.CommandKind == "session_init"
	if setup && hiddenRecord(rec) {
		return
	}
	name := recordDevice(rec)
	device, ok := g.byName[name]
	if !ok {
		device = int32(len(g.names))
		g.names = append(g.names, name)
		g.byName[name] = device
	}
	key := blockKey{setup: true, profile: rec.SessionInitProfile, command: rec.Command, occurrence: rec.CommandIndex}
	if !setup {
		key = g.requestedKey(rec, name)
	}
	b := g.blocks[key]
	if b == nil {
		b = &gatherBlock{key: key, position: rec.Dispatch.ScopePosition, index: rec.CommandIndex, groups: map[answerKey]*gatherGroup{}}
		g.blocks[key] = b
	} else if cmp.Or(cmp.Compare(rec.Dispatch.ScopePosition, b.position), cmp.Compare(rec.CommandIndex, b.index)) < 0 {
		b.position, b.index = rec.Dispatch.ScopePosition, rec.CommandIndex
	}
	answer := answerOf(rec)
	group := b.groups[answer]
	if group == nil {
		group = &gatherGroup{key: answer, first: rec.Sequence, device: device, index: rec.CommandIndex,
			answer: rec.OutputBytes > 0 || (rec.StderrBytes != nil && *rec.StderrBytes > 0), failed: rec.Error != nil}
		if rec.Error != nil {
			group.message = rec.Error.Message
		}
		b.groups[answer] = group
	}
	group.devices = append(group.devices, device)
}

// requestedKey is a requested record's block: its command with its
// occurrence among the device's commands so far.
func (g *gatherer) requestedKey(rec *records.CommandRecord, name string) blockKey {
	unit := rec.Device.ID
	if unit == "" {
		unit = name
	}
	counts := g.inFlight[unit]
	if counts == nil {
		counts = map[[2]string]int{}
		g.inFlight[unit] = counts
	}
	which := [2]string{rec.Command, rec.Privilege}
	counts[which]++
	key := blockKey{command: rec.Command, root: rec.Root(), occurrence: counts[which]}
	failed := g.failing[unit] || rec.Status != "success"
	if rec.CommandIndex >= rec.CommandCount {
		delete(g.inFlight, unit)
		delete(g.failing, unit)
		// A device dispatch never started (a halt, a wave gate, a cancel or
		// a shutdown before its start) is neither: job list's DONE and FAIL
		// count the devices dispatch ran. Its records are EmitUnstarted's,
		// the only ones whose error's operation is dispatch.
		if rec.Error == nil || rec.Error.Operation != "dispatch" {
			g.done++
			if failed {
				g.fail++
			}
		}
	} else if failed {
		g.failing[unit] = true
	}
	return key
}

func answerOf(rec *records.CommandRecord) answerKey {
	k := answerKey{status: rec.Status, output: rec.OutputSHA256}
	if rec.Error != nil {
		k.code = rec.Error.Code
	}
	if rec.ExitStatus != nil {
		k.exit = strconv.Itoa(*rec.ExitStatus)
	}
	if rec.ExitSignal != nil {
		k.signal = "signal " + *rec.ExitSignal
	}
	if rec.StderrSHA256 != nil {
		k.stderr = *rec.StderrSHA256
	}
	return k
}

// sortedBlocks is the blocks in the plan's order, the session-init blocks
// first (setup runs first on every device).
func (g *gatherer) sortedBlocks() []*gatherBlock {
	blocks := make([]*gatherBlock, 0, len(g.blocks))
	for _, b := range g.blocks {
		blocks = append(blocks, b)
	}
	slices.SortFunc(blocks, func(x, y *gatherBlock) int {
		if x.key.setup != y.key.setup {
			if x.key.setup {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(x.position, y.position), cmp.Compare(x.index, y.index))
	})
	return blocks
}

// names is a group's device names, in the order their records came.
func (g *gatherer) namesOf(group *gatherGroup) []string {
	names := make([]string, len(group.devices))
	for i, d := range group.devices {
		names[i] = g.names[d]
	}
	return names
}

// sortedGroups is a block's groups as the display shows them: the
// succeeded first, then the failures, each the largest first, a tie by the
// first name in the names' natural order (matching.CompareNames, the
// order a header's fold lists them in).
func (g *gatherer) sortedGroups(b *gatherBlock) []*gatherGroup {
	groups := make([]*gatherGroup, 0, len(b.groups))
	first := make(map[*gatherGroup]string, len(b.groups))
	for _, group := range b.groups {
		groups = append(groups, group)
		least := g.names[group.devices[0]]
		for _, d := range group.devices[1:] {
			if matching.CompareNames(g.names[d], least) < 0 {
				least = g.names[d]
			}
		}
		first[group] = least
	}
	slices.SortFunc(groups, func(x, y *gatherGroup) int {
		xs, ys := x.key.status == "success", y.key.status == "success"
		if xs != ys {
			if xs {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(len(y.devices), len(x.devices)), matching.CompareNames(first[x], first[y]))
	})
	return groups
}

// failureLine is a group's line when it did not succeed, today's failure
// line without the target the header names: its status and error code,
// and the message when the group is one device (for several the messages
// stay in errors.jsonl and each device's output file); a record not sent
// its status alone.
func (group *gatherGroup) failureLine() string {
	switch {
	case group.key.status == "success":
		return ""
	case !group.failed:
		return "karvi: status=" + group.key.status
	case len(group.devices) == 1:
		return "karvi: status=" + group.key.status + " error=" + group.key.code + ": " + group.message
	}
	return "karvi: status=" + group.key.status + " error=" + group.key.code
}

// gathers says whether the renderer can gather: a run's or a command's
// text display, never under another format, for another activity, in the
// daemon (which formats nothing), or under the live view.
func (r *recordRenderer) gathers() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.format == "text" && (r.activityType == "run" || r.activityType == "command") && !r.daemon && !r.live
}

// gatherFrom has the renderer gather its display where it can (gathers),
// read back at the end through open, stopped by interrupt (the
// invocation's), its progress drawn on progress (nil for none), drawn now
// as the display starts and erased when the interrupt comes; it says
// whether it does. An interrupt already come leaves the line never drawn.
func (r *recordRenderer) gatherFrom(open GatherFile, interrupt <-chan struct{}, progress *ProgressLine) bool {
	if !r.gathers() {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gather = newGatherer(open, interrupt)
	r.progress = progress
	if r.gather.interrupted() {
		progress.Stop()
		return true
	}
	progress.stopAt(interrupt)
	progress.show(0, 0)
	return true
}

// Gather has a run's text display gathered, read back at the end through
// open (gatherFrom), stopped by ctx's end (the invocation's interrupt);
// false when the format is not text. At a terminal (ShowsProgress) the
// progress line over the job's devices is drawn on the renderer's
// standard error, and is that standard error from here (Stderr); devices
// is asked only then, so a display that does not gather, or shows no
// line, never reads what it reads (a follow's summary.json), and its
// error is Gather's.
func (rr *RunRenderer) Gather(ctx context.Context, open GatherFile, devices func() (int, error)) (bool, error) {
	if rr.r == nil || !rr.r.gathers() {
		return false, nil
	}
	var progress *ProgressLine
	if ShowsProgress(rr.warnOut, rr.r.quiet) {
		m, err := devices()
		if err != nil {
			return false, err
		}
		progress = NewProgressLine(rr.warnOut, rr.warnStyle, m)
	}
	rr.r.gatherFrom(open, ctx.Done(), progress)
	if progress != nil {
		rr.warnOut = progress
		rr.r.mu.Lock()
		rr.r.warnOut = progress
		rr.r.mu.Unlock()
	}
	return true, nil
}

// Stderr is the follow's standard error: the progress line while the
// display is gathered at a terminal, so the follow's own lines erase and
// redraw it, otherwise the standard error the renderer was given.
func (rr *RunRenderer) Stderr() io.Writer { return rr.warnOut }

// StopProgress erases the progress line, for every end of a follow (an
// interrupt, a lost stream); the display's own end erased it already.
func (rr *RunRenderer) StopProgress() {
	if p, ok := rr.warnOut.(*ProgressLine); ok {
		p.Stop()
	}
}

// endGathered writes the gathered display, once: in process when every
// record of the job is in its commands.jsonl, where today's display has
// written its last record, so that the job's own failures found after its
// records follow the blocks as they follow today's records; on the follow
// paths from WriteFooter, before the footer. The progress line is erased
// first. After an error nothing is written; a file that cannot be read
// back is run_output_records_unreadable. In process an interrupt is the
// job's own (its end and exit 113 follow, then the footer), so endGathered
// says nothing of it; WriteFooter on a follow path ends the display there.
func (r *recordRenderer) endGathered() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endGatheredLocked()
}

// endGatheredLocked says whether the invocation's interrupt stopped the
// display: before it wrote anything (a Ctrl-C while the job ran), or
// between a read-back's pieces, what was written standing and a cut line
// ended with its line break.
func (r *recordRenderer) endGatheredLocked() (interrupted bool) {
	g := r.gather
	r.gather = nil
	r.progress.Stop()
	if g == nil || r.err != nil {
		return false
	}
	err := r.writeGatheredLocked(g)
	switch {
	case errors.Is(err, errGatherInterrupted):
		if r.lastByte != 0 && r.lastByte != '\n' {
			r.write([]byte{'\n'})
		}
		return true
	case err != nil:
		r.setErrorLocked("run_output_records_unreadable", err)
	}
	return false
}

// gatherKey is the key that gathers an activity's display, in the
// activity's family of display keys as its header and footer are: a
// command's display.command.gather, a run's display.run.gather.
func gatherKey(activityType string) string {
	if activityType == "command" {
		return "display.command.gather"
	}
	return "display.run.gather"
}

// appendBorderLocked appends the border due before what follows.
func (r *recordRenderer) appendBorderLocked(data []byte) []byte {
	if r.pendingBorder != "" {
		data = append(data, r.styledBorder(r.pendingBorder, r.pendingBorderColor)...)
		r.pendingBorder, r.pendingBorderColor = "", ""
	}
	return data
}

// markerLine is a block's first line, the collection file's marker
// (docs/COLLECTION.md): "! COMMAND", a root command "! --sudo COMMAND", a
// session-init command "! session_init=PROFILE: COMMAND" (its profile as
// today's failure line names it); the marker's words in the label colour,
// the profile and the command in the value colour.
func (r *recordRenderer) markerLine(key blockKey) string {
	label := func(text string) string { return display.ANSIStyle(text, r.lineStyle.Label, r.colorEnabled, false) }
	value := func(text string) string { return display.ANSIStyle(text, r.lineStyle.Value, r.colorEnabled, false) }
	switch {
	case key.setup:
		return label("! session_init=") + value(key.profile) + label(": ") + value(key.command)
	case key.root:
		return label("! --sudo ") + value(key.command)
	}
	return label("! ") + value(key.command)
}

// writeGatheredLocked writes g's display at the job's end, before the
// footer: each block's marker, then its groups, each its header
// (display.gather.header, one line however long), its answer read back
// when it has one, and its failure line, the run's border between groups
// and between blocks, a block's border and marker written with its first
// group; under --quiet no marker, header, or border. A commands.jsonl that
// cannot be read, or a line that is not the record the job's stream
// carried, is run_output_records_unreadable: the groups written stand, and
// the footer is not written. The invocation's interrupt is
// errGatherInterrupted: before the first group nothing is written, after
// it the display stops before the next group or the answer's next piece.
func (r *recordRenderer) writeGatheredLocked(g *gatherer) error {
	if g.interrupted() {
		return errGatherInterrupted
	}
	blocks := g.sortedBlocks()
	var lines []int64
	for _, b := range blocks {
		for _, group := range b.groups {
			if group.answer {
				lines = append(lines, group.first)
			}
		}
	}
	var (
		f     *os.File
		spans map[int64]output.LineSpan
	)
	if len(lines) > 0 {
		var err error
		if f, err = g.open(); err != nil {
			return errorcodes.Ensure(err, "run_output_records_unreadable")
		}
		defer f.Close()
		if spans, err = output.FindLines(stopReaderAt{r: f, g: g}, f.Name(), lines); err != nil {
			return err
		}
	}
	for _, b := range blocks {
		var lead []byte
		if !r.quiet {
			lead = appendLine(r.appendBorderLocked(nil), r.markerLine(b.key))
		}
		for _, group := range g.sortedGroups(b) {
			if g.interrupted() {
				return errGatherInterrupted
			}
			data := r.appendBorderLocked(lead)
			lead = nil
			r.currentHeaderWidth = 0
			if !r.quiet && r.gatherTemplate != "" {
				values := display.Values{Targets: matching.Condense(g.namesOf(group)), Count: len(group.devices)}
				line, err := r.formatter.RenderStyledLine(r.gatherTemplate, values, r.lineStyle)
				if err != nil {
					return errorcodes.Ensure(err, "config_display_template_invalid")
				}
				r.currentHeaderWidth = display.VisibleWidth(line)
				data = appendLine(data, line)
			}
			if group.answer {
				rec, src, err := output.ReadRecordAt(stopReaderAt{r: f, g: g}, f.Name(), group.first, spans[group.first])
				if err != nil {
					return err
				}
				if err := g.check(f.Name(), group, &rec); err != nil {
					return err
				}
				// The notice is the follow stream's, which the store never
				// writes in the file: the answer is the line's, read back.
				rec.Notices = slices.DeleteFunc(rec.Notices, func(n records.Notice) bool { return n.Code == "follow_output_omitted" })
				if err := r.writeAnswerLocked(rec, src, data, g); err != nil {
					return err
				}
				data = nil
			}
			if line := group.failureLine(); line != "" {
				data = appendLine(data, display.ANSIStyle(line, r.lineStyle.Error, r.colorEnabled, true))
			}
			r.write(data)
			if r.err != nil {
				return r.err
			}
			r.pendingBorder, r.pendingBorderColor = r.nextBorder()
		}
	}
	return r.err
}

// check is the line read back against the group's first record as the
// job's stream carried it: its sequence, device, command, and answer (the
// group's key: status, error code, output digest, and on the exec channel
// exit status, signal, and stderr digest); another record is
// run_output_records_unreadable, the two stacked.
func (g *gatherer) check(name string, group *gatherGroup, rec *records.CommandRecord) error {
	read := answerOf(rec)
	if rec.Sequence == group.first && recordDevice(rec) == g.names[group.device] && rec.CommandIndex == group.index && read == group.key {
		return nil
	}
	return errorcodes.Errorf("run_output_records_unreadable", "%s line %d is not the record the job's stream carried\n  carried  record %d, %s command %d, %s\n  read     record %d, %s command %d, %s",
		name, group.first, group.first, g.names[group.device], group.index, group.key.stacked(read), rec.Sequence, recordDevice(rec), rec.CommandIndex, read.stacked(group.key))
}

// stacked is k for a message beside other: its output digest and status,
// then each of the rest either one has (error code, exit, signal, stderr
// digest), so the two lines name the same fields.
func (k answerKey) stacked(other answerKey) string {
	text := "output " + k.output + ", status " + k.status
	for _, f := range []struct{ name, mine, theirs string }{
		{"code", k.code, other.code}, {"exit", k.exit, other.exit}, {"signal", strings.TrimPrefix(k.signal, "signal "), other.signal}, {"stderr", k.stderr, other.stderr},
	} {
		if f.mine != "" || f.theirs != "" {
			text += ", " + f.name + " " + cmp.Or(f.mine, "none")
		}
	}
	return text
}
