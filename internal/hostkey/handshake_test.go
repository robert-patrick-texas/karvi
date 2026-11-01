package hostkey

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
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

	// accept-new: a match, a changed key, and an unknown host enrolled once.
	known := store(t, entry)
	p := Policy{Mode: AcceptNew, KnownHostsFile: known}
	if v, err := Verify(p, "router1", 22, "ssh-ed25519", wire(t, enrolled)); err != nil || v != (Verdict{}) {
		t.Fatalf("match: %+v %v", v, err)
	}
	_, err := Verify(p, "router1", 22, "ssh-ed25519", wire(t, fakeKey(9)))
	if err == nil || err.(*Error).Code != "host_key_changed" || !strings.Contains(err.Error(), "enrolled=ssh-ed25519 SHA256:") || !strings.Contains(err.Error(), "presented=ssh-ed25519 SHA256:") {
		t.Fatalf("changed: %v", err)
	}
	_, err = Verify(p, "router1", 22, "ecdsa-sha2-nistp256", wire(t, fakeKey(8)))
	if err == nil || err.(*Error).Code != "host_key_changed" {
		t.Fatalf("another type for a known host: %v", err)
	}
	v, err := Verify(p, "router2", 830, "ssh-ed25519", wire(t, fakeKey(7)))
	if err != nil || !v.Stored || v.Mismatch != nil {
		t.Fatalf("enroll: %+v %v", v, err)
	}
	content, _ := os.ReadFile(known)
	if !strings.Contains(string(content), "\n[router2]:830 ssh-ed25519 "+fakeKey(7)+" karvi-auto-enrolled") {
		t.Fatalf("store after enrollment:\n%s", content)
	}
	if v, err := Verify(p, "router2", 830, "ssh-ed25519", wire(t, fakeKey(7))); err != nil || v.Stored {
		t.Fatalf("enrolled key on the next open: %+v %v", v, err)
	}

	// secure: unknown and changed refused, nothing written.
	secureStore := store(t, entry)
	s := Policy{Mode: Secure, KnownHostsFile: secureStore}
	if _, err := Verify(s, "router3", 22, "ssh-ed25519", wire(t, fakeKey(6))); err == nil || err.(*Error).Code != "host_key_not_enrolled" {
		t.Fatalf("secure unknown: %v", err)
	}
	if _, err := Verify(s, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6))); err == nil || err.(*Error).Code != "host_key_changed" {
		t.Fatalf("secure changed: %v", err)
	}
	if after, _ := os.ReadFile(secureStore); string(after) != entry {
		t.Fatalf("secure wrote the store:\n%s", after)
	}

	// insecure: always accepted, nothing stored; a key differing from the
	// stored one is in the Verdict with both fingerprints, an unknown or a
	// matching one is not.
	insecureStore := store(t, entry)
	i := Policy{Mode: Insecure, KnownHostsFile: insecureStore}
	v, err = Verify(i, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6)))
	if err != nil || v.Stored || v.Mismatch == nil || len(v.Mismatch.Enrolled) != 1 || len(v.Mismatch.Presented) != 1 ||
		!strings.HasPrefix(v.Mismatch.Enrolled[0], "ssh-ed25519 SHA256:") || v.Mismatch.Enrolled[0] == v.Mismatch.Presented[0] {
		t.Fatalf("insecure changed: %+v %v", v, err)
	}
	if v, err := Verify(i, "router1", 22, "ssh-ed25519", wire(t, enrolled)); err != nil || v != (Verdict{}) {
		t.Fatalf("insecure match: %+v %v", v, err)
	}
	if v, err := Verify(i, "router9", 22, "ssh-ed25519", wire(t, fakeKey(6))); err != nil || v != (Verdict{}) {
		t.Fatalf("insecure unknown: %+v %v", v, err)
	}
	if after, _ := os.ReadFile(insecureStore); string(after) != entry {
		t.Fatalf("insecure wrote the store:\n%s", after)
	}
	missing := Policy{Mode: Insecure, KnownHostsFile: filepath.Join(t.TempDir(), "absent")}
	if v, err := Verify(missing, "router1", 22, "ssh-ed25519", wire(t, fakeKey(6))); err != nil || v != (Verdict{}) {
		t.Fatalf("insecure without a store: %+v %v", v, err)
	}
}

