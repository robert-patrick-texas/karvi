// Package sshalgorithms holds the SSH algorithm lists both device transports
// offer: the global defaults, strongest first;
// which names are default, allowed only in a per-host profile, or refused;
// a profile's replace and append forms; the map rule that selects a
// device's profile; and a transport's filter to the names it implements.
package sshalgorithms

import (
	"errors"
	"fmt"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
)

// Kind is one algorithm list, named as its configuration key.
type Kind string

const (
	HostKey Kind = "host-key"
	Kex     Kind = "kex"
	Ciphers Kind = "ciphers"
	MACs    Kind = "macs"
)

// The sources of a device's key exchange, cipher, and MAC lists
// (ssh-algorithms.source, or its profile's source): karvi's lists, or the
// transport's own defaults. The host-key list is karvi's under either.
const (
	SourceKarvi     = "karvi"
	SourceTransport = "transport"
)

// Kinds are the lists in configuration order.
var Kinds = []Kind{HostKey, Kex, Ciphers, MACs}

// Label is the list's name in a message.
func (k Kind) Label() string {
	switch k {
	case HostKey:
		return "host key"
	case Kex:
		return "key exchange"
	case Ciphers:
		return "cipher"
	}
	return "MAC"
}

// defaults are the global lists, strongest first.
var defaults = map[Kind][]string{
	HostKey: {"ssh-ed25519", "ecdsa-sha2-nistp521", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp256", "rsa-sha2-512", "rsa-sha2-256", "ssh-rsa"},
	Kex: {"mlkem768x25519-sha256", "sntrup761x25519-sha512@openssh.com", "curve25519-sha256", "curve25519-sha256@libssh.org",
		"ecdh-sha2-nistp521", "ecdh-sha2-nistp384", "ecdh-sha2-nistp256",
		"diffie-hellman-group18-sha512", "diffie-hellman-group16-sha512", "diffie-hellman-group-exchange-sha256",
		"diffie-hellman-group14-sha256", "diffie-hellman-group14-sha1"},
	Ciphers: {"aes256-gcm@openssh.com", "chacha20-poly1305@openssh.com", "aes256-ctr", "aes256-cbc"},
	MACs:    {"hmac-sha2-512-etm@openssh.com", "hmac-sha2-256-etm@openssh.com", "hmac-sha2-512", "hmac-sha2-256", "hmac-sha1"},
}

// allowedOnly are names a per-host profile may add; the global section may
// not.
var allowedOnly = map[Kind][]string{
	Kex:     {"diffie-hellman-group1-sha1", "diffie-hellman-group-exchange-sha1"},
	Ciphers: {"aes192-ctr", "aes192-cbc", "aes128-gcm@openssh.com", "aes128-ctr", "aes128-cbc"},
	MACs:    {"umac-128-etm@openssh.com", "umac-128@openssh.com"},
}

// refused are names never offered.
var refused = map[Kind][]string{
	HostKey: {"ssh-dss"},
	Ciphers: {"3des-cbc", "blowfish-cbc", "cast128-cbc", "rijndael-cbc@lysator.liu.se", "arcfour", "arcfour128", "arcfour256", "none"},
	MACs: {"hmac-md5", "hmac-md5-etm@openssh.com", "hmac-md5-96", "hmac-md5-96-etm@openssh.com",
		"hmac-sha1-96", "hmac-sha1-96-etm@openssh.com", "umac-64@openssh.com", "umac-64-etm@openssh.com"},
}

// Class is how a name may be used.
type Class int

const (
	Unknown Class = iota
	Default
	AllowedOnly
	Refused
)

// Classify places name in kind's vocabulary. Names compare exactly.
func Classify(kind Kind, name string) Class {
	switch {
	case contains(defaults[kind], name):
		return Default
	case contains(allowedOnly[kind], name):
		return AllowedOnly
	case contains(refused[kind], name):
		return Refused
	case kind == HostKey && (strings.HasSuffix(name, "-cert-v01@openssh.com") || strings.HasPrefix(name, "sk-") || strings.HasPrefix(name, "webauthn-")):
		return Refused
	}
	return Unknown
}

// Lists is one set of the four lists.
type Lists map[Kind][]string

// Defaults is a copy of the global defaults.
func Defaults() Lists {
	out := Lists{}
	for _, k := range Kinds {
		out[k] = append([]string(nil), defaults[k]...)
	}
	return out
}

// Clone is a deep copy.
func (l Lists) Clone() Lists {
	out := Lists{}
	for k, v := range l {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// Scope is where a list is written.
type Scope int

const (
	Global Scope = iota
	Profile
)

// CheckList validates one list written at scope and returns the code of the
// first failing rule.
func CheckList(kind Kind, names []string, scope Scope) (string, error) {
	if len(names) == 0 {
		return "config_ssh_algorithm_list_empty", errors.New("the list is empty")
	}
	seen := map[string]bool{}
	for _, name := range names {
		switch Classify(kind, name) {
		case Unknown:
			return "config_ssh_algorithm_unknown", fmt.Errorf("%q is not a %s algorithm karvi knows", name, kind.Label())
		case Refused:
			return "config_ssh_algorithm_forbidden", fmt.Errorf("%q is refused", name)
		case AllowedOnly:
			if scope == Global {
				return "config_ssh_algorithm_profile_only", fmt.Errorf("%q may be added only by an ssh-algorithms-profile", name)
			}
		}
		if seen[name] {
			return "config_ssh_algorithm_duplicate", fmt.Errorf("%q is listed twice", name)
		}
		seen[name] = true
	}
	return "", nil
}

// ProfileError is a profile's failing rule: Field is the list key.
type ProfileError struct {
	Code, Field string
	Err         error
}

func (e *ProfileError) Error() string { return e.Err.Error() }

// CheckProfile validates a [ssh-algorithms-profile.NAME] table against the
// global lists and the global source: each list in the replace form or the
// append form, not both; at least one list or the source set; an appended
// name not already global; and no key exchange, cipher, or MAC list in a
// profile whose devices take the transport's (its source, else the global
// one, SourceTransport), where it would go unread.
func CheckProfile(profile map[string]any, global Lists, source string) error {
	own, set := profile["source"].(string)
	if set {
		source = own
	}
	for _, k := range Kinds {
		replace, hasReplace := profile[string(k)]
		appended, hasAppend := profile[string(k)+"-append"]
		if hasReplace && hasAppend {
			return &ProfileError{Code: "config_ssh_algorithms_profile_list_conflict", Field: string(k), Err: fmt.Errorf("%s and %s-append are both set", k, k)}
		}
		if source == SourceTransport && k != HostKey && (hasReplace || hasAppend) {
			field, whose := string(k), "ssh-algorithms.source"
			if hasAppend {
				field += "-append"
			}
			if own != "" {
				whose = "the profile's source"
			}
			return &ProfileError{Code: "config_ssh_algorithms_profile_lists_unread", Field: field, Err: fmt.Errorf("%s is read only under source %q, and %s is %q: this profile's devices take the transport's lists; set source = %q in the profile", field, SourceKarvi, whose, SourceTransport, SourceKarvi)}
		}
		switch {
		case hasReplace:
			set = true
			if code, err := CheckList(k, matching.Values(replace), Profile); err != nil {
				return &ProfileError{Code: code, Field: string(k), Err: err}
			}
		case hasAppend:
			set = true
			names := matching.Values(appended)
			if code, err := CheckList(k, names, Profile); err != nil {
				return &ProfileError{Code: code, Field: string(k) + "-append", Err: err}
			}
			for _, name := range names {
				if contains(global[k], name) {
					return &ProfileError{Code: "config_ssh_algorithm_duplicate", Field: string(k) + "-append", Err: fmt.Errorf("%q is already in the global %s list", name, k)}
				}
			}
		}
	}
	if !set {
		return &ProfileError{Code: "config_ssh_algorithms_profile_empty", Err: errors.New("the profile sets no list and no source")}
	}
	return nil
}

// Apply is the lists a device matched to profile uses: each list the
// profile replaces, the global list with the profile's appended names after
// it, or the global list.
func Apply(global Lists, profile map[string]any) Lists {
	out := global.Clone()
	for _, k := range Kinds {
		if v, ok := profile[string(k)]; ok {
			out[k] = matching.Values(v)
		} else if v, ok := profile[string(k)+"-append"]; ok {
			out[k] = append(out[k], matching.Values(v)...)
		}
	}
	return out
}

// Selection is a device's lists and where they came from.
type Selection struct {
	Lists   Lists
	Profile string // "" for the global section
	Rule    int    // the map rule's index; -1 for the global section
}

// Select chooses a device's lists: the profile of the map rule the shared
// matcher selects for f, else the global lists. Two address-cidr rules of
// equal longest prefix are ssh_algorithms_map_ambiguous.
func Select(global Lists, profiles map[string]map[string]any, rules []map[string]any, f matching.Fields) (Selection, error) {
	i, err := matching.Select(rules, f)
	if err != nil {
		var ambiguous *matching.AmbiguousError
		if errors.As(err, &ambiguous) {
			refs := make([]string, len(ambiguous.Indices))
			for n, index := range ambiguous.Indices {
				refs[n] = fmt.Sprintf("ssh-algorithms-map.%d", index)
			}
			return Selection{}, errorcodes.Errorf("ssh_algorithms_map_ambiguous", "%s match %s with the same prefix length %d", strings.Join(refs, ", "), f.Name, ambiguous.Prefix)
		}
		return Selection{}, errorcodes.Errorf("config_match_rule_pattern_invalid", "ssh-algorithms-map: %v", err)
	}
	if i < 0 {
		return Selection{Lists: global.Clone(), Rule: -1}, nil
	}
	name, _ := rules[i]["profile"].(string)
	return Selection{Lists: Apply(global, profiles[name]), Profile: name, Rule: i}, nil
}

// Offer is the lists filtered, in order, to the names a transport
// implements. A list left empty is ssh_algorithms_unavailable naming the
// list and the transport.
func (l Lists) Offer(transport string, implements func(Kind, string) bool) (Lists, error) {
	out := Lists{}
	for _, k := range Kinds {
		for _, name := range l[k] {
			if implements(k, name) {
				out[k] = append(out[k], name)
			}
		}
		if len(out[k]) == 0 {
			return nil, errorcodes.Errorf("ssh_algorithms_unavailable", "none of the configured %s algorithms (%s) is implemented by %s", k.Label(), strings.Join(l[k], ","), transport)
		}
	}
	return out, nil
}

// Describe is the lists for a debug line.
func (l Lists) Describe() string {
	parts := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		parts = append(parts, fmt.Sprintf("%s=%s", k, strings.Join(l[k], ",")))
	}
	return strings.Join(parts, " ")
}

// NegotiationFailed is ssh_algorithm_negotiation_failed for a list the
// device offered nothing of.
func NegotiationFailed(kind Kind, offered, deviceOffer []string) error {
	return errorcodes.Errorf("ssh_algorithm_negotiation_failed", "no %s algorithm in common: karvi offered %s; the device offered %s", kind.Label(), strings.Join(offered, ","), strings.Join(cleanOffer(deviceOffer), ","))
}

// cleanOffer drops the protocol markers a key-exchange offer carries.
func cleanOffer(offer []string) []string {
	var out []string
	for _, name := range offer {
		if name == "" || name == "ext-info-c" || name == "ext-info-s" || strings.HasPrefix(name, "kex-strict-") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
