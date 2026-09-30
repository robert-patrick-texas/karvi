package cli

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// TestV0100AddressAuthorityOptions: the two
// --address-authority forms are built, command and login take the enum, run
// takes the repeatable TARGET=client|daemon, and the refusals carry their
// codes: a malformed value cli_option_value_invalid, a target outside the set
// address_authority_target_unknown, and daemon authority while the gate is
// closed daemon_resolution_not_allowed, before any device is contacted.
func TestV0100AddressAuthorityOptions(t *testing.T) {
	inv := mustParse(t, "command", "--address-authority", "daemon", "r1", "show", "clock")
	if inv.String(optAddrAuthority) != "daemon" || len(inv.Pending) != 0 {
		t.Fatalf("command: %q pending=%v", inv.String(optAddrAuthority), inv.Pending)
	}
	inv = mustParse(t, "login", "r1", "--address-authority", "client")
	if inv.String(optAddrAuthority) != "client" {
		t.Fatalf("login: %q", inv.String(optAddrAuthority))
	}
	inv = mustParse(t, "run", "--target", "r1", "--address-authority", "r1=daemon", "--address-authority", "r2=client", "show", "clock")
	if !reflect.DeepEqual(inv.Strings(optAddrAuthorityRun), []string{"r1=daemon", "r2=client"}) {
		t.Fatalf("run: %v", inv.Strings(optAddrAuthorityRun))
	}
	for _, tc := range []struct {
		args []string
		code string
		exit int
	}{
		{[]string{"command", "--address-authority", "executor", "r1", "show", "clock"}, "cli_option_value_invalid", exitcode.ExitUsageError},
		{[]string{"run", "--no-daemon", "--target", "127.0.0.1", "--address-authority", "127.0.0.1=executor", "show", "clock"}, "cli_option_value_invalid", exitcode.ExitUsageError},
		{[]string{"run", "--no-daemon", "--target", "127.0.0.1", "--address-authority", "daemon", "show", "clock"}, "cli_option_value_invalid", exitcode.ExitUsageError},
		{[]string{"run", "--no-daemon", "--target", "127.0.0.1", "--address-authority", "ghost=daemon", "show", "clock"}, "address_authority_target_unknown", exitcode.ExitUsageError},
		{[]string{"run", "--no-daemon", "--target", "127.0.0.1", "--address-authority", "127.0.0.1=daemon", "show", "clock"}, "daemon_resolution_not_allowed", exitcode.ExitPermissionError},
		{[]string{"command", "--address-authority", "daemon", "127.0.0.1", "show", "clock"}, "daemon_resolution_not_allowed", exitcode.ExitPermissionError},
	} {
		t.Run(strings.Join(tc.args, "_"), func(t *testing.T) {
			// Under its own base directory: a run or command reserves its
			// job directory before the draft refuses it, and the operator's
			// real tree is not the place for that.
			args := append([]string{"--set", fmt.Sprintf("basedir=%q", t.TempDir())}, tc.args...)
			var stdout, stderr bytes.Buffer
			got := Main(args, strings.NewReader(""), &stdout, &stderr)
			if got != tc.exit || !strings.HasPrefix(stderr.String(), tc.code+": ") {
				t.Fatalf("exit=%d stderr=%q, want %d and prefix %s", got, stderr.String(), tc.exit, tc.code)
			}
		})
	}
}
