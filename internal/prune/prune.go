// Package prune implements the external, schema-aware retention helper
// karvi-prune. It removes only finished activity directories, ended
// transcripts, and terminal scoreboard files older than the retention age
// (or, under free-space pressure, the oldest eligible ones), from the job
// and transcript trees under the operator's private root and under the
// site's shared root, and never touches the collection directory,
// journald, or central audit storage.
package prune

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/transcript"
	"github.com/robert-patrick-texas/karvi/records"
)

// Options are one run's settings. PrivateRoots are the private roots whose
// jobs, transcripts, and state/scoreboards are walked: an operator's run
// every root of the operator's that exists (osutil.OperatorPrivateRoots),
// a root run the site's <systemroot>/users/<username> leaves
// (osutil.SiteUserRoots), or --basedir's one. SharedRoot is the
// sharedroot setting ("auto" consults osutil.SharedRoots, "none" or empty
// consults nothing, a path consults that root), whose jobs and transcripts
// trees are walked too, never its crun directory; ScoreboardRoot is the
// shared scoreboards. UID is the invoking user: a run removes what that
// user owns and passes another's by, and a root run (UID 0) removes
// everything eligible.
type Options struct {
	PrivateRoots   []string
	SharedRoot     string
	ScoreboardRoot string
	Days           int
	MinFreePercent float64
	DryRun         bool
	// Verbose adds one `kept` line per examined item that stays, with the
	// reason.
	Verbose bool
	// Format is the report's form, FormatText (the default, key=value lines)
	// or FormatJSONL (one JSON document per line).
	Format string
	UID    int
	Now    time.Time
}

// Result counts one run: Examined the records read, Removed the candidates
// removed (or that would be, under --dry-run), Skipped the entries passed
// by (a name that is not a day folder, a non-terminal record, a tree that
// could not be walked), NotOwned the eligible candidates another user owns,
// Failed the removals that returned an error.
type Result struct {
	Examined, Removed, Skipped, NotOwned, Failed int
	BytesEstimated                               int64
}

// A candidate is one eligible item: an activity folder, a transcript pair
// (path the metadata file, pair the transcript), or a scoreboard file. tree is the root it was found under, whose filesystem
// the pressure floor is judged on; uid its owner.
type candidate struct {
	path string
	pair string
	tree string
	mod  time.Time
	size int64
	kind string
	old  bool
	uid  int
	// ageOnly marks a candidate the age rule alone may take, never
	// pressure: an orphan folder or a stale non-terminal snapshot,
	// whose state is not a finished job's.
	ageOnly bool
}

// tree is one walk: a root, its kind, and the walker that lists its
// candidates.
type tree struct {
	root string
	kind string
	walk func(root string, cutoff time.Time, opts Options, out io.Writer) ([]candidate, int, int, error)
}

// walkers are the two trees karvi writes under a root, by their names.
var walkers = []struct {
	sub, kind, key, code string
	walk                 func(root string, cutoff time.Time, opts Options, out io.Writer) ([]candidate, int, int, error)
}{
	{"jobs", "activity", "output.root", "output_directory_not_writable", activityCandidates},
	{"transcripts", "transcript", "transcript.root", "transcript_directory_not_writable", transcriptCandidates},
}

// trees lists the walks of a run: each private root's jobs
// and transcripts, the shared root's jobs and transcripts where the shared
// root holds them (the choice karvi's own writers make, judged without
// writing), and the scoreboards, each private root's and the shared
// folder. A shared tree the operator cannot use is reported through out
// and passed by, as karvi itself would refuse it. Nothing is made.
func trees(opts Options, out io.Writer) []tree {
	var list []tree
	shared := opts.SharedRoot
	if shared == "" {
		shared = "none"
	}
	for _, w := range walkers {
		for _, r := range opts.PrivateRoots {
			list = append(list, tree{filepath.Join(r, w.sub), w.kind, w.walk})
		}
		p, ok, err := osutil.SharedTreeChoice(shared, w.sub, w.key, w.code)
		if err != nil {
			report(out, opts, "skipped", f("kind", "tree"), f("path", filepath.Join(shared, w.sub)), f("error", err))
			continue
		}
		if ok {
			list = append(list, tree{p, w.kind, w.walk})
		}
	}
	for _, r := range scoreboardRoots(opts) {
		list = append(list, tree{r, "scoreboard", scoreboardCandidates})
	}
	return list
}

