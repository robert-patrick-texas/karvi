package jobexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/testpty"
	"github.com/robert-patrick-texas/karvi/records"
)

// The gathered display's progress line (DECISIONS point 4, "Settled at
// S4"): at a terminal, one line on standard error, `gather: done=N/M
// fail=F`, drawn when the gathered display starts, rewritten as each device
// ends, erased and drawn again around every other line written to standard
// error, and erased before the blocks; none to a pipe, a file, or under
// --quiet. And the invocation's interrupt: the gathered display stops at
// once, before the blocks or between a read-back's pieces.

// drawn is the line as the terminal is sent it, colour off.
func drawn(done, devices, fail int) string {
	return fmt.Sprintf("\rgather: done=%d/%d fail=%d\x1b[K", done, devices, fail)
}

// progressConfig is the configuration of a gathered run, the footer left
// out, under sets.
func progressConfig(t *testing.T, sets ...string) configload.Snapshot {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: append([]string{"display.color=never", "display.run.footer="}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// progressRenderer is a run's text renderer gathering from path, its
// standard error stderr through the job's progress line over devices, as
// the in-process run builds them; interrupt, when set, is the
// invocation's.
func progressRenderer(t *testing.T, out io.Writer, path string, stderr *os.File, devices int, interrupt <-chan struct{}, sets ...string) (*recordRenderer, *ProgressLine) {
	t.Helper()
	cfg := progressConfig(t, sets...)
	r, err := newRecordRenderer(out, "text", cfg, false, false, "", "/srv/jobs/261108-130024-00", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := NewProgressLine(stderr, DisplayLineStyle(cfg, DisplayDestination(stderr)), devices)
	if p == nil {
		t.Fatal("no progress line at a terminal")
	}
	r.sayTo(p, cfg)
	if !r.gatherFrom(func() (*os.File, error) { return os.Open(path) }, interrupt, p) {
		t.Fatal("the renderer does not gather")
	}
	return r, p
}

// labProgress is what the terminal is sent while the lab job's
// show-version.jsonl is gathered over its eight devices, colour off, up to
// the line's last draw: the line drawn at the display's start as
// done=0/8, each of the seven host-key lines written on a line of its own
// (the line erased first, drawn again after), and the line rewritten as
// each device ends, router09 refused.
func labProgress() string {
	want := drawn(0, 8, 0)
	done, fail := 0, 0
	for _, device := range []struct {
		name    string
		hostKey bool
		failed  bool
	}{{"router01", true, false}, {"router04", true, false}, {"router03", true, false}, {"router02", true, false}, {"router09", false, true}, {"edge1", true, false}, {"router10", true, false}, {"edge2", true, false}} {
		if device.hostKey {
			want += eraseLine + "! ssh accepted new host-key " + device.name + " (ED25519)\n" + drawn(done, 8, fail)
		}
		done++
		if device.failed {
			fail++
		}
		want += drawn(done, 8, fail)
	}
	return want
}

// TestProgressLineAtATerminal: the lab job's eight devices, seven of them
// enrolling their host keys and router09 refused: the line is drawn at the
// display's start as done=0/8, each host-key line is written on a line of
// its own (the line erased first, drawn again after), the line is
// rewritten as each device ends, and erased before the blocks, which are
// today's gathered layout.
func TestProgressLineAtATerminal(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	var out bytes.Buffer
	r, p := progressRenderer(t, &out, path, slave, 8, nil)
	feed(t, r, path)
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	p.Stop()
	got := term.Wait(t, slave)
	if want := labProgress() + eraseLine; got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
	gathered, err := os.ReadFile(filepath.Join("testdata", "gather", "show-version.gathered"))
	if err != nil {
		t.Fatal(err)
	}
	if out.String() != string(gathered) {
		t.Errorf("the blocks:\n%s\nwant:\n%s", out.String(), gathered)
	}
}

// draws is the progress lines the terminal was sent, their texts in
// order: a line drawn again unchanged after a line written to standard
// error (a host-key line) once, but one drawn again straight after the
// same draw, nothing written between, twice: that redraw shows nothing
// new, and the line is not drawn for a record that ends no device.
func draws(term string) []string {
	var lines []string
	between := true
	for _, piece := range strings.Split(term, "\rgather: ")[1:] {
		line, after, _ := strings.Cut(piece, "\x1b[K")
		if line = "gather: " + line; len(lines) == 0 || lines[len(lines)-1] != line || !between {
			lines = append(lines, line)
		}
		between = after != ""
	}
	return lines
}

// TestProgressLineCounts: a device is done when its last requested record
// comes, and failed when one of its requested records did not succeed,
// job list's DONE and FAIL from the records alone: over the lab jobs (one
// list for all with a refused device, two commands with its not-attempted
// second, a --cf folder's lists of three and one, three device errors); a
// profile failing under stop (the requested record not attempted) fails
// its device, one failing under continue (the requested record succeeded)
// does not, and a cancelled device counts as failed. Records that end no
// device draw nothing.
func TestProgressLineCounts(t *testing.T) {
	for _, c := range []struct {
		job     string
		devices int
		last    string
		draws   int
	}{
		{"show-version", 8, "gather: done=8/8 fail=1", 9},
		{"two-commands", 8, "gather: done=8/8 fail=1", 9},
		{"cf-dir", 3, "gather: done=3/3 fail=0", 4},
		{"device-error", 3, "gather: done=3/3 fail=3", 4},
	} {
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		path := filepath.Join("testdata", "gather", c.job+".jsonl")
		var out bytes.Buffer
		r, p := progressRenderer(t, &out, path, slave, c.devices, nil)
		feed(t, r, path)
		if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
			t.Fatal(err)
		}
		p.Stop()
		got := term.Wait(t, slave)
		if lines := draws(got); len(lines) != c.draws || lines[len(lines)-1] != c.last || !strings.HasSuffix(got, eraseLine) {
			t.Errorf("%s: drawn %q, want %d ending %q\n%q", c.job, lines, c.draws, c.last, got)
		}
	}

	path, recs := writeJob(t, []records.CommandRecord{
		withError(sessionInit("stop", 1, 1, 1, "iosxe", "terminal length 0", "timeout"), "command_timeout", "timed out"),
		shellAnswer("stop", 1, 1, 1, "show version", "not_attempted_session_init_failure", ""),
		withError(sessionInit("cont", 2, 1, 1, "iosxe", "terminal length 0", "timeout"), "command_timeout", "timed out"),
		shellAnswer("cont", 2, 1, 1, "show version", "success", "v\n"),
		shellAnswer("gone", 3, 1, 1, "show version", "cancel", ""),
		shellAnswer("ok", 4, 1, 1, "show version", "success", "v\n"),
	})
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	var out bytes.Buffer
	r, p := progressRenderer(t, &out, path, slave, 4, nil)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	p.Stop()
	want := drawn(0, 4, 0) + drawn(1, 4, 1) + drawn(2, 4, 1) + drawn(3, 4, 2) + drawn(4, 4, 2) + eraseLine
	if got := term.Wait(t, slave); got != want {
		t.Errorf("profiles and a cancel:\n%q\nwant\n%q", got, want)
	}
}

// TestProgressLineOnlyAtATerminal: standard error is judged by itself, as
// the colour policy judges its stream, never by whether colour is on: a
// pipe, a regular file, or a buffer has no line, even under
// display.color=always, and --quiet has none at a terminal; a gathered
// display to a pipe writes nothing on it.
func TestProgressLineOnlyAtATerminal(t *testing.T) {
	cfg := progressConfig(t, "display.color=always")
	style := DisplayLineStyle(cfg, display.Terminal)
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pr.Close()
	defer pw.Close()
	file, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for name, stderr := range map[string]io.Writer{"a pipe": pw, "a file": file, "a buffer": &bytes.Buffer{}} {
		if p := NewProgressLine(stderr, style, 2); p != nil {
			t.Errorf("%s: a progress line", name)
		}
		if ShowsProgress(stderr, false) {
			t.Errorf("%s: ShowsProgress", name)
		}
	}
	_, slave := testpty.Open(t)
	if !ShowsProgress(slave, false) || ShowsProgress(slave, true) {
		t.Errorf("at a terminal: ShowsProgress %v, under --quiet %v", ShowsProgress(slave, false), ShowsProgress(slave, true))
	}

	// The follow's renderer to a pipe: nothing on standard error.
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	var out bytes.Buffer
	rr, err := NewRunRenderer(progressConfig(t, "display.run.gather=true"), false, false, "/srv/jobs/x", "text", false, false, false, &out, pw)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rr.Gather(context.Background(), openPath(path), devicesOf(8)); !ok || err != nil {
		t.Fatal("the follow's renderer does not gather")
	}
	if rr.Stderr() != pw {
		t.Errorf("the follow's standard error became %T", rr.Stderr())
	}
	feed(t, rr.r, path)
	if err := rr.Finish(&records.Summary{}); err != nil {
		t.Fatal(err)
	}
	pw.Close()
	var said bytes.Buffer
	if _, err := said.ReadFrom(pr); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(said.String(), "gather:") || strings.Contains(said.String(), "\x1b[K") {
		t.Errorf("the pipe was sent %q", said.String())
	}
}

// TestProgressLineStyle: at a terminal with colour on, the line is styled
// like the footer, its words in the label colour and its counts in the
// value colour; under --nocolor (TestProgressLineAtATerminal) it is plain.
// Colour is on by the default policy, display.color=auto, at a terminal
// with NO_COLOR unset, whatever the environment the test runs in.
func TestProgressLineStyle(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	_, slave := testpty.Open(t)
	cfg := progressConfig(t, "display.color=auto")
	style := DisplayLineStyle(cfg, DisplayDestination(slave))
	if !style.Enabled {
		t.Fatal("colour is off at a terminal under display.color=auto")
	}
	p := NewProgressLine(slave, style, 12)
	want := display.ANSIStyle("gather: done=", style.Label, true, false) + display.ANSIStyle("3/12", style.Value, true, false) +
		display.ANSIStyle(" fail=", style.Label, true, false) + display.ANSIStyle("1", style.Value, true, false)
	if got := p.textOf(3, 1); got != want {
		t.Errorf("styled %q, want %q", got, want)
	}
}

// TestProgressLineWrites: the line is the job's standard error: a write
// before the line is drawn, or after it stops, passes through unchanged; a
// whole line written while it shows erases it and draws it again after; a
// part line leaves it erased until the next draw; the writer is a terminal
// to the colour policy.
func TestProgressLineWrites(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	cfg := progressConfig(t)
	p := NewProgressLine(slave, DisplayLineStyle(cfg, DisplayDestination(slave)), 2)
	if DisplayDestination(p) != display.Terminal || p.Terminal() != slave {
		t.Fatalf("the progress line is %v to the colour policy", DisplayDestination(p))
	}
	for _, step := range []func(){
		func() { fmt.Fprint(p, "before\n") },
		func() { p.show(0, 0) },
		func() { fmt.Fprint(p, "warning: one\n") },
		func() { fmt.Fprint(p, "part") },
		func() { fmt.Fprint(p, " line\n") },
		func() { p.show(0, 0) },
		func() { p.show(1, 0) },
		func() { p.Stop() },
		func() { p.Stop() },
		func() { p.show(2, 0) },
		func() { fmt.Fprint(p, "after\n") },
	} {
		step()
	}
	want := "before\n" + drawn(0, 2, 0) + eraseLine + "warning: one\n" + drawn(0, 2, 0) + eraseLine + "part" + " line\n" + drawn(0, 2, 0) + drawn(1, 2, 0) + eraseLine + "after\n"
	if got := term.Wait(t, slave); got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
}

// interruptAfter is a standard output that closes stop at its nth write.
type interruptAfter struct {
	bytes.Buffer
	n    int
	stop chan struct{}
}

func (w *interruptAfter) Write(p []byte) (int, error) {
	if w.n--; w.n == 0 {
		close(w.stop)
	}
	return w.Buffer.Write(p)
}

// bigAnswers is a job of two devices whose answers differ, r1's 4 MiB of
// one line without a line break, read back in pieces.
func bigAnswers(t *testing.T) (string, []records.CommandRecord) {
	t.Helper()
	return writeJob(t, []records.CommandRecord{
		shellAnswer("r1", 1, 1, 1, "show big", "success", strings.Repeat("x", 4<<20)),
		shellAnswer("r2", 2, 1, 1, "show big", "success", "small\n"),
	})
}

// TestGatheredInterrupted: the invocation's interrupt stops the gathered
// display at once. Before the end (a Ctrl-C while the job ran) nothing
// gathered is written: in process the footer follows (the job's end, its
// exit 113, decided by the job); on a follow path the display ends there,
// cancelled, no footer. During the read-back it stops between the answer's
// pieces: what was written stands, the cut line ended with its line break,
// no other group; in process the footer after, on a follow path cancelled.
// The progress line is erased first either way.
func TestGatheredInterrupted(t *testing.T) {
	path, recs := bigAnswers(t)
	footer := "display.run.footer=! exit=<exit-code>"
	for _, inProcess := range []bool{true, false} {
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		stop := make(chan struct{})
		var out bytes.Buffer
		r, _ := progressRenderer(t, &out, path, slave, 2, stop, footer)
		for _, rec := range recs {
			r.OnRecord(rec)
		}
		close(stop)
		if inProcess {
			r.endGathered()
		}
		err := r.WriteFooter(time.Now(), 113, time.Second, nil)
		switch {
		case inProcess && (err != nil || out.String() != "! exit=113\n"):
			t.Errorf("in process, before the end: %v\n%q", err, out.String())
		case !inProcess && (errorcodes.Of(err) != "cancelled" || out.Len() != 0):
			t.Errorf("a follow, before the end: %v (%s)\n%q", err, errorcodes.Of(err), out.String())
		}
		if got := term.Wait(t, slave); !strings.HasSuffix(got, drawn(2, 2, 0)+eraseLine) {
			t.Errorf("before the end, in process %v: the terminal was sent %q", inProcess, got)
		}
	}

	for _, inProcess := range []bool{true, false} {
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		out := &interruptAfter{n: 2, stop: make(chan struct{})}
		cfg := progressConfig(t, footer)
		r, err := newRecordRenderer(out, "text", cfg, false, false, "", "/srv/jobs/261108-130024-00", "run", false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		p := NewProgressLine(slave, DisplayLineStyle(cfg, DisplayDestination(slave)), 2)
		r.sayTo(p, cfg)
		if !r.gatherFrom(func() (*os.File, error) { return os.Open(path) }, out.stop, p) {
			t.Fatal("the renderer does not gather")
		}
		for _, rec := range recs {
			r.OnRecord(rec)
		}
		if inProcess {
			r.endGathered()
		}
		err = r.WriteFooter(time.Now(), 113, time.Second, nil)
		got := out.String()
		lead := "! show big\n! r1 (1)\n"
		cut, end := strings.CutPrefix(got, lead)
		wantEnd := "\n"
		if inProcess {
			wantEnd = "\n! exit=113\n"
		}
		if !end || !strings.HasSuffix(cut, wantEnd) || strings.Count(cut, "x") == 0 || strings.Count(cut, "x") >= 4<<20 || strings.Contains(got, "small") {
			t.Errorf("in process %v, during the read-back: %d bytes written, %q ... %q", inProcess, len(got), got[:min(len(got), 40)], got[max(0, len(got)-40):])
		}
		if inProcess != (err == nil) || (!inProcess && errorcodes.Of(err) != "cancelled") {
			t.Errorf("in process %v, during the read-back: %v", inProcess, err)
		}
		if term := term.Wait(t, slave); !strings.HasSuffix(term, eraseLine) {
			t.Errorf("in process %v: the line not erased: %q", inProcess, term)
		}
	}
}

// TestRunRendererGatherWithProgress: the follow's renderer gathering at a
// terminal draws the line over the devices it is told, takes its host-key
// lines through it, and gives it as its standard error for the follow's own
// lines (a resume's not-kept line); StopProgress erases it, as the follow
// does on every return; under --quiet there is none.
func TestRunRendererGatherWithProgress(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	path := filepath.Join("testdata", "gather", "cf-dir.jsonl")
	var out bytes.Buffer
	cfg := progressConfig(t, "display.run.gather=true")
	rr, err := NewRunRenderer(cfg, false, false, "/srv/jobs/x", "text", false, false, false, &out, slave)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rr.Gather(context.Background(), openPath(path), devicesOf(3)); !ok || err != nil {
		t.Fatal("the follow's renderer does not gather")
	}
	feed(t, rr.r, path)
	WriteWarning(rr.Stderr(), cfg, "records 1 to 2 were before this follow")
	rr.StopProgress()
	want := drawn(0, 3, 0) + drawn(1, 3, 0) + drawn(2, 3, 0) + drawn(3, 3, 0) + eraseLine + "warning: records 1 to 2 were before this follow\n" + drawn(3, 3, 0) + eraseLine
	if got := term.Wait(t, slave); got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}

	_, slave = testpty.Open(t)
	rr, err = NewRunRenderer(cfg, true, false, "/srv/jobs/x", "text", false, false, false, &out, slave)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rr.Gather(context.Background(), openPath(path), devicesOf(3)); !ok || err != nil || rr.Stderr() != slave {
		t.Errorf("under --quiet the follow's standard error is %T", rr.Stderr())
	}
}

// syncBuffer is a standard error written from more than one goroutine:
// the line's watch of the interrupt erases it from its own.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// stoppedSoon says whether p is stopped within five seconds, waiting on
// the condition, never a fixed time.
func stoppedSoon(p *ProgressLine) bool {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		p.mu.Lock()
		stopped := p.stopped
		p.mu.Unlock()
		if stopped {
			return true
		}
	}
	return false
}

// devicesOf is a follow's devices for the progress line, n.
func devicesOf(n int) func() (int, error) {
	return func() (int, error) { return n, nil }
}

// openPath is a GatherFile opening path by its name.
func openPath(path string) GatherFile {
	return func() (*os.File, error) { return os.Open(path) }
}

// TestProgressLineErasedAtTheInterrupt: in process the invocation's
// Ctrl-C cancels the job while the display waits; the line is erased when
// the interrupt comes, not at the display's end, and never drawn after
// it, so the cancelled devices' records that follow draw nothing (they
// would count every one failed). A Ctrl-C before the display starts (during
// admission) leaves the line never drawn.
func TestProgressLineErasedAtTheInterrupt(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		shellAnswer("router1", 1, 1, 1, "show version", "success", "ok\n"),
		withError(shellAnswer("router2", 2, 1, 1, "show version", "cancel", ""), "cancelled", "interrupted"),
		withError(shellAnswer("router3", 3, 1, 1, "show version", "cancel", ""), "cancelled", "interrupted"),
	})
	for _, early := range []bool{false, true} {
		var out bytes.Buffer
		stderr := &syncBuffer{}
		r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		p := &ProgressLine{out: stderr, devices: 3}
		interrupt := make(chan struct{})
		if early {
			close(interrupt)
		}
		r.gatherFrom(openPath(path), interrupt, p)
		r.OnRecord(recs[0])
		want := ""
		if !early {
			close(interrupt)
			want = drawn(0, 3, 0) + drawn(1, 3, 0) + eraseLine
		}
		if !stoppedSoon(p) {
			t.Fatalf("early %v: the line is not stopped at the interrupt; the terminal was sent %q", early, stderr.String())
		}
		r.OnRecord(recs[1])
		r.OnRecord(recs[2])
		r.endGathered()
		if got := stderr.String(); got != want {
			t.Errorf("early %v: the terminal was sent\n%q\nwant\n%q", early, got, want)
		}
	}
}

