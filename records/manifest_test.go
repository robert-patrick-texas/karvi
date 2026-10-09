package records

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
)

const manifestSchema = "../schema/job-manifest.schema.json"

// fixtureManifest is a version 2 manifest over the plantest final plan, a
// projection that binds its targets to the fixture grants, and a commit
// header referencing the projection's digest.
func fixtureManifest(t *testing.T) Manifest {
	t.Helper()
	final := plantest.FinalPlan()
	scope := func(ids ...string) credentialpackage.CredentialScope {
		return credentialpackage.CredentialScope{TargetIDs: ids, Transports: []string{"system"}, Ports: []uint16{22}}
	}
	projection := credentialpackage.SafePackageProjection{
		SchemaVersion: credentialpackage.SchemaVersion, PackageID: "20260914T120005.000000+0000-0123456789abcdefghjk", JobID: plantest.JobID, PlanDigest: final.PlanDigest,
		Issuer: credentialpackage.Principal{Kind: credentialpackage.IssuerOperator, Username: "netops", UID: 1000, Hostname: "ops01"}, Audience: []string{"local"}, Protection: credentialpackage.ProtectionLocalPeer,
		IssuedAt: plantest.FinalizedAt, ExpiresAt: plantest.FinalizedAt.Add(credentialpackage.MaxPackageWindow),
		Grants: []credentialpackage.GrantProjection{
			{CredentialID: plantest.GrantA, Method: credentialpackage.MethodEmbeddedSecret, DeviceUsername: "u", Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}, Scope: scope("name:127.0.0.1", "name:edge-b"), NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour)},
			{CredentialID: plantest.GrantB, Method: credentialpackage.MethodEmbeddedSecret, DeviceUsername: "admin", Policy: "core", Backend: "cloginrc", MatchedOn: credentials.Match{Category: "device", Pattern: "core-*"}, Scope: scope("name:core-a"), NotBefore: plantest.DraftedAt, NotAfter: plantest.DraftedAt.Add(12 * time.Hour)},
		},
		Bindings: []credentialpackage.TargetCredentialBinding{{TargetID: "name:127.0.0.1", CredentialID: plantest.GrantA}, {TargetID: "name:edge-b", CredentialID: plantest.GrantA}, {TargetID: "name:core-a", CredentialID: plantest.GrantB}},
	}
	digest, err := projection.Sum()
	if err != nil {
		t.Fatal(err)
	}
	projection.PackageDigest = digest
	header := plantest.Header(final, executionplan.Committed)
	header.CredentialPackage = &executionplan.PackageReference{Protection: executionplan.ProtectionLocalPeer, Digest: digest}
	policy := ExecutionPolicy{HostKeyPolicy: "accept-new", KnownHostsFile: "auto", HaltOnHostKeyMismatch: true, AllowTelnet: false}
	policy.Digest, _ = policy.Sum()
	initial := []InitialState{}
	for _, x := range final.Targets {
		for i := range final.Commands {
			initial = append(initial, InitialState{DeviceID: x.TargetID, CommandIndex: i + 1, State: "not_started"})
		}
	}
	return Manifest{
		SchemaVersion: JobSchemaVersion, JobID: plantest.JobID, ActivityID: plantest.JobID, AcceptedAt: plantest.FinalizedAt.Add(time.Second),
		Operator: Operator{Username: "netops", UID: 1000, PrimaryGID: 1000, Groups: []string{"netops"}}, App: map[string]any{"build": map[string]any{"version": "0.9.2"}}, Mode: "live",
		Header: header, Plan: final, CredentialPackage: projection, Policy: policy,
		Selection:     Selection{Inputs: []TargetInput{{Kind: "target", Value: "127.0.0.1"}, {Kind: "site", Value: "*"}}, Excludes: []string{}, AddressAuthorities: []string{"core-a=daemon"}},
		InitialStates: initial,
	}
}

// TestManifestValidatesMatchesSchemaAndIsNonSecret: the fixture
// validates, its JSON names only keys the schema knows
// and every required key, and the structural walker finds no secret-bearing
// type reachable from Manifest.
func TestManifestValidatesMatchesSchemaAndIsNonSecret(t *testing.T) {
	m := fixtureManifest(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}
	data, _ := json.MarshalIndent(m, "", "  ")
	t.Logf("manifest:\n%s", data)
	canarytest.SchemaParity(t, manifestSchema, m)
	canarytest.Walk(t, reflect.TypeOf(Manifest{}), canarytest.Allow{Leaves: []string{"time.Time", "net/netip.Addr", reflect.TypeOf(executionplan.Digest{}).String(), reflect.TypeOf(inventory.Provenance{}).String(), "*string", "map[string]interface {}", "github.com/robert-patrick-texas/karvi/executionplan.Configuration"}})
	// The k03 assertions grep dispatch_order and shuffle_key anywhere in the
	// manifest; both sit inside the plan's dispatch block.
	if !json.Valid(data) || !containsKey(data, `"dispatch_order":`) {
		t.Fatal("dispatch_order is not in the manifest JSON")
	}
}

func containsKey(data []byte, key string) bool {
	return len(data) > 0 && string(data) != "" && indexOf(string(data), key) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestManifestValidationVectors(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Manifest)
		code string
	}{
		{"schema", func(m *Manifest) { m.SchemaVersion = 1 }, "manifest_schema_unsupported"},
		{"job id differs from header", func(m *Manifest) { m.JobID = plantest.PlanID }, "manifest_invalid"},
		{"tampered plan", func(m *Manifest) { m.Plan.Commands = append([]string{"show clock detail"}, m.Plan.Commands[1:]...) }, "plan_digest_mismatch"},
		{"projection digest", func(m *Manifest) { m.CredentialPackage.PackageDigest = executionplan.Sum([]byte("x")) }, "manifest_invalid"},
		{"header reference", func(m *Manifest) { m.Header.CredentialPackage.Digest = executionplan.Sum([]byte("x")) }, "manifest_invalid"},
		{"package names another plan", func(m *Manifest) {
			m.CredentialPackage.PlanDigest = executionplan.Sum([]byte("x"))
			m.CredentialPackage.PackageDigest, _ = sumCleared(m.CredentialPackage)
		}, "manifest_invalid"},
		{"policy digest", func(m *Manifest) { m.Policy.AllowTelnet = true }, "manifest_invalid"},
		{"initial state target", func(m *Manifest) { m.InitialStates[0].DeviceID = "name:ghost" }, "manifest_invalid"},
		{"initial state index", func(m *Manifest) { m.InitialStates[0].CommandIndex = 9 }, "manifest_invalid"},
		{"selection lists", func(m *Manifest) { m.Selection.Excludes = nil }, "manifest_invalid"},
	} {
		m := fixtureManifest(t)
		tc.edit(&m)
		if got := errorcodes.Of(m.Validate()); got != tc.code {
			t.Errorf("%s: code=%q, want %s (%v)", tc.name, got, tc.code, m.Validate())
		}
	}
}

func sumCleared(p credentialpackage.SafePackageProjection) (executionplan.Digest, error) {
	p.PackageDigest = executionplan.Digest{}
	return p.Sum()
}
