package jobexec

import (
	"io"
	"os"
	"strconv"
	"sync"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// The gathered display's progress line (DECISIONS point 4, "Settled at
// S4"): while a gathered display waits for the job's end, at a terminal,
// one line on standard error, `gather: done=N/M fail=F`, drawn when the
// display starts, rewritten as each device ends, and erased before the
// blocks, when the invocation's interrupt comes, and on an error. N and F
// are job list's DONE and FAIL counted from the records (the gatherer), M
// the job's devices. The line is the job's standard error while it may
// show: every line written there (a warning, a debug line, a host-key
// line) erases it first and draws it again after, so nothing is written
// after a half line. Standard error is judged by itself, as the colour
// policy judges its stream, never by whether colour is on: none to a pipe
// or a file, none under --quiet. It is drawn only where it renders
// correctly, checked before each draw: karvi's process group the
// terminal's foreground group (not a job sent to the background, whose
// draw would write over the shell's line or stop it under tostop), and
// the terminal wider than the line, one column spare (a narrower one wraps
// it, each redraw leaving a row behind, and at exactly its width the clear
// to the line's end erases its last character on xterm-class terminals);
// otherwise the draw is skipped and the counts kept, so the next record
// draws it once it can.

// redrawLine moves to the line's start, clearRest clears from there to
// the line's end (what a shorter text does not cover), and eraseLine is
// both: the line gone, the cursor at its start.
const (
	redrawLine = "\r"
	clearRest  = "\x1b[K"
	eraseLine  = redrawLine + clearRest
)

// ShowsProgress says whether a gathered display shows its progress line
// on stderr: when stderr is itself a terminal, and not under --quiet.
func ShowsProgress(stderr io.Writer, quiet bool) bool {
	return !quiet && terminalFile(stderr) != nil
}

// ProgressLine is the line on a terminal's standard error, and that
// standard error's writer for the job.
type ProgressLine struct {
	mu       sync.Mutex
	out      io.Writer
	terminal *os.File
	style    display.LineStyle
	devices  int
	// foreground says whether karvi's process group is the terminal's
	// foreground group, true where the terminal cannot say (not the
	// controlling terminal); columns is the terminal's width, 0 where it
	// cannot say. Nil, the line is always placed (a test's writer).
	foreground func() bool
	columns    func() int
	// text is the line with the latest counts; shown says it is on the
	// screen now; partial that a write not ending a line came after it,
	// so it is not drawn until a write ends one; started says the display
	// has asked for it once, stopped that it is never drawn again;
	// interrupt is the invocation's (stopAt), looked at before each draw;
	// halt, closed at Stop, ends the watch of it.
	text             string
	shown, partial   bool
	started, stopped bool
	done, fail       int
	interrupt        <-chan struct{}
	halt             chan struct{}
}

// NewProgressLine is the progress line over devices on stderr, styled by
// style (stderr's own); nil when stderr is not a terminal.
func NewProgressLine(stderr io.Writer, style display.LineStyle, devices int) *ProgressLine {
	terminal := terminalFile(stderr)
	if terminal == nil {
		return nil
	}
	return &ProgressLine{out: stderr, terminal: terminal, style: style, devices: devices,
		foreground: func() bool {
			foreground, known := osutil.ForegroundGroup(terminal)
			return foreground || !known
		},
		columns: func() int { return osutil.TerminalWidth(terminal) }}
}

// inForeground says whether the line may be written on the terminal now:
// karvi in its foreground, or a terminal that cannot say.
func (p *ProgressLine) inForeground() bool { return p.foreground == nil || p.foreground() }

// fits says whether the terminal is wider than the line's text, or
// cannot say.
func (p *ProgressLine) fits() bool {
	if p.columns == nil {
		return true
	}
	columns := p.columns()
	return columns == 0 || display.VisibleWidth(p.text) < columns
}

// interruptedLocked stops the line once the invocation's interrupt has
// come, and says so: a draw that comes before the watch's Stop (a record
// just after the Ctrl-C) erases it instead.
func (p *ProgressLine) interruptedLocked() bool {
	select {
	case <-p.interrupt:
		p.stopLocked()
		return true
	default:
		return false
	}
}

// Terminal is the terminal the line is drawn on, so the colour policy
// judges a write through the line as one to that terminal
// (DisplayDestination).
func (p *ProgressLine) Terminal() *os.File { return p.terminal }

// Write writes b to standard error: the line erased first when it shows,
// and drawn again after b when b ends a line and the line is placed; a b
// that does not end a line leaves it off until a write ends one. In the
// background the line is not touched (an erase would clear the shell's
// line), b written after it. An empty b writes nothing.
func (p *ProgressLine) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(b) == 0 {
		return 0, nil
	}
	if !p.started || p.stopped || p.interruptedLocked() {
		return p.out.Write(b)
	}
	foreground := p.inForeground()
	var lead []byte
	if p.shown && foreground {
		lead = []byte(eraseLine)
	}
	data := append(lead, b...)
	ends := b[len(b)-1] == '\n'
	redraw := ends && foreground && p.fits()
	if redraw {
		data = append(append(data, redrawLine+p.text...), clearRest...)
	}
	n, err := p.out.Write(data)
	p.shown, p.partial = redraw && err == nil && n == len(data), !ends
	return min(max(n-len(lead), 0), len(b)), err
}

