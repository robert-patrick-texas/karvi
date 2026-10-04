// Package executor implements one-device execution over an execution target
// and a credential grant: the address comes from the
// plan, the credential from the package, and the transport from the daemon's
// own configuration; it performs no DNS and no credential resolution.
package executor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/capacity"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/devsession"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/icmpgate"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/metrics"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/internal/transport/native"
	"github.com/robert-patrick-texas/karvi/internal/transport/systemssh"
	telnettransport "github.com/robert-patrick-texas/karvi/internal/transport/telnet"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/records"
)

// Work is one target of the plan queued for execution.
type Work struct {
	Target   executionplan.ExecutionTarget
	QueuedAt time.Time
}

// GrantProvider hands the executor the grant bound to a target;
// credentialpackage.CredentialPackage satisfies it directly.
type GrantProvider interface {
	ForTarget(targetID string) (credentialpackage.CredentialGrant, bool)
}

type Options struct {
	Config                          configload.Snapshot
	Operator                        credentials.Operator
	ActivityID, JobID, ActivityType string
	Commands                        []string
	// PlatformCommands is the plan's list per platform; a device whose
	// platform has one runs it in place of Commands.
	PlatformCommands map[string][]string
	// BlindReturns is each requested command's count of blind returns, one
	// entry per command, or empty for none; BlindWait is the wait for the
	// prompt after a blind send. Blind is each command's tolerance flag, one
	// entry per command or empty, and Expectations each command's
	// expect-and-send declarations, one list per command or empty, as the
	// plan carries them; a count above zero implies the flag. The executor
	// compiles the patterns once per device sequence; the plan's Validate
	// has compiled
	// them before, so a pattern that fails here is execution_plan_invalid
	// and nothing is contacted.
	BlindReturns   []int
	BlindWait      time.Duration
	Blind          []bool
	Expectations   [][]executionplan.Expectation
	CandidateCount int     // command: size of the ordered target set
	DispatchOrder  string  // recorded order name
	ShuffleKey     *string // shuffle and random only
	Grants         GrantProvider
	// SessionInit is the plan's session-init table: each target's profile
	// runs after Prepare and before its requested commands.
	SessionInit map[string]executionplan.SessionInitProfile
	// Protection is the package protection recorded on each credential
	// projection.
	Protection string
	// HaltOnCommandError stops a device's later commands after a failed one:
	// the configuration's rule unless the plan asked to continue.
	HaltOnCommandError bool
	// Ping is the plan's gate block and Pinger the method Detect chose for
	// the job; Pinger is nil, and never touched,
	// when the gate is disabled.
	Ping   executionplan.PingSettings
	Pinger icmpgate.Pinger

	Capacity                                   *capacity.Manager
	Store                                      *output.Store
	Audit                                      *audit.Sink
	Metrics                                    *metrics.Sampler
	ScratchDir, ControlRoot, Home, AskpassPath string
	// BaseDir is the operator's private root, which holds the trust store
	// under ssh.known-hosts-file "auto".
	BaseDir string
	// SpoolDir is spooldir resolved at admission,
	// where a command's response goes once its settled bytes pass
	// output.spool-threshold-bytes; "" keeps every response
	// in memory (a test's executor).
	SpoolDir string
	// InFlight receives each device's running byte count for the
	// scoreboard; nil when nobody watches.
	InFlight *output.InFlight
	// OnRecord receives each appended record with its output's source: a
	// spooled record's Output is empty and the
	// renderer streams from the source before the spool is removed.
	OnRecord func(records.CommandRecord, output.Source)
	Warn     func(string)
	Debug    func(string)
}
type DeviceExecutor struct {
	opts Options
	ping pingStats
}

// pingStats accumulates the summary's ping block.
type pingStats struct {
	mu      sync.Mutex
	summary records.PingSummary
}

// gate is one device's ICMP gate result as the records carry it: the ping
// object and the gate's duration, the same on every record of the device.
type gate struct {
	report *records.PingReport
	ns     *int64
}

// PingSummary is the summary's ping block: nil when the gate is disabled.
func (e *DeviceExecutor) PingSummary() *records.PingSummary {
	if !e.opts.Ping.Enabled {
		return nil
	}
	e.ping.mu.Lock()
	defer e.ping.mu.Unlock()
	out := e.ping.summary
	out.Enabled, out.Probes, out.TimeoutNS = true, e.opts.Ping.Probes, e.opts.Ping.TimeoutNS
	if e.opts.Pinger != nil {
		out.Method = e.opts.Pinger.Method()
	} else {
		out.Method = icmpgate.MethodUnavailable
	}
	out.Devices = map[string]int{}
	for _, k := range []string{records.PingDevicesGated, records.PingDevicesProceeded, records.PingDevicesDegraded, records.PingDevicesSkipped, records.PingDevicesCapabilityFailed} {
		out.Devices[k] = e.ping.summary.Devices[k]
	}
	return &out
}

func New(opts Options) *DeviceExecutor { return &DeviceExecutor{opts: opts} }

func (e *DeviceExecutor) debugf(format string, args ...any) {
	if e.opts.Debug != nil {
		e.opts.Debug(fmt.Sprintf(format, args...))
	}
}

// The shutdown causes. The daemon cancels a job's
// context with one of these through context.WithCancelCause; the executor
// and the runner read the cause and account every unfinished unit as
// incomplete_shutdown (error code shutdown_incomplete) rather than
// cancelled, so a command already sent is never recorded as succeeded or
// errored.
var (
	ErrShutdownForced       = errorcodes.Errorf("shutdown_forced", "the daemon was stopped with --force while the job was running")
	ErrShutdownGraceExpired = errorcodes.Errorf("shutdown_grace_expired", "the daemon's shutdown grace expired while the job was running")
)

// ErrJobCancelled is the cause a cancel_job request puts on one job's
// context: the registered reason code cancelled,
// the code every cancelled unit carries, with the operator's reason in the
// message when JobCancelled builds it. A context cancelled without any
// cause (an operator's interrupt of an in-process run) is accounted the
// same way; only the message differs.
var ErrJobCancelled = JobCancelled("")

// JobCancelRequest is what a cancel_job request carries into the job's
// context: the reason, the request's time
// and ID, and the requester from the connection's peer credentials. The
// runner records it in the summary's cancellation block and the audit
// record; a plain cancel carries none.
type JobCancelRequest struct {
	Reason       string
	RequestedAt  time.Time
	RequestID    string
	RequesterPID int
	RequesterUID int
}

// Cause is the context cause for this request: the registered reason code
// cancelled, with the reason in the message.
func (r JobCancelRequest) Cause() error { return &jobCancelError{req: r} }

func (r JobCancelRequest) message() string {
	if r.Reason == "" {
		return "the job was cancelled by the operator"
	}
	return "the job was cancelled by the operator: " + r.Reason
}

type jobCancelError struct{ req JobCancelRequest }

func (e *jobCancelError) Error() string     { return "cancelled: " + e.req.message() }
func (e *jobCancelError) ErrorCode() string { return "cancelled" }

// JobCancelled builds the cancel_job cause carrying only the operator's
// reason.
func JobCancelled(reason string) error { return JobCancelRequest{Reason: reason}.Cause() }

// CancelCause reports the cancel_job cause a cancelled context carries, or
// nil when the cancellation is a shutdown or carries no cause.
func CancelCause(ctx context.Context) error {
	cause := context.Cause(ctx)
	if cause == nil || cause == ctx.Err() || ShutdownCause(ctx) != nil {
		return nil
	}
	if errorcodes.Of(cause) == "cancelled" {
		return cause
	}
	return nil
}

