package devsession

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/platform"
)

// feedAll runs a response over chunks and finishes it with the prompt,
// returning finish's error for the caller to judge.
func feedAll(t *testing.T, r *response, chunks []string, returned string) error {
	t.Helper()
	for _, c := range chunks {
		if err := r.feed([]byte(c)); err != nil {
			return err
		}
	}
	return r.finish(returned)
}

// TestSettledSpoolsPastTheThreshold: the
// first threshold bytes stay in memory, the byte that would cross it opens
// the spool, the settled head goes in first, and the file holds the whole
// response, 0600, named by output.SpoolName; the result carries the file
// with its count, digest, and encoding, and no Output. A threshold of 0
// spools from the first byte; a threshold the response never reaches
// leaves it in memory.
func TestSettledSpoolsPastTheThreshold(t *testing.T) {
	dir := t.TempDir()
	const raw = "show big\r\nline 1 xxxx\r\nline 2 xxxx\r\nline 3 xxxx\r\nRouter#"
	const want = "line 1 xxxx\nline 2 xxxx\nline 3 xxxx\n"
	sum := sha256.Sum256([]byte(want))
	for _, threshold := range []int64{0, 5, 16, int64(len(want)) - 1} {
		sp := Spool{Dir: dir, Threshold: threshold, Activity: "260927-120000-00", Device: "lab-001"}
		var opened string
		sink := newSettled(1<<20, sp, 3, nil, func(p string) { opened = p })
		r := newResponse("show big", "Router#", sink)
		if err := feedAll(t, r, []string{raw[:12], raw[12:30], raw[30:]}, "Router#"); err != nil {
			t.Fatalf("threshold %d: %v", threshold, err)
		}
		var res platform.Result
		sink.output(&res)
		if res.Output != nil || res.Spool == nil {
			t.Fatalf("threshold %d: output %q spool %+v, want the spool alone", threshold, res.Output, res.Spool)
		}
		wantPath := filepath.Join(dir, output.SpoolName(sp.Activity, sp.Device, 3, os.Getpid()))
		if res.Spool.Path != wantPath || opened != wantPath {
			t.Fatalf("threshold %d: spool at %q (opened %q), want %q", threshold, res.Spool.Path, opened, wantPath)
		}
		data, err := os.ReadFile(res.Spool.Path)
		if err != nil || string(data) != want {
			t.Fatalf("threshold %d: the file holds %q (%v), want %q", threshold, data, err, want)
		}
		if fi, _ := os.Stat(res.Spool.Path); fi.Mode().Perm() != 0o600 {
			t.Fatalf("threshold %d: mode %v, want 0600", threshold, fi.Mode().Perm())
		}
		if res.Spool.Bytes != int64(len(want)) || res.Spool.SHA256 != hex.EncodeToString(sum[:]) || !res.Spool.UTF8 {
			t.Fatalf("threshold %d: described as %+v", threshold, res.Spool)
		}
		if sink.mem != nil {
			t.Fatalf("threshold %d: the memory was not let go: %q", threshold, sink.mem)
		}
		os.Remove(res.Spool.Path)
	}
	// At the response's own size nothing crosses: memory.
	sink := newSettled(1<<20, Spool{Dir: dir, Threshold: int64(len(want))}, 4, nil, nil)
	r := newResponse("show big", "Router#", sink)
	if err := feedAll(t, r, []string{raw}, "Router#"); err != nil {
		t.Fatal(err)
	}
	var res platform.Result
	sink.output(&res)
	if string(res.Output) != want || res.Spool != nil {
		t.Fatalf("at the threshold: output %q spool %+v", res.Output, res.Spool)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a spool was left: %v", entries)
	}
}

