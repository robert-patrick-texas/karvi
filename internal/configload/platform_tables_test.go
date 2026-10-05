package configload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlatformTableValidation: each table rule refused at load with its
// code and key, in the
// order names, driver, fields, the resolved definition (a fault of the
// table as a whole is reported at its first key); and the accepted
// forms (an alias with paging-commands and a level of its base, a
// built-in's table naming its own driver in another spelling, a name with
// a space, a table written in upper case).
func TestPlatformTableValidation(t *testing.T) {
	load := func(t *testing.T, body string) (Snapshot, error) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "karvi.toml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, ExplicitRoots: []string{path}})
	}
	for _, tc := range []struct {
		name, body, code, key, text string
	}{
		{"glob in the name", "[platform.\"c93*\"]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.c93*.driver", "glob"},
		{"question mark in the name", "[platform.\"c930?\"]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.c930?.driver", "glob"},
		{"class in the name", "[platform.\"c[9]300\"]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.c[9]300.driver", "glob"},
		{"negation in the name", "[platform.\"!c9300\"]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.!c9300.driver", "\"!\""},
		{"blank name", "[platform.\"  \"]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.  .driver", "blank"},
		{"two spellings of one name", "[platform.C9300]\ndriver = \"cisco_iosxe\"\n[platform.c9300]\ndriver = \"cisco_iosxe\"\n", "config_platform_name_invalid", "platform.c9300.driver", "one platform \"c9300\""},
		{"no driver", "[platform.c9300]\nssh-port = 2222\n", "config_platform_driver_unknown", "platform.c9300.driver", "driver is required"},
		{"driver not a built-in", "[platform.c9300]\ndriver = \"ios\"\n", "config_platform_driver_unknown", "platform.c9300.driver", "\"ios\" is not a built-in"},
		{"an alias of an alias", "[platform.c9300]\ndriver = \"cisco_iosxe\"\n[platform.c9300x]\ndriver = \"c9300\"\n", "config_platform_driver_unknown", "platform.c9300x.driver", "\"c9300\" is not a built-in"},
		{"a built-in's table with another driver", "[platform.cisco_iosxe]\ndriver = \"generic\"\n", "config_platform_driver_unknown", "platform.cisco_iosxe.driver", "driver is absent or \"cisco_iosxe\""},
		{"ssh-port out of range", "[platform.c9300]\ndriver = \"cisco_iosxe\"\nssh-port = 70000\n", "config_platform_ssh_port_out_of_range", "platform.c9300.ssh-port", "1..65535"},
		{"session-cap out of range", "[platform.c9300]\ndriver = \"cisco_iosxe\"\nsession-cap = 0\n", "config_platform_session_cap_out_of_range", "platform.c9300.session-cap", "1..32"},
		{"channel neither word", "[platform.c9300]\ndriver = \"cisco_iosxe\"\nchannel = \"pty\"\n", "config_platform_channel_invalid", "platform.c9300.channel", "\"shell\" or \"exec\""},
		{"channel not a string", "[platform.c9300]\ndriver = \"cisco_iosxe\"\nchannel = true\n", "config_platform_channel_invalid", "platform.c9300.channel", "\"shell\" or \"exec\""},
		{"fallback an unknown word", "[platform.linux]\nfallback = [\"agent\"]\n", "config_platform_fallback_invalid", "platform.linux.fallback", "\"agent\" is not"},
		{"fallback a word twice", "[platform.linux]\nfallback = [\"keys\", \"keys\"]\n", "config_platform_fallback_invalid", "platform.linux.fallback", "listed twice"},
		{"fallback not an array", "[platform.linux]\nfallback = \"keys\"\n", "config_type_error", "platform.linux.fallback", "array"},
		{"control-master is removed", "[platform.c9300]\ndriver = \"cisco_iosxe\"\ncontrol-master = true\n", "config_unknown_key", "platform.c9300.control-master", "unknown configuration key"},
		{"paging-commands not an array", "[platform.c9300]\ndriver = \"cisco_iosxe\"\npaging-commands = \"terminal length 0\"\n", "config_type_error", "platform.c9300.paging-commands", "array"},
		{"crun-commands not an array of strings", "[platform.c9300]\ndriver = \"cisco_iosxe\"\ncrun-commands = [\"show running-config\", 1]\n", "config_type_error", "platform.c9300.crun-commands", "array"},
		{"a privileged level the base lacks", "[platform.c9300]\ndriver = \"cisco_iosxe\"\nprivileged-level = \"admin\"\n", "platform_privilege_level_unknown", "platform.c9300.privileged-level", "exec, privilege-exec, configuration, tclsh"},
		{"a privileged level on a base without levels", "[platform.lab]\ndriver = \"generic\"\nprivileged-level = \"exec\"\n", "platform_privilege_level_unknown", "platform.lab.privileged-level", "no prompt levels"},
		{"a privileged level on a built-in without levels", "[platform.linux]\nprivileged-level = \"shell\"\n", "platform_privilege_level_unknown", "platform.linux.privileged-level", "no prompt levels"},
		{"default-transport invalid", "[platform.c9300]\ndriver = \"cisco_iosxe\"\ndefault-transport = \"Fast SSH\"\n", "platform_default_transport_invalid", "platform.c9300.default-transport", "selector"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(t, tc.body)
			var ce *Error
			if !errors.As(err, &ce) || ce.Code != tc.code || ce.Key != tc.key {
				t.Fatalf("err=%v, want %s at %s", err, tc.code, tc.key)
			}
			if !strings.Contains(err.Error(), tc.text) {
				t.Fatalf("message %q lacks %q", err.Error(), tc.text)
			}
			if ce.Source.Path == "" {
				t.Fatalf("no source: %+v", ce)
			}
		})
	}
	for _, body := range []string{
		"[platform.c9300]\ndriver = \"cisco_iosxe\"\npaging-commands = [\"terminal length 0\"]\ncrun-commands = [\"show running-config\", \"show inventory\"]\nprivileged-level = \"exec\"\nrequires-enable = false\nssh-port = 2222\ntelnet-port = 2323\nsession-cap = 4\nchannel = \"exec\"\n",
		"[platform.cisco_iosxe]\ndriver = \" CISCO_IOSXE \"\nssh-port = 2222\n",
		"[platform.\"lab core\"]\ndriver = \"juniper_junos\"\n",
		"[platform.C9300]\ndriver = \"Cisco_IOSXE\"\n",
		"[platform.generic]\npaging-commands = []\n",
		"[platform.linux]\nrequires-enable = true\n",
		"[platform.eos-exec]\ndriver = \"arista_eos\"\nchannel = \"exec\"\n",
		"[platform.linux]\nchannel = \"shell\"\n",
		"[platform.bastion]\ndriver = \"linux_shell\"\n",
		"[platform.linux]\nfallback = [\"netvars\", \"keys\"]\n",
		"[platform.c9300]\ndriver = \"cisco_iosxe\"\nfallback = []\n",
	} {
		snap, err := load(t, body)
		if err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if len(snap.NamedTables("platform")) != 1 {
			t.Fatalf("%s: tables %v", body, snap.NamedTables("platform"))
		}
	}
	// The list of built-ins in the driver message is the documented order
	// (an empty table has no keys and so is not a table at all in TOML).
	_, err := load(t, "[platform.c9300]\nssh-port = 22\n")
	if err == nil || !strings.Contains(err.Error(), "generic, cisco_iosxe, cisco_iosxr, cisco_nxos, juniper_junos, arista_eos, linux, linux_shell") {
		t.Fatalf("%v", err)
	}
}

// TestSSHIdentities: each entry an absolute path or one under the home,
// never a bare name or a relative path; the default is the three keys.
func TestSSHIdentities(t *testing.T) {
	snap, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(snap.Strings("ssh.identities"), " "); got != "~/.ssh/id_ed25519 ~/.ssh/id_ecdsa ~/.ssh/id_rsa" {
		t.Fatalf("default: %s", got)
	}
	if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{`ssh.identities=["/etc/karvi/keys/ops", "~/.ssh/id_ops"]`}}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"id_ed25519", ".ssh/id_rsa", "~id_rsa"} {
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{`ssh.identities=["` + bad + `"]`}})
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != "config_ssh_identities_invalid" || !strings.Contains(err.Error(), bad) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
	if _, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{"ssh.pubkey-authentication=true"}}); err == nil || !strings.Contains(err.Error(), "config_unknown_key") {
		t.Fatalf("the removed key: %v", err)
	}
}
