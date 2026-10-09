package daemon

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend"
	"github.com/robert-patrick-texas/karvi/internal/credentialframe"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// fakeSSH answers the interactive session the way the k03 fake device does.
const fakeSSH = `#!/bin/sh
case " $* " in
  *" -O check "*) exit 1 ;;
  *" BatchMode=no "*)
    printf '127.0.0.1#'
    while IFS= read -r line; do
      [ "$line" = exit ] && exit 0
      printf '%s\r\nok\r\n127.0.0.1#' "$line"
    done
    exit 0 ;;
esac
printf 'ok\n'
`

type envInput struct{}

func (envInput) LookupEnv(_ context.Context, name string) (string, bool, error) {
	switch name {
	case "NETUSER":
		return "u", true, nil
	case "NETPASS":
		return "p", true, nil
	}
	return "", false, nil
}
func (envInput) Prompt(context.Context, credentialbackend.PromptRequest) (string, error) {
	return "", os.ErrNotExist
}

// v5Fixture is a schema 5 daemon on the test socket helper with a fake
// device, an injected lookup, and a movable clock, plus the client-side
// state a submission needs.
type v5Fixture struct {
	t        *testing.T
	s        *Server
	socket   string
	cfg      configload.Snapshot
	operator credentials.Operator
	clock    time.Time
	lookups  int
}

// v5Options vary the fixture: the Go fake device (the test binary as ssh
// and as the askpass helper), extra configuration sets, and the job
// limit.
type v5Options struct {
	GoFakeDevice bool
	Sets         []string
	MaxJobs      int
	// MaxFrame is daemon.max-ipc-frame-bytes for the server; zero is the
	// default 8 MiB.
	MaxFrame int64
	Signals  chan os.Signal
	// MaxPreparations bounds the preparation table; set before Serve starts.
	MaxPreparations int
}

func newV5Fixture(t *testing.T) *v5Fixture { return newV5FixtureWith(t, v5Options{}) }

func newV5FixtureWith(t *testing.T, opts v5Options) *v5Fixture {
	t.Helper()
	dir := testsocket.Dir(t)
	state := t.TempDir()
	fake := filepath.Join(state, "fake-ssh")
	if opts.GoFakeDevice {
		linkTestBinary(t, fake)
	} else if err := os.WriteFile(fake, []byte(fakeSSH), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"home", "base", "sb", "cap"} {
		if err := os.MkdirAll(filepath.Join(state, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	// The system transport locates karvi-askpass on PATH, as the smoke suites
	// provide it; a stub is enough for the fake device.
	if opts.GoFakeDevice {
		linkTestBinary(t, filepath.Join(state, "karvi-askpass"))
	} else if err := os.WriteFile(filepath.Join(state, "karvi-askpass"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", state+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, err := configload.Load(configload.Options{HomeDir: filepath.Join(state, "home"), SkipAuto: true, Environment: []string{}, Sets: append([]string{
		`basedir="` + filepath.Join(state, "base") + `"`, `ssh.transports.system="` + fake + `"`, `ssh.run.transport="system"`, `ssh.host-key-policy="insecure"`,
		"audit.journald-required=false", `audit.file="` + filepath.Join(state, "audit.jsonl") + `"`, `scoreboards="` + filepath.Join(state, "sb") + `"`,
		`sessions.shared-capacity-root="` + filepath.Join(state, "cap") + `"`, "output.min-free-bytes-after-job=0", "name.allow-daemon-resolution=true", "name.address-family-preference=ipv4", "display.color=never",
	}, opts.Sets...)})
	if err != nil {
		t.Fatal(err)
	}
	operator, err := osutil.CurrentOperator()
	if err != nil {
		t.Fatal(err)
	}
	operator.Home = filepath.Join(state, "home")
	f := &v5Fixture{t: t, cfg: cfg, operator: operator, clock: time.Now()}
	caps := resolver.Capabilities{IPv4: true, IPv6: true}
	f.s = &Server{Socket: filepath.Join(dir, "daemon.sock"), StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), MaxJobs: opts.MaxJobs, MaxFrame: opts.MaxFrame, MaxPreparations: opts.MaxPreparations, Signals: opts.Signals, Config: cfg, Operator: operator,
		Clock: func() time.Time { return f.clock }, Capabilities: &caps,
		Lookup: func(_ context.Context, _, host string) ([]netip.Addr, error) {
			f.lookups++
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		}}
	f.socket = f.s.Socket
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = f.s.Serve(ctx) }()
	t.Cleanup(func() { stop(); <-done })
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ipc.Wait(waitCtx, f.socket); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *v5Fixture) ctx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	f.t.Cleanup(cancel)
	return ctx
}

