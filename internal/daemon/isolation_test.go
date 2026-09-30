package daemon

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// TestConcurrentJobsOfOneOperatorKeepTheirGrants: two jobs of one
// operator against the same device, each with
// its own canary password, run at the same time, and every session
// authenticates with its own job's password.
func TestConcurrentJobsOfOneOperatorKeepTheirGrants(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KARVI_TEST_FAKE_DIR", dir)
	f := newV5FixtureWith(t, v5Options{GoFakeDevice: true, MaxJobs: 4, Sets: []string{`security.child-environment-allowlist=["KARVI_TEST_FAKE_DIR"]`}})
	seedA, seedB := canarytest.Seed(t), canarytest.Seed(t)
	devices := []inventory.Device{direct("127.0.0.1")}
	subA := f.prepareAndPackageWith(mustID(t), []string{"show a"}, devices, fixedInput{"alice", seedA.Raw})
	subB := f.prepareAndPackageWith(mustID(t), []string{"show b"}, devices, fixedInput{"bob", seedB.Raw})
	if subA.pkg.Bindings[0].TargetID != subB.pkg.Bindings[0].TargetID {
		t.Fatalf("the two jobs bind different targets: %s %s", subA.pkg.Bindings[0].TargetID, subB.pkg.Bindings[0].TargetID)
	}
	if subA.pkg.Grants[0].CredentialID == subB.pkg.Grants[0].CredentialID {
		t.Fatal("the two jobs share a credential ID")
	}
	for _, sub := range []submission{subA, subB} {
		if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
			t.Fatalf("frame: %+v %v", receipt, err)
		}
	}
	results := make([]ipc.CommitResult, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, sub := range []submission{subA, subB} {
		wg.Add(1)
		go func(i int, sub submission) {
			defer wg.Done()
			results[i], errs[i] = CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
		}(i, sub)
	}
	wg.Wait()
	for i, name := range []string{"a", "b"} {
		if errs[i] != nil {
			t.Fatalf("job %s: %v", name, errs[i])
		}
		terminal := f.followToEnd(results[i].Receipt.JobID)
		if terminal.Outcome.ExitCode != 0 {
			failures, _ := os.ReadFile(filepath.Join(results[i].Receipt.ArtifactDir, "failures.jsonl"))
			t.Fatalf("job %s: outcome=%+v failures=%s", name, terminal.Outcome, failures)
		}
		assertAccounting(t, results[i].Receipt.ArtifactDir, 1, 1)
	}
	log, err := os.ReadFile(filepath.Join(dir, "log"))
	if err != nil {
		t.Fatal(err)
	}
	digest := func(s string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(s))) }
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 {
		t.Fatalf("device log: %q", log)
	}
	want := map[string]string{"show a": digest(seedA.Raw), "show b": digest(seedB.Raw)}
	seen := map[string]bool{}
	for _, line := range lines {
		i := strings.LastIndex(line, " ")
		cmd, sum := line[:i], line[i+1:]
		if want[cmd] != sum {
			t.Errorf("session for %q authenticated with digest %s, want its own job's %s", cmd, sum, want[cmd])
		}
		seen[cmd] = true
	}
	if !seen["show a"] || !seen["show b"] {
		t.Errorf("device log lacks a job: %q", log)
	}
	for _, m := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(dir, "overlap-"+m)); err != nil {
			t.Errorf("session %s did not see the other session running", m)
		}
	}
	if held := f.s.Preparations(); len(held) != 0 {
		t.Errorf("preparations retained: %v", held)
	}
	t.Logf("two concurrent jobs, same target %s: a=%s b=%s, each session saw its own password; both sessions overlapped", subA.pkg.Bindings[0].TargetID, subA.pkg.Grants[0].CredentialID, subB.pkg.Grants[0].CredentialID)
}