// TestProgressLineErasedAtAnError: a renderer error while the job runs
// (the in-process event sink's scoreboard_write_failed) ends the display's
// work; the line is erased then, not left drawn with its last counts for
// the rest of the job, as if hung.
func TestProgressLineErasedAtAnError(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		shellAnswer("router1", 1, 1, 1, "show version", "success", "ok\n"),
		shellAnswer("router2", 2, 1, 1, "show version", "success", "ok\n"),
		shellAnswer("router3", 3, 1, 1, "show version", "success", "ok\n"),
	})
	var out, stderr bytes.Buffer
	r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := &ProgressLine{out: &stderr, devices: 3}
	r.gatherFrom(openPath(path), nil, p)
	r.OnRecord(recs[0])
	r.SetError("scoreboard_write_failed", os.ErrPermission)
	r.OnRecord(recs[1])
	r.OnRecord(recs[2])
	if want := drawn(0, 3, 0) + drawn(1, 3, 0) + eraseLine; stderr.String() != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", stderr.String(), want)
	}
}

// unstarted is a record of a device dispatch never started, as
// EmitUnstarted writes it: its error the operation dispatch.
func unstarted(r records.CommandRecord, reason string) records.CommandRecord {
	r.Error = &records.StructuredError{Code: reason, Category: "shutdown", Message: reason, Operation: "dispatch"}
	return r
}

