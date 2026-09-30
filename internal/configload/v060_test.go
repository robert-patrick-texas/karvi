package configload

import (
	"errors"
	"strings"
	"testing"
)

func TestV090DisplayTransportAndTimeoutDefaults(t *testing.T) {
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Int("config.schema-version"); got != 6 {
		t.Fatalf("config.schema-version=%d, want 6", got)
	}
	if got := snap.String("display.login.header"); got == "" {
		t.Fatal("default login header is unexpectedly blank")
	}
	if got := snap.String("display.command.header"); got == "" {
		t.Fatal("default command header is unexpectedly blank")
	}
	if got := snap.String("display.login.footer"); got != "" {
		t.Fatalf("default login footer=%q, want blank", got)
	}
	if snap.Bool("ssh.halt-run-on-host-key-mismatch") {
		t.Fatal("host-key mismatch halt must be opt-in")
	}
	for _, key := range []string{"ssh.login.transport", "ssh.command.transport", "ssh.run.transport"} {
		if got := snap.String(key); got != "default" {
			t.Fatalf("%s=%q, want default", key, got)
		}
	}
	if got := snap.String("ssh.transports.system"); got != "ssh" {
		t.Fatalf("ssh.transports.system=%q, want ssh", got)
	}
	if got := snap.String("ssh.transports.native"); got != "scrapligo-v1" {
		t.Fatalf("ssh.transports.native=%q, want scrapligo-v1", got)
	}
	if snap.Bool("display.command.echo") || snap.Bool("display.run.echo") {
		t.Fatal("prompt echo must be opt-in by default")
	}
	if got := snap.Int("display.json.indent"); got != 2 {
		t.Fatalf("display.json.indent=%d, want 2", got)
	}
	if got := snap.Int("display.command.dynamic-border-length"); got != 72 {
		t.Fatalf("display.command.dynamic-border-length=%d, want 72", got)
	}
	if got := snap.String("display.command.footer"); got != "! exit=<exit-code> elapsed=<elapsed> artifacts=<artifacts>" {
		t.Fatalf("default command footer=%q", got)
	}

	if got := snap.String("name.dns-timeout"); got != "2s" {
		t.Fatalf("name.dns-timeout=%q, want 2s", got)
	}
	if got := snap.String("telnet.read-timeout"); got != "60s" {
		t.Fatalf("telnet.read-timeout=%q, want 60s", got)
	}
	for _, mode := range []string{"command", "run"} {
		if got := snap.String("display." + mode + ".border"); got != "\n" {
			t.Fatalf("display.%s.border=%q, want one blank line", mode, got)
		}
		if snap.Bool("display." + mode + ".last-border") {
			t.Fatalf("display.%s.last-border must default false", mode)
		}
		if got := snap.Int("display." + mode + ".dynamic-border-length"); got != 72 {
			t.Fatalf("display.%s.dynamic-border-length=%d, want 72", mode, got)
		}
	}
}

func TestV060RemovedConfigurationKeysAreUnknown(t *testing.T) {
	removed := []string{
		"ssh.strict-host-key-checking=\"accept-new\"",
		"ssh.user-known-hosts-file=\"/tmp/known_hosts\"",
		"native-ssh.strict-host-key-checking=\"yes\"",
		"native-ssh.known-hosts-file=\"/tmp/known_hosts\"",
		"output.command-headers=true",
		"watch.theme=\"dark\"",
		"login.transport=\"system\"",
		"command.transport=\"system\"",
		"run.transport=\"native\"",
		"ssh.binary=\"ssh\"",
		"ssh.control-master=\"auto\"",
	}
	for _, set := range removed {
		t.Run(set, func(t *testing.T) {
			_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}})
			if err == nil {
				t.Fatal("removed configuration key was accepted")
			}
			var configErr *Error
			if !errors.As(err, &configErr) {
				t.Fatalf("error type=%T, want *configload.Error", err)
			}
			if configErr.Code != "config_unknown_key" {
				t.Fatalf("code=%q, want config_unknown_key; err=%v", configErr.Code, err)
			}
			if !strings.Contains(err.Error(), "config_unknown_key:") {
				t.Fatalf("operator-facing error omits stable code: %v", err)
			}
		})
	}
}

func TestV060DisplayValidation(t *testing.T) {
	_, err := Load(Options{
		HomeDir:     t.TempDir(),
		SkipAuto:    true,
		Environment: []string{},
		Sets:        []string{"display.command.header=\"<password>\""},
	})
	if err == nil {
		t.Fatal("unknown display placeholder was accepted")
	}

	snap, err := Load(Options{
		HomeDir:     t.TempDir(),
		SkipAuto:    true,
		Environment: []string{},
		Sets: []string{
			"display.timestamp=\"yyyy mm dd - hh:mm:ss.sss\"",
			"display.command.border=\"<repeat:=:40>\\n\"",
			"display.login.header=\"\"",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("display.login.header"); got != "" {
		t.Fatalf("blank header did not survive validation: %q", got)
	}
}

func TestV090DisplayRangeValidation(t *testing.T) {
	for _, set := range []string{
		"display.json.indent=0",
		"display.json.indent=9",
		"display.command.dynamic-border-length=0",
		"display.command.dynamic-border-length=4097",
		"display.run.dynamic-border-length=0",
		"display.run.dynamic-border-length=4097",
	} {
		t.Run(set, func(t *testing.T) {
			_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}})
			if err == nil {
				t.Fatalf("invalid setting %q was accepted", set)
			}
		})
	}
}
