package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/daemon"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/exitcode"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/planner"
)

// clientDraft is the first half of every daemon-backed run, shared by the
// live and exercise submission and by the dry-run inspection, so the
// inspection cannot drift from what a live run
// drafts: configuration, the target set, the overrides, the draft plan with
// client-authority DNS, and the credential planner run on the draft, which
// binds exactly the targets whose address the client selected.
type clientDraft struct {
	cfg      configload.Snapshot
	operator credentials.Operator
	set      planner.TargetSet
	draft    executionplan.ExecutionPlan
	planner  *planner.CredentialPlanner
	timing   draftTiming
	// id is the activity ID, and release removes its reservation when the
	// run ends before the daemon accepts the job.
	id      string
	release func()
}

// draftTiming is what the inspection report records per client stage.
type draftTiming struct {
	config, inventory, dns, credential time.Duration
}

// draftClient runs the shared first half. On failure it returns the site
// code that names the stage and the error; the caller reports both.
// loadWarnings prints the load's warnings after the load, before the draft:
// a daemon's job runs under the plan's configuration block, which has
// none, so its client says them (an in-process job prints its own).
func draftClient(ctx context.Context, opts RunOptions, streams IO, loadWarnings bool) (*clientDraft, string, error) {
	if opts.Format == "" {
		opts.Format = "text"
	}
	t0 := time.Now()
	cfg, operator, err := prepareConfig(opts.CommonOptions)
	if err != nil {
		return nil, "config_load_failed", err
	}
	if loadWarnings {
		for _, w := range cfg.Warnings {
			warning(streams.Stderr, w)
		}
	}
	cd := &clientDraft{cfg: cfg, operator: operator}
	cd.timing.config = time.Since(t0)
	t0 = time.Now()
	// --platform is checked before the inventory is read, as in command.
	platformName, err := CheckPlatformOption(cfg, opts.Platform)
	if err != nil {
		return nil, "platform_option_unknown", err
	}
	set, err := assembleTargets(ctx, cfg, operator, opts.Targets, opts.Excludes, streams.Stderr)
	if err != nil {
		return nil, "inventory_load_failed", err
	}
	overridePlatform(&set, platformName)
	if err := applyManagementAddress(&set, opts.ManagementAddress); err != nil {
		return nil, "management_address_invalid", err
	}
	overrides, err := applyAuthorityOverrides(&set, opts.AddressAuthorities)
	if err != nil {
		return nil, "address_authority_target_unknown", err
	}
	cd.set = set
	cd.timing.inventory = time.Since(t0)
	// The run's ID is its job directory, reserved before the draft (chapter
	// 11); the dry run passes its unreserved ID and reserves nothing.
	cd.id, cd.release = opts.ActivityID, func() {}
	if cd.id == "" {
		if cd.id, cd.release, err = reserveActivityID(cfg, operator, time.Now()); err != nil {
			return nil, "activity_id_generation_failed", err
		}
	}
	warn := func(s string) { warning(streams.Stderr, s) }
	draftOpts := planner.DraftOptions{
		ActivityType: "run", Commands: opts.Commands, CommandsFile: opts.CommandsFile, Inputs: opts.Targets, BlindReturns: opts.BlindReturns, Blind: opts.Blind, Expectations: opts.Expectations, Timeouts: opts.Timeouts, MaxBytes: opts.MaxBytes,
		Transport:        opts.Transport,
		PlatformCommands: opts.PlatformCommands, Collection: opts.Collection, Suffix: opts.Suffix,
		Format: opts.Format, Echo: opts.Echo, DynamicBorder: opts.DynamicBorder, NoBorder: opts.NoBorder, Follow: !opts.Detach,
		Address: planner.AddressOptions{Overrides: overrides, Warn: warn},
		Warn:    warn,
	}
	now := time.Now()
	t0 = now
	draft, err := planner.Draft(ctx, cfg, operator, set, draftOpts, now)
	if err != nil {
		return nil, "execution_plan_invalid", err
	}
	cd.draft = draft
	cd.timing.dns = time.Since(t0)
	t0 = time.Now()
	cp, err := planner.NewCredentialPlanner(cfg, operator, set.Devices, now, planner.CredentialOptions{Warn: warn})
	if err != nil {
		return nil, "credential_resolution_failed", err
	}
	// Client-authority targets bind before the daemon is touched, so the
	// preparation window never covers typing.
	if err := cp.Resolve(ctx, draft); err != nil {
		cp.Destroy()
		return nil, "credential_resolution_failed", err
	}
	cd.planner = cp
	cd.timing.credential = time.Since(t0)
	return cd, "", nil
}

