package jobexec

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/hostkey"
)

// TestExerciseFindingMessageIsTheTextWithoutTheCode: a finding carries
// its code in its own field and the error's whole text without that code
// in its message: an uncoded wrapper keeps its own text, a wrapper over a
// coded error or a coded error over its own code says the code nowhere in
// the message, and a hostkey error keeps the trust store it names.
func TestExerciseFindingMessageIsTheTextWithoutTheCode(t *testing.T) {
	notExecutable := errorcodes.Errorf("transport_executable_not_executable", "/usr/local/bin/ssh is not executable")
	for _, tc := range []struct {
		name    string
		err     error
		code    string
		message string
	}{
		{"an uncoded wrapper over an uncoded error",
			fmt.Errorf("read /var/lib/karvi/capacity/server.json: %w", errors.New("input/output error")),
			"transport_unavailable", "read /var/lib/karvi/capacity/server.json: input/output error"},
		{"an uncoded wrapper over a coded error",
			fmt.Errorf("ssh.transports.system: %w", notExecutable),
			"transport_executable_not_executable", "ssh.transports.system: /usr/local/bin/ssh is not executable"},
		{"a coded error over one of its own code",
			errorcodes.Errorf("transport_executable_not_executable", "%s=%q is unavailable: %w", "ssh.transports.system", "/usr/local/bin/ssh", notExecutable),
			"transport_executable_not_executable", `ssh.transports.system="/usr/local/bin/ssh" is unavailable: /usr/local/bin/ssh is not executable`},
		{"a code written as the text's prefix",
			fmt.Errorf("transport_mapping_missing: selector %q has no %s mapping", "lab", "ssh.transports.lab"),
			"transport_mapping_missing", `selector "lab" has no ssh.transports.lab mapping`},
		{"a hostkey error naming its trust store",
			&hostkey.Error{Code: "host_key_trust_store_permission", Path: "/home/jsmith/.local/share/karvi/known_hosts", Err: errors.New("file mode is 0644; run chmod 600 /home/jsmith/.local/share/karvi/known_hosts")},
			"host_key_trust_store_permission", "known_hosts=/home/jsmith/.local/share/karvi/known_hosts: file mode is 0644; run chmod 600 /home/jsmith/.local/share/karvi/known_hosts"},
		{"a coded error",
			errorcodes.Errorf("platform_unknown", "platform %q is not a known platform here", "cisco_xr"),
			"platform_unknown", `platform "cisco_xr" is not a known platform here`},
	} {
		f := exerciseFinding("transport", executionplan.SeverityError, "name:router1", tc.err, nil)
		if f.Code != tc.code || f.Message != tc.message || strings.Contains(f.Message, f.Code+": ") {
			t.Errorf("%s:\n  code=%s message=%q\n  want code=%s message=%q", tc.name, f.Code, f.Message, tc.code, tc.message)
		}
	}
}
