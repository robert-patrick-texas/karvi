// Package inventoryload adapts configured CSV sources to the public inventory
// model. It owns source expansion and duplicate/conflict accounting.
package inventoryload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/tabular"
	"github.com/robert-patrick-texas/karvi/transform"
)

type Loader struct {
	Config configload.Snapshot
	Home   string
	Warn   func(string)
}

// logicalFields are the inventory columns the loader reads. `credkeyref`
// is found under its own name or a mapping
// (mappings.credkeyref); it is one word on purpose, so it has no underscore
// or hyphen spelling to confuse.
var logicalFields = []string{"id", "name", "management_address", "addresses", "address_authority", "platform", "transport", "port", "site", "groups", "name_transform", "session_cap", "risk_tier", "deployment_ring", "topology_domain", "credkeyref"}

// defaultAliases are the column aliases beyond the underscore and hyphen
// spellings: `alternate_ips` for the alternates and `dns_authority` for the
// address authority.
var defaultAliases = map[string][]string{
	"addresses":         {"alternate_ips", "alternate-ips"},
	"address_authority": {"dns_authority", "dns-authority"},
}

func (l Loader) Load(ctx context.Context) ([]inventory.Device, inventory.Provenance, error) {
	profiles, err := buildTransforms(l.Config)
	if err != nil {
		return nil, inventory.Provenance{}, err
	}
	sources := l.Config.IndexedTables("inventory-source")
	all := []inventory.Device{}
	prov := inventory.Provenance{}
	seen := map[string]inventory.Device{}
	for i, src := range sources {
		name, _ := src["name"].(string)
		path, _ := src["path"].(string)
		required := true
		if v, ok := src["required"].(bool); ok {
			required = v
		}
		if strings.HasPrefix(path, "~/") {
			path = filepath.Join(l.Home, path[2:])
		}
		paths, err := filepath.Glob(path)
		if err != nil {
			return nil, prov, errorcodes.Errorf("inventory_source_pattern_invalid", "inventory source %s path: %w", name, err)
		}
		sort.Strings(paths)
		if len(paths) == 0 {
			if required {
				return nil, prov, errorcodes.Errorf("inventory_missing", "inventory source %s matched no files: %s", name, path)
			}
			l.warning(fmt.Sprintf("optional inventory source %s matched no files", name))
			continue
		}
		for _, file := range paths {
			devices, ref, err := l.readFile(ctx, i, name, file, src, profiles)
			if err != nil {
				return nil, prov, err
			}
			prov.Sources = append(prov.Sources, ref)
			for _, d := range devices {
				if prior, ok := seen[d.ID]; ok {
					if sameDevice(prior, d) {
						l.warning(fmt.Sprintf("duplicate inventory device %s coalesced (%s:%d and %s:%d)", d.ID, prior.Source.Path, prior.Source.Line, d.Source.Path, d.Source.Line))
						continue
					}
					return nil, prov, errorcodes.Errorf("inventory_conflict", "inventory conflict for id %s between %s:%d and %s:%d", d.ID, prior.Source.Path, prior.Source.Line, d.Source.Path, d.Source.Line)
				}
				seen[d.ID] = d
				all = append(all, d)
			}
		}
	}
	return all, prov, nil
}
func (l Loader) warning(s string) {
	if l.Warn != nil {
		l.Warn(s)
	}
}
func (l Loader) readFile(ctx context.Context, index int, sourceName, path string, src map[string]any, profiles map[string]transform.Sequence) ([]inventory.Device, inventory.SourceRef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, inventory.SourceRef{}, errorcodes.Errorf("inventory_source_unreadable", "inventory source %s: %w", sourceName, err)
	}
	sum := sha256.Sum256(data)
	ref := inventory.SourceRef{Name: sourceName, Path: path, Digest: hex.EncodeToString(sum[:])}
	f, err := os.Open(path)
	if err != nil {
		return nil, ref, errorcodes.Errorf("inventory_source_unreadable", "inventory source %s: %w", sourceName, err)
	}
	defer f.Close()
	mode, _ := src["mode"].(string)
	if mode == "" {
		mode = "header"
	}
	delim := ','
	if s, _ := src["delimiter"].(string); s != "" {
		r := []rune(s)
		if len(r) != 1 {
			return nil, ref, errorcodes.Errorf("config_inventory_source_delimiter_invalid", "inventory source %s delimiter must be one rune", sourceName)
		}
		delim = r[0]
	}
	aliases := map[string][]string{}
	numeric := map[string]int{}
	fields := append([]string(nil), logicalFields...)
	for _, field := range fields {
		aliases[field] = append([]string{field, strings.ReplaceAll(field, "_", "-")}, defaultAliases[field]...)
		if v, ok := src["mappings."+field]; ok {
			switch x := v.(type) {
			case string:
				aliases[field] = append([]string{x}, aliases[field]...)
			case []any:
				for _, e := range x {
					if s, ok := e.(string); ok {
						aliases[field] = append(aliases[field], s)
					}
				}
			case int64:
				numeric[field] = int(x)
			}
		}
	}
	for k, v := range src {
		if strings.HasPrefix(k, "mappings.attributes.") {
			logical := "attributes." + strings.TrimPrefix(k, "mappings.attributes.")
			switch x := v.(type) {
			case string:
				aliases[logical] = []string{x}
			case []any:
				for _, e := range x {
					if s, ok := e.(string); ok {
						aliases[logical] = append(aliases[logical], s)
					}
				}
			}
			fields = appendUnique(fields, logical)
		}
	}
	mandatory := toStrings(src["mandatory-fields"])
	if len(mandatory) == 0 {
		mandatory = []string{"name"}
	}
	requested := append([]string(nil), fields...)
	it, err := (tabular.CSVReader{}).Read(ctx, f, tabular.Spec{Delimiter: delim, Mode: mode, CommentPrefix: '#', Requested: requested, Aliases: aliases, Mandatory: mandatory, NumericMappings: numeric, Trim: true, MaxPhysicalLineBytes: l.Config.Int("tabular.max-physical-line-bytes"), Warn: func(s string) { l.warning(path + ": " + s) }, CheckHeader: func(column int, header string) error { return secretColumn(path, column, header) }})
	if err != nil {
		return nil, ref, fmt.Errorf("inventory source %s: %w", sourceName, err)
	}
	out := []inventory.Device{}
	for it.Next() {
		row := it.Record()
		d, err := rowDevice(row.Values, ref, row.Line, src, profiles)
		if err != nil {
			return nil, ref, fmt.Errorf("%s:%d: %w", path, row.Line, err)
		}
		out = append(out, d)
	}
	if err := it.Err(); err != nil {
		return nil, ref, fmt.Errorf("%s: %w", path, err)
	}
	return out, ref, nil
}

