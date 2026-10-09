// Package configschema is the public, declarative configuration registry. It is
// the single source used by the loader, schema generator, environment mapping,
// reference configuration, and `config show --explain`.
package configschema

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// RegistrySchemaVersion moves once per release whose keys, table fields, or
// a key's path mark or reload class change: 26 takes the top-level
// scoreboards in place of watch.directory, and
// sessions.shared-capacity-root's default auto; 27 removes logging.level,
// logging.file, and logging.file-required, read by nothing; 28 marks the
// path-valued keys and their words (Place, Words, a table's Places) and
// gives the daemon's own keys the reload class daemon-start.
const (
	RegistrySchemaVersion = 28
	ConfigSchemaVersion   = 6
)

type Kind string

const (
	String      Kind = "string"
	Boolean     Kind = "boolean"
	Integer     Kind = "integer"
	Number      Kind = "number"
	Duration    Kind = "duration"
	Enum        Kind = "enum"
	StringArray Kind = "string_array"
	ObjectArray Kind = "object_array"
)

type Entry struct {
	Path           string `json:"path"`
	Kind           Kind   `json:"kind"`
	DefaultLiteral string `json:"default_literal,omitempty"`
	Required       bool   `json:"required"`
	LockEligible   bool   `json:"lock_eligible"`
	Sensitive      bool   `json:"sensitive"`
	// ReloadClass is when a changed value takes effect: next-job, the
	// default, for the invocation that sets it or the job it submits;
	// daemon-start for a key a running daemon keeps from its start.
	ReloadClass string   `json:"reload_class"`
	Environment string   `json:"environment,omitempty"`
	CLIFlags    []string `json:"cli_flags"`
	EnumValues  []string `json:"enum,omitempty"`
	// Min and Max bound an integer, number, or duration row, inclusive, as
	// literals in the row's own syntax ("1s", "64", "1.0"); a leading ">" or
	// "<" makes that bound exclusive (">0"). Either may be empty. The loader
	// refuses a value outside them as config_value_out_of_range
	// (internal/configload/ranges.go); the reference and the schema artifact
	// carry them. ZeroDisables admits 0 outside the range, meaning off.
	Min          string `json:"min,omitempty"`
	Max          string `json:"max,omitempty"`
	ZeroDisables bool   `json:"zero_disables,omitempty"`
	// Place marks a key whose value is a path, or a list of paths, and
	// Words the values it may hold instead that are not paths. The loader
	// makes a marked value absolute at the end of the load (`~` the
	// operator's home, a relative path from the working directory) unless it
	// is empty or one of the words, so a value means the same place to every
	// process that reads it (internal/configload/places.go).
	Place         bool     `json:"place,omitempty"`
	Words         []string `json:"words,omitempty"`
	Documentation string   `json:"documentation"`
	Since         string   `json:"since"`
}

// DynamicTable is a table of named or indexed rows; Places names the row's
// fields whose value is a path, as a fixed entry's Place marks one.
type DynamicTable struct {
	Pattern      string          `json:"pattern"`
	Fields       map[string]Kind `json:"fields"`
	LockEligible bool            `json:"lock_eligible"`
	Places       []string        `json:"places,omitempty"`
}
type Artifact struct {
	RegistrySchemaVersion int                 `json:"registry_schema_version"`
	ConfigSchemaVersion   int                 `json:"config_schema_version"`
	Keys                  []Entry             `json:"keys"`
	DynamicTables         []DynamicTable      `json:"dynamic_tables"`
	SpecialSections       []string            `json:"special_sections"`
	Enums                 map[string][]string `json:"enums"`
	TransformOperations   []string            `json:"transform_operations"`
	ErrorCodes            []string            `json:"error_codes"`
}

// colorValues is what every display.colors.<role> key accepts: the eight
// ANSI colours, gray, default for the theme's palette, and orange, the
// 256-colour index 208.
var colorValues = []string{"default", "black", "red", "green", "yellow", "blue", "magenta", "cyan", "white", "gray", "orange"}