// scoreboardRoots are the scoreboards a run walks: state/scoreboards under
// each private root, then the shared folder.
func scoreboardRoots(opts Options) []string {
	var roots []string
	for _, r := range opts.PrivateRoots {
		roots = append(roots, filepath.Join(r, "state", "scoreboards"))
	}
	if opts.ScoreboardRoot != "" {
		roots = append(roots, opts.ScoreboardRoot)
	}
	return roots
}

// Run prunes under opts and writes one line per removal, failure, or tree
// passed by to out (the report; the summary line is the caller's). It
// returns an error only for a setting it cannot act on; a tree that cannot
// be walked and a removal that fails are counted and the run goes on.
func Run(opts Options, out io.Writer) (Result, error) {
	if opts.Days <= 0 {
		opts.Days = 31
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	cutoff := opts.Now.Add(-time.Duration(opts.Days) * 24 * time.Hour)
	candidates := []candidate{}
	result := Result{}
	// pressure is judged per tree, on its own filesystem.
	pressure := map[string]bool{}
	var walked []tree
	for _, t := range trees(opts, out) {
		if opts.Verbose {
			// The sequence of the places a run looks at (docs/PRUNE.md).
			report(out, opts, "walk", f("kind", t.kind), f("path", t.root))
		}
		got, examined, skipped, err := t.walk(t.root, cutoff, opts, out)
		result.Examined += examined
		result.Skipped += skipped
		if err != nil {
			if !os.IsNotExist(err) {
				report(out, opts, "skipped", f("kind", "tree"), f("path", t.root), f("error", err))
				result.Skipped++
			}
			continue
		}
		walked = append(walked, t)
		for i := range got {
			got[i].tree = t.root
		}
		candidates = append(candidates, got...)
		pressure[t.root] = opts.MinFreePercent > 0 && freePercent(t.root) < opts.MinFreePercent
	}
	// Oldest first, so pressure takes the oldest eligible and keeps the
	// newest.
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].mod.Before(candidates[j].mod) })
	for _, c := range candidates {
		if !c.old && (!pressure[c.tree] || c.ageOnly) {
			keep(opts, out, c.kind, c.path, "young")
			continue
		}
		if opts.UID != 0 && c.uid != opts.UID {
			result.NotOwned++
			keep(opts, out, c.kind, c.path, "not-owned")
			continue
		}
		// One line per item: the removal first, then its line,
		// so a removal that fails is its failed line alone and not a removed
		// line followed by it (the sandbox's read-only file system).
		action := "removed"
		if opts.DryRun {
			action = "would-remove"
		} else if err := remove(c); err != nil {
			report(out, opts, "failed", f("kind", c.kind), f("path", c.path), f("error", err))
			result.Failed++
			continue
		}
		fields := []field{f("kind", c.kind), f("path", c.path)}
		if c.pair != "" {
			fields = append(fields, f("transcript", c.pair))
		}
		report(out, opts, action, append(fields, f("age", opts.Now.Sub(c.mod)), f("bytes", c.size))...)
		result.Removed++
		result.BytesEstimated += c.size
		if pressure[c.tree] && freePercent(c.tree) >= opts.MinFreePercent {
			pressure[c.tree] = false
		}
	}
	for _, t := range walked {
		if t.kind != "scoreboard" {
			sweepDayFolders(t.root, cutoff, opts, out, &result)
		}
	}
	return result, nil
}

// sweepDayFolders removes the day folders of one tree that are empty and
// older than the retention age by the date in their name: a folder
// made by the first activity of its day and left standing after its jobs
// or transcripts went. The day's own folder and any younger one are never
// touched, so a claim that has just made today's folder keeps it; the
// removal is the plain rmdir ReleaseJobID uses, which refuses a folder
// that gained an entry in the meantime, so there is no check-then-remove
// race; the ownership rule applies as for a job. An empty folder
// frees nothing and is not taken under pressure.
func sweepDayFolders(root string, cutoff time.Time, opts Options, out io.Writer, result *Result) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, e := range entries {
		day, ok := osutil.DayFolderDate(e.Name())
		if !ok || !e.IsDir() || !day.Before(cutoff) {
			continue
		}
		path := filepath.Join(root, e.Name())
		inside, err := os.ReadDir(path)
		if err != nil || len(inside) != 0 {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if opts.UID != 0 && owner(fi) != opts.UID {
			result.NotOwned++
			keep(opts, out, "day", path, "not-owned")
			continue
		}
		action := "removed"
		if opts.DryRun {
			action = "would-remove"
		} else if err := os.Remove(path); err != nil {
			if os.IsNotExist(err) || isNotEmpty(err) {
				// Gone already, or an activity landed in it: nothing to do.
				continue
			}
			report(out, opts, "failed", f("kind", "day"), f("path", path), f("error", err))
			result.Failed++
			continue
		}
		// The line after the removal, as for every item.
		report(out, opts, action, f("kind", "day"), f("path", path), f("age", opts.Now.Sub(day)), f("bytes", 0))
		result.Removed++
	}
}

