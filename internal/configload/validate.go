package configload

import (
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/sshalgorithms"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
)

func (l *loader) validate() error {
	keys := make([]string, 0, len(l.snap.Values))
	for k := range l.snap.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// Removed dispatch.order values get their own codes naming the replacement
	// instead of the generic enum code.
	if v, ok := l.snap.Values["dispatch.order"]; ok {
		switch v.Data {
		case "inventory":
			return newError("config_dispatch_order_inventory_removed", "`inventory` is removed; use `default`, which keeps the assembled target order", "dispatch.order", v.Source, nil)
		case "random-seeded":
			return newError("config_dispatch_order_random_seeded_removed", "`random-seeded` is removed; use `shuffle` with `dispatch.shuffle-key`", "dispatch.order", v.Source, nil)
		}
	}
	for _, k := range keys {
		v := l.snap.Values[k]
		if err := configschema.ValidateScalar(k, v.Data); err != nil {
			code := "config_type_error"
			var enumErr *configschema.EnumValueError
			if errors.As(err, &enumErr) {
				code = "config_enum_value_invalid"
			}
			return newError(code, err.Error(), k, v.Source, err)
		}
		if e, ok := configschema.Lookup(k); ok && e.Kind == configschema.Duration {
			d, err := time.ParseDuration(v.Data.(string))
			if err != nil {
				return newError("config_duration_error", "invalid Go duration", k, v.Source, err)
			}
			if d < 0 {
				return newError("config_duration_negative", "duration cannot be negative", k, v.Source, nil)
			}
		}
	}
	// Every documented range, from the registry rows (ranges.go); the
	// relational rules below assume their operands are in range.
	if err := l.validateRanges(); err != nil {
		return err
	}
	if l.snap.Int("config.schema-version") != configschema.ConfigSchemaVersion {
		return l.semantic("config_schema_version", "config.schema-version", fmt.Sprintf("must equal %d", configschema.ConfigSchemaVersion))
	}
	if err := l.validateTransportSyntax(); err != nil {
		return err
	}
	if !l.snap.Bool("ssh.pubkey-authentication") && !l.snap.Bool("ssh.password-authentication") && !l.snap.Bool("ssh.keyboard-interactive-authentication") {
		return l.semantic("config_ssh_auth_mechanisms_disabled", "ssh.password-authentication", "at least one SSH authentication mechanism must be enabled")
	}
	if l.snap.Bool("network.ping-targets") && !l.snap.Bool("network.ping-socket") && !l.snap.Bool("network.ping-system") {
		return l.semantic("config_ping_methods_disabled", "network.ping-targets", "network.ping-socket and network.ping-system are both false; at least one ICMP method must remain when pinging is enabled")
	}
	if l.snap.Int64("output.max-command-bytes") > l.snap.Int64("output.max-job-bytes") {
		return l.semantic("config_output_max_job_below_max_command", "output.max-job-bytes", "must be at least max command bytes")
	}
	if !l.snap.Bool("audit.enabled") {
		return l.semantic("config_audit_disabled", "audit.enabled", "audit.enabled must remain true")
	}
	if l.snap.Bool("audit.file-required") && l.snap.String("audit.file") == "" {
		return l.semantic("config_audit_file_required_missing", "audit.file", "must be set when audit.file-required=true")
	}
	if l.snap.Bool("logging.file-required") && l.snap.String("logging.file") == "" {
		return l.semantic("config_logging_file_required_missing", "logging.file", "must be set when logging.file-required=true")
	}
	if l.snap.Duration("dispatch.admission-poll-min") > l.snap.Duration("dispatch.admission-poll-max") {
		return l.semantic("config_dispatch_admission_poll_min_exceeds_max", "dispatch.admission-poll-min", "must not exceed dispatch.admission-poll-max")
	}
	ws, wm := l.snap.Int("dispatch.wave-start-width"), l.snap.Int("dispatch.wave-max-width")
	if ws > 0 && wm > 0 && ws > wm {
		return l.semantic("config_dispatch_wave_max_below_start", "dispatch.wave-max-width", "must be at least wave start width")
	}
	if wm > l.snap.Int("dispatch.absolute-max-width") {
		return l.semantic("config_dispatch_wave_max_exceeds_absolute", "dispatch.wave-max-width", "exceeds absolute maximum")
	}
	if l.snap.Duration("watch.stale-after") < 2*l.snap.Duration("watch.refresh") {
		return l.semantic("config_watch_stale_after_too_short", "watch.stale-after", "must be at least twice watch.refresh")
	}
	formatter, err := display.NewFormatter(l.snap.String("display.timestamp"), l.snap.String("timezone"))
	if err != nil {
		// The formatter checks the zone too; an unknown zone is the
		// timezone key's, at its own source.
		key := "display.timestamp"
		if errorcodes.Of(err) == "display_timezone_invalid" {
			key = "timezone"
		}
		return l.semanticErr("config_display_timestamp_invalid", key, err)
	}
	_ = formatter
	for _, key := range []string{"display.login.header", "display.login.footer", "display.command.header", "display.command.footer", "display.run.header", "display.run.footer", "display.collection.footer", "display.ping.header"} {
		if err := display.ValidateLineTemplate(l.snap.String(key)); err != nil {
			return l.semanticErr("config_display_template_invalid", key, err)
		}
	}
	for _, key := range []string{"display.command.border", "display.run.border"} {
		if err := display.ValidateBorder(l.snap.String(key)); err != nil {
			return l.semanticErr("config_display_border_invalid", key, err)
		}
	}
	if err := l.validateLocks(); err != nil {
		return err
	}
	if err := l.validateDynamic(); err != nil {
		return err
	}
	return nil
}

