package executor

import (
	"context"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
)

// TestDeviceTimeoutCutsTheCommandInFlight:
// execution.device-timeout bounds the device's whole list. The command the
// deadline interrupts is timeout/device_timeout, the session ends, and the
// rest is not attempted, under --continue-device-on-error too.
func TestDeviceTimeoutCutsTheCommandInFlight(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show slow", "show clock"}, false, `execution.command-timeout="10s"`, `execution.device-timeout="1s"`)
	start := time.Now()
	res, recs := h.run(context.Background())
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("the device took %s under a 1s device timeout", elapsed)
	}
	if len(recs) != 3 || recs[0].Status != "succeeded" || recs[1].Status != "timeout" || recs[2].Status != "not_attempted_prior_command_failure" || recs[2].Error != nil {
		t.Fatalf("records: %s", describe(recs))
	}
	e := recs[1].Error
	if e == nil || e.Code != "device_timeout" || e.Category != "timeout" || !e.Retryable || !e.External || !recs[1].RetryEligible || !strings.Contains(e.Message, "execution.device-timeout (1s)") || !strings.Contains(e.Message, "interrupted") {
		t.Fatalf("the interrupted command's error: %+v", e)
	}
	if res.Success || res.ErrorCode != "device_timeout" {
		t.Fatalf("result: %+v", res)
	}
	if saw := h.deviceSaw(); saw != "show version|show slow" {
		t.Fatalf("the device received %q", saw)
	}
}

// TestCommandTimeoutBeforeTheDeviceDeadlineKeepsItsCode: whichever deadline
// comes first names the code.
func TestCommandTimeoutBeforeTheDeviceDeadlineKeepsItsCode(t *testing.T) {
	h := newSessionHarness(t, []string{"show slow", "show clock"}, false, `execution.device-timeout="30s"`)
	_, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "timeout" || recs[0].Error == nil || recs[0].Error.Code != "command_timeout" {
		t.Fatalf("records: %s", describe(recs))
	}
}

// TestDeviceTimeoutBetweenCommands: a deadline that has passed when the next
// command is due is that command's device_timeout; it is not sent, and the
// shell, still in step, is closed with the platform's exit.
func TestDeviceTimeoutBetweenCommands(t *testing.T) {
	h := newSessionHarness(t, []string{"show version", "show clock"}, true, `execution.device-timeout="1s"`)
	// The loader holds the timeout to 1s or more; the passed deadline is
	// forced on the job's configuration, which the executor reads per
	// device.
	values := h.exec.opts.Config.ValueMap()
	values["execution.device-timeout"] = "1ns"
	forced, err := configload.FromValues(values)
	if err != nil {
		t.Fatal(err)
	}
	h.exec.opts.Config = forced
	res, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "timeout" || recs[1].Status != "not_attempted_prior_command_failure" || recs[1].Error != nil {
		t.Fatalf("records: %s", describe(recs))
	}
	if e := recs[0].Error; e == nil || e.Code != "device_timeout" || !strings.Contains(e.Message, "not sent") {
		t.Fatalf("the unsent command's error: %+v", e)
	}
	if res.ErrorCode != "device_timeout" {
		t.Fatalf("result: %+v", res)
	}
	log, _ := os.ReadFile(h.log)
	if saw := strings.TrimSpace(string(log)); strings.Contains(saw, "show") || !strings.HasSuffix(saw, "exit") {
		t.Fatalf("the device received %q", saw)
	}
}

// TestDeviceTimeoutDuringTheProfile: the clock covers the session-init
// profile; a profile command it cuts ends the device as a profile failure
// does, under on-error continue too.
func TestDeviceTimeoutDuringTheProfile(t *testing.T) {
	h := newSessionHarness(t, []string{"show version"}, false, `execution.device-timeout="1s"`)
	h.withProfile(executionplan.SessionInitContinue, 10*time.Second, "show slow", "show clock")
	res, recs := h.run(context.Background())
	if len(recs) != 3 || recs[0].Status != "timeout" || recs[0].Error == nil || recs[0].Error.Code != "device_timeout" ||
		recs[1].Status != "not_attempted_prior_command_failure" || recs[2].Status != "not_attempted_session_init_failure" {
		t.Fatalf("records: %s", describe(recs))
	}
	if res.ErrorCode != "device_timeout" {
		t.Fatalf("result: %+v", res)
	}
}