// secretColumn refuses an inventory file that carries a secret column: any
// header, mapped or not, whose name equals or ends with a listed word
// (inventory.SecretColumnWord). It is a hard failure with no override; the
// remedy is to move the secrets into a credential CSV and delete the column.
// The message names the file, the column's number, and the word that
// matched, and never the header's own text or a cell: a file without a
// header row has its first row read as the header, and a first row of a file
// that holds passwords is exactly where a password would be printed. The
// word stays although in that case it is the password's
// own tail: it is one of six public words, and for a real header it is what
// tells the operator why the column was caught. The caller adds the
// source's name.
func secretColumn(path string, column int, header string) error {
	word, secret := inventory.SecretColumnWord(header)
	if !secret {
		return nil
	}
	return errorcodes.Errorf("inventory_secret_column", "%s: the header of column %d names a secret (it is or ends with %q); an inventory file holds no secret column: move the secrets to a credential CSV (a credential backend of type \"csv\") and delete the column", path, column, word)
}

func rowDevice(v map[string]string, ref inventory.SourceRef, line int, src map[string]any, profiles map[string]transform.Sequence) (inventory.Device, error) {
	name := v["name"]
	profile := v["name_transform"]
	if profile == "" {
		profile, _ = src["name-transform"].(string)
	}
	if profile == "" {
		profile = "default"
	}
	seq, ok := profiles[profile]
	if !ok {
		return inventory.Device{}, errorcodes.Errorf("inventory_name_transform_unknown", "unknown name transform %q", profile)
	}
	canonical, _, err := seq.Apply(name)
	if err != nil {
		return inventory.Device{}, err
	}
	d := inventory.Device{SchemaVersion: 1, ID: v["id"], Name: name, CanonicalName: canonical, Platform: platform.Normalize(v["platform"]), Transport: strings.ToLower(v["transport"]), AddressAuthority: strings.ToLower(strings.TrimSpace(v["address_authority"])), Site: v["site"], Groups: splitList(v["groups"]), NameTransform: profile, RiskTier: v["risk_tier"], DeploymentRing: v["deployment_ring"], TopologyDomain: v["topology_domain"], Attributes: map[string]string{}, Source: ref}
	d.Source.Line = line
	if d.AddressAuthority == "" {
		if value, ok := src["defaults.address-authority"].(string); ok {
			d.AddressAuthority = strings.ToLower(strings.TrimSpace(value))
		}
	}
	if d.Platform == "" {
		if value, ok := src["defaults.platform"].(string); ok {
			d.Platform = platform.Normalize(value) // a source default is a name too
		}
	}
	// A row without a platform and without a source default stays blank:
	// not set. The
	// planner resolves it once, at planning (planner.ResolvePlatform); the
	// run selector and the maps never match a blank platform.
	if d.Transport == "" {
		if value, ok := src["defaults.transport"].(string); ok {
			d.Transport = strings.ToLower(value)
		}
	} else {
		d.TransportExplicit = true
	}
	if d.Transport != "" {
		d.TransportExplicit = true
	}
	if d.Transport == "" {
		d.Transport = "native"
	}
	if p := v["port"]; p != "" {
		n, e := strconv.ParseUint(p, 10, 16)
		if e != nil || n < 1 {
			return d, errorcodes.Errorf("inventory_port_invalid", "invalid port %q", p)
		}
		d.Port = uint16(n)
	}
	if c := v["session_cap"]; c != "" {
		n, e := strconv.Atoi(c)
		if e != nil {
			return d, errorcodes.Errorf("inventory_session_cap_invalid", "invalid session_cap %q", c)
		}
		d.SessionCap = &n
	}
	if a := v["management_address"]; a != "" {
		addr, e := netip.ParseAddr(a)
		if e != nil {
			return d, errorcodes.Errorf("inventory_management_address_invalid", "invalid management_address %q", a)
		}
		d.ManagementAddress = addr.Unmap()
		d.Addresses = []inventory.Address{{Address: d.ManagementAddress, Role: inventory.RoleManagement, Source: "inventory"}}
	}
	if list := strings.TrimSpace(v["addresses"]); list != "" {
		if !d.ManagementAddress.IsValid() {
			return d, errorcodes.Errorf("inventory_alternate_without_management", "addresses %q given without a management_address", list)
		}
		seen := map[netip.Addr]bool{d.ManagementAddress: true}
		for _, token := range splitList(list) {
			addr, e := netip.ParseAddr(token)
			if e != nil {
				return d, errorcodes.Errorf("inventory_alternate_address_invalid", "invalid alternate address %q", token)
			}
			addr = addr.Unmap()
			if seen[addr] {
				continue
			}
			seen[addr] = true
			d.Addresses = append(d.Addresses, inventory.Address{Address: addr, Role: inventory.RoleAlternate, Source: "inventory", Preference: len(d.Addresses)})
		}
	}
	// credkeyref pins the device to one credential row. A value must be a
	// legal key, the same literal rule the credential CSV's credkey column
	// obeys (matching.CheckKey): no pattern character, no leading "!". A
	// blank cell is no pin. The message names the column and the fault and
	// never the cell (a misplaced delimiter can put anything in a column);
	// the caller adds the file and the line.
	if ref := strings.TrimSpace(v["credkeyref"]); ref != "" {
		if reason := matching.CheckKey(ref); reason != "" {
			return d, errorcodes.Errorf("inventory_credkeyref_invalid", "column credkeyref: %s", reason)
		}
		d.CredKeyRef = ref
	}
	for k, val := range v {
		if strings.HasPrefix(k, "attributes.") && val != "" {
			d.Attributes[strings.TrimPrefix(k, "attributes.")] = val
		}
	}
	if err := d.Validate(); err != nil {
		return d, err
	}
	return d, nil
}
func buildTransforms(cfg configload.Snapshot) (map[string]transform.Sequence, error) {
	out := map[string]transform.Sequence{"default": {}}
	for name, t := range cfg.NamedTables("name-transform") {
		raw, ok := t["operations"].([]any)
		if !ok {
			return nil, errorcodes.Errorf("name_transform_operations_not_array", "name-transform.%s.operations must be an array", name)
		}
		items := []map[string]any{}
		for _, v := range raw {
			m, ok := v.(map[string]any)
			if !ok {
				return nil, errorcodes.Errorf("name_transform_operation_not_table", "name-transform.%s operation must be an inline table", name)
			}
			items = append(items, m)
		}
		seq, err := transform.FromMaps(items)
		if err != nil {
			return nil, fmt.Errorf("name-transform.%s: %w", name, err)
		}
		out[name] = seq
	}
	return out, nil
}
func sameDevice(a, b inventory.Device) bool {
	return a.ID == b.ID && a.CanonicalName == b.CanonicalName && a.ManagementAddress == b.ManagementAddress && a.Platform == b.Platform && a.Transport == b.Transport && a.Port == b.Port && a.Site == b.Site && strings.Join(a.Groups, "\x00") == strings.Join(b.Groups, "\x00") &&
		a.AddressAuthority == b.AddressAuthority && sameAddrs(a.Alternates(), b.Alternates()) &&
		// Two rows for one device that pin different keys are a conflict, not
		// a duplicate to coalesce: which credential the device took would
		// depend on file order. Keys compare folded.
		matching.FoldKey(a.CredKeyRef) == matching.FoldKey(b.CredKeyRef)
}

func sameAddrs(a, b []netip.Addr) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func splitList(s string) []string {
	f := func(r rune) bool { return r == ';' || r == '|' || r == ',' || r == ' ' || r == '\t' }
	raw := strings.FieldsFunc(s, f)
	seen := map[string]bool{}
	out := []string{}
	for _, v := range raw {
		if v != "" && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
func toStrings(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		out := []string{}
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
func appendUnique(a []string, s string) []string {
	for _, x := range a {
		if x == s {
			return a
		}
	}
	return append(a, s)
}

func stringMap(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
