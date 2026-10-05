package platform

import (
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func TestResolve(t *testing.T) {
	iosxe, _ := Builtin("cisco_iosxe")
	generic, _ := Builtin("generic")
	tables := map[string]map[string]any{
		"c9300":       {"driver": " CISCO_IOSXE ", "ssh-port": int64(2222), "session-cap": int64(4)},
		"edge":        {"driver": "c9300"},
		"lab":         {"driver": "custom", "requires-enable": true},
		"cisco_iosxe": {"driver": "cisco_nxos", "telnet-port": int64(2323)},
		"strict":      {"driver": "cisco_iosxe", "requires-enable": true, "paging-commands": []any{"terminal length 0"}, "legacy-class": "none", "privileged-level": "exec"},
		"collector":   {"driver": "cisco_iosxe", "crun-commands": []any{"show running-config", "show inventory"}},
		"silent":      {"driver": "cisco_iosxe", "crun-commands": []any{}},
		"Lab-Nxos":    {"driver": "cisco_nxos"},
	}

	got := Resolve("c9300", tables)
	want := iosxe
	want.Name, want.Base, want.SSHPort, want.SessionCap = "c9300", "cisco_iosxe", 2222, 4
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("alias:\n got %+v\nwant %+v", got, want)
	}
	if got.RequiresEnable {
		t.Fatal("an alias of a built-in requires enable only when its table says so")
	}

	for _, name := range []string{"edge", "unknown"} {
		got := Resolve(name, tables)
		if got.Base != "generic" || got.Name != name || len(got.PrivilegeLevels) != 0 || got.PromptPattern != generic.PromptPattern {
			t.Fatalf("%s: %+v", name, got)
		}
	}
	if got := Resolve("edge", tables); got.Driver != "c9300" {
		t.Fatalf("an alias of an alias keeps the label: %q", got.Driver)
	}
	if got := Resolve("lab", tables); got.Driver != "custom" || !got.RequiresEnable || got.Base != "generic" {
		t.Fatalf("lab: %+v", got)
	}

	got = Resolve("cisco_iosxe", tables)
	if got.Base != "cisco_iosxe" || got.Driver != "cisco_nxos" || got.TelnetPort != 2323 || !reflect.DeepEqual(got.PrivilegeLevels, iosxe.PrivilegeLevels) {
		t.Fatalf("a built-in's table: %+v", got)
	}

	got = Resolve("strict", tables)
	if !got.RequiresEnable || !reflect.DeepEqual(got.PagingCommands, []string{"terminal length 0"}) || got.LegacyClass != "none" || got.PrivilegedLevel != "exec" {
		t.Fatalf("strict: %+v", got)
	}

	// Names normalise on both sides of the lookup: a table written in upper
	// case applies to the lowercase
	// row, and a name with spaces and capitals resolves and is recorded
	// normalised.
	for _, name := range []string{"lab-nxos", " LAB-NXOS ", "Lab-Nxos"} {
		got := Resolve(name, tables)
		if got.Name != "lab-nxos" || got.Base != "cisco_nxos" {
			t.Fatalf("%q: %+v", name, got)
		}
	}
	if got := Resolve(" C9300 ", tables); got.Name != "c9300" || got.Base != "cisco_iosxe" || got.SSHPort != 2222 {
		t.Fatalf("spaces and case: %+v", got)
	}
	if got := Resolve("c9300", nil); got.Base != "generic" {
		t.Fatalf("no tables: %+v", got)
	}
	if got := Resolve("cisco_nxos", nil); got.Base != "cisco_nxos" || got.RequiresEnable {
		t.Fatalf("built-in: %+v", got)
	}
	for _, d := range Builtins() {
		if d.RequiresEnable {
			t.Fatalf("built-in %s requires enable", d.Name)
		}
	}
}