// TestLimitCountsSettledBytes: the settled byte that would
// pass the limit stops the read, the store holds exactly the first limit
// bytes, observed is the settled count at the cut; and a limit passed by
// the closing newline alone is the limit after the prompt.
func TestLimitCountsSettledBytes(t *testing.T) {
	sink := newSettled(10, Spool{}, 1, nil, nil)
	r := newResponse("show x", "R#", sink)
	err := feedAll(t, r, []string{"show x\r\nabcdefghijklmnop\r\n", "R#"}, "R#")
	var limit *limitError
	if !errors.As(err, &limit) {
		t.Fatalf("err = %v, want the limit", err)
	}
	if string(sink.mem) != "abcdefghij" || sink.n != 10 || limit.observed != 16 || limit.limit != 10 {
		t.Fatalf("stored %q (%d), observed %d of %d", sink.mem, sink.n, limit.observed, limit.limit)
	}
	if err := r.feed([]byte("more")); err != limit {
		t.Fatalf("after the limit feed returned %v", err)
	}
	var res platform.Result
	sink.output(&res)
	if string(res.Output) != "abcdefghij" {
		t.Fatalf("the result holds %q", res.Output)
	}

	// Exactly the limit settled before the prompt; the newline passes it at
	// finish: the read was whole (no error from feed), finish reports it.
	sink = newSettled(10, Spool{}, 1, nil, nil)
	r = newResponse("show x", "R#", sink)
	for _, c := range []string{"show x\r\nabcdefghij\r\n", "R#"} {
		if err := r.feed([]byte(c)); err != nil {
			t.Fatalf("feed: %v", err)
		}
	}
	err = r.finish("R#")
	if !errors.As(err, &limit) || limit.observed != 11 || string(sink.mem) != "abcdefghij" {
		t.Fatalf("finish = %v, stored %q", err, sink.mem)
	}

	// A threshold at or above the limit never opens a spool.
	dir := t.TempDir()
	sink = newSettled(10, Spool{Dir: dir, Threshold: 10}, 1, nil, nil)
	r = newResponse("show x", "R#", sink)
	_ = feedAll(t, r, []string{"show x\r\nabcdefghijklmnop\r\nR#"}, "R#")
	if entries, _ := os.ReadDir(dir); len(entries) != 0 || sink.file != nil {
		t.Fatalf("a spool opened at the limit: %v", entries)
	}
}