// semantic reports a fixed-key validation failure. Every rule passes its own
// code so each cause is distinguishable.
func (l *loader) semantic(code, key, msg string) error {
	v := l.snap.Values[key]
	return newError(code, msg, key, v.Source, nil)
}

// semanticErr reports a fixed-key failure under the specific code err carries,
// or under fallback when err has none.
func (l *loader) semanticErr(fallback, key string, err error) error {
	code := errorcodes.Of(err)
	if code == "" {
		code = fallback
	}
	return l.semantic(code, key, strings.TrimPrefix(errorcodes.Message(err), code+": "))
}

var transportNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func (l *loader) validateTransportSyntax() error {
	for _, mode := range []string{"login", "command", "run"} {
		key := "ssh." + mode + ".transport"
		value := strings.ToLower(strings.TrimSpace(l.snap.String(key)))
		if value == "default" || value == "preferred" || value == "telnet" || transportNamePattern.MatchString(value) {
			continue
		}
		return l.semantic("config_transport_selector_invalid", key, "must be default, preferred, telnet, or a lowercase named transport selector")
	}
	for name, raw := range l.snap.Prefix("ssh.transports") {
		if !transportNamePattern.MatchString(name) {
			return l.dynamicErr("config_transport_slot_name_invalid", "ssh.transports."+name, "transport slot name must match [a-z][a-z0-9_-]{0,63}")
		}
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return l.dynamicErr("config_transport_mapping_empty", "ssh.transports."+name, "transport mapping must be a nonempty string")
		}
		for _, r := range value {
			if r < 0x20 || r == 0x7f {
				return l.dynamicErr("config_transport_mapping_control_character", "ssh.transports."+name, "transport mapping contains a control character")
			}
		}
	}
	return nil
}

