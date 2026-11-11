package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestLoginPlatformResolvesAliases: login uses the one resolver, so an
// alias is its
// built-in, and a name the configuration does not know is refused with
// platform_unknown, never generic under the name (the backstop behind the
// planner's resolution).
func TestLoginPlatformResolvesAliases(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_ios"`, `platform.c9300.session-cap=4`}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := platformFor(cfg, "c9300")
	if err != nil || d.Base != "cisco_ios" || d.SessionCap != 4 || d.PrivilegedLevel != "privilege-exec" || d.RequiresEnable {
		t.Fatalf("alias: %+v %v", d, err)
	}
	if _, err = platformFor(cfg, "Lab-Box"); errorcodes.Of(err) != "platform_unknown" || !strings.Contains(err.Error(), "known: generic, cisco_ios") {
		t.Fatalf("other name: %v", err)
	}
	if d, err = platformFor(cfg, " Generic "); err != nil || d.Name != "generic" || d.Base != "generic" {
		t.Fatalf("built-in in another spelling: %+v %v", d, err)
	}
}

// TestLoginResolvesNoEnableSecret: a login sends no enable secret, its
// operator typing enable at the device, so a platform with requires-enable
// does not refuse one without it; the login goes on to the transport, here
// failing for want of the askpass helper with PATH empty.
func TestLoginResolvesNoEnableSecret(t *testing.T) {
	t.Setenv("NETUSER", "jsmith")
	t.Setenv("NETPASS", "S3cret")
	os.Unsetenv("NETENABLE")
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	base, trust := filepath.Join(dir, "base"), filepath.Join(dir, "trust")
	for _, d := range []string{base, trust} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{
		`basedir="` + base + `"`, `scoreboards="` + filepath.Join(dir, "scoreboards") + `"`,
		`audit.journald-required=false`, `audit.file="` + filepath.Join(dir, "audit.jsonl") + `"`,
		`ssh.known-hosts-file="` + filepath.Join(trust, "known_hosts") + `"`,
		`ssh.transports.system="` + fake + `"`, `display.color="never"`,
		"creds.interactive-prompt=false", "platform.cisco_ios.requires-enable=true",
	}})
	if err != nil {
		t.Fatal(err)
	}
	common := CommonOptions{Config: cfg, Operator: credentials.Operator{Username: "jsmith", UID: os.Getuid(), Home: dir}}
	var stderr bytes.Buffer
	streams := IO{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &stderr}
	result := ExecuteLogin(context.Background(), LoginOptions{CommonOptions: common, Targets: []records.TargetInput{{Kind: "target", Value: "127.0.0.1"}}, Platform: "cisco_ios"}, streams)
	if !strings.HasPrefix(result.Error, "dependency_askpass_unavailable: ") {
		t.Errorf("exit=%d error=%q, want dependency_askpass_unavailable; stderr %q", result.ExitCode, result.Error, stderr.String())
	}
}
