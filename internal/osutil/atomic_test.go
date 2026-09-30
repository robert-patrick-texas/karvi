package osutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The layout rule for a .json file: indented,
// multi-line, ending in a newline, HTML characters unescaped, the content
// unchanged. The .jsonl half of the rule (one compact object per line) is held
// by internal/output's line tests.
func TestAtomicJSONWritesAnIndentedFile(t *testing.T) {
	dir := t.TempDir()
	value := map[string]any{"final_status": "completed", "device_counts": map[string]int{"total": 2}, "text": "a<b>&c"}

	path := filepath.Join(dir, "summary.json")
	if err := AtomicJSON(path, value, 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"device_counts\": {\n    \"total\": 2\n  },\n  \"final_status\": \"completed\",\n  \"text\": \"a<b>&c\"\n}\n"
	if string(raw) != want {
		t.Fatalf(".json layout:\n got %q\nwant %q", raw, want)
	}
	var back map[string]any
	if err := json.Unmarshal(raw, &back); err != nil || back["final_status"] != "completed" {
		t.Fatalf("decode: %v %v", err, back)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode: %v %v", info, err)
	}

}