func (l *loader) validateLocks() error {
	concrete := make([]string, 0, len(l.snap.Values))
	for k := range l.snap.Values {
		if !strings.HasPrefix(k, "config-lock.") {
			concrete = append(concrete, k)
		}
	}
	for _, decl := range l.snap.Locks {
		covered := false
		for _, k := range concrete {
			if lockMatches(decl.Pattern, k) {
				covered = true
				break
			}
		}
		if !covered {
			return newError("config_lock_zero_coverage", "lock pattern matches no configured or registered key", decl.Pattern, decl.Source, nil)
		}
	}
	for _, k := range concrete {
		if _, err := winningLock(l.snap.Locks, k); err != nil {
			return newError("config_lock_equal_specificity", err.Error(), k, SourceRef{}, err)
		}
	}
	return nil
}
func (l *loader) validateDynamic() error {
	backends := l.snap.NamedTables("credential-backend")
	for name, b := range backends {
		typ, _ := b["type"].(string)
		if typ == "" {
			return l.dynamicErr("config_credential_backend_type_missing", "credential-backend."+name+".type", "backend type is required")
		}
		if !contains([]string{"env", "sqlite", "cloginrc", "csv", "redis", "vault", "formula"}, typ) {
			return l.dynamicErr("config_credential_backend_type_unsupported", "credential-backend."+name+".type", "unsupported backend type")
		}
		// File backends (cloginrc and csv) declare scope and required; other
		// types reject both keys.
		fileBackend := typ == "cloginrc" || typ == "csv"
		scopeRaw, hasScope := b["scope"]
		requiredRaw, hasRequired := b["required"]
		if !fileBackend {
			if hasScope {
				return l.dynamicErr("config_credential_backend_scope_unsupported", "credential-backend."+name+".scope", "scope applies to file backends only")
			}
			if hasRequired {
				return l.dynamicErr("config_credential_backend_scope_unsupported", "credential-backend."+name+".required", "required applies to file backends only")
			}
		}
		// The reader keys belong to csv alone. The
		// keys are visited in sorted order so the same configuration always
		// names the same key.
		if typ != "csv" {
			for _, key := range sortedKeys(b) {
				if configschema.CredentialCSVKey(key) {
					return l.dynamicErr("config_credential_backend_key_unsupported", "credential-backend."+name+"."+key, key+" applies to the csv backend type only")
				}
			}
		}
		// A csv backend declares its scope: the cloginrc default to user
		// exists only so an older configuration keeps working, and a new
		// type has none.
		if typ == "csv" && !hasScope {
			// A missing key has no source of its own; the report names
			// where the table is, as the session-init map's missing
			// profile does.
			return newError("config_credential_backend_scope_missing", "a csv credential backend must declare scope = \"user\" or \"shared\"", "credential-backend."+name+".scope", l.tableSource("credential-backend."+name, b), nil)
		}
		// A csv backend has no default path. Checked before
		// the shared-scope path rule, so a shared table with no path says
		// the path is missing, not that it is relative.
		if typ == "csv" {
			if raw, ok := b["path"]; ok {
				if _, isString := raw.(string); !isString {
					return l.dynamicErr("config_type_error", "credential-backend."+name+".path", "must be a string")
				}
			}
			if path, _ := b["path"].(string); strings.TrimSpace(path) == "" {
				return newError("config_credential_backend_path_missing", "a csv credential backend needs a path", "credential-backend."+name+".path", l.tableSource("credential-backend."+name, b), nil)
			}
		}
		scope := "user"
		if hasScope {
			v, ok := scopeRaw.(string)
			if !ok || (v != "user" && v != "shared") {
				return l.dynamicErr("config_enum_value_invalid", "credential-backend."+name+".scope", "must be one of user, shared")
			}
			scope = v
		}
		if hasRequired {
			if _, ok := requiredRaw.(bool); !ok {
				return l.dynamicErr("config_type_error", "credential-backend."+name+".required", "must be a boolean")
			}
		}
		if scope == "shared" {
			if req, ok := requiredRaw.(bool); hasRequired && ok && !req {
				return l.dynamicErr("config_credential_backend_shared_optional", "credential-backend."+name+".required", "a shared credential file is always required")
			}
			if path, _ := b["path"].(string); !filepath.IsAbs(path) {
				return l.dynamicErr("config_credential_backend_shared_path_relative", "credential-backend."+name+".path", "a shared credential file needs an absolute path")
			}
		}
		if typ == "csv" {
			if err := l.validateCredentialCSV(name, b); err != nil {
				return err
			}
		}
		if typ == "formula" {
			src, _ := b["password-source"].(string)
			if src == "" || src == name {
				return l.dynamicErr("config_credential_formula_source_invalid", "credential-backend."+name+".password-source", "formula requires a distinct password-source backend")
			}
			if _, ok := backends[src]; !ok {
				return l.dynamicErr("config_credential_formula_source_unknown", "credential-backend."+name+".password-source", "referenced backend does not exist")
			}
		}
	}
	policies := l.snap.NamedTables("credential-policy")
	for name, p := range policies {
		for _, b := range toStrings(p["backend-sequence"]) {
			if _, ok := backends[b]; !ok {
				return l.dynamicErr("config_credential_policy_backend_unknown", "credential-policy."+name+".backend-sequence", "unknown backend "+b)
			}
		}
		if parent, _ := p["inherits"].(string); parent != "" {
			if _, ok := policies[parent]; !ok && parent != "default" {
				return l.dynamicErr("config_credential_policy_parent_unknown", "credential-policy."+name+".inherits", "unknown parent policy")
			}
		}
	}
	if err := validateInheritance(policies); err != nil {
		return newError("config_policy_cycle", err.Error(), "credential-policy", SourceRef{}, err)
	}
	maps := l.snap.IndexedTables("credential-policy-map")
	if len(maps) > 0 {
		last := maps[len(maps)-1]
		if !isCatchAll(last, "policy") {
			return l.dynamicErr("config_credential_policy_map_catch_all_missing", "credential-policy-map", "final credential policy rule must be only name=\"*\"")
		}
		for i, m := range maps {
			policy, _ := m["policy"].(string)
			if policy == "" {
				return l.dynamicErr("config_credential_policy_map_policy_missing", fmt.Sprintf("credential-policy-map.%d.policy", i), "policy is required")
			}
			if policy != "default" {
				if _, ok := policies[policy]; !ok {
					return l.dynamicErr("config_credential_policy_map_policy_unknown", fmt.Sprintf("credential-policy-map.%d.policy", i), "unknown policy")
				}
			}
			if code, err := validateRule(m); err != nil {
				return l.dynamicErr(code, fmt.Sprintf("credential-policy-map.%d", i), err.Error())
			}
		}
	}
	sessions := l.snap.NamedTables("session-init")
	if err := l.validateSessionInitProfiles(sessions); err != nil {
		return err
	}
	smaps := l.snap.IndexedTables("session-init-map")
	if len(smaps) > 0 {
		if !isCatchAll(smaps[len(smaps)-1], "profile") {
			return l.dynamicErr("config_session_init_map_catch_all_missing", "session-init-map", "final session-init rule must be only name=\"*\"")
		}
		for i, m := range smaps {
			raw, present := m["profile"]
			profile, isString := raw.(string)
			switch {
			case present && !isString:
				return l.dynamicErr("config_type_error", fmt.Sprintf("session-init-map.%d.profile", i), "must be a string")
			case profile == "":
				return newError("config_session_init_map_profile_missing", "profile is required", fmt.Sprintf("session-init-map.%d.profile", i), l.tableSource(fmt.Sprintf("session-init-map.%d", i), m), nil)
			}
			if _, ok := sessions[profile]; !ok {
				return l.dynamicErr("config_session_init_map_profile_unknown", fmt.Sprintf("session-init-map.%d.profile", i), "unknown session-init profile")
			}
			if code, err := validateRule(m); err != nil {
				return l.dynamicErr(code, fmt.Sprintf("session-init-map.%d", i), err.Error())
			}
		}
	}
	if err := l.validateSSHAlgorithms(); err != nil {
		return err
	}
	sources := l.snap.IndexedTables("inventory-source")
	names := map[string]bool{}
	for i, s := range sources {
		name, _ := s["name"].(string)
		if name == "" {
			return l.dynamicErr("config_inventory_source_name_missing", fmt.Sprintf("inventory-source.%d.name", i), "name is required")
		}
		if names[name] {
			return l.dynamicErr("config_inventory_source_name_duplicate", fmt.Sprintf("inventory-source.%d.name", i), "duplicate inventory source name")
		}
		names[name] = true
		if typ, _ := s["type"].(string); typ != "csv" {
			return l.dynamicErr("config_inventory_source_type_unsupported", fmt.Sprintf("inventory-source.%d.type", i), "the current release supports only csv")
		}
		if p, _ := s["path"].(string); p == "" {
			return l.dynamicErr("config_inventory_source_path_missing", fmt.Sprintf("inventory-source.%d.path", i), "path is required")
		}
		if d, _ := s["delimiter"].(string); d != "" && len([]rune(d)) != 1 {
			return l.dynamicErr("config_inventory_source_delimiter_invalid", fmt.Sprintf("inventory-source.%d.delimiter", i), "delimiter must be one rune")
		}
		// An inventory file holds no secret column. A header-mode file's
		// headers are checked when the file is read (inventory_secret_column);
		// what validation can see without opening a file is an attribute
		// mapping's name, the one name a numeric-mode file has, and in header
		// mode the one way a secret could arrive under an innocent header
		// (mappings.attributes.password = "pw"). Attributes travel in the
		// plan and the records, so the name is refused in both modes. The
		// check reads the name and never a cell: a secret under an innocent
		// attribute name is not found. The keys are checked in sorted order
		// so one configuration fails one way.
		keys := make([]string, 0, len(s))
		for k := range s {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			attribute, ok := strings.CutPrefix(k, "mappings.attributes.")
			if !ok {
				continue
			}
			if word, secret := inventory.SecretColumnWord(attribute); secret {
				return l.dynamicErr("config_inventory_source_secret_mapping", fmt.Sprintf("inventory-source.%d.%s", i, k), fmt.Sprintf("inventory source %s: the attribute %q names a secret (it is or ends with %q); an inventory file holds no secret column: keep secrets in a credential CSV (a credential backend of type \"csv\")", name, attribute, word))
			}
		}
	}
	if err := l.validatePlatformTables(); err != nil {
		return err
	}
	return l.validatePlatformResolution(sources)
}

