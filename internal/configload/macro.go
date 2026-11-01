package configload

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
)

var macroRefRE = regexp.MustCompile(`%([A-Za-z][A-Za-z0-9_-]{0,63})%`)

// operatorMacro is the built-in macro, %operator%: the operator's login name
// from the password database, filled at the load in every value of every
// layer as a site's macro is, but kept as written in the keys a backend fills
// when it is asked (operatorKept). A [macros] table may not define it.
const operatorMacro = "operator"

// operatorTemplates are the credential backend keys that keep %operator%:
// an env backend's three variable templates, Redis's key-template, Vault's
// path-template, and a formula's username-template. The backend fills each
// with the operator's name after its operator transform.
var operatorTemplates = []string{"username-var-template", "password-var-template", "enable-var-template", "key-template", "path-template", "username-template"}

// operatorKept reports whether key keeps %operator% as written: a backend's
// template or a policy's username-template. The key being filled decides,
// so a site's macro holding %operator% keeps it there too.
func operatorKept(key string) bool {
	parts := strings.SplitN(key, ".", 3)
	if len(parts) != 3 || parts[1] == "" {
		return false
	}
	switch parts[0] {
	case "credential-backend":
		return oneOfWords(parts[2], operatorTemplates)
	case "credential-policy":
		return parts[2] == "username-template"
	}
	return false
}

func (l *loader) expandMacros() error {
	macros := map[string]any{}
	for k, v := range l.snap.Values {
		if name, ok := strings.CutPrefix(k, "macros."); ok {
			if name == operatorMacro {
				return newError("config_macro_name_reserved", "%operator% built in (the operator's login name)", k, v.Source, nil)
			}
			macros[name] = clone(v.Data)
		}
	}
	fill, keep := l.macroResolver(macros, false), l.macroResolver(macros, true)
	keys := make([]string, 0, len(l.snap.Values))
	for k := range l.snap.Values {
		if !strings.HasPrefix(k, "macros.") {
			keys = append(keys, k)
		}
	}
	// In sorted order, so a configuration with two faults always names the
	// same one.
	sort.Strings(keys)
	for _, k := range keys {
		v := l.snap.Values[k]
		resolve := fill
		if operatorKept(k) {
			resolve = keep
		}
		expanded, trace, err := expandAny(v.Data, resolve)
		if err != nil {
			var me *macroError
			if errors.As(err, &me) {
				return newError(me.code, me.msg, k, v.Source, nil)
			}
			return err
		}
		// A value is stored as the word its alias stands for (Canonical),
		// whether it was written out or arrived through a macro.
		v.Data = configschema.Canonical(k, expanded)
		v.MacroTrace = dedup(trace)
		if err := l.lockedMacros(k, v.MacroTrace); err != nil {
			return err
		}
		l.snap.Values[k] = v
	}
	return nil
}

// authorityLayer is the layer Read reads with authority: the global graph,
// which alone declares locks and alone writes a locked key.
const authorityLayer = "global"

// lockedMacros refuses a locked key's value that takes a macro (trace, the
// macros it takes, those they take included) whose winning definition comes
// from a layer below the global graph, which would change the locked value
// through the macro. %operator%, the operator's name, is no layer's.
func (l *loader) lockedMacros(key string, trace []string) error {
	if len(trace) == 0 {
		return nil
	}
	lock, _ := winningLock(l.snap.Locks, key)
	if lock == nil {
		return nil
	}
	for _, name := range trace {
		if name == operatorMacro {
			continue
		}
		def := l.snap.Values["macros."+name]
		if def.Source.Layer == authorityLayer {
			continue
		}
		return newError("config_lock_violation", fmt.Sprintf("%s takes %%%s%%: write blocked by lock %q declared at %s", key, name, lock.Pattern, lock.Source.String()), "macros."+name, def.Source, nil)
	}
	return nil
}

// macroResolver resolves a macro by its name, each site macro expanded once
// and held with the macros it takes, so every key taking it has the same
// trace: one resolver fills %operator% with the operator's name, the other
// (kept) leaves it as written, so a macro's held value is the one its key
// takes.
func (l *loader) macroResolver(macros map[string]any, kept bool) func(string) (any, []string, error) {
	type held struct {
		value any
		trace []string
	}
	resolved := map[string]held{}
	active := map[string]bool{}
	var resolve func(string) (any, []string, error)
	resolve = func(name string) (any, []string, error) {
		if name == operatorMacro {
			switch {
			case kept:
				return "%" + operatorMacro + "%", nil, nil
			case l.operator == "":
				return nil, nil, &macroError{code: "operator_identity_unavailable", msg: "%operator%: the operator's name unknown"}
			}
			return l.operator, []string{operatorMacro}, nil
		}
		if h, ok := resolved[name]; ok {
			return clone(h.value), append([]string(nil), h.trace...), nil
		}
		raw, ok := macros[name]
		if !ok {
			return nil, nil, &macroError{code: "config_macro_undefined", msg: fmt.Sprintf("undefined macro %%%s%%", name)}
		}
		if active[name] {
			return nil, nil, &macroError{code: "config_macro_cycle", msg: fmt.Sprintf("macro cycle involving %%%s%%", name)}
		}
		active[name] = true
		defer delete(active, name)
		v, trace, err := expandAny(raw, resolve)
		if err != nil {
			return nil, nil, err
		}
		trace = append([]string{name}, trace...)
		resolved[name] = held{value: clone(v), trace: trace}
		return v, append([]string(nil), trace...), nil
	}
	return resolve
}

// macroError carries the rule-specific code of a macro expansion failure.
type macroError struct{ code, msg string }

func (e *macroError) Error() string { return e.msg }

func expandAny(v any, resolve func(string) (any, []string, error)) (any, []string, error) {
	switch x := v.(type) {
	case string:
		return expandString(x, resolve)
	case []any:
		out := []any{}
		var traces []string
		for _, e := range x {
			ev, tr, err := expandAny(e, resolve)
			if err != nil {
				return nil, nil, err
			}
			traces = append(traces, tr...)
			if arr, ok := ev.([]any); ok {
				out = append(out, arr...)
			} else {
				out = append(out, ev)
			}
		}
		return out, traces, nil
	case map[string]any:
		out := map[string]any{}
		var traces []string
		for k, e := range x {
			ev, tr, err := expandAny(e, resolve)
			if err != nil {
				return nil, nil, err
			}
			out[k] = ev
			traces = append(traces, tr...)
		}
		return out, traces, nil
	default:
		return x, nil, nil
	}
}
func expandString(s string, resolve func(string) (any, []string, error)) (any, []string, error) {
	matches := macroRefRE.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s, nil, nil
	}
	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(s) {
		name := s[matches[0][2]:matches[0][3]]
		return resolve(name)
	}
	var b strings.Builder
	last := 0
	var traces []string
	for _, m := range matches {
		name := s[m[2]:m[3]]
		v, tr, err := resolve(name)
		if err != nil {
			return nil, nil, err
		}
		switch v.(type) {
		case []any, map[string]any:
			return nil, nil, &macroError{code: "config_macro_scalar_type", msg: fmt.Sprintf("list/object macro %%%s%% used in scalar text", name)}
		}
		b.WriteString(s[last:m[0]])
		b.WriteString(scalarText(v))
		last = m[1]
		traces = append(traces, tr...)
	}
	b.WriteString(s[last:])
	return b.String(), traces, nil
}
func scalarText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(x)
	}
}
func dedup(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
