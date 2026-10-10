package credentialframe

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// recordingConn copies every byte that crosses the JSON socket so the test
// can prove the canary never appears there.
type recordingConn struct {
	net.Conn
	log *bytes.Buffer
}

func (c recordingConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	c.log.Write(b[:n])
	return n, err
}

func (c recordingConn) Write(b []byte) (int, error) {
	c.log.Write(b)
	return c.Conn.Write(b)
}

// fakeDaemon speaks the schema 5 envelope and the credential frame with the
// real payload types and validators, and nothing else; it is the executed
// example of the frame sequence, not the production daemon.
type fakeDaemon struct {
	t          *testing.T
	audience   string
	tokens     map[string]ipc.ChannelToken // preparation -> one-use token
	packages   map[string]credentialpackage.SafePackageProjection
	drafts     map[string]executionplan.Digest
	credential string
}

func (d *fakeDaemon) serveEnvelope(conn net.Conn, jsonLog *bytes.Buffer) {
	rc := recordingConn{Conn: conn, log: jsonLog}
	var req ipc.Request
	if err := ipc.Read(rc, &req, 8<<20); err != nil {
		d.t.Errorf("daemon read: %v", err)
		return
	}
	reply := func(v any) {
		raw, _ := json.Marshal(v)
		_ = ipc.Write(rc, ipc.Response{IPCSchemaVersion: ipc.PrepareCommitSchema, RequestID: req.RequestID, Result: raw}, 8<<20)
	}
	fail := func(code string, err error) {
		_ = ipc.Write(rc, ipc.Response{IPCSchemaVersion: ipc.PrepareCommitSchema, RequestID: req.RequestID, Error: &ipc.Error{Code: code, Message: err.Error(), Details: map[string]any{}}}, 8<<20)
	}
	switch req.Operation {
	case ipc.OpPrepareJob:
		var pr ipc.PrepareRequest
		if err := json.Unmarshal(req.Payload, &pr); err != nil {
			fail("job_request_malformed", err)
			return
		}
		if err := pr.Validate(); err != nil {
			fail("job_rejected", err)
			return
		}
		draftSum, _ := executionplan.SumPlan(pr.Draft)
		report, err := executionplan.Seal(executionplan.PreparationReport{
			PreparationID: plantest.PreparationID, ExecutionEndpoint: executionplan.EndpointLocal, Audience: d.audience,
			PreparedAt: plantest.PreparedAt, ExpiresAt: plantest.PreparedAt.Add(ipc.PreparationLifetime), DraftDigest: draftSum, Accepted: true,
			Evidence: plantest.Evidence(pr.Draft),
			Daemon:   executionplan.DaemonReadiness{ExecutionEndpoint: executionplan.EndpointLocal, Audience: d.audience, Reachable: true, Status: executionplan.DaemonRunning, Version: "0.9.2", IPCSchemaVersion: ipc.PrepareCommitSchema, Capabilities: []string{"prepare_job", "commit_job"}, Findings: []executionplan.Finding{}},
			Findings: []executionplan.Finding{},
		})
		if err != nil {
			fail("internal_invariant", err)
			return
		}
		tok, _ := ipc.NewChannelToken()
		d.tokens[report.PreparationID] = tok
		d.drafts[report.PreparationID] = draftSum
		reply(ipc.PrepareResult{Preparation: report, CredentialChannel: ipc.CredentialChannel{Socket: d.credential, Token: tok, ExpiresAt: report.ExpiresAt, MaxFrame: MaxBody}})
	case ipc.OpCommitJob:
		var cr ipc.CommitRequest
		if err := json.Unmarshal(req.Payload, &cr); err != nil {
			fail("job_request_malformed", err)
			return
		}
		if err := cr.Validate(); err != nil {
			fail("job_rejected", err)
			return
		}
		pkg, ok := d.packages[cr.Preparations[0].PreparationID]
		if !ok || cr.Header.CredentialPackage.Digest != pkg.PackageDigest || pkg.PlanDigest != cr.Plan.PlanDigest {
			fail("credential_package_invalid", io.ErrUnexpectedEOF)
			return
		}
		reply(ipc.CommitResult{Receipt: ipc.JobReceipt{JobID: cr.Header.JobID, AcceptedAt: plantest.FinalizedAt.Add(time.Second), ArtifactDir: "/tmp/jobs/2026-09-14/" + cr.Header.JobID, PlanDigest: cr.Plan.PlanDigest, Mode: cr.Mode, ReportPath: "/tmp/jobs/2026-09-14/" + cr.Header.JobID + "/exercise.json"}})
	case ipc.OpProvideCredentials:
		fail("credential_channel_required", io.ErrUnexpectedEOF)
	default:
		fail("ipc_operation_unknown", io.ErrUnexpectedEOF)
	}
}

