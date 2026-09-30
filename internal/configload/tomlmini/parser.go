// Package tomlmini implements the deliberately small TOML surface needed by
// karvi's initial release. It supports tables, arrays of tables, dotted/quoted
// keys, strings (including multiline), numbers, booleans, arrays, and inline
// tables while preserving leaf source lines. Unsupported TOML constructs fail
// explicitly rather than being guessed.
package tomlmini

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Position struct {
	Line   int
	Column int
}
type Document struct {
	Root      map[string]any
	Positions map[string]Position
}

type syntaxError struct {
	Line    int
	Column  int
	Message string
}

func (e *syntaxError) Error() string {
	return fmt.Sprintf("TOML line %d column %d: %s", e.Line, e.Column, e.Message)
}

func Parse(data []byte) (Document, error) {
	text := strings.TrimPrefix(string(data), "\ufeff")
	root := map[string]any{}
	positions := map[string]Position{}
	current := root
	currentPath := []string{}
	declared := map[string]bool{}
	lines := strings.SplitAfter(text, "\n")
	var accum strings.Builder
	startLine := 0
	startColumn := 1
	state := lexState{}
	for i, raw := range lines {
		lineNo := i + 1
		line := strings.TrimSuffix(raw, "\n")
		line = strings.TrimSuffix(line, "\r")
		cleaned, next, err := cleanLine(line, state)
		if err != nil {
			return Document{}, &syntaxError{Line: lineNo, Column: 1, Message: err.Error()}
		}
		if accum.Len() == 0 {
			if strings.TrimSpace(cleaned) == "" {
				state = next
				continue
			}
			startLine = lineNo
			startColumn = firstNonSpace(cleaned) + 1
		}
		accum.WriteString(cleaned)
		accum.WriteByte('\n')
		state = next
		if state.inString() || state.square > 0 || state.curly > 0 {
			continue
		}
		stmt := strings.TrimSpace(accum.String())
		accum.Reset()
		if stmt == "" {
			continue
		}
		if strings.HasPrefix(stmt, "[[") {
			if !strings.HasSuffix(stmt, "]] ") && !strings.HasSuffix(strings.TrimSpace(stmt), "]]") {
				return Document{}, &syntaxError{startLine, startColumn, "invalid array-of-table header"}
			}
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(stmt), "[["), "]]"))
			path, err := parseKeyPath(body)
			if err != nil {
				return Document{}, &syntaxError{startLine, startColumn, err.Error()}
			}
			m, index, err := appendArrayTable(root, path)
			if err != nil {
				return Document{}, &syntaxError{startLine, startColumn, err.Error()}
			}
			current = m
			currentPath = append(append([]string(nil), path...), strconv.Itoa(index))
			declared[strings.Join(currentPath, ".")] = true
			continue
		}
		if strings.HasPrefix(stmt, "[") {
			trim := strings.TrimSpace(stmt)
			if !strings.HasSuffix(trim, "]") || strings.HasPrefix(trim, "[[") {
				return Document{}, &syntaxError{startLine, startColumn, "invalid table header"}
			}
			body := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trim, "["), "]"))
			path, err := parseKeyPath(body)
			if err != nil {
				return Document{}, &syntaxError{startLine, startColumn, err.Error()}
			}
			m, actual, err := ensureTable(root, path)
			if err != nil {
				return Document{}, &syntaxError{startLine, startColumn, err.Error()}
			}
			canonical := strings.Join(actual, ".")
			if declared[canonical] {
				return Document{}, &syntaxError{startLine, startColumn, "table redefined: " + canonical}
			}
			declared[canonical] = true
			current = m
			currentPath = actual
			continue
		}
		eq := findTopLevelEqual(stmt)
		if eq < 0 {
			return Document{}, &syntaxError{startLine, startColumn, "expected key = value"}
		}
		keyText := strings.TrimSpace(stmt[:eq])
		valueText := strings.TrimSpace(stmt[eq+1:])
		keys, err := parseKeyPath(keyText)
		if err != nil {
			return Document{}, &syntaxError{startLine, startColumn, err.Error()}
		}
		vp := valueParser{s: valueText, line: startLine}
		value, err := vp.parse()
		if err != nil {
			return Document{}, err
		}
		fullPath := append(append([]string(nil), currentPath...), keys...)
		if err := assign(current, keys, value); err != nil {
			return Document{}, &syntaxError{startLine, startColumn, err.Error()}
		}
		positions[strings.Join(fullPath, ".")] = Position{Line: startLine, Column: startColumn}
	}
	if accum.Len() > 0 || state.inString() || state.square != 0 || state.curly != 0 {
		return Document{}, &syntaxError{Line: max(1, len(lines)), Column: 1, Message: "unterminated TOML statement"}
	}
	return Document{Root: root, Positions: positions}, nil
}

