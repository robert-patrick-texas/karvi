package configload

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
)

// TestSSHAlgorithmsValidation: each rule of
// the global lists, the profiles, and the map refused at load with its code
// and key, and the valid forms accepted.
func TestSSHAlgorithmsValidation(t *testing.T) {
	load := func(t *testing.T, body string, env ...string) (Snapshot, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: append([]string{}, env...), ExplicitRoots: []string{path}})
	}
	const profile = "[ssh-algorithms-profile.old]\nciphers-append = [\"aes128-ctr\"]\n"
	for _, tc := range []struct {
		name, body, code, key string
		env                   []string
	}{
		{name: "unknown cipher", body: "[ssh-algorithms]\nciphers = [\"aes256-gcm\"]\n", code: "config_ssh_algorithm_unknown", key: "ssh-algorithms.ciphers"},
		{name: "a cipher in kex", body: "[ssh-algorithms]\nkex = [\"aes256-ctr\"]\n", code: "config_ssh_algorithm_unknown", key: "ssh-algorithms.kex"},
		{name: "3des globally", body: "[ssh-algorithms]\nciphers = [\"aes256-ctr\", \"3des-cbc\"]\n", code: "config_ssh_algorithm_forbidden", key: "ssh-algorithms.ciphers"},
		{name: "ssh-dss", body: "[ssh-algorithms]\nhost-key = [\"ssh-dss\"]\n", code: "config_ssh_algorithm_forbidden", key: "ssh-algorithms.host-key"},
		{name: "aes128 globally", body: "[ssh-algorithms]\nciphers = [\"aes256-ctr\", \"aes128-ctr\"]\n", code: "config_ssh_algorithm_profile_only", key: "ssh-algorithms.ciphers"},
		{name: "empty list", body: "[ssh-algorithms]\nmacs = []\n", code: "config_ssh_algorithm_list_empty", key: "ssh-algorithms.macs"},
		{name: "duplicate", body: "[ssh-algorithms]\nmacs = [\"hmac-sha1\", \"hmac-sha1\"]\n", code: "config_ssh_algorithm_duplicate", key: "ssh-algorithms.macs"},
		{name: "append already global", body: "[ssh-algorithms-profile.old]\nciphers-append = [\"aes256-ctr\"]\n", code: "config_ssh_algorithm_duplicate", key: "ssh-algorithms-profile.old.ciphers-append"},
		{name: "profile forbidden", body: "[ssh-algorithms-profile.old]\nciphers-append = [\"3des-cbc\"]\n", code: "config_ssh_algorithm_forbidden", key: "ssh-algorithms-profile.old.ciphers-append"},
		{name: "both forms", body: "[ssh-algorithms-profile.old]\nkex = [\"curve25519-sha256\"]\nkex-append = [\"diffie-hellman-group1-sha1\"]\n", code: "config_ssh_algorithms_profile_list_conflict", key: "ssh-algorithms-profile.old.kex"},
		{name: "profile not a list", body: "[ssh-algorithms-profile.old]\nkex = \"curve25519-sha256\"\n", code: "config_type_error", key: "ssh-algorithms-profile.old.kex"},
		{name: "map without profile", body: profile + "[[ssh-algorithms-map]]\nsite = \"lab\"\n", code: "config_ssh_algorithms_map_profile_missing", key: "ssh-algorithms-map.0.profile"},
		{name: "map unknown profile", body: profile + "[[ssh-algorithms-map]]\nprofile = \"new\"\nsite = \"lab\"\n", code: "config_ssh_algorithms_map_profile_unknown", key: "ssh-algorithms-map.0.profile"},
		{name: "map catch-all", body: profile + "[[ssh-algorithms-map]]\nprofile = \"old\"\nname = \"*\"\n", code: "config_ssh_algorithms_map_catch_all", key: "ssh-algorithms-map.0.name"},
		{name: "map no match key", body: profile + "[[ssh-algorithms-map]]\nprofile = \"old\"\n", code: "config_match_rule_empty", key: "ssh-algorithms-map.0"},
		{name: "map bad cidr", body: profile + "[[ssh-algorithms-map]]\nprofile = \"old\"\naddress-cidr = \"10.0.0.0/33\"\n", code: "config_match_rule_cidr_invalid", key: "ssh-algorithms-map.0"},
		{name: "legacy-hosts", body: "[ssh]\nlegacy-hosts = [\"old-ios-*\"]\n", code: "config_ssh_legacy_hosts_removed", key: "ssh.legacy-hosts"},
		{name: "ancient-hosts", body: "[ssh]\nancient-hosts = []\n", code: "config_ssh_legacy_hosts_removed", key: "ssh.ancient-hosts"},
		{name: "global source unknown", body: "[ssh-algorithms]\nsource = \"none\"\n", code: "config_enum_value_invalid", key: "ssh-algorithms.source"},
		{name: "profile source unknown", body: "[ssh-algorithms-profile.old]\nsource = \"system\"\n", code: "config_enum_value_invalid", key: "ssh-algorithms-profile.old.source"},
		{name: "profile source not a string", body: "[ssh-algorithms-profile.old]\nsource = 1\n", code: "config_type_error", key: "ssh-algorithms-profile.old.source"},
		{name: "lists under the profile's transport source", body: "[ssh-algorithms-profile.old]\nsource = \"transport\"\nkex-append = [\"diffie-hellman-group1-sha1\"]\n", code: "config_ssh_algorithms_profile_lists_unread", key: "ssh-algorithms-profile.old.kex-append"},
		{name: "lists under the global transport source", body: "[ssh-algorithms]\nsource = \"transport\"\n" + profile, code: "config_ssh_algorithms_profile_lists_unread", key: "ssh-algorithms-profile.old.ciphers-append"},
		{name: "legacy-hosts environment", body: "", env: []string{`KARVI__SSH__LEGACY_HOSTS=["x"]`}, code: "config_ssh_legacy_hosts_removed", key: "ssh.legacy-hosts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.body, tc.env...)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != tc.key {
				t.Fatalf("err=%v, want %s at %q", err, tc.code, tc.key)
			}
		})
	}
	snap, err := load(t, "[ssh-algorithms]\nciphers = [\"aes256-gcm@openssh.com\", \"aes256-ctr\"]\n"+profile+
		"[ssh-algorithms-profile.ancient]\nkex-append = [\"diffie-hellman-group1-sha1\", \"diffie-hellman-group-exchange-sha1\"]\nciphers = [\"aes128-cbc\"]\nmacs-append = [\"umac-128@openssh.com\"]\n"+
		"[[ssh-algorithms-map]]\nprofile = \"old\"\ndevice-group = \"legacy\"\n[[ssh-algorithms-map]]\nprofile = \"ancient\"\naddress-cidr = \"192.0.2.0/24\"\n")
	if err != nil {
		t.Fatalf("valid configuration refused: %v", err)
	}
	want := sshalgorithms.Defaults()
	want[sshalgorithms.Ciphers] = []string{"aes256-gcm@openssh.com", "aes256-ctr"}
	if got := snap.SSHAlgorithms(); !reflect.DeepEqual(got, want) {
		t.Fatalf("global %v", got)
	}
	defaults, err := load(t, "")
	if err != nil || !reflect.DeepEqual(defaults.SSHAlgorithms(), sshalgorithms.Defaults()) || defaults.String("ssh-algorithms.source") != sshalgorithms.SourceKarvi {
		t.Fatalf("defaults %v source %q %v", defaults.SSHAlgorithms(), defaults.String("ssh-algorithms.source"), err)
	}

	// Under the transport's source: the global lists stay valid, a profile
	// that says source karvi keeps its lists, and a profile may set the
	// host-key list, or its source alone.
	snap, err = load(t, "[ssh-algorithms]\nsource = \"transport\"\nciphers = [\"aes256-gcm@openssh.com\"]\n"+
		"[ssh-algorithms-profile.routers]\nsource = \"auto\"\nkex-append = [\"diffie-hellman-group1-sha1\"]\n"+
		"[ssh-algorithms-profile.servers]\nhost-key = [\"ssh-ed25519\"]\n"+
		"[ssh-algorithms-profile.bare]\nsource = \"transport\"\n")
	if err != nil {
		t.Fatalf("valid configuration under the transport's source refused: %v", err)
	}
	if got := snap.String("ssh-algorithms-profile.routers.source"); got != sshalgorithms.SourceKarvi {
		t.Fatalf("the profile's source auto stored as %q", got)
	}
}

