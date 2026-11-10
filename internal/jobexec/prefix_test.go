package jobexec

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// Line mode (display.run.prefix, display.command.prefix; DECISIONS point
// 6): every line of a record "NAME: line", no header, no border.

const lineFooter = "display.run.footer=! exit=<exit-code>"

// lineRenderer is an activity's text renderer, colour off unless sets
// say otherwise, not gathering.
func lineRenderer(t *testing.T, out *bytes.Buffer, activity string, quiet, echo bool, sets ...string) *recordRenderer {
	t.Helper()
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: append([]string{"display.color=never"}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRecordRenderer(out, "text", cfg, quiet, false, "", "/srv/jobs/261108-130024-00", activity, echo, false, false)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// streamed renders recs as the job's stream carries them, not gathered,
// and ends the display.
func streamed(t *testing.T, activity string, quiet, echo bool, recs []records.CommandRecord, sets ...string) string {
	t.Helper()
	var out bytes.Buffer
	r := lineRenderer(t, &out, activity, quiet, echo, sets...)
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, time.Second, nil); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// jobRecords is the records of a lab job's commands.jsonl.
func jobRecords(t *testing.T, path string) []records.CommandRecord {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var recs []records.CommandRecord
	for _, line := range bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
		var rec records.CommandRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, rec)
	}
	return recs
}