type lexState struct {
	mode          byte
	escaped       bool
	square, curly int
}

func (s lexState) inString() bool { return s.mode != 0 }

func cleanLine(line string, state lexState) (string, lexState, error) {
	var b strings.Builder
	for i := 0; i < len(line); {
		if state.mode == 0 {
			if line[i] == '#' {
				break
			}
			if strings.HasPrefix(line[i:], `"""`) {
				state.mode = 'B'
				b.WriteString(`"""`)
				i += 3
				continue
			}
			if strings.HasPrefix(line[i:], `'''`) {
				state.mode = 'L'
				b.WriteString(`'''`)
				i += 3
				continue
			}
			switch line[i] {
			case '"':
				state.mode = 'b'
				state.escaped = false
			case '\'':
				state.mode = 'l'
			case '[':
				state.square++
			case ']':
				state.square--
				if state.square < 0 {
					return "", state, fmt.Errorf("unexpected ]")
				}
			case '{':
				state.curly++
			case '}':
				state.curly--
				if state.curly < 0 {
					return "", state, fmt.Errorf("unexpected }")
				}
			}
			b.WriteByte(line[i])
			i++
			continue
		}
		switch state.mode {
		case 'b':
			b.WriteByte(line[i])
			if state.escaped {
				state.escaped = false
				i++
				continue
			}
			if line[i] == '\\' {
				state.escaped = true
				i++
				continue
			}
			if line[i] == '"' {
				state.mode = 0
			}
			i++
		case 'l':
			b.WriteByte(line[i])
			if line[i] == '\'' {
				state.mode = 0
			}
			i++
		case 'B':
			if strings.HasPrefix(line[i:], `"""`) && !state.escaped {
				b.WriteString(`"""`)
				i += 3
				state.mode = 0
				continue
			}
			b.WriteByte(line[i])
			if state.escaped {
				state.escaped = false
			} else if line[i] == '\\' {
				state.escaped = true
			}
			i++
		case 'L':
			if strings.HasPrefix(line[i:], `'''`) {
				b.WriteString(`'''`)
				i += 3
				state.mode = 0
				continue
			}
			b.WriteByte(line[i])
			i++
		}
	}
	if state.mode == 'b' || state.mode == 'l' {
		return "", state, fmt.Errorf("single-line string is not closed")
	}
	b.WriteByte('\n')
	return strings.TrimSuffix(b.String(), "\n"), state, nil
}

func firstNonSpace(s string) int {
	for i, r := range s {
		if !unicode.IsSpace(r) {
			return i
		}
	}
	return 0
}
func findTopLevelEqual(s string) int {
	mode := byte(0)
	escaped := false
	square, curly := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if mode != 0 {
			if mode == 'b' {
				if escaped {
					escaped = false
					continue
				}
				if c == '\\' {
					escaped = true
					continue
				}
				if c == '"' {
					mode = 0
				}
			} else if mode == 'l' && c == '\'' {
				mode = 0
			}
			continue
		}
		switch c {
		case '"':
			mode = 'b'
		case '\'':
			mode = 'l'
		case '[':
			square++
		case ']':
			square--
		case '{':
			curly++
		case '}':
			curly--
		case '=':
			if square == 0 && curly == 0 {
				return i
			}
		}
	}
	return -1
}

