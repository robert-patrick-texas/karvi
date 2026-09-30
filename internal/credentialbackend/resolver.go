// Package credentialbackend assembles concrete credential adapters and applies
// policy, transforms, fallback semantics, completeness checks, and tty prompts.
package credentialbackend

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	clog "github.com/robert-patrick-texas/karvi/internal/credentialbackend/cloginrc"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credcsv"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	envbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/env"
	redisbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/redis"
	vaultbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/vault"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/platform"
	"github.com/robert-patrick-texas/karvi/transform"
)

type Error struct {
	Code, Message, Backend, Policy string
	Retryable                      bool
}

func (e *Error) Error() string {
	parts := []string{e.Code + ": " + e.Message}
	if e.Policy != "" {
		parts = append(parts, "policy="+e.Policy)
	}
	if e.Backend != "" {
		parts = append(parts, "backend="+e.Backend)
	}
	return strings.Join(parts, " ")
}

// ErrorCode returns the registered error code.
func (e *Error) ErrorCode() string { return e.Code }

type policy struct {
	Name             string
	Sequence         []string
	UsernameTemplate string
	Inherits         string
}
type formulaSpec struct{ Name, UsernameTemplate, PasswordSource string }
type Resolver struct {
	cfg            configload.Snapshot
	backends       map[string]credentials.Backend
	formulas       map[string]formulaSpec
	policies       map[string]policy
	rules          []map[string]any
	transforms     map[string]credentialTransform
	backendProfile map[string]string
	warn           func(string)
	input          InputProvider
}
type credentialTransform struct{ Operator, Username transform.Sequence }
type credentialsBackend = credentials.Backend

// New builds the resolver over the configured backends with the process
// environment and terminal as its input provider (SetInput changes it).
func New(cfg configload.Snapshot, operator credentials.Operator, warn func(string)) (*Resolver, error) {
	r := &Resolver{cfg: cfg, backends: map[string]credentials.Backend{}, formulas: map[string]formulaSpec{}, backendProfile: map[string]string{}, warn: warn, input: TTYInput{}}
	profiles, err := buildTransforms(cfg)
	if err != nil {
		return nil, err
	}
	r.transforms = profiles
	defs := cfg.NamedTables("credential-backend")
	names := make([]string, 0, len(defs))
	for n := range defs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		d := defs[name]
		typ, _ := d["type"].(string)
		profile, _ := d["transform"].(string)
		if profile == "" {
			if _, ok := profiles[name]; ok {
				profile = name
			} else {
				profile = "default"
			}
		}
		if _, ok := profiles[profile]; !ok {
			return nil, errorcodes.Errorf("credential_transform_unknown", "credential backend %s references unknown transform %s", name, profile)
		}
		r.backendProfile[name] = profile
		indU := boolVal(d, "env-indirection.username", false)
		indP := boolVal(d, "env-indirection.password", false)
		indE := boolVal(d, "env-indirection.enable-password", false)
		switch typ {
		case "env":
			r.backends[name] = &envbackend.Backend{BackendName: name, UsernameTemplate: str(d, "username-var-template"), PasswordTemplate: str(d, "password-var-template"), EnableTemplate: str(d, "enable-var-template"), UsernameIndirect: indU, PasswordIndirect: indP, EnableIndirect: indE}
		case "cloginrc":
			scope := defaultString(str(d, "scope"), "user")
			r.backends[name] = &clog.Backend{Path: defaultString(str(d, "path"), "~/.cloginrc"), Rules: fileRules(cfg, operator, name, scope, boolVal(d, "required", scope == "shared")), Warn: warn}
		case "csv":
			// A credential CSV is the second file backend:
			// the same rules value as cloginrc, the reader keys from the
			// validated table. scope and path have no default here;
			// validation requires both.
			scope := str(d, "scope")
			r.backends[name] = credcsv.New(fileRules(cfg, operator, name, scope, boolVal(d, "required", scope == "shared")), d, cfg.Int("tabular.max-physical-line-bytes"))
		case "redis":
			r.backends[name] = &redisbackend.Backend{BackendName: name, Address: str(d, "address"), Database: intVal(d, "database", 0), KeyTemplate: defaultString(str(d, "key-template"), "karvi:credential:%s"), TLS: boolVal(d, "tls", true), CAFile: pathExpand(str(d, "ca-file"), operator.Home), ClientCertFile: pathExpand(str(d, "client-cert-file"), operator.Home), ClientKeyFile: pathExpand(str(d, "client-key-file"), operator.Home), AuthUsernameEnv: str(d, "auth-username-env"), AuthPasswordEnv: str(d, "auth-password-env"), Timeout: durationVal(d, "connect-timeout", 2*time.Second), UsernameIndirect: indU, PasswordIndirect: indP, EnableIndirect: indE}
		case "vault":
			r.backends[name] = &vaultbackend.Backend{BackendName: name, Address: str(d, "address"), Namespace: str(d, "namespace"), Mount: defaultString(str(d, "mount"), "secret"), PathTemplate: str(d, "path-template"), TokenEnv: defaultString(str(d, "token-env"), "VAULT_TOKEN"), CAFile: pathExpand(str(d, "ca-file"), operator.Home), ClientCertFile: pathExpand(str(d, "client-cert-file"), operator.Home), ClientKeyFile: pathExpand(str(d, "client-key-file"), operator.Home), Timeout: durationVal(d, "connect-timeout", 5*time.Second), UsernameField: defaultString(str(d, "username-field"), "username"), PasswordField: defaultString(str(d, "password-field"), "password"), EnableField: defaultString(str(d, "enable-field"), "enable_password"), UsernameIndirect: indU, PasswordIndirect: indP, EnableIndirect: indE}
		case "formula":
			r.formulas[name] = formulaSpec{Name: name, UsernameTemplate: str(d, "username-template"), PasswordSource: str(d, "password-source")}
		case "sqlite":
			r.backends[name] = unsupported{name: name, code: "sqlite_backend_unavailable", message: "the current portable build has no SQLite driver; use env, cloginrc, Redis, Vault, or rebuild with a future SQLite adapter"}
		default:
			return nil, errorcodes.Errorf("config_credential_backend_type_unsupported", "unsupported credential backend type %q", typ)
		}
	}
	r.policies, err = buildPolicies(cfg)
	if err != nil {
		return nil, err
	}
	r.rules = cfg.IndexedTables("credential-policy-map")
	return r, nil
}

