package ipc

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const ipcSchema = "../../schema/daemon-ipc.schema.json"

func TestSchema5PayloadsAreStructurallyNonSecret(t *testing.T) {
	allow := canarytest.Allow{Leaves: []string{"net/netip.Addr", "time.Time", "github.com/robert-patrick-texas/karvi/executionplan.Digest", "github.com/robert-patrick-texas/karvi/credentials.Match", "github.com/robert-patrick-texas/karvi/internal/ipc.ChannelToken"}}
	for _, rt := range []reflect.Type{reflect.TypeOf(PrepareRequest{}), reflect.TypeOf(PrepareResult{}), reflect.TypeOf(CommitRequest{}), reflect.TypeOf(JobReceipt{})} {
		canarytest.Walk(t, rt, allow)
	}
	// CommitResult carries a records.Summary whose halt field is untyped by
	// the existing contract; it is allowed by field, and the outcome mirrors
	// app.ActivityResult so the walker sees the same shape the daemon
	// returns today.
	canarytest.Walk(t, reflect.TypeOf(CommitResult{}), canarytest.Allow{Leaves: allow.Leaves, Fields: []string{"Summary.Halt", "Summary.Output", "Summary.AuditSinkStatus", "Summary.Bottleneck", "Summary.Recovery"}})
	// The token is allowed only where the design places it.
	for _, rt := range []reflect.Type{reflect.TypeOf(CredentialChannel{})} {
		found := false
		for i := 0; i < rt.NumField(); i++ {
			if rt.Field(i).Type == reflect.TypeOf(ChannelToken{}) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s lacks the token", rt)
		}
	}
}

func TestChannelTokenIsACapabilityNotACredential(t *testing.T) {
	tok, err := NewChannelToken()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := tok.MarshalText()
	seed := canary.Value{Raw: string(text)}
	canarytest.Exercise(t, tok, canary.Capability, seed)
	// Containers of the token redact under slog too (the JSON handler
	// serializes struct attributes with encoding/json, so each container
	// needs its own LogValue).
	channel := CredentialChannel{Socket: "/run/x", Token: tok, ExpiresAt: time.Now(), MaxFrame: 1}
	canarytest.Exercise(t, channel, canary.Capability, seed)
	res := fixturePrepareResult(t)
	res.CredentialChannel.Token = tok
	canarytest.Exercise(t, res, canary.Capability, seed)
	for _, v := range []any{tok, channel, res} {
		if _, ok := v.(slog.LogValuer); !ok {
			t.Errorf("%T does not implement slog.LogValuer", v)
		}
	}
	var back ChannelToken
	if err := back.UnmarshalText(text); err != nil || !back.Equal(tok) {
		t.Fatal("text round trip")
	}
	if tok.Equal(ChannelToken{}) || tok.IsZero() {
		t.Fatal("Equal or IsZero")
	}
	if err := back.UnmarshalText([]byte("short")); errorcodes.Of(err) != "ipc_result_malformed" {
		t.Errorf("short token: %v", err)
	}
}

func TestEnvelopeRefusesSecretPayloads(t *testing.T) {
	seed := canarytest.Seed(t)
	pkg := credentialpackage.CredentialPackage{SchemaVersion: 1, Grants: []credentialpackage.CredentialGrant{{Password: credentials.NewSecretString(seed.Raw)}}}
	for name, payload := range map[string]any{"package": pkg, "grant": pkg.Grants[0], "secret": credentials.NewSecretString(seed.Raw), "bytes": credentials.NewSecretBytes([]byte(seed.Raw)), "wrapped": map[string]any{"pkg": pkg}} {
		_, err := NewRequest("r1", OpProvideCredentials, "0.9.2", payload)
		if errorcodes.Of(err) != "secret_serialization_refused" {
			t.Errorf("NewRequest(%s): %v", name, err)
		}
		var buf bytes.Buffer
		err = Write(&buf, payload, 1<<20)
		if errorcodes.Of(err) != "secret_serialization_refused" || buf.Len() != 0 {
			t.Errorf("Write(%s): err=%v wrote %d bytes", name, err, buf.Len())
		}
	}
}

func fixturePrepare(t *testing.T) PrepareRequest {
	t.Helper()
	draft := plantest.DraftPlan()
	return PrepareRequest{Header: plantest.Header(draft, executionplan.Draft), Draft: draft}
}