func parseKeyPath(s string) ([]string, error) {
	var out []string
	i := 0
	for {
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		var key string
		if s[i] == '"' || s[i] == '\'' {
			quote := s[i]
			i++
			start := i
			var b strings.Builder
			escaped := false
			for i < len(s) {
				c := s[i]
				if quote == '"' && escaped {
					switch c {
					case '"', '\\':
						b.WriteByte(c)
					default:
						return nil, fmt.Errorf("unsupported key escape")
					}
					escaped = false
					i++
					continue
				}
				if quote == '"' && c == '\\' {
					b.WriteString(s[start:i])
					start = i + 1
					escaped = true
					i++
					continue
				}
				if c == quote {
					b.WriteString(s[start:i])
					i++
					key = b.String()
					break
				}
				i++
			}
			if key == "" && (i == 0 || s[i-1] != quote) {
				return nil, fmt.Errorf("unterminated quoted key")
			}
		} else {
			start := i
			for i < len(s) && (isBareKey(s[i])) {
				i++
			}
			if start == i {
				return nil, fmt.Errorf("invalid key near %q", s[i:])
			}
			key = s[start:i]
		}
		if key == "" {
			return nil, fmt.Errorf("blank key")
		}
		out = append(out, key)
		for i < len(s) && unicode.IsSpace(rune(s[i])) {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] != '.' {
			return nil, fmt.Errorf("unexpected key character %q", s[i])
		}
		i++
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("blank key path")
	}
	return out, nil
}
func isBareKey(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

func ensureTable(root map[string]any, path []string) (map[string]any, []string, error) {
	m := root
	actual := []string{}
	for _, seg := range path {
		actual = append(actual, seg)
		v, ok := m[seg]
		if !ok {
			n := map[string]any{}
			m[seg] = n
			m = n
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			m = x
		case []any:
			if len(x) == 0 {
				return nil, nil, fmt.Errorf("array table %s has no element", seg)
			}
			last, ok := x[len(x)-1].(map[string]any)
			if !ok {
				return nil, nil, fmt.Errorf("%s is not a table", seg)
			}
			actual = append(actual, strconv.Itoa(len(x)-1))
			m = last
		default:
			return nil, nil, fmt.Errorf("%s is already a scalar", strings.Join(actual, "."))
		}
	}
	return m, actual, nil
}
func appendArrayTable(root map[string]any, path []string) (map[string]any, int, error) {
	if len(path) == 0 {
		return nil, 0, fmt.Errorf("blank array table")
	}
	m := root
	for _, seg := range path[:len(path)-1] {
		v, ok := m[seg]
		if !ok {
			n := map[string]any{}
			m[seg] = n
			m = n
			continue
		}
		switch x := v.(type) {
		case map[string]any:
			m = x
		case []any:
			if len(x) == 0 {
				return nil, 0, fmt.Errorf("empty parent array table")
			}
			last, ok := x[len(x)-1].(map[string]any)
			if !ok {
				return nil, 0, fmt.Errorf("parent %s is not table", seg)
			}
			m = last
		default:
			return nil, 0, fmt.Errorf("parent %s is scalar", seg)
		}
	}
	leaf := path[len(path)-1]
	var arr []any
	if v, ok := m[leaf]; ok {
		var good bool
		arr, good = v.([]any)
		if !good {
			return nil, 0, fmt.Errorf("%s is not an array of tables", leaf)
		}
	}
	n := map[string]any{}
	arr = append(arr, n)
	m[leaf] = arr
	return n, len(arr) - 1, nil
}
func assign(m map[string]any, path []string, value any) error {
	for _, seg := range path[:len(path)-1] {
		v, ok := m[seg]
		if !ok {
			n := map[string]any{}
			m[seg] = n
			m = n
			continue
		}
		n, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("key %s conflicts with scalar", seg)
		}
		m = n
	}
	leaf := path[len(path)-1]
	if _, exists := m[leaf]; exists {
		return fmt.Errorf("duplicate key %s", strings.Join(path, "."))
	}
	m[leaf] = value
	return nil
}

