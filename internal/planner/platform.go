package planner

import (
	"fmt"
	"strings"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
)

// PlatformResolution is what the planner decides for one device: the
// platform the session uses, and the notice that says why when the device
// did not set it, carried by the plan target to the device's first record.
type PlatformResolution struct {
	Platform string
	Notice   *executionplan.TargetNotice
}

// The platform-resolution notice codes.
const (
	NoticePlatformNotSet          = "platform_not_set"
	NoticePlatformUnknownFallback = "platform_unknown_fallback"
)

// ResolvePlatforms is the one place a target set's platforms are resolved to
// the platforms the sessions use, run once over the whole set, first in the plan draft, so the operator
// sees every bad value at once. The set platform, each device's own field,
// stays what the row, the source default, or --platform set, and the run
// selector and the maps match that field.
//
// Per device, the first that applies decides: a known set platform is used
// as named; an unknown set platform refuses the activity with
// platform_unknown (inventory, exit 5) under platform-resolution.on-unknown
// = "fail", or under "warn" proceeds as
// platform-resolution.unknown-fallback with the notice
// platform_unknown_fallback; a blank platform, not set,
// proceeds as platform-resolution.default when configured, else generic
// with the notice platform_not_set for an inventory row and no notice for a
// direct target.
//
// The refusal is one error covering every unknown value: one clause per
// distinct value per inventory source, in inventory order, naming the
// source and the first offending line, the count, and the first three
// devices, then the known platforms once. When the activity proceeds, warn
// (when given) receives the warning lines with the same
// grouping, one per distinct unknown value per source and one per source
// with not-set rows. A refusal prints no warnings: the error is the whole
// answer.
func ResolvePlatforms(cfg configload.Snapshot, devices []inventory.Device, warn func(string)) ([]PlatformResolution, error) {
	tables := cfg.NamedTables("platform")
	defaultName := platform.Normalize(cfg.String("platform-resolution.default"))
	fallback := platform.Normalize(cfg.String("platform-resolution.unknown-fallback"))
	if fallback == "" {
		fallback = "generic"
	}
	warnUnknown := cfg.String("platform-resolution.on-unknown") == "warn"
	out := make([]PlatformResolution, len(devices))
	var unknown, fallen, notSet []platformGroup // in inventory order
	for i, d := range devices {
		name := platform.Normalize(d.Platform)
		switch {
		case name != "" && platform.Known(name, tables):
			out[i] = PlatformResolution{Platform: name}
		case name != "" && !warnUnknown:
			unknown = addToGroup(unknown, d, name, "")
		case name != "":
			out[i] = PlatformResolution{Platform: fallback, Notice: &executionplan.TargetNotice{
				Code:    "platform_unknown_fallback",
				Message: fmt.Sprintf("platform %q is not a known platform; proceeding as %s (platform-resolution.on-unknown = \"warn\")", name, fallback),
				Details: map[string]string{"supplied": name, "source": sourceLine(d), "used": fallback},
			}}
			fallen = addToGroup(fallen, d, name, fallback)
		default:
			used := defaultName
			if used == "" {
				used = "generic"
			}
			out[i] = PlatformResolution{Platform: used}
			if d.Source.Name == "direct" {
				continue // setup-free direct work takes no notice
			}
			out[i].Notice = &executionplan.TargetNotice{
				Code:    "platform_not_set",
				Message: fmt.Sprintf("no platform in inventory source %s; proceeding as %s (%s)", sourceLine(d), used, defaultOrigin(defaultName)),
				Details: map[string]string{"supplied": "", "source": sourceLine(d), "used": used},
			}
			notSet = addToGroup(notSet, d, "", used)
		}
	}
	if len(unknown) > 0 {
		clauses := make([]string, 0, len(unknown))
		for _, g := range unknown {
			clauses = append(clauses, fmt.Sprintf("inventory source %s: platform %q is not a known platform; %s", g.source, g.value, g.devices()))
		}
		return nil, errorcodes.Errorf("platform_unknown", "%s (known: %s)", strings.Join(clauses, "; "), strings.Join(platform.KnownNames(tables), ", "))
	}
	if warn != nil {
		for _, g := range fallen {
			warn(fmt.Sprintf("%s: inventory source %s: platform %q is not a known platform; %s %s as %s (platform-resolution.unknown-fallback): %s", NoticePlatformUnknownFallback, g.source, g.value, g.count(), g.verb("proceeds", "proceed"), g.used, g.names()))
		}
		for _, g := range notSet {
			warn(fmt.Sprintf("%s: inventory source %s: %s %s no platform; %s as %s (%s): %s", NoticePlatformNotSet, g.source, g.count(), g.verb("has", "have"), g.verb("it proceeds", "they proceed"), g.used, defaultOrigin(defaultName), g.names()))
		}
	}
	return out, nil
}

