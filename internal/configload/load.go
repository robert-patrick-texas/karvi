package configload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload/tomlmini"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

type loader struct {
	snap      Snapshot
	inc       includeState
	home      string
	arrayNext map[string]int
}

// Reading is a configuration as its files and the environment give it: the
// built-in defaults, the global, user, and explicit roots with their
// includes and locks, and the KARVI__ variables, a load's warnings among
// them. Its Snapshot takes an invocation's options and --set over it, as
// often as a stream has jobs, without reading again.
type Reading struct {
	snap Snapshot
	home string
}

// Load builds and validates an immutable configuration snapshot: the
// Reading of opts, then its Snapshot under opts' options and --set.
func Load(opts Options) (Snapshot, error) {
	r, err := Read(opts)
	if err != nil {
		return Snapshot{}, err
	}
	return r.Snapshot(opts.FlagValues, opts.Sets)
}

// Read reads the configuration's files and environment as opts names them;
// opts' FlagValues and Sets are the Snapshot's.
func Read(opts Options) (Reading, error) {
	home := opts.HomeDir
	if home == "" {
		if u, err := user.Current(); err == nil {
			home = u.HomeDir
		}
	}
	if home == "" {
		home = os.Getenv("HOME")
	}
	l := &loader{snap: Snapshot{Values: map[string]Value{}}, inc: includeState{seen: map[string]SourceRef{}}, home: home, arrayNext: map[string]int{}}
	if err := l.loadDefaults(); err != nil {
		return Reading{}, err
	}
	if !opts.InternalOnly && !opts.SkipAuto {
		global, err := firstExisting(globalRoots)
		if err != nil {
			return Reading{}, err
		}
		if global != "" {
			if err := l.processRoot(global, "global", true); err != nil {
				return Reading{}, err
			}
		}
		if l.boolValue("config.allow-user-layer") {
			userPath, err := firstExisting([]string{filepath.Join(home, ".config/karvi/config.toml")})
			if err != nil {
				return Reading{}, err
			}
			if userPath != "" {
				if err := l.processRoot(userPath, "user", false); err != nil {
					return Reading{}, err
				}
			}
		}
	}
	for _, root := range opts.ExplicitRoots {
		abs, err := expandHome(root, home)
		if err != nil && strings.HasPrefix(root, "~") {
			return Reading{}, err
		}
		if !strings.HasPrefix(root, "~") {
			abs = root
		}
		if _, err := os.Stat(abs); err != nil {
			return Reading{}, newError("config_explicit_missing", "explicit configuration root unavailable", "", SourceRef{Layer: "explicit", Path: abs}, err)
		}
		if err := l.processRoot(abs, "explicit", false); err != nil {
			return Reading{}, err
		}
	}
	env := opts.Environment
	if env == nil {
		env = os.Environ()
	}
	if err := l.mergeEnvironment(env); err != nil {
		return Reading{}, err
	}
	return Reading{snap: l.snap, home: home}, nil
}

// Snapshot builds and validates the reading's snapshot under flags, an
// invocation's options by key, and sets, its --set in order. The reading is
// left as it was, so every call is independent of the others.
func (r Reading) Snapshot(flags map[string]FlagValue, sets []string) (Snapshot, error) {
	l := &loader{snap: r.snap.own(), home: r.home}
	keys := make([]string, 0, len(flags))
	for k := range flags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		f := flags[k]
		if err := l.assign(k, f.Value, SourceRef{Layer: "cli", Path: f.Option}, false); err != nil {
			return Snapshot{}, err
		}
	}
	for i, set := range sets {
		eq := strings.IndexByte(set, '=')
		if eq <= 0 {
			return Snapshot{}, newError("config_set_syntax", "--set requires key=value", "", SourceRef{Layer: "set", Path: fmt.Sprintf("--set[%d]", i+1)}, nil)
		}
		key := strings.TrimSpace(set[:eq])
		raw := strings.TrimSpace(set[eq+1:])
		v, err := parseLayerValue(key, raw)
		if err != nil {
			return Snapshot{}, newError("config_set_value", "invalid --set value", key, SourceRef{Layer: "set", Path: fmt.Sprintf("--set[%d]", i+1)}, err)
		}
		if err := l.assign(key, v, SourceRef{Layer: "set", Path: fmt.Sprintf("--set[%d]", i+1)}, false); err != nil {
			return Snapshot{}, err
		}
	}
	if err := l.expandMacros(); err != nil {
		return Snapshot{}, err
	}
	if err := l.validate(); err != nil {
		return Snapshot{}, err
	}
	if err := l.absolutePlaces(); err != nil {
		return Snapshot{}, err
	}
	raw, err := l.snap.CanonicalJSON()
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(raw)
	l.snap.Digest = hex.EncodeToString(sum[:])
	return l.snap, nil
}