type valueParser struct {
	s    string
	i    int
	line int
}

func (p *valueParser) parse() (any, error) {
	p.skip()
	v, err := p.value()
	if err != nil {
		return nil, err
	}
	p.skip()
	if p.i != len(p.s) {
		return nil, p.err("unexpected trailing input")
	}
	return v, nil
}
func (p *valueParser) value() (any, error) {
	p.skip()
	if p.i >= len(p.s) {
		return nil, p.err("missing value")
	}
	if strings.HasPrefix(p.s[p.i:], `"""`) {
		return p.multilineBasic()
	}
	if strings.HasPrefix(p.s[p.i:], `'''`) {
		return p.multilineLiteral()
	}
	switch p.s[p.i] {
	case '"':
		return p.basic()
	case '\'':
		return p.literal()
	case '[':
		return p.array()
	case '{':
		return p.inlineTable()
	}
	return p.atom()
}
func (p *valueParser) skip() {
	for p.i < len(p.s) {
		r, n := utf8.DecodeRuneInString(p.s[p.i:])
		if !unicode.IsSpace(r) {
			break
		}
		p.i += n
	}
}
func (p *valueParser) err(msg string) error {
	return &syntaxError{Line: p.line + strings.Count(p.s[:p.i], "\n"), Column: 1, Message: msg}
}
func (p *valueParser) basic() (any, error) {
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		p.i++
		if c == '"' {
			return b.String(), nil
		}
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		if p.i >= len(p.s) {
			return nil, p.err("unfinished escape")
		}
		e := p.s[p.i]
		p.i++
		switch e {
		case 'b':
			b.WriteByte('\b')
		case 't':
			b.WriteByte('\t')
		case 'n':
			b.WriteByte('\n')
		case 'f':
			b.WriteByte('\f')
		case 'r':
			b.WriteByte('\r')
		case '"':
			b.WriteByte('"')
		case '\\':
			b.WriteByte('\\')
		case 'u', 'U':
			digits := 4
			if e == 'U' {
				digits = 8
			}
			if p.i+digits > len(p.s) {
				return nil, p.err("short unicode escape")
			}
			n, err := strconv.ParseUint(p.s[p.i:p.i+digits], 16, 32)
			if err != nil {
				return nil, p.err("invalid unicode escape")
			}
			b.WriteRune(rune(n))
			p.i += digits
		default:
			return nil, p.err("unsupported escape")
		}
	}
	return nil, p.err("unterminated string")
}
func (p *valueParser) literal() (any, error) {
	p.i++
	start := p.i
	for p.i < len(p.s) {
		if p.s[p.i] == '\'' {
			v := p.s[start:p.i]
			p.i++
			return v, nil
		}
		p.i++
	}
	return nil, p.err("unterminated literal string")
}
func (p *valueParser) multilineBasic() (any, error) {
	p.i += 3
	if p.i < len(p.s) && p.s[p.i] == '\n' {
		p.i++
	}
	end := findUnescapedTriple(p.s, p.i, `"""`)
	if end < 0 {
		return nil, p.err("unterminated multiline string")
	}
	raw := p.s[p.i:end]
	p.i = end + 3
	return decodeBasicBody(raw, p.line)
}
func (p *valueParser) multilineLiteral() (any, error) {
	p.i += 3
	if p.i < len(p.s) && p.s[p.i] == '\n' {
		p.i++
	}
	end := strings.Index(p.s[p.i:], `'''`)
	if end < 0 {
		return nil, p.err("unterminated multiline literal")
	}
	v := p.s[p.i : p.i+end]
	p.i += end + 3
	return v, nil
}
func findUnescapedTriple(s string, start int, triple string) int {
	for i := start; i+3 <= len(s); i++ {
		if strings.HasPrefix(s[i:], triple) {
			slashes := 0
			for j := i - 1; j >= start && s[j] == '\\'; j-- {
				slashes++
			}
			if slashes%2 == 0 {
				return i
			}
		}
	}
	return -1
}
func decodeBasicBody(raw string, line int) (string, error) {
	q := "\"" + strings.ReplaceAll(raw, "\n", "\\n") + "\""
	v, err := strconv.Unquote(q)
	if err != nil {
		return "", &syntaxError{Line: line, Column: 1, Message: "invalid multiline string escape: " + err.Error()}
	}
	return v, nil
}
func (p *valueParser) array() (any, error) {
	p.i++
	var out []any
	for {
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.err("unterminated array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.err("unterminated array")
		}
		if p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
		if p.s[p.i] != ',' {
			return nil, p.err("expected comma in array")
		}
		p.i++
		p.skip()
		if p.i < len(p.s) && p.s[p.i] == ']' {
			p.i++
			return out, nil
		}
	}
}
func (p *valueParser) inlineTable() (any, error) {
	p.i++
	m := map[string]any{}
	for {
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.err("unterminated inline table")
		}
		if p.s[p.i] == '}' {
			p.i++
			return m, nil
		}
		start := p.i
		eq := -1
		mode := byte(0)
		for p.i < len(p.s) {
			c := p.s[p.i]
			if mode == 0 && (c == '"' || c == '\'') {
				mode = c
				p.i++
				continue
			}
			if mode != 0 {
				if c == mode {
					mode = 0
				}
				p.i++
				continue
			}
			if c == '=' {
				eq = p.i
				break
			}
			if c == ',' || c == '}' {
				break
			}
			p.i++
		}
		if eq < 0 {
			return nil, p.err("expected = in inline table")
		}
		keyText := strings.TrimSpace(p.s[start:eq])
		keys, err := parseKeyPath(keyText)
		if err != nil {
			return nil, p.err(err.Error())
		}
		p.i = eq + 1
		v, err := p.value()
		if err != nil {
			return nil, err
		}
		if err := assign(m, keys, v); err != nil {
			return nil, p.err(err.Error())
		}
		p.skip()
		if p.i >= len(p.s) {
			return nil, p.err("unterminated inline table")
		}
		if p.s[p.i] == '}' {
			p.i++
			return m, nil
		}
		if p.s[p.i] != ',' {
			return nil, p.err("expected comma in inline table")
		}
		p.i++
	}
}
func (p *valueParser) atom() (any, error) {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if unicode.IsSpace(rune(c)) || c == ',' || c == ']' || c == '}' {
			break
		}
		p.i++
	}
	tok := p.s[start:p.i]
	switch tok {
	case "true":
		return true, nil
	case "false":
		return false, nil
	}
	clean := strings.ReplaceAll(tok, "_", "")
	if strings.ContainsAny(clean, ".eE") {
		v, err := strconv.ParseFloat(clean, 64)
		if err == nil {
			return v, nil
		}
	}
	v, err := strconv.ParseInt(clean, 0, 64)
	if err == nil {
		return v, nil
	}
	return nil, p.err("unsupported bare value " + strconv.Quote(tok))
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ParseValue decodes one TOML scalar/array/inline-table value. It is used for
// registry defaults, environment overrides, command flags, and --set so all
// layers share identical conversion semantics.
func ParseValue(s string) (any, error) {
	p := valueParser{s: strings.TrimSpace(s), line: 1}
	return p.parse()
}
