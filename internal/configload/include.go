package configload

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload/tomlmini"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

type directive struct {
	Target   string
	Optional bool
	Line     int
}
type scannedFile struct {
	Clean          []byte
	Directives     []directive
	FirstDirective int
}

type includeState struct {
	active []string
	seen   map[string]SourceRef
}

// scanIncludes recognizes directives only while outside TOML strings and
// preserves one physical output line for each input line.
func scanIncludes(data []byte) (scannedFile, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	var dirs []directive
	first := 0
	mode := byte(0)
	escaped := false
	for idx, raw := range lines {
		lineNo := idx + 1
		line := strings.TrimSuffix(raw, "\n")
		hadNL := strings.HasSuffix(raw, "\n")
		if mode == 0 {
			trim := strings.TrimLeft(line, " \t")
			low := strings.ToLower(trim)
			token := ""
			if strings.HasPrefix(low, "@include?") && tokenBoundary(trim, len("@include?")) {
				token = trim[:len("@include?")]
			}
			if token == "" && strings.HasPrefix(low, "@include") && tokenBoundary(trim, len("@include")) {
				token = trim[:len("@include")]
			}
			if token != "" {
				target, err := parseDirectiveTarget(strings.TrimSpace(trim[len(token):]))
				if err != nil {
					return scannedFile{}, fmt.Errorf("include directive line %d: %w", lineNo, err)
				}
				if target != "" {
					dirs = append(dirs, directive{Target: target, Optional: strings.HasSuffix(token, "?"), Line: lineNo})
					if first == 0 {
						first = lineNo
					}
				}
				if hadNL {
					out.WriteByte('\n')
				}
				continue
			}
		}
		out.WriteString(line)
		if hadNL {
			out.WriteByte('\n')
		}
		for i := 0; i < len(line); {
			if mode == 0 {
				if line[i] == '#' {
					break
				}
				if strings.HasPrefix(line[i:], `"""`) {
					mode = 'B'
					escaped = false
					i += 3
					continue
				}
				if strings.HasPrefix(line[i:], `'''`) {
					mode = 'L'
					i += 3
					continue
				}
				if line[i] == '"' {
					mode = 'b'
					escaped = false
					i++
					continue
				}
				if line[i] == '\'' {
					mode = 'l'
					i++
					continue
				}
				i++
				continue
			}
			switch mode {
			case 'b':
				if escaped {
					escaped = false
					i++
					continue
				}
				if line[i] == '\\' {
					escaped = true
					i++
					continue
				}
				if line[i] == '"' {
					mode = 0
				}
				i++
			case 'l':
				if line[i] == '\'' {
					mode = 0
				}
				i++
			case 'B':
				if strings.HasPrefix(line[i:], `"""`) && !escaped {
					mode = 0
					i += 3
					continue
				}
				if escaped {
					escaped = false
				} else if line[i] == '\\' {
					escaped = true
				}
				i++
			case 'L':
				if strings.HasPrefix(line[i:], `'''`) {
					mode = 0
					i += 3
					continue
				}
				i++
			}
		}
	}
	return scannedFile{Clean: []byte(out.String()), Directives: dirs, FirstDirective: first}, nil
}
func tokenBoundary(s string, n int) bool { return len(s) == n || s[n] == ' ' || s[n] == '\t' }
func parseDirectiveTarget(s string) (string, error) {
	if s == "" || strings.HasPrefix(s, "#") {
		return "", nil
	}
	if s[0] == '"' {
		escaped := false
		for i := 1; i < len(s); i++ {
			if escaped {
				escaped = false
				continue
			}
			if s[i] == '\\' {
				escaped = true
				continue
			}
			if s[i] == '"' {
				v, err := strconv.Unquote(s[:i+1])
				return v, err
			}
		}
		return "", fmt.Errorf("unterminated quoted target")
	}
	if s[0] == '\'' {
		if i := strings.IndexByte(s[1:], '\''); i >= 0 {
			return s[1 : 1+i], nil
		}
		return "", fmt.Errorf("unterminated quoted target")
	}
	if i := strings.IndexAny(s, " \t"); i >= 0 {
		s = s[:i]
	}
	return s, nil
}

