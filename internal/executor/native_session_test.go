package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
)

// TestNativeRunsTheDeviceSession is the native transport at the executor: a
// device over scrapligo-v1 is admitted by its base platform, runs the same
// session as the system transport (paging, device errors, connection
// reuse), and records its commands.
func TestNativeRunsTheDeviceSession(t *testing.T) {
	srv, err := fakedevice.Start(fakedevice.Options{StartPrivileged: true, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := newGateHarness(t, nil, nil)
	h.withAlgorithmConfig(`platform.c9300.driver="cisco_iosxe"`)
	h.target.Device.Transport = "native"
	h.target.Device.Platform = "c9300"
	h.target.Device.Port = uint16(srv.Port())
	h.exec.opts.Commands = []string{"show clock", "show bogus"}
	res, recs := h.run(context.Background())
	if res.Success || res.ErrorCode != "device_command_error" || h.transportAttempted() {
		t.Fatalf("result %+v system transport run=%t", res, h.transportAttempted())
	}
	if len(recs) != 2 || recs[0].Status != "succeeded" || recs[1].Error == nil || recs[1].Error.Code != "device_command_error" {
		t.Fatalf("records %+v", recs)
	}
	if recs[0].ConnectionReused == nil || *recs[0].ConnectionReused || recs[1].ConnectionReused == nil || !*recs[1].ConnectionReused {
		t.Fatalf("connection_reused %v %v", recs[0].ConnectionReused, recs[1].ConnectionReused)
	}
	time.Sleep(100 * time.Millisecond)
	if got := strings.Join(srv.Lines(), "|"); got != "terminal length 0|terminal width 512|show clock|show bogus|exit" || srv.Connections() != 1 {
		t.Fatalf("device saw %q connections=%d", got, srv.Connections())
	}
}
