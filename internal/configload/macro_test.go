package configload

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestLockedValueTakesNoMacroFromBelowTheLock: a locked key's value that
// takes a macro is refused as config_lock_violation when the macro's winning
// definition comes from below the global graph (--set, an explicit root),
// directly or through another macro, naming the macro's key, where it was
// written, the locked key, and the lock; a macro the global graph alone
// defines, %operator%, and an unlocked key taking a macro from below all
// load.
func TestLockedValueTakesNoMacroFromBelowTheLock(t *testing.T) {
	dir := t.TempDir()
	global := filepath.Join(dir, "global.toml")
	saved := globalRoots
	globalRoots = []string{global}
	t.Cleanup(func() { globalRoots = saved })
	body := "[config-lock]\n\"ssh.host-key-policy\" = true\n\"display.run.header\" = true\n" +
		"[macros]\npolicy = \"%strict%\"\nstrict = \"secure\"\n" +
		"[ssh]\nhost-key-policy = \"%policy%\"\n" +
		"[display.run]\nheader = \"! %operator%\"\n" +
		"[display.command]\nheader = \"! %policy%\"\n"
	if err := os.WriteFile(global, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(dir, "explicit.toml")
	if err := os.WriteFile(explicit, []byte("[macros]\nstrict = \"insecure\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	load := func(opts Options) (Snapshot, error) {
		opts.HomeDir, opts.Operator, opts.Environment = dir, "jsmith", []string{}
		return Load(opts)
	}

	snap, err := load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("ssh.host-key-policy"); got != "secure" {
		t.Fatalf("the global's own macros: %q", got)
	}
	if got := snap.Values["ssh.host-key-policy"].MacroTrace; !reflect.DeepEqual(got, []string{"policy", "strict"}) {
		t.Errorf("trace %v, want the macro and the one it takes", got)
	}
	if got := snap.String("display.run.header"); got != "! jsmith" {
		t.Fatalf("%%operator%% in a locked key: %q", got)
	}
	if _, err := load(Options{Sets: []string{`macros.region="nyc"`, `display.login.header="! %region%"`}}); err != nil {
		t.Fatalf("an unlocked key taking a macro from --set: %v", err)
	}

	for _, tc := range []struct {
		name  string
		opts  Options
		macro string
		at    string
	}{
		{"--set the macro", Options{Sets: []string{`macros.policy="insecure"`}}, "policy", "--set[1]"},
		{"--set the macro it takes", Options{Sets: []string{`macros.strict="insecure"`}}, "strict", "--set[1]"},
		{"an explicit root", Options{ExplicitRoots: []string{explicit}}, "strict", explicit + ":2"},
	} {
		// The keys and their macros are visited in no fixed order; each load
		// gives the same refusal.
		for range 10 {
			_, err := load(tc.opts)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != "config_lock_violation" || ce.Key != "macros."+tc.macro || ce.Source.String() != tc.at {
				t.Errorf("%s: err=%v, want config_lock_violation for macros.%s at %s", tc.name, err, tc.macro, tc.at)
				break
			}
			for _, want := range []string{"ssh.host-key-policy", "%" + tc.macro + "%", `"ssh.host-key-policy"`, global + ":2"} {
				if !strings.Contains(ce.Message, want) {
					t.Fatalf("%s: message %q lacks %q", tc.name, ce.Message, want)
				}
			}
		}
	}
}

// TestMacroValueTakesTheCanonicalWord: a value that arrives through a macro
// is stored as the word its alias stands for, as a value written out is:
// dispatch.order's name is sorted, an algorithm profile's source auto is
// karvi.
func TestMacroValueTakesTheCanonicalWord(t *testing.T) {
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{
		`macros.ord="name"`, `dispatch.order="%ord%"`,
		`macros.src="auto"`, `ssh-algorithms-profile.old.source="%src%"`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.String("dispatch.order"); got != "sorted" {
		t.Errorf("dispatch.order %q, want sorted", got)
	}
	if got := snap.NamedTables("ssh-algorithms-profile")["old"]["source"]; got != "karvi" {
		t.Errorf("ssh-algorithms-profile.old.source %v, want karvi", got)
	}
}
