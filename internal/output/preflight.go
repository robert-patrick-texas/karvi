package output

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// The one free-space check of admission. An activity names the places it will write; the check
// groups them by the volume behind each path (stat's device number), reads
// the free space once per volume, and judges each volume by the `freecheck`
// word. Nothing else in karvi reads free space at admission: the audit
// file, the scoreboard directory, and the state root make their first
// write before any device is contacted and write kilobytes after, so their
// own write failures are their check.

// FreeCheck is a `freecheck` value.
const (
	FreeCheckAuto   = "auto"
	FreeCheckAlways = "always"
	FreeCheckNever  = "never"
)

// Place is one path an activity will write. Need is the estimate of what
// the activity leaves there when it has finished (the output root's copies
// of the device estimate, the collection's one copy), 0 for a place with
// no estimate (a login's transcripts root, floored alone). Spool marks the
// spool directory, whose ask is the transient Width × Limit and the one
// term `auto` may narrow.
type Place struct {
	Path  string
	Need  int64
	Spool bool
}

// Preflight is the admission check: Check is the `freecheck` word; Floor is
// output.min-free-bytes-after-job, asked once of every volume; Limit is
// output.max-command-bytes and Width the width the job would run at (the
// smaller of its workers, the server's cap, and its device count), which
// together are the spool's term.
type Preflight struct {
	Check  string
	Floor  int64
	Places []Place
	Limit  int64
	Width  int
}

// Admission is what the check decided: the width the job runs at, the
// narrowing's warning (spool_width_narrowed) when `auto` narrowed it, and
// the number of volumes read, for the debug line.
type Admission struct {
	Width   int
	Warning string
	Volumes int
}

// freeBytes and volumeOf are the readers every check goes through; a test
// that cannot empty a disk or mount a volume replaces them.
var (
	freeBytes = FreeBytes
	volumeOf  = VolumeID
)

// volume is the places that share one device, in the order they were named.
type volume struct {
	paths []string
	need  int64 // the sum of the fixed terms
	spool bool  // the spool directory is on it
}

// Run applies the rule. `never` reads nothing and runs the job at its
// width. `always` reads each volume once and requires Floor free on it.
// `auto` reads each volume once and requires the sum of its places' Need
// plus Floor once, plus Width × Limit where the spool is; when that sum is
// short but at least one Limit fits above the fixed terms, the width is
// narrowed to what fits with the warning; below one Limit, or below the
// fixed terms on any volume, the job is refused with output_preflight_space
// naming the volume's paths and both figures. A read of exactly zero
// refuses as any shortfall does.
func (p Preflight) Run() (Admission, error) {
	out := Admission{Width: p.Width}
	if p.Check == FreeCheckNever || len(p.Places) == 0 {
		return out, nil
	}
	vols, order := p.group()
	out.Volumes = len(order)
	for _, key := range order {
		v := vols[key]
		free, err := freeBytes(v.paths[0])
		if err != nil {
			return out, err
		}
		names := strings.Join(v.paths, ", ")
		if p.Check != FreeCheckAuto {
			if err := refuse(names, p.Floor, free); err != nil {
				return out, err
			}
			continue
		}
		fixed := v.need + p.Floor
		if !v.spool || p.Limit <= 0 || p.Width <= 0 {
			if err := refuse(names, fixed, free); err != nil {
				return out, err
			}
			continue
		}
		if free >= fixed+int64(p.Width)*p.Limit {
			continue
		}
		fits := (free - fixed) / p.Limit
		if fits < 1 {
			return out, refuse(names, fixed+p.Limit, free)
		}
		if int(fits) < out.Width {
			out.Width = int(fits)
			out.Warning = fmt.Sprintf("spool_width_narrowed: %s has %d bytes free, %d per command in flight (output.max-command-bytes) above the %d bytes the job's other files and the floor take: the job runs %d at a time instead of %d", names, free, p.Limit, fixed, out.Width, p.Width)
		}
	}
	return out, nil
}

// group sorts the places into volumes by device number, keeping the order
// in which the places were named so a message reads as the activity does.
func (p Preflight) group() (map[uint64]*volume, []uint64) {
	vols := map[uint64]*volume{}
	var order []uint64
	for _, pl := range p.Places {
		dev := volumeOf(pl.Path)
		v, ok := vols[dev]
		if !ok {
			v = &volume{}
			vols[dev] = v
			order = append(order, dev)
		}
		v.paths = append(v.paths, pl.Path)
		v.need += pl.Need
		v.spool = v.spool || pl.Spool
	}
	return vols, order
}

// refuse is the one refusal: the paths, the bytes needed, the
// bytes free.
func refuse(paths string, need, free int64) error {
	if free < need {
		return errorcodes.Errorf("output_preflight_space", "%s: need %d bytes free, only %d free", paths, need, free)
	}
	return nil
}

// FinishedSize is the estimate of what a job leaves under a root when it
// has finished: the device estimate for every device, once per
// copy the root takes (the output root writes commands.jsonl and the text
// file, two; the collection directory one), then the reserve multiplier.
func FinishedSize(expectedPerDevice int64, devices, copies int, multiplier float64) int64 {
	if multiplier <= 0 {
		multiplier = 1.25
	}
	return int64(float64(expectedPerDevice*int64(devices)*int64(copies)) * multiplier)
}

// VolumeID is the device number behind path, from stat of the path or of
// its nearest existing parent (a job folder is checked before it exists;
// a path that has to be created lands on its parent's volume). A path
// with no existing parent at all reports 0, which groups such paths
// together and lets the free-space reader report the real error.
func VolumeID(path string) uint64 {
	probe := path
	for {
		var st syscall.Stat_t
		if err := syscall.Stat(probe, &st); err == nil {
			return uint64(st.Dev)
		} else if !os.IsNotExist(err) {
			return 0
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return 0
		}
		probe = parent
	}
}
