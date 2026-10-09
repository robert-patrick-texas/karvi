package executor

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// withAlgorithmConfig reloads the harness's configuration with sets added
// and collects its debug lines.
func (h *gateHarness) withAlgorithmConfig(sets ...string) func() []string {
	h.t.Helper()
	base := h.exec.opts.Config
	all := []string{
		fmt.Sprintf("ssh.transports.system=%q", base.String("ssh.transports.system")), `ssh.host-key-policy="insecure"`,
		fmt.Sprintf("ssh.known-hosts-file=%q", base.String("ssh.known-hosts-file")),
	}
	cfg, err := configload.Load(configload.Options{HomeDir: filepath.Dir(base.String("ssh.known-hosts-file")), SkipAuto: true, Environment: []string{}, Sets: append(all, sets...)})
	if err != nil {
		h.t.Fatal(err)
	}
	h.exec.opts.Config = cfg
	var mu sync.Mutex
	var lines []string
	h.exec.opts.Debug = func(s string) { mu.Lock(); lines = append(lines, s); mu.Unlock() }
	return func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), lines...) }
}

// TestSSHAlgorithmsMapAmbiguityFailsBeforeContact: two address-cidr rules
// of the same prefix fail the device with
// ssh_algorithms_map_ambiguous before capacity and transport.
func TestSSHAlgorithmsMapAmbiguityFailsBeforeContact(t *testing.T) {
	h := newGateHarness(t, nil, nil)
	h.withAlgorithmConfig(
		`ssh-algorithms-profile.a.ciphers-append=["aes128-ctr"]`, `ssh-algorithms-profile.b.kex-append=["diffie-hellman-group1-sha1"]`,
		`ssh-algorithms-map.0.profile="a"`, `ssh-algorithms-map.0.address-cidr="127.0.0.0/8"`,
		`ssh-algorithms-map.1.profile="b"`, `ssh-algorithms-map.1.address-cidr="127.0.0.0/8"`,
	)
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "ssh_algorithms_map_ambiguous" || h.transportAttempted() {
		t.Fatalf("result %+v transport=%t", res, h.transportAttempted())
	}
	if recs[0].Error == nil || recs[0].Error.Code != "ssh_algorithms_map_ambiguous" || recs[0].Error.Category != "config" || !strings.Contains(recs[0].Error.Message, "ssh-algorithms-map.0, ssh-algorithms-map.1") {
		t.Fatalf("record 1 %+v", recs[0].Error)
	}
	for _, r := range recs[1:] {
		if r.Status != "not_attempted_prior_command_failure" {
			t.Fatalf("later record %s", r.Status)
		}
	}
}

// TestSSHAlgorithmsProfileSelected is the debug line of the selected
// profile, and of the global section when no rule matches.
func TestSSHAlgorithmsProfileSelected(t *testing.T) {
	h := newGateHarness(t, nil, nil)
	debug := h.withAlgorithmConfig(`ssh-algorithms-profile.old.ciphers-append=["aes128-ctr"]`, `ssh-algorithms-map.0.profile="old"`, `ssh-algorithms-map.0.site="branch"`, `ssh-algorithms-map.1.profile="old"`, `ssh-algorithms-map.1.name="127.0.0.1"`)
	if res, _ := h.run(context.Background()); !res.Success {
		t.Fatalf("result %+v", res)
	}
	found := false
	for _, l := range debug() {
		if strings.HasPrefix(l, "device ssh algorithms target=") {
			found = true
			if !strings.Contains(l, "profile=old rule=ssh-algorithms-map.1 ") || !strings.Contains(l, "ciphers=aes256-gcm@openssh.com,chacha20-poly1305@openssh.com,aes256-ctr,aes256-cbc,aes128-ctr ") {
				t.Fatalf("debug line %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("no algorithm debug line in %q", debug())
	}
}