// TestRevokedKeyRefusedUnderEveryPolicy: a presented key the store marks
// @revoked for the device is host_key_revoked under accept-new, secure, and
// insecure, whether the line names the device or every host and whether a
// plain line holds the same key; nothing is stored.
func TestRevokedKeyRefusedUnderEveryPolicy(t *testing.T) {
	revoked := fakeKey(1)
	for _, content := range []string{
		"@revoked router1 ssh-ed25519 " + revoked + "\n",
		"@revoked * ssh-ed25519 " + revoked + "\n",
		"router1 ssh-ed25519 " + revoked + "\n@revoked * ssh-ed25519 " + revoked + "\n",
	} {
		for _, mode := range []Mode{AcceptNew, Secure, Insecure} {
			file := store(t, content)
			v, err := Verify(Policy{Mode: mode, KnownHostsFile: file}, "router1", 22, "ssh-ed25519", wire(t, revoked))
			if errorcodes.Of(err) != "host_key_revoked" || v != (Verdict{}) || !strings.Contains(err.Error(), "presented=ssh-ed25519 SHA256:") {
				t.Errorf("%s, %q: %+v %v", mode, content, v, err)
			}
			if after, _ := os.ReadFile(file); string(after) != content {
				t.Errorf("%s, %q: the store changed:\n%s", mode, content, after)
			}
		}
	}
}

// TestMarkerLinesAreNotEnrolledKeys: a line marked @cert-authority, one
// marked @revoked for another key, and one with a marker OpenSSH does not
// define hold no enrolled key for the device: accept-new enrols its plain
// key, secure refuses it as host_key_not_enrolled, insecure reports no
// mismatch, the whole host-key list is offered, and HasEnrolledHost is
// false.
func TestMarkerLinesAreNotEnrolledKeys(t *testing.T) {
	host := "core-nyc-01.example.net"
	presented := fakeKey(5)
	whole := []string{"ssh-ed25519", "ecdsa-sha2-nistp256", "rsa-sha2-512"}
	for _, content := range []string{
		"@cert-authority *.example.net ssh-ed25519 " + fakeKey(1) + "\n",
		"@cert-authority core-nyc-01.example.net ecdsa-sha2-nistp256 " + fakeKey(2) + "\n",
		"@revoked core-nyc-01.example.net ssh-ed25519 " + fakeKey(3) + "\n",
		"@trusted core-nyc-01.example.net ssh-ed25519 " + fakeKey(4) + "\n",
	} {
		if HasEnrolledHost(store(t, content), host, 22) {
			t.Errorf("%q: HasEnrolledHost true", content)
		}
		if got, err := (Policy{Mode: Secure, KnownHostsFile: store(t, content)}).HostKeyAlgorithms(whole, Identity(host, 22)); err != nil || !reflect.DeepEqual(got, whole) {
			t.Errorf("%q: offered %q %v", content, got, err)
		}
		if _, err := Verify(Policy{Mode: Secure, KnownHostsFile: store(t, content)}, host, 22, "ssh-ed25519", wire(t, presented)); errorcodes.Of(err) != "host_key_not_enrolled" {
			t.Errorf("%q: secure %v", content, err)
		}
		if v, err := Verify(Policy{Mode: Insecure, KnownHostsFile: store(t, content)}, host, 22, "ssh-ed25519", wire(t, presented)); err != nil || v != (Verdict{}) {
			t.Errorf("%q: insecure %+v %v", content, v, err)
		}
		file := store(t, content)
		v, err := Verify(Policy{Mode: AcceptNew, KnownHostsFile: file}, host, 22, "ssh-ed25519", wire(t, presented))
		if err != nil || !v.Stored {
			t.Errorf("%q: accept-new %+v %v", content, v, err)
		}
		if after, _ := os.ReadFile(file); string(after) != content+host+" ssh-ed25519 "+presented+" karvi-auto-enrolled\n" {
			t.Errorf("%q: the store after accept-new:\n%s", content, after)
		}
	}
}

// TestInsecurePhrases: the two insecure notices' messages, the device's
// name between the phrase's two parts.
func TestInsecurePhrases(t *testing.T) {
	if got := MismatchPhrase().Text("r1"); got != "ssh host-key mismatch r1 proceeding at risk" {
		t.Fatalf("mismatch %q", got)
	}
	if got := NotComparedPhrase(NotComparedTimedOut).Text("r1"); got != "ssh host-key r1 not compared: ssh-keyscan timed out" {
		t.Fatalf("not compared %q", got)
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
	if got := EnrolledPhrase("ED25519").Text("router2"); got != "ssh accepted new host-key router2 (ED25519)" {
		t.Fatalf("message %q", got)
	}
}