var enumValues = map[string][]string{
	"name.address-family-preference": {"ipv4", "ipv6"}, "name.resolver": {"system"},
	"name.default-address-authority": {"client", "daemon"},
	"ssh.host-key-policy":            {"accept-new", "secure", "insecure"},
	"dispatch.default":               {"serial", "parallel", "wave"}, "dispatch.order": {"default", "sorted", "name", "shuffle", "random"},
	"targets.empty-source": {"warn", "error"}, "platform-resolution.on-unknown": {"fail", "warn"}, "output.directory-mode": {"0700", "0750", "0770"},
	// The collection directory and its files: a
	// shared directory may be world-readable, which the job tree's enum has
	// no value for.
	"crun.directory-mode": {"0750", "0755", "0770"}, "crun.file-mode": {"0640", "0644", "0660"},
	"output.ansi": {"auto", "strip", "preserve"}, "transcript.on-write-error": {"terminate", "continue"},
	"transcript.format": {"text", "jsonl", "json"}, "transcript.metadata-format": {"text", "jsonl", "json"},
	"session-init.<name>.on-error": {"fail-device", "continue"}, "display.theme": {"auto", "dark", "light", "nocolor"},
	"display.color": {"auto", "always", "never"},
	// freecheck takes display.color's three words: the registry
	// has no "auto or a number" kind, and true/false exist only as the
	// boolean kind, so the three-way switch is this enum.
	"freecheck":                     {"auto", "always", "never"},
	"display.colors.success":        colorValues,
	"display.colors.warning":        colorValues,
	"display.colors.error":          colorValues,
	"display.colors.muted":          colorValues,
	"display.colors.accent":         colorValues,
	"display.colors.target":         colorValues,
	"display.colors.address":        colorValues,
	"display.colors.label":          colorValues,
	"display.colors.value":          colorValues,
	"display.colors.border":         colorValues,
	"display.colors.timestamp":      colorValues,
	"display.colors.dynamic-border": colorValues,
}

var dynamicTables = []DynamicTable{
	{Pattern: "ssh.transports.<name>", Fields: map[string]Kind{"value": String}, LockEligible: true},
	{Pattern: "macros.<name>", Fields: map[string]Kind{"value": String}, LockEligible: true},
	{Pattern: "name-transform.<name>", Fields: map[string]Kind{"operations": ObjectArray}, LockEligible: true},
	{Pattern: "credential-transform.<name>", Fields: map[string]Kind{"operator": ObjectArray, "username": ObjectArray}, LockEligible: true},
	{Pattern: "credential-backend.<name>", Fields: map[string]Kind{}, LockEligible: true, Places: []string{"path", "ca-file", "client-cert-file", "client-key-file"}},
	{Pattern: "credential-policy.<name>", Fields: map[string]Kind{"inherits": String, "backend-sequence": StringArray, "username-template": String}, LockEligible: true},
	{Pattern: "session-init.<name>", Fields: map[string]Kind{"commands": StringArray, "on-error": Enum, "command-timeout": Duration}, LockEligible: true},
	// paging-commands joined the fields at registry 11, no bump: a
	// dynamic-table field, not a builtin-layer key. crun-commands joined at
	// registry 14: the
	// platform's collection list, sent by a crun that names no command;
	// registry 25: channel in place of control-master, and fallback.
	{Pattern: "platform.<name>", Fields: map[string]Kind{"driver": String, "default-transport": Enum, "ssh-port": Integer, "telnet-port": Integer, "privileged-level": String, "requires-enable": Boolean, "legacy-class": Enum, "session-cap": Integer, "channel": Enum, "fallback": StringArray, "paging-commands": StringArray, "crun-commands": StringArray, "crun-filters": StringArray}, LockEligible: true},
	{Pattern: "inventory-source.<index>", Fields: map[string]Kind{}, LockEligible: true, Places: []string{"path"}},
	{Pattern: "credential-policy-map.<index>", Fields: map[string]Kind{}, LockEligible: true},
	{Pattern: "session-init-map.<index>", Fields: map[string]Kind{}, LockEligible: true},
	{Pattern: "ssh-algorithms-profile.<name>", Fields: map[string]Kind{"host-key": StringArray, "kex": StringArray, "ciphers": StringArray, "macs": StringArray, "host-key-append": StringArray, "kex-append": StringArray, "ciphers-append": StringArray, "macs-append": StringArray}, LockEligible: true},
	{Pattern: "ssh-algorithms-map.<index>", Fields: map[string]Kind{}, LockEligible: true},
}

func Entries() []Entry {
	out := make([]Entry, len(fixedEntries))
	copy(out, fixedEntries)
	for i := range out {
		if out[i].Since == "" {
			out[i].Since = "0.2.0"
		}
		if out[i].ReloadClass == "" {
			out[i].ReloadClass = "next-job"
		}
		out[i].EnumValues = append([]string(nil), enumValues[out[i].Path]...)
		out[i].Words = append([]string(nil), out[i].Words...)
	}
	return out
}