// isNotEmpty says whether a remove failed because the directory gained an
// entry (ENOTEMPTY, or EEXIST on the systems that report it so).
func isNotEmpty(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.ENOTEMPTY || errno == syscall.EEXIST
	}
	return false
}

// remove takes one candidate off the disk: a scoreboard file, a transcript
// pair (both files; one already gone is not an error), or an activity
// folder whole.
func remove(c candidate) error {
	switch c.kind {
	case "scoreboard":
		return os.Remove(c.path)
	case "transcript":
		var err error
		for _, p := range []string{c.path, c.pair} {
			if p == "" {
				continue
			}
			if e := os.Remove(p); e != nil && !os.IsNotExist(e) {
				err = e
			}
		}
		return err
	}
	return os.RemoveAll(c.path)
}

// keep writes the `kept` line of an examined item that stays, under
// --verbose: the reason is one word an operator can act on.
func keep(opts Options, out io.Writer, kind, path, reason string) {
	if opts.Verbose {
		report(out, opts, "kept", f("kind", kind), f("path", path), f("reason", reason))
	}
}

// ageString renders an age as an operator reads one: whole
// days from one day on, else whole hours, else minutes.
func ageString(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dm", int(d.Minutes()))
}

// owner is the uid of a file's owner, -1 when the platform does not say.
func owner(fi os.FileInfo) int {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}

// activityCandidates finds the job folders of a job tree that may go: a
// finished job by its summary, and an orphan, a folder without
// a summary (a job the daemon's death cut short, or one run with
// output.files.summary-json false), when its day is older than the
// retention age, nothing in it is newer than the cutoff, and its snapshot,
// if any, is not live. An orphan goes under the age
// rule only.
func activityCandidates(root string, cutoff time.Time, opts Options, out io.Writer) ([]candidate, int, int, error) {
	found := []candidate{}
	examined, skipped := 0, 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || path == root {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			skipped++
			keep(opts, out, "activity", path, "symlink")
			return filepath.SkipDir
		}
		// Only the YYMMDD day folders of osutil.DayFolderLayout are walked;
		// any other name under the root, an earlier layout's included, is
		// skipped.
		if filepath.Dir(path) == root && !osutil.IsDayFolder(d.Name()) {
			skipped++
			keep(opts, out, "activity", path, "not-a-day-folder")
			return filepath.SkipDir
		}
		summary := filepath.Join(path, "summary.json")
		var terminal bool
		var when time.Time
		b, e := os.ReadFile(summary)
		switch {
		case e == nil:
			examined++
			var s records.Summary
			if json.Unmarshal(b, &s) != nil {
				keep(opts, out, "activity", path, "unreadable")
			} else if !records.TerminalStatus(s.FinalStatus) {
				keep(opts, out, "activity", path, "live")
			} else {
				terminal = true
				when = s.EndedAt
			}
		case os.IsNotExist(e) && filepath.Dir(filepath.Dir(path)) == root:
			// An orphan: a job folder directly under a day folder with no
			// summary. Its day is the folder's name; its time the newest
			// thing in it.
			examined++
			day, _ := osutil.DayFolderDate(filepath.Base(filepath.Dir(path)))
			newest := newestTime(path)
			switch {
			case !day.Before(cutoff) || !newest.Before(cutoff):
				skipped++
				keep(opts, out, "orphan", path, "young")
				return filepath.SkipDir
			case snapshotLive(scoreboardRoots(opts), filepath.Base(path), cutoff):
				skipped++
				keep(opts, out, "orphan", path, "live")
				return filepath.SkipDir
			}
			fi, e := os.Stat(path)
			if e != nil {
				return e
			}
			found = append(found, candidate{path: path, mod: newest, size: dirSize(path), kind: "orphan", old: true, uid: owner(fi), ageOnly: true})
			return filepath.SkipDir
		}
		if !terminal {
			return nil
		}
		fi, e := os.Stat(path)
		if e != nil {
			return e
		}
		if when.IsZero() {
			when = fi.ModTime()
		}
		size := dirSize(path)
		found = append(found, candidate{path: path, mod: when, size: size, kind: "activity", old: when.Before(cutoff), uid: owner(fi)})
		return filepath.SkipDir
	})
	return found, examined, skipped, err
}