func direct(name string) inventory.Device {
	d := inventory.Direct(name, "generic", "system", 0)
	d.TransportExplicit = true
	return d
}

// draft plans one client-authority literal and one daemon-authority name.
func (f *v5Fixture) draft(jobID string, commands []string) (executionplan.ExecutionPlan, executionplan.PublicJobHeader, *planner.CredentialPlanner, []inventory.Device) {
	return f.draftWith(jobID, commands, []inventory.Device{direct("127.0.0.1"), direct("core-a.example")}, envInput{})
}

// draftWith plans the given devices with the given input provider; a
// device named core-a.example is planned under daemon authority.
func (f *v5Fixture) draftWith(jobID string, commands []string, devices []inventory.Device, input credentialbackend.InputProvider) (executionplan.ExecutionPlan, executionplan.PublicJobHeader, *planner.CredentialPlanner, []inventory.Device) {
	f.t.Helper()
	overrides := map[string]string{}
	for _, d := range devices {
		if d.CanonicalName == "core-a.example" {
			overrides[d.ID] = "daemon"
		}
	}
	opts := planner.DraftOptions{ActivityType: "run", Commands: commands, Follow: true, Address: planner.AddressOptions{Overrides: overrides, Capabilities: &resolver.Capabilities{IPv4: true, IPv6: true}}}
	draft, err := planner.Draft(context.Background(), f.cfg, f.operator, planner.TargetSet{Devices: devices, Order: executionplan.OrderDefault}, opts, f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	cp, err := planner.NewCredentialPlanner(f.cfg, f.operator, devices, f.clock, planner.CredentialOptions{Input: input})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := cp.Resolve(context.Background(), draft); err != nil {
		f.t.Fatal(err)
	}
	header, err := planner.Header(draft, jobID, executionplan.ModeLive)
	if err != nil {
		f.t.Fatal(err)
	}
	return draft, header, cp, devices
}

type submission struct {
	prepared ipc.PrepareResult
	final    executionplan.ExecutionPlan
	commit   executionplan.PublicJobHeader
	pkg      credentialpackage.CredentialPackage
	proj     credentialpackage.SafePackageProjection
	request  ipc.CommitRequest
}

// prepareAndPackage runs the client sequence up to the frame.
func (f *v5Fixture) prepareAndPackage(jobID string, commands []string) submission {
	return f.prepareAndPackageWith(jobID, commands, []inventory.Device{direct("127.0.0.1"), direct("core-a.example")}, envInput{})
}

func (f *v5Fixture) prepareAndPackageWith(jobID string, commands []string, devices []inventory.Device, input credentialbackend.InputProvider) submission {
	return f.prepareAndPackageMode(jobID, commands, devices, input, executionplan.ModeLive)
}

// prepareAndPackageMode is prepareAndPackageWith under the given mode in
// every header and the commit request.
func (f *v5Fixture) prepareAndPackageMode(jobID string, commands []string, devices []inventory.Device, input credentialbackend.InputProvider, mode executionplan.Mode) submission {
	f.t.Helper()
	draft, header, cp, _ := f.draftWith(jobID, commands, devices, input)
	header.Mode = mode
	prepared, err := PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft})
	if err != nil {
		f.t.Fatal(err)
	}
	if !prepared.Preparation.Accepted {
		f.t.Fatalf("not accepted: %+v", prepared.Preparation.Findings)
	}
	plan, err := executionplan.IncorporatePreparation(draft, prepared.Preparation.Evidence)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := cp.Resolve(context.Background(), plan); err != nil {
		f.t.Fatal(err)
	}
	bound, err := cp.Bind(plan)
	if err != nil {
		f.t.Fatal(err)
	}
	final, err := executionplan.Finalize(bound, f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	finalHeader, _ := planner.Header(final, jobID, mode)
	host, _ := os.Hostname()
	pkg, err := cp.Package(final, finalHeader, prepared.Preparation.Audience, host, f.clock)
	if err != nil {
		f.t.Fatal(err)
	}
	ref, proj, err := planner.PackageReference(pkg)
	if err != nil {
		f.t.Fatal(err)
	}
	commit, _ := planner.CommitHeader(final, jobID, mode, ref)
	request := ipc.CommitRequest{Header: commit, Plan: final, Mode: mode, Preparations: []ipc.PreparationReference{{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: prepared.Preparation.PreparationID, PreparationDigest: prepared.Preparation.PreparationDigest}}}
	return submission{prepared: prepared, final: final, commit: commit, pkg: pkg, proj: proj, request: request}
}

