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
	if err := Enroll(known, "router1", 22, first); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(known)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "router1 ssh-ed25519") || strings.Contains(string(data), "192.0.2.10") {
		t.Fatalf("enrollment=%q", data)
	}
	if err := Enroll(known, "router1", 22, first); err != nil {
		t.Fatalf("idempotent enroll: %v", err)
	}
	changed := []keyRecord{{Type: "ssh-ed25519", Blob: fakeKey(8)}}
	if err := Enroll(known, "router1", 22, changed); err == nil || !strings.Contains(err.Error(), "host_key_changed") {
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

// TestWarnInsecureSystemComparesTheIdentity covers the system transport's
// insecure warning: the enrolled key is looked
// up under the device's identity on its port.
func TestWarnInsecureSystemComparesTheIdentity(t *testing.T) {
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
		var warnings []string
		WarnInsecureSystem(context.Background(), policy, "router1", "192.0.2.10", tc.port, func(m string) { warnings = append(warnings, m) })
		got := len(warnings) == 2 && strings.Contains(warnings[1], "mismatch accepted only because policy=insecure")
		if got != tc.mismatch || len(warnings) == 0 {
			t.Fatalf("port %d: warnings %q", tc.port, warnings)
		}
	}
}
