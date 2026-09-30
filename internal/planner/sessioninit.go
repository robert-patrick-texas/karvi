package planner

import (
	"errors"
	"sort"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/inventory"
)

// sessionInitSelector chooses a target's session-init profile with the
// session-init map over the device view its credential sees. The
// configuration was validated at load;
// the profiles are converted once to the plan's form.
type sessionInitSelector struct {
	cfg      configload.Snapshot
	rules    []map[string]any
	profiles map[string]executionplan.SessionInitProfile
}

func newSessionInitSelector(cfg configload.Snapshot) (*sessionInitSelector, error) {
	s := &sessionInitSelector{cfg: cfg, rules: cfg.IndexedTables("session-init-map"), profiles: map[string]executionplan.SessionInitProfile{}}
	for name, table := range cfg.NamedTables("session-init") {
		profile := executionplan.SessionInitProfile{Commands: []string{}, OnError: executionplan.SessionInitFailDevice}
		switch list := table["commands"].(type) {
		case []string:
			profile.Commands = append(profile.Commands, list...)
		case []any:
			for _, item := range list {
				c, _ := item.(string)
				profile.Commands = append(profile.Commands, c)
			}
		}
		if v, ok := table["on-error"].(string); ok {
			profile.OnError = v
		}
		if v, ok := table["command-timeout"].(string); ok {
			d, err := time.ParseDuration(v)
			if err != nil {
				return nil, errorcodes.Errorf("config_duration_error", "session-init.%s.command-timeout: invalid Go duration", name)
			}
			profile.CommandTimeoutNS = d.Nanoseconds()
		}
		s.profiles[name] = profile
	}
	return s, nil
}

// selectProfile returns the profile name for d, or none when no map is
// configured. Errors carry the credential map's mapping.
func (s *sessionInitSelector) selectProfile(d inventory.Device) (string, error) {
	if len(s.rules) == 0 {
		return executionplan.SessionInitNone, nil
	}
	fields := matching.Fields{Name: d.CanonicalName, Address: d.ManagementAddress, Platform: d.Platform, Site: d.Site, Groups: d.Groups}
	index, err := matching.Select(s.rules, fields)
	var ambiguous *matching.AmbiguousError
	var ruleErr *matching.RuleError
	switch {
	case errors.As(err, &ambiguous):
		return "", errorcodes.Errorf("session_init_prefix_ambiguous", "rules %s match %s with the same prefix length %d", s.cfg.RuleList("session-init-map", "profile", ambiguous.Indices), d.CanonicalName, ambiguous.Prefix)
	case errors.As(err, &ruleErr) && ruleErr.CIDR:
		return "", errorcodes.Errorf("config_match_rule_cidr_invalid", "session-init-map: %v", ruleErr)
	case errors.As(err, &ruleErr):
		return "", errorcodes.Errorf("config_match_rule_pattern_invalid", "session-init-map: %v", ruleErr)
	case err != nil:
		return "", err
	case index < 0:
		return "", errorcodes.Errorf("session_init_map_unmatched", "no session-init profile matched %s", d.CanonicalName)
	}
	name, _ := s.rules[index]["profile"].(string)
	if _, ok := s.profiles[name]; !ok {
		return "", errorcodes.Errorf("config_session_init_map_profile_unknown", "unknown session-init profile %s", name)
	}
	return name, nil
}

// table is the plan's session-init table for the selected names: only the
// profiles some target selects, each with its own command slice.
func (s *sessionInitSelector) table(names []string) map[string]executionplan.SessionInitProfile {
	out := map[string]executionplan.SessionInitProfile{}
	sort.Strings(names)
	for _, name := range names {
		if name == executionplan.SessionInitNone {
			continue
		}
		profile := s.profiles[name]
		profile.Commands = append([]string{}, profile.Commands...)
		out[name] = profile
	}
	return out
}
