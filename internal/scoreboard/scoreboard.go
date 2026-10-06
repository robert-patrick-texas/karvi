// Package scoreboard writes and reads credential-free atomic activity snapshots.
package scoreboard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/records"
)

// Writer is one activity's scoreboard file: Directory the shared directory
// or the private fallback, Path the file once the activity's ID is known.
// An activity that reserves its ID elsewhere (a run or a cmd, by its job
// directory) names the file with Use; a login reserves its ID here, by the
// file itself (Reserve), so that every job ID on the watch screen has the
// one form YYMMDD-HHMMSS-xx.
type Writer struct {
	Directory, Path string
	Mode            os.FileMode
	Enabled         bool
	// mu serializes the writes: the activity's own on its events and the
	// heartbeat's between them, so the file always holds one whole snapshot
	// and the heartbeat never overtakes a later state with an older one.
	mu sync.Mutex
}

// NewWriter resolves the scoreboards (the scoreboards key, raw; home the
// operator's, base the private root) and makes the folder: under "auto"
// the scratch root's folder where the scratch root exists, made in it
// with the root's bits when missing (osutil.MakeSharedDirectory), passed
// by with one warning when it cannot be used; else, or then, the private
// <basedir>/state/scoreboards, made 0700. An explicit path is used or
// refused. A disabled writer (watch.enabled false) resolves nothing and
// writes nothing.
func NewWriter(raw, home, base string, enabled bool, warn func(string)) (*Writer, error) {
	w := &Writer{Enabled: enabled}
	if !enabled {
		return w, nil
	}
	f, err := Choice(raw, home, base)
	if err != nil {
		return nil, err
	}
	if f.Path != "" {
		err := osutil.MakeSharedDirectory(f.Path, 0770)
		if err == nil {
			err = osutil.UsableDirectory(f.Path)
		}
		if err == nil {
			w.Directory, w.Mode = f.Path, 0640
			return w, nil
		}
		if f.Fallback == "" {
			return nil, errorcodes.Errorf("scoreboard_root_invalid", "the scoreboards %s cannot be used (%v); set scoreboards to a folder the operator can write", f.Path, err)
		}
		if warn != nil {
			warn(fmt.Sprintf("shared scoreboard directory unavailable (%s: %v); using private fallback %s", f.Path, err, f.Fallback))
		}
	}
	if err := osutil.MakeDirectories(f.Fallback, 0700); err != nil {
		return nil, err
	}
	if err := osutil.UsableDirectory(f.Fallback); err != nil {
		return nil, errorcodes.Errorf("scoreboard_root_invalid", "the private scoreboards %s cannot be used: %v", f.Fallback, err)
	}
	w.Directory, w.Mode = f.Fallback, 0600
	return w, nil
}

// Choice is the scoreboards' chain (osutil.ScratchFolderChoice): the
// scratch root's folder, then the private one, or an explicit path.
func Choice(raw, home, base string) (osutil.ScratchFolder, error) {
	return osutil.ScratchFolderChoice(raw, home, "scoreboards", "scoreboards", base)
}

// Place is NewWriter's twin: the folder an activity would write its
// scoreboard in, and the shared folder passed by, creating nothing.
func Place(raw, home, base string) (osutil.Place, error) {
	f, err := Choice(raw, home, base)
	if err != nil {
		return osutil.Place{}, err
	}
	return f.Place(nil, "scoreboard_root_invalid")
}

// Directories are the folders `karvi watch` reads, making nothing: every
// place of the chain that exists, the shared folder and the operator's
// private fallback, or the explicit path alone.
func Directories(raw, home, base string) ([]string, error) {
	f, err := Choice(raw, home, base)
	if err != nil {
		return nil, err
	}
	if f.Fallback == "" {
		return []string{f.Path}, nil
	}
	var dirs []string
	for _, p := range []string{f.Path, f.Fallback} {
		if fi, err := os.Stat(p); p != "" && err == nil && fi.IsDir() {
			dirs = append(dirs, p)
		}
	}
	return dirs, nil
}

// Use names the file of an activity whose ID was reserved elsewhere, or
// chosen by the recording wrapper (app.RecorderSessionIDEnv).
func (w *Writer) Use(activityID string) {
	if w.Enabled {
		w.Path = filepath.Join(w.Directory, activityID+".json")
	}
}

// Reserve takes the first free job ID of now for an activity whose only
// file is its scoreboard, a login: the file is created exclusively, empty,
// sequence 00 first and the next on "exists", through the one sequence
// loop the job directory uses (osutil.ReserveSequence). The first Write
// replaces the empty file by rename. A disabled writer reserves nothing and
// gives the unreserved ID of the second, as an activity that writes no
// files does.
func (w *Writer) Reserve(now time.Time, loc *time.Location) (string, error) {
	if !w.Enabled {
		return osutil.UnreservedJobID(now, loc), nil
	}
	return osutil.ReserveSequence(now, loc, w.Directory, func(id string) error {
		path := filepath.Join(w.Directory, id+".json")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, w.Mode)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return err
			}
			return errorcodes.Errorf("scoreboard_write_failed", "cannot create %s: %w", path, err)
		}
		f.Close()
		w.Path = path
		return nil
	})
}