// TestLineModeOfTheLabJobs: the lab jobs' records (testdata/gather) under
// display.run.prefix give point 6's display, streamed and gathered:
// *.prefix and *.gathered-prefix, made from today's displays by point 6's
// rules (S5/golden/linemode.py in the release evidence), the same as the
// agreed prototype's point6/prefix.out and gather-prefix.out. A border and
// a last border set draw nothing.
func TestLineModeOfTheLabJobs(t *testing.T) {
	for _, job := range []string{"show-version", "two-commands", "cf-dir", "device-error"} {
		path := filepath.Join("testdata", "gather", job+".jsonl")
		for _, sets := range [][]string{
			{"display.run.prefix=true", lineFooter},
			{"display.run.prefix=true", lineFooter, "display.run.border=<repeat:-:8>", "display.run.last-border=true"},
		} {
			want, err := os.ReadFile(filepath.Join("testdata", "gather", job+".prefix"))
			if err != nil {
				t.Fatal(err)
			}
			if got := streamed(t, "run", false, false, jobRecords(t, path), sets...); got != string(want) {
				t.Errorf("%s streamed %v:\n%s\nwant:\n%s", job, sets[2:], got, want)
			}
			want, err = os.ReadFile(filepath.Join("testdata", "gather", job+".gathered-prefix"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := gathered(t, path, false, sets...)
			if err != nil {
				t.Fatalf("%s: %v", job, err)
			}
			if got != string(want) {
				t.Errorf("%s gathered %v:\n%s\nwant:\n%s", job, sets[2:], got, want)
			}
		}
	}
}

// TestLineModeKeys: a command's line mode is display.command.prefix and a
// run's display.run.prefix, each in its activity's family of display keys
// (DECISIONS point 1, settled at S3c); neither key shapes the other's
// display, and false (the default) leaves today's display.
func TestLineModeKeys(t *testing.T) {
	recs := []records.CommandRecord{shellAnswer("r1", 1, 1, 1, "show clock", "success", "10:00\n")}
	header := []string{"display.run.header=! <target>", "display.command.header=! <target>", lineFooter, "display.command.footer=! exit=<exit-code>"}
	for _, tc := range []struct {
		activity, key string
		want          string
	}{
		{"run", "display.run.prefix=true", "r1: 10:00\n! exit=0\n"},
		{"run", "display.command.prefix=true", "! r1\n10:00\n! exit=0\n"},
		{"run", "display.run.prefix=false", "! r1\n10:00\n! exit=0\n"},
		{"command", "display.command.prefix=true", "r1: 10:00\n! exit=0\n"},
		{"command", "display.run.prefix=true", "! r1\n10:00\n! exit=0\n"},
		{"command", "display.command.prefix=false", "! r1\n10:00\n! exit=0\n"},
	} {
		if got := streamed(t, tc.activity, false, false, recs, append(header, tc.key)...); got != tc.want {
			t.Errorf("%s %s:\n%q\nwant\n%q", tc.activity, tc.key, got, tc.want)
		}
	}
}

// TestLineModeLines: every line of a record is prefixed, an empty one
// "NAME: " (as pdsh and clush write it), a last line without its break
// ended; the --echo line; an exec record's stdout then its stderr, each
// line prefixed, no mark on stderr; the follow's notice in the output's
// place; a failure line and a not-attempted line without the target= the
// prefix repeats, the not-attempted keeping command=i/n; a session-init
// failure's; a message of several lines each line prefixed. --quiet keeps
// every one of these and drops the footer.
func TestLineModeLines(t *testing.T) {
	echoed := shellAnswer("r1", 1, 1, 3, "show clock", "success", "a\n\nb")
	echoed.PromptBefore, echoed.PromptSource = "r1#", "observed"
	omitted := shellAnswer("r1", 1, 2, 3, "show big", "success", "")
	omitted.OutputBytes, omitted.OutputSHA256 = 73000, "abc"
	omitted.PromptBefore, omitted.PromptSource = "r1#", "observed"
	omitted.Notices = []records.Notice{{Code: "follow_output_omitted", Message: "output of 73000 bytes left out of the follow stream"}}
	recs := []records.CommandRecord{
		echoed,
		omitted,
		withError(shellAnswer("r1", 1, 3, 3, "show bogus", "device_error", "% Invalid input\n"), "device_command_error", "device reported a command error"),
		execAnswer("edge1", 2, 1, 3, "ls /srv", "d", "e\nf\n", 1),
		withError(shellAnswer("edge1", 2, 2, 3, "ls /var", "timeout", ""), "command_timeout", "timed out after 30s\nthe second line"),
		shellAnswer("edge1", 2, 3, 3, "ls /tmp", "not_attempted_prior_command_failure", ""),
		withError(sessionInit("edge2", 3, 1, 1, "iosxe", "terminal length 0", "timeout"), "command_timeout", "session-init command timed out after 30s"),
		shellAnswer("edge2", 3, 1, 1, "show version", "not_attempted_session_init_failure", ""),
	}
	// The empty line is "r1: ", its space kept.
	body := "r1: r1#show clock\nr1: a\nr1: \nr1: b\n" + `r1: r1#show big
r1: karvi: output of 73000 bytes left out of the follow stream
r1: r1#show bogus
r1: % Invalid input
r1: karvi: status=device_error error=device_command_error: device reported a command error
edge1: edge1# ls /srv
edge1: d
edge1: e
edge1: f
edge1: karvi: status=timeout error=command_timeout: timed out after 30s
edge1: the second line
edge1: karvi: status=not_attempted_prior_command_failure command=3/3
edge2: karvi: session_init=iosxe command=1/1 status=timeout error=command_timeout: session-init command timed out after 30s
edge2: karvi: status=not_attempted_session_init_failure command=1/1
`
	if got := streamed(t, "run", false, true, recs, "display.run.prefix=true", lineFooter); got != body+"! exit=0\n" {
		t.Errorf("got:\n%s\nwant:\n%s! exit=0\n", got, body)
	}
	if got := streamed(t, "run", true, true, recs, "display.run.prefix=true", lineFooter); got != body {
		t.Errorf("quiet:\n%s\nwant:\n%s", got, body)
	}
}

// TestLineModeStreamsTheAnswer: a spooled answer streamed in pieces is
// prefixed line by line, the same bytes as the answer as a string, with
// and without the escape-sequence stripper; the stripper takes the
// device's sequences, never the prefix's colour.
func TestLineModeStreamsTheAnswer(t *testing.T) {
	raws := map[string][]byte{
		"lines":   []byte(strings.Repeat("line of output é€😀\n", 5000)),
		"no-end":  []byte(strings.Repeat("x", 200000) + "\n" + strings.Repeat("y", 70000)),
		"escapes": []byte(strings.Repeat("\x1b[32mgreen\x1b[0m plain\n", 3000) + "\x1b[1mbold at the end"),
	}
	for _, ansi := range []string{"strip", "preserve"} {
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`output.ansi="` + ansi + `"`, "display.color=always", "display.command.prefix=true", "display.command.footer="}})
		if err != nil {
			t.Fatal(err)
		}
		style := DisplayLineStyle(cfg, DisplayDestination(&bytes.Buffer{}))
		prefix := display.ANSIStyle("r1", style.Target, true, true) + display.ANSIStyle(": ", style.Label, true, false)
		for name, raw := range raws {
			fromString, fromSpool, src := spooledRecord(t, raw)
			render := func(rec records.CommandRecord, src output.Source) string {
				var out bytes.Buffer
				r, err := newRecordRenderer(&out, "text", cfg, false, false, "activity-1", "/tmp/artifacts", "command", false, false, false)
				if err != nil {
					t.Fatal(err)
				}
				r.OnRecordFrom(rec, src)
				if err := r.WriteFooter(time.Time{}, 0, 0, nil); err != nil || r.Error() != nil {
					t.Fatalf("%s %s: %v %v", ansi, name, err, r.Error())
				}
				return out.String()
			}
			want := render(fromString, output.FromRecord(&fromString))
			if got := render(fromSpool, src); got != want {
				t.Fatalf("ansi=%s %s: the spooled answer renders differently (%d and %d bytes)", ansi, name, len(got), len(want))
			}
			text := string(raw)
			if ansi == "strip" {
				text = ansiRE.ReplaceAllString(text, "")
			}
			text = strings.TrimSuffix(text, "\n")
			expect := prefix + strings.ReplaceAll(text, "\n", "\n"+prefix) + "\n"
			if want != expect {
				t.Errorf("ansi=%s %s: not each line prefixed (%d bytes, want %d)\n%q", ansi, name, len(want), len(expect), want[:min(len(want), 200)])
			}
		}
	}
}