func (f *v5Fixture) provide(sub submission) (credentialframe.Receipt, error) {
	f.t.Helper()
	protector, _ := credentialpackage.ProtectorFor(credentialpackage.ProtectionLocalPeer)
	protected, err := protector.Protect(context.Background(), sub.pkg)
	if err != nil {
		f.t.Fatal(err)
	}
	defer protected.Destroy()
	return ProvideCredentials(f.ctx(), sub.prepared.CredentialChannel, sub.prepared.Preparation.PreparationID, protected)
}

// mustID is a job ID for a test: the stamp of now and a sequence from a
// counter, so two jobs of one test never share a name (the server holds
// one job per ID) without a reservation on disk.
var testJobSequence atomic.Int64

func mustID(t *testing.T) string {
	t.Helper()
	n := testJobSequence.Add(1) % 100
	return fmt.Sprintf("%s-%02d", osutil.JobIDStamp(time.Now(), time.UTC), n)
}

// TestSchema5SequenceRunsAJob drives the real primitives through prepare,
// the frame, and commit against the real server: the
// daemon-authority target is resolved at prepare with the injected lookup,
// the job runs against the fake device, and a replay of the commit returns
// the identical receipt.
func TestSchema5SequenceRunsAJob(t *testing.T) {
	f := newV5Fixture(t)
	jobID := mustID(t)
	sub := f.prepareAndPackage(jobID, []string{"show clock"})
	if f.lookups != 1 {
		t.Fatalf("daemon lookups=%d, want 1 for the daemon-authority target", f.lookups)
	}
	ev := sub.prepared.Preparation.Evidence.Addresses
	if len(ev) != 1 || ev[0].TargetID != "name:core-a.example" || ev[0].SelectedSource != executionplan.SourceDNSDaemon || ev[0].Selected.String() != "127.0.0.1" {
		t.Fatalf("evidence=%+v", ev)
	}
	if d := sub.prepared.Preparation.Daemon; d.IPCSchemaVersion != ipc.SchemaVersion || len(d.Capabilities) != 3 || d.Audience != f.s.Audience() {
		t.Fatalf("readiness=%+v", d)
	}
	receipt, err := f.provide(sub)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckReceipt(receipt, sub.proj); err != nil || receipt.GrantCount != 1 || receipt.BindingCount != 2 {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
	result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.JobID != jobID || result.Receipt.ArtifactDir == "" {
		t.Fatalf("receipt=%+v", result.Receipt)
	}
	// The commit answered at acceptance: the manifest is durable already,
	// and the outcome arrives through follow_job.
	if _, err := os.Stat(filepath.Join(result.Receipt.ArtifactDir, "manifest.json")); err != nil {
		t.Fatal(err)
	}
	terminal := f.followToEnd(jobID)
	if terminal.Outcome.ExitCode != 0 || terminal.Outcome.JobID != jobID {
		errs, _ := os.ReadFile(filepath.Join(result.Receipt.ArtifactDir, "errors.jsonl"))
		t.Fatalf("outcome=%+v errors=%s", terminal.Outcome, errs)
	}
	if len(f.s.Preparations()) != 0 {
		t.Fatalf("preparation retained after commit: %v", f.s.Preparations())
	}
	assertAccounting(t, result.Receipt.ArtifactDir, 2, 1)
	// Replay under the same key returns the identical receipt.
	again, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again.Receipt, result.Receipt) {
		t.Fatalf("replay differs: %+v vs %+v", again.Receipt, result.Receipt)
	}
	// A different plan under the same key is a conflict; the same job ID
	// under another key too.
	other := f.prepareAndPackage(jobID, []string{"show version"})
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, other.request); errorcodes.Of(err) != "idempotency_conflict" {
		t.Fatalf("different digest under the same key: %v", err)
	}
	foreign := other.request
	foreign.Header.IdempotencyKey = "other-key"
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, foreign); errorcodes.Of(err) != "job_id_conflict" {
		t.Fatalf("job ID under another key: %v", err)
	}
	t.Logf("job %s exit=%s artifacts=%s", result.Receipt.JobID, terminal.Outcome.ExitName, result.Receipt.ArtifactDir)
}