func (r *Resolver) Resolve(ctx context.Context, operator credentials.Operator, device inventory.Device) (credentials.Resolved, error) {
	p, err := r.selectPolicy(device)
	if err != nil {
		return credentials.Resolved{}, err
	}
	requiredEnable := requiresEnable(device, r.cfg)
	requirePassword := device.Transport == "telnet" || !r.cfg.Bool("ssh.pubkey-authentication")
	// A pin is a pin. A device with a credkeyref
	// walks its policy's sequence as any device does, but only backends that
	// can honour a key are asked; every other is skipped. A keyed backend
	// without the key answers NotFound and the walk goes on, still looking
	// for the key. A failure of a keyed backend (an unsafe file, a bad row)
	// stops the resolution as it does for an unpinned device, and the row
	// that holds the key is judged by the ordinary completeness rules.
	pinned := device.CredKeyRef != ""
	var asked, skipped []string
	for _, name := range p.Sequence {
		if pinned {
			if !r.keyed(name) {
				skipped = append(skipped, name)
				continue
			}
			asked = append(asked, name)
		}
		result := r.resolveBackend(ctx, name, p, operator, device)
		if result.Outcome == credentials.NotFound {
			continue
		}
		if result.Outcome != credentials.Success {
			return credentials.Resolved{}, &Error{Code: defaultString(result.ErrorCode, "credential_backend_error"), Message: result.Message, Backend: name, Policy: p.Name, Retryable: result.Retryable}
		}
		resolved, err := r.finalize(result.Credential, name, p.Name, requiredEnable, requirePassword, false)
		if err != nil {
			return credentials.Resolved{}, err
		}
		return resolved, nil
	}
	// The end of a pinned device's sequence is an error that names the device
	// and the key, before the built-in fallback and the prompt are reached: a
	// pinned device never falls to a general row, the environment, a prompt,
	// or any other catch-all, since a mistyped reference would then take a
	// general credential and produce failed logins, on some devices
	// lockouts, with nothing naming the cause. The key is an inventory cell
	// that passed the literal rule and inventory holds no secret column, so
	// it is quoted; the backends asked and skipped are listed so the
	// operator sees where the key was looked for. A keyed backend answers
	// NotFound both for an absent key and for a key whose row's other cells
	// exclude the device, so the message names both causes.
	if pinned {
		return credentials.Resolved{}, &Error{Code: "credkeyref_unresolved", Message: fmt.Sprintf("device %s: credkeyref %q is unresolved: no backend of the policy that can honour a key answered for it (asked: %s; skipped, cannot honour a key: %s); the key is absent, or its row's other selectors exclude the device; a pinned device takes no general credential", device.CanonicalName, device.CredKeyRef, listOrNone(asked), listOrNone(skipped)), Policy: p.Name}
	}
	// The built-in fallback reads the environment and prompts through the
	// input provider; without one there is neither.
	var username, password, enable string
	source := "builtin-env-fallback"
	if r.input != nil {
		var uok, pok, eok bool
		if username, uok, err = r.input.LookupEnv(ctx, "NETUSER"); err != nil {
			return credentials.Resolved{}, &Error{Code: "credential_prompt_unavailable", Message: err.Error(), Policy: p.Name}
		}
		if password, pok, err = r.input.LookupEnv(ctx, "NETPASS"); err != nil {
			return credentials.Resolved{}, &Error{Code: "credential_prompt_unavailable", Message: err.Error(), Policy: p.Name}
		}
		if enable, eok, err = r.input.LookupEnv(ctx, "NETENABLE"); err != nil {
			return credentials.Resolved{}, &Error{Code: "credential_prompt_unavailable", Message: err.Error(), Policy: p.Name}
		}
		if !uok && !pok && !eok {
			username, password, enable = "", "", ""
		}
		allowPrompt := r.cfg.Bool("creds.interactive-prompt")
		prompt := func(field string, masked bool) (string, error) {
			v, e := r.input.Prompt(ctx, PromptRequest{Field: field, Masked: masked, Target: device.CanonicalName})
			if e != nil {
				return "", &Error{Code: "credential_prompt_unavailable", Message: e.Error(), Policy: p.Name}
			}
			source = "interactive-tty"
			return v, nil
		}
		if username == "" && allowPrompt && r.cfg.Bool("creds.prompt-for-username") {
			if username, err = prompt(FieldUsername, false); err != nil {
				return credentials.Resolved{}, err
			}
		}
		if requirePassword && password == "" && allowPrompt && r.cfg.Bool("creds.prompt-for-password") {
			if password, err = prompt(FieldPassword, true); err != nil {
				return credentials.Resolved{}, err
			}
		}
		if requiredEnable && enable == "" && allowPrompt && r.cfg.Bool("creds.prompt-for-password") {
			if enable, err = prompt(FieldEnablePassword, true); err != nil {
				return credentials.Resolved{}, err
			}
		}
	}
	if username == "" {
		return credentials.Resolved{}, &Error{Code: "credential_username_missing", Message: "no backend, NETUSER value, or tty username is available", Policy: p.Name}
	}
	cred := credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: source, Policy: p.Name, MatchedOn: credentials.Match{Category: "operator", SafeValue: operator.Username, Source: source}, FieldSources: map[string]credentials.FieldSource{"username": {Backend: source, Path: source}, "password": {Backend: source, Path: source}, "enable_password": {Backend: source, Path: source}}}
	return r.finalize(cred, source, p.Name, requiredEnable, requirePassword, true)
}