// TestLineModeColours: the name in the target colour, ": " in the label
// colour, a failure line in the error colour after them, the device's
// output and the echo uncoloured; gathered, the folded names the same and
// the marker as today's; nothing coloured into a file.
func TestLineModeColours(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always", "display.theme=dark"}})
	if err != nil {
		t.Fatal(err)
	}
	style := DisplayLineStyle(cfg, DisplayDestination(&bytes.Buffer{}))
	name := func(n string) string {
		return display.ANSIStyle(n, style.Target, true, true) + display.ANSIStyle(": ", style.Label, true, false)
	}
	failure := func(text string) string { return display.ANSIStyle(text, style.Error, true, true) }
	ok := shellAnswer("r1", 1, 1, 1, "show clock", "success", "10:00\n")
	ok.PromptBefore, ok.PromptSource = "r1#", "observed"
	recs := []records.CommandRecord{ok, withError(shellAnswer("r2", 2, 1, 1, "show clock", "timeout", ""), "command_timeout", "timed out")}
	got := streamed(t, "run", false, true, recs, "display.color=always", "display.theme=dark", "display.run.prefix=true", "display.run.footer=")
	want := name("r1") + "r1#show clock\n" + name("r1") + "10:00\n" + name("r2") + failure("karvi: status=timeout error=command_timeout: timed out") + "\n"
	if got != want {
		t.Errorf("streamed:\n%q\nwant\n%q", got, want)
	}

	path, recs := writeJob(t, []records.CommandRecord{
		shellAnswer("r1", 1, 1, 1, "show clock", "success", "10:00\n"),
		shellAnswer("r2", 2, 1, 1, "show clock", "success", "10:00\n"),
		withError(shellAnswer("r3", 3, 1, 1, "show clock", "timeout", ""), "command_timeout", "timed out"),
	})
	var out bytes.Buffer
	r := gatheringRenderer(t, &out, path, false, "display.color=always", "display.theme=dark", "display.run.prefix=true")
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
		t.Fatal(err)
	}
	want = r.markerLine(blockKey{command: "show clock", occurrence: 1}) + "\n" + name("r[1-2]") + "10:00\n" + name("r3") + failure("karvi: status=timeout error=command_timeout: timed out") + "\n"
	if out.String() != want {
		t.Errorf("gathered:\n%q\nwant\n%q", out.String(), want)
	}

	if got := streamed(t, "run", false, true, recs[:1], "display.color=auto", "display.run.prefix=true", "display.run.footer="); got != "r1: 10:00\n" {
		t.Errorf("into a file: %q", got)
	}
}

// TestLineModeGatheredQuiet: gathered under --quiet the markers go as the
// gathered display's do, every answer and failure line prefixed.
func TestLineModeGatheredQuiet(t *testing.T) {
	got, err := gathered(t, filepath.Join("testdata", "gather", "show-version.jsonl"), true, "display.run.prefix=true", lineFooter)
	want := "edge[1-2],router[01-04,10]: Cisco IOS XE Software, Version 17.09.04a\nedge[1-2],router[01-04,10]: fake-iosxe uptime is 1 day\nrouter09: karvi: status=connection_error error=native_session_open_failed: dial tcp 127.0.0.9:38537: connect: connection refused\n"
	if err != nil || got != want {
		t.Errorf("%v\n%s\nwant\n%s", err, got, want)
	}
}

