package app

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// TestShortcutModes: the device shortcut's rules 4 and 5 in login and
// command (EXAMPLES chapter 71). An inventory name, a near miss among them,
// and a literal address go ahead with no lookup; a near miss that is no
// inventory name is cli_command_near_miss before any claim or lookup; a name
// DNS does not know is cli_command_unknown, under daemon address authority
// too; a timeout keeps dns_timeout, and the same name given without the
// shortcut keeps dns_nxdomain.
func TestShortcutModes(t *testing.T) {
	// The credentials come from the environment, never a prompt; the device
	// transport is a fake that fails, and with PATH empty login finds no
	// askpass helper: an activity that goes ahead ends there, having
	// contacted nothing.
	t.Setenv("NETUSER", "u")
	t.Setenv("NETPASS", "p")
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	csv := filepath.Join(dir, "inv.csv")
	if err := os.WriteFile(csv, []byte("name,management_address,platform\nlgoin,127.0.0.1,cisco_ios\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "karvi.toml")
	if err := os.WriteFile(config, []byte("[[inventory-source]]\nname = \"lab\"\ntype = \"csv\"\npath = \""+csv+"\"\nrequired = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	base, scoreboards, trust := filepath.Join(dir, "base"), filepath.Join(dir, "scoreboards"), filepath.Join(dir, "trust")
	for _, d := range []string{base, trust} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: []string{config}, HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{
		`basedir="` + base + `"`, `scoreboards="` + scoreboards + `"`,
		`audit.journald-required=false`, `audit.file="` + filepath.Join(dir, "audit.jsonl") + `"`,
		`ssh.known-hosts-file="` + filepath.Join(trust, "known_hosts") + `"`,
		`ssh.transports.system="` + fake + `"`, `display.color="never"`, `name.allow-daemon-resolution=true`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	common := CommonOptions{Config: cfg, Operator: credentials.Operator{Username: "tester", UID: os.Getuid(), Home: dir}}
	nxdomain := &net.DNSError{Err: "no such host", IsNotFound: true}
	timeout := &net.DNSError{Err: "i/o timeout", IsTimeout: true}
	for _, mode := range []string{"login", "command"} {
		for _, tc := range []struct {
			name      string
			word      string
			shortcut  Shortcut
			authority string
			answer    error
			code      string
			exit      int
			lookups   int
		}{
			{"inventory name", "lgoin", Shortcut{"lgoin", "login"}, "", nil, "dependency_askpass_unavailable: ", 8, 0},
			{"literal", "127.0.0.1", Shortcut{"127.0.0.1", ""}, "", nil, "dependency_askpass_unavailable: ", 8, 0},
			{"guard", "logni", Shortcut{"logni", "login"}, "", nil, `cli_command_near_miss: "logni" is not a command; did you mean login?`, 4, 0},
			{"no such name", "router9", Shortcut{"router9", ""}, "", nxdomain, `cli_command_unknown: "router9" is neither a command nor a target: DNS has no such name`, 4, 2},
			{"no such name, daemon authority", "router9", Shortcut{"router9", ""}, "daemon", nxdomain, `cli_command_unknown: "router9" is neither a command nor a target: DNS has no such name`, 4, 2},
			{"a timeout", "router9", Shortcut{"router9", ""}, "", timeout, "dns_timeout: ", 7, 2},
			{"no shortcut", "router9", Shortcut{}, "", nxdomain, "dns_nxdomain: ", 7, 2},
		} {
			t.Run(mode+" "+tc.name, func(t *testing.T) {
				lookups := 0
				lookup := func(ctx context.Context, network, host string) ([]netip.Addr, error) {
					lookups++
					if tc.answer != nil {
						return nil, tc.answer
					}
					return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
				}
				targets := []TargetInput{{Kind: "target", Value: tc.word}}
				var stderr bytes.Buffer
				streams := IO{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &stderr}
				claims, _ := os.ReadDir(scoreboards)
				var result ActivityResult
				if mode == "login" {
					result = ExecuteLogin(context.Background(), LoginOptions{CommonOptions: common, Targets: targets, Shortcut: tc.shortcut, Lookup: lookup, AddressAuthority: tc.authority}, streams)
				} else {
					result = ExecuteCommand(context.Background(), CommandOptions{CommonOptions: common, Targets: targets, Transport: "system", Commands: []string{"show clock"}, Shortcut: tc.shortcut, Lookup: lookup, AddressAuthority: tc.authority}, streams)
				}
				if result.ExitCode != tc.exit || !strings.HasPrefix(result.Error, tc.code) {
					t.Errorf("exit=%d error=%q, want %d %q; stderr %q", result.ExitCode, result.Error, tc.exit, tc.code, stderr.String())
				}
				if lookups != tc.lookups {
					t.Errorf("%d lookups, want %d", lookups, tc.lookups)
				}
				// The guard comes before the claim; the others after it.
				if after, _ := os.ReadDir(scoreboards); (len(after) > len(claims)) != (tc.name != "guard") {
					t.Errorf("claims %d before, %d after", len(claims), len(after))
				}
			})
		}
	}
}