// TestProgressLineUnstartedDevices: a device dispatch never started (a
// halt, a wave gate, a cancel or a shutdown before its start: its records
// EmitUnstarted's, their error the operation dispatch) is neither done nor
// failed, as job list's DONE and FAIL count only the devices dispatch ran;
// one cancelled after its start is both.
func TestProgressLineUnstartedDevices(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		withError(shellAnswer("router1", 1, 1, 1, "show bad", "error", ""), "command_failed", "rejected"),
		withError(shellAnswer("router2", 2, 1, 1, "show bad", "cancel", ""), "cancelled", "interrupted"),
		unstarted(shellAnswer("router3", 3, 1, 2, "show bad", "not_started_halt", ""), "halt_error_count"),
		unstarted(shellAnswer("router3", 3, 2, 2, "show clock", "not_started_halt", ""), "halt_error_count"),
		unstarted(shellAnswer("router4", 4, 1, 2, "show bad", "cancel", ""), "cancelled"),
		unstarted(shellAnswer("router4", 4, 2, 2, "show clock", "cancel", ""), "cancelled"),
	})
	var out, stderr bytes.Buffer
	r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := &ProgressLine{out: &stderr, devices: 4}
	r.gatherFrom(openPath(path), nil, p)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	r.endGathered()
	if want := drawn(0, 4, 0) + drawn(1, 4, 1) + drawn(2, 4, 2) + eraseLine; stderr.String() != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", stderr.String(), want)
	}
}

