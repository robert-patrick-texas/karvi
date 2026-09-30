package app

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// TestLoginPlatformResolvesAliases: login uses the one resolver, so an
// alias is its
// built-in, and a name the configuration does not know is refused with
// platform_unknown, never generic under the name (the backstop behind the
// planner's resolution).
func TestLoginPlatformResolvesAliases(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{`platform.c9300.driver="cisco_iosxe"`, `platform.c9300.session-cap=4`}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := platformFor(cfg, "c9300")
	if err != nil || d.Base != "cisco_iosxe" || d.SessionCap != 4 || d.PrivilegedLevel != "privilege-exec" || d.RequiresEnable {
		t.Fatalf("alias: %+v %v", d, err)
	}
	if _, err = platformFor(cfg, "Lab-Box"); errorcodes.Of(err) != "platform_unknown" || !strings.Contains(err.Error(), "known: generic, cisco_iosxe") {
		t.Fatalf("other name: %v", err)
	}
	if d, err = platformFor(cfg, " Generic "); err != nil || d.Name != "generic" || d.Base != "generic" {
		t.Fatalf("built-in in another spelling: %+v %v", d, err)
	}
}
