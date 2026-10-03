// Package configload assembles karvi configuration from built-ins, discovered
// files, explicit roots, environment variables, dedicated flags, and --set
// assignments. Every winning value retains provenance and every lower-authority
// write is checked against global locks before mutation.
package configload

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SourceRef identifies the exact origin of a configuration write.
type SourceRef struct {
	Layer string `json:"layer"`
	Path  string `json:"path"`
	Line  int    `json:"line,omitempty"`
}

func (s SourceRef) String() string {
	if s.Line > 0 {
		return fmt.Sprintf("%s:%d", s.Path, s.Line)
	}
	if s.Path != "" {
		return s.Path
	}
	return s.Layer
}

// Value is one fully-qualified configuration leaf and its provenance history.
type Value struct {
	Data       any         `json:"value"`
	Default    any         `json:"default,omitempty"`
	Source     SourceRef   `json:"source"`
	Overridden []SourceRef `json:"overridden,omitempty"`
	Lock       *LockDecl   `json:"lock,omitempty"`
	MacroTrace []string    `json:"macro_trace,omitempty"`
}

// Snapshot is immutable after Load returns. Callers receive defensive copies
// for collection values through accessors.
type Snapshot struct {
	Values       map[string]Value `json:"values"`
	Locks        []LockDecl       `json:"locks,omitempty"`
	Sources      []string         `json:"sources,omitempty"`
	Warnings     []string         `json:"warnings,omitempty"`
	Digest       string           `json:"digest"`
	LoadedAt     time.Time        `json:"loaded_at"`
	EffectiveCPU int              `json:"effective_cpu_count,omitempty"`
}

// FlagValue is one value an option sets for its key, and what set it: the
// option by its long name (--blind-wait), or the word that implies it
// (crun). The value's source names it, in every message and in config show.
type FlagValue struct {
	Value  any
	Option string
}

// Options describes non-file layers. FlagValues are already-associated
// registry keys; Sets preserve command-line order and use key=value grammar.
type Options struct {
	ExplicitRoots []string
	Environment   []string
	FlagValues    map[string]FlagValue
	Sets          []string
	HomeDir       string
	SkipAuto      bool
	InternalOnly  bool
}

// Error carries a stable configuration error code suitable for CLI mapping.
type Error struct {
	Code    string
	Message string
	Key     string
	Source  SourceRef
	Cause   error
}

func (e *Error) Error() string {
	where := ""
	if e.Source.Path != "" || e.Source.Layer != "" {
		where = " at " + e.Source.String()
	}
	key := ""
	if e.Key != "" {
		key = " for " + e.Key
	}
	message := e.Message + key + where
	if e.Cause != nil {
		message = fmt.Sprintf("%s: %v", message, e.Cause)
	}
	// The stable code is part of the operator-facing error contract. Keeping it
	// in Error() makes text CLI output as actionable as structured callers that
	// inspect Error.Code directly.
	if e.Code != "" {
		return e.Code + ": " + message
	}
	return message
}
func (e *Error) Unwrap() error { return e.Cause }

// ErrorCode returns the registered error code.
func (e *Error) ErrorCode() string { return e.Code }

func newError(code, message, key string, src SourceRef, cause error) error {
	return &Error{Code: code, Message: message, Key: key, Source: src, Cause: cause}
}

func (s Snapshot) Has(path string) bool { _, ok := s.Values[path]; return ok }

