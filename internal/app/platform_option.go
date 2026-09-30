package app

import (
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
)

// CheckPlatformOption is the one check of --platform NAME in login, command,
// and run. It takes the loaded
// configuration and the option's value and returns the normalised name the
// first device (login, command) or every device of the set (run) takes: an
// empty value passes as empty; a value holding a glob
// character or beginning with "!" is refused (a name is required, never a
// pattern); any other value must be a known platform, a built-in or a
// configured [platform.NAME] table (platform.Known). The refusal is
// platform_option_unknown (usage, exit 4), naming the value and the known
// platforms in their known order, and it is made once, after the
// configuration loads and before the inventory is read, so an inventory fault
// never masks a bad option and no transcript or child is created for one. The
// parser stays unaware: it cannot see the tables, and one site with one
// message beats a parser refusal for glob characters beside an app refusal
// for unknown names. The recording wrapper calls this too, so it is exported.
func CheckPlatformOption(cfg configload.Snapshot, value string) (string, error) {
	name := platform.Normalize(value)
	if name == "" {
		return "", nil
	}
	tables := cfg.NamedTables("platform")
	known := strings.Join(platform.KnownNames(tables), ", ")
	if strings.HasPrefix(name, "!") || strings.ContainsAny(name, `*?[\`) {
		return "", errorcodes.Errorf("platform_option_unknown", "--platform %q: a platform name is required, not a pattern (--select-platform selects inventory devices by platform); known platforms: %s", value, known)
	}
	if !platform.Known(name, tables) {
		return "", errorcodes.Errorf("platform_option_unknown", "--platform %q is not a known platform; known platforms: %s", value, known)
	}
	return name, nil
}

// withPlatform is the one place --platform acts on a device: the device runs
// as platformName, the known, normalised name CheckPlatformOption returned,
// whatever its inventory row says. An empty name changes nothing. The
// platform is then set, so no platform_not_set notice follows.
func withPlatform(d inventory.Device, platformName string) inventory.Device {
	if platformName != "" {
		d.Platform = platform.Normalize(platformName) // idempotent after the check; keeps direct callers safe
	}
	return d
}

// overridePlatform is run's --platform: every device of the assembled
// set runs as platformName, the
// inventory's devices and the direct targets alike. It acts after the set is
// assembled, so --select-platform has matched the inventory's own values:
// "--select-platform generic --platform cisco_iosxe" runs the rows the
// inventory calls generic as IOS XE.
func overridePlatform(set *TargetSet, platformName string) {
	if platformName == "" {
		return
	}
	for i := range set.Devices {
		set.Devices[i] = withPlatform(set.Devices[i], platformName)
	}
}