// RunViaDaemon is the client's submission sequence over IPC schema 6:
// draft the plan and header,
// resolve client-authority credentials, prepare_job, incorporate the
// evidence, resolve daemon-authority credentials, bind, finalize, build and
// validate the package, send it over the credential channel, commit, then
// follow the accepted job to its terminal. The daemon at socket must
// already be running and compatible.
//
// ensure makes a compatible daemon answer at socket, launching one when none
// does (the client's ensureDaemon). It is called after the client has
// planned and just before the first request, so that the idle timer
// has the least room to end the daemon
// between the probe and the request; and when that first request still
// finds the daemon gone or draining, ensure is called once more and the
// request repeated. Nothing before the request is repeated: the plan, the
// credentials, and the identifiers are the ones already made.
func RunViaDaemon(ctx context.Context, opts RunOptions, socket string, maxFrame int64, ensure func(context.Context) error, streams IO) ActivityResult {
	cd, code, err := draftClient(ctx, opts, streams, true)
	if err != nil {
		if ctx.Err() != nil {
			return interrupted(stageClientPlanning, err)
		}
		return failedResult(code, stageError(stageClientPlanning, err))
	}
	cp, draft, id := cd.planner, cd.draft, cd.id
	// Until the daemon accepts the job, the reservation is the client's to
	// release; from the receipt on, the directory is the job's.
	accepted := false
	defer func() {
		if !accepted {
			cd.release()
		}
	}()
	defer cp.Destroy()
	// The mode is in every header and the commit request; nothing else in
	// the sequence depends on it.
	mode := executionplan.ModeLive
	if opts.Exercise {
		mode = executionplan.ModeExercise
	}
	header, err := planner.Header(draft, id, mode)
	if err != nil {
		return failedResult("job_header_invalid", err)
	}
	if err := ensure(ctx); err != nil {
		return failedResult("daemon_start_failed", err)
	}
	prepared, err := daemon.PrepareJob(ctx, socket, maxFrame, ipc.PrepareRequest{Header: header, Draft: draft})
	if err != nil && ctx.Err() == nil && daemonGone(err) {
		// The daemon left between the probe and the request (its idle
		// timer, or a stop): once more, with a daemon that answers.
		if err = ensure(ctx); err != nil {
			return failedResult("daemon_start_failed", err)
		}
		prepared, err = daemon.PrepareJob(ctx, socket, maxFrame, ipc.PrepareRequest{Header: header, Draft: draft})
	}
	if err != nil {
		if ctx.Err() != nil {
			// Before the frame: the preparation expires in the daemon's
			// sweep.
			return interrupted(stageDaemonValidation, err)
		}
		return failedResult("daemon_submit_failed", stageError(stageDaemonValidation, err))
	}
	if !prepared.Preparation.Accepted {
		return failedResult("daemon_submit_failed", prepareRefusal(prepared.Preparation.Findings))
	}
	plan, err := executionplan.IncorporatePreparation(draft, prepared.Preparation.Evidence)
	if err != nil {
		return failedResult("execution_plan_invalid", err)
	}
	if err := cp.Resolve(ctx, plan); err != nil {
		if ctx.Err() != nil {
			return interrupted(stageClientPlanning, err)
		}
		return failedResult("credential_resolution_failed", stageError(stageClientPlanning, err))
	}
	bound, err := cp.Bind(plan)
	if err != nil {
		return failedResult("credential_resolution_failed", err)
	}
	final, err := executionplan.Finalize(bound, time.Now())
	if err != nil {
		return failedResult("execution_plan_invalid", err)
	}
	finalHeader, err := planner.Header(final, id, mode)
	if err != nil {
		return failedResult("job_header_invalid", err)
	}
	hostname, _ := os.Hostname()
	pkg, err := cp.Package(final, finalHeader, prepared.Preparation.Audience, hostname, time.Now())
	if err != nil {
		return failedResult("credential_package_invalid", err)
	}
	defer pkg.Destroy()
	ref, projection, err := planner.PackageReference(pkg)
	if err != nil {
		return failedResult("credential_package_invalid", err)
	}
	commit, err := planner.CommitHeader(final, id, mode, ref)
	if err != nil {
		return failedResult("job_header_invalid", err)
	}
	// The local-peer protector derives the envelope and the body from the
	// one package.
	protector, err := credentialpackage.ProtectorFor(credentialpackage.ProtectionLocalPeer)
	if err != nil {
		return failedResult("credential_package_invalid", err)
	}
	protected, err := protector.Protect(ctx, pkg)
	if err != nil {
		return failedResult("credential_package_invalid", err)
	}
	// From the frame through the commit nothing is cancellable: an
	// interrupt cannot leave a package on the daemon
	// without a decision, and both steps are short.
	receipt, err := daemon.ProvideCredentials(context.WithoutCancel(ctx), prepared.CredentialChannel, prepared.Preparation.PreparationID, protected)
	protected.Destroy()
	pkg.Destroy()
	cp.Destroy()
	if err != nil {
		return failedResult("daemon_submit_failed", stageError(stageDaemonValidation, err))
	}
	if err := daemon.CheckReceipt(receipt, projection); err != nil {
		return failedResult("credential_package_invalid", stageError(stageDaemonValidation, err))
	}
	request := ipc.CommitRequest{Header: commit, Plan: final, Mode: mode, Preparations: []ipc.PreparationReference{{ExecutionEndpoint: prepared.Preparation.ExecutionEndpoint, PreparationID: prepared.Preparation.PreparationID, PreparationDigest: prepared.Preparation.PreparationDigest}}}
	// The commit is short and runs without cancellation: once the frame is
	// sent, the package is
	// consumed by the commit or expires with the preparation.
	committed, err := daemon.CommitJob(context.WithoutCancel(ctx), socket, maxFrame, request)
	if err != nil {
		return failedResult("daemon_submit_failed", stageError(stageDaemonValidation, err))
	}
	accepted = true
	jobReceipt := committed.Receipt
	// The daemon's admission warnings (IPC schema 10: a narrowed width) on
	// standard error, as the in-process path prints its
	// own; --detach and the follow alike, before the receipt or the records.
	jobexec.WriteAdmissionWarnings(streams.Stderr, cd.cfg, jobReceipt.Warnings)
	if opts.Detach {
		// --detach: the receipt and nothing more;
		// exit 0 means accepted.
		if err := renderReceipt(streams.Stdout, opts.Format, jobReceipt); err != nil {
			r := failedResult("terminal_write_failed", err)
			r.JobID, r.ArtifactDir = jobReceipt.JobID, jobReceipt.ArtifactDir
			return r
		}
		return ActivityResult{ExitCode: exitcode.ExitSuccess, ExitName: exitcode.ExitName(exitcode.ExitSuccess), ActivityID: id, JobID: jobReceipt.JobID, ArtifactDir: jobReceipt.ArtifactDir}
	}
	// Then follow the accepted job to its terminal, rendering its records
	// as they become durable; an exercise has no record to render and its
	// report is read afterwards.
	terminal, err := followJob(ctx, followOptions{cfg: cd.cfg, socket: socket, maxFrame: maxFrame, jobID: jobReceipt.JobID, render: opts.Follow && mode == executionplan.ModeLive, format: opts.Format, quiet: opts.Quiet, debug: opts.Debug, echo: opts.Echo, dynamic: opts.DynamicBorder, noBorder: opts.NoBorder, stderr: streams.Stderr}, jobReceipt.ArtifactDir, streams.Stdout)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(streams.Stderr, InterruptedLine(jobReceipt.JobID, jobReceipt.ArtifactDir, "continues in the daemon"))
			r := failedResult("cancelled", errorcodes.Errorf("cancelled", "the follow of job %s was interrupted; the job continues", jobReceipt.JobID))
			r.JobID, r.ArtifactDir = jobReceipt.JobID, jobReceipt.ArtifactDir
			return r
		}
		fmt.Fprintf(streams.Stderr, "job %s continues in the daemon; artifacts %s\n", jobReceipt.JobID, jobReceipt.ArtifactDir)
		r := failedResult("follow_stream_lost", err)
		r.JobID, r.ArtifactDir = jobReceipt.JobID, jobReceipt.ArtifactDir
		return r
	}
	o := terminal.Outcome
	// A job cancelled from another invocation: the
	// records rendered so far stand, the line names the reason and the
	// artifacts, and the exit is the job's, 113. A cancel accepted too late
	// to change the job, and any cancel of an exercise, leaves the block in
	// the summary and prints no line.
	if c := o.Summary.CancelledBy(); c != nil {
		fmt.Fprintln(streams.Stderr, cancelledLine(jobReceipt.JobID, c.Reason, jobReceipt.ArtifactDir))
	}
	result := ActivityResult{ExitCode: o.ExitCode, ExitName: o.ExitName, ActivityID: o.ActivityID, JobID: jobReceipt.JobID, ArtifactDir: jobReceipt.ArtifactDir, Summary: o.Summary, Error: o.Error}
	if mode == executionplan.ModeExercise {
		report, err := readExerciseReport(jobReceipt.ReportPath, jobReceipt.JobID, final.PlanDigest)
		if err != nil {
			r := failedResult("exercise_report_unreadable", err)
			r.JobID, r.ArtifactDir = jobReceipt.JobID, jobReceipt.ArtifactDir
			return r
		}
		if err := renderExercise(streams.Stdout, opts.Format, report, socket); err != nil {
			r := failedResult("terminal_write_failed", err)
			r.JobID, r.ArtifactDir = jobReceipt.JobID, jobReceipt.ArtifactDir
			return r
		}
	}
	return result
}

