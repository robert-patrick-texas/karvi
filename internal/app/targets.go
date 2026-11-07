package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/inventoryload"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/planner"
	"github.com/robert-patrick-texas/karvi/internal/resolver"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// TargetInput is records.TargetInput, kept under its old name for callers.
type TargetInput = records.TargetInput

// TargetSet is the assembled, deduplicated, excluded, and ordered device list
// the planner drafts from.
type TargetSet = planner.TargetSet

// resolveOrder reads dispatch.order and the key that goes with it. now
// supplies the epoch seconds for random.
func resolveOrder(cfg configload.Snapshot, now func() time.Time) (order string, key *string) {
	order = cfg.String("dispatch.order")
	switch order {
	case inventory.OrderShuffle:
		k := cfg.String("dispatch.shuffle-key")
		key = &k
	case inventory.OrderRandom:
		k := strconv.FormatInt(now().Unix(), 10)
		key = &k
	case "":
		order = inventory.OrderDefault
	}
	return order, key
}

// AssembleTargets loads the inventory under the invocation's read, then
// assembles the target set for inputs. The login recorder wrapper uses it to
// learn the device a session will connect to.
func AssembleTargets(ctx context.Context, common CommonOptions, inputs []TargetInput, excludes []string, stderr io.Writer) (TargetSet, error) {
	return assembleTargets(ctx, common.Config, common.Operator, inputs, excludes, stderr)
}

// assembleTargets loads the inventory and assembles the target set.
func assembleTargets(ctx context.Context, cfg configload.Snapshot, operator credentials.Operator, inputs []TargetInput, excludes []string, stderr io.Writer) (TargetSet, error) {
	if !hasPositiveInput(inputs) {
		return TargetSet{}, errorcodes.Errorf("inventory_positive_selector_missing", "a target input that selects is required: DEVICE, --target, --tf, --site, --device-group, --select-platform, or --all (a value beginning with \"!\" only removes devices)")
	}
	// The --select-platform values must reach a known platform
	// definition, checked here, before the inventory is read, so
	// the four run paths share it and an inventory fault never masks it.
	if err := checkPlatformSelectors(cfg, inputs); err != nil {
		return TargetSet{}, err
	}
	loader := inventoryload.Loader{Config: cfg, Home: operator.Home, Warn: func(s string) { jobexec.WriteWarning(stderr, cfg, s) }}
	configured, provenance, err := loader.Load(ctx)
	if err != nil {
		return TargetSet{}, errorcodes.Ensure(err, "inventory_load_failed")
	}
	devices, err := assemble(configured, inputs, excludes, planner.SetPlatformFunc(cfg), rulesOf(cfg))
	if err != nil {
		return TargetSet{}, err
	}
	order, key := resolveOrder(cfg, time.Now)
	keyValue := ""
	if key != nil {
		keyValue = *key
	}
	inventory.Sort(devices, order, keyValue)
	return TargetSet{Devices: devices, Provenance: provenance, Order: order, ShuffleKey: key}, nil
}

// targetRules are the configuration's rules for a target name the
// inventory may lack.
type targetRules struct {
	// maxCreated is targets.max-created-names, the most words one name's
	// numeric ranges may spell.
	maxCreated int
	// inventoryOnly is targets.inventory-only: only inventory devices run.
	inventoryOnly bool
}

// rulesOf reads the target rules from cfg.
func rulesOf(cfg configload.Snapshot) targetRules {
	return targetRules{maxCreated: cfg.Int("targets.max-created-names"), inventoryOnly: cfg.Bool("targets.inventory-only")}
}