func (d *fakeDaemon) serveFrame(conn *net.UnixConn, frameLog *bytes.Buffer) {
	defer conn.Close()
	if _, err := ipc.PeerUID(conn); err != nil {
		d.t.Error(err)
		return
	}
	h, body, err := Read(io.TeeReader(conn, frameLog), Limits{})
	if err != nil {
		d.t.Errorf("frame read: %v", err)
		return
	}
	reject := func(code, msg string) {
		var rbuf bytes.Buffer
		_ = WriteReceipt(&rbuf, Receipt{PackageID: h.Envelope.PackageID, PackageDigest: h.Envelope.PackageDigest, Accepted: false, Findings: []executionplan.Finding{{Code: code, Severity: executionplan.SeverityError, Stage: "credential_package", Message: msg}}})
		_, _ = conn.Write(rbuf.Bytes())
	}
	tok, ok := d.tokens[h.PreparationID]
	if !ok {
		reject("preparation_unknown", "no preparation holds this token; a token is one use")
		return
	}
	delete(d.tokens, h.PreparationID) // one use, whatever follows
	if err := h.VerifyToken(tok); err != nil {
		reject("credential_channel_token_invalid", err.Error())
		return
	}
	protector, err := credentialpackage.ProtectorFor(h.Envelope.Protection)
	if err != nil {
		d.t.Error(err)
		return
	}
	pkg, err := protector.Unprotect(context.Background(), credentialpackage.Protected{Envelope: h.Envelope, Body: body}, h.Envelope.Audience[0])
	if err != nil {
		d.t.Error(err)
		return
	}
	defer pkg.Destroy()
	proj, err := pkg.SafeProjection()
	if err != nil {
		d.t.Error(err)
		return
	}
	d.packages[h.PreparationID] = proj
	var rbuf bytes.Buffer
	_ = WriteReceipt(&rbuf, Receipt{PackageID: proj.PackageID, PackageDigest: proj.PackageDigest, GrantCount: len(pkg.Grants), BindingCount: len(pkg.Bindings), Accepted: true})
	_, _ = conn.Write(rbuf.Bytes())
}

