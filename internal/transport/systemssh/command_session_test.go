package systemssh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/testdir"
	"github.com/robert-patrick-texas/karvi/platform"
)

func TestCommandModeUsesOneInteractiveShellAndBypassesControlMaster(t *testing.T) {
	home := t.TempDir()
	scratch := testdir.Short(t)
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
		Definition: platform.Definition{Name: "cisco_ios"},
		Metadata:   map[string]string{"canonical_name": "switch01"},
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

// TestShellSessionsEndTheOptionsBeforeTheDevice: the command session, the
// interactive login, and ssh -G each end OpenSSH's options with "--" before
// the device, and give nothing after it.
func TestShellSessionsEndTheOptionsBeforeTheDevice(t *testing.T) {
	home := t.TempDir()
	scratch := testdir.Short(t)
	bin := filepath.Join(home, "fake-ssh")
	script := `#!/bin/sh
line=$(printf '%s|' "$@")
printf '%s\n' "$line" >> "$0.log"
case "$1" in -Q|-G) exit 0 ;; esac
printf 'switch01#'
while IFS= read -r line; do
  case "$line" in exit) exit 0 ;; esac
  printf '%s\r\nswitch01#' "$line"
done
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{
		"ssh.include-user-config=false", "audit.journald-required=false", "execution.prompt-timeout=2s",
	}})
	if err != nil {
		t.Fatal(err)
	}
	factory := Factory{Binary: bin, Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: scratch, ControlRoot: filepath.Join(home, "control"), AskpassPath: "/bin/true"}
	opened, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "192.0.2.10", Port: 22, Username: "operator",
		Definition: platform.Definition{Name: "cisco_ios"},
		Metadata:   map[string]string{"canonical_name": "switch01"},
		Password:   func(fn func([]byte) error) error { return fn([]byte("test-only")) },
	})
	if err != nil {
		t.Fatal(err)
	}
	d := opened.(*Driver)
	defer d.Close()
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr strings.Builder
	if err := d.Interactive(context.Background(), strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("interactive: %v (%s)", err, stderr.String())
	}
	d.sessionFailure().effective()
	data, err := os.ReadFile(bin + ".log")
	if err != nil {
		t.Fatal(err)
	}
	shells, evaluations := 0, 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		switch {
		case strings.HasPrefix(line, "-Q|"):
			continue
		case strings.HasSuffix(line, "|-tt|--|192.0.2.10|"):
			shells++
		case strings.HasPrefix(line, "-G|") && strings.HasSuffix(line, "|--|192.0.2.10|"):
			evaluations++
		default:
			t.Errorf("command line without -- before the device: %s", line)
		}
	}
	if shells != 2 || evaluations != 1 {
		t.Fatalf("shells %d, ssh -G %d, want 2 and 1:\n%s", shells, evaluations, data)
	}
}

// bannerDriver is a prepared command session over a stand-in for OpenSSH
// that writes banner on its standard error as OpenSSH writes a device's
// banner at LogLevel VERBOSE, before its own "Authenticated to" line, then
// gives the prompt; "show clock" ends it with OpenSSH's unanswered
// keepalives line and exit 255. sets are the configuration's, notice the
// request's HostKeyNotice.
func bannerDriver(t *testing.T, banner string, notice func(platform.HostKeyNotice), sets ...string) *Driver {
	t.Helper()
	d := bannerSession(t, banner, `printf 'Authenticated to 192.0.2.10 ([192.0.2.10]:22) using "password".\r\n' >&2
printf 'switch01#'
while IFS= read -r line; do
  case "$line" in
    exit) exit 0 ;;
    'show clock') printf 'Timeout, server 192.0.2.10 not responding.\r\n' >&2; exit 255 ;;
  esac
  printf '%s\r\nswitch01#' "$line"