// assemble is the pure assembly rule: each input contributes at its
// position, a file in file order
// and a selector or glob its inventory matches in inventory order; the first
// occurrence of a lowercase name keeps its position and later ones are
// dropped; --exclude matches and the "not" selectors are then removed.
// Ordering is applied by the caller.
//
// Every selector uses the shared grammar and matches without regard to
// case. A --target, --site, --device-group,
// or --select-platform value beginning with "!" never selects: it removes the
// devices whose field matches, wherever it stands, and a device without a
// value for the field is not removed.
//
// A target name holding a numeric range stands for the words it spells, in
// written order, as if each were typed in its place (matching.Spell): a
// word with no other glob is a name, an inventory device or a direct target
// recorded under the word as typed (input_target), and a word holding a
// glob selects. rules.maxCreated is the most words one name may spell; the
// command line checks it before any target file is read
// (CheckCreatedNames), so here it refuses only a name that did not come
// through it. A "!" name and --exclude match the range and spell nothing.
//
// Under rules.inventoryOnly nothing is spelled or made: a name holding a
// range or a glob selects the inventory's devices it matches, at any size,
// and a plain name the inventory lacks is gathered, once, as first typed;
// after every input the gathered names refuse the set, before any other
// empty-set code (notInInventoryError).
//
// setPlatform is the platform a device is matched on for --select-platform
// (planner.SetPlatformFunc): blank for a
// device whose platform is not set or falls back, which no --select-platform value
// matches.
func assemble(configured []inventory.Device, inputs []TargetInput, excludes []string, setPlatform func(inventory.Device) string, rules targetRules) ([]inventory.Device, error) {
	out := []inventory.Device{}
	seen := map[string]bool{}
	add := func(d inventory.Device) {
		key := strings.ToLower(d.CanonicalName)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, d)
	}
	selectorUsed, selectorMatched := false, false
	selector := func(match func(inventory.Device) bool) {
		selectorUsed = true
		for _, d := range configured {
			if match(d) {
				selectorMatched = true
				add(d)
			}
		}
	}
	var removals []func(inventory.Device) bool
	var absent []string // plain names the inventory lacks, under rules.inventoryOnly
	absentSeen := map[string]bool{}
	// addWord adds one name as typed, or the inventory's matches of one
	// glob; p is typed compiled.
	addWord := func(typed string, p matching.Pattern) error {
		if matching.HasMeta(typed) {
			selector(func(d inventory.Device) bool { return deviceMatchesName(d, p) })
			return nil
		}
		for _, d := range configured {
			if deviceMatchesName(d, p) {
				add(d)
				return nil
			}
		}
		if rules.inventoryOnly {
			if key := strings.ToLower(typed); !absentSeen[key] {
				absentSeen[key] = true
				absent = append(absent, typed)
			}
			return nil
		}
		d := inventory.Direct(strings.ToLower(typed), "", "", 0) // Name and CanonicalName are the lowercase name; the platform is not set
		d.InputToken = typed                                     // as supplied (records: input_target)
		d.TransportExplicit = false
		if err := d.Validate(); err != nil {
			return err
		}
		add(d)
		return nil
	}
	addName := func(raw, option string) error {
		typed := strings.TrimSpace(raw)
		if typed == "" {
			return errorcodes.Errorf("device_name_blank", "a target name is blank")
		}
		sel, err := parseSelector(option, typed)
		if err != nil {
			return err
		}
		if sel.Negated {
			removals = append(removals, func(d inventory.Device) bool { return deviceMatchesName(d, sel.Pattern) })
			return nil
		}
		if !sel.Pattern.Spells() || rules.inventoryOnly {
			return addWord(typed, sel.Pattern)
		}
		words, ok := sel.Pattern.Spell(rules.maxCreated)
		if !ok {
			return createdNamesError(option, typed, sel.Pattern, rules.maxCreated)
		}
		for _, w := range words {
			p, err := matching.Compile(w) // a word of a compiled pattern compiles
			if err != nil {
				return err
			}
			if err := addWord(w, p); err != nil {
				return err
			}
		}
		return nil
	}
	field := func(kind, value string, match func(inventory.Device, matching.Pattern) bool) error {
		sel, err := parseSelector(selectorOption(kind), value)
		if err != nil {
			return err
		}
		if sel.Negated {
			removals = append(removals, func(d inventory.Device) bool { return match(d, sel.Pattern) })
			return nil
		}
		selector(func(d inventory.Device) bool { return match(d, sel.Pattern) })
		return nil
	}
	for _, in := range inputs {
		var err error
		switch in.Kind {
		case "target":
			err = addName(in.Value, "--target")
		case "names":
			for _, n := range in.Names {
				// A target file's "!" lines are comments (targetsource.Lines);
				// a name here never negates.
				if strings.HasPrefix(strings.TrimSpace(n), "!") {
					continue
				}
				if err = addName(n, "target file "+in.Source+" entry"); err != nil {
					break
				}
			}
		case "site":
			err = field(in.Kind, in.Value, func(d inventory.Device, p matching.Pattern) bool { return d.Site != "" && p.Match(d.Site) })
		case "device-group":
			err = field(in.Kind, in.Value, func(d inventory.Device, p matching.Pattern) bool {
				for _, g := range d.Groups {
					if p.Match(g) {
						return true
					}
				}
				return false
			})
		case "platform":
			err = field(in.Kind, matching.PlatformValue(in.Value), func(d inventory.Device, p matching.Pattern) bool {
				set := setPlatform(d)
				return set != "" && p.Match(set)
			})
		case "all":
			selector(func(inventory.Device) bool { return true })
		default:
			err = errorcodes.Errorf("inventory_load_failed", "unknown target input kind %q", in.Kind)
		}
		if err != nil {
			return nil, err
		}
	}
	if len(absent) > 0 {
		return nil, notInInventoryError(absent)
	}
	for _, glob := range excludes {
		sel, err := parseSelector("--exclude", glob)
		if err != nil {
			return nil, err
		}
		if sel.Negated {
			return nil, errorcodes.Errorf("target_selector_pattern_invalid", "--exclude value %q is not a valid selector: a leading \"!\" is refused", glob)
		}
		removals = append(removals, func(d inventory.Device) bool { return deviceMatchesName(d, sel.Pattern) })
	}
	if len(removals) > 0 {
		kept := out[:0]
		for _, d := range out {
			removed := false
			for _, remove := range removals {
				if remove(d) {
					removed = true
					break
				}
			}
			if !removed {
				kept = append(kept, d)
			}
		}
		out = kept
	}
	if len(out) == 0 {
		if selectorUsed && !selectorMatched {
			return nil, errorcodes.Errorf("inventory_empty_selection", "the target selectors matched no devices")
		}
		return nil, errorcodes.Errorf("target_set_empty", "the target set is empty after duplicate removal, --exclude, and the \"!\" selectors")
	}
	return out, nil
}

