package auditview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestRunReadsALargeEventWhole: journalctl writes a field over 4,096 bytes
// as null in its JSON unless given --all; the fake does the same, so an
// event of 300 statements, its JSON past that size, has its row only when
// the reader asks for the field whole.
func TestRunReadsALargeEventWhole(t *testing.T) {
	commands := make([]string, 300)
	for i := range commands {
		commands[i] = "ip prefix-list PL-CUST seq " + strconv.Itoa((i+1)*5) + " permit 192.0.2.0/24 le 32"
	}
	action, _ := json.Marshal(map[string]any{"mode": "cmd", "commands": commands})
	event := `{"schema_version":3,"event_name":"cmd.start","operator":{"username":"jsmith"},"job_id":"261101-090100-00","action":` + string(action) + `}`
	if len(event) <= 4096 {
		t.Fatalf("the event is %d bytes", len(event))
	}
	dir := t.TempDir()
	whole, null := filepath.Join(dir, "whole"), filepath.Join(dir, "null")
	if err := os.WriteFile(whole, []byte(entry(event)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(null, []byte(`{"__CURSOR":"s=1","KARVI_AUDIT_JSON":null}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = --all ] && exec cat '" + whole + "'; done\nexec cat '" + null + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "journalctl"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("NO_COLOR", "")
	code, out, errs := runFiles(t)
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || errs != "" || len(lines) != 2 || !strings.HasPrefix(lines[1], "261101-090100-00 ") || !strings.Contains(lines[1], "jsmith") {
		t.Fatalf("the large event: %d %q %q", code, out, errs)
	}
}
