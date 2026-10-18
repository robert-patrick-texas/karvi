// Package credentialbackend assembles concrete credential adapters and applies
// policy, transforms, fallback semantics, completeness checks, and tty prompts.
package credentialbackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	clog "github.com/robert-patrick-texas/karvi/internal/credentialbackend/cloginrc"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credcsv"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/credfile"
	envbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/env"
	"github.com/robert-patrick-texas/karvi/internal/credentialbackend/operatorkeys"
	redisbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/redis"
	vaultbackend "github.com/robert-patrick-texas/karvi/internal/credentialbackend/vault"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/secrets"
	"github.com/robert-patrick-texas/karvi/internal/termline"
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
	transforms     map[string]transform.Credential
	backendProfile map[string]string
	warn           func(string)
	input          InputProvider
	// keyRules are the operator's file rules for ssh.identities, judged
	// once (keysOnce) into keys.
	keyRules credfile.Rules
	keysOnce sync.Once
	keys     operatorkeys.Result
	// held are the operator-keyed backends' answers, one per backend and
	// operator name (ask).
	heldMu sync.Mutex
	held   map[heldKey]*heldAnswer
}

// heldKey names one operator-keyed answer: the backend asked and the
// operator's name after the transform the request carried.
type heldKey struct{ backend, operator string }

// heldAnswer is one backend's answer to one operator name, read once; mu is
// held through the read, so devices resolving beside the first wait for it.
type heldAnswer struct {
	mu     sync.Mutex
	read   bool
	result credentials.BackendResult
}
type credentialsBackend = credentials.Backend

