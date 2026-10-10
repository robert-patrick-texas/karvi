package configschema

import (
	"reflect"
	"sort"
	"testing"
)

// TestPlace: the registry marks as paths the thirteen fixed string keys,
// the list ssh.identities, and the dynamic tables' file fields, each with
// its words; an executable specification and a field beside a file are not
// paths.
func TestPlace(t *testing.T) {
	var marked []string
	for _, e := range Entries() {
		if e.Place {
			marked = append(marked, e.Path)
		}
	}
	sort.Strings(marked)
	want := []string{"audit.file", "basedir", "crun.directory", "daemon.sockets", "output.root", "scoreboards", "sessions.shared-capacity-root", "sharedroot", "spooldir", "ssh.control-path-root", "ssh.identities", "ssh.known-hosts-file", "tempdir", "transcript.root"}
	if !reflect.DeepEqual(marked, want) {
		t.Errorf("marked %v, want %v", marked, want)
	}
	for key, words := range map[string][]string{
		"basedir":                               {"auto"},
		"sharedroot":                            {"auto", "none"},
		"audit.file":                            nil,
		"ssh.identities":                        nil,
		"inventory-source.0.path":               nil,
		"credential-backend.x.path":             nil,
		"credential-backend.x.ca-file":          nil,
		"credential-backend.x.client-cert-file": nil,
		"credential-backend.x.client-key-file":  nil,
	} {
		got, ok := Place(key)
		if !ok || !reflect.DeepEqual(got, words) {
			t.Errorf("%s: %v %v, want %v", key, got, ok, words)
		}
	}
	for _, key := range []string{"ssh.transports.system", "credential-backend.x.type", "credential-backend.x.path-template", "inventory-source.0.name", "inventory-source.path", "timezone"} {
		if _, ok := Place(key); ok {
			t.Errorf("%s: marked as a path", key)
		}
	}
}

// TestReloadClass: the five keys a running daemon keeps from its start are
// daemon-start; every other key, the daemon's start timeout and socket and
// basedir among them, is next-job.
func TestReloadClass(t *testing.T) {
	start := map[string]bool{"daemon.max-accepted-jobs": true, "daemon.shutdown-idle-timer": true, "daemon.shutdown-grace-seconds": true, "daemon.max-ipc-frame-bytes": true, "daemon.forced-grace-seconds": true}
	for _, e := range Entries() {
		want := "next-job"
		if start[e.Path] {
			want = "daemon-start"
		}
		if e.ReloadClass != want {
			t.Errorf("%s: %s, want %s", e.Path, e.ReloadClass, want)
		}
	}
}

// TestLookupThroughIndex: Lookup gives every key the row Entries gives it,
// the row's slices its own, and finds it without copying the registry: two
// allocations at most, the enum values and the words.
func TestLookupThroughIndex(t *testing.T) {
	for _, e := range Entries() {
		got, ok := Lookup(e.Path)
		if !ok || !reflect.DeepEqual(got, e) {
			t.Errorf("%s: %+v %v, want %+v", e.Path, got, ok, e)
		}
	}
	e, _ := Lookup("ssh.host-key-policy")
	e.EnumValues[0] = "changed"
	w, _ := Lookup("basedir")
	w.Words[0] = "changed"
	if e, _ := Lookup("ssh.host-key-policy"); e.EnumValues[0] == "changed" {
		t.Errorf("an altered row's enum values reached the next lookup")
	}
	if w, _ := Lookup("basedir"); w.Words[0] == "changed" {
		t.Errorf("an altered row's words reached the next lookup")
	}
	for _, e := range Entries() {
		if allocs := testing.AllocsPerRun(5, func() { Lookup(e.Path) }); allocs > 2 {
			t.Fatalf("%s: %v allocations for one lookup", e.Path, allocs)
		}
	}
}