func TestPrepareFrameCommitSequence(t *testing.T) {
	seed := canarytest.Seed(t)
	dir := testsocket.Dir(t)
	envelopePath, framePath := filepath.Join(dir, "d.sock"), filepath.Join(dir, "c.sock")
	d := &fakeDaemon{t: t, audience: "daemon:ops01:1000", tokens: map[string]ipc.ChannelToken{}, packages: map[string]credentialpackage.SafePackageProjection{}, drafts: map[string]executionplan.Digest{}, credential: framePath}
	var jsonLog, frameLog bytes.Buffer
	envelopeListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: envelopePath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer envelopeListener.Close()
	frameListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: framePath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer frameListener.Close()
	go func() {
		for {
			conn, err := envelopeListener.AcceptUnix()
			if err != nil {
				return
			}
			d.serveEnvelope(conn, &jsonLog)
			conn.Close()
		}
	}()
	go func() {
		for {
			conn, err := frameListener.AcceptUnix()
			if err != nil {
				return
			}
			d.serveFrame(conn, &frameLog)
		}
	}()
	call := func(op string, payload any, out any) {
		t.Helper()
		req, err := ipc.NewRequestForSchema(ipc.PrepareCommitSchema, op+"-1", op, "0.9.2", payload)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := contextWithTimeout(5 * time.Second)
		defer cancel()
		if err := ipc.CallForSchema(ctx, envelopePath, req, ipc.PrepareCommitSchema, 8<<20, out); err != nil {
			t.Fatalf("%s: %v", op, err)
		}
	}

	// 1. prepare_job with the draft.
	draft := plantest.DraftPlan()
	prepareReq := ipc.PrepareRequest{Header: plantest.Header(draft, executionplan.Draft), Draft: draft}
	var prepared ipc.PrepareResult
	call(ipc.OpPrepareJob, prepareReq, &prepared)
	if err := prepared.Validate(); err != nil {
		t.Fatal(err)
	}

	// 2. Incorporate the evidence, bind, finalize.
	plan, err := executionplan.IncorporatePreparation(draft, prepared.Preparation.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	for i := range plan.Targets {
		if plan.Targets[i].TargetID == "name:core-a" {
			plan.Targets[i].CredentialBindingID = plantest.GrantB
		} else {
			plan.Targets[i].CredentialBindingID = plantest.GrantA
		}
		plan.Targets[i].SessionInitProfile = executionplan.SessionInitNone
	}
	final, err := executionplan.Finalize(plan, plantest.FinalizedAt)
	if err != nil {
		t.Fatal(err)
	}

	// 3. The package over the credential socket.
	pkg := fixturePackage(seed.Raw)
	pkg.PlanDigest = final.PlanDigest
	proj, err := pkg.SafeProjection()
	if err != nil {
		t.Fatal(err)
	}
	protector, _ := credentialpackage.ProtectorFor(credentialpackage.ProtectionLocalPeer)
	protected, err := protector.Protect(context.Background(), pkg)
	if err != nil {
		t.Fatal(err)
	}
	defer protected.Destroy()
	fconn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: prepared.CredentialChannel.Socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(fconn, Header{PreparationID: prepared.Preparation.PreparationID, Token: prepared.CredentialChannel.Token, Envelope: protected.Envelope}, protected.Body); err != nil {
		t.Fatal(err)
	}
	receipt, err := ReadReceipt(fconn)
	fconn.Close()
	if err != nil || !receipt.Accepted || receipt.PackageDigest != proj.PackageDigest {
		t.Fatalf("receipt %+v: %v", receipt, err)
	}

	// 4. commit_job in exercise mode.
	header := plantest.Header(final, executionplan.Committed)
	header.Mode = executionplan.ModeExercise
	header.CredentialPackage = &executionplan.PackageReference{Protection: executionplan.ProtectionLocalPeer, Digest: receipt.PackageDigest}
	commitReq := ipc.CommitRequest{Header: header, Plan: final, Mode: executionplan.ModeExercise, Preparations: []ipc.PreparationReference{{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: prepared.Preparation.PreparationID, PreparationDigest: prepared.Preparation.PreparationDigest}}}
	var committed ipc.CommitResult
	call(ipc.OpCommitJob, commitReq, &committed)
	if err := committed.Receipt.Validate(&commitReq); err != nil {
		t.Fatal(err)
	}

	// 5. A second frame for the same preparation is refused (one use).
	fconn2, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: framePath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	_ = Write(fconn2, Header{PreparationID: prepared.Preparation.PreparationID, Token: prepared.CredentialChannel.Token, Envelope: protected.Envelope}, protected.Body)
	second, err := ReadReceipt(fconn2)
	fconn2.Close()
	if err != nil || second.Accepted || len(second.Findings) != 1 || second.Findings[0].Code != "preparation_unknown" {
		t.Errorf("a second frame for the same preparation must be refused with a finding: %+v %v", second, err)
	}

	// The canary crossed the frame socket only.
	if hits := canary.Scan("json socket", jsonLog.Bytes(), seed); len(hits) != 0 {
		t.Fatalf("the JSON socket carried the canary: %v", hits)
	}
	if !canary.Found(frameLog.Bytes(), seed) {
		t.Fatal("the frame socket did not carry the package")
	}
	// The envelopes, with the token redacted, and the frame prefix.
	tokenText, _ := prepared.CredentialChannel.Token.MarshalText()
	redacted := strings.ReplaceAll(jsonLog.String(), string(tokenText), "<redacted>")
	for _, line := range strings.Split(strings.TrimSpace(redacted), "\n") {
		t.Logf("envelope: %s", truncateLine(line, 300))
	}
	t.Logf("frame prefix: %s", hex.EncodeToString(frameLog.Bytes()[:prefixLen]))
	t.Logf("package receipt: %+v", receipt)
	t.Logf("job receipt: %+v", committed.Receipt)
	if !regexp.MustCompile(`"operation":"prepare_job"`).MatchString(redacted) || !strings.Contains(redacted, `"operation":"commit_job"`) {
		t.Fatal("both envelope operations must appear on the JSON socket")
	}
}

func truncateLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func contextWithTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}
