package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/buildinfo"
	"github.com/robert-patrick-texas/karvi/internal/credentialframe"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/records"
)

// Bounds of the preparation table and the retained commit receipts.
const (
	MaxPreparations = 128
	MaxReceipts     = 1024
)

// preparation is one prepare_job the daemon holds until its commit or
// expiry.
type preparation struct {
	header      executionplan.PublicJobHeader
	draftDigest executionplan.Digest
	report      executionplan.PreparationReport
	token       ipc.ChannelToken
	expiresAt   time.Time
	tokenUsed   bool
	scopes      []credentialpackage.TargetScope // the draft's, for the provided stage
	pkg         *credentialpackage.CredentialPackage
	projection  credentialpackage.SafePackageProjection
	// What the daemon measured, for an exercise report's timing.
	prepareNS, dnsNS, frameNS int64
}

type preparationTable struct {
	mu      sync.Mutex
	entries map[string]*preparation
}

func (t *preparationTable) init() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = map[string]*preparation{}
}

// sweep destroys every expired preparation's package.
func (t *preparationTable) sweep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, p := range t.entries {
		if !now.Before(p.expiresAt) {
			if p.pkg != nil {
				p.pkg.Destroy()
			}
			delete(t.entries, id)
		}
	}
}

// count is the number of live preparations.
func (t *preparationTable) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}

func (t *preparationTable) destroyAll() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, p := range t.entries {
		if p.pkg != nil {
			p.pkg.Destroy()
		}
		delete(t.entries, id)
	}
}

// idempotencyTable holds one entry per commit key for the daemon's lifetime,
// bounded at MaxReceipts with the oldest evicted.
type idempotencyTable struct {
	mu      sync.Mutex
	entries map[string]*committed // key -> entry
	jobs    map[string]string     // job ID -> key
	order   []string
}

type committed struct {
	planDigest executionplan.Digest
	jobID      string
	result     ipc.CommitResult
	done       chan struct{}
}

func (t *idempotencyTable) init() {
	t.entries = map[string]*committed{}
	t.jobs = map[string]string{}
}

// Audience is the daemon's package audience: daemon:<hostname>:<uid>.
func (s *Server) Audience() string {
	host, _ := os.Hostname()
	return fmt.Sprintf("daemon:%s:%d", host, s.UID)
}