// ResolvePlatform is the one-device form of ResolvePlatforms without the
// warning lines: the login --record wrapper's transcript metadata, whose
// child prints the warning itself.
func ResolvePlatform(cfg configload.Snapshot, d inventory.Device) (PlatformResolution, error) {
	out, err := ResolvePlatforms(cfg, []inventory.Device{d}, nil)
	if err != nil {
		return PlatformResolution{}, err
	}
	return out[0], nil
}

// platformGroup is one clause of the refusal or one warning line: the
// devices of one inventory source sharing one supplied value (the unknown
// value, or blank for not set), in inventory order.
type platformGroup struct {
	key, source, value, used string
	first                    string // the source and line of the first device
	all                      []string
}

func (g platformGroup) count() string {
	if len(g.all) == 1 {
		return "1 device"
	}
	return fmt.Sprintf("%d devices", len(g.all))
}

// names lists the first three devices, then an ellipsis when there are more.
func (g platformGroup) names() string {
	if len(g.all) <= 3 {
		return strings.Join(g.all, ", ")
	}
	return strings.Join(g.all[:3], ", ") + ", …"
}

// verb picks the singular or plural form for the group's count.
func (g platformGroup) verb(one, many string) string {
	if len(g.all) == 1 {
		return one
	}
	return many
}

func (g platformGroup) devices() string { return g.count() + ": " + g.names() }

func addToGroup(groups []platformGroup, d inventory.Device, value, used string) []platformGroup {
	key := d.Source.Name + "\x00" + value
	for i := range groups {
		if groups[i].key == key {
			groups[i].all = append(groups[i].all, d.CanonicalName)
			return groups
		}
	}
	source := d.Source.Name
	if value != "" {
		source = sourceLine(d) // the first offending line names the value
	}
	return append(groups, platformGroup{key: key, source: source, value: value, used: used, first: sourceLine(d), all: []string{d.CanonicalName}})
}

// sourceLine is the notice's source detail: the inventory source's name
// and, when the loader recorded one, the row's line.
func sourceLine(d inventory.Device) string {
	if d.Source.Line > 0 {
		return fmt.Sprintf("%s line %d", d.Source.Name, d.Source.Line)
	}
	return d.Source.Name
}

func defaultOrigin(defaultName string) string {
	if defaultName == "" {
		return "platform-resolution.default is empty"
	}
	return "platform-resolution.default"
}

// SetPlatformFunc returns the rule for the platform a device is matched on
// for the credential and session-init maps: the set platform,
// blank when the device's field is blank (not set), and blank when the
// field names an unknown platform under platform-resolution.on-unknown =
// "warn", since that device falls back and a fallback is not set for
// matching; otherwise the normalised name. Under "fail" an unknown name
// stays set, so a run selector reaches it and the activity is refused at
// planning. The two readers are the target-set assembly's
// --select-platform selector and the credential planner's device view, which the
// credential-policy and session-init maps match; the loader stays what the
// inventory says. The tables are read once, for a fleet-sized set.
func SetPlatformFunc(cfg configload.Snapshot) func(inventory.Device) string {
	tables := cfg.NamedTables("platform")
	warnUnknown := cfg.String("platform-resolution.on-unknown") == "warn"
	return func(d inventory.Device) string {
		name := platform.Normalize(d.Platform)
		if name == "" || (warnUnknown && !platform.Known(name, tables)) {
			return ""
		}
		return name
	}
}

// SetPlatform is SetPlatformFunc for one device.
func SetPlatform(cfg configload.Snapshot, d inventory.Device) string { return SetPlatformFunc(cfg)(d) }
