package configload

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
)

// RenderReference emits deterministic TOML containing every fixed key and its
// documented default. It is generated from the registry, not a second schema.
// The rows are grouped by table: the top-level keys first, with no table
// header (a key after one would belong to that table), then each table once,
// in the order of its first row, with all its rows in registry order. TOML
// refuses a table opened twice, so a table whose rows lie apart in the
// registry is still opened once.
func RenderReference() string {
	var b strings.Builder
	var tables []string
	rows := map[string][]configschema.Entry{}
	for _, e := range configschema.Entries() {
		table := ""
		if i := strings.LastIndexByte(e.Path, '.'); i >= 0 {
			table = e.Path[:i]
		}
		if _, ok := rows[table]; !ok {
			tables = append(tables, table)
		}
		rows[table] = append(rows[table], e)
	}
	sort.SliceStable(tables, func(i, j int) bool { return tables[i] == "" && tables[j] != "" })
	for _, table := range tables {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		if table != "" {
			fmt.Fprintf(&b, "[%s]\n", table)
		}
		for _, e := range rows[table] {
			key := e.Path[strings.LastIndexByte(e.Path, '.')+1:]
			for _, line := range wrapComment(e.Documentation, 92) {
				fmt.Fprintf(&b, "# %s\n", line)
			}
			fmt.Fprintf(&b, "%s = %s\n", quoteKey(key), e.DefaultLiteral)
		}
	}
	b.WriteString("\n# Dynamic sections such as [[inventory-source]], [credential-backend.NAME],\n# [credential-policy.NAME], [[credential-policy-map]], [session-init.NAME],\n# [ssh-algorithms-profile.NAME], [[ssh-algorithms-map]], and [platform.NAME]\n# are shown in configs/example.toml.\n")
	b.WriteString("#\n# A [platform.NAME] table for a built-in name (generic, cisco_iosxe, cisco_iosxr,\n# cisco_nxos, juniper_junos, arista_eos, linux) overrides that platform's fields;\n# a table for any other name is an alias and needs driver = \"<built-in>\", inheriting\n# the whole built-in definition. Names compare without regard to case and are\n# recorded lowercase; a name holds no glob character and does not begin with \"!\".\n# Fields: driver, default-transport, ssh-port, telnet-port, privileged-level (a\n# level of the base), requires-enable, session-cap, control-master, paging-commands,\n# crun-commands (the collection list a crun sends when its command line names no\n# command), crun-filters (regular expressions whose matching output lines are\n# dropped from the collection file; an empty array turns the built-in list off).\n# A device whose platform is not set runs as platform-resolution.default, else\n# generic, with the notice platform_not_set, and is matched by no --select-platform\n# selector or map platform rule; an inventory row naming an unknown platform\n# refuses the activity (platform_unknown) unless platform-resolution.on-unknown =\n# \"warn\", when it runs as unknown-fallback with the notice platform_unknown_fallback\n# and is matched by neither.\n")
	b.WriteString("#\n# File credential sources declare scope and required, for example:\n#   [credential-backend.rancid]          # optional user file, mode 0600\n#   type = \"cloginrc\"\n#   scope = \"user\"\n#   required = false\n#   path = \"~/.cloginrc\"\n#   [credential-backend.shared-rancid]   # mandatory shared file, mode 0640\n#   type = \"cloginrc\"\n#   scope = \"shared\"\n#   required = true\n#   path = \"/etc/karvi/credentials/cloginrc\"\n")
	// The csv type beside the two .cloginrc shapes: scope and path have no
	// default for it.
	b.WriteString("#   [credential-backend.site-creds]      # a credential CSV: scope and path are required\n#   type = \"csv\"\n#   scope = \"user\"\n#   required = false\n#   path = \"~/.karvi/credentials.csv\"\n#   mode = \"header\"                      # or \"numeric\", with mappings as column numbers\n#   mandatory-fields = [\"username\"]\n#   mappings.device_name = [\"device\", \"hostname\"]\n# Rows match top to bottom, first match wins; the columns are device_name,\n# address_cidr, platform, site, device_group, credkey, username, password,\n# and enable_password. See configs/example.toml.\n")
	b.WriteString("#\n# The ICMP gate (network.ping-targets) sends its probes directly over Linux ping\n# sockets when the kernel grants them to the operator's group; set the sysctl\n#   net.ipv4.ping_group_range = 1000 9999    # the range your operators' groups need\n# to use the internal pinger. Without it karvi falls back to the system ping\n# binary, which works but opens a sub-process for every gated device.\n")
	return b.String()
}
func quoteKey(k string) string {
	for _, r := range k {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return strconv.Quote(k)
		}
	}
	return k
}
func wrapComment(s string, width int) []string {
	words := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(s, "`", ""), "\n", " "))
	if len(words) == 0 {
		return nil
	}
	out := []string{}
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+1+len(w) > width {
			out = append(out, line)
			line = w
		} else {
			line += " " + w
		}
	}
	return append(out, line)
}

