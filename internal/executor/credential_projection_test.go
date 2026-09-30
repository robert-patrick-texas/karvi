package executor

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
)

// TestCredentialProjectionCredKey covers credkey on the command record:
// matched_on gains credkey for a credential
// CSV row and only then; the five keys it always wrote stay as they were,
// whatever they hold.
func TestCredentialProjectionCredKey(t *testing.T) {
	e := New(Options{})
	always := []string{"category", "safe_value", "pattern", "source", "line"}
	csv := e.credentialProjection(credentialpackage.GrantProjection{Backend: "creds", MatchedOn: credentials.Match{Category: "csv_row", Pattern: "device_name=sw-*", Source: "/etc/karvi/credentials.csv", Line: 3, CredKey: "creds:3"}})
	if got := csv.MatchedOn["credkey"]; got != "creds:3" {
		t.Errorf("credkey = %v", got)
	}
	env := e.credentialProjection(credentialpackage.GrantProjection{Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}})
	if _, present := env.MatchedOn["credkey"]; present {
		t.Errorf("credkey is written for a backend that sets none: %v", env.MatchedOn)
	}
	for _, m := range []map[string]any{csv.MatchedOn, env.MatchedOn} {
		for _, key := range always {
			if _, present := m[key]; !present {
				t.Errorf("matched_on lacks %s: %v", key, m)
			}
		}
	}
}
