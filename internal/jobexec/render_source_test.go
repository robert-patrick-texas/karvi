package jobexec

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// spooledRecord is a record whose output is in a spool file, and the same
// record carrying the output as its string, for the comparison.
func spooledRecord(t *testing.T, raw []byte) (fromString records.CommandRecord, fromSpool records.CommandRecord, src output.Source) {
	t.Helper()
	text, enc, digest := output.EncodeOutput(raw)
	observed := true
	fromString = records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: "r1", ActivityType: "command", Command: "show big", Status: "succeeded", Output: text, OutputEncoding: enc, OutputBytes: int64(len(raw)), OutputSHA256: digest, Prompt: "r1#", PromptBefore: "r1#", PromptSource: "observed", PromptObserved: &observed, Device: records.DeviceProjection{CanonicalName: "r1"}, SelectedAddress: "192.0.2.1", Timing: records.Timing{EndedAt: time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)}, Notices: []records.Notice{}}
	fromSpool = fromString
	fromSpool.Output = ""
	path := filepath.Join(t.TempDir(), "a.r1.1.1.spool")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return fromString, fromSpool, output.FromSpool(path, int64(len(raw)), digest, enc)
}

// TestRendererStreamsASpooledRecordInEveryFormat:
// the text, json, and jsonl formats of a spooled record, streamed from its
// source, are byte for byte what the same record renders with its output
// as a string, for output that is UTF-8 and output that is not, with and
// without the escape-sequence stripper.
func TestRendererStreamsASpooledRecordInEveryFormat(t *testing.T) {
	raws := map[string][]byte{
		"utf-8":  []byte(strings.Repeat("line of \"output\" é€😀\n", 5000)),
		"ansi":   []byte(strings.Repeat("\x1b[32mgreen\x1b[0m plain \x1b]0;title\x07 text\n", 3000) + "\x1b[1mbold at the end"),
		"binary": append([]byte(strings.Repeat("bytes\n", 20000)), 0xff, 0xfe),
	}
	for _, ansi := range []string{"strip", "preserve"} {
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`output.ansi="` + ansi + `"`}})
		if err != nil {
			t.Fatal(err)
		}
		for name, raw := range raws {
			for _, format := range []string{"text", "json", "jsonl"} {
				fromString, fromSpool, src := spooledRecord(t, raw)
				render := func(rec records.CommandRecord, src output.Source) []byte {
					var out bytes.Buffer
					r, err := newRecordRenderer(&out, format, cfg, false, false, "activity-1", "/tmp/artifacts", "command", true, false, false)
					if err != nil {
						t.Fatal(err)
					}
					r.OnRecordFrom(rec, src)
					if err := r.WriteFooter(time.Time{}, 0, 0, nil); err != nil {
						t.Fatal(err)
					}
					if err := r.Error(); err != nil {
						t.Fatalf("%s %s %s: %v", ansi, name, format, err)
					}
					return out.Bytes()
				}
				want := render(fromString, output.FromRecord(&fromString))
				got := render(fromSpool, src)
				if !bytes.Equal(got, want) {
					t.Fatalf("ansi=%s %s %s: the spooled record renders differently (%d and %d bytes)", ansi, name, format, len(got), len(want))
				}
				if format == "text" && ansi == "strip" && name == "ansi" && bytes.Contains(got, []byte{0x1b}) {
					t.Fatalf("an escape sequence survived the stripper: %q", got[:min(len(got), 80)])
				}
			}
		}
	}
}

// TestANSIStripperMatchesReplaceAll holds the streaming stripper to
// ansiRE.ReplaceAll over the whole output, the output cut into random
// chunks, sequences cut mid-way included; an escape that never completes
// is text.
func TestANSIStripperMatchesReplaceAll(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	pieces := []string{"text ", "\n", "\x1b[31m", "\x1b[0m", "\x1b]0;a title\x07", "\x1b]2;osc\x1b\\", "\x1b", "[", "m", "é", "\x1b[38;5;200m"}
	for i := 0; i < 3000; i++ {
		var b strings.Builder
		for n := rng.Intn(12); n > 0; n-- {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		raw := []byte(b.String())
		want := ansiRE.ReplaceAll(raw, nil)
		var got bytes.Buffer
		a := &ansiStripper{w: &got}
		for rest := raw; len(rest) > 0; {
			k := 1 + rng.Intn(len(rest))
			if _, err := a.Write(rest[:k]); err != nil {
				t.Fatal(err)
			}
			rest = rest[k:]
		}
		if err := a.flush(); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got.Bytes(), want) {
			t.Fatalf("%q: stripped to %q, want %q", raw, got.Bytes(), want)
		}
		if a.wrote != (len(want) > 0) {
			t.Fatalf("%q: wrote=%v for %d bytes out", raw, a.wrote, len(want))
		}
	}
	// A carry past the bound is text: an escape followed by more than
	// ansiCarryMax bytes of nothing that ends it.
	long := append([]byte{0x1b, ']'}, bytes.Repeat([]byte("x"), ansiCarryMax+10)...)
	var got bytes.Buffer
	a := &ansiStripper{w: &got}
	a.Write(long[:100])
	a.Write(long[100:])
	a.flush()
	if !bytes.Equal(got.Bytes(), long) {
		t.Fatalf("an unbounded escape must pass as text: %d bytes out of %d", got.Len(), len(long))
	}
}

// TestDaemonRendererCountsAndWritesNothing: under
// the daemon the renderer keeps the counts the summary needs and formats
// nothing, in every format.
func TestDaemonRendererCountsAndWritesNothing(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	fromString, _, _ := spooledRecord(t, []byte("ok\n"))
	failed := fromString
	failed.RecordID, failed.Status = "r2", "timeout"
	failed.Error = &records.StructuredError{Code: "command_timeout", Category: "timeout", Message: "cut"}
	for _, format := range []string{"text", "json", "jsonl"} {
		var out bytes.Buffer
		r, err := newRecordRenderer(&out, format, cfg, true, false, "activity-1", "", "run", false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		r.daemon = true
		r.OnRecordFrom(fromString, output.FromRecord(&fromString))
		r.OnRecordFrom(failed, output.FromRecord(&failed))
		if out.Len() != 0 || r.OutputBytes() != 0 {
			t.Fatalf("%s: the daemon's renderer wrote %d bytes", format, out.Len())
		}
		if r.RequestedCounts()["succeeded"] != 1 || r.RequestedCounts()["timeout"] != 1 || r.ErrorCounts()["command_timeout"] != 1 {
			t.Fatalf("%s: counts %v %v", format, r.RequestedCounts(), r.ErrorCounts())
		}
	}
}
