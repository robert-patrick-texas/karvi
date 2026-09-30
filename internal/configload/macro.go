package configload

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var macroRefRE = regexp.MustCompile(`%([A-Za-z][A-Za-z0-9_-]{0,63})%`)

func (l *loader) expandMacros() error {
	macros := map[string]any{}
	for k, v := range l.snap.Values {
		if strings.HasPrefix(k, "macros.") {
			macros[strings.TrimPrefix(k, "macros.")] = clone(v.Data)
		}
	}
	resolved := map[string]any{}
	active := map[string]bool{}
	var resolve func(string) (any, []string, error)
	resolve = func(name string) (any, []string, error) {
		if v, ok := resolved[name]; ok {
			return clone(v), []string{name}, nil
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
		resolved[name] = clone(v)
		return v, append([]string{name}, trace...), nil
	}
	keys := make([]string, 0, len(l.snap.Values))
	for k := range l.snap.Values {
		if !strings.HasPrefix(k, "macros.") {
			keys = append(keys, k)
		}
	}
	for _, k := range keys {
		v := l.snap.Values[k]
		expanded, trace, err := expandAny(v.Data, resolve)
		if err != nil {
			var me *macroError
			if errors.As(err, &me) {
				return newError(me.code, me.msg, k, v.Source, nil)
			}
			return err
		}
		v.Data = expanded
		v.MacroTrace = dedup(trace)
		l.snap.Values[k] = v
	}
	return nil
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