// keyed reports whether a sequence entry can honour a credential key
// (credentials.Keyed). A formula is never keyed, whatever its password
// source: its username comes from the template and the operator, not from
// the row the key names, so it would answer a pin with a credential the
// pinned row does not describe. A formula over a csv source stays a
// legitimate answer for the policy's unpinned devices: the row supplies the
// secrets, literal or through env-indirection, and the template the name.
// An unknown name is not keyed either; for an unpinned device
// resolveBackend reports it.
func (r *Resolver) keyed(name string) bool {
	if _, formula := r.formulas[name]; formula {
		return false
	}
	k, ok := r.backends[name].(credentials.Keyed)
	return ok && k.HonoursCredKey()
}

// listOrNone joins backend names for a message.
func listOrNone(names []string) string {
	if len(names) == 0 {
		return "none"
	}
	return strings.Join(names, ", ")
}

func (r *Resolver) resolveBackend(ctx context.Context, name string, p policy, operator credentials.Operator, d inventory.Device) credentials.BackendResult {
	profile := r.transforms[r.backendProfile[name]]
	lookup := operator.Username
	if out, _, err := profile.Operator.Apply(lookup); err == nil {
		lookup = out
	} else {
		return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "credential_transform_error", Message: err.Error()}
	}
	op := operator
	op.Username = lookup
	req := credentials.ResolveRequest{Operator: op, Device: d, Policy: p.Name, UsernameTemplate: p.UsernameTemplate, Now: time.Now()}
	if f, ok := r.formulas[name]; ok {
		source, ok := r.backends[f.PasswordSource]
		if !ok {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "formula_source_invalid", Message: "formula password source is unavailable"}
		}
		result := source.Resolve(ctx, req)
		if result.Outcome != credentials.Success {
			return result
		}
		_, pass, enable, err := materialStrings(result.Credential.Material)
		if err != nil {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "credential_material_error", Message: err.Error()}
		}
		tmpl := p.UsernameTemplate
		if tmpl == "" {
			tmpl = f.UsernameTemplate
		}
		if tmpl == "" {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "formula_template_missing", Message: "formula username template is empty"}
		}
		username := fmt.Sprintf(tmpl, lookup)
		result.Credential.Material.Destroy()
		result.Credential.Material = secrets.NewMaterial(username, pass, enable)
		result.Credential.Backend = name
		result.Credential.Policy = p.Name
		result.Credential.MatchedOn = credentials.Match{Category: "formula", SafeValue: lookup, Source: f.PasswordSource}
		return result
	}
	b, ok := r.backends[name]
	if !ok {
		return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "credential_backend_unknown", Message: "backend is not registered"}
	}
	return b.Resolve(ctx, req)
}

