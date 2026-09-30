package matching

import (
	"fmt"
	"net/netip"
	"strings"
)

// RuleKeys are a map rule's match keys in evaluation order.
var RuleKeys = []string{"name", "address-cidr", "platform", "site", "device-group"}

// Fields is the device view a map rule is evaluated against: the canonical
// name, the selected management address, and the inventory's platform, site,
// and groups.
type Fields struct {
	Name     string
	Address  netip.Addr
	Platform string
	Site     string
	Groups   []string
}

// RuleError is a rule value that does not compile. Key and Value name it;
// Err is a *PatternError or a CIDR parse error.
type RuleError struct {
	Key, Value string
	CIDR       bool
	Err        error
}

func (e *RuleError) Error() string {
	if e.CIDR {
		return fmt.Sprintf("%s value %q is not a CIDR prefix: %v", e.Key, e.Value, e.Err)
	}
	return fmt.Sprintf("%s: %v", e.Key, e.Err)
}
func (e *RuleError) Unwrap() error { return e.Err }

// Values reads a rule key's values: a string or an array of strings.
func Values(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// CheckRule compiles every value of the rule's match keys and returns the
// first that fails, as a *RuleError. The key checks of configuration
// validation (a key with only negations, a rule with no key) stay with the
// caller.
func CheckRule(rule map[string]any) error {
	for _, key := range RuleKeys {
		for _, raw := range Values(rule[key]) {
			if _, _, err := compileValue(key, raw); err != nil {
				return err
			}
		}
	}
	return nil
}

type compiled struct {
	negated  bool
	selector Selector
	prefix   netip.Prefix
}

func compileValue(key, raw string) (compiled, bool, error) {
	if key == "address-cidr" {
		c := compiled{negated: strings.HasPrefix(raw, "!")}
		p, err := netip.ParsePrefix(strings.TrimPrefix(raw, "!"))
		if err != nil {
			return compiled{}, false, &RuleError{Key: key, Value: raw, CIDR: true, Err: err}
		}
		c.prefix = p.Masked()
		return c, true, nil
	}
	s, err := ParseSelector(raw)
	if err != nil {
		return compiled{}, false, &RuleError{Key: key, Value: raw, Err: err}
	}
	return compiled{negated: s.Negated, selector: s}, false, nil
}

// EvaluateRule reports whether every populated key of rule matches f and,
// for a rule with address-cidr, the longest matching
// prefix length; prefix is -1 otherwise.
func EvaluateRule(rule map[string]any, f Fields) (matched bool, prefix int, err error) {
	prefix = -1
	for _, key := range RuleKeys {
		raw, exists := rule[key]
		if !exists {
			continue
		}
		ok, best, err := evaluateKey(key, Values(raw), f)
		if err != nil {
			return false, -1, err
		}
		if !ok {
			return false, -1, nil
		}
		if best > prefix {
			prefix = best
		}
	}
	return true, prefix, nil
}

func evaluateKey(key string, values []string, f Fields) (bool, int, error) {
	positive, matched := false, false
	best := -1
	for _, raw := range values {
		c, cidr, err := compileValue(key, raw)
		if err != nil {
			return false, -1, err
		}
		m, bits := false, -1
		switch {
		case cidr:
			m = f.Address.IsValid() && c.prefix.Contains(f.Address)
			bits = c.prefix.Bits()
		case key == "device-group":
			for _, g := range f.Groups {
				if c.selector.Match(g) {
					m = true
					break
				}
			}
		default:
			m = c.selector.Match(fieldValue(key, f))
		}
		if c.negated {
			if m {
				return false, -1, nil
			}
			continue
		}
		positive = true
		if m {
			matched = true
			if bits > best {
				best = bits
			}
		}
	}
	return positive && matched, best, nil
}

func fieldValue(key string, f Fields) string {
	switch key {
	case "name":
		return f.Name
	case "platform":
		return f.Platform
	case "site":
		return f.Site
	}
	return ""
}

// AmbiguousError is two or more address-cidr rules matching at the same
// longest prefix; Indices are the rules' zero-based positions.
type AmbiguousError struct {
	Prefix  int
	Indices []int
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("rules %v match with the same prefix length %d", e.Indices, e.Prefix)
}

// Select returns the index of the rule selected for f: the earlier of
// the first matching rule without address-cidr and the sole matching
// address-cidr rule with the longest prefix. It returns -1 when no rule
// matches, an *AmbiguousError for equal longest prefixes, and a *RuleError
// for a value that does not compile.
func Select(rules []map[string]any, f Fields) (int, error) {
	ordinary := -1
	bestPrefix := -1
	var finalists []int
	for i, rule := range rules {
		ok, prefix, err := EvaluateRule(rule, f)
		if err != nil {
			return -1, err
		}
		if !ok {
			continue
		}
		if _, cidr := rule["address-cidr"]; !cidr {
			if ordinary < 0 {
				ordinary = i
			}
			continue
		}
		switch {
		case prefix > bestPrefix:
			bestPrefix, finalists = prefix, []int{i}
		case prefix == bestPrefix:
			finalists = append(finalists, i)
		}
	}
	if len(finalists) > 1 {
		return -1, &AmbiguousError{Prefix: bestPrefix, Indices: finalists}
	}
	chosen := ordinary
	if len(finalists) == 1 && (chosen < 0 || finalists[0] < chosen) {
		chosen = finalists[0]
	}
	return chosen, nil
}
