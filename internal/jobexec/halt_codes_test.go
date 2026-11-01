package jobexec

import (
	"slices"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// TestHostKeyHaltNamesTheRevokedKey: under
// ssh.halt-run-on-host-key-mismatch a run halts on a revoked key as on a
// changed one; without it, or for another activity, nothing halts on a key.
func TestHostKeyHaltNamesTheRevokedKey(t *testing.T) {
	for _, tc := range []struct {
		activity string
		halt     bool
		want     []string
	}{
		{"run", true, []string{"host_key_changed", "host_key_changed_during_enrollment", "host_key_revoked"}},
		{"run", false, nil},
		{"command", true, nil},
	} {
		sets := []string{}
		if tc.halt {
			sets = append(sets, "ssh.halt-run-on-host-key-mismatch=true")
		}
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		got := DispatchPlan(cfg, tc.activity, executionplan.DispatchSettings{Mode: "serial", Width: 1}).HaltErrorCodes
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s halt=%v: halt codes %v, want %v", tc.activity, tc.halt, got, tc.want)
		}
	}
}