func (r *Resolver) finalize(c credentials.Credential, backend, policyName string, requiredEnable, requirePassword, promptable bool) (credentials.Resolved, error) {
	username, password, enable, err := materialStrings(c.Material)
	if err != nil {
		return credentials.Resolved{}, err
	}
	profile := r.transforms[r.backendProfile[backend]]
	if backend == "builtin-env-fallback" || backend == "interactive-tty" {
		profile = r.transforms["default"]
	}
	if out, _, e := profile.Username.Apply(username); e != nil {
		return credentials.Resolved{}, &Error{Code: "credential_transform_error", Message: e.Error(), Backend: backend, Policy: policyName}
	} else {
		username = out
	}
	c.Material.Destroy()
	c.Material = secrets.NewMaterial(username, password, enable)
	if username == "" {
		c.Material.Destroy()
		return credentials.Resolved{}, &Error{Code: "credential_incomplete", Message: "username is required", Backend: backend, Policy: policyName}
	}
	if requirePassword && password == "" {
		c.Material.Destroy()
		return credentials.Resolved{}, &Error{Code: "credential_password_missing", Message: "password is required for the selected authentication policy", Backend: backend, Policy: policyName}
	}
	if requiredEnable && enable == "" {
		c.Material.Destroy()
		return credentials.Resolved{}, &Error{Code: "credential_enable_missing", Message: "enable password is required by the platform", Backend: backend, Policy: policyName}
	}
	_ = promptable
	c.Backend = backend
	c.Policy = policyName
	return credentials.Resolved{Credential: c, DeviceUsername: username}, nil
}
func materialStrings(m credentials.Material) (string, string, string, error) {
	if m == nil {
		return "", "", "", errorcodes.Errorf("credential_material_missing", "credential material is nil")
	}
	var u, p, e string
	var err error
	if m.UsernameSet() {
		err = m.WithUsername(func(v []byte) error { u = string(v); return nil })
	}
	if err == nil && m.PasswordSet() {
		err = m.WithPassword(func(v []byte) error { p = string(v); return nil })
	}
	if err == nil && m.EnablePasswordSet() {
		err = m.WithEnablePassword(func(v []byte) error { e = string(v); return nil })
	}
	return u, p, e, err
}

type unsupported struct{ name, code, message string }

func (u unsupported) Name() string                  { return u.name }
func (u unsupported) Mode() credentials.BackendMode { return credentials.OperatorKeyed }
func (u unsupported) Resolve(context.Context, credentials.ResolveRequest) credentials.BackendResult {
	return credentials.BackendResult{Outcome: credentials.Unavailable, ErrorCode: u.code, Message: u.message}
}

