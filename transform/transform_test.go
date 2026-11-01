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

// TestOperationParameterOfAnotherKindIsRefused: an operation's string
// parameter (chars, suffix, replacement, a pair's from and to) given as a
// number, a boolean, or an array is refused as config_type_error, naming the
// operation and the parameter, never read as one left out.
func TestOperationParameterOfAnotherKindIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		item map[string]any
		text string
	}{
		{"a replacement that is a number", map[string]any{"op": "replace-suffix", "suffix": ".corp", "replacement": int64(5)}, "operation 1: replacement must be a string"},
		{"a suffix that is a number", map[string]any{"op": "replace-suffix", "suffix": int64(5), "replacement": ".lab"}, "operation 1: suffix must be a string"},
		{"chars that are a number", map[string]any{"op": "strip-chars", "chars": int64(5)}, "operation 1: chars must be a string"},
		{"a strip-suffix boolean", map[string]any{"op": "strip-suffix", "suffix": true}, "operation 1: suffix must be a string"},
		{"an add-suffix array", map[string]any{"op": "add-suffix", "suffix": []any{".example.net"}}, "operation 1: suffix must be a string"},
		{"a pair's from that is a number", map[string]any{"op": "replace-chars", "pairs": []any{map[string]any{"from": int64(5), "to": "-"}}}, "operation 1: pair 1: from must be a string"},
		{"a pair's to that is a number", map[string]any{"op": "replace-chars", "pairs": []any{map[string]any{"from": "_", "to": "-"}, map[string]any{"from": "_", "to": int64(5)}}}, "operation 1: pair 2: to must be a string"},
	} {
		_, err := FromMaps([]map[string]any{tc.item})
		if errorcodes.Of(err) != "config_type_error" || !strings.Contains(err.Error(), tc.text) {
			t.Errorf("%s: err=%v, want config_type_error with %q", tc.name, err, tc.text)
		}
	}
	seq, err := FromMaps([]map[string]any{{"op": "replace-suffix", "suffix": ".corp"}})
	if err != nil || len(seq) != 1 {
		t.Fatalf("a replacement left out: %d, %v", len(seq), err)
	}
	if got, _, _ := seq.Apply("router1.corp"); got != "router1" {
		t.Fatalf("a replacement left out: got %q, want router1", got)
	}
}
