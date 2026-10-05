package devsession

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// prompts is a platform definition's prompt shapes, compiled.
type prompts struct {
	levels  []compiledLevel
	generic *regexp.Regexp // when the platform has no levels
}

type compiledLevel struct {
	platform.PrivilegeLevel
	pattern  *regexp.Regexp
	escalate *regexp.Regexp // the escalate prompt, when EscalateAuth
}

// Validate checks a definition's prompt patterns and its privileged level
// before any connection: a pattern that does not compile is
// platform_prompt_pattern_invalid; a PrivilegedLevel that names no level of
// a platform with levels is platform_privilege_level_unknown.
func Validate(def platform.Definition) error {
	_, err := compile(def)
	return err
}

func compile(def platform.Definition) (*prompts, error) {
	p := &prompts{}
	if len(def.PrivilegeLevels) == 0 {
		pattern := def.PromptPattern
		if pattern == "" {
			pattern = platform.BroadPromptPattern
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, errorcodes.Errorf("platform_prompt_pattern_invalid", "platform %s: prompt pattern %q: %v", def.Name, pattern, err)
		}
		p.generic = re
		return p, nil
	}
	for _, l := range def.PrivilegeLevels {
		re, err := regexp.Compile(l.Pattern)
		if err != nil {
			return nil, errorcodes.Errorf("platform_prompt_pattern_invalid", "platform %s: level %s prompt pattern %q: %v", def.Name, l.Name, l.Pattern, err)
		}
		c := compiledLevel{PrivilegeLevel: l, pattern: re}
		if l.EscalateAuth && l.EscalatePrompt != "" {
			c.escalate, err = regexp.Compile(l.EscalatePrompt)
			if err != nil {
				return nil, errorcodes.Errorf("platform_prompt_pattern_invalid", "platform %s: level %s escalate prompt %q: %v", def.Name, l.Name, l.EscalatePrompt, err)
			}
		}
		p.levels = append(p.levels, c)
	}
	if def.PrivilegedLevel != "" {
		if _, ok := def.Level(def.PrivilegedLevel); !ok {
			return nil, errorcodes.Errorf("platform_privilege_level_unknown", "platform %s: privileged level %q is not one of its levels (%s)", def.Name, def.PrivilegedLevel, levelNames(def))
		}
	}
	return p, nil
}

func levelNames(def platform.Definition) string {
	names := make([]string, 0, len(def.PrivilegeLevels))
	for _, l := range def.PrivilegeLevels {
		names = append(names, l.Name)
	}
	return strings.Join(names, ", ")
}

// match reports whether line, the last line as rendered without its
// trailing blanks, is a prompt, and the level it belongs to ("" for a
// platform without levels).
func (p *prompts) match(line string) (prompt, level string, ok bool) {
	if line == "" {
		return "", "", false
	}
	if p.generic != nil {
		if p.generic.MatchString(line) {
			return strings.TrimSpace(line), "", true
		}
		return "", "", false
	}
	for _, l := range p.levels {
		if l.pattern.MatchString(line) {
			return strings.TrimSpace(line), l.Name, true
		}
	}
	return "", "", false
}

// escalatePrompt reports whether line, the last line, is the level's
// escalate prompt (the password prompt after enable).
func (l compiledLevel) escalatePrompt(line string) bool {
	if l.escalate == nil {
		return false
	}
	return l.escalate.MatchString(line)
}

// deviceFailure reports whether output carries one of the platform's
// failure patterns (scrapligo's failed-when-contains semantics: a plain,
// case-sensitive substring). The reader runs it on the bytes as they
// settle, with a carry across chunk borders (failureScan).
func deviceFailure(patterns []string, output []byte) bool {
	for _, p := range patterns {
		if p != "" && bytes.Contains(output, []byte(p)) {
			return true
		}
	}
	return false
}