// TestGatheredInterruptedFindingTheLines: the interrupt is looked at
// during the read-back's one pass over commands.jsonl to find the groups'
// lines (output.FindLines), not only before it and between groups. It
// comes as the read-back opens the file, which here holds filler and not
// the group's line: the pass, run to its end, would find the line missing
// (run_output_records_unreadable); stopped at its first read it is the
// interrupt, the display cancelled, nothing shown.
func TestGatheredInterruptedFindingTheLines(t *testing.T) {
	_, recs := writeJob(t, []records.CommandRecord{shellAnswer("router1", 1, 1, 1, "show version", "success", "ok\n")})
	recs[0].Sequence = 1025 // the group's line, past the filler's 1024
	filler := filepath.Join(t.TempDir(), "commands.jsonl")
	if err := os.WriteFile(filler, bytes.Repeat([]byte(strings.Repeat("x", 1023)+"\n"), 1024), 0o640); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	interrupt := make(chan struct{})
	r.gatherFrom(func() (*os.File, error) {
		close(interrupt) // the Ctrl-C, as the read-back starts
		return os.Open(filler)
	}, interrupt, nil)
	r.OnRecord(recs[0])
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); errorcodes.Of(err) != "cancelled" || out.Len() != 0 {
		t.Errorf("the display ended %v (%s), shown %q", err, errorcodes.Of(err), out.String())
	}
}

