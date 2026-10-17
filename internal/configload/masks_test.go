package configload

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestMasksValidation covers the masks rule at the loader: a masks array
// loads and is read by name, an entry that is not a string is a type
// error, and a pattern that does not compile or holds no capturing group is
// config_platform_mask_invalid naming the table, the entry, and the reason;
// crun.mask is false by default.
func TestMasksValidation(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.masks=['(?i)^\s*snmp-s\S*\s+comm\S*\s+(\S+)']`}})
	if err != nil {
		t.Fatalf("a valid list: %v", err)
	}
	list, ok := snap.NamedTables("platform")["c9300"]["masks"].([]any)
	if !ok || len(list) != 1 || list[0] != `(?i)^\s*snmp-s\S*\s+comm\S*\s+(\S+)` {
		t.Fatalf("the list as loaded: %v", snap.NamedTables("platform")["c9300"])
	}
	if snap.Bool("crun.mask") {
		t.Fatal("crun.mask is true by default")
	}
	if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.masks=["(ok)", 7]`}}); errorcodes.Of(err) != "config_type_error" {
		t.Fatalf("a non-string entry: %v", err)
	}
	for _, c := range []struct{ list, reason string }{
		{`["key (\\S+)", "("]`, `pattern 2 "(" does not compile: error parsing regexp`},
		{`["key (\\S+)", "^enable secret \\S+"]`, `pattern 2 "^enable secret \\S+" holds no capturing group`},
	} {
		_, err = Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.masks=` + c.list}})
		if errorcodes.Of(err) != "config_platform_mask_invalid" || !strings.Contains(err.Error(), "platform.c9300.masks") || !strings.Contains(err.Error(), c.reason) {
			t.Fatalf("%s: %v", c.list, err)
		}
	}
	if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.cisco_iosxe.masks=[]`, `crun.mask=true`}}); err != nil {
		t.Fatalf("an empty array turns the mask off: %v", err)
	}
}