// Locked reports whether a global lock covers path.
func (s Snapshot) Locked(path string) bool {
	lock, _ := winningLock(s.Locks, path)
	return lock != nil
}
func (s Snapshot) Raw(path string) (any, bool) {
	v, ok := s.Values[path]
	if !ok {
		return nil, false
	}
	return clone(v.Data), true
}
func (s Snapshot) Provenance(path string) (Value, bool) {
	v, ok := s.Values[path]
	if !ok {
		return Value{}, false
	}
	v.Data = clone(v.Data)
	v.Default = clone(v.Default)
	return v, true
}
func (s Snapshot) String(path string) string {
	if v, ok := s.Values[path]; ok {
		if x, ok := v.Data.(string); ok {
			return x
		}
	}
	return ""
}
func (s Snapshot) Bool(path string) bool {
	if v, ok := s.Values[path]; ok {
		if x, ok := v.Data.(bool); ok {
			return x
		}
	}
	return false
}
func (s Snapshot) Int(path string) int {
	if v, ok := s.Values[path]; ok {
		switch x := v.Data.(type) {
		case int64:
			return int(x)
		case int:
			return x
		case float64:
			return int(x)
		}
	}
	return 0
}
func (s Snapshot) Int64(path string) int64 {
	if v, ok := s.Values[path]; ok {
		switch x := v.Data.(type) {
		case int64:
			return x
		case int:
			return int64(x)
		case float64:
			return int64(x)
		}
	}
	return 0
}
func (s Snapshot) Float(path string) float64 {
	if v, ok := s.Values[path]; ok {
		switch x := v.Data.(type) {
		case float64:
			return x
		case int64:
			return float64(x)
		case int:
			return float64(x)
		}
	}
	return 0
}
func (s Snapshot) Duration(path string) time.Duration {
	d, _ := time.ParseDuration(s.String(path))
	return d
}
func (s Snapshot) Strings(path string) []string {
	v, ok := s.Values[path]
	if !ok {
		return nil
	}
	return toStrings(v.Data)
}

// Prefix returns leaves below prefix with the prefix removed.
func (s Snapshot) Prefix(prefix string) map[string]any {
	out := map[string]any{}
	prefix = strings.TrimSuffix(prefix, ".") + "."
	for k, v := range s.Values {
		if strings.HasPrefix(k, prefix) {
			out[strings.TrimPrefix(k, prefix)] = clone(v.Data)
		}
	}
	return out
}

// NamedTables groups paths such as credential-backend.NAME.FIELD by NAME.
func (s Snapshot) NamedTables(prefix string) map[string]map[string]any {
	out := map[string]map[string]any{}
	p := strings.TrimSuffix(prefix, ".") + "."
	for k, v := range s.Values {
		if !strings.HasPrefix(k, p) {
			continue
		}
		rest := strings.TrimPrefix(k, p)
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 {
			continue
		}
		if out[parts[0]] == nil {
			out[parts[0]] = map[string]any{}
		}
		out[parts[0]][parts[1]] = clone(v.Data)
	}
	return out
}

// IndexedTables groups paths such as inventory-source.0.FIELD by numeric index.
func (s Snapshot) IndexedTables(prefix string) []map[string]any {
	named := s.NamedTables(prefix)
	max := -1
	for k := range named {
		if i, err := strconv.Atoi(k); err == nil && i > max {
			max = i
		}
	}
	if max < 0 {
		return nil
	}
	out := make([]map[string]any, max+1)
	for i := 0; i <= max; i++ {
		out[i] = named[strconv.Itoa(i)]
	}
	return out
}

// RuleList names map rules by their configuration key and source, as
// "credential-policy-map.1 (/etc/karvi/karvi.toml:40), ...", for an ambiguity
// message; field is the rule's value key (policy, profile), whose source
// stands for the rule's.
func (s Snapshot) RuleList(table, field string, indices []int) string {
	parts := make([]string, 0, len(indices))
	for _, i := range indices {
		source := s.Values[fmt.Sprintf("%s.%d.%s", table, i, field)].Source.String()
		if source == "" {
			parts = append(parts, fmt.Sprintf("%s.%d", table, i))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s.%d (%s)", table, i, source))
	}
	return strings.Join(parts, ", ")
}

func toStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{x}
	}
	return nil
}

func clone(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = clone(x[i])
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = clone(v)
		}
		return out
	case []string:
		return append([]string(nil), x...)
	default:
		return x
	}
}

// CanonicalJSON returns a stable, secret-free configuration projection.
func (s Snapshot) CanonicalJSON() ([]byte, error) {
	keys := make([]string, 0, len(s.Values))
	for k := range s.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]struct {
		Key   string `json:"key"`
		Value any    `json:"value"`
	}, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, struct {
			Key   string `json:"key"`
			Value any    `json:"value"`
		}{k, s.Values[k].Data})
	}
	return json.Marshal(ordered)
}