// TestCrossUseBetweenJobsIsRefused: every way one job's
// credential context could reach another's is refused at the earliest
// step, and the other job still runs to completion.
func TestCrossUseBetweenJobsIsRefused(t *testing.T) {
	ctx := context.Background()
	protector, _ := credentialpackage.ProtectorFor(credentialpackage.ProtectionLocalPeer)
	commitOK := func(f *v5Fixture, sub submission, name string) {
		t.Helper()
		result, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request)
		if err != nil {
			t.Fatalf("%s was not accepted: %v", name, err)
		}
		if terminal := f.followToEnd(result.Receipt.JobID); terminal.Outcome.ExitCode != 0 {
			t.Fatalf("%s did not complete: %+v", name, terminal.Outcome)
		}
	}
	provideOK := func(f *v5Fixture, sub submission, name string) {
		t.Helper()
		if receipt, err := f.provide(sub); err != nil || !receipt.Accepted {
			t.Fatalf("%s frame: %+v %v", name, receipt, err)
		}
	}
	// 1. A's package on B's channel is refused at the frame (job_id); B
	// then has no package; A completes on its own channel.
	f := newV5Fixture(t)
	a, b := f.prepareAndPackage(mustID(t), []string{"show clock"}), f.prepareAndPackage(mustID(t), []string{"show clock"})
	pa, err := protector.Protect(ctx, a.pkg)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := ProvideCredentials(f.ctx(), b.prepared.CredentialChannel, b.prepared.Preparation.PreparationID, pa)
	pa.Destroy()
	if err != nil || receipt.Accepted || !strings.Contains(receipt.Findings[0].Message, "rule=job_id:") {
		t.Fatalf("A's package on B's channel: %+v %v", receipt, err)
	}
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, b.request); errorcodes.Of(err) != "credential_package_missing" {
		t.Fatalf("B after the foreign frame: %v", err)
	}
	provideOK(f, a, "A")
	commitOK(f, a, "A")
	t.Logf("1: A's package on B's channel: %s; A completed", receipt.Findings[0].Message)

	// 2. B's token under A's preparation is refused; A's token is spent by
	// the attempt; B completes with its own token.
	f = newV5Fixture(t)
	a, b = f.prepareAndPackage(mustID(t), []string{"show clock"}), f.prepareAndPackage(mustID(t), []string{"show clock"})
	pb, _ := protector.Protect(ctx, b.pkg)
	channel := a.prepared.CredentialChannel
	channel.Token = b.prepared.CredentialChannel.Token
	receipt, err = ProvideCredentials(f.ctx(), channel, a.prepared.Preparation.PreparationID, pb)
	pb.Destroy()
	if err != nil || receipt.Accepted || receipt.Findings[0].Code != "credential_channel_token_invalid" {
		t.Fatalf("B's token under A's preparation: %+v %v", receipt, err)
	}
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, a.request); errorcodes.Of(err) != "credential_package_missing" {
		t.Fatalf("A after the spent token: %v", err)
	}
	provideOK(f, b, "B")
	commitOK(f, b, "B")
	t.Logf("2: B's token under A's preparation: %s; B completed", receipt.Findings[0].Code)

	// 3. A's commit referencing B's preparation is refused before execution:
	// the request's reference is not the digest A's plan carries for its
	// own preparation, so the request validator refuses it before the
	// daemon compares packages.
	f = newV5Fixture(t)
	a, b = f.prepareAndPackage(mustID(t), []string{"show clock"}), f.prepareAndPackage(mustID(t), []string{"show clock"})
	provideOK(f, b, "B")
	crossed := a.request
	crossed.Preparations = b.request.Preparations
	_, err = CommitJob(f.ctx(), f.socket, 1<<20, crossed)
	if errorcodes.Of(err) != "plan_digest_mismatch" {
		t.Fatalf("A's commit over B's preparation: %v", err)
	}
	provideOK(f, a, "A")
	commitOK(f, a, "A")
	t.Logf("3: A's commit over B's preparation: %v; A completed", err)

	// 4. B's commit under A's idempotency key is a conflict; B completes
	// under its own key.
	f = newV5Fixture(t)
	a, b = f.prepareAndPackage(mustID(t), []string{"show clock"}), f.prepareAndPackage(mustID(t), []string{"show clock"})
	provideOK(f, a, "A")
	provideOK(f, b, "B")
	commitOK(f, a, "A")
	keyed := b.request
	keyed.Header.IdempotencyKey = a.request.Header.IdempotencyKey
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, keyed); errorcodes.Of(err) != "idempotency_conflict" {
		t.Fatalf("B under A's key: %v", err)
	}
	commitOK(f, b, "B")
	t.Log("4: B under A's idempotency key: idempotency_conflict; B completed under its own")
}
