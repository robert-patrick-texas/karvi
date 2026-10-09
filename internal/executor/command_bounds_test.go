package executor

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestACommandsOwnTimeout: a requested command's --timeout replaces the
// job's command timeout for it alone, the next command keeping the job's;
// each expiry's message names its source, and the command start event
// carries the effective timeout and limit.
func TestACommandsOwnTimeout(t *testing.T) {
	h := newSessionHarness(t, []string{"show slow", "show slow"}, true)
	h.exec.opts.TimeoutsNS = []int64{int64(10 * time.Second), 0}
	_, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Status != "timeout" || recs[1].Error == nil || !strings.HasPrefix(recs[1].Error.Message, "command timed out after 1s (execution.command-timeout) while waiting") {
		t.Fatalf("records: %s", describe(recs))
	}
	if !h.debugHas("device command start target=") || !debugLine(h, "index=1 ", "timeout=10s maxbytes=67108864 blind=false") || !debugLine(h, "index=2 ", "timeout=1s maxbytes=67108864 blind=false") {
		t.Fatalf("command start events:\n%s", strings.Join(h.debug, "\n"))
	}
	h = newSessionHarness(t, []string{"show slow"}, true, `execution.command-timeout="10s"`)
	h.exec.opts.TimeoutsNS = []int64{int64(time.Second)}
	if _, recs = h.run(context.Background()); len(recs) != 1 || recs[0].Status != "timeout" || !strings.HasPrefix(recs[0].Error.Message, "command timed out after 1s (--timeout) while waiting") {
		t.Fatalf("records: %s", describe(recs))
	}
}

// TestACommandsOwnByteLimit: a requested command's --maxbytes replaces the
// job's limit for it alone, its message naming --maxbytes, and the next
// command (after the session the limit closed) is not attempted; without
// it the same output fits.
func TestACommandsOwnByteLimit(t *testing.T) {
	h := newSessionHarness(t, []string{"show big", "show clock"}, true)
	h.exec.opts.MaxBytes = []int64{1024, 0}
	_, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "output_limit_exceeded" || recs[0].OutputBytes != 1024 || !strings.HasPrefix(recs[0].Error.Message, "command output exceeded 1024 bytes (--maxbytes) before the prompt returned") || !debugLine(h, "index=1 ", "maxbytes=1024 ") {
		t.Fatalf("records: %s", describe(recs))
	}
	h = newSessionHarness(t, []string{"show big"}, true)
	if _, recs = h.run(context.Background()); len(recs) != 1 || recs[0].Status != "succeeded" || recs[0].OutputBytes < 2900 {
		t.Fatalf("records: %s", describe(recs))
	}
}

// debugLine reports whether a command start event holding marker holds
// want.
func debugLine(h *sessionHarness, marker, want string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, l := range h.debug {
		if strings.HasPrefix(l, "device command start ") && strings.Contains(l, marker) {
			return strings.Contains(l, want)
		}
	}
	return false
}
