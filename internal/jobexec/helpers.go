package jobexec

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"os"
	"sort"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// jobWidth is the width a job would run at for the spool directory's
// free-space rule: the smaller of the job's own
// width (the parallel width, the wave's maximum, 1 for a serial job), the
// server's cap, and the device count.
func jobWidth(d executionplan.DispatchSettings, serverLimit, devices int) int {
	width := 1
	switch d.Mode {
	case executionplan.DispatchParallel:
		width = d.Width
	case executionplan.DispatchWave:
		width = d.MaxWidth
	}
	width = minInt(maxInt(1, width), maxInt(1, devices))
	if serverLimit > 0 {
		width = minInt(width, serverLimit)
	}
	return width
}

// narrowDispatch caps the job's widths at width (spool_width_narrowed): the
// parallel width, and the wave's start and maximum; a serial job has no
// width to narrow.
func narrowDispatch(d executionplan.DispatchSettings, width int) executionplan.DispatchSettings {
	if width < 1 {
		width = 1
	}
	if d.Width > width {
		d.Width = width
	}
	if d.StartWidth > width {
		d.StartWidth = width
	}
	if d.MaxWidth > width {
		d.MaxWidth = width
	}
	return d
}

// SortedStatusKeys is used by human summaries and tests.

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func cloneIntMap(m map[string]int) map[string]int {
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func digestJSON(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func commandHashes(commands []string) []string {
	out := make([]string, len(commands))
	for i, c := range commands {
		sum := sha256.Sum256([]byte(c))
		out[i] = hex.EncodeToString(sum[:])
	}
	return out
}

func hostname() string { h, _ := os.Hostname(); return h }

func producerInfo() records.Producer {
	return records.Producer{Hostname: hostname(), BootID: osutil.BootID(), PID: os.Getpid(), ProcessStartIdentity: osutil.ProcessStartIdentity(os.Getpid()), AppVersion: buildinfo.Version}
}

func determineExit(activityType string, d dispatch.Summary, outputErr, contextErr, shutdown error) int {
	if outputErr != nil {
		return exitcode.ExitOutputFailure
	}
	if shutdown != nil {
		return exitcode.ExitShutdownIncomplete
	}
	if contextErr != nil || d.HaltReason == "cancelled" {
		return exitcode.ExitCancelled
	}
	switch d.HaltReason {
	case "host_key_mismatch":
		return exitcode.ExitHaltHostKeyMismatch
	case "error_count":
		return exitcode.ExitHaltErrorCount
	case "error_percent":
		return exitcode.ExitHaltErrorPercent
	}
	switch d.GateReason {
	case "error_count":
		return exitcode.ExitWaveGateErrorCount
	case "error_percent":
		return exitcode.ExitWaveGateErrorPercent
	}
	if d.Counts.Failed == 0 && d.Counts.NotStarted == 0 {
		return exitcode.ExitSuccess
	}
	if activityType == "run" {
		return exitcode.ExitPartialFailure
	}
	// The first failure whose registered code sets an exit decides it.
	for _, r := range d.Results {
		if entry, ok := errorcodes.Lookup(r.ErrorCode); ok && entry.Exit != 0 {
			return entry.Exit
		}
	}
	return exitcode.ExitDeviceFailure
}

func finalScoreboardStatus(code int, d dispatch.Summary) string {
	switch code {
	case exitcode.ExitSuccess:
		return "completed"
	case exitcode.ExitHaltErrorCount, exitcode.ExitHaltErrorPercent, exitcode.ExitWaveGateErrorCount, exitcode.ExitWaveGateErrorPercent, exitcode.ExitHaltHostKeyMismatch:
		return "halted"
	case exitcode.ExitCancelled:
		return "cancelled"
	case exitcode.ExitShutdownIncomplete:
		return "incomplete"
	default:
		_ = d
		return "errored"
	}
}

// DispatchPlan is the dispatcher's plan for d under cfg, without its
// tasks: what a job would run with, for a report (a dry run's dispatch
// line) to describe.
func DispatchPlan(cfg configload.Snapshot, activityType string, d executionplan.DispatchSettings) dispatch.Plan {
	return buildPlan(cfg, activityType, d, nil)
}

func buildPlan(cfg configload.Snapshot, activityType string, d executionplan.DispatchSettings, tasks []dispatch.Task) dispatch.Plan {
	plan := dispatch.Plan{Mode: d.Mode, Tasks: tasks, Width: d.Width, AbsoluteMaxWidth: cfg.Int("dispatch.absolute-max-width"), WaveStartWidth: d.StartWidth, WaveMaxWidth: d.MaxWidth, WaveDepthMultiplier: cfg.Int("dispatch.wave-depth-multiplier"), CPUThreshold: cfg.Float("dispatch.wave-cpu-threshold-percent"), CPUTargetZone: cfg.Float("dispatch.wave-cpu-target-zone-percent"), StepUpPercent: cfg.Float("dispatch.wave-step-up-percent"), StepDownPercent: cfg.Float("dispatch.wave-step-down-percent"), CooldownWaves: cfg.Int("dispatch.wave-cooldown-waves"), HaltErrorCount: cfg.Int("dispatch.halt-on-error-count"), HaltErrorPercent: cfg.Int("dispatch.halt-on-error-percent"), WaveGateErrorCount: cfg.Int("dispatch.wave-gate-error-count"), WaveGateErrorPercent: cfg.Int("dispatch.wave-gate-error-percent"), WaveDelay: cfg.Duration("dispatch.wave-gate-timed-delay")}
	if activityType == "run" && cfg.Bool("ssh.halt-run-on-host-key-mismatch") {
		plan.HaltErrorCodes = []string{"host_key_changed", "host_key_changed_during_enrollment"}
	}
	return plan
}

// SortedStatusKeys is used by human summaries and tests.
func SortedStatusKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// terminalCounts moves the devices a shutdown or a cancel left unfinished
// out of the dispatcher's completed and failed counts: under a shutdown
// cause they are incomplete, under a cancelled context they are
// cancelled, and in both cases the devices never started join them.
// Neither kind is a device error;
// a device that failed on its own before the cancel keeps its failure.
func terminalCounts(summary *dispatch.Summary, shutdown error, cancelled bool) (incomplete, cancelledN int) {
	var code string
	switch {
	case shutdown != nil:
		code = "shutdown_incomplete"
	case cancelled || summary.HaltReason == "cancelled":
		code = "cancelled"
	default:
		return 0, 0
	}
	n := 0
	for _, r := range summary.Results {
		if !r.Success && r.ErrorCode == code {
			n++
			summary.Counts.Terminal--
			summary.Counts.Failed--
		}
	}
	n += summary.Counts.NotStarted
	summary.Counts.NotStarted = 0
	if code == "cancelled" {
		return 0, n
	}
	return n, 0
}

// cancellationOf is the summary's cancellation block for a context the
// cancel_job cause cancelled, nil otherwise.
func cancellationOf(ctx context.Context) *records.Cancellation {
	rq, ok := CancelRequestOf(ctx)
	if !ok {
		return nil
	}
	return &records.Cancellation{RequestedAt: rq.RequestedAt, Reason: rq.Reason, Requester: records.CancelRequester{PID: rq.RequesterPID, UID: rq.RequesterUID}, RequestID: rq.RequestID}
}

// WriteCancelRequested writes the run.cancel_requested audit record for an
// accepted cancel_job request, through the sink the caller holds; the
// request's details are the cancellation block.
func WriteCancelRequested(s *audit.Sink, req Request, jobID string, rq JobCancelRequest) error {
	c := &records.Cancellation{RequestedAt: rq.RequestedAt, Reason: rq.Reason, Requester: records.CancelRequester{PID: rq.RequesterPID, UID: rq.RequesterUID}, RequestID: rq.RequestID}
	return writeActivityAudit(s, req, req.ActivityID, jobID, "cancel_requested", "informational", 0, "cancelled", c.AuditDetails())
}
