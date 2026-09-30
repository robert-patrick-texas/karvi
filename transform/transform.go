// Package transform implements the shared, deterministic operation pipeline for
// device names and credential lookup/user names.
package transform

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

type Step struct {
	Operation string `json:"operation"`
	Before    string `json:"before"`
	After     string `json:"after"`
}

type Operation interface {
	Name() string
	Validate() error
	Apply(input string) (string, error)
}

type Pipeline interface {
	Apply(input string) (output string, trace []Step, err error)
}

type Sequence []Operation

func (s Sequence) Apply(input string) (string, []Step, error) {
	out := input
	trace := make([]Step, 0, len(s))
	for _, op := range s {
		if err := op.Validate(); err != nil {
			return "", trace, fmt.Errorf("transform %s: %w", op.Name(), err)
		}
		next, err := op.Apply(out)
		if err != nil {
			return "", trace, fmt.Errorf("transform %s: %w", op.Name(), err)
		}
		trace = append(trace, Step{Operation: op.Name(), Before: out, After: next})
		out = next
	}
	return out, trace, nil
}

type Lowercase struct{}

func (Lowercase) Name() string                   { return "lowercase" }
func (Lowercase) Validate() error                { return nil }
func (Lowercase) Apply(s string) (string, error) { return strings.ToLower(s), nil }

type Uppercase struct{}

func (Uppercase) Name() string                   { return "uppercase" }
func (Uppercase) Validate() error                { return nil }
func (Uppercase) Apply(s string) (string, error) { return strings.ToUpper(s), nil }

type CropToDot struct{}

func (CropToDot) Name() string    { return "crop-to-dot" }
func (CropToDot) Validate() error { return nil }
func (CropToDot) Apply(s string) (string, error) {
	if i := strings.IndexRune(s, '.'); i >= 0 {
		return s[:i], nil
	}
	return s, nil
}

type StripChars struct{ Chars string }

func (StripChars) Name() string { return "strip-chars" }
func (o StripChars) Validate() error {
	if o.Chars == "" {
		return errorcodes.Errorf("transform_chars_empty", "chars must be nonempty")
	}
	return nil
}
func (o StripChars) Apply(s string) (string, error) {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(o.Chars, r) {
			return -1
		}
		return r
	}, s), nil
}

type RunePair struct{ From, To string }
type ReplaceChars struct{ Pairs []RunePair }

func (ReplaceChars) Name() string { return "replace-chars" }
func (o ReplaceChars) Validate() error {
	if len(o.Pairs) == 0 {
		return errorcodes.Errorf("transform_pairs_empty", "pairs must be nonempty")
	}
	for _, p := range o.Pairs {
		if utf8.RuneCountInString(p.From) != 1 || utf8.RuneCountInString(p.To) != 1 {
			return errorcodes.Errorf("transform_pair_rune_invalid", "from/to must each contain one rune")
		}
	}
	return nil
}
func (o ReplaceChars) Apply(s string) (string, error) {
	m := map[rune]rune{}
	for _, p := range o.Pairs {
		a, _ := utf8.DecodeRuneInString(p.From)
		b, _ := utf8.DecodeRuneInString(p.To)
		m[a] = b
	}
	return strings.Map(func(r rune) rune {
		if x, ok := m[r]; ok {
			return x
		}
		return r
	}, s), nil
}

type ReplaceSuffix struct{ Suffix, Replacement string }

func (ReplaceSuffix) Name() string { return "replace-suffix" }
func (o ReplaceSuffix) Validate() error {
	if o.Suffix == "" {
		return errorcodes.Errorf("transform_suffix_empty", "suffix must be nonempty")
	}
	return nil
}
func (o ReplaceSuffix) Apply(s string) (string, error) {
	if strings.HasSuffix(s, o.Suffix) {
		return strings.TrimSuffix(s, o.Suffix) + o.Replacement, nil
	}
	return s, nil
}

type StripSuffix struct{ Suffix string }

func (StripSuffix) Name() string { return "strip-suffix" }
func (o StripSuffix) Validate() error {
	if o.Suffix == "" {
		return errorcodes.Errorf("transform_suffix_empty", "suffix must be nonempty")
	}
	return nil
}
func (o StripSuffix) Apply(s string) (string, error) { return strings.TrimSuffix(s, o.Suffix), nil }

type AddSuffix struct{ Suffix string }

func (AddSuffix) Name() string { return "add-suffix" }
func (o AddSuffix) Validate() error {
	if o.Suffix == "" {
		return errorcodes.Errorf("transform_suffix_empty", "suffix must be nonempty")
	}
	return nil
}
func (o AddSuffix) Apply(s string) (string, error) {
	if strings.ContainsRune(s, '.') {
		return s, nil
	}
	return s + o.Suffix, nil
}

// FromMaps builds a validated operation sequence from decoded TOML inline tables.
func FromMaps(items []map[string]any) (Sequence, error) {
	seq := make(Sequence, 0, len(items))
	for i, item := range items {
		name, _ := item["op"].(string)
		var op Operation
		switch name {
		case "lowercase":
			op = Lowercase{}
		case "uppercase":
			op = Uppercase{}
		case "crop-to-dot":
			op = CropToDot{}
		case "strip-chars":
			op = StripChars{Chars: stringValue(item["chars"])}
		case "replace-suffix":
			op = ReplaceSuffix{Suffix: stringValue(item["suffix"]), Replacement: stringValue(item["replacement"])}
		case "strip-suffix":
			op = StripSuffix{Suffix: stringValue(item["suffix"])}
		case "add-suffix":
			op = AddSuffix{Suffix: stringValue(item["suffix"])}
		case "replace-chars":
			var pairs []RunePair
			if raw, ok := item["pairs"].([]any); ok {
				for _, v := range raw {
					if m, ok := v.(map[string]any); ok {
						pairs = append(pairs, RunePair{From: stringValue(m["from"]), To: stringValue(m["to"])})
					}
				}
			}
			op = ReplaceChars{Pairs: pairs}
		default:
			return nil, errorcodes.Errorf("transform_operation_unknown", "operation %d has unknown op %q", i+1, name)
		}
		if err := op.Validate(); err != nil {
			return nil, fmt.Errorf("operation %d: %w", i+1, err)
		}
		seq = append(seq, op)
	}
	return seq, nil
}
func stringValue(v any) string { s, _ := v.(string); return s }