// TestProgressLinePartLine: a write that does not end a line (a prompt)
// leaves the line off until a write ends one: a device ending meanwhile
// is not drawn over the part line, and the line ending draws it again with
// the latest counts. An empty write writes nothing and leaves the line as
// it is.
func TestProgressLinePartLine(t *testing.T) {
	var stderr bytes.Buffer
	p := &ProgressLine{out: &stderr, devices: 2}
	p.show(0, 0)
	if n, err := p.Write(nil); n != 0 || err != nil || stderr.String() != drawn(0, 2, 0) {
		t.Fatalf("an empty write: %d, %v; the terminal was sent %q", n, err, stderr.String())
	}
	fmt.Fprint(p, "Password for router1: ")
	p.show(1, 0)
	fmt.Fprint(p, "\n")
	p.show(1, 0)
	p.Stop()
	if want := drawn(0, 2, 0) + eraseLine + "Password for router1: " + "\n" + drawn(1, 2, 0) + eraseLine; stderr.String() != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", stderr.String(), want)
	}
}

// TestProgressLineNarrowTerminal: the line is drawn only where it fits,
// the terminal wider than its text, one column spare: on a narrower one
// it would wrap and leave a row behind at every redraw, and at exactly
// its width the clear to the line's end erases its last character on
// xterm-class terminals (a cut count). Nothing is drawn there, the counts
// kept, a line written to standard error passes through without the line
// after it; once the terminal is wide enough the next record draws it,
// even one that ends no device.
func TestProgressLineNarrowTerminal(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	text := len("gather: done=0/2 fail=0")
	testpty.SetSize(t, slave, 24, uint16(text-1))
	p := NewProgressLine(slave, display.LineStyle{}, 2)
	p.show(0, 0)
	fmt.Fprint(p, "warning: one\n")
	testpty.SetSize(t, slave, 24, uint16(text))
	p.show(0, 0)
	fmt.Fprint(p, "warning: two\n")
	testpty.SetSize(t, slave, 24, uint16(text+1))
	p.show(0, 0)
	p.show(1, 0)
	p.Stop()
	if got, want := term.Wait(t, slave), "warning: one\nwarning: two\n"+drawn(0, 2, 0)+drawn(1, 2, 0)+eraseLine; got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
}

