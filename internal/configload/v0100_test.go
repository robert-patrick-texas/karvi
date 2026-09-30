package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestV0100HostKeyPolicyValues(t *testing.T) {
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("ssh.host-key-policy"); got != "accept-new" {
		t.Fatalf("ssh.host-key-policy=%q, want accept-new", got)
	}
	for _, value := range []string{"accept-new", "secure", "insecure"} {
		set := "ssh.host-key-policy=\"" + value + "\""
		if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}}); err != nil {
			t.Fatalf("%s rejected: %v", set, err)
		}
	}
	for _, removed := range []string{"auto", "default"} {
		set := "ssh.host-key-policy=\"" + removed + "\""
		if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}}); err == nil {
			t.Fatalf("removed value accepted: %s", set)
		}
	}
}

func TestV0100PlatformControlMaster(t *testing.T) {
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{"platform.linux.control-master=true"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := snap.NamedTables("platform")["linux"]["control-master"].(bool); !ok || !got {
		t.Fatalf("platform.linux.control-master=%v, want true", snap.NamedTables("platform")["linux"]["control-master"])
	}
	if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{"platform.linux.control-master=\"yes\""}}); err == nil {
		t.Fatal("non-boolean platform control-master was accepted")
	}
}

// TestV0100HostKeyPolicyLock: with a global lock fixing ssh.host-key-policy
// to secure,
// insecure is refused from --set, from the cli layer the flag writes, and
// from the environment, each as config_lock_violation naming the lock,
// and the effective value stays secure; without the lock the same three
// paths select insecure. A lock declared in an explicit root is refused
// as config_lock_authority_violation.
func TestV0100HostKeyPolicyLock(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	locked := "[config-lock]\n\"ssh.host-key-policy\" = true\n[ssh]\nhost-key-policy = \"secure\"\n"
	unlocked := "[ssh]\nhost-key-policy = \"secure\"\n"
	saved := globalRoots
	globalRoots = []string{global}
	t.Cleanup(func() { globalRoots = saved })

	paths := []struct {
		name string
		opts Options
	}{
		{"set", Options{Sets: []string{`ssh.host-key-policy="insecure"`}}},
		{"cli", Options{FlagValues: map[string]any{"ssh.host-key-policy": "insecure"}}},
		{"env", Options{Environment: []string{"KARVI__SSH__HOST_KEY_POLICY=insecure"}}},
	}
	for _, body := range []string{locked, unlocked} {
		if err := os.WriteFile(global, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, p := range paths {
			opts := p.opts
			opts.HomeDir = t.TempDir()
			if opts.Environment == nil {
				opts.Environment = []string{}
			}
			snap, err := Load(opts)
			if body == unlocked {
				if err != nil || snap.String("ssh.host-key-policy") != "insecure" {
					t.Errorf("unlocked, %s: err=%v value=%q, want insecure selected", p.name, err, snap.String("ssh.host-key-policy"))
				}
				continue
			}
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != "config_lock_violation" || ce.Key != "ssh.host-key-policy" {
				t.Errorf("locked, %s: err=%v, want config_lock_violation on ssh.host-key-policy", p.name, err)
				continue
			}
			if !strings.Contains(ce.Message, `"ssh.host-key-policy"`) || !strings.Contains(ce.Message, global) {
				t.Errorf("locked, %s: message %q does not name the lock and its declaration", p.name, ce.Message)
			}
			t.Logf("locked, %s: %v", p.name, err)
			// The lock holds without the offending layer.
			base := Options{HomeDir: t.TempDir(), Environment: []string{}}
			if snap, err := Load(base); err != nil || snap.String("ssh.host-key-policy") != "secure" || snap.Values["ssh.host-key-policy"].Lock == nil {
				t.Errorf("locked: effective value %q lock=%v err=%v, want secure under the lock", snap.String("ssh.host-key-policy"), snap.Values["ssh.host-key-policy"].Lock, err)
			}
		}
	}
	// Only the global graph may declare locks.
	if err := os.WriteFile(global, []byte(unlocked), 0o600); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(dir, "explicit.toml")
	if err := os.WriteFile(explicit, []byte(locked), 0o600); err != nil {
		t.Fatal(err)
	}
	var ce *Error
	if _, err := Load(Options{HomeDir: t.TempDir(), Environment: []string{}, ExplicitRoots: []string{explicit}}); !errors.As(err, &ce) || ce.Code != "config_lock_authority_violation" {
		t.Errorf("explicit root declaring a lock: %v, want config_lock_authority_violation", err)
	}
}

// TestPersistCommandLockRefusesNof: `cmd --nof` is a flag-origin value of
// output.persist-command, so a site that
// locks the setting true refuses the option by the lock alone, and without
// the lock the option selects false. `cmd --of[=PATH]` is the mirror
// (section 8.5): persist-command true and, with PATH, output.root; a lock on
// either key refuses it, and without one the values are selected.
func TestPersistCommandLockRefusesNof(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	saved := globalRoots
	globalRoots = []string{global}
	t.Cleanup(func() { globalRoots = saved })
	for _, tc := range []struct {
		name     string
		flags    map[string]any
		body     string
		wantCode string
		want     func(Snapshot) bool
	}{
		{"nof locked", map[string]any{"output.persist-command": false},
			"[config-lock]\n\"output.persist-command\" = true\n[output]\npersist-command = true\n", "config_lock_violation", nil},
		{"nof free", map[string]any{"output.persist-command": false},
			"[output]\npersist-command = true\n", "", func(s Snapshot) bool { return !s.Bool("output.persist-command") }},
		{"of locked", map[string]any{"output.persist-command": true},
			"[config-lock]\n\"output.persist-command\" = true\n[output]\npersist-command = false\n", "config_lock_violation", nil},
		{"of free", map[string]any{"output.persist-command": true},
			"[output]\npersist-command = false\n", "", func(s Snapshot) bool { return s.Bool("output.persist-command") }},
		{"of path locked", map[string]any{"output.persist-command": true, "output.root": "/x/out"},
			"[config-lock]\n\"output.root\" = true\n[output]\nroot = \"/site/jobs\"\n", "config_lock_violation", nil},
		{"of path free", map[string]any{"output.persist-command": true, "output.root": "/x/out"},
			"[output]\nroot = \"/site/jobs\"\n", "", func(s Snapshot) bool { return s.String("output.root") == "/x/out" }},
	} {
		if err := os.WriteFile(global, []byte(tc.body), 0o600); err != nil {
			t.Fatal(err)
		}
		snap, err := Load(Options{FlagValues: tc.flags, HomeDir: dir, Environment: []string{}})
		code := ""
		var ce *Error
		if errors.As(err, &ce) {
			code = ce.Code
		}
		if code != tc.wantCode || (tc.wantCode == "" && err != nil) {
			t.Fatalf("%s: %v, want %q", tc.name, err, tc.wantCode)
		}
		if err == nil && !tc.want(snap) {
			t.Fatalf("%s: the option's value was not selected", tc.name)
		}
	}
}
