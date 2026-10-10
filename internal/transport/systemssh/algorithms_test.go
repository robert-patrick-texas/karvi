package systemssh

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/platform"
)

// fakeQueryBinary answers ssh -Q like OpenSSH 9.6 for kex (no ML-KEM) and
// cipher (no CBC but aes128-cbc), and nothing for mac and
// HostKeyAlgorithms, as a stand-in would.
func fakeQueryBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ssh")
	script := `#!/bin/sh
case "$1 $2" in
  "-Q kex") printf 'diffie-hellman-group14-sha1\nsntrup761x25519-sha512@openssh.com\ncurve25519-sha256\necdh-sha2-nistp256\n' ;;
  "-Q cipher") printf 'aes128-ctr\naes256-ctr\naes128-cbc\naes256-gcm@openssh.com\n' ;;
  "-Q mac") printf 'switch01#' ;;
esac
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestGeneratedConfigOffersTheDeviceListsTheBinaryImplements(t *testing.T) {
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", `ssh.host-key-policy="insecure"`}})
	if err != nil {
		t.Fatal(err)
	}
	bin := fakeQueryBinary(t)
	caps := binaryCapabilities(cfg, bin)
	if !caps.answered[sshalgorithms.Kex] || !caps.answered[sshalgorithms.Ciphers] || caps.answered[sshalgorithms.MACs] || caps.answered[sshalgorithms.HostKey] {
		t.Fatalf("answered %v", caps.answered)
	}
	lists := sshalgorithms.Apply(cfg.SSHAlgorithms(), map[string]any{"ciphers-append": []any{"aes128-ctr", "aes128-cbc"}})
	f := Factory{Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), Algorithms: lists}
	f.offered, err = f.offeredAlgorithms(caps.implements)
	if err != nil {
		t.Fatal(err)
	}
	text, err := f.renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"  KexAlgorithms sntrup761x25519-sha512@openssh.com,curve25519-sha256,ecdh-sha2-nistp256,diffie-hellman-group14-sha1\n",
		"  Ciphers aes256-gcm@openssh.com,aes256-ctr,aes128-ctr,aes128-cbc\n",
		"  MACs hmac-sha2-512-etm@openssh.com,hmac-sha2-256-etm@openssh.com,hmac-sha2-512,hmac-sha2-256,hmac-sha1\n",
		"  HostKeyAlgorithms ssh-ed25519,ecdsa-sha2-nistp521,ecdsa-sha2-nistp384,ecdsa-sha2-nistp256,rsa-sha2-512,rsa-sha2-256,ssh-rsa\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("want %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Host legacy") || strings.Contains(text, "PubkeyAcceptedAlgorithms") {
		t.Fatalf("a legacy block remains:\n%s", text)
	}

	// A list the binary implements none of fails before any connection.
	f = Factory{Binary: bin, Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: t.TempDir(), Algorithms: sshalgorithms.Apply(cfg.SSHAlgorithms(), map[string]any{"kex": []any{"mlkem768x25519-sha256"}})}
	_, err = f.Open(context.Background(), platform.OpenRequest{Address: "192.0.2.10", Port: 22, Username: "operator", Metadata: map[string]string{"canonical_name": "switch01"}})
	if errorcodes.Of(err) != "ssh_algorithms_unavailable" || !strings.Contains(err.Error(), "key exchange algorithms (mlkem768x25519-sha256) is implemented by system") {
		t.Fatalf("unavailable: %v", err)
	}
}

// TestGeneratedConfigUnderTheTransportsSource: lists holding the host-key
// list alone (ssh-algorithms.source "transport") write HostKeyAlgorithms and
// no other algorithm line, so OpenSSH's own lists apply.
func TestGeneratedConfigUnderTheTransportsSource(t *testing.T) {
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", `ssh.host-key-policy="insecure"`, `ssh-algorithms.source="transport"`}})
	if err != nil {
		t.Fatal(err)
	}
	selection, err := cfg.SelectSSHAlgorithms(matching.Fields{Name: "switch01"})
	if err != nil || selection.Source != sshalgorithms.SourceTransport {
		t.Fatalf("selection %+v %v", selection, err)
	}
	f := Factory{Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), Algorithms: selection.Lists}
	if f.offered, err = f.offeredAlgorithms(binaryCapabilities(cfg, fakeQueryBinary(t)).implements); err != nil {
		t.Fatal(err)
	}
	text, err := f.renderConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "  HostKeyAlgorithms ssh-ed25519,") {
		t.Fatalf("no host-key line:\n%s", text)
	}
	for _, option := range []string{"KexAlgorithms", "Ciphers", "MACs"} {
		if strings.Contains(text, option) {
			t.Fatalf("%s written under the transport's source:\n%s", option, text)
		}
	}
}

// TestEffectiveAlgorithms: ssh -G over the session's arguments gives
// OpenSSH's own lists, by its names for them; a failing ssh -G gives none.
func TestEffectiveAlgorithms(t *testing.T) {
	cfg, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ssh")
	script := "#!/bin/sh\n[ \"$1 $2 $3\" = \"-G -F /cfg\" ] || exit 255\nprintf 'user operator\\nciphers chacha20-poly1305@openssh.com,aes128-ctr\\nkexalgorithms mlkem768x25519-sha256\\nmacs umac-64-etm@openssh.com\\nhostkeyalgorithms ssh-ed25519\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	got := effectiveAlgorithms(cfg, bin, []string{"-F", "/cfg", "192.0.2.10"})
	want := sshalgorithms.Lists{sshalgorithms.Ciphers: {"chacha20-poly1305@openssh.com", "aes128-ctr"}, sshalgorithms.Kex: {"mlkem768x25519-sha256"}, sshalgorithms.MACs: {"umac-64-etm@openssh.com"}, sshalgorithms.HostKey: {"ssh-ed25519"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("effective %v, want %v", got, want)
	}
	if got := effectiveAlgorithms(cfg, bin, []string{"-F", "/other", "192.0.2.10"}); len(got) != 0 {
		t.Fatalf("a failing ssh -G gave %v", got)
	}
}

// TestEffectiveListsOncePerJob: two devices of one job whose negotiation
// fails on a list OpenSSH chose start one ssh -G and name the same lists;
// another job starts its own; without a job's cache each failure starts
// one; an ssh -G that gives nothing names the transport's own list.
func TestEffectiveListsOncePerJob(t *testing.T) {
	cfg, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	bin := filepath.Join(dir, "ssh")
	script := "#!/bin/sh\necho \"$*\" >>" + calls + "\n[ \"$1 $2 $3\" = \"-G -F /cfg\" ] || exit 255\nprintf 'ciphers chacha20-poly1305@openssh.com,aes128-ctr\\n'\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	count := func() int {
		data, _ := os.ReadFile(calls)
		return strings.Count(string(data), "\n")
	}
	transport := sshalgorithms.Lists{sshalgorithms.HostKey: {"ssh-ed25519"}}
	fail := func(job *EffectiveLists, address, configPath string) error {
		d := &Driver{f: Factory{Config: cfg, Effective: job, offered: transport}, req: platform.OpenRequest{Address: address, Port: 22, Username: "operator"}, binary: bin, configPath: configPath}
		return d.sessionFailure().classify("Unable to negotiate with "+address+" port 22: no matching cipher found. Their offer: aes256-cbc", nil)
	}
	job := &EffectiveLists{}
	for _, address := range []string{"192.0.2.10", "192.0.2.11"} {
		if err := fail(job, address, "/cfg"); !strings.HasSuffix(err.Error(), "system offered chacha20-poly1305@openssh.com,aes128-ctr; the device offered aes256-cbc") {
			t.Fatalf("%s: %v", address, err)
		}
	}
	if n := count(); n != 1 {
		t.Fatalf("one job's two failures started %d ssh -G", n)
	}
	_ = fail(&EffectiveLists{}, "192.0.2.12", "/cfg")
	_ = fail(nil, "192.0.2.13", "/cfg")
	_ = fail(nil, "192.0.2.14", "/cfg")
	if n := count(); n != 4 {
		t.Fatalf("another job and two uncached failures: %d ssh -G in all, want 4", n)
	}
	if err := fail(nil, "192.0.2.15", "/other"); !strings.HasSuffix(err.Error(), "system offered its own list; the device offered aes256-cbc") {
		t.Fatalf("an ssh -G that gave nothing: %v", err)
	}
}

func TestNegotiationFailureDiagnostic(t *testing.T) {
	offered := sshalgorithms.Defaults()
	kind, err, ok := negotiationFailure("Unable to negotiate with 192.0.2.10 port 22: no matching key exchange method found. Their offer: diffie-hellman-group1-sha1,kex-strict-s-v00@openssh.com\r\n", offered, nil)
	if !ok || kind != sshalgorithms.Kex || errorcodes.Of(err) != "ssh_algorithm_negotiation_failed" || !strings.Contains(err.Error(), "the device offered diffie-hellman-group1-sha1") {
		t.Fatalf("%s %v %t", kind, err, ok)
	}
	if kind, _, ok := negotiationFailure("Unable to negotiate with 192.0.2.10 port 22: no matching cipher found. Their offer: aes128-cbc,3des-cbc", offered, nil); !ok || kind != sshalgorithms.Ciphers {
		t.Fatalf("cipher: %s %t", kind, ok)
	}
	if _, _, ok := negotiationFailure("Permission denied (password).", offered, nil); ok {
		t.Fatal("matched an authentication failure")
	}
	// A list karvi does not hold is OpenSSH's own, named as the system's.
	transport := sshalgorithms.Lists{sshalgorithms.HostKey: offered[sshalgorithms.HostKey]}
	effective := func() sshalgorithms.Lists {
		return sshalgorithms.Lists{sshalgorithms.Ciphers: {"chacha20-poly1305@openssh.com", "aes128-ctr"}}
	}
	if _, err, ok := negotiationFailure("Unable to negotiate with 192.0.2.10 port 22: no matching cipher found. Their offer: aes256-cbc", transport, effective); !ok || !strings.HasSuffix(err.Error(), "no cipher algorithm in common: system offered chacha20-poly1305@openssh.com,aes128-ctr; the device offered aes256-cbc") {
		t.Fatalf("under the transport's source: %v %t", err, ok)
	}
}

// TestHostKeyAliasIsTheIdentity: OpenSSH
// checks the device under the canonical name on port 22 and [canonical]:PORT
// on any other port.
func TestHostKeyAliasIsTheIdentity(t *testing.T) {
	home := t.TempDir()
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.include-user-config=false", `ssh.host-key-policy="insecure"`}})
	if err != nil {
		t.Fatal(err)
	}
	for port, want := range map[uint16]string{22: "HostKeyAlias=switch01", 2222: "HostKeyAlias=[switch01]:2222"} {
		f := Factory{Binary: fakeQueryBinary(t), Config: cfg, Home: home, BaseDir: filepath.Join(home, ".local", "share", "karvi"), ScratchDir: t.TempDir()}
		driver, err := f.Open(context.Background(), platform.OpenRequest{Address: "192.0.2.10", Port: port, Username: "operator", Metadata: map[string]string{"canonical_name": "switch01"}})
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Join(driver.(*Driver).baseArgs(), " ")
		driver.Close()
		if !strings.Contains(args, " "+want+" ") {
			t.Fatalf("port %d: %s", port, args)
		}
	}
}
