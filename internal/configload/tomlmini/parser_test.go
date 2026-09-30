package tomlmini

import "testing"

func TestParseCore(t *testing.T) {
	s := `[config]
schema-version = 1
[[inventory-source]]
name = "a"
[inventory-source.mappings]
name = ["hostname", "name"]
[config-lock]
"ssh.*" = true
[name-transform.default]
operations = [{op="lowercase"}, {op="add-suffix", suffix=".example"}]
`
	d, err := Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	if d.Root["config"].(map[string]any)["schema-version"].(int64) != 1 {
		t.Fatal("bad int")
	}
	arr := d.Root["inventory-source"].([]any)
	if len(arr) != 1 {
		t.Fatal("bad aot")
	}
	if !d.Root["config-lock"].(map[string]any)["ssh.*"].(bool) {
		t.Fatal("bad quoted key")
	}
}
func TestDuplicateKey(t *testing.T) {
	if _, err := Parse([]byte("a=1\na=2\n")); err == nil {
		t.Fatal("expected duplicate")
	}
}
func TestMultilineIncludeText(t *testing.T) {
	d, err := Parse([]byte("x=\"\"\"a\n@include nope\nb\"\"\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Root["x"].(string) == "" {
		t.Fatal("empty")
	}
}