func (l *loader) loadDefaults() error {
	for _, e := range configschema.Entries() {
		v, err := tomlmini.ParseValue(e.DefaultLiteral)
		if err != nil {
			return errorcodes.Errorf("config_builtin_default_invalid", "invalid built-in default %s: %w", e.Path, err)
		}
		src := SourceRef{Layer: "builtin", Path: "<builtin>"}
		l.snap.Values[e.Path] = Value{Data: v, Default: clone(v), Source: src}
	}
	return nil
}

// globalRoots are the auto-discovered global configuration roots, the only
// graph that may declare locks. Tests replace the variable to place a
// global file under a temporary directory; no option or environment
// variable reaches it.
var globalRoots = []string{"/etc/karvi/config.toml", "/opt/karvi/config.toml"}

func firstExisting(paths []string) (string, error) {
	for _, p := range paths {
		st, err := os.Stat(p)
		if err == nil {
			if st.IsDir() {
				return "", errorcodes.Errorf("config_discovery_candidate_directory", "configuration candidate is a directory: %s", p)
			}
			return p, nil
		}
		if os.IsNotExist(err) {
			continue
		}
		return "", newError("config_discovery_error", "configuration candidate cannot be inspected", "", SourceRef{Layer: "discovery", Path: p}, err)
	}
	return "", nil
}
func (l *loader) addSource(path string) {
	for _, p := range l.snap.Sources {
		if p == path {
			return
		}
	}
	l.snap.Sources = append(l.snap.Sources, path)
}
func (l *loader) warn(s string)           { l.snap.Warnings = append(l.snap.Warnings, s) }
func (l *loader) boolValue(k string) bool { return l.snap.Bool(k) }
func (l *loader) intValue(k string) int   { return l.snap.Int(k) }

func (l *loader) mergeDocument(doc tomlmini.Document, src SourceRef, authority bool) error {
	if raw, ok := doc.Root["config-lock"]; ok {
		table, ok := raw.(map[string]any)
		if !ok {
			return newError("config_lock_type", "config-lock must be a table", "config-lock", src, nil)
		}
		if !authority {
			return newError("config_lock_authority_violation", "only the auto-discovered global graph may declare locks", "config-lock", src, nil)
		}
		names := make([]string, 0, len(table))
		for k := range table {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, pattern := range names {
			v := table[pattern]
			b, ok := v.(bool)
			if !ok || !b {
				return newError("config_lock_value", "lock declarations must equal true", pattern, src, nil)
			}
			for _, d := range l.snap.Locks {
				if strings.EqualFold(d.Pattern, pattern) {
					return newError("config_lock_duplicate", "duplicate lock declaration", pattern, src, nil)
				}
			}
			line := positionLine(doc.Positions, "config-lock."+pattern)
			l.snap.Locks = append(l.snap.Locks, LockDecl{Pattern: pattern, Source: SourceRef{Layer: src.Layer, Path: src.Path, Line: line}})
		}
		delete(doc.Root, "config-lock")
	}
	return l.mergeMap(doc.Root, "", doc.Positions, src, authority)
}
func (l *loader) mergeMap(m map[string]any, prefix string, pos map[string]tomlmini.Position, src SourceRef, authority bool) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		switch x := v.(type) {
		case map[string]any:
			if err := l.mergeMap(x, path, pos, src, authority); err != nil {
				return err
			}
		case []any:
			if isObjectArray(x) && isTopLevelArrayTable(path) {
				base := l.arrayNext[path]
				for i, e := range x {
					if err := l.mergeMap(e.(map[string]any), fmt.Sprintf("%s.%d", path, base+i), pos, src, authority); err != nil {
						return err
					}
				}
				l.arrayNext[path] = base + len(x)
			} else {
				line := positionLine(pos, path)
				s := src
				s.Line = line
				if err := l.assign(path, x, s, authority); err != nil {
					return err
				}
			}
		default:
			line := positionLine(pos, path)
			s := src
			s.Line = line
			if err := l.assign(path, x, s, authority); err != nil {
				return err
			}
		}
	}
	return nil
}
func isObjectArray(a []any) bool {
	if len(a) == 0 {
		return false
	}
	for _, v := range a {
		if _, ok := v.(map[string]any); !ok {
			return false
		}
	}
	return true
}
func isTopLevelArrayTable(path string) bool {
	return path == "inventory-source" || path == "credential-policy-map" || path == "session-init-map" || path == "ssh-algorithms-map"
}
func positionLine(pos map[string]tomlmini.Position, path string) int {
	if p, ok := pos[path]; ok {
		return p.Line
	}
	parts := strings.Split(path, ".")
	for len(parts) > 0 {
		p := strings.Join(parts, ".")
		if v, ok := pos[p]; ok {
			return v.Line
		}
		parts = parts[:len(parts)-1]
	}
	return 0
}