// TestProgressLineInTheBackground: the line is drawn only while karvi's
// process group is the terminal's foreground group (osutil.ForegroundGroup;
// a terminal that cannot say, as this test's, is drawn on). In the
// background nothing is drawn or erased (an erase would clear the shell's
// line, a draw stop the job under tostop): the counts are kept, a line
// written to standard error follows whatever is on the screen; back in
// the foreground the next record draws it, even one that ends no device.
func TestProgressLineInTheBackground(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	p := NewProgressLine(slave, display.LineStyle{}, 2)
	if !p.inForeground() {
		t.Fatal("a terminal that cannot say is not drawn on")
	}
	foreground := false
	p.foreground = func() bool { return foreground }
	p.show(0, 0)
	fmt.Fprint(p, "warning: one\n")
	foreground = true
	p.show(0, 0)
	foreground = false
	fmt.Fprint(p, "warning: two\n")
	p.show(1, 0)
	foreground = true
	p.show(1, 0)
	foreground = false
	p.Stop()
	if got, want := term.Wait(t, slave), "warning: one\n"+drawn(0, 2, 0)+"warning: two\n"+drawn(1, 2, 0); got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
}

// TestProgressLineErasedBeforeTheBlocks: standard output and standard
// error the same terminal, as at an operator's shell: the line's last draw
// is erased before the first block's marker, so the blocks start on a
// clean line and nothing is written after a half line; the display's own
// end erases it (the test does not stop the line), in process where every
// record is in (endGathered) and on a follow path at the footer.
func TestProgressLineErasedBeforeTheBlocks(t *testing.T) {
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	gathered, err := os.ReadFile(filepath.Join("testdata", "gather", "show-version.gathered"))
	if err != nil {
		t.Fatal(err)
	}
	for _, inProcess := range []bool{true, false} {
		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		r, _ := progressRenderer(t, slave, path, slave, 8, nil)
		feed(t, r, path)
		if inProcess {
			r.endGathered()
		}
		if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
			t.Fatal(err)
		}
		if got, want := term.Wait(t, slave), labProgress()+eraseLine+string(gathered); got != want {
			t.Errorf("in process %v: the terminal was sent\n%q\nwant\n%q", inProcess, got, want)
		}
	}
}

// TestGatheredInterruptedBetweenGroups: the interrupt comes as the first
// group's answer is written; the display stops before the next group,
// though the groups after it are failures with no answer to stream, which
// no check inside a stream would stop. What was written stands; in
// process the footer follows (the job's end, its exit 113), on a follow
// path the display ends there, cancelled, no footer.
func TestGatheredInterruptedBetweenGroups(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		withError(shellAnswer("router1", 1, 1, 1, "show version", "connection_error", ""), "native_session_open_failed", "refused"),
		withError(shellAnswer("router2", 2, 1, 1, "show version", "connection_error", ""), "command_timeout", "timed out"),
		shellAnswer("router3", 3, 1, 1, "show version", "success", "ok\n"),
	})
	for _, inProcess := range []bool{true, false} {
		// The second write is the first group's answer, its one piece.
		out := &interruptAfter{n: 2, stop: make(chan struct{})}
		r, err := newRecordRenderer(out, "text", progressConfig(t, "display.run.footer=! exit=<exit-code>"), false, false, "", "/srv/jobs/x", "run", false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		r.gatherFrom(openPath(path), out.stop, nil)
		for _, rec := range recs {
			r.OnRecord(rec)
		}
		if inProcess {
			r.endGathered()
		}
		err = r.WriteFooter(time.Now(), 113, time.Second, nil)
		want := "! show version\n! router3 (1)\nok\n"
		if inProcess {
			want += "! exit=113\n"
		}
		if out.String() != want {
			t.Errorf("in process %v: written\n%q\nwant\n%q", inProcess, out.String(), want)
		}
		if inProcess != (err == nil) || (!inProcess && errorcodes.Of(err) != "cancelled") {
			t.Errorf("in process %v: the display ended %v (%s)", inProcess, err, errorcodes.Of(err))
		}
	}
}