func buildTransforms(cfg configload.Snapshot) (map[string]credentialTransform, error) {
	out := map[string]credentialTransform{"default": {}}
	for name, t := range cfg.NamedTables("credential-transform") {
		ct := credentialTransform{}
		for _, field := range []string{"operator", "username"} {
			raw, _ := t[field].([]any)
			items := []map[string]any{}
			for _, v := range raw {
				m, ok := v.(map[string]any)
				if !ok {
					return nil, errorcodes.Errorf("credential_transform_operation_not_table", "credential-transform.%s.%s has a non-table operation", name, field)
				}
				items = append(items, m)
			}
			seq, err := transform.FromMaps(items)
			if err != nil {
				return nil, err
			}
			if field == "operator" {
				ct.Operator = seq
			} else {
				ct.Username = seq
			}
		}
		out[name] = ct
	}
	return out, nil
}
func buildPolicies(cfg configload.Snapshot) (map[string]policy, error) {
	raw := cfg.NamedTables("credential-policy")
	out := map[string]policy{"default": {Name: "default", Sequence: cfg.Strings("creds.backend-sequence")}}
	active := map[string]bool{}
	var build func(string) (policy, error)
	build = func(name string) (policy, error) {
		if p, ok := out[name]; ok && name != "default" {
			return p, nil
		}
		if active[name] {
			return policy{}, errorcodes.Errorf("config_policy_cycle", "credential policy cycle at %s", name)
		}
		active[name] = true
		defer delete(active, name)
		m, exists := raw[name]
		base := policy{Name: name}
		if name == "default" {
			base = out["default"]
		}
		if exists {
			if parent := str(m, "inherits"); parent != "" {
				pp, err := build(parent)
				if err != nil {
					return policy{}, err
				}
				base = pp
				base.Name = name
			}
			if _, ok := m["backend-sequence"]; ok {
				base.Sequence = toStrings(m["backend-sequence"])
			}
			if _, ok := m["username-template"]; ok {
				base.UsernameTemplate = str(m, "username-template")
			}
			base.Inherits = str(m, "inherits")
		}
		out[name] = base
		return base, nil
	}
	for name := range raw {
		if _, err := build(name); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (r *Resolver) selectPolicy(d inventory.Device) (policy, error) {
	if len(r.rules) == 0 {
		return r.policies["default"], nil
	}
	fields := matching.Fields{Name: d.CanonicalName, Address: d.ManagementAddress, Platform: d.Platform, Site: d.Site, Groups: d.Groups}
	index, err := matching.Select(r.rules, fields)
	var ambiguous *matching.AmbiguousError
	var ruleErr *matching.RuleError
	switch {
	case errors.As(err, &ambiguous):
		return policy{}, errorcodes.Errorf("credential_policy_prefix_ambiguous", "rules %s match %s with the same prefix length %d", r.cfg.RuleList("credential-policy-map", "policy", ambiguous.Indices), d.CanonicalName, ambiguous.Prefix)
	case errors.As(err, &ruleErr) && ruleErr.CIDR:
		return policy{}, errorcodes.Errorf("config_match_rule_cidr_invalid", "credential-policy-map: %v", ruleErr)
	case errors.As(err, &ruleErr):
		return policy{}, errorcodes.Errorf("config_match_rule_pattern_invalid", "credential-policy-map: %v", ruleErr)
	case err != nil:
		return policy{}, err
	case index < 0:
		return policy{}, errorcodes.Errorf("credential_policy_unmatched", "no credential policy matched %s", d.CanonicalName)
	}
	name := str(r.rules[index], "policy")
	p, exists := r.policies[name]
	if !exists && name == "default" {
		p, exists = r.policies["default"], true
	}
	if !exists {
		return policy{}, errorcodes.Errorf("config_credential_policy_map_policy_unknown", "unknown credential policy %s", name)
	}
	return p, nil
}

// requiresEnable is the platform used's requires-enable: an enable secret
// is optional unless the platform's table requires one. The platform used
// is the view's PlatformUsed, which the planner and login fill from their
// resolution;
// when it is empty (a caller outside planning) the set platform stands in,
// blank as generic.
func requiresEnable(d inventory.Device, cfg configload.Snapshot) bool {
	name := d.PlatformUsed
	if name == "" {
		name = d.Platform
	}
	if name == "" {
		name = "generic"
	}
	return platform.Resolve(name, cfg.NamedTables("platform")).RequiresEnable
}
func str(m map[string]any, k string) string { s, _ := m[k].(string); return s }
func intVal(m map[string]any, k string, d int) int {
	if v, ok := m[k].(int64); ok {
		return int(v)
	}
	return d
}
func boolVal(m map[string]any, k string, d bool) bool {
	if v, ok := m[k].(bool); ok {
		return v
	}
	return d
}
func durationVal(m map[string]any, k string, d time.Duration) time.Duration {
	if s, ok := m[k].(string); ok {
		if v, e := time.ParseDuration(s); e == nil {
			return v
		}
	}
	return d
}

// fileRules builds the shared file rules of a file backend (cloginrc, and
// csv when it is built) from the backend's declared scope and required and
// the security settings every credential file obeys.
func fileRules(cfg configload.Snapshot, operator credentials.Operator, name, scope string, required bool) credfile.Rules {
	return credfile.Rules{BackendName: name, Scope: credfile.Scope(scope), Required: required, Home: operator.Home, UID: operator.UID, SharedGroup: cfg.String("security.shared-group"), ApprovedAdminUsers: cfg.Strings("security.approved-admin-users"), AllowSymlink: cfg.Bool("security.allow-credential-symlinks")}
}

func defaultString(s, d string) string {
	if s == "" {
		return d
	}
	return s
}
func pathExpand(s, home string) string {
	if strings.HasPrefix(s, "~/") {
		return filepath.Join(home, s[2:])
	}
	return s
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
