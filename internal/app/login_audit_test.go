package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/audit"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestLoginEvents: a login's events are login.start, with no outcome, and
// login.end with its outcome, under the login's ID, naming the device by its
// canonical name and the operator's login user, with no command list; the
// lines keep to the published schema.
func TestLoginEvents(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, "audit.jsonl")
	cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: []string{`audit.file="` + file + `"`, `audit.journald-required=false`}})
	if err != nil {
		t.Fatal(err)
	}
	sink, err := audit.New(cfg, home)
	if err != nil {
		t.Fatal(err)
	}
	operator := credentials.Operator{Username: "tester", UID: 1001, LoginUser: "tester.login"}
	d := inventory.Device{ID: "name:fake-iosxe", Name: "Fake-IOSXE", CanonicalName: "fake-iosxe", Platform: "cisco_ios"}
	for _, e := range [][2]string{{"start", ""}, {"end", "complete"}} {
		if err := writeLoginAudit(sink, operator, cfg, "261017-101500-00", d, "netops", "builtin-env-fallback", e[0], e[1], 0, map[string]any{"diagnostic": ""}); err != nil {
			t.Fatal(err)
		}
	}
	sink.Close()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	canarytest.SchemaParityLines(t, "../../schema/audit.schema.json", data)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d lines: %s", len(lines), data)
	}
	for i, want := range []struct{ name, outcome string }{{"login.start", ""}, {"login.end", "complete"}} {
		var r records.AuditRecord
		if err := json.Unmarshal([]byte(lines[i]), &r); err != nil {
			t.Fatal(err)
		}
		if r.EventName != want.name || r.Outcome != want.outcome || r.LoginID != "261017-101500-00" || r.JobID != "" || r.LoginUser != "tester.login" || r.Device == nil || r.Device.CanonicalName != "fake-iosxe" || r.Action["mode"] != "login" || len(r.Action) != 2 {
			t.Errorf("line %d: %s", i, lines[i])
		}
	}
}