done
`, notice, sets...)
	if err := d.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	return d
}

// bannerSession is a command session, not yet prepared, over a stand-in for
// OpenSSH that writes banner on its standard error, then runs rest.
func bannerSession(t *testing.T, banner, rest string, notice func(platform.HostKeyNotice), sets ...string) *Driver {
	t.Helper()
	home := t.TempDir()
	scratch := testdir.Short(t)
	bin := filepath.Join(home, "fake-ssh")
	bannerPath := filepath.Join(home, "banner")
	if err := os.WriteFile(bannerPath, []byte(banner), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$1\" in -Q|-G) exit 0 ;; esac\ncat '" + bannerPath + "' >&2\n" + rest
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{
		"ssh.include-user-config=false", "audit.journald-required=false", "execution.prompt-timeout=2s",
	}, sets...)})
	if err != nil {
		t.Fatal(err)
	}
	factory := Factory{Binary: bin, Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: scratch, ControlRoot: filepath.Join(home, "control"), AskpassPath: "/bin/true"}
	opened, err := factory.Open(context.Background(), platform.OpenRequest{
		Address: "192.0.2.10", Port: 22, Username: "operator",
		Definition:    platform.Definition{Name: "cisco_ios"},
		Metadata:      map[string]string{"canonical_name": "switch01"},
		Password:      func(fn func([]byte) error) error { return fn([]byte("test-only")) },
		HostKeyNotice: notice,
	})
	if err != nil {
		t.Fatal(err)
	}
	d := opened.(*Driver)
	t.Cleanup(func() { d.Close() })
	return d
}

// TestABannerDoesNotClassifyALaterFailure: a failure after authentication
// is classified by what OpenSSH wrote after it said the session
// authenticated: a device's banner saying "Permission denied", or one
// longer than the diagnostics hold, does not make unanswered keepalives
// authentication_failed or hide them.
func TestABannerDoesNotClassifyALaterFailure(t *testing.T) {
	for name, banner := range map[string]string{
		"permission denied": "Permission denied to unauthorised users\r\nauthentication failed attempts are logged\r\n",
		"longer than held":  strings.Repeat("AUTHORIZED ACCESS ONLY\r\n", 3000),
	} {
		t.Run(name, func(t *testing.T) {
			d := bannerDriver(t, banner, nil)
			r := d.Execute(context.Background(), platform.Command{Text: "show clock"})
			if r.ErrorCode != "session_keepalive_timeout" || r.Err == nil || strings.Contains(r.Err.Error(), "AUTHORIZED") || strings.Contains(r.Err.Error(), "unauthorised") {
				t.Fatalf("code %q err %.300v", r.ErrorCode, r.Err)
			}
		})
	}
}

// TestABannerIsNoEnrollment: under accept-new, a device's banner holding
// OpenSSH's "Permanently added" line says nothing of a key the trust store
// already held for the device; OpenSSH writes its own line, before
// authentication, only for a device the store holds nothing for.
func TestABannerIsNoEnrollment(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "known_hosts")
	entry := "switch01 ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8g\n"
	if err := os.WriteFile(store, []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	var said []platform.HostKeyNotice
	bannerDriver(t, "Warning: Permanently added 'switch01' (RSA) to the list of known hosts.\r\n",
		func(n platform.HostKeyNotice) { said = append(said, n) },
		"ssh.host-key-policy=accept-new", `ssh.known-hosts-file="`+store+`"`)
	if len(said) != 0 {
		t.Fatalf("said %+v", said)
	}
}

// TestABannerIsNotAnAuthenticationFailure: before authentication, the
// login's failure is authentication_failed only on OpenSSH's own lines
// saying so, never on a device's banner holding the words.
func TestABannerIsNotAnAuthenticationFailure(t *testing.T) {
	banner := "Permission denied to unauthorised users\r\nauthentication failed attempts are logged\r\n"
	for _, tc := range []struct{ name, stderr, want string }{
		{"connection closed", "Connection closed by 192.0.2.10 port 22\r\n", "ssh_process_failed"},
		{"every method refused", "operator@192.0.2.10: Permission denied (publickey,password).\r\n", "authentication_failed"},
		{"password refused", "Permission denied (password).\r\n", "authentication_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := bannerSession(t, banner, "printf '"+tc.stderr+"' >&2\nexit 255\n", nil)
			if err := d.Prepare(context.Background()); errorcodes.Of(err) != tc.want {
				t.Fatalf("err %v, want %s", err, tc.want)
			}
		})
	}
}

// TestABannerDoesNotDecideALoginsFailure: OpenSSH writes its own failure
// after the device's banner, so a banner holding host-key wording, or one
// longer than the diagnostics hold, neither gives the code of a refused
// login nor pushes OpenSSH's line out of the diagnostics or the message.
func TestABannerDoesNotDecideALoginsFailure(t *testing.T) {
	refused := "operator@192.0.2.10: Permission denied (password)."
	for name, banner := range map[string]string{
		"host-key wording": "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED! Report changes to the NOC.\r\n" +
			"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\r\nHost key verification failed.\r\n",
		"longer than held": strings.Repeat("Host key verification failed.\r\n", 3000),
	} {
		t.Run(name, func(t *testing.T) {
			d := bannerSession(t, banner, "printf '"+refused+"\\r\\n' >&2\nexit 255\n", nil)
			err := d.Prepare(context.Background())
			if errorcodes.Of(err) != "authentication_failed" || !strings.Contains(err.Error(), refused) {
				t.Fatalf("err %.300v", err)
			}
		})
	}
}