func (l *loader) processRoot(path string, layer string, authority bool) error {
	info, err := os.Lstat(path)
	if err != nil {
		return uninspectable(layer, path, err)
	}
	inherited := l.boolValue("config.includes")
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(path)
		if err != nil {
			return uninspectable(layer, path, err)
		}
		ti, err := os.Stat(target)
		if err != nil {
			return uninspectable(layer, target, err)
		}
		if ti.IsDir() {
			return newError("config_include_symlink_directory", "directory symlinks are not allowed", "", SourceRef{Layer: layer, Path: path}, nil)
		}
		path = target
		info = ti
	}
	if info.IsDir() {
		return l.processDirectory(path, layer, authority, 0, 0, inherited, SourceRef{Layer: layer, Path: path})
	}
	if !info.Mode().IsRegular() {
		return newError("config_unsupported_file_type", "configuration root is not a regular file or directory", "", SourceRef{Layer: layer, Path: path}, nil)
	}
	return l.processFile(path, layer, authority, 0, 0, true, inherited, SourceRef{Layer: layer, Path: path})
}

func (l *loader) processFile(path, layer string, authority bool, includeDepth, dirDepth int, isGraphRoot bool, inheritedIncludes bool, reached SourceRef) error {
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return uninspectable(layer, path, err)
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return uninspectable(layer, canonical, err)
	}
	for _, p := range l.inc.active {
		if p == canonical {
			return newError("config_include_cycle", "circular include: "+strings.Join(append(append([]string(nil), l.inc.active...), canonical), " -> "), "", reached, nil)
		}
	}
	if first, ok := l.inc.seen[canonical]; ok {
		return newError("config_include_duplicate_diamond", fmt.Sprintf("configuration file was reached twice; first from %s", first.String()), "", reached, nil)
	}
	if includeDepth > l.intValue("config.include-depth") {
		return newError("config_include_depth_exceeded", "include depth exceeded", "", reached, nil)
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return uninspectable(layer, canonical, err)
	}
	if !info.Mode().IsRegular() {
		return newError("config_unsupported_file_type", "include target is not a regular file", "", reached, nil)
	}
	data, err := os.ReadFile(canonical)
	if err != nil {
		return newError("config_read_error", "cannot read configuration file", "", SourceRef{Layer: layer, Path: canonical}, err)
	}
	scanned, err := scanIncludes(data)
	if err != nil {
		return newError("config_include_syntax", err.Error(), "", SourceRef{Layer: layer, Path: canonical}, err)
	}
	doc, err := tomlmini.Parse(scanned.Clean)
	if err != nil {
		return newError("config_toml_syntax", "invalid TOML", "", SourceRef{Layer: layer, Path: canonical}, err)
	}
	if pos, ok := doc.Positions["config.schema-version"]; ok {
		if v, ok := lookupTree(doc.Root, []string{"config", "schema-version"}); !ok || asInt(v) != configschema.ConfigSchemaVersion {
			return newError("config_schema_version", "unsupported configuration schema version", "config.schema-version", SourceRef{Layer: layer, Path: canonical, Line: pos.Line}, nil)
		}
	}
	if pos, ok := doc.Positions["config.includes"]; ok {
		if !isGraphRoot {
			return newError("config_includes_not_at_file_root", "config.includes is legal only in a file graph root", "config.includes", SourceRef{Layer: layer, Path: canonical, Line: pos.Line}, nil)
		}
		if scanned.FirstDirective > 0 && pos.Line > scanned.FirstDirective {
			return newError("config_includes_too_late", "config.includes appears after the first active include directive", "config.includes", SourceRef{Layer: layer, Path: canonical, Line: pos.Line}, nil)
		}
	}
	l.inc.seen[canonical] = reached
	l.inc.active = append(l.inc.active, canonical)
	defer func() { l.inc.active = l.inc.active[:len(l.inc.active)-1] }()
	if err := l.mergeDocument(doc, SourceRef{Layer: layer, Path: canonical}, authority); err != nil {
		return err
	}
	l.addSource(canonical)
	enabled := inheritedIncludes
	if isGraphRoot {
		if v, ok := doc.Positions["config.includes"]; ok && v.Line > 0 {
			enabled = l.boolValue("config.includes")
		}
	}
	if !enabled {
		return nil
	}
	for _, d := range scanned.Directives {
		resolved := d.Target
		if strings.HasPrefix(resolved, "~") {
			expanded, e := expandHome(resolved, l.home)
			if e != nil {
				code := errorcodes.Of(e)
				if code == "" {
					code = "config_include_path"
				}
				return newError(code, strings.TrimPrefix(errorcodes.Message(e), code+": "), "", SourceRef{Layer: layer, Path: canonical, Line: d.Line}, nil)
			}
			resolved = expanded
		} else if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(canonical), resolved)
		}
		st, e := os.Lstat(resolved)
		if e != nil {
			if os.IsNotExist(e) && d.Optional {
				l.warn(fmt.Sprintf("optional include missing: %s:%d -> %s", canonical, d.Line, resolved))
				continue
			}
			return newError("config_include_missing", "include target unavailable", "", SourceRef{Layer: layer, Path: canonical, Line: d.Line}, e)
		}
		if st.Mode()&os.ModeSymlink != 0 {
			target, e := filepath.EvalSymlinks(resolved)
			if e != nil {
				return uninspectable(layer, resolved, e)
			}
			ti, e := os.Stat(target)
			if e != nil {
				return uninspectable(layer, target, e)
			}
			if ti.IsDir() {
				return newError("config_include_symlink_directory", "directory symlinks are not allowed", "", SourceRef{Layer: layer, Path: canonical, Line: d.Line}, nil)
			}
			resolved = target
			st = ti
		}
		src := SourceRef{Layer: layer, Path: canonical, Line: d.Line}
		if st.IsDir() {
			if e := l.processDirectory(resolved, layer, authority, includeDepth+1, 0, enabled, src); e != nil {
				return e
			}
		} else if st.Mode().IsRegular() {
			if e := l.processFile(resolved, layer, authority, includeDepth+1, dirDepth, false, enabled, src); e != nil {
				return e
			}
		} else {
			return newError("config_unsupported_file_type", "unsupported include target", "", src, nil)
		}
	}
	return nil
}