func (l *loader) assign(key string, value any, src SourceRef, authority bool) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return newError("config_key_blank", "blank configuration key", key, src, nil)
	}
	if r, ok := removedKeyByPath(key); ok {
		return r.refuse(key, src)
	}
	if !configschema.IsKnownLeaf(key) {
		return newError("config_unknown_key", "unknown configuration key", key, src, nil)
	}
	if !authority {
		lock, err := winningLock(l.snap.Locks, key)
		if err != nil {
			return newError("config_lock_equal_specificity", err.Error(), key, src, nil)
		}
		if lock != nil {
			return newError("config_lock_violation", fmt.Sprintf("write blocked by lock %q declared at %s", lock.Pattern, lock.Source.String()), key, src, nil)
		}
	}
	old, exists := l.snap.Values[key]
	nv := Value{Data: clone(configschema.Canonical(key, value)), Source: src}
	if exists {
		nv.Default = old.Default
		nv.Overridden = append(append([]SourceRef(nil), old.Overridden...), old.Source)
	}
	if lock, _ := winningLock(l.snap.Locks, key); lock != nil {
		copy := *lock
		nv.Lock = &copy
	}
	l.snap.Values[key] = nv
	return nil
}

func (l *loader) mergeEnvironment(env []string) error {
	index := configschema.EnvironmentIndex()
	reject := l.boolValue("config.reject-unknown-env")
	for _, item := range env {
		eq := strings.IndexByte(item, '=')
		if eq < 0 {
			continue
		}
		name, value := item[:eq], item[eq+1:]
		if !strings.HasPrefix(name, "KARVI__") {
			continue
		}
		if r, ok := removedKeyByEnvironment(name); ok {
			return r.refuse(r.path, SourceRef{Layer: "environment", Path: name})
		}
		key, ok := index[name]
		if !ok {
			if reject {
				return newError("config_unknown_environment", "unknown KARVI__ environment variable", "", SourceRef{Layer: "environment", Path: name}, nil)
			}
			l.warn("ignored unknown environment variable " + name)
			continue
		}
		v, err := parseLayerValue(key, value)
		if err != nil {
			return newError("config_environment_value", "invalid environment override", key, SourceRef{Layer: "environment", Path: name}, err)
		}
		if err := l.assign(key, v, SourceRef{Layer: "environment", Path: name}, false); err != nil {
			return err
		}
	}
	return nil
}
func parseLayerValue(key, raw string) (any, error) {
	e, ok := configschema.Lookup(key)
	if ok && (e.Kind == configschema.String || e.Kind == configschema.Duration || e.Kind == configschema.Enum) {
		trim := strings.TrimSpace(raw)
		if len(trim) > 0 && (trim[0] == '"' || trim[0] == '\'') {
			return tomlmini.ParseValue(trim)
		}
		return raw, nil
	}
	return tomlmini.ParseValue(raw)
}
func lookupTree(root map[string]any, path []string) (any, bool) {
	var cur any = root
	for _, p := range path {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[p]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}
func asInt(v any) int64 {
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return -1
}