// Place reports whether key's value is a path, a fixed entry marked Place
// or a dynamic table's field named in its Places, and the words the value
// may hold instead.
func Place(key string) (words []string, ok bool) {
	for _, e := range fixedEntries {
		if e.Path == key {
			return e.Words, e.Place
		}
	}
	parts := strings.Split(key, ".")
	for _, t := range dynamicTables {
		head := strings.Split(t.Pattern, ".")
		n := len(head) - 1 // the fixed segments before the row's name
		if len(t.Places) == 0 || len(parts) < n+2 || strings.Join(parts[:n], ".") != strings.Join(head[:n], ".") {
			continue
		}
		return nil, oneOf(strings.Join(parts[n+1:], "."), t.Places...)
	}
	return nil, false
}

func Lookup(path string) (Entry, bool) {
	for _, e := range Entries() {
		if e.Path == path {
			return e, true
		}
	}
	return Entry{}, false
}
func EnvironmentIndex() map[string]string {
	m := map[string]string{}
	for _, e := range Entries() {
		m[e.Environment] = e.Path
	}
	return m
}

func ArtifactValue() Artifact {
	return Artifact{RegistrySchemaVersion: RegistrySchemaVersion, ConfigSchemaVersion: ConfigSchemaVersion, Keys: Entries(), DynamicTables: dynamicTables, SpecialSections: []string{"config-lock", "macros", "@include"}, Enums: enumValues, TransformOperations: []string{"lowercase", "uppercase", "strip-chars", "replace-chars", "replace-suffix", "strip-suffix", "crop-to-dot", "add-suffix"}, ErrorCodes: errorcodes.ActiveWithPrefix("config_")}
}

var macroName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
var transportAlias = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// IsKnownLeaf validates fixed and dynamic leaf paths. Array-of-table indices are
// represented as decimal path segments by the loader.
func IsKnownLeaf(path string) bool {
	if _, ok := Lookup(path); ok {
		return true
	}
	parts := strings.Split(path, ".")
	if len(parts) == 0 {
		return false
	}
	switch parts[0] {
	case "config-lock":
		return len(parts) >= 2
	case "macros":
		return len(parts) == 2 && macroName.MatchString(parts[1])
	case "ssh":
		return len(parts) == 3 && parts[1] == "transports" && transportAlias.MatchString(parts[2])
	case "name-transform":
		return len(parts) == 3 && parts[1] != "" && parts[2] == "operations"
	case "credential-transform":
		return len(parts) == 3 && parts[1] != "" && (parts[2] == "operator" || parts[2] == "username")
	case "credential-policy":
		return len(parts) == 3 && parts[1] != "" && oneOf(parts[2], "inherits", "backend-sequence", "username-template")
	case "session-init":
		return len(parts) == 3 && parts[1] != "" && oneOf(parts[2], "commands", "on-error", "command-timeout")
	case "platform":
		return len(parts) == 3 && parts[1] != "" && oneOf(parts[2], "driver", "default-transport", "ssh-port", "telnet-port", "privileged-level", "requires-enable", "legacy-class", "session-cap", "channel", "fallback", "paging-commands", "crun-commands", "crun-filters")
	case "inventory-source":
		return len(parts) >= 3 && isIndex(parts[1]) && inventoryField(strings.Join(parts[2:], "."))
	case "credential-policy-map":
		return len(parts) == 3 && isIndex(parts[1]) && oneOf(parts[2], "policy", "name", "address-cidr", "platform", "site", "device-group")
	case "session-init-map":
		return len(parts) == 3 && isIndex(parts[1]) && oneOf(parts[2], "profile", "name", "address-cidr", "platform", "site", "device-group")
	case "ssh-algorithms-profile":
		return len(parts) == 3 && parts[1] != "" && oneOf(parts[2], "host-key", "kex", "ciphers", "macs", "host-key-append", "kex-append", "ciphers-append", "macs-append")
	case "ssh-algorithms-map":
		return len(parts) == 3 && isIndex(parts[1]) && oneOf(parts[2], "profile", "name", "address-cidr", "platform", "site", "device-group")
	case "credential-backend":
		return len(parts) >= 3 && parts[1] != "" && backendField(strings.Join(parts[2:], "."))
	}
	return false
}
func isIndex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
func oneOf(s string, vals ...string) bool {
	for _, v := range vals {
		if s == v {
			return true
		}
	}
	return false
}
func inventoryField(s string) bool {
	// defaults.* are the source defaults the loader honors: the platform
	// and transport defaults, and the address authority; the table's other
	// listed keys stay refused until a loader reads them. mappings.credkeyref
	// is the pin column; a field of this dynamic table, so no registry
	// number moves.
	return oneOf(s, "name", "type", "path", "required", "mode", "delimiter", "comment-prefix", "mandatory-fields", "name-transform", "defaults.platform", "defaults.transport", "defaults.address-authority", "mappings.id", "mappings.name", "mappings.management_address", "mappings.addresses", "mappings.address_authority", "mappings.platform", "mappings.transport", "mappings.port", "mappings.site", "mappings.groups", "mappings.name_transform", "mappings.session_cap", "mappings.risk_tier", "mappings.deployment_ring", "mappings.topology_domain", "mappings.credkeyref") || strings.HasPrefix(s, "mappings.attributes.")
}

