package app

import (
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/platform"
)

// selectorOption is the option word of a selector input's kind, for
// messages: the "platform" kind is --select-platform (--platform names the
// platform the devices run as), every other kind is its own word.
func selectorOption(kind string) string {
	if kind == "platform" {
		return "--select-platform"
	}
	return "--" + kind
}

// checkPlatformSelectors is the one check of the --select-platform
// selectors. Each platform input is
// parsed with the shared grammar (a malformed pattern keeps
// target_selector_pattern_invalid): a literal, after normalisation, must be
// a known platform, a built-in or a configured [platform.NAME] table; a glob
// must match at least one known name over the known set, without regard to
// case; a "!" value is held to the same rule on the pattern after the "!".
// Otherwise platform_selector_unknown (usage, exit 4), naming the value and
// the known platforms in their known order. So "cisco*" passes and
// "cisco_iosx", "nexus*", and "!nexus*" are refused before the inventory is
// read. Matching itself is unchanged: a device whose platform is set and
// matches the pattern; a known value matching no device stays
// inventory_empty_selection.
func checkPlatformSelectors(cfg configload.Snapshot, inputs []TargetInput) error {
	tables := cfg.NamedTables("platform")
	var names []string // the known names, listed once a check needs them
	for _, in := range inputs {
		if in.Kind != "platform" {
			continue
		}
		sel, err := parseSelector(selectorOption(in.Kind), in.Value)
		if err != nil {
			return err
		}
		if names == nil {
			names = platform.KnownNames(tables)
		}
		bare := strings.TrimPrefix(in.Value, "!")
		if !sel.Negated {
			bare = in.Value
		}
		if !matching.HasMeta(bare) {
			if platform.Known(bare, tables) {
				continue
			}
			return errorcodes.Errorf("platform_selector_unknown", "--select-platform %q is not a known platform; known platforms: %s", in.Value, strings.Join(names, ", "))
		}
		matched := false
		for _, name := range names {
			if sel.Pattern.Match(name) {
				matched = true
				break
			}
		}
		if !matched {
			return errorcodes.Errorf("platform_selector_unknown", "--select-platform %q matches no known platform; known platforms: %s", in.Value, strings.Join(names, ", "))
		}
	}
	return nil
}
