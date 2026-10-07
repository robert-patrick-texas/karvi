package hostkey

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func wire(t *testing.T, blob string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func store(t *testing.T, content string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "karvi")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "known_hosts")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHostKeyAlgorithmsStrongestFirstAndFiltered(t *testing.T) {
	names := Identity("router1", 22)
	whole := []string{"ssh-ed25519", "ecdsa-sha2-nistp521", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"}
	cases := []struct {
		name    string
		mode    Mode
		content string
		want    []string
		code    string
	}{
		{name: "no entry", mode: AcceptNew, content: "other ssh-ed25519 " + fakeKey(1) + "\n", want: whole},
		{name: "ed25519 enrolled", mode: Secure, content: "router1 ssh-ed25519 " + fakeKey(1) + "\n", want: []string{"ssh-ed25519"}},
		{name: "rsa enrolled", mode: AcceptNew, content: "router1 ssh-rsa " + fakeKey(2) + "\n", want: []string{"rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"}},
		{name: "several types keep the preference order", mode: AcceptNew, content: "router1 ssh-rsa " + fakeKey(2) + "\nrouter1 ecdsa-sha2-nistp256 " + fakeKey(3) + "\nrouter1 ecdsa-sha2-nistp521 " + fakeKey(4) + "\n", want: []string{"ecdsa-sha2-nistp521", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"}},
		{name: "insecure ignores entries", mode: Insecure, content: "router1 ssh-rsa " + fakeKey(2) + "\n", want: whole},
		{name: "only a type karvi never offers", mode: AcceptNew, content: "router1 ssh-dss " + fakeKey(5) + "\n", code: "host_key_changed"},
	}
	for _, c := range cases {
		got, err := Policy{Mode: c.mode, KnownHostsFile: store(t, c.content)}.HostKeyAlgorithms(whole, names)
		if c.code != "" {
			if err == nil || err.(*Error).Code != c.code {
				t.Fatalf("%s: got %q err=%v, want %s", c.name, got, err, c.code)
			}
			continue
		}
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s: got %q err=%v, want %q", c.name, got, err, c.want)
		}
	}
	missing := Policy{Mode: Secure, KnownHostsFile: filepath.Join(t.TempDir(), "absent")}
	if got, err := missing.HostKeyAlgorithms(whole, names); err != nil || !reflect.DeepEqual(got, whole) {
		t.Fatalf("missing store: %q %v", got, err)
	}
	if got, err := (Policy{Mode: AcceptNew, KnownHostsFile: store(t, "router1 ssh-rsa "+fakeKey(2)+"\n")}).HostKeyAlgorithms([]string{"ssh-ed25519"}, names); err == nil || err.(*Error).Code != "host_key_changed" {
		t.Fatalf("a list without the enrolled type: %q %v", got, err)
	}
}

func TestVerifyModes(t *testing.T) {
	enrolled := fakeKey(1)
	entry := "router1 ssh-ed25519 " + enrolled + "\n"
	var warnings []string
	warn := func(m string) { warnings = append(warnings, m) }

	// accept-new: a match, a changed key, and an unknown host enrolled once.
	known := store(t, entry)
	p := Policy{Mode: AcceptNew, KnownHostsFile: known}
	if _, err := Verify(p, "router1", 22, "ssh-ed25519", wire(t, enrolled), warn); err != nil {
		t.Fatalf("match: %v", err)
	}
	_, err := Verify(p, "router1", 22, "ssh-ed25519", wire(t, fakeKey(9)), warn)
	if err == nil || err.(*Error).Code != "host_key_changed" || !strings.Contains(err.Error(), "enrolled=ssh-ed25519 SHA256:") || !strings.Contains(err.Error(), "presented=ssh-ed25519 SHA256:") {
		t.Fatalf("changed: %v", err)
	}
	_, err = Verify(p, "router1", 22, "ecdsa-sha2-nistp256", wire(t, fakeKey(8)), warn)
	if err == nil || err.(*Error).Code != "host_key_changed" {
		t.Fatalf("another type for a known host: %v", err)
	}
	warnings = nil
	stored, err := Verify(p, "router2", 830, "ssh-ed25519", wire(t, fakeKey(7)), warn)
	if err != nil || !stored {
		t.Fatalf("enroll: stored=%t %v", stored, err)
	}
	content, _ := os.ReadFile(known)
	if !strings.Contains(string(content), "\n[router2]:830 ssh-ed25519 "+fakeKey(7)+" karvi-auto-enrolled") {
		t.Fatalf("store after enrollment:\n%s", content)
	}
	// The enrollment is the caller's to say (EnrolledMessage), not a warning.
	if len(warnings) != 0 {
		t.Fatalf("warnings %q", warnings)
	}
	if stored, err := Verify(p, "router2", 830, "ssh-ed25519", wire(t, fakeKey(7)), warn); err != nil || stored {
		t.Fatalf("enrolled key on the next open: stored=%t %v", stored, err)
	}

	// secure: unknown and changed refused, nothing written.
	secureStore := store(t, entry)
	s := Policy{Mode: Secure, KnownHostsFile: secureStore}
	if _, err := Verify(s, "router3", 22, "ssh-ed25519", wire(t, fakeKey(6)), warn); err == nil || err.(*Error).Code != "host_key_not_enrolled" {
		t.Fatalf("secure unknown: %v", err)
	}
	if _, err := Verify(s, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6)), warn); err == nil || err.(*Error).Code != "host_key_changed" {
		t.Fatalf("secure changed: %v", err)
	}
	if after, _ := os.ReadFile(secureStore); string(after) != entry {
		t.Fatalf("secure wrote the store:\n%s", after)
	}

	// insecure: always accepted; the mismatch warning only for a known host.
	warnings = nil
	i := Policy{Mode: Insecure, KnownHostsFile: store(t, entry)}
	if _, err := Verify(i, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6)), warn); err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 2 || !strings.Contains(warnings[1], "mismatch accepted only because policy=insecure") {
		t.Fatalf("insecure changed warnings %q", warnings)
	}
	warnings = nil
	missing := Policy{Mode: Insecure, KnownHostsFile: filepath.Join(t.TempDir(), "absent")}
	if _, err := Verify(missing, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6)), warn); err != nil || len(warnings) != 1 {
		t.Fatalf("insecure without a store: %v %q", err, warnings)
	}
}

