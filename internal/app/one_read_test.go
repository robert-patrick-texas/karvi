package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestCollectionHookUnderTheRead: a crun's hook runs under the invocation's
// read, the snapshot its job was planned under: the configuration file
// changed after the read changes neither the hook that runs nor the audit
// file its event goes to.
func TestCollectionHookUnderTheRead(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "hook.out")
	for _, h := range []string{"A", "B"} {
		if err := os.WriteFile(filepath.Join(dir, "hook"+h+".sh"), []byte("#!/bin/sh\necho "+h+" >>"+out+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(dir, "config.toml")
	name := func(hook string) {
		t.Helper()
		if err := os.WriteFile(file, []byte(fmt.Sprintf("crun.after = %q\naudit.file = %q\n", filepath.Join(dir, "hook"+hook+".sh"), filepath.Join(dir, "audit"+hook+".jsonl"))), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	name("A")
	audit := filepath.Join(dir, "auditA.jsonl")
	operator, err := osutil.CurrentOperator()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: []string{file}, SkipAuto: true, HomeDir: dir, Environment: []string{}, Sets: []string{"audit.journald-required=false"}})
	if err != nil {
		t.Fatal(err)
	}
	name("B")

	result := ActivityResult{JobID: "261010-120000-00", ArtifactDir: filepath.Join(dir, "261010-120000-00"), Summary: records.Summary{Collection: &records.CollectionSummary{Directory: dir}}}
	var stderr bytes.Buffer
	RunCollectionHook(context.Background(), CommonOptions{Config: cfg, Operator: operator}, result, &stderr)
	if got, _ := os.ReadFile(out); string(got) != "A\n" {
		t.Fatalf("the hook that ran wrote %q, stderr=%q", got, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "auditB.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("the audit file the changed configuration names: %v", err)
	}
	data, err := os.ReadFile(audit)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityLines(t, "../../schema/audit.schema.json", data)
	f, err := os.Open(audit)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var event struct {
		EventName string `json:"event_name"`
		Outcome   string `json:"outcome"`
	}
	lines := bufio.NewScanner(f)
	if !lines.Scan() || json.Unmarshal(lines.Bytes(), &event) != nil || event.EventName != "crun.after" || event.Outcome != "success" {
		t.Fatalf("audit event %+v, want crun.after succeeded", event)
	}
}
