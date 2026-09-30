package configload

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestCrunFiltersValidation covers the crun-filters rule at the
// loader: a crun-filters array loads and is read by name, an entry that is
// not a string is a type error, and a pattern that does not compile is
// config_platform_crun_filter_invalid naming the table, the entry, and the
// reason, before anything else runs.
func TestCrunFiltersValidation(t *testing.T) {
	snap, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.crun-filters=['^Building configuration\.\.\.$', ' uptime is ']`}})
	if err != nil {
		t.Fatalf("a valid list: %v", err)
	}
	list, ok := snap.NamedTables("platform")["c9300"]["crun-filters"].([]any)
	if !ok || len(list) != 2 || list[0] != `^Building configuration\.\.\.$` {
		t.Fatalf("the list as loaded: %v", snap.NamedTables("platform")["c9300"])
	}
	if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.crun-filters=["ok", 7]`}}); errorcodes.Of(err) != "config_type_error" {
		t.Fatalf("a non-string entry: %v", err)
	}
	_, err = Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.crun-filters=["^ok$", "("]`}})
	if errorcodes.Of(err) != "config_platform_crun_filter_invalid" || !strings.Contains(err.Error(), "platform.c9300.crun-filters") || !strings.Contains(err.Error(), "pattern 2") {
		t.Fatalf("a pattern that does not compile: %v", err)
	}
	if _, err := Load(Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.cisco_iosxe.crun-filters=[]`}}); err != nil {
		t.Fatalf("an empty array turns the filter off: %v", err)
	}
}