func TestSchema5RefusalVectors(t *testing.T) {
	f := newV5Fixture(t)
	// commit before the frame
	sub := f.prepareAndPackage(mustID(t), []string{"show clock"})
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request); errorcodes.Of(err) != "credential_package_missing" {
		t.Fatalf("commit before frame: %v", err)
	}
	// the frame, then token reuse
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("frame: %+v %v", receipt, err)
	}
	if receipt, err := f.provide(sub); err != nil || receipt.Accepted || receipt.Findings[0].Code != "credential_channel_token_invalid" {
		t.Fatalf("token reuse: %+v %v", receipt, err)
	}
	// mismatched preparation digest
	bad := sub.request
	bad.Preparations = []ipc.PreparationReference{{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: sub.prepared.Preparation.PreparationID, PreparationDigest: executionplan.Sum([]byte("x"))}}
	bad.Plan.Preparation[0].PreparationDigest = executionplan.Sum([]byte("x"))
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, bad); errorcodes.Of(err) != "plan_digest_mismatch" {
		t.Fatalf("mismatched preparation digest: %v", err)
	}
	// unknown preparation on the frame
	unknown := sub
	unknown.prepared.Preparation.PreparationID = plantest.PreparationID
	if receipt, err := f.provide(unknown); err != nil || receipt.Accepted || receipt.Findings[0].Code != "preparation_unknown" {
		t.Fatalf("unknown preparation: %+v %v", receipt, err)
	}
	// expiry through the clock: the preparation and its frame
	late := f.prepareAndPackage(mustID(t), []string{"show clock"})
	f.clock = f.clock.Add(ipc.PreparationLifetime + time.Minute)
	if receipt, err := f.provide(late); err != nil || receipt.Accepted || receipt.Findings[0].Code != "preparation_expired" {
		t.Fatalf("expired frame: %+v %v", receipt, err)
	}
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, late.request); errorcodes.Of(err) != "preparation_unknown" && errorcodes.Of(err) != "preparation_expired" {
		t.Fatalf("expired commit: %v", err)
	}
	f.clock = time.Now()
	// sealed refused at the frame: a raw header the client codec would not write
	sealed := f.prepareAndPackage(mustID(t), []string{"show clock"})
	env := sealed.proj.Envelope()
	env.Protection = credentialpackage.ProtectionSealed
	env.Sealed = &credentialpackage.SealedPayload{KeyID: "k", Nonce: []byte{1}, Ciphertext: []byte{2}}
	env.PackageDigest = executionplan.Sum(env.Sealed.Ciphertext)
	receipt := rawFrame(t, sealed.prepared.CredentialChannel.Socket, credentialframe.Header{PreparationID: sealed.prepared.Preparation.PreparationID, Token: sealed.prepared.CredentialChannel.Token, Envelope: env}, []byte("{}"))
	if receipt.Accepted || len(receipt.Findings) != 1 || receipt.Findings[0].Code != "credential_package_invalid" || !strings.Contains(receipt.Findings[0].Message, "protection_unsupported") {
		t.Fatalf("sealed: %+v", receipt)
	}
	// submit_job, a provide_credentials envelope, a foreign header UID
	for _, tc := range []struct{ op, code string }{{"submit_job", "ipc_operation_unknown"}, {ipc.OpProvideCredentials, "credential_channel_required"}} {
		req, _ := ipc.NewRequest(mustID(t), tc.op, "0.9.2", map[string]any{})
		var out json.RawMessage
		if err := ipc.Call(f.ctx(), f.socket, req, 1<<20, &out); errorcodes.Of(err) != tc.code {
			t.Fatalf("%s: %v", tc.op, err)
		}
	}
	draft, header, _, _ := f.draft(mustID(t), []string{"show clock"})
	header.Operator.UID = os.Geteuid() + 1
	if _, err := PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft}); errorcodes.Of(err) != "peer_uid_denied" {
		t.Fatalf("foreign header uid: %v", err)
	}
}