func TestKnownAndKnownNames(t *testing.T) {
	tables := map[string]map[string]any{
		"C9300": {"driver": "cisco_iosxe"},
		"edge":  {"driver": "c9300"},
		"lab":   {},
	}
	builtins := []string{"generic", "cisco_iosxe", "cisco_iosxr", "cisco_nxos", "juniper_junos", "arista_eos", "linux", "linux_shell"}
	if got := KnownNames(nil); !reflect.DeepEqual(got, builtins) {
		t.Fatalf("no tables: %v", got)
	}
	// The built-ins in their documented order, then the table names sorted
	// and normalised; a table is known whatever its driver says (its driver
	// is validation's concern).
	want := append(append([]string{}, builtins...), "c9300", "edge", "lab")
	if got := KnownNames(tables); !reflect.DeepEqual(got, want) {
		t.Fatalf("with tables: %v", got)
	}
	for _, name := range []string{"generic", "CISCO_IOSXE", " linux ", "c9300", "C9300", "edge", "lab"} {
		if !Known(name, tables) {
			t.Fatalf("%q should be known", name)
		}
	}
	for _, name := range []string{"", "cisco_iosx", "*", "c93*", "!c9300", "ios"} {
		if Known(name, tables) {
			t.Fatalf("%q should not be known", name)
		}
	}
	// A built-in's own table adds no name.
	if got := KnownNames(map[string]map[string]any{"cisco_iosxe": {"ssh-port": int64(2222)}}); !reflect.DeepEqual(got, builtins) {
		t.Fatalf("a built-in's table: %v", got)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{" Cisco_IOSXE ": "cisco_iosxe", "c9300": "c9300", "": "", "  ": "", "\tLinux": "linux"} {
		if got := Normalize(in); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestValidatePrivilegedLevel: a privileged level
// names a level of the base definition; a base without levels has none.
func TestValidatePrivilegedLevel(t *testing.T) {
	tables := map[string]map[string]any{
		"ok":      {"driver": "cisco_iosxe", "privileged-level": "exec"},
		"admin":   {"driver": "cisco_iosxe", "privileged-level": "admin"},
		"nolevel": {"driver": "generic", "privileged-level": "exec"},
	}
	for name, wantCode := range map[string]string{"ok": "", "admin": "platform_privilege_level_unknown", "nolevel": "platform_privilege_level_unknown", "cisco_iosxe": "", "generic": "", "linux": ""} {
		def := Resolve(name, tables)
		err := def.Validate()
		got := ""
		if err != nil {
			got = errorcodes.Of(err)
		}
		if got != wantCode {
			t.Fatalf("%s: code %q (%v), want %q", name, got, err, wantCode)
		}
	}
	def := Resolve("admin", tables)
	if err := def.Validate(); err == nil || !strings.Contains(err.Error(), "exec, privilege-exec, configuration, tclsh") {
		t.Fatalf("the message names the base's levels: %v", err)
	}
	// Validate normalises the name it is given.
	def = Definition{Name: " Lab "}
	if err := def.Validate(); err != nil || def.Name != "lab" {
		t.Fatalf("name %q, err %v", def.Name, err)
	}
}

// TestCrunCommands: every built-in with
// a configuration ships a collection list, the configuration first; an
// alias inherits it; a table's crun-commands replaces it whole, an empty
// array included; generic and linux have none.
func TestCrunCommands(t *testing.T) {
	for _, name := range []string{"cisco_iosxe", "cisco_iosxr", "cisco_nxos", "juniper_junos", "arista_eos"} {
		def, _ := Builtin(name)
		if len(def.CrunCommands) < 2 || !strings.HasPrefix(def.CrunCommands[0], "show ") || !strings.Contains(def.CrunCommands[0], "config") {
			t.Fatalf("%s: crun-commands %q", name, def.CrunCommands)
		}
	}
	for _, name := range []string{"generic", "linux"} {
		if def, _ := Builtin(name); len(def.CrunCommands) != 0 {
			t.Fatalf("%s has a collection list: %q", name, def.CrunCommands)
		}
	}
	tables := map[string]map[string]any{
		"c9300":     {"driver": "cisco_iosxe"},
		"collector": {"driver": "cisco_iosxe", "crun-commands": []any{"show running-config", "show inventory"}},
		"silent":    {"driver": "cisco_iosxe", "crun-commands": []any{}},
	}
	iosxe, _ := Builtin("cisco_iosxe")
	if got := Resolve("c9300", tables).CrunCommands; !reflect.DeepEqual(got, iosxe.CrunCommands) {
		t.Fatalf("an alias inherits the list: %q", got)
	}
	if got := Resolve("collector", tables).CrunCommands; !reflect.DeepEqual(got, []string{"show running-config", "show inventory"}) {
		t.Fatalf("a table replaces the list: %q", got)
	}
	if got := Resolve("silent", tables).CrunCommands; got == nil || len(got) != 0 {
		t.Fatalf("an empty array clears the list: %#v", got)
	}
}

// linux_shell is linux's definition on the shell channel with linux as its
// base, and an alias of it is based on linux too; a table sets channel on
// any driver, and a definition that leaves it unset validates as shell.
func TestChannelAndLinuxShell(t *testing.T) {
	tables := map[string]map[string]any{
		"bastion":  {"driver": "linux_shell"},
		"eos-exec": {"driver": "arista_eos", "channel": "exec"},
		"linux":    {"channel": "exec"},
	}
	for _, tc := range []struct{ name, base, driver, channel string }{
		{"linux_shell", "linux", "linux_shell", ChannelShell},
		{"bastion", "linux", "linux_shell", ChannelShell},
		{"eos-exec", "arista_eos", "arista_eos", ChannelExec},
		{"linux", "linux", "linux", ChannelExec},
		{"cisco_iosxe", "cisco_iosxe", "cisco_iosxe", ChannelShell},
	} {
		def := Resolve(tc.name, tables)
		if def.Base != tc.base || def.Driver != tc.driver || def.Channel != tc.channel {
			t.Errorf("%s: base %q driver %q channel %q", tc.name, def.Base, def.Driver, def.Channel)
		}
	}
	for _, d := range Builtins() {
		if d.Channel != ChannelShell || d.Base == "" {
			t.Errorf("built-in %s: channel %q base %q", d.Name, d.Channel, d.Base)
		}
	}
	unset := Definition{Name: "lab"}
	if err := unset.Validate(); err != nil || unset.Channel != ChannelShell {
		t.Fatalf("unset channel: %q %v", unset.Channel, err)
	}
	bad := Definition{Name: "lab", Channel: "pty"}
	if err := bad.Validate(); errorcodes.Of(err) != "config_platform_channel_invalid" {
		t.Fatalf("channel pty: %v", err)
	}
}