// CheckCreatedNames refuses a target name whose numeric ranges spell more
// words than limit, targets.max-created-names, counted without spelling
// them: target_created_names_exceeded. label names where the name came
// from ("--tl", "DEVICE", "target file F entry"). A name the grammar
// refuses, a "!" name, and a name without a range create nothing and pass,
// the selector check owning the first.
func CheckCreatedNames(label, value string, limit int) error {
	typed := strings.TrimSpace(value)
	sel, err := matching.ParseSelector(typed)
	if err != nil || sel.Negated || !sel.Pattern.Spells() {
		return nil
	}
	return createdNamesError(label, typed, sel.Pattern, limit)
}

// createdNamesError is CheckCreatedNames's refusal, nil when p spells no
// more than limit words: the name, then the count and the limit stacked.
func createdNamesError(label, typed string, p matching.Pattern, limit int) error {
	words := p.Words()
	if words.Cmp(big.NewInt(int64(limit))) <= 0 {
		return nil
	}
	return errorcodes.Errorf("target_created_names_exceeded", "%s %s creates too many names\n  names: %s\n  limit: %d (targets.max-created-names)", label, typed, words, limit)
}

// notInInventoryError is the refusal of the plain names the inventory
// lacks under targets.inventory-only: the count, then the names stacked,
// resolver.ListedFailures of them and then the count left.
func notInInventoryError(absent []string) error {
	var b strings.Builder
	if len(absent) == 1 {
		b.WriteString("1 target name is not in the inventory (targets.inventory-only)")
	} else {
		fmt.Fprintf(&b, "%d target names are not in the inventory (targets.inventory-only)", len(absent))
	}
	for i, name := range absent {
		if i == resolver.ListedFailures {
			fmt.Fprintf(&b, "\n  and %d more", len(absent)-i)
			break
		}
		fmt.Fprintf(&b, "\n  %s", name)
	}
	return errorcodes.Errorf("target_not_in_inventory", "%s", b.String())
}

// hasPositiveInput reports whether any input can select a device: every
// input but a "not" selector (a --target, --site, --device-group, or
// --select-platform value beginning with "!").
func hasPositiveInput(inputs []TargetInput) bool {
	for _, in := range inputs {
		switch in.Kind {
		case "target", "site", "device-group", "platform":
			if !strings.HasPrefix(strings.TrimSpace(in.Value), "!") {
				return true
			}
		default:
			return true
		}
	}
	return false
}