func TestSchema5PreparationTableBound(t *testing.T) {
	// The bound is set through the fixture before Serve starts; setting it
	// on the running server raced with Serve's default assignment.
	f := newV5FixtureWith(t, v5Options{MaxPreparations: 1})
	f.prepareAndPackage(mustID(t), []string{"show clock"})
	draft, header, _, _ := f.draft(mustID(t), []string{"show clock"})
	if _, err := PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft}); errorcodes.Of(err) != "job_rejected" {
		t.Fatalf("table bound: %v", err)
	}
}

// TestSchema5DaemonDNSFailureIsAFinding: a daemon-authority name the daemon
// cannot resolve makes the preparation unaccepted with a daemon_dns finding
// naming the target, and no channel.
func TestSchema5DaemonDNSFailureIsAFinding(t *testing.T) {
	f := newV5Fixture(t)
	f.s.Lookup = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	draft, header, _, _ := f.draft(mustID(t), []string{"show clock"})
	prepared, err := PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Preparation.Accepted || len(prepared.Preparation.Findings) != 1 || prepared.Preparation.Findings[0].Code != "dns_nxdomain" || prepared.Preparation.Findings[0].Stage != "daemon_dns" || prepared.Preparation.Findings[0].TargetID != "name:core-a.example" || prepared.CredentialChannel.Socket != "" {
		t.Fatalf("prepared=%+v", prepared)
	}
}