// validateCredentialCSV checks a csv backend's reader keys without opening
// the file: the mode enum, the
// delimiter's length, each mapping's type for the mode (a header name or a
// list of header names under header mode, a one-based column number under
// numeric mode), and that every mandatory field is a logical field. Whether
// the file has the mandatory columns is the reader's question at first use
// (credential_csv_malformed), not validation's.
func (l *loader) validateCredentialCSV(name string, b map[string]any) error {
	key := func(k string) string { return "credential-backend." + name + "." + k }
	mode := "header"
	if raw, ok := b["mode"]; ok {
		v, isString := raw.(string)
		if !isString {
			return l.dynamicErr("config_type_error", key("mode"), "must be a string")
		}
		if v != "header" && v != "numeric" {
			return l.dynamicErr("config_enum_value_invalid", key("mode"), "must be one of header, numeric")
		}
		mode = v
	}
	if raw, ok := b["delimiter"]; ok {
		v, isString := raw.(string)
		if !isString {
			return l.dynamicErr("config_type_error", key("delimiter"), "must be a string")
		}
		if len([]rune(v)) != 1 {
			return l.dynamicErr("config_credential_backend_delimiter_invalid", key("delimiter"), "delimiter must be one character")
		}
	}
	fields := configschema.CredentialCSVFields()
	for _, field := range fields {
		raw, ok := b["mappings."+field]
		if !ok {
			continue
		}
		k := key("mappings." + field)
		if mode == "numeric" {
			col, isInt := raw.(int64)
			if !isInt {
				return l.dynamicErr("config_type_error", k, "under mode = \"numeric\" a mapping is a column number")
			}
			if col < 1 {
				return l.dynamicErr("config_type_error", k, "a column number starts at 1")
			}
			continue
		}
		switch x := raw.(type) {
		case string:
			if strings.TrimSpace(x) == "" {
				return l.dynamicErr("config_type_error", k, "a header name must not be blank")
			}
		case []any:
			if len(x) == 0 {
				return l.dynamicErr("config_type_error", k, "a list of header names must not be empty")
			}
			for _, e := range x {
				if h, isString := e.(string); !isString || strings.TrimSpace(h) == "" {
					return l.dynamicErr("config_type_error", k, "must be a header name or a list of header names")
				}
			}
		default:
			return l.dynamicErr("config_type_error", k, "under mode = \"header\" a mapping is a header name or a list of header names")
		}
	}
	if raw, ok := b["mandatory-fields"]; ok {
		list, isList := raw.([]any)
		if !isList {
			return l.dynamicErr("config_type_error", key("mandatory-fields"), "must be a list of field names")
		}
		for _, e := range list {
			field, isString := e.(string)
			if !isString {
				return l.dynamicErr("config_type_error", key("mandatory-fields"), "must be a list of field names")
			}
			if !contains(fields, field) {
				return l.dynamicErr("config_enum_value_invalid", key("mandatory-fields"), fmt.Sprintf("%q is not a credential CSV field (fields: %s)", field, strings.Join(fields, ", ")))
			}
		}
	}
	return nil
}

