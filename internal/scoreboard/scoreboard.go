// Package scoreboard writes and reads credential-free atomic activity snapshots.
package scoreboard

import (
	"encoding/json"
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
// Every activity claims its ID in the same folder before its scoreboard
// is written (osutil.ClaimActivityID, the folder by Folder) and names the
// file with Use.
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
	var err error
	if w.Directory, w.Mode, err = Folder(raw, home, base, warn); err != nil {
		return nil, err
	}
	return w, nil
}

// Folder is NewWriter's folder whatever watch.enabled says, the place every
// activity claims its ID: the folder, made when missing, and the mode of
// the files in it (0640 in the shared folder, 0600 in the private one).
// warn is told when the shared folder is passed by; nil passes it by
// silently, for a caller whose scoreboard writer says it.
func Folder(raw, home, base string, warn func(string)) (string, os.FileMode, error) {
	f, err := Choice(raw, home, base)
	if err != nil {
		return "", 0, err
	}
	if f.Path != "" {
		err := osutil.MakeSharedDirectory(f.Path, 0770)
		if err == nil {
			err = osutil.UsableDirectory(f.Path)
		}
		if err == nil {
			return f.Path, 0640, nil
		}
		if f.Fallback == "" {
			return "", 0, errorcodes.Errorf("scoreboard_root_invalid", "the scoreboards %s cannot be used (%v); set scoreboards to a folder the operator can write", f.Path, err)
		}
		if warn != nil {
			warn(fmt.Sprintf("shared scoreboard directory unavailable (%s: %v); using private fallback %s", f.Path, err, f.Fallback))
		}
	}
	if err := osutil.MakeDirectories(f.Fallback, 0700); err != nil {
		return "", 0, err
	}
	if err := osutil.UsableDirectory(f.Fallback); err != nil {
		return "", 0, errorcodes.Errorf("scoreboard_root_invalid", "the private scoreboards %s cannot be used: %v", f.Fallback, err)
	}
	return f.Fallback, 0600, nil
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

// Use names the file of the activity's claimed ID, or of the ID the
// recording wrapper claimed (app.RecorderSessionIDEnv).
func (w *Writer) Use(activityID string) {
	if w.Enabled {
		w.Path = filepath.Join(w.Directory, activityID+".json")
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
	// The scoreboards alone: the claims beside them (osutil.ClaimExtension)
	// are neither read nor counted.
	files := []string{}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files = append(files, e.Name())
		}
	}
	if len(files) > max {
		return nil, errorcodes.Errorf("scoreboard_file_limit_exceeded", "scoreboard file count %d exceeds configured maximum %d", len(files), max)
	}
	rows := []Row{}
	now := time.Now()
	for _, name := range files {
		path := filepath.Join(directory, name)
		data, err := os.ReadFile(path)
		if err != nil {
			rows = append(rows, Row{Path: path, Error: err.Error()})
			continue
		}
		var snap records.ScoreboardSnapshot
		if err := json.Unmarshal(data, &snap); err != nil {
			rows = append(rows, Row{Path: path, Error: err.Error()})
			continue
		}
		rows = append(rows, Row{Snapshot: snap, Path: path, Stale: Stale(snap, staleAfter, now)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Snapshot.LastUpdatedAt.After(rows[j].Snapshot.LastUpdatedAt) })
	return rows, nil
}

// Stale is watch's rule (watch.stale-after): a snapshot whose status is not
// terminal and whose last update is older than staleAfter at now; a zero
// staleAfter judges none stale.
func Stale(s records.ScoreboardSnapshot, staleAfter time.Duration, now time.Time) bool {
	return staleAfter > 0 && now.Sub(s.LastUpdatedAt) > staleAfter && !records.TerminalStatus(s.Status)
}

// Find is the snapshot of activity id in dirs, the newer where two hold it,
// as ReadAll takes it; false when no folder holds one it can read and
// decode. It reads the one file, not the folder.
func Find(dirs []string, id string) (records.ScoreboardSnapshot, bool) {
	var found records.ScoreboardSnapshot
	ok := false
	for _, dir := range dirs {
		data, err := os.ReadFile(filepath.Join(dir, id+".json"))
		if err != nil {
			continue
		}
		var s records.ScoreboardSnapshot
		if json.Unmarshal(data, &s) != nil {
			continue
		}
		if !ok || s.LastUpdatedAt.After(found.LastUpdatedAt) {
			found, ok = s, true
		}
	}
	return found, ok
}
