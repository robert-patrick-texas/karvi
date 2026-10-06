package credfile

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func userRules() Rules {
	return Rules{BackendName: "creds", Scope: ScopeUser, UID: os.Getuid()}
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// TestOpenReturnsTheCheckedDescriptor: the file
// handed back is the one that was checked, so replacing the path after
// Open does not change what the backend reads.
func TestOpenReturnsTheCheckedDescriptor(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "creds.csv")
	write(t, p, "checked", 0o600)
	f, canonical, err := userRules().Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if canonical != p {
		t.Errorf("canonical %s, want %s", canonical, p)
	}
	// Swap the path for a file that would fail the mode check.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	write(t, p, "swapped", 0o644)
	body, err := io.ReadAll(f)
	if err != nil || string(body) != "checked" {
		t.Errorf("read %q, %v; want the checked file's content", body, err)
	}
}

// TestOpenCodes walks the user-scope checks; every message names the
// backend and the path.
func TestOpenCodes(t *testing.T) {
	d := t.TempDir()
	wide := filepath.Join(d, "wide")
	write(t, wide, "x", 0o644)
	link := filepath.Join(d, "link")
	if err := os.Symlink(wide, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(d, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, code string }{
		{"mode", wide, "credential_file_mode_unsafe"},
		{"symlink", link, "credential_file_symlink_rejected"},
		{"directory", d, "credential_file_not_regular"},
		{"fifo", fifo, "credential_file_not_regular"},
	} {
		done := make(chan error, 1)
		go func() {
			f, _, err := userRules().Open(tc.path)
			if err == nil {
				f.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if errorcodes.Of(err) != tc.code || !strings.Contains(err.Error(), "creds") || !strings.Contains(err.Error(), tc.path) {
				t.Errorf("%s: %v", tc.name, err)
			}
			if IsUnavailable(err) {
				t.Errorf("%s: a failed check reads as unavailable", tc.name)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: Open blocked", tc.name)
		}
	}
	other := userRules()
	other.UID = os.Getuid() + 1
	tight := filepath.Join(d, "tight")
	write(t, tight, "x", 0o600)
	if _, _, err := other.Open(tight); errorcodes.Of(err) != "credential_file_owner_mismatch" {
		t.Errorf("owner: %v", err)
	}
}

// TestPresentUnreadableFileFailsItsCheck: a file the operator can see but
// not open (mode 0000) is reported by its check, not passed over as
// unavailable; an absent one is unavailable.
func TestPresentUnreadableFileFailsItsCheck(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root opens a mode-0 file")
	}
	d := t.TempDir()
	p := filepath.Join(d, "closed")
	write(t, p, "x", 0)
	if _, _, err := userRules().Open(p); errorcodes.Of(err) != "credential_file_mode_unsafe" {
		t.Errorf("mode 0000: %v", err)
	}
	if _, _, err := userRules().Open(filepath.Join(d, "absent")); !IsUnavailable(err) || errorcodes.Of(err) != "" {
		t.Errorf("absent: %v", err)
	}
}

// TestAllowedSymlink: with security.allow-credential-symlinks the target
// is opened and checked, and the canonical path names the target.
func TestAllowedSymlink(t *testing.T) {
	d := t.TempDir()
	target := filepath.Join(d, "target")
	write(t, target, "x", 0o600)
	link := filepath.Join(d, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	r := userRules()
	r.AllowSymlink = true
	f, canonical, err := r.Open(link)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	want, _ := filepath.EvalSymlinks(target)
	if canonical != want {
		t.Errorf("canonical %s, want %s", canonical, want)
	}
	// The target's own mode is what is checked.
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.Open(link); errorcodes.Of(err) != "credential_file_mode_unsafe" {
		t.Errorf("wide target: %v", err)
	}
}

func TestExpandPath(t *testing.T) {
	u := Rules{Scope: ScopeUser, Home: "/home/op"}
	s := Rules{Scope: ScopeShared, Home: "/home/op"}
	for _, tc := range []struct {
		r        Rules
		in, want string
	}{
		{u, "~/creds.csv", "/home/op/creds.csv"},
		{u, "~", "/home/op"},
		{u, "/etc/x", "/etc/x"},
		{u, "~other/x", ""},
		{s, "~/creds.csv", "~/creds.csv"},
	} {
		if got, _ := tc.r.ExpandPath(tc.in); got != tc.want {
			t.Errorf("%s %q: %q, want %q", tc.r.EffectiveScope(), tc.in, got, tc.want)
		}
	}
	if _, err := u.ExpandPath("~other/x"); errorcodes.Of(err) != "path_other_user_home_unsupported" {
		t.Errorf("~other: %v", err)
	}
}

// TestClassify covers Classify with the backend's own
// parse-failure code for an uncoded error.
func TestClassify(t *testing.T) {
	unavailable := &UnavailableError{Err: os.ErrNotExist}
	optional := Rules{BackendName: "creds", Scope: ScopeUser}
	if r := optional.Classify("/p", unavailable, "x_malformed"); r.Outcome != credentials.NotFound || r.ErrorCode != "" || r.Message != "" {
		t.Errorf("optional: %+v", r)
	}
	required := Rules{BackendName: "creds", Scope: ScopeUser, Required: true}
	if r := required.Classify("/p", unavailable, "x_malformed"); r.ErrorCode != "credential_file_unavailable" || !strings.Contains(r.Message, "creds") || !strings.Contains(r.Message, "/p") {
		t.Errorf("required: %+v", r)
	}
	shared := Rules{BackendName: "creds", Scope: ScopeShared}
	if r := shared.Classify("/p", unavailable, "x_malformed"); r.ErrorCode != "credential_file_unavailable" || !strings.Contains(r.Message, "shared-scope") {
		t.Errorf("shared: %+v", r)
	}
	if r := optional.Classify("/p", errors.New("line 3: bad"), "x_malformed"); r.Outcome != credentials.Malformed || r.ErrorCode != "x_malformed" {
		t.Errorf("uncoded: %+v", r)
	}
	if r := optional.Classify("/p", errorcodes.Errorf("credential_file_mode_unsafe", "m"), "x_malformed"); r.ErrorCode != "credential_file_mode_unsafe" {
		t.Errorf("coded: %+v", r)
	}
}

// TestCache: one read, a cached failure, and a
// cancelled context left uncached.
func TestCache(t *testing.T) {
	classify := func(err error) credentials.BackendResult {
		return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "x_malformed", Message: err.Error()}
	}
	var c Cache[[]string]
	reads := 0
	read := func() ([]string, error) { reads++; return []string{"row"}, nil }
	for i := 0; i < 3; i++ {
		if v, failed := c.Load(context.Background(), read, classify); failed != nil || len(v) != 1 {
			t.Fatalf("%v %+v", v, failed)
		}
	}
	if reads != 1 {
		t.Errorf("%d reads, want 1", reads)
	}

	var cancelled Cache[[]string]
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, failed := cancelled.Load(ctx, func() ([]string, error) { return nil, ctx.Err() }, classify); failed == nil || failed.ErrorCode != "credential_backend_error" || !failed.Retryable {
		t.Errorf("cancelled: %+v", failed)
	}
	if v, failed := cancelled.Load(context.Background(), read, classify); failed != nil || len(v) != 1 {
		t.Errorf("a cancelled load was cached: %v %+v", v, failed)
	}

	var failing Cache[[]string]
	fails := 0
	bad := func() ([]string, error) { fails++; return nil, errors.New("bad") }
	for i := 0; i < 2; i++ {
		if _, failed := failing.Load(context.Background(), bad, classify); failed == nil || failed.ErrorCode != "x_malformed" {
			t.Errorf("%+v", failed)
		}
	}
	if fails != 1 {
		t.Errorf("%d failing reads, want 1", fails)
	}
}
