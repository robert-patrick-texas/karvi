package planner

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/executionplan/plantest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/inventory"
)

func loadTOML(t *testing.T, toml string) (configload.Snapshot, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(path, []byte(toml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: []string{path}, HomeDir: dir, SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	return cfg, path
}

// TestSessionInitNoneWithoutAMap: with no session-init-map every target is
// none and the table is empty, before and after binding.
func TestSessionInitNoneWithoutAMap(t *testing.T) {
	cfg := testConfig(t, `creds.backend-sequence=["fake"]`)
	backend := &fakeBackend{password: "p"}
	devices := []inventory.Device{direct("10.0.0.1"), direct("10.0.0.2")}
	draft := planFor(t, cfg, devices, nil)
	if len(draft.SessionInit) != 0 || draft.SessionInit == nil || draft.Targets[0].SessionInitProfile != "" {
		t.Fatalf("draft carries a selection: %+v %q", draft.SessionInit, draft.Targets[0].SessionInitProfile)
	}
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	final, _, _ := finish(t, p, draft)
	for _, target := range final.Targets {
		if target.SessionInitProfile != executionplan.SessionInitNone {
			t.Errorf("%s: profile %q, want none", target.TargetID, target.SessionInitProfile)
		}
	}
	if final.SessionInit == nil || len(final.SessionInit) != 0 {
		t.Errorf("table %+v, want {}", final.SessionInit)
	}
	if err := final.Validate(executionplan.Committed); err != nil {
		t.Fatal(err)
	}
}

// TestSessionInitSelectedAtCredentialResolution: a client-authority target is
// selected at the first Resolve, a daemon-authority one only after the
// prepare_job evidence gives it the address its rule matches; the table holds
// only the selected profiles in the plan's form.
func TestSessionInitSelectedAtCredentialResolution(t *testing.T) {
	cfg, _ := loadTOML(t, `name.allow-daemon-resolution = true
creds.backend-sequence = ["fake"]
[credential-backend.fake]
type = "env"
[session-init.branch-init]
commands = ["terminal width 511", "", "show clock"]
on-error = "continue"
command-timeout = "45s"
[session-init.quiet]
commands = []
[session-init.unused]
commands = ["show users"]
[[session-init-map]]
profile = "branch-init"
address-cidr = "198.51.100.0/24"
[[session-init-map]]
profile = "quiet"
name = "*"
`)
	backend := &fakeBackend{password: "p"}
	devices := []inventory.Device{direct("10.0.0.1"), direct("branch.example")}
	draft := planFor(t, cfg, devices, map[string]string{"name:branch.example": "daemon"})
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	if err := p.Resolve(context.Background(), draft); err != nil {
		t.Fatal(err)
	}
	if got, ok := p.SessionInitProfile("name:10.0.0.1"); !ok || got != "quiet" {
		t.Fatalf("client target: %q %v", got, ok)
	}
	if got, ok := p.SessionInitProfile("name:branch.example"); ok {
		t.Fatalf("daemon target selected before its evidence: %q", got)
	}
	lookup := func(_ context.Context, _, host string) ([]netip.Addr, error) { return mustAddrs("198.51.100.7"), nil }
	evidence, err := resolver.PrepareDaemonWith(context.Background(), cfg, draft.Targets, dual, lookup)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := executionplan.IncorporatePreparation(draft, executionplan.PreparationEvidence{ExecutionEndpoint: executionplan.EndpointLocal, PreparationID: plantest.PreparationID, PreparationDigest: executionplan.Sum([]byte("p")), PreparedAt: plantest.PreparedAt, Addresses: evidence})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Resolve(context.Background(), prepared); err != nil {
		t.Fatal(err)
	}
	final, _, _ := finish(t, p, prepared)
	if final.Targets[0].SessionInitProfile != "quiet" || final.Targets[1].SessionInitProfile != "branch-init" {
		t.Fatalf("profiles %q, %q", final.Targets[0].SessionInitProfile, final.Targets[1].SessionInitProfile)
	}
	want := map[string]executionplan.SessionInitProfile{
		"branch-init": {Commands: []string{"terminal width 511", "", "show clock"}, OnError: executionplan.SessionInitContinue, CommandTimeoutNS: int64(45 * time.Second)},
		"quiet":       {Commands: []string{}, OnError: executionplan.SessionInitFailDevice},
	}
	if !reflect.DeepEqual(final.SessionInit, want) {
		t.Fatalf("table %+v, want %+v", final.SessionInit, want)
	}
}

// TestSessionInitSelectionFailsBeforeCredentials: equal address-cidr
// prefixes fail the plan as a credential error does, naming the rules and
// their sources, before any backend is asked.
func TestSessionInitSelectionFailsBeforeCredentials(t *testing.T) {
	cfg, path := loadTOML(t, `creds.backend-sequence = ["fake"]
[credential-backend.fake]
type = "env"
[session-init.a]
commands = ["show clock"]
[session-init.b]
commands = ["show users"]
[[session-init-map]]
profile = "a"
address-cidr = "10.0.0.0/24"
[[session-init-map]]
profile = "b"
address-cidr = "10.0.0.0/24"
site = "*"
[[session-init-map]]
profile = "a"
name = "*"
`)
	backend := &fakeBackend{password: "p"}
	devices := []inventory.Device{direct("10.0.0.1"), direct("192.0.2.1")}
	draft := planFor(t, cfg, devices, nil)
	p, err := NewCredentialPlanner(cfg, operator, devices, plantest.DraftedAt, CredentialOptions{Resolver: newResolver(t, cfg, backend)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Destroy()
	err = p.Resolve(context.Background(), draft)
	want := fmt.Sprintf("session_init_prefix_ambiguous: 1 of 2 targets failed session-init selection: name:10.0.0.1 (session_init_prefix_ambiguous: rules session-init-map.0 (%s:9), session-init-map.1 (%s:12) match 10.0.0.1 with the same prefix length 24)", path, path)
	if errorcodes.Of(err) != "session_init_prefix_ambiguous" || err.Error() != want {
		t.Fatalf("err=%v\nwant %s", err, want)
	}
	if backend.calls != 0 || p.Grants() != 0 {
		t.Fatalf("backend asked before selection failed: calls=%d grants=%d", backend.calls, p.Grants())
	}
}

// TestSessionInitSelectorErrors covers what load validation makes
// unreachable: no rule matching, and a rule that does not compile.
func TestSessionInitSelectorErrors(t *testing.T) {
	s := &sessionInitSelector{profiles: map[string]executionplan.SessionInitProfile{"x": {Commands: []string{}, OnError: executionplan.SessionInitFailDevice}}}
	d := direct("sw1")
	d.Site = "branch"
	s.rules = []map[string]any{{"site": "hq", "profile": "x"}}
	if _, err := s.selectProfile(d); errorcodes.Of(err) != "session_init_map_unmatched" || !strings.Contains(err.Error(), "no session-init profile matched sw1") {
		t.Fatalf("unmatched: %v", err)
	}
	s.rules = []map[string]any{{"site": "ny[c", "profile": "x"}, {"name": "*", "profile": "x"}}
	if _, err := s.selectProfile(d); errorcodes.Of(err) != "config_match_rule_pattern_invalid" {
		t.Fatalf("malformed: %v", err)
	}
	s.rules = []map[string]any{{"site": "*", "profile": "x"}}
	if got, err := s.selectProfile(d); err != nil || got != "x" {
		t.Fatalf("match: %q %v", got, err)
	}
}