// sortedKeys returns a table's keys in order, for a rule whose report must
// not depend on map iteration.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validatePlatformResolution checks the platform names configuration
// supplies, after the
// platform tables' pass so the known set is already valid, at karvi config
// validate and at load: platform-resolution.default when set and
// platform-resolution.unknown-fallback always (blank included: the fallback
// must name a platform), then each inventory source's defaults.platform
// when set. A configuration value is refused whatever on-unknown says;
// on-unknown governs inventory row data only, at planning.
func (l *loader) validatePlatformResolution(sources []map[string]any) error {
	tables := l.snap.NamedTables("platform")
	known := func(name string) bool { return platform.Known(name, tables) }
	unknown := func(value string) string {
		return fmt.Sprintf("%q is not a known platform (known: %s)", value, strings.Join(platform.KnownNames(tables), ", "))
	}
	if v := l.snap.String("platform-resolution.default"); strings.TrimSpace(v) != "" && !known(v) {
		return l.semantic("config_platform_resolution_unknown", "platform-resolution.default", unknown(v))
	}
	if v := l.snap.String("platform-resolution.unknown-fallback"); !known(v) {
		return l.semantic("config_platform_resolution_unknown", "platform-resolution.unknown-fallback", unknown(v))
	}
	for i, s := range sources {
		if v, _ := s["defaults.platform"].(string); strings.TrimSpace(v) != "" && !known(v) {
			name, _ := s["name"].(string)
			return l.dynamicErr("config_inventory_source_platform_unknown", fmt.Sprintf("inventory-source.%d.defaults.platform", i), fmt.Sprintf("inventory source %s: defaults.platform %s", name, unknown(v)))
		}
	}
	return nil
}