// InterruptedLine is the line of an interrupted follow: the job continues,
// and the way back
// to it is job follow or the summary.
// ArtifactsLabel is a result line's <artifacts>: the job's folder, or
// "none" for a job that keeps none (`--nof`),
// so that no line ends in a bare "artifacts=" that reads as a fault. The
// one rule for the daemon path's lines, as jobexec's footer has it for the
// in-process paths.
func ArtifactsLabel(artifactDir string) string {
	if artifactDir == "" {
		return "none"
	}
	return artifactDir
}

func InterruptedLine(jobID, artifactDir, state string) string {
	if artifactDir == "" {
		return fmt.Sprintf("interrupted: job %s %s; run \"karvi job follow %s\" again or read its summary.json when it ends", jobID, state, jobID)
	}
	return fmt.Sprintf("interrupted: job %s %s; artifacts %s; run \"karvi job follow %s\" again or read %s/summary.json when it ends", jobID, state, artifactDir, jobID, artifactDir)
}

// CancelledLine is the line a client prints for a job cancelled through
// cancel_job: for a summary whose
// CancelledBy answers the request, never for the block alone.
func CancelledLine(jobID, reason, artifactDir string) string {
	return cancelledLine(jobID, reason, artifactDir)
}

func cancelledLine(jobID, reason, artifactDir string) string {
	if reason == "" {
		return fmt.Sprintf("job %s cancelled; artifacts %s", jobID, artifactDir)
	}
	return fmt.Sprintf("job %s cancelled: %s; artifacts %s", jobID, reason, artifactDir)
}