func TestTypesNotOfferedIsChanged(t *testing.T) {
	p := Policy{Mode: AcceptNew, KnownHostsFile: store(t, "router1 ssh-rsa "+fakeKey(2)+"\n")}
	err := TypesNotOffered(p, Identity("router1", 22), []string{"ssh-ed25519"})
	if err.(*Error).Code != "host_key_changed" || !strings.Contains(err.Error(), "enrolled=ssh-rsa SHA256:") || !strings.Contains(err.Error(), "offered: ssh-ed25519") {
		t.Fatalf("%v", err)
	}
}

// TestTypeLabelIsOpenSSHs: the label OpenSSH's "Permanently added" line
// gives for each key type, so both transports say one name.
func TestTypeLabelIsOpenSSHs(t *testing.T) {
	for keyType, want := range map[string]string{
		"ssh-ed25519": "ED25519", "ecdsa-sha2-nistp256": "ECDSA", "ecdsa-sha2-nistp521": "ECDSA",
		"ssh-rsa": "RSA", "rsa-sha2-512": "RSA", "ssh-dss": "DSA",
		"sk-ssh-ed25519@openssh.com": "ED25519-SK", "sk-ecdsa-sha2-nistp256@openssh.com": "ECDSA-SK",
	} {
		if got := TypeLabel(keyType); got != want {
			t.Errorf("TypeLabel(%q) = %q, want %q", keyType, got, want)
		}
	}
	if got := EnrolledMessage("router2", "ED25519"); got != "ssh accepted new host key for router2 (ED25519)" {
		t.Fatalf("message %q", got)
	}
}