// CancelRequestOf is the cancel_job request a cancelled context carries.
func CancelRequestOf(ctx context.Context) (JobCancelRequest, bool) {
	if e, ok := CancelCause(ctx).(*jobCancelError); ok {
		return e.req, true
	}
	return JobCancelRequest{}, false
}

// ShutdownCause reports the shutdown cause a cancelled context carries, or
// nil when the cancellation is not a shutdown.
func ShutdownCause(ctx context.Context) error {
	switch cause := context.Cause(ctx); cause {
	case ErrShutdownForced, ErrShutdownGraceExpired:
		return cause
	}
	return nil
}

// cancelStatus is the status and error of a unit the cancelled context
// stopped: incomplete_shutdown under a shutdown cause, cancelled otherwise,
// carrying the cancel_job cause when there is one. It is applied at four
// points: a cancel before the gate, a capacity wait or an
// open the cancel interrupted, the loop top, and a command whose driver
// error arrived with the context cancelled.
func cancelStatus(ctx context.Context) (status, code string, cause error) {
	if cause := ShutdownCause(ctx); cause != nil {
		return "incomplete_shutdown", "shutdown_incomplete", errorcodes.Errorf("shutdown_incomplete", "%s", errorcodes.Message(cause))
	}
	if cause := CancelCause(ctx); cause != nil {
		return "cancelled", "cancelled", cause
	}
	return "cancelled", "cancelled", ctx.Err()
}