// validatePlatformTables checks every [platform.NAME] table, at karvi
// config validate and at load, in this order: the name (and two spellings
// of one name), the driver, the fields' ranges, then the resolved
// definition's own check (the default transport, the session cap, and the
// privileged level, a level of the base definition). The tables are
// visited in sorted order so the first fault reported is the same on every
// run.
func (l *loader) validatePlatformTables() error {
	tables := l.snap.NamedTables("platform")
	names := make([]string, 0, len(tables))
	for name := range tables {
		names = append(names, name)
	}
	sort.Strings(names)
	normalised := map[string]string{} // normalised name -> the spelling seen first
	for _, name := range names {
		p := tables[name]
		key := "platform." + name
		// A fault of the table as a whole, or of a key it lacks, is reported
		// at the table's first key, so the message carries a file and line.
		fields := make([]string, 0, len(p))
		for f := range p {
			fields = append(fields, f)
		}
		sort.Strings(fields)
		first := key
		if len(fields) > 0 {
			first = key + "." + fields[0]
		}
		tableErr := func(code, at, msg string) error {
			src := SourceRef{}
			if v, ok := l.snap.Values[at]; ok {
				src = v.Source
			} else if v, ok := l.snap.Values[first]; ok {
				src = v.Source
			}
			return newError(code, msg, at, src, nil)
		}
		norm := platform.Normalize(name)
		switch {
		case norm == "":
			return tableErr("config_platform_name_invalid", first, "the table name is blank")
		case matching.HasMeta(norm):
			return tableErr("config_platform_name_invalid", first, "a platform name holds no glob character (*, ?, [, \\): every platform name is also a literal selector")
		case strings.HasPrefix(norm, "!"):
			return tableErr("config_platform_name_invalid", first, "a platform name does not begin with \"!\", the selector's negation")
		}
		if seen, dup := normalised[norm]; dup {
			return tableErr("config_platform_name_invalid", first, fmt.Sprintf("names compare without regard to case and spaces: this table and [platform.%s] are one platform %q", seen, norm))
		}
		normalised[norm] = name
		driver, hasDriver := p["driver"].(string)
		driver = platform.Normalize(driver)
		if _, builtin := platform.Builtin(norm); builtin {
			if hasDriver && driver != "" && driver != norm {
				return l.dynamicErr("config_platform_driver_unknown", key+".driver", fmt.Sprintf("a built-in platform's table keeps its own definition: driver is absent or %q", norm))
			}
		} else {
			if _, ok := platform.Builtin(driver); !ok {
				what := "driver is required"
				if driver != "" {
					what = fmt.Sprintf("driver %q is not a built-in platform", driver)
				}
				return tableErr("config_platform_driver_unknown", key+".driver", what+": an alias names one of "+strings.Join(builtinNames(), ", ")+" (no alias of an alias)")
			}
		}
		if port := asInt(p["ssh-port"]); p["ssh-port"] != nil && (port < 1 || port > 65535) {
			return l.dynamicErr("config_platform_ssh_port_out_of_range", key+".ssh-port", "must be 1..65535")
		}
		if c := asInt(p["session-cap"]); p["session-cap"] != nil && (c < 1 || c > 32) {
			return l.dynamicErr("config_platform_session_cap_out_of_range", key+".session-cap", "must be 1..32")
		}
		if v, ok := p["channel"]; ok {
			if s, _ := v.(string); s != platform.ChannelShell && s != platform.ChannelExec {
				return l.dynamicErr("config_platform_channel_invalid", key+".channel", fmt.Sprintf("must be %q or %q", platform.ChannelShell, platform.ChannelExec))
			}
		}
		// The three string-array fields (crun-commands since registry 14,
		// crun-filters since registry 19).
		for _, field := range []string{"paging-commands", "crun-commands", "crun-filters"} {
			v, ok := p[field]
			if !ok {
				continue
			}
			list, isList := v.([]any)
			for _, item := range list {
				if _, isString := item.(string); !isString {
					isList = false
				}
			}
			if !isList {
				return l.dynamicErr("config_type_error", key+"."+field, "must be an array of strings")
			}
		}
		// Every crun-filters pattern compiles (12.19 rule 1), refused here
		// before any device is contacted.
		if v, ok := p["crun-filters"]; ok {
			patterns, _ := v.([]any)
			list := make([]string, 0, len(patterns))
			for _, item := range patterns {
				list = append(list, item.(string))
			}
			if _, i, err := platform.CompileFilters(list); err != nil {
				return l.dynamicErr("config_platform_crun_filter_invalid", key+".crun-filters", fmt.Sprintf("pattern %d %q does not compile: %v", i+1, list[i], err))
			}
		}
		def := platform.Resolve(norm, tables)
		if err := def.Validate(); err != nil {
			field := "default-transport"
			if errorcodes.Of(err) == "platform_privilege_level_unknown" {
				field = "privileged-level"
			}
			src := SourceRef{}
			if v, ok := l.snap.Values[key+"."+field]; ok {
				src = v.Source
			}
			return newError(errorcodes.Of(err), err.Error(), key+"."+field, src, err)
		}
	}
	return nil
}

