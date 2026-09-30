package executionplan

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

func fixtureReadiness() DaemonReadiness {
	return DaemonReadiness{ExecutionEndpoint: EndpointLocal, Audience: "daemon:ops01:1000", Reachable: true, Status: DaemonRunning, PID: 4100, Version: "0.9.2", IPCSchemaVersion: 5, Capabilities: []string{"prepare_job", "commit_job"}, Findings: []Finding{}}
}

func fixturePreparation(t *testing.T) PreparationReport {
	t.Helper()
	draft := fixtureDraftPlan(t)
	ev := fixtureEvidence(t, draft)
	draftSum, _ := SumPlan(draft)
	r := PreparationReport{
		PreparationID: fixturePrepID, ExecutionEndpoint: EndpointLocal, Audience: "daemon:ops01:1000",
		PreparedAt: ev.PreparedAt, ExpiresAt: ev.PreparedAt.Add(10 * time.Minute), DraftDigest: draftSum, Accepted: true,
		Evidence: ev, Daemon: fixtureReadiness(), Findings: []Finding{},
	}
	sealed, err := Seal(r)
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func TestPreparationReportSealsAndValidates(t *testing.T) {
	r := fixturePreparation(t)
	if err := r.Validate("preparation"); err != nil {
		t.Fatal(err)
	}
	if r.PreparationDigest.IsZero() || r.Evidence.PreparationDigest != r.PreparationDigest {
		t.Fatal("Seal did not fill both digests")
	}
	tampered := r
	tampered.Audience = "daemon:other:1000"
	if err := tampered.Validate("preparation"); err == nil || !strings.Contains(err.Error(), "preparation_digest") {
		t.Fatalf("tampered report: %v", err)
	}
	cases := []struct {
		name  string
		edit  func(*PreparationReport)
		field string
	}{
		{"id", func(p *PreparationReport) { p.PreparationID = "prep" }, "preparation.preparation_id"},
		{"endpoint", func(p *PreparationReport) { p.ExecutionEndpoint = "remote" }, "preparation.execution_endpoint"},
		{"audience", func(p *PreparationReport) { p.Audience = "" }, "preparation.audience"},
		{"expiry", func(p *PreparationReport) { p.ExpiresAt = p.PreparedAt }, "preparation.expires_at"},
		{"evidence endpoint", func(p *PreparationReport) { p.Evidence.PreparationID = fixturePlanID }, "preparation.evidence"},
		{"daemon endpoint", func(p *PreparationReport) { p.Daemon.ExecutionEndpoint = "remote" }, "preparation.daemon.execution_endpoint"},
		{"daemon status", func(p *PreparationReport) { p.Daemon.Status = "absent" }, "preparation.daemon.reachable"},
		{"finding", func(p *PreparationReport) {
			p.Findings = []Finding{{Code: "x", Severity: "fatal", Stage: "daemon_policy", Message: "m"}}
		}, "preparation.findings[0].severity"},
	}
	for _, c := range cases {
		p := fixturePreparation(t)
		c.edit(&p)
		if p.Accepted {
			p, _ = Seal(p) // re-seal so only the edited rule fails
		}
		err := p.Validate("preparation")
		if err == nil || !strings.HasPrefix(err.Error(), "plan_report_invalid: "+c.field) {
			t.Errorf("%s: want %s, got %v", c.name, c.field, err)
		}
	}
}

func TestFindingScreensDetailKeys(t *testing.T) {
	f := Finding{Code: "transport_unavailable", Severity: SeverityError, Stage: "transport", TargetID: "name:edge-b", Message: "system ssh binary not found", Details: map[string]string{"binary": "/usr/bin/ssh"}}
	if err := f.Validate("f"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"password", "Secret", "api_token", "private-key", "output", "transcript_path"} {
		f.Details = map[string]string{key: "x"}
		if err := f.Validate("f"); err == nil || !strings.Contains(err.Error(), "f.details") {
			t.Errorf("key %q accepted: %v", key, err)
		}
	}
	f.Details = nil
	f.Stage = "somewhere"
	if err := f.Validate("f"); err == nil {
		t.Error("unknown stage accepted")
	}
}

func TestReportTypesAreStructurallyNonSecret(t *testing.T) {
	allow := canarytest.Allow{Leaves: append(walkAllow.Leaves, "time.Time")}
	for _, rt := range []reflect.Type{reflect.TypeOf(Finding{}), reflect.TypeOf(DaemonReadiness{}), reflect.TypeOf(PreparationReport{})} {
		canarytest.Walk(t, rt, allow)
	}
}