// TestTailIsBounded: a last line longer than one read
// chunk settles from its front and is no prompt candidate, even when it
// ends like a prompt; its newline restores the match, and the recorded
// bytes are the whole line.
func TestTailIsBounded(t *testing.T) {
	p, err := compile(iosxe(t))
	if err != nil {
		t.Fatal(err)
	}
	sink := newSettled(1<<20, Spool{}, 1, nil, nil)
	r := newResponse("show x", "Router#", sink)
	long := strings.Repeat("y", 5000) + "Router#"
	for _, c := range []string{"show x\r\n", long[:3000], long[3000:]} {
		if err := r.feed([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.tail) > tailMax || !r.overflow {
		t.Fatalf("tail %d bytes overflow=%v", len(r.tail), r.overflow)
	}
	if prompt, _, _ := r.candidate(nil, p); prompt != "" {
		t.Fatalf("a settled-from-the-front line matched the prompt %q", prompt)
	}
	if err := r.feed([]byte("\r\nRouter#")); err != nil {
		t.Fatal(err)
	}
	if prompt, level, _ := r.candidate(nil, p); prompt != "Router#" || level != "privilege-exec" {
		t.Fatalf("after the newline: prompt %q level %q", prompt, level)
	}
	if err := r.finish("Router#"); err != nil {
		t.Fatal(err)
	}
	if string(sink.mem) != long+"\n" {
		t.Fatalf("recorded %d bytes, want the long line whole", len(sink.mem))
	}
	// A whitespace run before the last line is part of the tail and is
	// bounded with it; the last line stays a candidate when it was not cut.
	sink = newSettled(1<<20, Spool{}, 1, nil, nil)
	r = newResponse("show x", "Router#", sink)
	if err := feedAll(t, r, []string{"show x\r\nabc\r\n" + strings.Repeat("\r\n", 3000), "Router#"}, "Router#"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(sink.mem), "abc\n") || r.overflow {
		t.Fatalf("recorded %q overflow=%v", sink.mem[:min(len(sink.mem), 8)], r.overflow)
	}
}

// TestFailurePatternsAreFoundAcrossChunkBorders:
// a failure pattern cut by a chunk border is found as the bytes settle.
func TestFailurePatternsAreFoundAcrossChunkBorders(t *testing.T) {
	patterns := iosxe(t).FailurePatterns
	const bad = "% Invalid input detected at '^' marker."
	if !deviceFailure(patterns, []byte(bad)) {
		t.Skip("the platform's patterns do not include the marker line")
	}
	raw := "show bogus\r\n     ^\r\n" + bad + "\r\n\r\nRouter#"
	for i := 1; i < len(raw); i++ {
		sink := newSettled(1<<20, Spool{}, 1, patterns, nil)
		r := newResponse("show bogus", "Router#", sink)
		if err := feedAll(t, r, []string{raw[:i], raw[i:]}, "Router#"); err != nil {
			t.Fatal(err)
		}
		if !sink.failures.found {
			t.Fatalf("split at %d: the failure pattern was not found in %q", i, sink.mem)
		}
	}
	sink := newSettled(1<<20, Spool{}, 1, patterns, nil)
	if err := feedAll(t, newResponse("show clock", "Router#", sink), []string{"show clock\r\n12:00\r\nRouter#"}, "Router#"); err != nil || sink.failures.found {
		t.Fatalf("a clean answer was found failing (%v)", err)
	}
}

// TestUTF8CheckMatchesValid holds the incremental check to utf8.Valid over
// random byte strings cut at random borders, runes split included.
func TestUTF8CheckMatchesValid(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pieces := []string{"a", "é", "€", "😀", "\xff", "\xe2\x82", "\x80", "\xf0\x9f", "z"}
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for n := rng.Intn(8); n > 0; n-- {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		raw := []byte(b.String())
		var u utf8Check
		for len(raw) > 0 {
			k := 1 + rng.Intn(len(raw))
			u.write(raw[:k])
			raw = raw[k:]
		}
		if u.valid() != utf8.Valid([]byte(b.String())) {
			t.Fatalf("%q: check %v, utf8.Valid %v", b.String(), u.valid(), utf8.Valid([]byte(b.String())))
		}
	}
}

// TestSpoolWriteFailureRecordsNothing: a spool that cannot
// be created ends the read with output_spool_write_failed, the memory let
// go, so the result holds nothing of the output.
func TestSpoolWriteFailureRecordsNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	sink := newSettled(1<<20, Spool{Dir: dir, Threshold: 4, Activity: "a", Device: "d"}, 1, nil, nil)
	r := newResponse("show x", "R#", sink)
	err := feedAll(t, r, []string{"show x\r\nabcdefgh\r\nR#"}, "R#")
	if errorcodes.Of(err) != "output_spool_write_failed" || !strings.Contains(err.Error(), dir) {
		t.Fatalf("err = %v", err)
	}
	var res platform.Result
	sink.output(&res)
	if res.Output != nil || res.Spool != nil || sink.n != 0 {
		t.Fatalf("something of the output was handed over: %q %+v n=%d", res.Output, res.Spool, sink.n)
	}
}

// TestSetupReadsSettleIntoMemory: the login, enable, and
// paging reads use the reader without a spool, whatever the session's
// spool says, and their answers are the cleaned lines the set-up shows.
func TestSetupReadsSettleIntoMemory(t *testing.T) {
	dir := t.TempDir()
	sh := newShell(shellOptions{enable: "en"})
	s := open(t, sh, Options{Definition: iosxe(t), EnableSecret: secret("en"), Spool: Spool{Dir: dir, Threshold: 0, Activity: "a", Device: "d"}})
	if err := s.Prepare(t.Context()); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("a set-up read spooled: %v", entries)
	}
	lines := s.SetupLines()
	if len(lines) < 2 || lines[0].Statement != "enable" || lines[0].Output != "Password:\n" || lines[1].Output != "" {
		t.Fatalf("set-up lines %+v", lines)
	}
	// A command with threshold 0 spools from its first byte, and the result
	// names the file.
	r := s.Execute(t.Context(), platform.Command{Text: "show clock"})
	if r.Err != nil || r.Spool == nil || r.Output != nil {
		t.Fatalf("show clock: %+v", r)
	}
	data, err := os.ReadFile(r.Spool.Path)
	if err != nil || string(data) != "*10:00:00.000 UTC Tue Sep 15 2026\n" || r.Spool.Bytes != int64(len(data)) {
		t.Fatalf("the spool holds %q (%v), described %+v", data, err, r.Spool)
	}
	if filepath.Base(r.Spool.Path) != output.SpoolName("a", "d", 1, os.Getpid()) {
		t.Fatalf("named %s", filepath.Base(r.Spool.Path))
	}
	os.Remove(r.Spool.Path)
	s.Close()
}

// TestInFlightBytesArePublished: the sink stores
// its settled count into the counter as bytes settle, and Execute leaves
// it at 0 when the command ends.
func TestInFlightBytesArePublished(t *testing.T) {
	var count atomic.Int64
	sink := newSettled(1<<20, Spool{}, 1, nil, nil)
	sink.progress = &count
	r := newResponse("show x", "R#", sink)
	if err := feedAll(t, r, []string{"show x\r\nabc\r\ndefgh\r\n", "R#"}, "R#"); err != nil {
		t.Fatal(err)
	}
	if count.Load() != int64(len("abc\ndefgh\n")) || count.Load() != sink.n {
		t.Fatalf("published %d, settled %d", count.Load(), sink.n)
	}
	sh := newShell(shellOptions{privileged: true, bigLines: 200})
	count.Store(0)
	s := open(t, sh, Options{Definition: iosxe(t), InFlightBytes: &count})
	res := s.Execute(t.Context(), platform.Command{Text: "show big", Timeout: 5 * time.Second})
	if res.Err != nil || len(res.Output) != 200*73 {
		t.Fatalf("show big: %+v", res)
	}
	if count.Load() != 0 {
		t.Fatalf("after the command the counter holds %d", count.Load())
	}
	s.Close()
}