// transcriptCandidates finds ended login sessions below the YYMMDD day
// folders of the transcript tree: a metadata file with
// its end record and the transcript sharing its name, the candidate's path
// and pair.
func transcriptCandidates(root string, cutoff time.Time, opts Options, out io.Writer) ([]candidate, int, int, error) {
	days, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, 0, err
	}
	found := []candidate{}
	examined, skipped := 0, 0
	for _, day := range days {
		if !day.IsDir() || !osutil.IsDayFolder(day.Name()) {
			skipped++
			keep(opts, out, "transcript", filepath.Join(root, day.Name()), "not-a-day-folder")
			continue
		}
		entries, err := os.ReadDir(filepath.Join(root, day.Name()))
		if err != nil {
			return nil, examined, skipped, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.Contains(e.Name(), ".meta.") {
				continue
			}
			meta := filepath.Join(root, day.Name(), e.Name())
			examined++
			if !transcript.IsEnd(meta) {
				skipped++
				keep(opts, out, "transcript", meta, "live")
				continue
			}
			when, ok := transcript.EndedAt(meta)
			fi, err := e.Info()
			if err != nil {
				return nil, examined, skipped, err
			}
			if !ok {
				when = fi.ModTime()
			}
			size := fi.Size()
			pair := ""
			if t, ok := transcript.TranscriptFor(meta); ok {
				pair = t
				if tfi, err := os.Stat(t); err == nil {
					size += tfi.Size()
				}
			}
			found = append(found, candidate{path: meta, pair: pair, mod: when, size: size, kind: "transcript", old: when.Before(cutoff), uid: owner(fi)})
		}
	}
	return found, examined, skipped, nil
}

// scoreboardCandidates finds the terminal scoreboard files of the
// scoreboard directory, and the non-terminal ones last updated
// before the cutoff, which no live job holds:
// the file of a job the daemon's death cut short says running until the
// directory clears at a reboot. Those go under the age rule only.
func scoreboardCandidates(root string, cutoff time.Time, opts Options, out io.Writer) ([]candidate, int, int, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, 0, 0, err
	}
	found := []candidate{}
	examined, skipped := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		examined++
		path := filepath.Join(root, e.Name())
		b, er := os.ReadFile(path)
		if er != nil {
			skipped++
			keep(opts, out, "scoreboard", path, "unreadable")
			continue
		}
		var s records.ScoreboardSnapshot
		if json.Unmarshal(b, &s) != nil {
			skipped++
			keep(opts, out, "scoreboard", path, "unreadable")
			continue
		}
		fi, er := e.Info()
		if er != nil {
			return nil, examined, skipped, er
		}
		when := s.LastUpdatedAt
		if when.IsZero() {
			when = fi.ModTime()
		}
		if !records.TerminalStatus(s.Status) {
			if !when.Before(cutoff) {
				skipped++
				keep(opts, out, "scoreboard", path, "live")
				continue
			}
			found = append(found, candidate{path: path, mod: when, size: fi.Size(), kind: "stale-scoreboard", old: true, uid: owner(fi), ageOnly: true})
			continue
		}
		found = append(found, candidate{path: path, mod: when, size: fi.Size(), kind: "scoreboard", old: when.Before(cutoff), uid: owner(fi)})
	}
	return found, examined, skipped, nil
}

// newestTime is the latest modification time of a folder and everything
// in it: the last moment anything was written there.
func newestTime(root string) time.Time {
	var newest time.Time
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil {
			if fi, e := d.Info(); e == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		return nil
	})
	return newest
}

// snapshotLive says whether the scoreboard file of activity id, in any of
// roots, names a live job: a non-terminal status last updated after cutoff. A snapshot
// untouched for the retention age is no lease, by the reading the
// watch screen makes after watch.stale-after; a file that is absent,
// unreadable, or terminal is not live.
func snapshotLive(roots []string, id string, cutoff time.Time) bool {
	for _, root := range roots {
		b, err := os.ReadFile(filepath.Join(root, id+".json"))
		if err != nil {
			continue
		}
		var s records.ScoreboardSnapshot
		if json.Unmarshal(b, &s) != nil {
			continue
		}
		if !records.TerminalStatus(s.Status) && !s.LastUpdatedAt.Before(cutoff) {
			return true
		}
	}
	return false
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				total += fi.Size()
			}
		}
		return nil
	})
	return total
}
func freePercent(path string) float64 {
	var st syscall.Statfs_t
	if path == "" || syscall.Statfs(path, &st) != nil || st.Blocks == 0 {
		return 100
	}
	return float64(st.Bavail) * 100 / float64(st.Blocks)
}