func (s *Server) prepareJob(ctx context.Context, conn net.Conn, req ipc.Request) {
	var pr ipc.PrepareRequest
	if err := json.Unmarshal(req.Payload, &pr); err != nil {
		s.logf(req.Operation, req.RequestID, "", "", "job_request_malformed")
		s.writeError(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	if err := pr.Validate(); err != nil {
		s.logf(req.Operation, req.RequestID, "", pr.Header.JobID, s.writeCoded(conn, "job_rejected", err, req.RequestID))
		return
	}
	if s.draining.Load() {
		s.logf(req.Operation, req.RequestID, "", pr.Header.JobID, "daemon_draining")
		s.writeError(conn, "daemon_draining", fmt.Errorf("daemon is draining and accepts no new jobs"), req.RequestID)
		return
	}
	if pr.Header.Operator.UID != s.UID {
		s.logf(req.Operation, req.RequestID, "", pr.Header.JobID, "peer_uid_denied")
		s.writeError(conn, "peer_uid_denied", fmt.Errorf("header operator uid %d differs from the daemon's %d", pr.Header.Operator.UID, s.UID), req.RequestID)
		return
	}
	s.preparations.mu.Lock()
	full := len(s.preparations.entries) >= s.MaxPreparations
	s.preparations.mu.Unlock()
	if full {
		s.logf(req.Operation, req.RequestID, "", pr.Header.JobID, "job_rejected")
		s.writeError(conn, "job_rejected", fmt.Errorf("daemon preparation table holds %d entries", s.MaxPreparations), req.RequestID)
		return
	}
	now := s.now()
	prepareStart := time.Now()
	prepID, err := osutil.NewID(now)
	if err != nil {
		s.writeError(conn, "activity_id_generation_failed", err, req.RequestID)
		return
	}
	draftSum, err := executionplan.SumPlan(pr.Draft)
	if err != nil {
		s.writeError(conn, "execution_plan_invalid", err, req.RequestID)
		return
	}
	caps := resolver.ProbeFamilies()
	if s.Capabilities != nil {
		caps = *s.Capabilities
	}
	findings := []executionplan.Finding{}
	dnsStart := time.Now()
	evidence, err := resolver.PrepareDaemonWith(ctx, s.Config, pr.Draft.Targets, caps, s.Lookup)
	dnsNS := time.Since(dnsStart).Nanoseconds()
	accepted := err == nil
	if err != nil {
		if te, ok := err.(*resolver.TargetErrors); ok {
			for _, f := range te.Failures {
				findings = append(findings, executionplan.Finding{Code: codeOf(f.Err, "name_resolution_error"), Severity: executionplan.SeverityError, Stage: "daemon_dns", TargetID: f.TargetID, Message: errorcodes.Message(f.Err)})
			}
		} else {
			findings = append(findings, executionplan.Finding{Code: codeOf(err, "name_resolution_error"), Severity: executionplan.SeverityError, Stage: "daemon_dns", Message: errorcodes.Message(err)})
		}
		evidence = []executionplan.AddressEvidence{}
	}
	policy, err := jobexec.Policy(s.Config)
	if err != nil {
		s.writeError(conn, "ipc_encode_failed", err, req.RequestID)
		return
	}
	report := executionplan.PreparationReport{
		PreparationID: prepID, ExecutionEndpoint: executionplan.EndpointLocal, Audience: s.Audience(),
		PreparedAt: now, ExpiresAt: now.Add(ipc.PreparationLifetime), DraftDigest: draftSum, Accepted: accepted,
		Evidence: executionplan.PreparationEvidence{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: prepID, PreparedAt: now, Addresses: evidence},
		Daemon: executionplan.DaemonReadiness{
			ExecutionEndpoint: executionplan.EndpointLocal, Audience: s.Audience(), Reachable: true, Status: s.state(),
			PID: os.Getpid(), Version: buildinfo.Version, IPCSchemaVersion: ipc.SchemaVersion, PolicyDigest: policy.Digest,
			Capabilities: []string{ipc.OpPrepareJob, ipc.OpProvideCredentials, ipc.OpCommitJob}, Findings: []executionplan.Finding{},
		},
		Findings: findings,
	}
	if accepted {
		report, err = executionplan.Seal(report)
		if err != nil {
			s.writeError(conn, "ipc_encode_failed", err, req.RequestID)
			return
		}
	}
	result := ipc.PrepareResult{Preparation: report}
	if accepted {
		token, err := ipc.NewChannelToken()
		if err != nil {
			s.writeError(conn, "ipc_encode_failed", err, req.RequestID)
			return
		}
		s.preparations.mu.Lock()
		s.preparations.entries[prepID] = &preparation{header: pr.Header, draftDigest: draftSum, report: report, token: token, expiresAt: report.ExpiresAt, scopes: credentialpackage.ScopeOf(&pr.Draft), prepareNS: time.Since(prepareStart).Nanoseconds(), dnsNS: dnsNS}
		s.preparations.mu.Unlock()
		result.CredentialChannel = ipc.CredentialChannel{Socket: CredentialSocket(s.Socket), Token: token, ExpiresAt: report.ExpiresAt, MaxFrame: credentialframe.MaxBody}
	}
	code := "accepted"
	if !accepted && len(findings) > 0 {
		code = findings[0].Code
	}
	s.logf(req.Operation, req.RequestID, prepID, pr.Header.JobID, code)
	s.writeResult(conn, req.RequestID, result)
}

// handleFrame is the credential channel: one frame per connection, the
// protector, the provided-stage
// validation, then one receipt.
func (s *Server) handleFrame(ctx context.Context, conn *net.UnixConn) {
	uid, err := ipc.PeerUID(conn)
	if err != nil || int(uid) != s.UID {
		_ = credentialframe.WriteReceipt(conn, credentialframe.Receipt{Accepted: false, Findings: []executionplan.Finding{{Code: "peer_uid_denied", Severity: executionplan.SeverityError, Stage: "credential_package", Message: "the credential channel accepts only the same effective UID"}}})
		return
	}
	frameStart := time.Now()
	h, body, err := credentialframe.Read(conn, credentialframe.Limits{})
	if err != nil {
		code := codeOf(err, "credential_frame_malformed")
		s.logf(ipc.OpProvideCredentials, "", "", "", code)
		_ = credentialframe.WriteReceipt(conn, credentialframe.Receipt{Accepted: false, Findings: []executionplan.Finding{{Code: code, Severity: executionplan.SeverityError, Stage: "credential_package", Message: errorcodes.Message(err)}}})
		return
	}
	defer body.Destroy()
	if s.Logger != nil {
		s.Logger.Info("credential frame", "header", h)
	}
	reject := func(code, msg string) {
		s.logf(ipc.OpProvideCredentials, "", h.PreparationID, h.Envelope.JobID, code)
		_ = credentialframe.WriteReceipt(conn, credentialframe.Receipt{PackageID: h.Envelope.PackageID, PackageDigest: h.Envelope.PackageDigest, Accepted: false, Findings: []executionplan.Finding{{Code: code, Severity: executionplan.SeverityError, Stage: "credential_package", Message: msg}}})
	}
	now := s.now()
	s.preparations.mu.Lock()
	p, ok := s.preparations.entries[h.PreparationID]
	if !ok {
		s.preparations.mu.Unlock()
		reject("preparation_unknown", "no preparation holds this token")
		return
	}
	if !now.Before(p.expiresAt) {
		s.preparations.mu.Unlock()
		reject("preparation_expired", "the preparation has expired")
		return
	}
	used := p.tokenUsed
	p.tokenUsed = true // one use, whatever follows
	token := p.token
	s.preparations.mu.Unlock()
	if used {
		reject("credential_channel_token_invalid", "the preparation's token was already used")
		return
	}
	if err := h.VerifyToken(token); err != nil {
		reject("credential_channel_token_invalid", errorcodes.Message(err))
		return
	}
	// The protector for the envelope's protection turns the body back into
	// the package: sealed is refused here, the
	// envelope is validated and must name the daemon's audience, and the
	// decoded package must match the envelope.
	protector, err := credentialpackage.ProtectorFor(h.Envelope.Protection)
	if err != nil {
		reject(codeOf(err, "credential_package_invalid"), errorcodes.Message(err))
		return
	}
	pkg, err := protector.Unprotect(ctx, credentialpackage.Protected{Envelope: h.Envelope, Body: body}, s.Audience())
	if err != nil {
		reject(codeOf(err, "credential_package_invalid"), errorcodes.Message(err))
		return
	}
	projection, err := pkg.SafeProjection()
	if err != nil {
		pkg.Destroy()
		reject("credential_package_invalid", errorcodes.Message(err))
		return
	}
	// The provided stage: every package rule but the
	// two that need the final plan, against the preparation's header and
	// the draft's target scopes. Commit runs the full Validate again.
	if err := pkg.ValidateProvided(p.scopes, p.header, s.Audience(), now); err != nil {
		pkg.Destroy()
		reject(codeOf(err, "credential_package_invalid"), errorcodes.Message(err))
		return
	}
	s.preparations.mu.Lock()
	if p.pkg != nil {
		p.pkg.Destroy()
	}
	p.pkg, p.projection = &pkg, projection
	p.frameNS = time.Since(frameStart).Nanoseconds()
	s.preparations.mu.Unlock()
	s.logf(ipc.OpProvideCredentials, "", h.PreparationID, pkg.JobID, "accepted")
	_ = credentialframe.WriteReceipt(conn, credentialframe.Receipt{PackageID: projection.PackageID, PackageDigest: projection.PackageDigest, GrantCount: len(pkg.Grants), BindingCount: len(pkg.Bindings), Accepted: true, Findings: []executionplan.Finding{}})
}

func (s *Server) commitJob(ctx context.Context, conn net.Conn, req ipc.Request) {
	var cr ipc.CommitRequest
	if err := json.Unmarshal(req.Payload, &cr); err != nil {
		s.logf(req.Operation, req.RequestID, "", "", "job_request_malformed")
		s.writeError(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	jobID := cr.Header.JobID
	fail := func(fallback string, err error) {
		s.logf(req.Operation, req.RequestID, prepIDOf(cr), jobID, s.writeCoded(conn, fallback, err, req.RequestID))
	}
	if err := cr.Validate(); err != nil {
		fail("job_rejected", err)
		return
	}
	if len(cr.Preparations) != 1 {
		fail("execution_plan_invalid", errorcodes.Errorf("execution_plan_invalid", "commit_job references %d preparations; v1 takes exactly one", len(cr.Preparations)))
		return
	}
	// Idempotency (decisions 6.4, 7.5): the envelope key is the header's.
	key := req.IdempotencyKey
	if key == "" {
		key = cr.Header.IdempotencyKey
	}
	s.idempotency.mu.Lock()
	if held, ok := s.idempotency.entries[key]; ok {
		s.idempotency.mu.Unlock()
		<-held.done
		if held.planDigest != cr.Plan.PlanDigest {
			fail("idempotency_conflict", errorcodes.Errorf("idempotency_conflict", "key %q was committed with plan %s, not %s", key, held.planDigest, cr.Plan.PlanDigest))
			return
		}
		s.logf(req.Operation, req.RequestID, prepIDOf(cr), jobID, "replayed")
		s.writeResult(conn, req.RequestID, held.result)
		return
	}
	if other, ok := s.idempotency.jobs[jobID]; ok && other != key {
		s.idempotency.mu.Unlock()
		fail("job_id_conflict", errorcodes.Errorf("job_id_conflict", "job %s is held under another idempotency key", jobID))
		return
	}
	entry := &committed{planDigest: cr.Plan.PlanDigest, jobID: jobID, done: make(chan struct{})}
	s.idempotency.entries[key] = entry
	s.idempotency.jobs[jobID] = key
	s.idempotency.order = append(s.idempotency.order, key)
	for len(s.idempotency.order) > MaxReceipts {
		oldest := s.idempotency.order[0]
		s.idempotency.order = s.idempotency.order[1:]
		if e := s.idempotency.entries[oldest]; e != nil {
			delete(s.idempotency.jobs, e.jobID)
		}
		delete(s.idempotency.entries, oldest)
	}
	s.idempotency.mu.Unlock()
	// An entry that never commits is forgotten, so a corrected retry is not
	// a conflict.
	forget := func() {
		s.idempotency.mu.Lock()
		delete(s.idempotency.entries, key)
		delete(s.idempotency.jobs, jobID)
		s.idempotency.mu.Unlock()
		close(entry.done)
	}
	ref := cr.Preparations[0]
	now := s.now()
	s.preparations.mu.Lock()
	p, ok := s.preparations.entries[ref.PreparationID]
	if !ok {
		s.preparations.mu.Unlock()
		forget()
		fail("preparation_unknown", errorcodes.Errorf("preparation_unknown", "preparation %s is not held", ref.PreparationID))
		return
	}
	if !now.Before(p.expiresAt) {
		s.preparations.mu.Unlock()
		forget()
		fail("preparation_expired", errorcodes.Errorf("preparation_expired", "preparation %s expired at %s", ref.PreparationID, p.expiresAt.Format(time.RFC3339)))
		return
	}
	if ref.PreparationDigest != p.report.PreparationDigest {
		s.preparations.mu.Unlock()
		forget()
		fail("plan_digest_mismatch", errorcodes.Errorf("plan_digest_mismatch", "preparation %s digest %s is not the stored report's %s", ref.PreparationID, ref.PreparationDigest, p.report.PreparationDigest))
		return
	}
	if p.pkg == nil {
		s.preparations.mu.Unlock()
		forget()
		fail("credential_package_missing", errorcodes.Errorf("credential_package_missing", "preparation %s received no credential frame", ref.PreparationID))
		return
	}
	pkg, projection := *p.pkg, p.projection
	prepReport := p.report
	prepareNS, dnsNS, frameNS := p.prepareNS, p.dnsNS, p.frameNS
	delete(s.preparations.entries, ref.PreparationID)
	s.preparations.mu.Unlock()
	commitStart := time.Now()
	destroyed, handed := false, false
	var destroyMu sync.Mutex
	destroy := func() {
		destroyMu.Lock()
		defer destroyMu.Unlock()
		if !destroyed {
			pkg.Destroy()
			destroyed = true
		}
	}
	defer func() {
		if !handed {
			destroy()
		}
	}()
	if cr.Header.CredentialPackage == nil || cr.Header.CredentialPackage.Digest != projection.PackageDigest || cr.Header.CredentialPackage.Protection != string(projection.Protection) {
		forget()
		fail("credential_package_invalid", errorcodes.Errorf("credential_package_invalid", "rule=header_reference: the header's package reference is not the provided package"))
		return
	}
	if err := pkg.Validate(&cr.Plan, cr.Header, s.Audience(), now); err != nil {
		forget()
		fail("credential_package_invalid", err)
		return
	}
	select {
	case s.jobs <- struct{}{}:
	default:
		forget()
		fail("job_rejected", errorcodes.Errorf("job_rejected", "daemon accepted-job limit %d is in use", s.MaxJobs))
		return
	}
	if !s.admit() {
		<-s.jobs
		forget()
		fail("daemon_draining", errorcodes.Errorf("daemon_draining", "daemon is draining and accepts no new jobs"))
		return
	}
	s.total.Add(1)
	_ = s.writeState(s.state())
	// The job runs on its own goroutine and commit_job answers at
	// acceptance: OnAccepted fires once the
	// manifest is durable; the package is destroyed when the run returns.
	j := newJob()
	// Each job runs under its own child of the server's job context: a
	// forced stop or grace expiry cancels the
	// parent and reaches every job with its shutdown cause; cancel_job
	// cancels this child alone with the cancel_job cause.
	jobCtx, cancelJob := context.WithCancelCause(s.jobCtx)
	j.cancel = cancelJob
	accepted := make(chan acceptance, 1)
	finished := make(chan jobexec.ActivityResult, 1)
	handed = true
	s.jobWait.Add(1)
	request := jobexec.Request{
		Plan: cr.Plan, Header: cr.Header, Package: projection, Grants: pkg, Protection: string(projection.Protection), Mode: cr.Mode,
		Config: s.Config, Operator: s.Operator, Quiet: true, Daemon: true, Logger: s.Logger,
		ActivityType: "run", ActivityID: jobID,
		OnAccepted: func(dir string, warnings []string) { accepted <- acceptance{dir, warnings} }, OnDurable: j.durable,
		Preparations: []executionplan.PreparationReport{prepReport},
		Timing:       exerciseTiming(prepareNS, dnsNS, frameNS, time.Since(commitStart).Nanoseconds()),
	}
	// The cancel_job audit record is written through the daemon's own sink
	// with the job's plan and operator, never its grants.
	auditReq := request
	auditReq.Grants = nil
	j.auditCancel = func(rq jobexec.JobCancelRequest) {
		if s.audit != nil {
			if err := jobexec.WriteCancelRequested(s.audit, auditReq, jobID, rq); err != nil && s.Logger != nil {
				s.Logger.Warn("cancel_requested audit record not written", slog.String("job_id", jobID), slog.String("error", err.Error()))
			}
		}
	}
	go func() {
		defer s.jobWait.Done()
		defer cancelJob(nil)
		result := jobexec.Run(jobCtx, request, jobexec.IO{Stdout: io.Discard, Stderr: io.Discard})
		destroy()
		j.finish(outcomeOf(result.Summary, result.ExitCode, result.ExitName, result.ActivityID, result.JobID, result.ArtifactDir, result.Error))
		s.active.Add(-1)
		s.touch() // the idle clock runs from the job's end
		<-s.jobs
		_ = s.writeState(s.state())
		s.logf(ipc.OpCommitJob, req.RequestID, ref.PreparationID, jobID, result.ExitName)
		finished <- result
	}()
	acceptedAt := s.now()
	var acc acceptance
	select {
	case acc = <-accepted:
	case result := <-finished:
		// A run that ends this fast either failed before acceptance or
		// accepted and finished together; the channel says which.
		select {
		case acc = <-accepted:
		default:
			forget()
			fail("job_rejected", errors.New(result.Error))
			return
		}
	}
	dir := acc.dir
	// The receipt carries the admission warnings (IPC schema 10), and the
	// follow start repeats them for a follow by
	// job ID alone.
	receipt := ipc.JobReceipt{JobID: jobID, AcceptedAt: acceptedAt, ArtifactDir: dir, PlanDigest: cr.Plan.PlanDigest, Mode: cr.Mode, Warnings: acc.warnings}
	if cr.Mode == executionplan.ModeExercise {
		receipt.ReportPath = filepath.Join(dir, jobexec.ExerciseReportName)
	}
	if err := j.open(dir, receipt); err != nil {
		forget()
		fail("output_store_create_failed", err)
		return
	}
	s.jobTable.add(jobID, j)
	entry.result = ipc.CommitResult{Receipt: receipt}
	close(entry.done)
	s.logf(req.Operation, req.RequestID, ref.PreparationID, jobID, "accepted")
	s.writeResult(conn, req.RequestID, entry.result)
}

// acceptance is what the activity hands over when the job is accepted:
// its folder and its admission warnings.
type acceptance struct {
	dir      string
	warnings []string
}

// exerciseTiming is the daemon's part of an exercise report's timing.
func exerciseTiming(prepareNS, dnsNS, frameNS, commitNS int64) records.ReportTiming {
	return records.ReportTiming{DaemonPrepareNS: &prepareNS, DaemonDNSNS: &dnsNS, PackageTransferNS: &frameNS, CommitNS: &commitNS}
}

func prepIDOf(cr ipc.CommitRequest) string {
	if len(cr.Preparations) == 1 {
		return cr.Preparations[0].PreparationID
	}
	return ""
}

func codeOf(err error, fallback string) string {
	if c := errorcodes.Of(err); c != "" {
		return c
	}
	return fallback
}

// Preparations reports the held preparation IDs, for tests.
func (s *Server) Preparations() []string {
	s.preparations.mu.Lock()
	defer s.preparations.mu.Unlock()
	out := make([]string, 0, len(s.preparations.entries))
	for id := range s.preparations.entries {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
