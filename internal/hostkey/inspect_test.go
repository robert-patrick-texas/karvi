package hostkey

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fakeKey(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed + byte(i)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func fakeScanner(t *testing.T, blob string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ssh-keyscan")
	script := "#!/bin/sh\nprintf '%s\\n' \"device ssh-ed25519 " + blob + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectMatchMismatchAndUnknown(t *testing.T) {
	home := t.TempDir()
	known := filepath.Join(home, "known_hosts")
	enrolled := fakeKey(1)
	if err := os.WriteFile(known, []byte("router1,192.0.2.10 ssh-ed25519 "+enrolled+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	got, err := inspectRemoteWithBinary(ctx, fakeScanner(t, enrolled), known, "router1", "192.0.2.10", 22, time.Second)
	if err != nil || got.Comparison != Match {
		t.Fatalf("match=%+v err=%v", got, err)
	}
	got, err = inspectRemoteWithBinary(ctx, fakeScanner(t, fakeKey(9)), known, "router1", "192.0.2.10", 22, time.Second)
	if err != nil || got.Comparison != Mismatch {
		t.Fatalf("mismatch=%+v err=%v", got, err)
	}
	got, err = inspectRemoteWithBinary(ctx, fakeScanner(t, fakeKey(9)), known, "router2", "192.0.2.11", 22, time.Second)
	if err != nil || got.Comparison != Unknown {
		t.Fatalf("unknown=%+v err=%v", got, err)
	}
}

func TestEnrollAppendsAndRejectsChange(t *testing.T) {
	home := t.TempDir()
	known := filepath.Join(home, "known_hosts")
	if err := os.WriteFile(known, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	first := []keyRecord{{Type: "ssh-ed25519", Blob: fakeKey(1)}}
	if wrote, err := Enroll(known, "router1", 22, first); err != nil || !wrote {
		t.Fatalf("first enroll: wrote=%t %v", wrote, err)
	}
	data, err := os.ReadFile(known)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "router1 ssh-ed25519") || strings.Contains(string(data), "192.0.2.10") {
		t.Fatalf("enrollment=%q", data)
	}
	// The same key stored meanwhile by another process: nothing written,
	// and wrote false, so that only the process that stored it says so.
	if wrote, err := Enroll(known, "router1", 22, first); err != nil || wrote {
		t.Fatalf("idempotent enroll: wrote=%t %v", wrote, err)
	}
	changed := []keyRecord{{Type: "ssh-ed25519", Blob: fakeKey(8)}}
	if _, err := Enroll(known, "router1", 22, changed); err == nil || !strings.Contains(err.Error(), "host_key_changed") {
		t.Fatalf("expected change rejection, got %v", err)
	}
}

// TestIdentityPerPort: the canonical name on port 22, [canonical]:PORT elsewhere, and no address in either.
func TestIdentityPerPort(t *testing.T) {
	for _, tc := range []struct {
		host string
		port int
		want string
	}{{"router1", 22, "router1"}, {"router1", 0, "router1"}, {"router1", 2222, "[router1]:2222"}, {"[router1]", 830, "[router1]:830"}, {"192.0.2.10", 2222, "[192.0.2.10]:2222"}} {
		if got := Identity(tc.host, tc.port); got != tc.want {
			t.Fatalf("Identity(%q, %d) = %q, want %q", tc.host, tc.port, got, tc.want)
		}
	}
	known := filepath.Join(t.TempDir(), "known_hosts")
	store := "[router1]:2222 ssh-ed25519 " + fakeKey(1) + "\nrouter2 ssh-ed25519 " + fakeKey(2) + "\n[192.0.2.30]:2222 ssh-ed25519 " + fakeKey(3) + "\n"
	if err := os.WriteFile(known, []byte(store), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host string
		port int
		want bool
	}{{"router1", 2222, true}, {"router1", 22, false}, {"router1", 2223, false}, {"router2", 22, true}, {"router2", 2222, false}, {"router3", 2222, false}} {
		if got := HasEnrolledHost(known, tc.host, tc.port); got != tc.want {
			t.Fatalf("HasEnrolledHost(%q, %d) = %t", tc.host, tc.port, got)
		}
	}
}

func installKeyscanOnPath(t *testing.T, blob string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh-keyscan")
	script := "#!/bin/sh\nprintf '%s\\n' \"device ssh-ed25519 " + blob + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// TestCompareInsecureComparesTheIdentity covers the system transport's
// insecure comparison: the enrolled key is looked up under the device's
// identity on its port, a differing key is a KeyMismatch with both
// fingerprints, a device the store does not hold is not compared, a
// failed ssh-keyscan is the reason, and another policy compares nothing.
func TestCompareInsecureComparesTheIdentity(t *testing.T) {
	known := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(known, []byte("[router1]:2222 ssh-ed25519 "+fakeKey(1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	installKeyscanOnPath(t, fakeKey(8))
	policy := Policy{Mode: Insecure, KnownHostsFile: known}
	for _, tc := range []struct {
		port     int
		mismatch bool
	}{{2222, true}, {22, false}, {2223, false}} {
		m, nc := CompareInsecure(context.Background(), policy, "router1", "192.0.2.10", tc.port)
		if (m != nil) != tc.mismatch || nc != nil {
			t.Fatalf("port %d: %+v %+v", tc.port, m, nc)
		}
		if m != nil && (len(m.Enrolled) != 1 || len(m.Presented) == 0 || !strings.HasPrefix(m.Enrolled[0], "ssh-ed25519 SHA256:")) {
			t.Fatalf("port %d: fingerprints %+v", tc.port, m)
		}
	}
	if m, nc := CompareInsecure(context.Background(), Policy{Mode: AcceptNew, KnownHostsFile: known}, "router1", "192.0.2.10", 2222); m != nil || nc != nil {
		t.Fatalf("accept-new compared: %+v %+v", m, nc)
	}
	t.Setenv("PATH", t.TempDir())
	if m, nc := CompareInsecure(context.Background(), policy, "router1", "192.0.2.10", 2222); m != nil || nc == nil || nc.Cause != NotComparedNoScanner || !strings.Contains(nc.Reason, "dependency_ssh_keyscan_unavailable") {
		t.Fatalf("no ssh-keyscan: %+v %+v", m, nc)
	}
}

// TestNotComparedCauses: an ssh-keyscan that ends without a key is "timed
// out" at or past its bound and "failed" before it, as ssh-keyscan reports
// both alike; one that prints nothing usable is "no usable key".
func TestNotComparedCauses(t *testing.T) {
	known := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(known, []byte("router1 ssh-ed25519 "+fakeKey(1)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ script, cause string }{
		{"sleep 1.2; exit 1", NotComparedTimedOut},
		{"exit 1", NotComparedScanFailed},
		{"exit 0", NotComparedNoKey},
	} {
		scanner := filepath.Join(t.TempDir(), "ssh-keyscan")
		if err := os.WriteFile(scanner, []byte("#!/bin/sh\n"+tc.script+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := inspectRemoteWithBinary(context.Background(), scanner, known, "router1", "192.0.2.10", 22, time.Second)
		if err == nil || notComparedCause(err) != tc.cause {
			t.Errorf("%q: %v, cause %q, want %q", tc.script, err, notComparedCause(err), tc.cause)
		}
	}
}