// TestLineModeLeavesOtherDisplays: the key set true shapes the text
// display alone: jsonl and json are as without it, and the live view
// (--live, whose session bytes are the display) is unchanged.
func TestLineModeLeavesOtherDisplays(t *testing.T) {
	recs := jobRecords(t, filepath.Join("testdata", "gather", "show-version.jsonl"))
	for _, format := range []string{"jsonl", "json"} {
		render := func(sets ...string) string {
			cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			r, err := newRecordRenderer(&out, format, cfg, false, false, "", "/srv/jobs/x", "run", false, false, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, rec := range recs {
				r.OnRecord(rec)
			}
			if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
				t.Fatal(err)
			}
			return out.String()
		}
		if render("display.run.prefix=true") != render() {
			t.Errorf("%s: the key changed the format", format)
		}
	}
	live := func(sets ...string) string {
		r, out := liveRenderer(t, "run", false, sets...)
		v := r.LiveDevice(liveRecord("r1"))
		v.Write([]byte("Router#show clock\r\n*10:00\r\nRouter#"))
		r.OnRecord(liveCommand("r1", "show clock", "success", "*10:00\n", nil))
		v.Write([]byte("show bogus\r\n% Invalid input\r\nRouter#"))
		r.OnRecord(liveCommand("r1", "show bogus", "device_error", "% Invalid input\n", &records.StructuredError{Code: "device_command_error", Message: "the device reported an error"}))
		v.End()
		if err := r.WriteFooter(time.Now(), 0, 0, nil); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if a, b := live("display.run.prefix=true"), live(); a != b || !strings.Contains(b, "karvi: target=r1 status=device_error") {
		t.Errorf("live: the key changed the view\n%q\nwant\n%q", a, b)
	}
}

// TestLineModeInterruptedReadBack: gathered, the invocation's interrupt
// between an answer's pieces leaves what was written, the cut line ended
// with its break, as the gathered display does.
func TestLineModeInterruptedReadBack(t *testing.T) {
	path, recs := bigAnswers(t)
	out := &interruptAfter{n: 3, stop: make(chan struct{})}
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=never", "display.run.prefix=true", lineFooter}})
	if err != nil {
		t.Fatal(err)
	}
	r, err := newRecordRenderer(out, "text", cfg, false, false, "", "/srv/jobs/x", "run", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if !r.gatherFrom(func() (*os.File, error) { return os.Open(path) }, out.stop, nil) {
		t.Fatal("the renderer does not gather")
	}
	for _, rec := range recs {
		r.OnRecord(rec)
	}
	r.endGathered()
	got := out.String()
	if !strings.HasPrefix(got, "! show big\nr1: x") || !strings.HasSuffix(got, "x\n") || strings.Contains(got, "small") || len(got) >= 4<<20 {
		t.Errorf("interrupted: %d bytes, %q ... %q", len(got), got[:min(len(got), 40)], got[max(0, len(got)-40):])
	}
}

// TestLineModeLeavesOutThePingLines: line mode leaves out the ICMP gate's
// line and its --debug lines with the header, as the gathered display
// does (settled at S5): a skipped device shows by its failure line.
func TestLineModeLeavesOutThePingLines(t *testing.T) {
	a := pingRecord(records.PingDecisionProceedDegraded, []records.PingOutcome{replyOut, timeoutOut}, []records.Notice{lossNotice}, 1)
	a.CommandCount = 1
	b := pingRecord(records.PingDecisionSkip, []records.PingOutcome{{Sequence: 1, Status: records.PingTimeout}, errorOut}, nil, 1)
	b.Device.CanonicalName, b.InputTarget, b.CommandCount, b.Status, b.Output = "core-nyc-02", "core-nyc-02", 1, "icmp_unreachable", ""
	b.Error = &records.StructuredError{Code: "icmp_unreachable", Message: "no validated reply"}
	want := "core-nyc-01: ok\ncore-nyc-02: karvi: status=icmp_unreachable error=icmp_unreachable: no validated reply\n"
	for _, debug := range []bool{false, true} {
		if got := renderAll(t, []string{"display.color=never", "display.run.prefix=true"}, false, debug, a, b); got != want {
			t.Errorf("debug %v:\n%s\nwant:\n%s", debug, got, want)
		}
	}
	// Without line mode the line is today's.
	if got := renderAll(t, []string{"display.color=never"}, false, false, a); !strings.HasPrefix(got, "! core-nyc-01 [2001:db8:10::1] ping(1) 2.1ms, ping(2) timeout, proceed\n") {
		t.Errorf("without line mode:\n%s", got)
	}
}

// TestLineModeColoursOfKarvisLines (from S5's mutants M18, M26): in colour
// a not-attempted line is an error line after the name, as a failure line
// is, and the follow's notice is plain after it, as today; a message
// ending in a line break is one line.
func TestLineModeColoursOfKarvisLines(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always", "display.theme=dark"}})
	if err != nil {
		t.Fatal(err)
	}
	style := DisplayLineStyle(cfg, DisplayDestination(&bytes.Buffer{}))
	name := display.ANSIStyle("r1", style.Target, true, true) + display.ANSIStyle(": ", style.Label, true, false)
	omitted := shellAnswer("r1", 1, 1, 3, "show big", "success", "")
	omitted.OutputBytes, omitted.OutputSHA256 = 73000, "abc"
	omitted.Notices = []records.Notice{{Code: "follow_output_omitted", Message: "output of 73000 bytes left out of the follow stream"}}
	recs := []records.CommandRecord{
		omitted,
		withError(shellAnswer("r1", 1, 2, 3, "show x", "timeout", ""), "command_timeout", "timed out\n"),
		shellAnswer("r1", 1, 3, 3, "show y", "not_attempted_prior_command_failure", ""),
	}
	got := streamed(t, "run", false, false, recs, "display.color=always", "display.theme=dark", "display.run.prefix=true", "display.run.footer=")
	want := name + "karvi: output of 73000 bytes left out of the follow stream\n" +
		name + display.ANSIStyle("karvi: status=timeout error=command_timeout: timed out", style.Error, true, true) + "\n" +
		name + display.ANSIStyle("karvi: status=not_attempted_prior_command_failure command=3/3", style.Error, true, true) + "\n"
	if got != want {
		t.Errorf("%q\nwant\n%q", got, want)
	}
}

// TestGatheredFailureLineColour (from S5's mutant M07): outside line mode
// a group's failure line is in the error colour, bold, as before line
// mode's lines took the same path.
func TestGatheredFailureLineColour(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.color=always", "display.theme=dark"}})
	if err != nil {
		t.Fatal(err)
	}
	style := DisplayLineStyle(cfg, DisplayDestination(&bytes.Buffer{}))
	got, err := gatherRecords(t, []records.CommandRecord{
		shellAnswer("r1", 1, 1, 1, "show clock", "success", "10:00\n"),
		withError(shellAnswer("r3", 2, 1, 1, "show clock", "timeout", ""), "command_timeout", "timed out"),
	}, "display.color=always", "display.theme=dark")
	want := display.ANSIStyle("karvi: status=timeout error=command_timeout: timed out", style.Error, true, true) + "\n"
	if err != nil || !strings.HasSuffix(got, want) {
		t.Errorf("%v\n%q\nlacks %q", err, got, want)
	}
}

// widestWriter records the largest write it is given.
type widestWriter struct {
	bytes.Buffer
	widest int
}

func (w *widestWriter) Write(p []byte) (int, error) {
	w.widest = max(w.widest, len(p))
	return w.Buffer.Write(p)
}

// refusesFirst refuses its first write and counts what it is given after.
type refusesFirst struct{ calls, after int }

func (w *refusesFirst) Write(p []byte) (int, error) {
	if w.calls++; w.calls == 1 {
		return 0, errors.New("refused")
	}
	w.after += len(p)
	return len(p), nil
}

// TestLinePrefixerBoundAndError (from S5's mutants M12 to M14): a long
// line in one write reaches the writer in pieces of at most prefixChunk
// and one prefix; a write the writer refuses, the last piece or one
// before it, is the prefixer's error, nothing written after it.
func TestLinePrefixerBoundAndError(t *testing.T) {
	w := &widestWriter{}
	in := strings.Repeat("x", 200000) + "\n"
	if n, err := (&linePrefixer{w: w, prefix: []byte("r1: ")}).Write([]byte(in)); n != len(in) || err != nil {
		t.Fatal(n, err)
	}
	if w.String() != "r1: "+in || w.widest > prefixChunk+len("r1: ") {
		t.Errorf("%d bytes, the largest write %d", w.Len(), w.widest)
	}
	for _, in := range []string{"a\n", strings.Repeat("y\n", 100000)} {
		w := &refusesFirst{}
		if n, err := (&linePrefixer{w: w, prefix: []byte("r1: ")}).Write([]byte(in)); err == nil || n != 0 || w.after != 0 {
			t.Errorf("%d bytes in: n=%d err=%v, %d written after the error", len(in), n, err, w.after)
		}
	}
}
