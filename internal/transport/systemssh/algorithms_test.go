package systemssh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
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

func TestNegotiationFailureDiagnostic(t *testing.T) {
	offered := sshalgorithms.Defaults()
	kind, err, ok := negotiationFailure("Unable to negotiate with 192.0.2.10 port 22: no matching key exchange method found. Their offer: diffie-hellman-group1-sha1,kex-strict-s-v00@openssh.com\r\n", offered)
	if !ok || kind != sshalgorithms.Kex || errorcodes.Of(err) != "ssh_algorithm_negotiation_failed" || !strings.Contains(err.Error(), "the device offered diffie-hellman-group1-sha1") {
		t.Fatalf("%s %v %t", kind, err, ok)
	}
	if kind, _, ok := negotiationFailure("Unable to negotiate with 192.0.2.10 port 22: no matching cipher found. Their offer: aes128-cbc,3des-cbc", offered); !ok || kind != sshalgorithms.Ciphers {
		t.Fatalf("cipher: %s %t", kind, ok)
	}
	if _, _, ok := negotiationFailure("Permission denied (password).", offered); ok {
		t.Fatal("matched an authentication failure")
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
