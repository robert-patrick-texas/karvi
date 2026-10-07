package executor

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/records"
)

// enrolledNotices are a record's host_key_enrolled notices.
func enrolledNotices(r records.CommandRecord) []records.Notice {
	var out []records.Notice
	for _, n := range r.Notices {
		if n.Code == "host_key_enrolled" {
			out = append(out, n)
		}
	}
	return out
}

// TestHostKeyEnrolledOnTheFirstRecord: under accept-new with an empty trust
// store the device's first record carries host_key_enrolled, with OpenSSH's
// label of the key type, and no later record does; the next job, the key
// known, carries none; and a session that stores the key and then fails to
// authenticate says so on its failure record.
func TestHostKeyEnrolledOnTheFirstRecord(t *testing.T) {
	srv, err := fakedevice.Start(fakedevice.Options{StartPrivileged: true, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	// The trust store in a directory of its own, 0700 whatever the umask:
	// accept-new refuses one that group or others may write.
	dir := filepath.Join(t.TempDir(), "trust")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "known_hosts")
	native := func() *gateHarness {
		h := newGateHarness(t, executionplan.PingSettings{}, nil)
		h.withAlgorithmConfig(`platform.c9300.driver="cisco_iosxe"`, `ssh.host-key-policy="accept-new"`, `ssh.known-hosts-file="`+store+`"`)
		h.target.Device.Transport = "native"
		h.target.Device.Platform = "c9300"
		h.target.Device.Port = uint16(srv.Port())
		h.exec.opts.Commands = []string{"show clock", "show version"}
		return h
	}
	h := native()
	_, recs := h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "succeeded" {
		t.Fatalf("records %+v, first error %+v", recs, recs[0].Error)
	}
	want := []records.Notice{{Code: "host_key_enrolled", Message: "ssh accepted new host key for " + recs[0].Device.CanonicalName + " (ED25519)", Details: map[string]any{"key_type": "ED25519"}}}
	if got := enrolledNotices(recs[0]); !reflect.DeepEqual(got, want) {
		t.Fatalf("first record's notices %+v", recs[0].Notices)
	}
	if got := enrolledNotices(recs[1]); len(got) != 0 {
		t.Fatalf("second record's notices %+v", got)
	}

	// The next job: the key known, nothing said.
	h = native()
	_, recs = h.run(context.Background())
	if len(recs) != 2 || len(enrolledNotices(recs[0])) != 0 {
		t.Fatalf("the known key: %+v", recs)
	}

	// Stored, then refused: the failure record says the key was stored. The
	// refusing device listens on a port of its own, an identity
	// ([127.0.0.1]:PORT) the store does not hold.
	refusing, err := fakedevice.Start(fakedevice.Options{StartPrivileged: true, Username: "u", Password: "another"})
	if err != nil {
		t.Fatal(err)
	}
	defer refusing.Close()
	h = native()
	h.target.Device.Port = uint16(refusing.Port())
	_, recs = h.run(context.Background())
	if len(recs) != 2 || recs[0].Status != "authentication_error" || !reflect.DeepEqual(enrolledNotices(recs[0]), want) || len(enrolledNotices(recs[1])) != 0 {
		t.Fatalf("the refused session's records %+v", recs)
	}
}

// TestAuditDetailsNameTheEnrolledKey: a command_completed event's details
// carry host_key_enrolled from the record's notice, and are empty without
// one.
func TestAuditDetailsNameTheEnrolledKey(t *testing.T) {
	r := records.CommandRecord{Notices: []records.Notice{{Code: "platform_not_set"}, {Code: "host_key_enrolled", Message: "ssh accepted new host key for r1 (ECDSA)", Details: map[string]any{"key_type": "ECDSA"}}}}
	if got := auditDetails(r); !reflect.DeepEqual(got, map[string]any{"host_key_enrolled": "ECDSA"}) {
		t.Fatalf("details %v", got)
	}
	if got := auditDetails(records.CommandRecord{}); len(got) != 0 {
		t.Fatalf("no notice: %v", got)
	}
}