// RenderFlatTOML emits the effective scalar tree as deterministic dotted-key
// assignments. Dotted form avoids introducing ordering ambiguity.
func (s Snapshot) RenderFlatTOML() string {
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		if strings.HasPrefix(k, "macros.") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "%s = %s\n", renderDottedKey(k), tomlValue(s.Values[k].Data))
	}
	return b.String()
}
func renderDottedKey(k string) string {
	parts := strings.Split(k, ".")
	for i := range parts {
		parts[i] = quoteKey(parts[i])
	}
	return strings.Join(parts, ".")
}
func tomlValue(v any) string {
	switch x := v.(type) {
	case string:
		return strconv.Quote(x)
	case bool:
		return strconv.FormatBool(x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []any:
		parts := make([]string, len(x))
		for i := range x {
			parts[i] = tomlValue(x[i])
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, quoteKey(k)+" = "+tomlValue(x[k]))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	default:
		data, _ := json.Marshal(x)
		return string(data)
	}
}

// Resolver names the path a key's value comes to on this host, for the keys
// whose place depends on what the host holds; ok is false for any other key.
type Resolver func(key string) (path string, ok bool, err error)

// Explain renders one or every key with source, default, overrides, lock, and
// macro trace, and the resolved path where resolve names one (nil names
// none). Sensitive keys are metadata-only.
func (s Snapshot) Explain(key string, resolve Resolver) string {
	keys := []string{}
	if key != "" {
		keys = []string{key}
	} else {
		for k := range s.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
	}
	var b bytes.Buffer
	for idx, k := range keys {
		v, ok := s.Values[k]
		if !ok {
			fmt.Fprintf(&b, "key: %s\nerror: not found\n", k)
			continue
		}
		if idx > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "key:        %s\nvalue:      %s\nsource:     %s\ndefault:    %s\n", k, tomlValue(v.Data), v.Source.String(), tomlValue(v.Default))
		if resolve != nil {
			if path, ok, err := resolve(k); ok && err != nil {
				fmt.Fprintf(&b, "resolved:   error: %v\n", err)
			} else if ok {
				fmt.Fprintf(&b, "resolved:   %s\n", path)
			}
		}
		if e, ok := configschema.Lookup(k); ok {
			fmt.Fprintf(&b, "environment: %s\nreload:      %s\nvalidation:  %s\n", e.Environment, e.ReloadClass, e.Documentation)
		}
		if v.Lock != nil {
			fmt.Fprintf(&b, "lock:        %s (%s)\n", v.Lock.Pattern, v.Lock.Source.String())
		} else {
			b.WriteString("lock:        none\n")
		}
		if len(v.MacroTrace) > 0 {
			fmt.Fprintf(&b, "macros:      %s\n", strings.Join(v.MacroTrace, ", "))
		}
		if len(v.Overridden) > 0 {
			parts := make([]string, len(v.Overridden))
			for i, x := range v.Overridden {
				parts[i] = x.String()
			}
			fmt.Fprintf(&b, "overridden:  %s\n", strings.Join(parts, ", "))
		}
	}
	return b.String()
}
