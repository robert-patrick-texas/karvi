package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestCollectionHook covers the collection hook at the
// runner: the hook runs in the collection directory with the replaced files
// sorted on stdin and the job in the environment, its output on stderr; a
// non-zero exit, a hook that cannot start, and one past its bound (a shell
// and its child ended together) each name their cause; nothing replaced is
// an empty stdin.
func TestCollectionHook(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hook.out")
	t.Setenv("HOOK_OUT", out)
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n{ echo \"pwd=$PWD\"; echo \"$KARVI_JOB_ID $KARVI_JOB_DIR $KARVI_CRUN_DIRECTORY $KARVI_CRUN_REPLACED $KARVI_CRUN_KEPT $KARVI_EXIT\"; cat; } >\"$HOOK_OUT\"\necho hook ran\nexit \"${HOOK_EXIT:-0}\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	summary := &records.CollectionSummary{Directory: dir, Replaced: 2, Kept: 1, Devices: map[string]records.CollectionDevice{
		"r2": {File: "r2", Outcome: records.CollectionReplaced}, "r1": {File: "r1", Outcome: records.CollectionReplaced}, "dead": {File: "dead", Outcome: records.CollectionKept}}}
	h := collectionHook{Path: script, Timeout: 5 * time.Second, JobID: "260926-101515-00", JobDir: "/j/260926-101515-00", Exit: 101, Summary: summary}
	var stderr bytes.Buffer
	if o := h.run(context.Background(), &stderr); o.err != nil || o.exit != 0 || o.timedOut {
		t.Fatalf("a hook that exits 0: %+v", o)
	}
	if stderr.String() != "hook ran\n" {
		t.Fatalf("the hook's output goes to stderr: %q", stderr.String())
	}
	got, _ := os.ReadFile(out)
	want := "pwd=" + dir + "\n260926-101515-00 /j/260926-101515-00 " + dir + " 2 1 101\nr1\nr2\n"
	if string(got) != want {
		t.Fatalf("the hook's input:\n%s\nwant:\n%s", got, want)
	}

	t.Setenv("HOOK_EXIT", "3")
	if o := h.run(context.Background(), &stderr); o.err == nil || o.exit != 3 || !strings.Contains(o.err.Error(), "exited 3") {
		t.Fatalf("a hook that exits 3: %+v", o)
	}
	t.Setenv("HOOK_EXIT", "0")

	none := collectionHook{Path: script, Timeout: 5 * time.Second, Summary: &records.CollectionSummary{Directory: dir, Kept: 1, Devices: map[string]records.CollectionDevice{"dead": {File: "dead", Outcome: records.CollectionKept}}}}
	if o := none.run(context.Background(), &stderr); o.err != nil {
		t.Fatalf("nothing replaced still runs the hook: %+v", o)
	}
	if got, _ := os.ReadFile(out); !strings.HasSuffix(string(got), " 0 1 0\n") {
		t.Fatalf("nothing replaced is an empty stdin: %q", got)
	}

	missing := collectionHook{Path: filepath.Join(dir, "absent"), Timeout: time.Second, Summary: summary}
	if o := missing.run(context.Background(), &stderr); o.err == nil || !strings.Contains(o.err.Error(), "could not be started") {
		t.Fatalf("a hook that cannot start: %+v", o)
	}

	slow := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bound := collectionHook{Path: slow, Timeout: 300 * time.Millisecond, Summary: summary}
	start := time.Now()
	o := bound.run(context.Background(), &stderr)
	if !o.timedOut || o.err == nil || !strings.Contains(o.err.Error(), "crun.after-timeout") {
		t.Fatalf("a hook past its bound: %+v", o)
	}
	if wall := time.Since(start); wall > 3*time.Second {
		t.Fatalf("the shell and its sleep were not ended together: %s", wall)
	}
}

// TestCollectionHookEndsItsGroup covers the bound on a hook whose child
// ignores SIGTERM: the shell goes at the signal, the child is killed killGrace
// later, and run returns once the hook's process group is gone.
func TestCollectionHookEndsItsGroup(t *testing.T) {
	dir := t.TempDir()
	pgid := filepath.Join(dir, "hook.pgid")
	script := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho \"$$\" >\""+pgid+"\"\n(trap '' TERM; sleep 30)\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := collectionHook{Path: script, Timeout: 300 * time.Millisecond, Summary: &records.CollectionSummary{Directory: dir}}
	var stderr bytes.Buffer
	start := time.Now()
	o := h.run(context.Background(), &stderr)
	wall := time.Since(start)
	if !o.timedOut {
		t.Fatalf("a hook past its bound: %+v", o)
	}
	b, err := os.ReadFile(pgid)
	if err != nil {
		t.Fatal(err)
	}
	group, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(-group, 0); !errors.Is(err, syscall.ESRCH) {
		_ = syscall.Kill(-group, syscall.SIGKILL)
		t.Fatalf("the hook's group %d outlived run: %v", group, err)
	}
	if wall < killGrace || wall > killGrace+3*time.Second {
		t.Fatalf("run returned after %s, want the bound and killGrace", wall)
	}
}