// TestProgressLineFailedByAnyRecord: with two-command devices whose
// records interleave, a device is done when its last requested record
// comes, not its first, and failed when any of its requested records did
// not succeed, its first (router1) or its last (router2), not only one of
// them; one whose records all succeed (router3) is done and not failed.
// The line is drawn as each device ends and only then.
func TestProgressLineFailedByAnyRecord(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		withError(shellAnswer("router1", 1, 1, 2, "show version", "device_error", "% bad\n"), "device_error", "rejected"),
		shellAnswer("router2", 2, 1, 2, "show version", "success", "ok\n"),
		shellAnswer("router1", 1, 2, 2, "show clock", "success", "ok\n"),
		withError(shellAnswer("router2", 2, 2, 2, "show clock", "device_error", "% bad\n"), "device_error", "rejected"),
		shellAnswer("router3", 3, 1, 2, "show version", "success", "ok\n"),
		shellAnswer("router3", 3, 2, 2, "show clock", "success", "ok\n"),
	})
	var out, stderr bytes.Buffer
	r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := &ProgressLine{out: &stderr, devices: 3}
	r.gatherFrom(openPath(path), nil, p)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	r.endGathered()
	if want := drawn(0, 3, 0) + drawn(1, 3, 1) + drawn(2, 3, 2) + drawn(3, 3, 2) + eraseLine; stderr.String() != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", stderr.String(), want)
	}
}

// TestRunRendererHostKeyLinesThroughTheLine: the follow's renderer
// gathering at a terminal takes its records' host-key lines through the
// line, as the in-process run does: each written on a line of its own,
// the line erased first and drawn again after, never written after the
// drawn line or drawn over; the follow's end erases it.
func TestRunRendererHostKeyLinesThroughTheLine(t *testing.T) {
	master, slave := testpty.Open(t)
	term := testpty.Record(master)
	path := filepath.Join("testdata", "gather", "show-version.jsonl")
	var out bytes.Buffer
	rr, err := NewRunRenderer(progressConfig(t, "display.run.gather=true"), false, false, "/srv/jobs/x", "text", false, false, false, &out, slave)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := rr.Gather(context.Background(), openPath(path), devicesOf(8)); !ok || err != nil {
		t.Fatal("the follow's renderer does not gather")
	}
	feed(t, rr.r, path)
	if err := rr.Finish(&records.Summary{}); err != nil {
		t.Fatal(err)
	}
	if got, want := term.Wait(t, slave), labProgress()+eraseLine; got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
}