// credentialCSVFields are the nine logical fields of a credential CSV: the
// six selector columns in the row evaluator's order, then the three result
// columns. A
// field's name is its mapping key (mappings.device_name), its default
// header, and a legal entry of mandatory-fields.
var credentialCSVFields = []string{"device_name", "address_cidr", "platform", "site", "device_group", "credkey", "username", "password", "enable_password"}

// CredentialCSVFields returns the logical fields of a credential CSV, the
// one list the allow-list, validation, and the backend share.
func CredentialCSVFields() []string {
	return append([]string(nil), credentialCSVFields...)
}

// CredentialCSVKey reports whether a [credential-backend.NAME] key belongs
// to the csv type alone: the reader keys mode, delimiter, and
// mandatory-fields, and mappings.<field> for a logical field. Validation
// refuses these keys on every other type
// (config_credential_backend_key_unsupported). path, scope,
// and required are not csv-only: the file backends share them, and path
// also belongs to sqlite.
func CredentialCSVKey(s string) bool {
	if oneOf(s, "mode", "delimiter", "mandatory-fields") {
		return true
	}
	field, ok := strings.CutPrefix(s, "mappings.")
	return ok && oneOf(field, credentialCSVFields...)
}

// backendField is the allow-list of [credential-backend.NAME] keys. It
// admits every type's keys on every type; the rules that tie a key to a type
// live in configload's validation. A mapping for anything but a logical
// field is
// config_unknown_key: a credential CSV has no attribute columns.
func backendField(s string) bool {
	if oneOf(s, "type", "transform", "scope", "required", "env-indirection.username", "env-indirection.password", "env-indirection.enable-password", "username-var-template", "password-var-template", "enable-var-template", "path", "table", "operator-column", "username-column", "password-column", "enable-column", "busy-timeout", "address", "database", "key-template", "tls", "ca-file", "client-cert-file", "client-key-file", "auth-username-env", "auth-password-env", "connect-timeout", "namespace", "mount", "path-template", "token-env", "username-field", "password-field", "enable-field", "username-template", "password-source") {
		return true
	}
	return CredentialCSVKey(s)
}

func ValidateScalar(path string, v any) error {
	e, ok := Lookup(path)
	if !ok {
		return nil
	}
	switch e.Kind {
	case String, Duration, Enum:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("%s expects string", path)
		}
	case Boolean:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s expects boolean", path)
		}
	case Integer:
		if _, ok := v.(int64); !ok {
			return fmt.Errorf("%s expects integer", path)
		}
	case Number:
		switch v.(type) {
		case int64, float64:
		default:
			return fmt.Errorf("%s expects number", path)
		}
	case StringArray:
		a, ok := v.([]any)
		if !ok {
			return fmt.Errorf("%s expects array", path)
		}
		for _, x := range a {
			if _, ok := x.(string); !ok {
				return fmt.Errorf("%s expects an array of strings", path)
			}
		}
	}
	if vals := enumValues[path]; len(vals) > 0 {
		value, _ := v.(string)
		if !oneOf(value, vals...) {
			return &EnumValueError{Path: path, Allowed: vals}
		}
	}
	return nil
}

// EnumValueError reports a value of the correct type that is outside an
// enumerated key's allowed set, so callers can distinguish it from a type
// mismatch.
type EnumValueError struct {
	Path    string
	Allowed []string
}

func (e *EnumValueError) Error() string {
	return fmt.Sprintf("%s must be one of %s", e.Path, strings.Join(e.Allowed, ", "))
}

func SortedPaths() []string {
	p := make([]string, 0, len(fixedEntries))
	for _, e := range fixedEntries {
		p = append(p, e.Path)
	}
	sort.Strings(p)
	return p
}
