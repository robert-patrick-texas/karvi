package executor

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSleepWaitsBeforeItsCommand: a command's --sleep is waited after the
// previous command ends and before the command is sent (EXAMPLES chapter
// 95); it is no command, so the records are the two commands', and the
// debug line names it.
func TestSleepWaitsBeforeItsCommand(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show clock"}, false)
	h.exec.opts.SleepsNS = []int64{0, int64(400 * time.Millisecond)}
	_, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "success" || recs[1].Status != "success" {
		t.Fatalf("records: %s", describe(recs))
	}
	if recs[1].Timing.CommandStartedAt == nil {
		t.Fatal("the second command has no start time")
	}
	if gap := recs[1].Timing.CommandStartedAt.Sub(recs[0].Timing.EndedAt); gap < 400*time.Millisecond {
		t.Fatalf("the second command started %s after the first ended; the sleep is 400ms", gap)
	}
	if saw := h.deviceSaw(); saw != "show version|show clock" {
		t.Fatalf("the device received %q", saw)
	}
	h.mu.Lock()
	debug := strings.Join(h.debug, "\n")
	h.mu.Unlock()
	if !strings.Contains(debug, "device sleep target=") || !strings.Contains(debug, "index=2 total=2 sleep=400ms") {
		t.Fatalf("debug lines:\n%s", debug)
	}
}

// TestSleepEndsAtTheDeviceDeadline: execution.device-timeout cuts a sleep
// short; the command after it is the device's timeout, not sent.
func TestSleepEndsAtTheDeviceDeadline(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show clock"}, false, `execution.device-timeout="1s"`)
	h.exec.opts.SleepsNS = []int64{0, int64(30 * time.Second)}
	start := time.Now()
	res, recs := h.run(context.Background())
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the device took %s under a 1s device timeout", elapsed)
	}
	if len(recs) != 2 || recs[0].Status != "success" || recs[1].Status != "timeout" {
		t.Fatalf("records: %s", describe(recs))
	}
	if e := recs[1].Error; e == nil || e.Code != "device_timeout" || !strings.Contains(e.Message, "not sent") {
		t.Fatalf("the unsent command's error: %+v", e)
	}
	if res.ErrorCode != "device_timeout" {
		t.Fatalf("result: %+v", res)
	}
	if saw := h.deviceSaw(); saw != "show version" {
		t.Fatalf("the device received %q", saw)
	}
}

// TestSleepEndsAtCancel: a cancel ends a sleep at once; the command after
// it is not attempted, as any command a cancel stops before it is sent.
func TestSleepEndsAtCancel(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show clock"}, false)
	h.exec.opts.SleepsNS = []int64{0, int64(30 * time.Second)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			h.mu.Lock()
			asleep := strings.Contains(strings.Join(h.debug, "\n"), "device sleep target=")
			h.mu.Unlock()
			if asleep {
				cancel()
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()
	start := time.Now()
	_, recs := h.run(ctx)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the device took %s after a cancel during a 30s sleep", elapsed)
	}
	if len(recs) != 2 || recs[0].Status != "success" || recs[1].Status != "cancel" {
		t.Fatalf("records: %s", describe(recs))
	}
	if saw := h.deviceSaw(); saw != "show version" {
		t.Fatalf("the device received %q", saw)
	}
}