// TestAliasStoredAsItsValue: an alias is stored as the value it stands for,
// from a file, the environment, and --set alike, so config show and the
// digest carry one word for one meaning.
func TestAliasStoredAsItsValue(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	load := func(t *testing.T, body string, env, sets []string) Snapshot {
		t.Helper()
		path := filepath.Join(dir, "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		snap, err := Load(Options{HomeDir: home, SkipAuto: true, Environment: env, Sets: sets, ExplicitRoots: []string{path}})
		if err != nil {
			t.Fatal(err)
		}
		return snap
	}
	for _, tc := range []struct{ key, alias, value, table, variable string }{
		{"dispatch.order", "name", "sorted", "dispatch", "KARVI__DISPATCH__ORDER"},
		{"ssh-algorithms.source", "auto", "karvi", "ssh-algorithms", "KARVI__SSH_ALGORITHMS__SOURCE"},
	} {
		field := tc.key[len(tc.table)+1:]
		want := load(t, fmt.Sprintf("[%s]\n%s = %q\n", tc.table, field, tc.value), nil, nil)
		for name, got := range map[string]Snapshot{
			"file":        load(t, fmt.Sprintf("[%s]\n%s = %q\n", tc.table, field, tc.alias), nil, nil),
			"environment": load(t, "", []string{tc.variable + "=" + tc.alias}, nil),
			"--set":       load(t, "", nil, []string{fmt.Sprintf("%s=%q", tc.key, tc.alias)}),
		} {
			if got.String(tc.key) != tc.value || got.Digest != want.Digest {
				t.Errorf("%s %s=%s: stored %q digest %s, want %q digest %s", name, tc.key, tc.alias, got.String(tc.key), got.Digest, tc.value, want.Digest)
			}
		}
	}
}