func builtinNames() []string {
	out := []string{}
	for _, d := range platform.Builtins() {
		out = append(out, d.Name)
	}
	return out
}

// dynamicErr reports a dynamic-table validation failure with its rule code.
func (l *loader) dynamicErr(code, key, msg string) error {
	src := SourceRef{}
	if v, ok := l.snap.Values[key]; ok {
		src = v.Source
	}
	return newError(code, msg, key, src, nil)
}
func validateInheritance(policies map[string]map[string]any) error {
	active := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) error
	visit = func(n string) error {
		if done[n] {
			return nil
		}
		if active[n] {
			return fmt.Errorf("credential policy inheritance cycle at %s", n)
		}
		active[n] = true
		if p := policies[n]; p != nil {
			if parent, _ := p["inherits"].(string); parent != "" && parent != "default" {
				if err := visit(parent); err != nil {
					return err
				}
			}
		}
		delete(active, n)
		done[n] = true
		return nil
	}
	for n := range policies {
		if err := visit(n); err != nil {
			return err
		}
	}
	return nil
}

// validateSessionInitProfiles checks each [session-init.NAME] profile as
// it loads: commands present and an
// array of strings without a NUL ([] is a named no-op; blank commands are
// commands), on-error fail-device or continue, command-timeout a duration of
// 1s to 12h, and no profile named none, the plan's and the records' word
// for no profile.
func (l *loader) validateSessionInitProfiles(sessions map[string]map[string]any) error {
	names := make([]string, 0, len(sessions))
	for name := range sessions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := sessions[name]
		prefix := "session-init." + name
		if name == "none" {
			return newError("config_session_init_profile_name_reserved", "the name none is reserved: it records that no profile was selected", prefix, l.tableSource(prefix, p), nil)
		}
		raw, ok := p["commands"]
		if !ok {
			return newError("config_session_init_commands_missing", "commands is required; use [] for a profile that sends nothing", prefix+".commands", l.tableSource(prefix, p), nil)
		}
		var commands []string
		switch list := raw.(type) {
		case []string:
			commands = list
		case []any:
			for _, item := range list {
				c, isString := item.(string)
				if !isString {
					return l.dynamicErr("config_type_error", prefix+".commands", "must be an array of strings")
				}
				commands = append(commands, c)
			}
		default:
			return l.dynamicErr("config_type_error", prefix+".commands", "must be an array of strings")
		}
		for i, c := range commands {
			if strings.ContainsRune(c, 0) {
				return l.dynamicErr("config_session_init_command_invalid", prefix+".commands", fmt.Sprintf("command %d contains a NUL character", i+1))
			}
		}
		if raw, ok := p["on-error"]; ok {
			if v, isString := raw.(string); !isString || (v != "fail-device" && v != "continue") {
				return l.dynamicErr("config_enum_value_invalid", prefix+".on-error", "must be one of fail-device, continue")
			}
		}
		if raw, ok := p["command-timeout"]; ok {
			key := prefix + ".command-timeout"
			v, isString := raw.(string)
			if !isString {
				return l.dynamicErr("config_type_error", key, "must be a duration string")
			}
			d, err := time.ParseDuration(v)
			switch {
			case err != nil:
				return l.dynamicErr("config_duration_error", key, "invalid Go duration")
			case d < 0:
				return l.dynamicErr("config_duration_negative", key, "duration cannot be negative")
			case d < time.Second || d > 12*time.Hour:
				return l.dynamicErr("config_session_init_command_timeout_out_of_range", key, "must be between 1s and 12h")
			}
		}
	}
	return nil
}