func fixturePrepareResult(t *testing.T) PrepareResult {
	t.Helper()
	draft := plantest.DraftPlan()
	draftSum, _ := executionplan.SumPlan(draft)
	prep, err := executionplan.Seal(executionplan.PreparationReport{
		PreparationID: plantest.PreparationID, ExecutionEndpoint: executionplan.EndpointLocal, Audience: "daemon:ops01:1000",
		PreparedAt: plantest.PreparedAt, ExpiresAt: plantest.PreparedAt.Add(PreparationLifetime), DraftDigest: draftSum, Accepted: true,
		Evidence: plantest.Evidence(draft),
		Daemon:   executionplan.DaemonReadiness{ExecutionEndpoint: executionplan.EndpointLocal, Audience: "daemon:ops01:1000", Reachable: true, Status: executionplan.DaemonRunning, Capabilities: []string{"prepare_job"}, Findings: []executionplan.Finding{}},
		Findings: []executionplan.Finding{},
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, _ := NewChannelToken()
	return PrepareResult{Preparation: prep, CredentialChannel: CredentialChannel{Socket: "/run/user/1000/karvi/socket/credentials.sock", Token: tok, ExpiresAt: prep.ExpiresAt, MaxFrame: 4 << 20}}
}

func fixtureCommit(t *testing.T) CommitRequest {
	t.Helper()
	final := plantest.FinalPlan()
	return CommitRequest{Header: plantest.Header(final, executionplan.Committed), Plan: final, Mode: executionplan.ModeLive,
		Preparations: []PreparationReference{{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: plantest.PreparationID, PreparationDigest: final.Preparation[0].PreparationDigest}}}
}

func TestSchema5PayloadsValidateAndMatchTheSchema(t *testing.T) {
	pr := fixturePrepare(t)
	if err := pr.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/prepare_request", pr)
	res := fixturePrepareResult(t)
	if err := res.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/prepare_result", res)
	cr := fixtureCommit(t)
	if err := cr.Validate(); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/commit_request", cr)
	receipt := JobReceipt{JobID: plantest.JobID, AcceptedAt: plantest.FinalizedAt.Add(time.Second), ArtifactDir: "/tmp/jobs/2026-09-14/" + plantest.JobID, PlanDigest: cr.Plan.PlanDigest, Mode: executionplan.ModeLive}
	if err := receipt.Validate(&cr); err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/commit_result", CommitResult{Receipt: receipt})
	req, err := NewRequestForSchema(PrepareCommitSchema, "r1", OpPrepareJob, "0.9.2", pr)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityAt(t, ipcSchema, "#/$defs/request", req)
	// Validation vectors.
	bad := fixturePrepare(t)
	bad.Header.CredentialPackage = &executionplan.PackageReference{Protection: "local-peer", Digest: executionplan.Sum(nil)}
	if err := bad.Validate(); errorcodes.Of(err) != "job_header_invalid" {
		t.Errorf("prepare with package: %v", err)
	}
	bad = fixturePrepare(t)
	bad.Draft.Commands[0] = "changed"
	bad.Draft.CommandPlanDigest = executionplan.SumCommands(bad.Draft.Commands)
	if err := bad.Validate(); errorcodes.Of(err) != "job_header_invalid" {
		t.Errorf("prepare header not matching draft: %v", err)
	}
	badc := fixtureCommit(t)
	badc.Mode = executionplan.ModeExercise
	if err := badc.Validate(); errorcodes.Of(err) != "job_header_invalid" {
		t.Errorf("commit mode differs: %v", err)
	}
	badc = fixtureCommit(t)
	badc.Preparations[0].PreparationDigest = executionplan.Sum([]byte("x"))
	if err := badc.Validate(); errorcodes.Of(err) != "plan_digest_mismatch" {
		t.Errorf("commit with wrong preparation digest: %v", err)
	}
	badc = fixtureCommit(t)
	badc.Preparations = []PreparationReference{}
	if err := badc.Validate(); errorcodes.Of(err) != "plan_digest_mismatch" {
		t.Errorf("commit without preparation reference: %v", err)
	}
	badc = fixtureCommit(t)
	badc.Plan = plantest.DraftPlan()
	if err := badc.Validate(); err == nil {
		t.Error("commit with a draft plan accepted")
	}
	exercise := JobReceipt{JobID: plantest.JobID, AcceptedAt: time.Now(), ArtifactDir: "/x", PlanDigest: cr.Plan.PlanDigest, Mode: executionplan.ModeExercise}
	cr.Mode, cr.Header.Mode = executionplan.ModeExercise, executionplan.ModeExercise
	if err := exercise.Validate(&cr); err == nil || !strings.Contains(err.Error(), "report") {
		t.Errorf("exercise receipt without report: %v", err)
	}
	badr := fixturePrepareResult(t)
	badr.CredentialChannel.Token = ChannelToken{}
	if err := badr.Validate(); errorcodes.Of(err) != "ipc_result_malformed" {
		t.Errorf("result without token: %v", err)
	}
	// The token survives the envelope as hex and nothing else.
	data, _ := json.Marshal(res.CredentialChannel)
	text, _ := res.CredentialChannel.Token.MarshalText()
	if !strings.Contains(string(data), string(text)) {
		t.Error("token not encoded as hex in the envelope")
	}
}