// TestRunRendererProgressLineStyle: the follow's renderer draws its line
// in standard error's own style, styled like the footer: under
// display.color=always at a terminal its words in the label colour and
// its counts in the value colour, while standard output, a regular file
// (`> out.txt`), is written plain, as the colour policy writes a file;
// under display.color=never the line is plain.
func TestRunRendererProgressLineStyle(t *testing.T) {
	path := filepath.Join("testdata", "gather", "cf-dir.jsonl")
	gathered, err := os.ReadFile(filepath.Join("testdata", "gather", "cf-dir.gathered"))
	if err != nil {
		t.Fatal(err)
	}
	for _, color := range []string{"always", "never"} {
		cfg := progressConfig(t, "display.run.gather=true", "display.color="+color)
		style := DisplayLineStyle(cfg, display.Terminal)
		if style.Enabled != (color == "always") || style.Label == style.Value {
			t.Fatalf("display.color=%s: colour %v, label %q, value %q", color, style.Enabled, style.Label, style.Value)
		}
		label := func(s string) string { return display.ANSIStyle(s, style.Label, style.Enabled, false) }
		value := func(s string) string { return display.ANSIStyle(s, style.Value, style.Enabled, false) }
		want := ""
		for done := 0; done <= 3; done++ {
			want += redrawLine + label("gather: done=") + value(fmt.Sprintf("%d/3", done)) + label(" fail=") + value("0") + clearRest
		}
		want += eraseLine

		master, slave := testpty.Open(t)
		term := testpty.Record(master)
		out, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
		if err != nil {
			t.Fatal(err)
		}
		rr, err := NewRunRenderer(cfg, false, false, "/srv/jobs/x", "text", false, false, false, out, slave)
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := rr.Gather(context.Background(), openPath(path), devicesOf(3)); !ok || err != nil {
			t.Fatal("the follow's renderer does not gather")
		}
		feed(t, rr.r, path)
		if err := rr.Finish(&records.Summary{}); err != nil {
			t.Fatal(err)
		}
		if got := term.Wait(t, slave); got != want {
			t.Errorf("display.color=%s: the terminal was sent\n%q\nwant\n%q", color, got, want)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		if blocks, err := os.ReadFile(out.Name()); err != nil || string(blocks) != string(gathered) {
			t.Errorf("display.color=%s: the blocks in the file (%v):\n%q\nwant:\n%q", color, err, blocks, gathered)
		}
	}
}

// TestGatheredInterruptedWithNothingGathered: on a follow path a display
// that gathered nothing (the interrupt came before any record) is stopped
// by the interrupt too: cancelled, no footer, never the job's exit as if
// the Ctrl-C had not come.
func TestGatheredInterruptedWithNothingGathered(t *testing.T) {
	path, _ := writeJob(t, []records.CommandRecord{shellAnswer("router1", 1, 1, 1, "show version", "success", "ok\n")})
	var out bytes.Buffer
	r, err := newRecordRenderer(&out, "text", progressConfig(t, "display.run.footer=! exit=<exit-code>"), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	r.gatherFrom(openPath(path), stop, nil)
	close(stop)
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); errorcodes.Of(err) != "cancelled" || out.Len() != 0 {
		t.Errorf("the display ended %v (%s), shown %q", err, errorcodes.Of(err), out.String())
	}
}

// TestGatheredInterruptedBeforeAPiece: an interrupt that comes between a
// streamed answer's pieces (here after the group's header, before the
// answer's first piece) stops the display before the next piece is
// written, not after it: none of the answer is shown.
func TestGatheredInterruptedBeforeAPiece(t *testing.T) {
	path, recs := bigAnswers(t)
	out := &interruptAfter{n: 1, stop: make(chan struct{})}
	r, err := newRecordRenderer(out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	r.gatherFrom(openPath(path), out.stop, nil)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); errorcodes.Of(err) != "cancelled" || out.String() != "! show big\n! r1 (1)\n" {
		t.Errorf("the display ended %v; shown %d bytes, %d of the answer", err, out.Len(), strings.Count(out.String(), "x"))
	}
}

// TestProgressLineNotDrawnAfterTheInterrupt: a record or a line on
// standard error that comes just after the invocation's Ctrl-C, before the
// watch of the interrupt has stopped the line, does not draw it again: the
// line is erased at once and stays so (in process the cancelled devices'
// records come straight after the Ctrl-C, each counted failed).
func TestProgressLineNotDrawnAfterTheInterrupt(t *testing.T) {
	path, recs := writeJob(t, []records.CommandRecord{
		shellAnswer("router1", 1, 1, 1, "show version", "success", "ok\n"),
		withError(shellAnswer("router2", 2, 1, 1, "show version", "cancel", ""), "cancelled", "interrupted"),
		withError(shellAnswer("router3", 3, 1, 1, "show version", "cancel", ""), "cancelled", "interrupted"),
	})
	var out bytes.Buffer
	stderr := &syncBuffer{}
	r, err := newRecordRenderer(&out, "text", progressConfig(t), false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	p := &ProgressLine{out: stderr, devices: 3}
	interrupt := make(chan struct{})
	r.gatherFrom(openPath(path), interrupt, p)
	r.OnRecord(recs[0])
	close(interrupt)
	r.OnRecord(recs[1])
	fmt.Fprint(p, "warning: one\n")
	r.OnRecord(recs[2])
	if !stoppedSoon(p) {
		t.Fatal("the line is not stopped")
	}
	r.endGathered()
	if got, want := stderr.String(), drawn(0, 3, 0)+drawn(1, 3, 0)+eraseLine+"warning: one\n"; got != want {
		t.Errorf("the terminal was sent\n%q\nwant\n%q", got, want)
	}
}

// TestProgressLineInterruptLookedAtBeforeEachDraw: each draw looks at the
// invocation's interrupt itself, whichever comes first after it: a line
// written to standard error erases the line and is written through, not
// followed by a redraw; a device ending erases it and draws nothing. The
// line here has no watch of its own, so neither order depends on when the
// watch runs.
func TestProgressLineInterruptLookedAtBeforeEachDraw(t *testing.T) {
	for _, writeFirst := range []bool{true, false} {
		var stderr bytes.Buffer
		interrupt := make(chan struct{})
		p := &ProgressLine{out: &stderr, devices: 2, interrupt: interrupt}
		p.show(0, 0)
		close(interrupt)
		if writeFirst {
			fmt.Fprint(p, "warning: one\n")
			p.show(1, 0)
		} else {
			p.show(1, 0)
			fmt.Fprint(p, "warning: one\n")
		}
		if want := drawn(0, 2, 0) + eraseLine + "warning: one\n"; stderr.String() != want || !p.stopped {
			t.Errorf("write first %v: the terminal was sent\n%q\nwant\n%q", writeFirst, stderr.String(), want)
		}
	}
}