// validateSSHAlgorithms checks the algorithm configuration: the global
// lists, each [ssh-algorithms-profile.NAME],
// and each [[ssh-algorithms-map]] rule (a profile that exists, the match keys,
// no catch-all).
func (l *loader) validateSSHAlgorithms() error {
	global := sshalgorithms.Lists{}
	for _, k := range sshalgorithms.Kinds {
		key := "ssh-algorithms." + string(k)
		global[k] = l.snap.Strings(key)
		if code, err := sshalgorithms.CheckList(k, global[k], sshalgorithms.Global); err != nil {
			return l.semantic(code, key, err.Error())
		}
	}
	profiles := l.snap.NamedTables("ssh-algorithms-profile")
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		prefix := "ssh-algorithms-profile." + name
		for field, raw := range profiles[name] {
			if _, isList := raw.([]any); !isList {
				if _, isStrings := raw.([]string); !isStrings {
					return l.dynamicErr("config_type_error", prefix+"."+field, "must be an array of strings")
				}
			}
		}
		if err := sshalgorithms.CheckProfile(profiles[name], global); err != nil {
			pe := err.(*sshalgorithms.ProfileError)
			if pe.Field == "" {
				return newError(pe.Code, pe.Error(), prefix, l.tableSource(prefix, profiles[name]), nil)
			}
			return l.dynamicErr(pe.Code, prefix+"."+pe.Field, pe.Error())
		}
	}
	for i, m := range l.snap.IndexedTables("ssh-algorithms-map") {
		rule := fmt.Sprintf("ssh-algorithms-map.%d", i)
		raw, present := m["profile"]
		profile, isString := raw.(string)
		switch {
		case present && !isString:
			return l.dynamicErr("config_type_error", rule+".profile", "must be a string")
		case profile == "":
			return newError("config_ssh_algorithms_map_profile_missing", "profile is required", rule+".profile", l.tableSource(rule, m), nil)
		}
		if _, ok := profiles[profile]; !ok {
			return l.dynamicErr("config_ssh_algorithms_map_profile_unknown", rule+".profile", "unknown ssh-algorithms profile")
		}
		if isCatchAll(m, "profile") {
			return l.dynamicErr("config_ssh_algorithms_map_catch_all", rule+".name", "a rule matching every device belongs in [ssh-algorithms]")
		}
		if code, err := validateRule(m); err != nil {
			return l.dynamicErr(code, rule, err.Error())
		}
	}
	return nil
}

// tableSource is the source of a table's first field in key order, so an
// error about the table or a field it lacks points at where it is written.
func (l *loader) tableSource(prefix string, table map[string]any) SourceRef {
	fields := make([]string, 0, len(table))
	for f := range table {
		fields = append(fields, f)
	}
	sort.Strings(fields)
	for _, f := range fields {
		if v, ok := l.snap.Values[prefix+"."+f]; ok {
			return v.Source
		}
	}
	return SourceRef{}
}

// validateRule checks one policy or session-init match rule and returns the
// code of the first failing check.
func validateRule(m map[string]any) (string, error) {
	keys := []string{"name", "address-cidr", "platform", "site", "device-group"}
	present := false
	for _, k := range keys {
		vals := toStrings(m[k])
		if len(vals) == 0 {
			continue
		}
		present = true
		positive := false
		for _, v := range vals {
			if !strings.HasPrefix(v, "!") {
				positive = true
			}
			if k == "address-cidr" {
				s := strings.TrimPrefix(v, "!")
				if _, err := netip.ParsePrefix(s); err != nil {
					return "config_match_rule_cidr_invalid", fmt.Errorf("invalid CIDR %q", s)
				}
			}
		}
		if !positive {
			return "config_match_rule_only_negated", fmt.Errorf("%s has only negated selectors", k)
		}
	}
	if !present {
		return "config_match_rule_empty", fmt.Errorf("at least one match key is required")
	}
	// Every name, platform, site, and device-group value compiles under the
	// shared selector grammar, so a malformed pattern is refused here rather
	// than never matching.
	if err := matching.CheckRule(m); err != nil {
		return "config_match_rule_pattern_invalid", err
	}
	return "", nil
}
func isCatchAll(m map[string]any, targetKey string) bool {
	if m == nil {
		return false
	}
	target, _ := m[targetKey].(string)
	if target == "" {
		return false
	}
	for k := range m {
		if k == targetKey || k == "name" {
			continue
		}
		return false
	}
	vals := toStrings(m["name"])
	return len(vals) == 1 && vals[0] == "*"
}
func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func ResolvePath(raw, home string) (string, error) {
	p, err := expandHome(raw, home)
	if err == nil {
		return filepath.Clean(p), nil
	}
	if strings.HasPrefix(raw, "~") {
		return "", err
	}
	return filepath.Clean(raw), nil
}
func parseIndex(s string) int { i, _ := strconv.Atoi(s); return i }