// Release removes a reservation that never became a snapshot: the file
// only while it is still empty, so a written scoreboard is never touched.
// A login that fails before its first Write, or a recording wrapper whose
// child never ran, leaves nothing behind.
func (w *Writer) Release() {
	if !w.Enabled || w.Path == "" {
		return
	}
	if fi, err := os.Stat(w.Path); err == nil && fi.Size() == 0 {
		_ = os.Remove(w.Path)
	}
}

func (w *Writer) Write(s records.ScoreboardSnapshot) error {
	if !w.Enabled {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.write(s)
}

func (w *Writer) write(s records.ScoreboardSnapshot) error {
	if err := s.Validate(); err != nil {
		return err
	}
	return osutil.AtomicJSON(w.Path, s, w.Mode)
}

// Heartbeat rewrites the activity's snapshot every interval while it
// runs: snapshot gives the activity's state now, and
// each beat writes it with a fresh last_updated_at and elapsed time, so a
// login longer than watch.stale-after, or a run whose devices are quiet
// that long, no longer shows stale on the watch screen; the beat is also
// what carries the running byte count between the activity's own
// events. The interval is watch.refresh, the screen's own. The returned
// stop ends the beats and waits for one in progress, so the final snapshot
// the activity writes after it is the last word. A disabled writer beats
// nothing. Errors of a beat are not reported: the next beat, or the
// activity's own write, tries again.
func (w *Writer) Heartbeat(every time.Duration, snapshot func() records.ScoreboardSnapshot) (stop func()) {
	if !w.Enabled || every <= 0 {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(every)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				w.mu.Lock()
				s := snapshot()
				now := time.Now()
				s.LastUpdatedAt = now
				if !s.StartedAt.IsZero() {
					s.ElapsedNS = now.Sub(s.StartedAt).Nanoseconds()
				}
				_ = w.write(s)
				w.mu.Unlock()
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}
func (w *Writer) Remove() error {
	if !w.Enabled {
		return nil
	}
	return os.Remove(w.Path)
}

// ReadAll is Read over every folder of dirs, one row per job: a job found
// in two (its ID the file's name) is the newer snapshot. Of several
// folders, one the operator cannot read is passed by, as the shared
// folder closed to the operator is; a single folder's error is returned.
func ReadAll(dirs []string, max int, staleAfter time.Duration) ([]Row, error) {
	byJob := map[string]Row{}
	for _, dir := range dirs {
		rows, err := Read(dir, max, staleAfter)
		if err != nil {
			if len(dirs) > 1 && os.IsPermission(err) {
				continue
			}
			return nil, err
		}
		for _, r := range rows {
			id := strings.TrimSuffix(filepath.Base(r.Path), ".json")
			if prior, ok := byJob[id]; ok && !r.Snapshot.LastUpdatedAt.After(prior.Snapshot.LastUpdatedAt) {
				continue
			}
			byJob[id] = r
		}
	}
	rows := make([]Row, 0, len(byJob))
	for _, r := range byJob {
		rows = append(rows, r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Snapshot.LastUpdatedAt.After(rows[j].Snapshot.LastUpdatedAt) })
	return rows, nil
}

type Row struct {
	Snapshot records.ScoreboardSnapshot
	Path     string
	Stale    bool
	Error    string
}

func Read(directory string, max int, staleAfter time.Duration) ([]Row, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if max <= 0 {
		max = 10000
	}
	if len(entries) > max {
		return nil, errorcodes.Errorf("scoreboard_file_limit_exceeded", "scoreboard file count %d exceeds configured maximum %d", len(entries), max)
	}
	rows := []Row{}
	now := time.Now()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(directory, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			rows = append(rows, Row{Path: path, Error: err.Error()})
			continue
		}
		// An empty file is a login's reservation whose first snapshot is
		// moments away (Writer.Reserve): not a row, and not invalid.
		if len(data) == 0 {
			continue
		}
		var snap records.ScoreboardSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			rows = append(rows, Row{Path: path, Error: err.Error()})
			continue
		}
		rows = append(rows, Row{Snapshot: snap, Path: path, Stale: staleAfter > 0 && now.Sub(snap.LastUpdatedAt) > staleAfter && !records.TerminalStatus(snap.Status)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Snapshot.LastUpdatedAt.After(rows[j].Snapshot.LastUpdatedAt) })
	return rows, nil
}