// show draws the line with these counts when it is not on the screen or
// they changed, and it is placed (karvi in the terminal's foreground, the
// terminal wide enough) with no part line after it; otherwise it keeps
// the counts for the next draw, a line on the screen that no longer fits
// erased. Nothing once stopped. A nil line draws nothing.
func (p *ProgressLine) show(done, fail int) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped || p.interruptedLocked() || (p.shown && done == p.done && fail == p.fail) {
		return
	}
	p.started, p.done, p.fail, p.text = true, done, fail, p.textOf(done, fail)
	switch {
	case p.partial:
		return
	case !p.inForeground():
		p.shown = false
		return
	case !p.fits():
		if p.shown {
			_, _ = io.WriteString(p.out, eraseLine)
		}
		p.shown = false
		return
	}
	n, err := io.WriteString(p.out, redrawLine+p.text+clearRest)
	p.shown = err == nil && n == len(redrawLine+p.text+clearRest)
}

// textOf is the line's text, styled like the footer: its words in the
// label colour, its counts in the value colour.
func (p *ProgressLine) textOf(done, fail int) string {
	label := func(s string) string { return display.ANSIStyle(s, p.style.Label, p.style.Enabled, false) }
	value := func(s string) string { return display.ANSIStyle(s, p.style.Value, p.style.Enabled, false) }
	return label("gather: done=") + value(strconv.Itoa(done)+"/"+strconv.Itoa(p.devices)) + label(" fail=") + value(strconv.Itoa(fail))
}

// Stop erases the line (in the foreground) and never draws it again;
// standard error is written through unchanged after. A nil line, or a
// second Stop, does nothing.
func (p *ProgressLine) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopLocked()
}

func (p *ProgressLine) stopLocked() {
	if p.stopped {
		return
	}
	if p.shown && p.inForeground() {
		_, _ = io.WriteString(p.out, eraseLine)
	}
	p.shown, p.stopped = false, true
	if p.halt != nil {
		close(p.halt)
	}
}

// stopAt stops the line when interrupt closes (the invocation's Ctrl-C),
// at once rather than at the display's end, so the records that come
// after it (an in-process job's cancelled devices) draw nothing: a
// goroutine watches it, for the erase when nothing more is written, and
// each draw looks at it first, for one that comes before the watch runs.
// The watch ends at Stop. A nil line or interrupt watches nothing.
func (p *ProgressLine) stopAt(interrupt <-chan struct{}) {
	if p == nil || interrupt == nil {
		return
	}
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	if p.halt == nil {
		p.halt = make(chan struct{})
	}
	p.interrupt = interrupt
	halt := p.halt
	p.mu.Unlock()
	go func() {
		select {
		case <-interrupt:
			p.Stop()
		case <-halt:
		}
	}()
}