func (e *DeviceExecutor) Execute(ctx context.Context, task dispatch.Task, dc dispatch.Context) dispatch.Result {
	start := time.Now()
	work, ok := task.Value.(Work)
	if !ok {
		return dispatch.Result{Task: task, Success: false, ErrorCode: "internal_task_type", StartedAt: start, EndedAt: time.Now()}
	}
	t := work.Target
	d := t.Device
	seq := e.sequence(t)
	// The device's records are all appended when Execute returns, on every
	// path (a failure set, a cancel, the whole list): the store's collection
	// ends here.
	defer e.opts.Store.EndDevice(d.CanonicalName)
	e.debugf("device start target=%q canonical=%q scope_position=%d dispatch=%s wave=%d", d.Name, d.CanonicalName, task.Position, dc.Mode, dc.WaveNumber)
	if work.QueuedAt.IsZero() {
		work.QueuedAt = start
	}
	if !seq.known {
		// The plan's Validate refuses a profile name its table lacks; nothing
		// is contacted for a target that names one.
		err := fmt.Errorf("execution_target_invalid: target %s names session-init profile %q, which the plan's table does not hold", t.TargetID, seq.profile)
		e.emitFailureSet(work, dc, seq, nil, nil, "", "execution_target_invalid", "config", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "execution_target_invalid", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	if seq.invalid != nil {
		// A declaration the plan's Validate would have refused: the same
		// answer, before any connection.
		e.emitFailureSet(work, dc, seq, nil, nil, "", "execution_plan_invalid", "inventory", seq.invalid, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "execution_plan_invalid", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	selection, selectErr := e.selection(t)
	if selectErr != nil {
		code := errorcodes.Of(selectErr)
		if code == "" {
			code = "transport_unavailable"
		}
		e.emitFailureSet(work, dc, seq, nil, nil, "", code, "dependency", selectErr, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: code, External: false, StartedAt: start, EndedAt: time.Now()}
	}
	e.debugf("device transport target=%q selector=%q implementation=%q kind=%s", d.CanonicalName, selection.Selector, selection.Implementation, selection.Kind)
	address := t.AddressPlan.Selected
	if !address.IsValid() {
		err := fmt.Errorf("execution_target_invalid: target %s has no selected address", t.TargetID)
		e.emitFailureSet(work, dc, seq, nil, nil, "", "execution_target_invalid", "name_resolution", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "execution_target_invalid", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	port := e.port(t)
	e.debugf("device resolved target=%q address=%s source=%s candidates=%d authority=%s actor=%s", d.CanonicalName, address, t.AddressPlan.SelectedSource, len(t.AddressPlan.ClientCandidates)+len(t.AddressPlan.DaemonCandidates), t.AddressPlan.Authority, t.AddressPlan.ResolverContext)
	grant, ok := e.opts.Grants.ForTarget(t.TargetID)
	if !ok {
		err := fmt.Errorf("credential_resolution_failed: target %s has no credential grant", t.TargetID)
		e.emitFailureSet(work, dc, seq, nil, nil, "", "credential_resolution_failed", "credential", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "credential_resolution_failed", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	projection, err := grant.SafeProjection()
	if err != nil {
		e.emitFailureSet(work, dc, seq, nil, nil, "", "credential_material_error", "credential", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "credential_material_error", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	cred := e.credentialProjection(projection)
	e.debugf("device credential target=%q device_user=%q backend=%q policy=%q matched_category=%q matched_pattern=%q matched_source=%q credential_id=%s", d.CanonicalName, projection.DeviceUsername, projection.Backend, projection.Policy, projection.MatchedOn.Category, projection.MatchedOn.Pattern, projection.MatchedOn.Source, projection.CredentialID)
	// The ICMP gate: after the grant, so a missing
	// credential is still reported first and the records carry the
	// projection; before the capacity lease, because two probes are not a
	// session; before openReq exists, so a skipped target never has its
	// password callbacks constructed. Only the selected address is probed.
	var g *gate
	if e.opts.Ping.Enabled {
		g = e.runGate(ctx, t, address)
		if ctx.Err() != nil {
			status, code, cause := cancelStatus(ctx)
			for _, st := range seq.steps {
				e.emitNotAttempted(work, dc, cred, g, st, status, cause)
			}
			return dispatch.Result{Task: task, Success: false, ErrorCode: code, StartedAt: start, EndedAt: time.Now()}
		}
		switch g.report.Decision {
		case records.PingDecisionSkip:
			err := errorcodes.Errorf("icmp_unreachable", "ICMP gate enabled: no validated reply from %s to %d probes (%s); target skipped because ping gating is enabled, no transport opened", address, len(g.report.Outcomes), describeOutcomes(g.report.Outcomes))
			e.emitFailureSet(work, dc, seq, cred, g, records.StatusICMPUnreachable, "icmp_unreachable", "connection", err, true)
			return dispatch.Result{Task: task, Success: false, ErrorCode: "icmp_unreachable", External: true, StartedAt: start, EndedAt: time.Now()}
		case records.PingDecisionCapabilityUnavailable:
			err := errorcodes.Errorf("icmp_capability_unavailable", "ICMP gate enabled but the pinger could not run for %s (%s); target skipped, no transport opened", address, describeOutcomes(g.report.Outcomes))
			e.emitFailureSet(work, dc, seq, cred, g, records.StatusICMPCapabilityUnavailable, "icmp_capability_unavailable", "dependency", err, false)
			return dispatch.Result{Task: task, Success: false, ErrorCode: "icmp_capability_unavailable", External: false, StartedAt: start, EndedAt: time.Now()}
		}
	}
	def, known := definition(e.opts.Config, d.Platform)
	if !known {
		// The plan names a platform this configuration does not know (a
		// daemon without the client's alias table): refused before any
		// connection, never driven as generic.
		err := errorcodes.Errorf("platform_unknown", "platform %q is not a known platform here (known: %s)", d.Platform, strings.Join(platform.KnownNames(e.opts.Config.NamedTables("platform")), ", "))
		e.emitFailureSet(work, dc, seq, cred, g, "", "platform_unknown", "inventory", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "platform_unknown", External: false, StartedAt: start, EndedAt: time.Now()}
	}
	if err := devsession.Validate(def); err != nil {
		// The definition's own error, before any connection.
		code := errorcodes.Of(err)
		e.emitFailureSet(work, dc, seq, cred, g, "", code, "config", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: code, External: false, StartedAt: start, EndedAt: time.Now()}
	}
	// A native implementation admits the definition's base platform, before
	// any connection.
	if selection.Kind == transportselect.KindNative {
		if err := native.Admits(selection.Implementation, def); err != nil {
			code := errorcodes.Of(err)
			e.emitFailureSet(work, dc, seq, cred, g, "", code, "dependency", err, false)
			return dispatch.Result{Task: task, Success: false, ErrorCode: code, External: false, StartedAt: start, EndedAt: time.Now()}
		}
	}
	// The device's SSH algorithm lists from this process's configuration,
	// before any connection.
	var algorithms sshalgorithms.Lists
	if selection.Kind != transportselect.KindTelnet {
		chosen, err := e.opts.Config.SelectSSHAlgorithms(matching.Fields{Name: d.CanonicalName, Address: address, Platform: d.Platform, Site: d.Site, Groups: d.Groups})
		if err != nil {
			code := errorcodes.Of(err)
			e.emitFailureSet(work, dc, seq, cred, g, "", code, "config", err, false)
			return dispatch.Result{Task: task, Success: false, ErrorCode: code, External: false, StartedAt: start, EndedAt: time.Now()}
		}
		algorithms = chosen.Lists
		e.debugf("device ssh algorithms target=%q %s", d.CanonicalName, describeAlgorithmSelection(chosen))
	}
	cap := def.SessionCap
	if d.SessionCap != nil {
		cap = *d.SessionCap
	}
	waitStart := time.Now()
	lease, err := e.opts.Capacity.Acquire(ctx, d.CanonicalName, cap)
	capacityWait := time.Since(waitStart)
	if err != nil {
		if ctx.Err() != nil {
			return e.cancelSet(ctx, work, dc, seq, cred, g, task, start)
		}
		e.emitFailureSet(work, dc, seq, cred, g, "", "capacity_admission_failed", "capacity", err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: "capacity_admission_failed", StartedAt: start, EndedAt: time.Now()}
	}
	defer lease.Release()
	e.debugf("device capacity admitted target=%q session_cap=%d wait=%s", d.CanonicalName, cap, capacityWait)
	if e.opts.Metrics != nil {
		e.opts.Metrics.AddStage("capacity_wait", capacityWait)
	}
	openReq := platform.OpenRequest{Address: address.String(), Port: port, Username: projection.DeviceUsername, Definition: def, Timeout: e.opts.Config.Duration("ssh.connect-timeout"), Metadata: map[string]string{"canonical_name": d.CanonicalName, "device_id": d.ID, "activity_type": e.opts.ActivityType, "transport_selector": selection.Selector, "display_prompt": inferredPrompt(d.CanonicalName, d.Name, def)}}
	if grant.Password.IsSet() {
		openReq.Password = grant.Password.WithBytes
	}
	if grant.EnablePassword.IsSet() {
		openReq.EnablePassword = grant.EnablePassword.WithBytes
	}
	if e.opts.InFlight != nil {
		openReq.InFlightBytes = e.opts.InFlight.Counter(d.CanonicalName)
	}
	factory := e.factory(selection, algorithms)
	connectStart := time.Now()
	e.debugf("device transport opening target=%q address=%s port=%d platform=%q", d.CanonicalName, address, port, def.Name)
	driver, err := factory.Open(ctx, openReq)
	if err == nil {
		err = driver.Prepare(ctx)
	}
	connectDur := time.Since(connectStart)
	if e.opts.Metrics != nil {
		e.opts.Metrics.AddStage("connect", connectDur)
	}
	// The set-up the session sent (enable, the paging commands) has no
	// record; the store shows it in the device's output.TARGET.txt ahead of
	// the first record's block. It is handed over before any record of the
	// device exists, and after a failed Prepare too, so that a failed enable
	// is seen up to the statement that failed.
	if reporter, ok := driver.(platform.SetupReporter); ok {
		e.opts.Store.SetTextSetup(d.CanonicalName, reporter.SetupLines())
	}
	if err != nil {
		if driver != nil {
			driver.Close()
		}
		if ctx.Err() != nil {
			return e.cancelSet(ctx, work, dc, seq, cred, g, task, start)
		}
		code, category := classifyOpen(err)
		e.emitFailureSet(work, dc, seq, cred, g, openStatus(code), code, category, err, false)
		return dispatch.Result{Task: task, Success: false, ErrorCode: code, External: true, StartedAt: start, EndedAt: time.Now()}
	}
	defer driver.Close()
	e.debugf("device transport ready target=%q connect_duration=%s", d.CanonicalName, connectDur)
	// execution.device-timeout bounds the device's whole command list, the
	// session-init profile included, from the prepared session; the gate,
	// the capacity wait, and the open have their own bounds. Zero is
	// unbounded.
	var deviceDeadline time.Time
	deviceTimeout := e.opts.Config.Duration("execution.device-timeout")
	if deviceTimeout > 0 {
		deviceDeadline = time.Now().Add(deviceTimeout)
	}
	deviceExpired := func() bool { return !deviceDeadline.IsZero() && !time.Now().Before(deviceDeadline) }
	allSuccess := true
	// failureCode is the device's first counted failure, never replaced by a
	// later failure, cancel, or shutdown code. A profile command that fails
	// under continue with the session
	// usable does not count: it becomes a notice on requested record 1.
	failureCode := ""
	fail := func(code string) {
		allSuccess = false
		if failureCode == "" {
			failureCode = code
		}
	}
	var profileNotices []records.Notice
	// The profile's duration, the first session-init command's start to the
	// last sent one's end, is on the requested records; null when none was
	// sent.
	var initStart, initEnd time.Time
	initNS := func() *int64 {
		if initStart.IsZero() {
			return nil
		}
		ns := initEnd.Sub(initStart).Nanoseconds()
		return &ns
	}
	// finish completes a record the loop writes: the plan target's
	// planning notices (a not-set or fallen-back platform) and the
	// packet-loss notice on the device's first record, whatever that
	// record's kind or status, and on the requested records the profile's
	// duration, with the profile's failure notices on requested record 1.
	finish := func(r *records.CommandRecord, pos int) {
		if pos == 0 {
			r.Notices = append(r.Notices, targetNotices(work.Target)...)
		}
		if pos == 0 && g != nil && g.report.Decision == records.PingDecisionProceedDegraded {
			r.Notices = append(r.Notices, records.Notice{Code: "icmp_packet_loss", Message: "ICMP packet loss 50%; proceeding because one validated reply was received", Details: map[string]any{"replies": g.report.Replies, "outcomes": describeOutcomes(g.report.Outcomes)}})
		}
		if r.CommandKind == kindRequested {
			r.Timing.SessionInitNS = initNS()
			if r.CommandIndex == 1 {
				r.Notices = append(r.Notices, profileNotices...)
			}
		}
	}
	notAttempted := func(pos int, status string, cause error) {
		r := e.notAttemptedRecord(work, dc, cred, g, seq.steps[pos], status, cause)
		finish(&r, pos)
		e.appendRecord(r)
	}
	// skipRest records the steps after i as not attempted: the requested
	// commands after a failed profile command as the profile's failure, every
	// other as the prior command's.
	skipRest := func(i int) {
		for j := i + 1; j < len(seq.steps); j++ {
			status := "not_attempted_prior_command_failure"
			if seq.steps[i].kind == kindSessionInit && seq.steps[j].kind == kindRequested {
				status = "not_attempted_session_init_failure"
			}
			notAttempted(j, status, nil)
		}
	}
	for i, st := range seq.steps {
		if ctx.Err() != nil {
			status, code, cause := cancelStatus(ctx)
			notAttempted(i, status, cause)
			fail(code)
			continue
		}
		if deviceExpired() {
			// The deadline passed between two commands: this one is not sent
			// and carries the code; the shell is in step, so the close is the
			// ordinary one.
			r := e.record(work, dc, cred, g, st, "timeout", nil, nil, nil, "", "", nil, 0, 0, time.Time{}, time.Now())
			r.Error = &records.StructuredError{Code: "device_timeout", Category: "timeout", Message: fmt.Sprintf("the device's command list did not complete within execution.device-timeout (%s); this command was not sent", deviceTimeout), Operation: st.operation(), Retryable: true, External: true}
			r.RetryEligible = true
			finish(&r, i)
			e.appendRecord(r)
			e.debugf("device timeout target=%q%s index=%d sent=false remaining=%d", d.CanonicalName, st.debugKind(), st.index+1, len(seq.steps)-i-1)
			fail("device_timeout")
			skipRest(i)
			break
		}
		cmdStart := time.Now()
		if st.kind == kindSessionInit && initStart.IsZero() {
			initStart = cmdStart
		}
		commandHash := sha256.Sum256([]byte(st.command))
		e.debugf("device command start target=%q%s index=%d total=%d sha256=%s bytes=%d command=%s blind=%t blind_returns=%d expectations=%s", d.CanonicalName, st.debugKind(), st.index+1, st.count, hex.EncodeToString(commandHash[:8]), len(st.command), debugCommandText(st.command), st.blind, st.returns, debugExpectations(st.expect))
		execCtx, cancelExec := ctx, context.CancelFunc(func() {})
		if !deviceDeadline.IsZero() {
			execCtx, cancelExec = context.WithDeadline(ctx, deviceDeadline)
		}
		r := driver.Execute(execCtx, platform.Command{Text: st.command, Timeout: st.timeout, Blind: st.blind, BlindReturns: st.returns, Expectations: st.expect})
		cancelExec()
		if r.Spool != nil {
			e.debugf("device command spooled target=%q%s index=%d path=%s bytes=%d", d.CanonicalName, st.debugKind(), st.index+1, r.Spool.Path, r.Spool.Bytes)
		}
		if r.EndedAt.IsZero() {
			r.EndedAt = time.Now()
		}
		if st.kind == kindSessionInit {
			initEnd = r.EndedAt
		}
		responseDur := r.EndedAt.Sub(r.StartedAt)
		if e.opts.Metrics != nil {
			e.opts.Metrics.AddStage("device_response", responseDur)
		}
		status := statusFor(r)
		// A command the shutdown or the cancel interrupted has an unknown
		// outcome: never succeeded, never errored.
		var cancelErr error
		if r.Err != nil && ctx.Err() != nil {
			var code string
			status, code, cancelErr = cancelStatus(ctx)
			r.ErrorCode, r.ErrorCategory, r.Retryable, r.External, r.Err = code, "shutdown", false, false, errors.New(bareMessage(cancelErr))
		}
		// A command the device deadline cut is the device's timeout, whatever
		// the transport made of its ended context; a command that reached its
		// own timeout or the output limit first keeps that code.
		deviceCut := cancelErr == nil && r.Err != nil && !r.DeviceError && deviceExpired() && r.ErrorCode != "command_timeout" && r.ErrorCode != "output_limit_exceeded"
		if deviceCut {
			status = "timeout"
			r.ErrorCode, r.ErrorCategory, r.Retryable, r.External = "device_timeout", "timeout", true, true
			r.Err = fmt.Errorf("the device's command list did not complete within execution.device-timeout (%s); this command was interrupted and the session is closed", deviceTimeout)
		}
		record := e.record(work, dc, cred, g, st, status, r.Output, r.Spool, r.ConnectionReused, r.Prompt, r.PromptSource, r.PromptObserved, connectDur, capacityWait, cmdStart, r.EndedAt)
		// Only a statement that was sent has a prompt before it; the records
		// of statements not sent leave it empty.
		record.PromptBefore = r.PromptBefore
		// The record holds the output as its string from here on, or the
		// source names its spool. The bytes are let go before the record
		// waits its turn at the store: at width every device's response
		// arrives at about the same time and the store writes one line at a
		// time, so what a waiting device holds is what the daemon holds.
		outputBytes, src := len(r.Output), output.FromRecord(&record)
		if r.Spool != nil {
			outputBytes, src = int(r.Spool.Bytes), output.FromSpool(r.Spool.Path, r.Spool.Bytes, r.Spool.SHA256, record.OutputEncoding)
		}
		r.Output = nil
		if r.Err != nil {
			record.Error = &records.StructuredError{Code: defaultString(r.ErrorCode, "command_failed"), Category: defaultString(r.ErrorCategory, "device"), Message: errorcodes.Message(r.Err), Operation: st.operation(), Retryable: r.Retryable, External: r.External}
			record.RetryEligible = r.Retryable
		}
		for _, n := range r.Notices {
			record.Notices = append(record.Notices, records.Notice{Code: n.Code, Message: n.Message, Details: n.Details})
		}
		finish(&record, i)
		_, appendErr := e.opts.Store.AppendRecord(&record, src)
		if appendErr == nil {
			e.afterRecord(record, src)
		}
		// The one removal of the command's spool: after the append, whose
		// every file consumer has read it, and after afterRecord, where the
		// display and the follower take it; on the path where no record could
		// be appended as well. A cut ending
		// reaches here through the same result, so a cancel or a signal
		// needs no removal of its own.
		e.removeSpool(r.Spool, d.CanonicalName, st.index+1)
		if appendErr != nil {
			code := errorcodes.Of(appendErr)
			if code == "" {
				code = "output_write_failed"
			}
			return dispatch.Result{Task: task, Success: false, ErrorCode: code, StartedAt: start, EndedAt: time.Now()}
		}
		errorCode := ""
		if record.Error != nil {
			errorCode = record.Error.Code
		}
		e.debugf("device command complete target=%q%s index=%d status=%s error_code=%q output_bytes=%d elapsed=%s prompt_source=%q", d.CanonicalName, st.debugKind(), st.index+1, status, errorCode, outputBytes, responseDur, r.PromptSource)
		if status == "succeeded" {
			if driver.Usable() || i == len(seq.steps)-1 {
				continue
			}
			// A blind send whose prompt did not return ended the session: the
			// rest cannot be sent, and the
			// device fails as a lost session.
			e.debugf("device session ended after a blind send target=%q index=%d remaining=%d", d.CanonicalName, st.index+1, len(seq.steps)-i-1)
			fail("command_session_lost")
			cause := errorcodes.Errorf("command_session_lost", "the prompt did not return after the blind send of command %d; the session was closed and this command was not sent", st.index+1)
			for j := i + 1; j < len(seq.steps); j++ {
				notAttempted(j, "not_attempted_prior_command_failure", cause)
			}
			break
		}
		errorCode = defaultString(errorCode, "command_failed")
		if cancelErr != nil {
			fail(errorCode)
			for j := i + 1; j < len(seq.steps); j++ {
				notAttempted(j, status, cancelErr)
			}
			break
		}
		usable := driver.Usable() && !deviceCut
		remaining := len(seq.steps) - i - 1
		if st.kind == kindSessionInit {
			// A profile command's failure stops the profile under fail-device
			// and when it ended the session; under continue with the session
			// usable the rest is sent and the failure is a notice on requested
			// record 1.
			if seq.onError == executionplan.SessionInitContinue && usable {
				profileNotices = append(profileNotices, records.Notice{Code: "session_init_command_failed", Message: fmt.Sprintf("session-init profile %s command %d/%d failed (%s); continuing because on-error is continue", seq.profile, st.index+1, st.count, errorCode), Details: map[string]any{"profile": seq.profile, "command_index": st.index + 1, "code": errorCode}})
				continue
			}
			fail(errorCode)
			if !usable && remaining > 0 {
				e.debugf("device session ended target=%q kind=%s index=%d remaining=%d", d.CanonicalName, kindSessionInit, st.index+1, remaining)
			}
			skipRest(i)
			break
		}
		fail(errorCode)
		// A session the failure ended takes nothing more under every
		// setting; a usable one continues unless the device halts on a
		// command error.
		if !usable || e.opts.HaltOnCommandError {
			if !usable && remaining > 0 {
				e.debugf("device session ended target=%q index=%d remaining=%d", d.CanonicalName, st.index+1, remaining)
			}
			skipRest(i)
			break
		}
	}
	return dispatch.Result{Task: task, Success: allSuccess, ErrorCode: failureCode, External: !allSuccess, StartedAt: start, EndedAt: time.Now()}
}

// EmitUnstarted persists the immutable command-plan entries for a device that
// dispatch deliberately did not start, the session-init profile's first.
// This keeps command cardinality
// truthful after a run-wide halt, wave gate, cancellation, or shutdown. A
// cancel_job cause supplies the message, reason included; otherwise the
// message is the reason code.
func (e *DeviceExecutor) EmitUnstarted(work Work, dc dispatch.Context, status, reason string, cause error) {
	message := reason
	if cause != nil {
		message = bareMessage(cause)
	}
	for pos, st := range e.sequence(work.Target).steps {
		r := e.record(work, dc, nil, nil, st, status, nil, nil, nil, "", "", nil, 0, 0, time.Time{}, time.Now())
		r.Error = &records.StructuredError{Code: reason, Category: "shutdown", Message: message, Operation: "dispatch", Retryable: false, External: false}
		if pos == 0 {
			r.Notices = append(r.Notices, targetNotices(work.Target)...) // the device's first record
		}
		e.appendRecord(r)
	}
	// A device the halt or the shutdown stopped before contact: its records
	// are the not-started ones above, and its collection is kept.
	e.opts.Store.EndDevice(work.Target.Device.CanonicalName)
}

type factory interface {
	Open(context.Context, platform.OpenRequest) (platform.Driver, error)
}

func (e *DeviceExecutor) factory(selection transportselect.Selection, algorithms sshalgorithms.Lists) factory {
	switch selection.Kind {
	case transportselect.KindSystem:
		return systemssh.Factory{Binary: selection.Binary, Config: e.opts.Config, ScratchDir: e.opts.ScratchDir, ControlRoot: e.opts.ControlRoot, Home: e.opts.Home, BaseDir: e.opts.BaseDir, AskpassPath: e.opts.AskpassPath, MaxOutputBytes: e.opts.Config.Int64("output.max-command-bytes"), Spool: e.spool(), Warn: e.opts.Warn, Debug: e.opts.Debug, Algorithms: algorithms}
	case transportselect.KindTelnet:
		return telnettransport.Factory{Config: e.opts.Config, MaxOutputBytes: e.opts.Config.Int64("output.max-command-bytes")}
	default:
		return native.Factory{Implementation: selection.Implementation, Config: e.opts.Config, Home: e.opts.Home, BaseDir: e.opts.BaseDir, MaxOutputBytes: e.opts.Config.Int64("output.max-command-bytes"), Spool: e.spool(), Warn: e.opts.Warn, Debug: e.opts.Debug, Algorithms: algorithms}
	}
}

// describeAlgorithmSelection is the debug line's profile, rule, and lists.
func describeAlgorithmSelection(s sshalgorithms.Selection) string {
	if s.Rule < 0 {
		return "profile=global " + s.Lists.Describe()
	}
	return fmt.Sprintf("profile=%s rule=ssh-algorithms-map.%d %s", s.Profile, s.Rule, s.Lists.Describe())
}

// selection resolves the transport the plan's target names: the slot the
// planner resolved the kind from, else the kind itself, under the daemon's
// own configuration.
func (e *DeviceExecutor) selection(t executionplan.ExecutionTarget) (transportselect.Selection, error) {
	selector := t.Device.TransportSelector
	if selector == "" {
		selector = t.Device.Transport
	}
	return transportselect.Resolve(e.opts.Config, e.opts.ActivityType, selector)
}

// port is the projection's effective port, which the planner fixed; the
// kind default covers a projection that omits it.
func (e *DeviceExecutor) port(t executionplan.ExecutionTarget) uint16 {
	if t.Device.Port != 0 {
		return t.Device.Port
	}
	if t.Device.Transport == transportselect.KindTelnet {
		return 23
	}
	return 22
}

func (e *DeviceExecutor) credentialProjection(p credentialpackage.GrantProjection) *records.CredentialProjection {
	matched := map[string]any{"category": p.MatchedOn.Category, "safe_value": p.MatchedOn.SafeValue, "pattern": p.MatchedOn.Pattern, "source": p.MatchedOn.Source, "line": p.MatchedOn.Line}
	// credkey is a credential CSV row's key or generated label: additive,
	// and present only for a backend
	// that sets it. The five keys above are written whatever they hold, as
	// they always were; a consumer may index them.
	if p.MatchedOn.CredKey != "" {
		matched["credkey"] = p.MatchedOn.CredKey
	}
	return &records.CredentialProjection{Policy: p.Policy, DeviceUsername: p.DeviceUsername, Backend: p.Backend, MatchedOn: matched, CredentialID: p.CredentialID, Protection: e.opts.Protection}
}

// emitFailureSet persists a device that failed before its first command:
// requested command 1 carries firstStatus (derived from the category when
// empty) and the error; every other record, the session-init profile's
// included, is not attempted without an error object. A gate result, when
// present, rides on every record, and
// the plan target's planning notices on the device's first record, whatever
// its kind or status.
func (e *DeviceExecutor) emitFailureSet(work Work, dc dispatch.Context, seq deviceSequence, cred *records.CredentialProjection, g *gate, firstStatus, code, category string, err error, external bool) {
	for pos, st := range seq.steps {
		status := "not_attempted_prior_command_failure"
		var recErr *records.StructuredError
		if st.kind == kindRequested && st.index == 0 {
			status = firstStatus
			if status == "" {
				status = "connection_error"
				if category == "credential" {
					status = "authentication_error"
				}
			}
			operation := category
			retryable := false
			// The record's error carries the code in its own field; the
			// message is the bare text so the renderer's "error=<code>:
			// <message>" names the code once.
			message := strings.TrimPrefix(errorcodes.Message(err), code+": ")
			if g != nil && g.report.Decision != records.PingDecisionProceed && g.report.Decision != records.PingDecisionProceedDegraded {
				operation = "icmp_gate"
				if entry, ok := errorcodes.Lookup(code); ok {
					retryable = entry.Retryable
				}
			}
			recErr = &records.StructuredError{Code: code, Category: category, Message: message, Operation: operation, Retryable: retryable, External: external}
		}
		r := e.record(work, dc, cred, g, st, status, nil, nil, nil, "", "", nil, 0, 0, time.Time{}, time.Now())
		r.Error = recErr
		if pos == 0 {
			r.Notices = append(r.Notices, targetNotices(work.Target)...)
		}
		e.appendRecord(r)
	}
}

// emitNotAttempted persists one command that was not sent.
func (e *DeviceExecutor) emitNotAttempted(work Work, dc dispatch.Context, cred *records.CredentialProjection, g *gate, st step, status string, cause error) {
	e.appendRecord(e.notAttemptedRecord(work, dc, cred, g, st, status, cause))
}

// notAttemptedRecord is a command that was not sent: a cancelled or
// shutdown one carries its error object, a not-attempted one none.
func (e *DeviceExecutor) notAttemptedRecord(work Work, dc dispatch.Context, cred *records.CredentialProjection, g *gate, st step, status string, cause error) records.CommandRecord {
	r := e.record(work, dc, cred, g, st, status, nil, nil, nil, "", "", nil, 0, 0, time.Time{}, time.Now())
	switch status {
	case "cancelled":
		r.Error = &records.StructuredError{Code: "cancelled", Category: "shutdown", Message: bareMessage(cause), Operation: st.operation()}
	case "incomplete_shutdown":
		r.Error = &records.StructuredError{Code: "shutdown_incomplete", Category: "shutdown", Message: errorcodes.Message(cause), Operation: st.operation()}
	case "not_attempted_prior_command_failure":
		// Only after a blind send that ended the session: the record names
		// it (every other not-attempted record carries no error).
		if cause != nil {
			code := errorcodes.Of(cause)
			entry, _ := errorcodes.Lookup(code)
			r.Error = &records.StructuredError{Code: code, Category: entry.Category, Message: errorcodes.Message(cause), Operation: st.operation(), Retryable: entry.Retryable, External: true}
			r.RetryEligible = entry.Retryable
		}
	}
	return r
}

// appendRecord writes a record the device's result does not depend on; a
// store failure surfaces through the store's own error.
func (e *DeviceExecutor) appendRecord(r records.CommandRecord) {
	if _, err := e.opts.Store.AppendRecord(&r, output.FromRecord(&r)); err == nil {
		e.afterRecord(r, output.FromRecord(&r))
	}
}

// removeSpool removes a command's spool once nothing reads it (decision
// 4.4); a result without one needs nothing.
func (e *DeviceExecutor) removeSpool(sp *platform.Spool, target string, index int) {
	if sp == nil {
		return
	}
	if err := os.Remove(sp.Path); err != nil && !os.IsNotExist(err) {
		e.debugf("device command spool not removed target=%q index=%d path=%s error=%v", target, index, sp.Path, err)
		return
	}
	e.debugf("device command spool removed target=%q index=%d path=%s", target, index, sp.Path)
}

// cancelSet persists a device the cancelled context stopped before its
// first command was sent (a capacity wait or an open the cancel
// interrupted): every command, the profile's included, is
// incomplete_shutdown under a shutdown cause and cancelled otherwise.
func (e *DeviceExecutor) cancelSet(ctx context.Context, work Work, dc dispatch.Context, seq deviceSequence, cred *records.CredentialProjection, g *gate, task dispatch.Task, start time.Time) dispatch.Result {
	status, code, cause := cancelStatus(ctx)
	for _, st := range seq.steps {
		e.emitNotAttempted(work, dc, cred, g, st, status, cause)
	}
	return dispatch.Result{Task: task, Success: false, ErrorCode: code, StartedAt: start, EndedAt: time.Now()}
}

// The command kinds of a record.
const (
	kindRequested   = "requested"
	kindSessionInit = "session_init"
)

// step is one command of a device's sequence: the session-init profile's
// commands, then the requested ones, each numbered within its kind.
type step struct {
	kind    string
	profile string // the plan's profile name or none, on every record
	index   int    // zero-based within the kind
	count   int
	command string
	timeout time.Duration
	// blind is the tolerance: the prompt may not return, timeout is the
	// blind wait, and its absence is a success with the notice. returns is
	// the count of carriage returns sent after the command without a prompt
	// match, always with blind. expect is the command's compiled
	// expect-and-send declarations.
	blind   bool
	returns int
	expect  []platform.Expectation
}

// operation is the error operation of a command that was sent or stopped.
func (s step) operation() string {
	if s.kind == kindSessionInit {
		return "session_init"
	}
	return "execute_command"
}

// debugKind marks a profile command on the device command lines.
func (s step) debugKind() string {
	if s.kind == kindSessionInit {
		return " kind=" + kindSessionInit
	}
	return ""
}

// deviceSequence is a target's commands in the order they are sent, with
// the profile's name and on-error; known is false for a name the plan's
// table does not hold, whose sequence is the requested commands alone.
type deviceSequence struct {
	profile string
	onError string
	known   bool
	steps   []step
	// invalid is set when a requested command's declaration does not
	// compile: nothing is contacted (execution_plan_invalid).
	invalid error
}

// sequence is the target's session-init profile from the plan's table
// followed by the requested commands:
// each profile command's timeout is the profile's, else
// execution.command-timeout.
func (e *DeviceExecutor) sequence(t executionplan.ExecutionTarget) deviceSequence {
	seq := deviceSequence{profile: t.SessionInitProfile, known: true}
	if seq.profile == "" {
		seq.profile = executionplan.SessionInitNone
	}
	commandTimeout := e.opts.Config.Duration("execution.command-timeout")
	if seq.profile != executionplan.SessionInitNone {
		prof, ok := e.opts.SessionInit[seq.profile]
		seq.known = ok
		seq.onError = prof.OnError
		timeout := commandTimeout
		if prof.CommandTimeoutNS > 0 {
			timeout = time.Duration(prof.CommandTimeoutNS)
		}
		for i, c := range prof.Commands {
			seq.steps = append(seq.steps, step{kind: kindSessionInit, profile: seq.profile, index: i, count: len(prof.Commands), command: c, timeout: timeout})
		}
	}
	// The device's list: its platform's when the plan carries lists, else
	// the plan's commands. The blind and
	// expect declarations belong to the plan's commands alone.
	commands, own := e.opts.Commands, true
	if e.opts.PlatformCommands != nil {
		if list, ok := e.opts.PlatformCommands[t.Device.Platform]; ok {
			commands, own = list, false
		}
	}
	for i, c := range commands {
		st := step{kind: kindRequested, profile: seq.profile, index: i, count: len(commands), command: c, timeout: commandTimeout}
		if own && i < len(e.opts.BlindReturns) {
			st.returns = e.opts.BlindReturns[i]
		}
		// The flag, or the count that implies it, makes the command blind
		// and its wait the blind wait; session-init commands are never blind
		// and carry no declaration.
		if st.returns > 0 || (own && i < len(e.opts.Blind) && e.opts.Blind[i]) {
			st.blind, st.timeout = true, e.opts.BlindWait
		}
		if own && i < len(e.opts.Expectations) {
			for j, d := range e.opts.Expectations[i] {
				re, err := regexp.Compile(d.Pattern)
				if err != nil {
					seq.invalid = fmt.Errorf("execution_plan_invalid: expectations: command %d declaration %d: pattern %q: %v", i+1, j+1, d.Pattern, err)
					break
				}
				st.expect = append(st.expect, platform.Expectation{Pattern: re, Response: d.Response})
			}
		}
		seq.steps = append(seq.steps, st)
	}
	return seq
}

// spool is the session's spool for this activity:
// the device is the transport's to fill.
func (e *DeviceExecutor) spool() devsession.Spool {
	return devsession.Spool{Dir: e.opts.SpoolDir, Threshold: e.opts.Config.Int64("output.spool-threshold-bytes"), Activity: e.opts.ActivityID}
}

// debugExpectations renders a step's declarations for the device command
// start event: each pattern and its response's byte count, never the
// response, which is device text the operator may not want in a log
// (the secrets rule).
func debugExpectations(list []platform.Expectation) string {
	if len(list) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(list))
	for _, d := range list {
		parts = append(parts, fmt.Sprintf("{pattern:%q response_bytes:%d}", d.Pattern.String(), len(d.Response)))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// record builds the command record from the plan's target: the address
// fields from the address plan, the credential
// projection from the grant, and null DNS and credential timings because
// both happened at planning. The output is raw when it stayed in memory
// (the record's string, encoded here) or spool when it passed the
// threshold (the record's Output is empty, its count, digest, and encoding
// the reader's; the line is written from the file).
func (e *DeviceExecutor) record(work Work, dc dispatch.Context, cred *records.CredentialProjection, g *gate, st step, status string, raw []byte, spool *platform.Spool, reused *bool, prompt, promptSource string, promptObserved *bool, connectDur, capacityWait time.Duration, commandStart, end time.Time) records.CommandRecord {
	id, _ := osutil.NewID(time.Now())
	t := work.Target
	d := t.Device
	a := t.AddressPlan
	out, enc, outHash := output.EncodeOutput(raw)
	outputBytes := int64(len(raw))
	if spool != nil {
		out, enc, outHash, outputBytes = "", "utf-8", spool.SHA256, spool.Bytes
		if !spool.UTF8 {
			enc = "base64"
		}
	}
	cmdHash := sha256.Sum256([]byte(st.command))
	devStart := work.QueuedAt
	if commandStart.IsZero() {
		commandStart = end
	}
	total := end.Sub(work.QueuedAt).Nanoseconds()
	sched := devStart.Sub(work.QueuedAt).Nanoseconds()
	connNS := connectDur.Nanoseconds()
	capNS := capacityWait.Nanoseconds()
	respNS := end.Sub(commandStart).Nanoseconds()
	candidates := append(addrStrings(a.ClientCandidates), addrStrings(a.DaemonCandidates)...)
	r := records.CommandRecord{SchemaVersion: records.CommandSchemaVersion, RecordID: id, JobID: e.opts.JobID, ActivityID: e.opts.ActivityID, ActivityType: e.opts.ActivityType, Operator: osutil.RecordOperator(e.opts.Operator),
		Device:      records.DeviceProjection{ID: d.ID, Name: d.Name, CanonicalName: d.CanonicalName, Site: d.Site, Groups: append([]string{}, d.Groups...), RiskTier: d.RiskTier, DeploymentRing: d.DeploymentRing, TopologyDomain: d.TopologyDomain},
		InputTarget: t.InputTarget, TransformedName: a.TransformedName, DNSQueryName: a.QueryName, DNSSuffixAction: a.SuffixAction,
		AddressCandidates: candidates, SelectedAddress: addrString(a.Selected), AddressFamily: familyOf(a.Selected), AddressSource: a.SelectedSource,
		AddressAuthority: string(a.Authority), ClientAddressCandidates: addrStrings(a.ClientCandidates), DaemonAddressCandidates: addrStrings(a.DaemonCandidates), AlternateAddresses: addrStrings(a.Alternates), AddressResolutionActor: a.ResolverContext,
		Platform: d.Platform, Transport: d.Transport, Port: e.port(t), ConnectionReused: reused, SessionInitProfile: st.profile,
		Dispatch:     records.DispatchContext{Mode: dc.Mode, ServerID: t.ExecutionEndpoint, WaveNumber: dc.WaveNumber, WaveWidth: dc.WaveWidth, WaveDepth: dc.WaveDepth, WorkerID: dc.WorkerID, ScopePosition: dc.ScopePosition, DesiredWidth: dc.DesiredWidth, EffectiveInflight: dc.EffectiveInflight},
		CommandIndex: st.index + 1, CommandCount: st.count, CommandKind: st.kind, Command: st.command, CommandSHA256: hex.EncodeToString(cmdHash[:]), Status: status, Output: out, OutputEncoding: enc, OutputBytes: outputBytes, OutputSHA256: outHash, Prompt: prompt, PromptSource: promptSource, PromptObserved: promptObserved, Notices: []records.Notice{},
		Timing:        records.Timing{QueuedAt: work.QueuedAt, DeviceStartedAt: &devStart, CommandStartedAt: &commandStart, EndedAt: end, TotalNS: total, SchedulerWaitNS: &sched, ServerCapacityWaitNS: &capNS, ConnectNS: &connNS, DeviceResponseNS: &respNS},
		RetryEligible: false}
	r.CandidateCount = e.opts.CandidateCount
	r.DispatchOrder, r.ShuffleKey = e.opts.DispatchOrder, e.opts.ShuffleKey
	r.Credential = cred
	if g != nil {
		r.Ping, r.Timing.PingNS = g.report, g.ns
	}
	return r
}

// runGate sends the two probes to the selected address and records the
// outcome. A pinger error is a capability
// failure for this device alone; the job-level check happened at start.
func (e *DeviceExecutor) runGate(ctx context.Context, t executionplan.ExecutionTarget, address netip.Addr) *gate {
	timeout := time.Duration(e.opts.Ping.TimeoutNS)
	report := &records.PingReport{Address: address.String(), Family: icmpgate.Family(address), Method: icmpgate.MethodUnavailable, ExecutionEndpoint: t.ExecutionEndpoint, Probes: e.opts.Ping.Probes, TimeoutNS: e.opts.Ping.TimeoutNS, Outcomes: []records.PingOutcome{}}
	started := time.Now()
	var outcomes []icmpgate.Outcome
	var err error
	if e.opts.Pinger == nil {
		err = errorcodes.Errorf("icmp_capability_unavailable", "no pinger was detected for this job")
	} else {
		report.Method = e.opts.Pinger.Method()
		outcomes, err = e.opts.Pinger.Probe(ctx, address, e.opts.Ping.Probes, timeout)
	}
	total := time.Since(started)
	for len(outcomes) < e.opts.Ping.Probes {
		detail := "not sent"
		if err != nil {
			detail = err.Error()
		}
		outcomes = append(outcomes, icmpgate.Outcome{Sequence: len(outcomes) + 1, SentAt: started, Status: icmpgate.StatusError, Detail: detail})
	}
	for _, o := range outcomes[:e.opts.Ping.Probes] {
		report.Outcomes = append(report.Outcomes, records.PingOutcome{Sequence: o.Sequence, SentAt: o.SentAt, Status: o.Status, RTTNS: o.RTTNS, From: o.From, Detail: o.Detail})
		e.debugf("device ping target=%q address=%s method=%s seq=%d status=%s rtt=%s from=%q detail=%q", t.Device.CanonicalName, address, report.Method, o.Sequence, o.Status, rttString(o.RTTNS), o.From, o.Detail)
	}
	report.Replies = icmpgate.Replies(outcomes)
	report.Losses = e.opts.Ping.Probes - report.Replies
	report.TotalNS = total.Nanoseconds()
	switch {
	case err != nil:
		report.Decision = records.PingDecisionCapabilityUnavailable
	case report.Replies == e.opts.Ping.Probes:
		report.Decision = records.PingDecisionProceed
	case report.Replies > 0:
		report.Decision = records.PingDecisionProceedDegraded
	default:
		report.Decision = records.PingDecisionSkip
	}
	e.debugf("device ping gate target=%q address=%s replies=%d losses=%d decision=%s duration=%s", t.Device.CanonicalName, address, report.Replies, report.Losses, report.Decision, total)
	if e.opts.Metrics != nil {
		e.opts.Metrics.AddStage("ping", total)
	}
	e.ping.mu.Lock()
	st := &e.ping.summary
	if st.Devices == nil {
		st.Devices = map[string]int{}
	}
	st.Devices[records.PingDevicesGated]++
	switch report.Decision {
	case records.PingDecisionProceed:
		st.Devices[records.PingDevicesProceeded]++
	case records.PingDecisionProceedDegraded:
		st.Devices[records.PingDevicesDegraded]++
	case records.PingDecisionSkip:
		st.Devices[records.PingDevicesSkipped]++
	default:
		st.Devices[records.PingDevicesCapabilityFailed]++
	}
	if err == nil {
		st.ProbesSent += len(report.Outcomes)
	}
	st.Replies += report.Replies
	for _, o := range report.Outcomes {
		switch o.Status {
		case icmpgate.StatusTimeout:
			st.Timeouts++
		case icmpgate.StatusError:
			st.Errors++
		}
	}
	st.TotalNS += report.TotalNS
	e.ping.mu.Unlock()
	ns := report.TotalNS
	return &gate{report: report, ns: &ns}
}

// describeOutcomes renders the probes for a message: "1: 2.1ms; 2: timeout".
func describeOutcomes(outcomes []records.PingOutcome) string {
	parts := make([]string, 0, len(outcomes))
	for _, o := range outcomes {
		switch o.Status {
		case icmpgate.StatusReply:
			parts = append(parts, fmt.Sprintf("%d: %s", o.Sequence, rttString(o.RTTNS)))
		case icmpgate.StatusError:
			if o.From != "" {
				parts = append(parts, fmt.Sprintf("%d: error %s from %s", o.Sequence, o.Detail, o.From))
			} else {
				parts = append(parts, fmt.Sprintf("%d: error %s", o.Sequence, o.Detail))
			}
		default:
			parts = append(parts, fmt.Sprintf("%d: timeout", o.Sequence))
		}
	}
	return strings.Join(parts, "; ")
}

func rttString(ns *int64) string {
	if ns == nil {
		return "-"
	}
	ms := float64(*ns) / float64(time.Millisecond)
	if ms < 0.1 {
		return "<0.1ms"
	}
	return fmt.Sprintf("%.1fms", ms)
}

// afterRecord is what follows an appended record: the display and the
// audit. The record's notice is not among them: the store gives it to the
// followers itself, in sequence (output.Options.OnDurable).
func (e *DeviceExecutor) afterRecord(r records.CommandRecord, src output.Source) {
	if e.opts.OnRecord != nil {
		e.opts.OnRecord(r, src)
	}
	if e.opts.Audit != nil {
		_ = e.opts.Audit.WriteAudit(records.AuditRecord{SchemaVersion: 1, EventID: r.RecordID, EventName: "command_completed", Timestamp: r.Timing.EndedAt, Outcome: r.Status, Severity: severity(r.Status), Operator: r.Operator, Process: map[string]any{"pid": 0}, ActivityID: r.ActivityID, JobID: r.JobID, Device: map[string]any{"id": r.Device.ID, "canonical_name": r.Device.CanonicalName, "platform": r.Platform}, DeviceIdentity: map[string]any{"selected_address": r.SelectedAddress}, Action: map[string]any{"command_sha256": r.CommandSHA256, "command_index": r.CommandIndex, "command_count": r.CommandCount, "command_kind": r.CommandKind}, Policy: map[string]any{"credential_policy": func() string {
			if r.Credential != nil {
				return r.Credential.Policy
			}
			return ""
		}(), "transport": r.Transport, "ssh_host_key_policy": e.opts.Config.String("ssh.host-key-policy")}, Result: map[string]any{"status": r.Status, "error_code": func() string {
			if r.Error != nil {
				return r.Error.Code
			}
			return ""
		}()}, Source: map[string]any{}, Details: map[string]any{}})
	}
}

func inferredPrompt(canonical, name string, def platform.Definition) string {
	n := strings.TrimSpace(canonical)
	if n == "" {
		n = strings.TrimSpace(name)
	}
	if n == "" {
		n = "device"
	}
	driver := strings.ToLower(def.Driver)
	suffix := "#"
	switch {
	case strings.Contains(driver, "junos"):
		suffix = ">"
	case driver == "linux" || strings.Contains(driver, "linux"):
		suffix = "$"
	}
	return n + suffix
}

// Definition is the effective platform definition the executor would open
// a session with, and whether this configuration knows the name, for the
// exercise's read-only checks (an unknown name is an error finding, as the
// executor would refuse it before any connection).
func Definition(cfg configload.Snapshot, name string) (platform.Definition, bool) {
	return definition(cfg, name)
}

// targetNotices converts the plan target's planning notices to the record's
// shape: the code and message as the client wrote
// them, the string details as the record's details.
func targetNotices(t executionplan.ExecutionTarget) []records.Notice {
	out := []records.Notice{}
	for _, n := range t.Notices {
		details := map[string]any{}
		for k, v := range n.Details {
			details[k] = v
		}
		out = append(out, records.Notice{Code: n.Code, Message: n.Message, Details: details})
	}
	return out
}

// definition is the platform used's definition and whether this
// configuration knows the name.
func definition(cfg configload.Snapshot, name string) (platform.Definition, bool) {
	return platform.Lookup(name, cfg.NamedTables("platform"))
}
func addrStrings(list []netip.Addr) []string {
	out := []string{}
	for _, a := range list {
		out = append(out, a.String())
	}
	return out
}
func familyOf(a netip.Addr) string {
	switch {
	case !a.IsValid():
		return ""
	case a.Is4():
		return "ipv4"
	}
	return "ipv6"
}

func statusFor(r platform.Result) string {
	if r.Err == nil && !r.DeviceError {
		return "succeeded"
	}
	switch r.ErrorCategory {
	case "authentication":
		return "authentication_error"
	case "connection", "name_resolution":
		return "connection_error"
	case "timeout":
		return "timeout"
	case "output":
		if r.ErrorCode == "output_limit_exceeded" {
			return "output_limit_exceeded"
		}
		return "output_error"
	}
	if r.DeviceError {
		return "device_error"
	}
	return "connection_error"
}

// classifyOpen keeps the specific registered code carried by an open failure.
// Only an uncoded failure is classified from its message.
// openStatus is the first record's status for a session that failed to
// open or prepare: the privilege step's own status, the device's for a
// rejected paging command, and the default (connection or authentication)
// otherwise.
func openStatus(code string) string {
	switch code {
	case "privilege_failed":
		return "privilege_error"
	case "paging_disable_failed":
		return "device_error"
	case "authentication_failed":
		return "authentication_error"
	}
	return ""
}

func classifyOpen(err error) (string, string) {
	if code := errorcodes.Of(err); code != "" {
		entry, _ := errorcodes.Lookup(code)
		return code, entry.Category
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "authentication") || strings.Contains(s, "password") {
		return "authentication_failed", "authentication"
	}
	return "connection_open_failed", "connection"
}
func addrString(a netip.Addr) string {
	if a.IsValid() {
		return a.String()
	}
	return ""
}
func defaultString(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// bareMessage is an error's text without its code prefix: the record's
// error carries the code in its own field, and the renderer prints
// "error=<code>: <message>", so the code appears once.
func bareMessage(e error) string {
	if e == nil {
		return ""
	}
	if code := errorcodes.Of(e); code != "" {
		return strings.TrimPrefix(errorcodes.Message(e), code+": ")
	}
	return e.Error()
}

func errorString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
func severity(status string) string {
	if status == "succeeded" {
		return "info"
	}
	if strings.HasPrefix(status, "not_started") || strings.HasPrefix(status, "not_attempted") {
		return "warning"
	}
	return "error"
}

// debugCommandText renders a command for the device command start event:
// the string the operator provided, from
// the command plan and never from the device's echo, quoted so it stays on
// one line; the debug writer bounds the line with a truncation marker. A
// fragment an input source marks secret would be redacted here; no
// input source marks fragments yet, and karvi does not guess which words of
// an unmarked command are secrets.
func debugCommandText(command string) string {
	return fmt.Sprintf("%q", command)
}