// TestClientRefusals covers the client's refusals: a foreign draft
// digest, an unaccepted receipt, a receipt naming another package, and a
// credential channel whose peer reports a different UID.
func TestClientRefusals(t *testing.T) {
	socket := filepath.Join(testsocket.Dir(t), "daemon.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	draft := plantest.DraftPlan()
	header := plantest.Header(draft, executionplan.Draft)
	go func() {
		for {
			conn, err := listener.AcceptUnix()
			if err != nil {
				return
			}
			var req ipc.Request
			_ = ipc.Read(conn, &req, 1<<20)
			report, _ := executionplan.Seal(executionplan.PreparationReport{PreparationID: plantest.PreparationID, ExecutionEndpoint: executionplan.EndpointLocal, Audience: "daemon:ops01:1000", PreparedAt: plantest.PreparedAt, ExpiresAt: plantest.PreparedAt.Add(ipc.PreparationLifetime), DraftDigest: executionplan.Sum([]byte("foreign")), Accepted: true, Evidence: plantest.Evidence(draft), Daemon: executionplan.DaemonReadiness{ExecutionEndpoint: executionplan.EndpointLocal, Reachable: true, Status: executionplan.DaemonRunning, Capabilities: []string{}, Findings: []executionplan.Finding{}}, Findings: []executionplan.Finding{}})
			tok, _ := ipc.NewChannelToken()
			raw, _ := json.Marshal(ipc.PrepareResult{Preparation: report, CredentialChannel: ipc.CredentialChannel{Socket: socket, Token: tok, ExpiresAt: report.ExpiresAt, MaxFrame: 1}})
			_ = ipc.Write(conn, ipc.Response{IPCSchemaVersion: ipc.SchemaVersion, RequestID: req.RequestID, Result: raw}, 1<<20)
			conn.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := PrepareJob(ctx, socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft}); errorcodes.Of(err) != "plan_digest_mismatch" {
		t.Fatalf("foreign draft digest: %v", err)
	}
	proj := credentialpackage.SafePackageProjection{PackageID: plantest.GrantA, PackageDigest: executionplan.Sum([]byte("p"))}
	if err := CheckReceipt(credentialframe.Receipt{Accepted: false, Findings: []executionplan.Finding{{Code: "credential_package_expired", Message: "closed"}}}, proj); errorcodes.Of(err) != "credential_package_expired" {
		t.Fatalf("unaccepted receipt: %v", err)
	}
	if err := CheckReceipt(credentialframe.Receipt{Accepted: true, PackageID: plantest.GrantB, PackageDigest: proj.PackageDigest}, proj); errorcodes.Of(err) != "ipc_result_malformed" {
		t.Fatalf("receipt naming another package: %v", err)
	}
	if err := CheckReceipt(credentialframe.Receipt{Accepted: true, PackageID: proj.PackageID, PackageDigest: proj.PackageDigest}, proj); err != nil {
		t.Fatal(err)
	}
	// The client's own peer check refuses a channel whose peer is another UID.
	saved := peerUID
	peerUID = func(*net.UnixConn) (uint32, error) { return uint32(os.Geteuid() + 1), nil }
	defer func() { peerUID = saved }()
	body := credentials.NewSecretBytes([]byte("x"))
	if _, err := ProvideCredentials(ctx, ipc.CredentialChannel{Socket: socket, ExpiresAt: time.Now().Add(time.Minute)}, plantest.PreparationID, credentialpackage.Protected{Body: body}); errorcodes.Of(err) != "peer_uid_denied" {
		t.Fatalf("foreign peer uid: %v", err)
	}
}

// rawFrame writes a frame the client codec would refuse, so the daemon's
// own checks are exercised, and returns the receipt.
func rawFrame(t *testing.T, socket string, h credentialframe.Header, body []byte) credentialframe.Receipt {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	header, _ := json.Marshal(h)
	var buf bytes.Buffer
	prefix := make([]byte, 16)
	binary.BigEndian.PutUint32(prefix[0:4], credentialframe.Magic)
	binary.BigEndian.PutUint16(prefix[4:6], credentialframe.FrameVersion)
	binary.BigEndian.PutUint32(prefix[8:12], uint32(len(header)))
	binary.BigEndian.PutUint32(prefix[12:16], uint32(len(body)))
	buf.Write(prefix)
	buf.Write(header)
	buf.Write(body)
	if _, err := conn.Write(buf.Bytes()); err != nil {
		t.Fatal(err)
	}
	receipt, err := credentialframe.ReadReceipt(conn)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

// TestProvidedStageRefusesAtTheFrame: the three
// packages the frame once accepted are refused at the frame with
// their rule named, a commit then finds no package, a second frame finds
// the token used, and a wrong plan digest passes the frame and fails only
// at commit.
func TestProvidedStageRefusesAtTheFrame(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(sub *submission)
		rule   string
	}{
		{"binding to a target outside the plan", func(sub *submission) { sub.pkg.Bindings[0].TargetID = "name:foreign" }, "binding_target"},
		{"audience of another daemon", func(sub *submission) { sub.pkg.Audience = []string{"daemon:elsewhere:1"} }, "audience"},
		{"issuer uid differs from the header", func(sub *submission) { sub.pkg.Issuer.UID++ }, "issuer"},
		{"grant unbound", func(sub *submission) {
			extra := sub.pkg.Grants[0]
			extra.CredentialID = plantest.GrantB
			sub.pkg.Grants = append(sub.pkg.Grants, extra)
		}, "grant_unbound"},
	} {
		f := newV5Fixture(t)
		sub := f.prepareAndPackage(mustID(t), []string{"show clock"})
		tc.mutate(&sub)
		receipt, err := f.provide(sub)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if receipt.Accepted || len(receipt.Findings) != 1 || receipt.Findings[0].Code != "credential_package_invalid" || !strings.Contains(receipt.Findings[0].Message, "rule="+tc.rule+":") {
			t.Fatalf("%s: %+v", tc.name, receipt)
		}
		t.Logf("%s: refused at the frame: %s", tc.name, receipt.Findings[0].Message)
		ref, _, err := planner.PackageReference(sub.pkg)
		if err != nil {
			t.Fatal(err)
		}
		sub.request.Header, _ = planner.CommitHeader(sub.final, sub.request.Header.JobID, executionplan.ModeLive, ref)
		if _, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request); errorcodes.Of(err) != "credential_package_missing" {
			t.Fatalf("%s: commit after refusal: %v", tc.name, err)
		}
		if receipt, err := f.provide(sub); err != nil || receipt.Accepted || receipt.Findings[0].Code != "credential_channel_token_invalid" {
			t.Fatalf("%s: second frame: %+v %v", tc.name, receipt, err)
		}
	}
	// The split: a wrong plan digest is not a provided-stage rule.
	f := newV5Fixture(t)
	sub := f.prepareAndPackage(mustID(t), []string{"show clock"})
	sub.pkg.PlanDigest = executionplan.Sum([]byte("x"))
	if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
		t.Fatalf("wrong plan digest at the frame: %+v %v", receipt, err)
	}
	ref, _, _ := planner.PackageReference(sub.pkg)
	sub.request.Header, _ = planner.CommitHeader(sub.final, sub.request.Header.JobID, executionplan.ModeLive, ref)
	_, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
	if errorcodes.Of(err) != "credential_package_invalid" || !strings.Contains(err.Error(), "rule=plan_digest") {
		t.Fatalf("wrong plan digest at commit: %v", err)
	}
	t.Logf("wrong plan digest: accepted at the frame, refused at commit: %v", err)
}