// New builds the resolver over the configured backends with the process
// environment and terminal as its input provider (SetInput changes it).
func New(cfg configload.Snapshot, operator credentials.Operator, warn func(string)) (*Resolver, error) {
	r := &Resolver{cfg: cfg, backends: map[string]credentials.Backend{}, formulas: map[string]formulaSpec{}, backendProfile: map[string]string{}, warn: warn, input: TTYInput{}, keyRules: fileRules(cfg, operator, operatorkeys.Backend, "user", false)}
	profiles, err := transform.Credentials(cfg.NamedTables("credential-transform"))
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
		// Every default of a backend is set here, once: its Resolve reads
		// the backend and never writes it, so devices resolving in parallel
		// share it.
		switch typ {
		case "env":
			r.backends[name] = &envbackend.Backend{BackendName: name, UsernameTemplate: str(d, "username-var-template"), PasswordTemplate: str(d, "password-var-template"), EnableTemplate: str(d, "enable-var-template")}
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
			tls, err := tlsFiles(d, operator.Home)
			if err != nil {
				return nil, err
			}
			r.backends[name] = &redisbackend.Backend{BackendName: name, Address: str(d, "address"), Database: intVal(d, "database", 0), KeyTemplate: defaultString(str(d, "key-template"), "karvi:credential:"+credentials.OperatorMacro), TLS: boolVal(d, "tls", true), TLSVerify: boolVal(d, "tls-verify", true), CAFile: tls[0], ClientCertFile: tls[1], ClientKeyFile: tls[2], AuthUsernameEnv: str(d, "auth-username-env"), AuthPasswordEnv: str(d, "auth-password-env"), Timeout: durationVal(d, "connect-timeout", 2*time.Second)}
		case "vault":
			tls, err := tlsFiles(d, operator.Home)
			if err != nil {
				return nil, err
			}
			r.backends[name] = &vaultbackend.Backend{BackendName: name, Address: str(d, "address"), Namespace: str(d, "namespace"), Mount: defaultString(str(d, "mount"), "secret"), PathTemplate: defaultString(str(d, "path-template"), "karvi/"+credentials.OperatorMacro), TokenEnv: defaultString(str(d, "token-env"), "VAULT_TOKEN"), TLSVerify: boolVal(d, "tls-verify", true), CAFile: tls[0], ClientCertFile: tls[1], ClientKeyFile: tls[2], Timeout: durationVal(d, "connect-timeout", 5*time.Second), UsernameField: defaultString(str(d, "username-field"), "username"), PasswordField: defaultString(str(d, "password-field"), "password"), EnableField: defaultString(str(d, "enable-field"), "enable_password")}
		case "formula":
			r.formulas[name] = formulaSpec{Name: name, UsernameTemplate: str(d, "username-template"), PasswordSource: str(d, "password-source")}
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
	// A backend's credential has no key yet (the credential CSV's keyfile
	// is later work), so it needs a password.
	requirePassword := true
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
		resolved, err := r.finalize(result.Credential, name, p.Name, requiredEnable, requirePassword)
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
	// exclude the device, which the code's description names.
	if pinned {
		return credentials.Resolved{}, &Error{Code: "credkeyref_unresolved", Message: fmt.Sprintf("device %s: credkeyref %q: no keyed backend answered (asked: %s; skipped, unkeyed: %s)", device.CanonicalName, device.CredKeyRef, listOrNone(asked), listOrNone(skipped)), Policy: p.Name}
	}
	// What follows the policy's backends is the platform's fallback.
	return r.fallback(ctx, operator, device, p.Name, requiredEnable)
}

// fallback walks the platform's fallback in order once the policy's
// backends have not answered: netvars reads NETUSER, NETPASS, and
// NETENABLE into the fields it finds empty, and ends the walk when they
// make a whole credential; prompt asks the terminal for the fields still
// empty and ends the walk with whatever username it holds; keys is the
// operator's login name and usable keys, a whole credential or none. When
// nothing ends the walk, the failure is the last source's that found
// something (the fields incomplete, or no key usable), else that no
// username was supplied. The input provider serves the variables and the
// prompts; without one there is neither.
func (r *Resolver) fallback(ctx context.Context, operator credentials.Operator, device inventory.Device, policyName string, requiredEnable bool) (credentials.Resolved, error) {
	list := platformFallback(device, r.cfg)
	var username, password, enable string
	source := "builtin-env-fallback"
	fieldsCredential := func() credentials.Credential {
		return credentials.Credential{Material: secrets.NewMaterial(username, password, enable), Backend: source, Policy: policyName, MatchedOn: credentials.Match{Category: "operator", SafeValue: operator.Username, Source: source}}
	}
	// failure is the last source's that found something; incomplete marks
	// that it was netvars, whose fields make no whole credential, and
	// finalize then names the field missing.
	var failure error
	incomplete := false
	for _, word := range list {
		switch word {
		case platform.FallbackNetvars:
			if r.input == nil {
				continue
			}
			found := false
			for _, v := range []struct {
				name  string
				field *string
			}{{"NETUSER", &username}, {"NETPASS", &password}, {"NETENABLE", &enable}} {
				if value, ok := r.lookupEnv(v.name); ok {
					found = true
					if *v.field == "" {
						*v.field = value
					}
				}
			}
			if !found {
				continue
			}
			if username != "" && password != "" && (enable != "" || !requiredEnable) {
				return r.finalize(fieldsCredential(), source, policyName, requiredEnable, true)
			}
			// Incomplete: the prompt, when it follows, fills the rest, and
			// until a later source finds something of its own the fields
			// are the failure.
			if username+password+enable != "" {
				failure, incomplete = nil, true
			}
		case platform.FallbackPrompt:
			if r.input == nil {
				continue
			}
			if err := r.prompt(ctx, device, policyName, requiredEnable, &username, &password, &enable, &source); err != nil {
				return credentials.Resolved{}, err
			}
			if username != "" {
				return r.finalize(fieldsCredential(), source, policyName, requiredEnable, true)
			}
		case platform.FallbackKeys:
			if device.Transport == "telnet" {
				failure, incomplete = &Error{Code: "credential_password_missing", Message: "the target is reached over telnet, which takes no key, and the platform's fallback offers no password", Policy: policyName}, false
				continue
			}
			result := r.operatorKeys()
			if len(result.Keys) == 0 {
				failure, incomplete = &Error{Code: "credential_operator_keys_missing", Message: "no key of ssh.identities is usable; examined: " + listOrNone(result.Examined), Policy: policyName}, false
				continue
			}
			cred := credentials.Credential{Material: secrets.NewMaterial(operator.Username, "", ""), Backend: operatorkeys.Backend, Policy: policyName, MatchedOn: credentials.Match{Category: "operator", SafeValue: operator.Username, Source: operatorkeys.Backend}, Keys: append([]credentials.KeyRef(nil), result.Keys...)}
			resolved, err := r.finalize(cred, operatorkeys.Backend, policyName, requiredEnable, false)
			if err != nil {
				return credentials.Resolved{}, err
			}
			for _, skip := range result.Skipped {
				resolved.Notices = append(resolved.Notices, credentials.Notice{Code: "operator_key_skipped", Message: skippedMessage(skip), Details: map[string]string{"file": skip.Path, "reason": skip.Reason}})
			}
			return resolved, nil
		}
	}
	if incomplete && username != "" {
		return r.finalize(fieldsCredential(), source, policyName, requiredEnable, true)
	}
	if failure != nil {
		return credentials.Resolved{}, failure
	}
	return credentials.Resolved{}, &Error{Code: "credential_username_missing", Message: fmt.Sprintf("no backend answered and the platform's fallback (%s) supplied no username", listOrNone(list)), Policy: policyName}
}

// prompt asks the terminal for the fields still empty: the username when
// creds.prompt-for-username, the password, and the enable secret where the
// platform requires one, each under creds.interactive-prompt. A prompt is
// asked only for a field the device needs, so no job runs past one left
// unanswered: Ctrl-C (or a signal) at the prompt is
// credential_prompt_interrupted, an empty answer the field's missing code,
// at once and before any later prompt; Ctrl-D on an empty line, like a
// missing terminal, is credential_prompt_unavailable. An answer makes the
// source interactive-tty.
func (r *Resolver) prompt(ctx context.Context, device inventory.Device, policyName string, requiredEnable bool, username, password, enable, source *string) error {
	if !r.cfg.Bool("creds.interactive-prompt") {
		return nil
	}
	ask := func(field string, masked bool, into *string) error {
		req := PromptRequest{Field: field, Masked: masked, Target: device.CanonicalName}
		v, e := r.input.Prompt(ctx, req)
		switch {
		case errors.Is(e, termline.ErrInterrupt) || errors.Is(e, context.Canceled):
			return &Error{Code: "credential_prompt_interrupted", Message: fmt.Sprintf("interrupted at the %s prompt", strings.ToLower(req.Label())), Policy: policyName}
		case errors.Is(e, io.EOF):
			return &Error{Code: "credential_prompt_unavailable", Message: fmt.Sprintf("the %s prompt was ended by Ctrl-D or the terminal's end", strings.ToLower(req.Label())), Policy: policyName}
		case e != nil:
			return &Error{Code: "credential_prompt_unavailable", Message: e.Error(), Policy: policyName}
		case v == "":
			return &Error{Code: emptyAnswerCodes[field], Message: fmt.Sprintf("nothing was entered at the %s prompt", strings.ToLower(req.Label())), Policy: policyName}
		}
		*into, *source = v, "interactive-tty"
		return nil
	}
	if *username == "" && r.cfg.Bool("creds.prompt-for-username") {
		if err := ask(FieldUsername, false, username); err != nil {
			return err
		}
	}
	if *password == "" && r.cfg.Bool("creds.prompt-for-password") {
		if err := ask(FieldPassword, true, password); err != nil {
			return err
		}
	}
	if requiredEnable && *enable == "" && r.cfg.Bool("creds.prompt-for-password") {
		if err := ask(FieldEnablePassword, true, enable); err != nil {
			return err
		}
	}
	return nil
}

// operatorKeys judges ssh.identities once per resolver, at the first
// device whose fallback reaches the keys; every later device shares the
// judgement and its notices. Each skipped key is a warning line once, as a
// platform not set is, beside the notice on each device's first record.
func (r *Resolver) operatorKeys() operatorkeys.Result {
	r.keysOnce.Do(func() {
		r.keys = operatorkeys.Judge(r.keyRules, r.cfg.Strings("ssh.identities"))
		for _, skip := range r.keys.Skipped {
			if r.warn != nil {
				r.warn(fmt.Sprintf("operator_key_skipped: %s", skippedMessage(skip)))
			}
		}
	})
	return r.keys
}

// skippedMessage is a skipped key's notice text: the file and the reason,
// never the contents.
func skippedMessage(skip operatorkeys.Skip) string {
	return fmt.Sprintf("operator key %s skipped: %s", skip.Path, skip.Reason)
}

// platformFallback is the platform used's fallback, as requiresEnable reads
// requires-enable; a definition that leaves it unset is netvars then
// prompt.
func platformFallback(d inventory.Device, cfg configload.Snapshot) []string {
	name := d.PlatformUsed
	if name == "" {
		name = d.Platform
	}
	if name == "" {
		name = "generic"
	}
	list := platform.Resolve(name, cfg.NamedTables("platform")).Fallback
	if list == nil {
		return platform.DefaultFallback()
	}
	return list
}

// emptyAnswerCodes are the codes of a prompt answered with nothing, by
// field: the codes of the same field missing from every source.
var emptyAnswerCodes = map[string]string{
	FieldUsername:       "credential_username_missing",
	FieldPassword:       "credential_password_missing",
	FieldEnablePassword: "credential_enable_missing",
}

// keyed reports whether a sequence entry can honour a credential key
// (credentials.Keyed). A formula is never keyed, whatever its password
// source: its username comes from the template and the operator, not from
// the row the key names, so it would answer a pin with a credential the
// pinned row does not describe. A formula over a csv source stays a
// legitimate answer for the policy's unpinned devices: the row supplies the
// secrets, literal or through an env: cell, and the template the name.
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

// lookupEnv is the resolver's one lookup of a variable, its input
// provider's: the built-in fallback reads NETUSER, NETPASS, and NETENABLE
// through it, and every backend through its request, so a value the
// provider holds reaches each. Without a provider no variable is set.
func (r *Resolver) lookupEnv(name string) (string, bool) {
	if r.input == nil {
		return "", false
	}
	return r.input.LookupEnv(name)
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
	req := credentials.ResolveRequest{Operator: op, Device: d, Policy: p.Name, LookupEnv: r.lookupEnv}
	if f, ok := r.formulas[name]; ok {
		source, ok := r.backends[f.PasswordSource]
		if !ok {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "formula_source_invalid", Message: "formula password source is unavailable"}
		}
		result := r.ask(ctx, f.PasswordSource, source, req)
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
		username := req.Fill(tmpl)
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
	return r.ask(ctx, name, b, req)
}

// ask is backend name's answer to req. An operator-keyed backend (the env,
// Redis, and Vault types) asks the same of every device, the operator's name
// after its transform, so it is asked once per resolver for each name, once
// per invocation or per job of a stream, and its answer held: a credential,
// each device given its own copy, so that sealing one grant leaves the held
// answer whole; not found; or a failure, every device given it without
// asking again, so a dead store costs one timeout. Devices resolving in
// parallel wait for the one read, and a formula over the backend takes the
// same answer. A read the context ended is not held.
func (r *Resolver) ask(ctx context.Context, name string, b credentials.Backend, req credentials.ResolveRequest) credentials.BackendResult {
	if b.Mode() != credentials.OperatorKeyed {
		return b.Resolve(ctx, req)
	}
	key := heldKey{name, req.Operator.Username}
	r.heldMu.Lock()
	if r.held == nil {
		r.held = map[heldKey]*heldAnswer{}
	}
	h := r.held[key]
	if h == nil {
		h = &heldAnswer{}
		r.held[key] = h
	}
	r.heldMu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.read {
		result := b.Resolve(ctx, req)
		if ctx.Err() != nil && result.Outcome != credentials.Success {
			return result
		}
		h.read, h.result = true, result
	}
	result := h.result
	if result.Outcome == credentials.Success {
		material, err := copyMaterial(result.Credential.Material)
		if err != nil {
			return credentials.BackendResult{Outcome: credentials.Malformed, ErrorCode: "credential_material_error", Message: err.Error()}
		}
		result.Credential.Material = material
		result.Credential.Keys = append([]credentials.KeyRef(nil), result.Credential.Keys...)
		result.Credential.Policy = req.Policy
	}
	return result
}

// copyMaterial is a copy of m's fields, so destroying either leaves the
// other whole; a field m does not set is unset in the copy.
func copyMaterial(m credentials.Material) (credentials.Material, error) {
	if m == nil {
		return nil, errorcodes.Errorf("credential_material_missing", "credential material is nil")
	}
	out := &secrets.Material{Username: secrets.New(""), Password: secrets.New(""), EnablePassword: secrets.New("")}
	for _, f := range []struct {
		set  bool
		with func(func([]byte) error) error
		into **secrets.Value
	}{
		{m.UsernameSet(), m.WithUsername, &out.Username},
		{m.PasswordSet(), m.WithPassword, &out.Password},
		{m.EnablePasswordSet(), m.WithEnablePassword, &out.EnablePassword},
	} {
		if !f.set {
			continue
		}
		if err := f.with(func(v []byte) error { *f.into = secrets.NewBytes(v); return nil }); err != nil {
			out.Destroy()
			return nil, err
		}
	}
	return out, nil
}

func (r *Resolver) finalize(c credentials.Credential, backend, policyName string, requiredEnable, requirePassword bool) (credentials.Resolved, error) {
	username, password, enable, err := materialStrings(c.Material)
	if err != nil {
		return credentials.Resolved{}, err
	}
	profile := r.transforms[r.backendProfile[backend]]
	if backend == "builtin-env-fallback" || backend == "interactive-tty" || backend == operatorkeys.Backend {
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

// tlsFiles are a backend's ca-file, client-cert-file, and client-key-file,
// each set one resolved by osutil.ResolvePath, an unset one empty.
func tlsFiles(d map[string]any, home string) ([3]string, error) {
	var out [3]string
	for i, key := range []string{"ca-file", "client-cert-file", "client-key-file"} {
		p, err := osutil.ResolvePath(str(d, key), home)
		if err != nil {
			return out, err
		}
		out[i] = p
	}
	return out, nil
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
