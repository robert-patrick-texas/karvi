package output

import (
	"errors"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// withFakeVolumes replaces the readers: volumes maps a path to its device
// number and free maps a device number to its free bytes; reads counts the
// free-space reads.
func withFakeVolumes(t *testing.T, volumes map[string]uint64, free map[uint64]int64, reads *int) {
	t.Helper()
	prevFree, prevVol := freeBytes, volumeOf
	volumeOf = func(path string) uint64 { return volumes[path] }
	freeBytes = func(path string) (int64, error) { *reads++; return free[volumes[path]], nil }
	t.Cleanup(func() { freeBytes, volumeOf = prevFree, prevVol })
}

// TestPreflightGroupsByVolume: one read per volume, the
// places on a volume summed, the floor once per volume, the refusal naming
// every path on the short volume.
func TestPreflightGroupsByVolume(t *testing.T) {
	const gib = int64(1 << 30)
	volumes := map[string]uint64{"/jobs/j": 1, "/crun": 1, "/spool": 2, "/transcripts": 1}
	reads := 0
	free := map[uint64]int64{1: 4 * gib, 2: 4 * gib}
	withFakeVolumes(t, volumes, free, &reads)
	p := Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Limit: gib, Width: 2, Places: []Place{
		{Path: "/jobs/j", Need: gib}, {Path: "/crun", Need: gib / 2}, {Path: "/spool", Spool: true},
	}}
	got, err := p.Run()
	if err != nil || got.Width != 2 || got.Volumes != 2 || reads != 2 {
		t.Fatalf("two volumes with room: %+v %v, %d reads", got, err, reads)
	}
	// Volume 1 asks 1 + 0.5 + 2 GiB: at 3.5 GiB it passes, a byte under refuses
	// naming both paths on it and neither of the others.
	free[1] = 3*gib + gib/2
	if _, err := p.Run(); err != nil {
		t.Fatalf("exactly the sum: %v", err)
	}
	free[1]--
	err = p.Run2()
	if errorcodes.Of(err) != "output_preflight_space" || !strings.Contains(err.Error(), "/jobs/j, /crun: need") || strings.Contains(err.Error(), "/spool") {
		t.Fatalf("a byte short: %v", err)
	}
	free[1] = 4 * gib
	// The spool's volume: 3 GiB holds the floor and one command, so the width
	// narrows to 1 with the warning; below the floor plus one limit it refuses.
	free[2] = 3 * gib
	got, err = Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Limit: gib, Width: 4, Places: p.Places}.Run()
	if err != nil || got.Width != 1 || !strings.Contains(got.Warning, "spool_width_narrowed: /spool has") || !strings.Contains(got.Warning, "per command in flight (output.max-command-bytes)") || !strings.Contains(got.Warning, "runs 1 at a time instead of 4") {
		t.Fatalf("narrowed: %+v %v", got, err)
	}
	// The largest limit a --maxbytes: the warning names the declaration.
	got, err = Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Limit: gib, Declared: true, Width: 4, Places: p.Places}.Run()
	if err != nil || got.Width != 1 || !strings.Contains(got.Warning, "per command in flight (--maxbytes)") {
		t.Fatalf("narrowed by a declared limit: %+v %v", got, err)
	}
	free[2] = 3*gib - 1
	_, err = Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Limit: gib, Width: 4, Places: p.Places}.Run()
	if errorcodes.Of(err) != "output_preflight_space" || !strings.Contains(err.Error(), "/spool: need 3221225472 bytes free") {
		t.Fatalf("below one command: %v", err)
	}
	// A place with no estimate (a transcripts root) asks the floor alone.
	reads = 0
	got, err = Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Places: []Place{{Path: "/transcripts"}}}.Run()
	if err != nil || reads != 1 || got.Volumes != 1 {
		t.Fatalf("the floor alone: %+v %v, %d reads", got, err, reads)
	}
	free[1] = 2*gib - 1
	if _, err := (Preflight{Check: FreeCheckAuto, Floor: 2 * gib, Places: []Place{{Path: "/transcripts"}}}).Run(); errorcodes.Of(err) != "output_preflight_space" {
		t.Fatalf("under the floor: %v", err)
	}
}

// Run2 is Run for a test that wants the error alone.
func (p Preflight) Run2() error { _, err := p.Run(); return err }

// TestPreflightWords: `always` asks the floor of every volume and nothing
// else, `never` reads nothing, and a reader's error is reported.
func TestPreflightWords(t *testing.T) {
	const gib = int64(1 << 30)
	volumes := map[string]uint64{"/jobs/j": 1, "/spool": 2}
	reads := 0
	free := map[uint64]int64{1: 2 * gib, 2: 2 * gib}
	withFakeVolumes(t, volumes, free, &reads)
	places := []Place{{Path: "/jobs/j", Need: 100 * gib}, {Path: "/spool", Spool: true}}
	got, err := Preflight{Check: FreeCheckAlways, Floor: 2 * gib, Limit: gib, Width: 64, Places: places}.Run()
	if err != nil || got.Width != 64 || reads != 2 {
		t.Fatalf("always at the floor: %+v %v, %d reads", got, err, reads)
	}
	free[2] = 2*gib - 1
	if _, err := (Preflight{Check: FreeCheckAlways, Floor: 2 * gib, Places: places}).Run(); errorcodes.Of(err) != "output_preflight_space" || !strings.Contains(err.Error(), "/spool: need 2147483648 bytes free, only 2147483647 free") {
		t.Fatalf("always under the floor: %v", err)
	}
	reads = 0
	free[1], free[2] = 0, 0
	got, err = Preflight{Check: FreeCheckNever, Floor: 2 * gib, Limit: gib, Width: 64, Places: places}.Run()
	if err != nil || got.Width != 64 || reads != 0 || got.Volumes != 0 {
		t.Fatalf("never: %+v %v, %d reads", got, err, reads)
	}
	// Zero free refuses as any shortfall does, the message
	// naming the path and "only 0 free".
	err = Preflight{Check: FreeCheckAuto, Floor: 1, Places: []Place{{Path: "/jobs/j"}}}.Run2()
	if errorcodes.Of(err) != "output_preflight_space" || !strings.Contains(err.Error(), "/jobs/j: need 1 bytes free, only 0 free") {
		t.Fatalf("zero free: %v", err)
	}
	// A floor of zero and no estimate asks nothing of a volume with nothing.
	if err := (Preflight{Check: FreeCheckAuto, Places: []Place{{Path: "/jobs/j"}}}).Run2(); err != nil {
		t.Fatalf("nothing asked: %v", err)
	}
	freeBytes = func(string) (int64, error) { return 0, errors.New("statfs failed") }
	if err := (Preflight{Check: FreeCheckAlways, Floor: 1, Places: places}).Run2(); err == nil || errorcodes.Of(err) == "output_preflight_space" {
		t.Fatalf("a reader's error is reported as it is: %v", err)
	}
}

// TestVolumeIDWalksUp: a path that does not exist yet reports its nearest
// existing parent's device, so a job folder is grouped with its root.
func TestVolumeIDWalksUp(t *testing.T) {
	dir := t.TempDir()
	if VolumeID(dir+"/day/job") != VolumeID(dir) || VolumeID(dir) == 0 {
		t.Fatalf("a folder to be created is on its parent's volume")
	}
}
