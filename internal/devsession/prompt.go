package devsession

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/platform"
)

// ansiRE strips terminal control sequences from the line a prompt is
// matched against.
var ansiRE = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

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

// lastLine is the final line of the bytes received, without carriage
// returns, control sequences, or trailing blanks: the line a prompt is.
func lastLine(data []byte) string {
	return strings.TrimRight(lastLineUntrimmed(data), " \t")
}

// lastLineUntrimmed is lastLine with the trailing blanks kept: the line an
// expectation is matched against, so a device's value prompt ending in a
// space ("Destination filename [startup-config]? ") is seen as the device
// wrote it and only a $-anchored pattern must account for the space.
func lastLineUntrimmed(data []byte) string {
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[i+1:]
	}
	line := strings.ReplaceAll(string(data), "\r", "")
	return ansiRE.ReplaceAllString(line, "")
}

// match reports the prompt at the end of data and the level it belongs to
// ("" for a platform without levels).
func (p *prompts) match(data []byte) (prompt, level string, ok bool) {
	line := lastLine(data)
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

// escalatePromptAtEnd reports whether data ends in the level's escalate
// prompt (the password prompt after enable).
func (l compiledLevel) escalatePromptAtEnd(data []byte) bool {
	if l.escalate == nil {
		return false
	}
	return l.escalate.MatchString(lastLine(data))
}

// dropCarriageReturns removes every carriage return from data in place and
// returns the shortened slice. Up to the first one nothing moves. The
// streaming cleaner (settle.go) applies it to each chunk as it arrives.
func dropCarriageReturns(data []byte) []byte {
	w := bytes.IndexByte(data, '\r')
	if w < 0 {
		return data
	}
	for _, c := range data[w+1:] {
		if c != '\r' {
			data[w] = c
			w++
		}
	}
	return data[:w]
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
