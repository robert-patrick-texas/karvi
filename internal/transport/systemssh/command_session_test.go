package systemssh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/platform"
)

func TestCommandModeUsesOneInteractiveShellAndBypassesControlMaster(t *testing.T) {
	home := t.TempDir()
	scratch, err := os.MkdirTemp("/tmp", "nd-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	bin := filepath.Join(home, "fake-ssh")
	logPath := bin + ".log"
	script := `#!/bin/sh
log="$0.log"
printf 'START:%s\n' "$*" >> "$log"
# The algorithm capability queries.
[ "$1" = -Q ] && exit 0
case " $* " in
  *" -O check "*|*" -M "*)
    printf 'FORBIDDEN-CONTROLMASTER\n' >> "$log"
    exit 97
    ;;
esac
case " $* " in
  *" ControlPath=none "*) : ;;
  *) printf 'MISSING-CONTROLPATH-NONE\n' >> "$log"; exit 98 ;;
esac
printf 'Model: Cisco C9300\r\n\r\nswitch01#'
while IFS= read -r line; do
  printf 'INPUT:%s\n' "$line" >> "$log"
  case "$line" in
    exit) exit 0 ;;
    'show clock')
      printf 'show clock\r\n01:20:19.849 EDT Thu Sep 10 2026\r\nswitch01#'
      ;;
    'show version')
      printf 'show version\r\nCisco IOS XE Software, Version 17.12\r\nswitch01#'
      ;;
    *)
      printf '%s\r\n%% Invalid input detected\r\nswitch01#' "$line"
      ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	sets := []string{
		"ssh.include-user-config=false",
		"audit.journald-required=false",
		"execution.prompt-timeout=2s",
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: sets})
	if err != nil {
		t.Fatal(err)
	}
	factory := Factory{Binary: bin, Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: scratch, ControlRoot: filepath.Join(home, "control"), AskpassPath: "/bin/true"}
	driver, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "192.0.2.10", Port: 22, Username: "operator",
		Definition: platform.Definition{Name: "cisco_iosxe"},
		Metadata:   map[string]string{"canonical_name": "switch01", "activity_type": "command"},
		Password:   func(fn func([]byte) error) error { return fn([]byte("test-only")) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	if err := driver.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !driver.Usable() {
		t.Fatal("a prepared session must be usable")
	}
	first := driver.Execute(context.Background(), platform.Command{Text: "show clock"})
	second := driver.Execute(context.Background(), platform.Command{Text: "show version"})
	for i, result := range []platform.Result{first, second} {
		if result.Err != nil {
			t.Fatalf("command %d: %v", i+1, result.Err)
		}
		// The first command sent on the connection is not a reuse; the
		// second is.
		if result.ConnectionReused == nil || *result.ConnectionReused != (i > 0) {
			t.Fatalf("command %d connection_reused = %v, want %t", i+1, result.ConnectionReused, i > 0)
		}
		if result.Prompt != "switch01#" || result.PromptObserved == nil || !*result.PromptObserved {
			t.Fatalf("command %d prompt provenance = %q observed=%v", i+1, result.Prompt, result.PromptObserved)
		}
	}
	if got := string(first.Output); got != "01:20:19.849 EDT Thu Sep 10 2026\n" {
		t.Fatalf("first output = %q", got)
	}
	if got := string(second.Output); got != "Cisco IOS XE Software, Version 17.12\n" {
		t.Fatalf("second output = %q", got)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "START:-F ") != 1 {
		t.Fatalf("OpenSSH session process count != 1:\n%s", text)
	}
	for _, query := range []string{"kex", "cipher", "mac", "HostKeyAlgorithms"} {
		if strings.Count(text, "START:-Q "+query+"\n") != 1 {
			t.Fatalf("ssh -Q %s not queried once:\n%s", query, text)
		}
	}
	if strings.Contains(text, "FORBIDDEN-CONTROLMASTER") || strings.Contains(text, "MISSING-CONTROLPATH-NONE") {
		t.Fatalf("command path used an incompatible ControlMaster flow:\n%s", text)
	}
	if strings.Count(text, "INPUT:show ") != 2 {
		t.Fatalf("command count != 2:\n%s", text)
	}
}

func TestMasterSessionRefusalIsNotAuthenticationFailure(t *testing.T) {
	code, category, _, _ := classify("Master refused session request: Permission denied", os.ErrPermission)
	if code != "ssh_session_channel_refused" || category != "connection" {
		t.Fatalf("classification = %s/%s", code, category)
	}
}

// TestUnansweredKeepalivesAreTheirOwnCode: OpenSSH's ServerAliveCountMax
// exit is session_keepalive_timeout, retryable, not the fallback.
func TestUnansweredKeepalivesAreTheirOwnCode(t *testing.T) {
	code, category, _, retryable := classify("Timeout, server 127.0.0.1 not responding.", os.ErrPermission)
	if code != "session_keepalive_timeout" || category != "connection" || !retryable {
		t.Fatalf("classification = %s/%s retryable=%t", code, category, retryable)
	}
}
