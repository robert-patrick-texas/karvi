package transform

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func TestSequence(t *testing.T) {
	p := Sequence{Lowercase{}, CropToDot{}, AddSuffix{Suffix: ".example.gov"}}
	got, trace, err := p.Apply("SW1.EXAMPLE.NET")
	if err != nil {
		t.Fatal(err)
	}
	if got != "sw1.example.gov" {
		t.Fatalf("got %q", got)
	}
	if len(trace) != 3 {
		t.Fatalf("trace=%d", len(trace))
	}
}
func TestReplaceCharsValidation(t *testing.T) {
	if (ReplaceChars{Pairs: []RunePair{{From: "ab", To: "x"}}}).Validate() == nil {
		t.Fatal("expected error")
	}
}

// TestOperationKeysAreItsOwn: an operation's keys are op and its own
// parameters, a replace-chars pair's from and to; any other key is refused as
// config_unknown_key, naming the operation and the key, and a pair that is
// not an inline table as transform_pair_rune_invalid. Every operation loads
// with all of its own keys.
func TestOperationKeysAreItsOwn(t *testing.T) {
	pair := func(kv ...string) map[string]any {
		m := map[string]any{}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	for _, tc := range []struct {
		name       string
		item       map[string]any
		code, text string
	}{
		{"a misspelled replacement", map[string]any{"op": "replace-suffix", "suffix": ".corp", "replacment": ".lab"}, "config_unknown_key", `operation 1: replace-suffix has no key "replacment"`},
		{"another operation's parameter", map[string]any{"op": "lowercase", "suffix": ".corp"}, "config_unknown_key", `operation 1: lowercase has no key "suffix"`},
		{"strip-suffix with a replacement", map[string]any{"op": "strip-suffix", "suffix": ".corp", "replacement": ".lab"}, "config_unknown_key", `operation 1: strip-suffix has no key "replacement"`},
		{"a pair that is not a table", map[string]any{"op": "replace-chars", "pairs": []any{pair("from", "_", "to", "-"), []any{"a", "b"}}}, "transform_pair_rune_invalid", "operation 1: pair 2 is not an inline table"},
		{"a pair's unknown key", map[string]any{"op": "replace-chars", "pairs": []any{pair("from", "_", "to", "-", "too", "x")}}, "config_unknown_key", `operation 1: pair 1 has no key "too"`},
	} {
		_, err := FromMaps([]map[string]any{tc.item})
		if errorcodes.Of(err) != tc.code || err == nil || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: err=%v, want %s with %q", tc.name, err, tc.code, tc.text)
		}
	}
	seq, err := FromMaps([]map[string]any{
		{"op": "lowercase"}, {"op": "uppercase"}, {"op": "crop-to-dot"},
		{"op": "strip-chars", "chars": "-"},
		{"op": "replace-chars", "pairs": []any{pair("from", "_", "to", "-")}},
		{"op": "replace-suffix", "suffix": ".corp", "replacement": ".lab"},
		{"op": "strip-suffix", "suffix": ".lab"},
		{"op": "add-suffix", "suffix": ".example.net"},
	})
	if err != nil || len(seq) != 8 {
		t.Fatalf("every operation with its own keys: %d, %v", len(seq), err)
	}
}