func (l *loader) processDirectory(path, layer string, authority bool, includeDepth, dirDepth int, inherited bool, reached SourceRef) error {
	if dirDepth > l.intValue("config.dir-recursion-depth") {
		return newError("config_directory_depth_exceeded", "configuration directory recursion depth exceeded", "", reached, nil)
	}
	st, err := os.Lstat(path)
	if err != nil {
		return uninspectable(layer, path, err)
	}
	if st.Mode()&os.ModeSymlink != 0 {
		return newError("config_include_symlink_directory", "directory symlinks are not allowed", "", reached, nil)
	}
	if !st.IsDir() {
		return newError("config_unsupported_file_type", "expected configuration directory", "", reached, nil)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return uninspectable(layer, path, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		child := filepath.Join(path, entry.Name())
		info, err := os.Lstat(child)
		if err != nil {
			return uninspectable(layer, child, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := filepath.EvalSymlinks(child)
			if e != nil {
				return uninspectable(layer, child, e)
			}
			ti, e := os.Stat(target)
			if e != nil {
				return uninspectable(layer, target, e)
			}
			if ti.IsDir() {
				return newError("config_include_symlink_directory", "directory symlinks are not allowed", "", SourceRef{Layer: layer, Path: child}, nil)
			}
			if strings.HasSuffix(entry.Name(), ".toml") && ti.Mode().IsRegular() {
				if e := l.processFile(child, layer, authority, includeDepth, dirDepth, false, inherited, reached); e != nil {
					return e
				}
			}
			continue
		}
		if info.IsDir() {
			if e := l.processDirectory(child, layer, authority, includeDepth, dirDepth+1, inherited, reached); e != nil {
				return e
			}
			continue
		}
		if info.Mode().IsRegular() && strings.HasSuffix(entry.Name(), ".toml") {
			if e := l.processFile(child, layer, authority, includeDepth, dirDepth, false, inherited, reached); e != nil {
				return e
			}
		}
	}
	return nil
}

func expandHome(path, home string) (string, error) {
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return "", errorcodes.Errorf("path_other_user_home_unsupported", "~otheruser expansion is not supported: %s", path)
}

// uninspectable reports a configuration path that cannot be resolved or
// inspected while walking the configuration graph.
func uninspectable(layer, path string, err error) error {
	return newError("config_path_uninspectable", "configuration path cannot be inspected", "", SourceRef{Layer: layer, Path: path}, err)
}