// parseSelector compiles a selector for assembly; the command line has
// already refused a malformed one, so an error here names a target file's
// entry or an input that did not come through the parser.
func parseSelector(option, value string) (matching.Selector, error) {
	sel, err := matching.ParseSelector(value)
	if err != nil {
		var pe *matching.PatternError
		if errors.As(err, &pe) {
			return matching.Selector{}, errorcodes.Errorf("target_selector_pattern_invalid", "%s %q is not a valid selector: %s", option, value, pe.Reason)
		}
		return matching.Selector{}, err
	}
	return sel, nil
}

// deviceMatchesName reports whether the pattern matches the device's
// canonical name, inventory name, or ID, without regard to case.
func deviceMatchesName(d inventory.Device, p matching.Pattern) bool {
	return p.Match(d.CanonicalName) || p.Match(d.Name) || p.Match(d.ID)
}

// CheckManagementAddress refuses a literal address with a set of more than one
// device without changing the set; the recording wrapper uses it
// before the child applies the address.
func CheckManagementAddress(set TargetSet, address string) error {
	copy := TargetSet{Devices: append([]inventory.Device(nil), set.Devices...)}
	return applyManagementAddress(&copy, address)
}

// applyManagementAddress attaches a literal --management-address to the one
// device of the set.
func applyManagementAddress(set *TargetSet, address string) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return nil
	}
	if len(set.Devices) != 1 {
		return errorcodes.Errorf("management_address_scope_error", "--management-address/--address requires exactly one selected device; the target set holds %d", len(set.Devices))
	}
	addr, err := netip.ParseAddr(address)
	if err != nil {
		return errorcodes.Errorf("management_address_invalid", "invalid --management-address %q: %w", address, err)
	}
	addr = addr.Unmap()
	d := &set.Devices[0]
	// The literal replaces the management address; the inventory alternates
	// stay behind it in the selection rule.
	addresses := []inventory.Address{{Address: addr, Role: inventory.RoleManagement, Source: "cli"}}
	for _, alt := range d.Alternates() {
		if alt != addr {
			addresses = append(addresses, inventory.Address{Address: alt, Role: inventory.RoleAlternate, Source: "inventory"})
		}
	}
	d.ManagementAddress = addr
	d.Addresses = addresses
	return nil
}

// applyAuthorityOverrides applies run's repeatable TARGET=client|daemon
// values to the assembled set: the target part is
// matched without globs, case-insensitively, against canonical name, name,
// or ID; a name matching nothing is address_authority_target_unknown. The
// value is written to the device field and returned keyed by device ID for
// the planner's precedence; a later value for the same target wins.
func applyAuthorityOverrides(set *TargetSet, values []string) (map[string]string, error) {
	overrides := map[string]string{}
	for _, raw := range values {
		target, authority, err := parseAuthorityOverride(raw)
		if err != nil {
			return nil, err
		}
		matched := false
		for i := range set.Devices {
			d := &set.Devices[i]
			if strings.EqualFold(d.CanonicalName, target) || strings.EqualFold(d.Name, target) || strings.EqualFold(d.ID, target) {
				d.AddressAuthority = authority
				overrides[d.ID] = authority
				matched = true
			}
		}
		if !matched {
			return nil, errorcodes.Errorf("address_authority_target_unknown", "--address-authority %s: %q is not in the target set", raw, target)
		}
	}
	return overrides, nil
}

// parseAuthorityOverride splits TARGET=client|daemon.
func parseAuthorityOverride(raw string) (target, authority string, err error) {
	target, authority, ok := strings.Cut(raw, "=")
	target = strings.TrimSpace(target)
	authority = strings.ToLower(strings.TrimSpace(authority))
	if !ok || target == "" || (authority != inventory.AuthorityClient && authority != inventory.AuthorityDaemon) {
		return "", "", errorcodes.Errorf("cli_option_value_invalid", "--address-authority %q must be TARGET=client|daemon", raw)
	}
	return target, authority, nil
}

// applyFirstAuthority applies command's and login's single --address-authority
// value to the first device, as --platform does.
func applyFirstAuthority(d *inventory.Device, authority string) map[string]string {
	authority = strings.ToLower(strings.TrimSpace(authority))
	if authority == "" {
		return nil
	}
	d.AddressAuthority = authority
	return map[string]string{d.ID: authority}
}

// scopeSummary renders the target inputs for scoreboards and audit.
