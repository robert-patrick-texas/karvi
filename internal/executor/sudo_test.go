package executor

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestRootCommandOnAnExecTarget: a --sudo command on an exec target keeps
// its command as written on its record, with privilege sudo, is shown at
// the root prompt in the live view, and --debug names whether its sudo has
// a password (sudo=password, the grant's enable password, or sudo=none);
// the command beside it has no privilege, a "$" prompt, and no sudo field
// (EXAMPLES chapter 96). The records keep to the published schema. What
// sudo itself does is the session's (devsession) and the fake's to show.
func TestRootCommandOnAnExecTarget(t *testing.T) {
	for _, tc := range []struct {
		enable, mode string
	}{{"pw1", "sudo=password"}, {"", "sudo=none"}} {
		srv, err := fakedevice.Start(fakedevice.Options{Persona: fakedevice.PersonaLinux, Username: "u", Password: "p"})
		if err != nil {
			t.Fatal(err)
		}
		defer srv.Close()
		h := newGateHarness(t, nil, nil)
		h.target.Device.Transport = "native"
		h.target.Device.Platform = "linux"
		h.target.Channel = executionplan.ChannelExec
		h.target.Device.Port = uint16(srv.Port())
		h.exec.opts.Grants = liveGrant(tc.enable)
		h.exec.opts.Commands = []string{"uname -snrm", "whoami"}
		h.exec.opts.Sudo = []bool{false, true}
		var mu sync.Mutex
		var debug []string
		h.exec.opts.Debug = func(s string) {
			mu.Lock()
			debug = append(debug, s)
			mu.Unlock()
		}
		live := &liveRecorder{}
		h.exec.opts.Live = live.open
		_, recs := h.run(context.Background())
		if len(recs) != 2 || recs[0].Privilege != "" || recs[1].Privilege != records.PrivilegeSudo || recs[1].Command != "whoami" || !recs[1].Root() || recs[0].Root() {
			t.Fatalf("%s: records %s", tc.mode, describe(recs))
		}
		for _, r := range recs {
			canarytest.SchemaParity(t, "../../schema/command-record.schema.json", r)
		}
		name := h.target.Device.CanonicalName
		if got := live.text(); !strings.HasPrefix(got, name+"$ uname -snrm\n") || !strings.Contains(got, name+"# whoami\n") {
			t.Fatalf("%s: view %q", tc.mode, got)
		}
		mu.Lock()
		var starts []string
		for _, line := range debug {
			if strings.HasPrefix(line, "device command start ") {
				starts = append(starts, line)
			}
		}
		mu.Unlock()
		if len(starts) != 2 || strings.Contains(starts[0], "sudo=") || !strings.HasSuffix(starts[1], " "+tc.mode) {
			t.Fatalf("%s: start lines %q", tc.mode, starts)
		}
		for _, line := range debug {
			if tc.enable != "" && strings.Contains(line, tc.enable) {
				t.Fatalf("%s: the password reached a debug line: %q", tc.mode, line)
			}
		}
	}
}