// The stages of a submission, written after the code in an abort's
// message.
const (
	stageClientPlanning   = "client planning"
	stageDaemonDNS        = "daemon DNS preparation"
	stageDaemonValidation = "daemon validation"
)

// stageError names the stage after the code; the code is preserved.
func stageError(stage string, err error) error {
	return fmt.Errorf("%s: %w", stage, err)
}

// prepareRefusal is the abort of a prepare_job refusal: a refusal whose
// findings are all daemon DNS aborts under
// executor_dns_failed with the stage named; any other refusal aborts under
// its first finding's code as daemon validation.
func prepareRefusal(findings []executionplan.Finding) error {
	if len(findings) == 0 {
		return errorcodes.Errorf("job_rejected", "%s: the daemon did not accept the request and gave no finding", stageDaemonValidation)
	}
	allDNS := true
	for _, f := range findings {
		if f.Stage != "daemon_dns" {
			allDNS = false
		}
	}
	parts := findingParts(findings)
	if allDNS {
		return errorcodes.Errorf("executor_dns_failed", "%s: %d finding(s): %s", stageDaemonDNS, len(findings), strings.Join(parts, "; "))
	}
	return errorcodes.Errorf(findings[0].Code, "%s: %d finding(s): %s", stageDaemonValidation, len(findings), strings.Join(parts, "; "))
}

func findingParts(findings []executionplan.Finding) []string {
	parts := make([]string, 0, len(findings))
	for _, f := range findings {
		if f.TargetID != "" {
			parts = append(parts, fmt.Sprintf("%s: %s (%s)", f.TargetID, f.Message, f.Code))
		} else {
			parts = append(parts, fmt.Sprintf("%s (%s)", f.Message, f.Code))
		}
	}
	return parts
}

// interrupted is the abort of an interrupt before the frame: cancelled,
// the stage named, nothing left on the daemon
// that does not expire on its own.
func interrupted(stage string, cause error) ActivityResult {
	return failedResult("cancelled", errorcodes.Errorf("cancelled", "%s: interrupted before any job was submitted (%v)", stage, errorText(cause)))
}

// renderReceipt prints the accepted job for --detach.
func renderReceipt(out io.Writer, format string, r ipc.JobReceipt) error {
	switch format {
	case "json":
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	case "jsonl":
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(out, "%s\n", b)
		return err
	}
	_, err := fmt.Fprintf(out, "job_id: %s\nartifact_dir: %s\n", r.JobID, r.ArtifactDir)
	return err
}

// daemonGone reports whether a request failed because no daemon accepts
// it: the socket is gone or refuses the connection, or the daemon answered
// daemon_draining. Any other failure is the request's own.
func daemonGone(err error) bool {
	return errorcodes.Of(err) == "daemon_draining" || errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, fs.ErrNotExist)
}
