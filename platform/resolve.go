package platform

import (
	"sort"
	"strings"
)

// Normalize is the one spelling of a platform name:
// trimmed and lowercase. Every place a name enters, an inventory row, a
// source default, --platform, a [platform.NAME] table, applies it, and the
// record's platform is the normalised name.
func Normalize(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

// Known reports whether name, normalised, is a known platform: a built-in
// definition name or the name of a configured [platform.NAME] table. The
// --platform check, the --select-platform check, table validation, and the
// planner's platform resolution all read it.
func Known(name string, tables map[string]map[string]any) bool {
	name = Normalize(name)
	if _, ok := Builtin(name); ok {
		return true
	}
	_, ok := tableFor(name, tables)
	return ok
}

// KnownNames lists the known platforms in the order messages and help show
// them: the built-ins in their documented order, then the configured table
// names sorted, each normalised and listed once.
func KnownNames(tables map[string]map[string]any) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, d := range Builtins() {
		out = append(out, d.Name)
		seen[d.Name] = true
	}
	extra := []string{}
	for key := range tables {
		name := Normalize(key)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		extra = append(extra, name)
	}
	sort.Strings(extra)
	return append(out, extra...)
}

// tableFor finds the [platform.NAME] table for a normalised name, whatever
// the table's spelling in the configuration. Two tables that normalise to
// the same name are a configuration fault (config_platform_name_invalid);
// until validation refuses them, the spelling that sorts first applies, so
// the choice is deterministic.
func tableFor(name string, tables map[string]map[string]any) (map[string]any, bool) {
	var chosen map[string]any
	chosenKey := ""
	for key, table := range tables {
		if Normalize(key) != name {
			continue
		}
		if chosen == nil || key < chosenKey {
			chosen, chosenKey = table, key
		}
	}
	return chosen, chosen != nil
}

// Resolve is the definition for a platform name under the configured
// [platform.NAME] tables: a built-in name is that built-in; a table whose driver names
// a built-in is an alias, that built-in's whole definition under the table's
// name; any other name is generic's definition under the name, a
// fall-through that no session reaches: the
// planner resolves every platform to a known name, and the executor and
// login refuse an unknown one through Lookup, so the
// port and enable readers that call Resolve on such a name are harmless.
// The name's table then overrides the allowed fields. The name and the table names are
// normalised before the lookup, so [platform.C9300] applies to a row c9300.
// Lookup is Resolve with the answer whether the name is known: a built-in
// or a configured table's name.
// An unknown name still returns generic's definition under the name, so a
// caller that must refuse checks the flag; the executor's definition step
// and login's do, as the backstop for a daemon whose configuration lacks
// the client's alias table.
func Lookup(name string, tables map[string]map[string]any) (Definition, bool) {
	return Resolve(name, tables), Known(name, tables)
}

func Resolve(name string, tables map[string]map[string]any) Definition {
	name = Normalize(name)
	table, _ := tableFor(name, tables)
	def, ok := Builtin(name)
	alias := false
	switch {
	case ok:
		def.Base = def.Name
	default:
		driver, _ := table["driver"].(string)
		if base, isBuiltin := Builtin(Normalize(driver)); isBuiltin {
			def = base
			alias = true
			def.Base = base.Name
			def.Driver = base.Name
		} else {
			def, _ = Builtin("generic")
			def.Base = "generic"
			def.Driver = name
		}
		def.Name = name
	}
	if table == nil {
		return def
	}
	// A built-in's table, or a name that is not an alias, keeps its
	// definition; a driver there changes only the label.
	if s, ok := table["driver"].(string); ok && s != "" && !alias {
		def.Driver = s
	}
	if s, ok := table["default-transport"].(string); ok && s != "" {
		def.DefaultTransport = s
	}
	if v, ok := asInt(table["ssh-port"]); ok && v > 0 {
		def.SSHPort = uint16(v)
	}
	if v, ok := asInt(table["telnet-port"]); ok && v > 0 {
		def.TelnetPort = uint16(v)
	}
	if s, ok := table["privileged-level"].(string); ok && s != "" {
		def.PrivilegedLevel = s
	}
	if v, ok := table["requires-enable"].(bool); ok {
		def.RequiresEnable = v
	}
	if v, ok := asInt(table["session-cap"]); ok {
		def.SessionCap = v
	}
	if v, ok := table["control-master"].(bool); ok {
		def.ControlMaster = v
	}
	if s, ok := table["legacy-class"].(string); ok {
		def.LegacyClass = s
	}
	if list, ok := stringList(table["paging-commands"]); ok {
		def.PagingCommands = list
	}
	if list, ok := stringList(table["crun-commands"]); ok {
		def.CrunCommands = list
	}
	// A table's crun-filters replaces the built-in list whole; an empty
	// array turns the filter off.
	if _, present := table["crun-filters"]; present {
		list, _ := stringList(table["crun-filters"])
		def.CrunFilters = list
	}
	return def
}

// stringList reads a table's string-array field: the strings of the array,
// in order, and whether the field was there at all (an empty array is a
// list of none, which clears a built-in's).
func stringList(v any) ([]string, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := []string{}
	for _, item := range list {
		if s, isString := item.(string); isString {
			out = append(out, s)
		}
	}
	return out, true
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	}
	return 0, false
}
