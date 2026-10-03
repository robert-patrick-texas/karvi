package jobexec

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/robert-patrick-texas/karvi/dispatch"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// scoreboardState is a job's schema 2 snapshot between the dispatcher's
// events: the targets and their states, the
// durations of the devices that finished, and what the store holds. The
// dispatcher's sink calls apply from its workers, so the state is locked;
// snapshot returns a copy for the writer.
type scoreboardState struct {
	mu       sync.Mutex
	snap     records.ScoreboardSnapshot
	index    map[string]int // target ID -> position in snap.Targets
	finished int
	sumMS    int64
	minMS    int64
	maxMS    int64
	store    *output.Store
	collect  bool
	// inFlight is each device's running byte count,
	// read at each snapshot into the target rows and the metrics.
	inFlight *output.InFlight
}

// scoreboardMode is the MODE the screen shows: what the
// operator ran, not the activity type alone; a crun is told by its
// collection's word, since a run or command with --cd collects too.
func scoreboardMode(req Request) string {
	switch {
	case req.Mode == executionplan.ModeExercise:
		return "exercise"
	case req.Plan.Output.Crun():
		return "crun"
	case req.ActivityType == "command":
		return "cmd"
	}
	return "run"
}

func newScoreboardState(req Request, id, jobID string, now time.Time, producer records.Producer, store *output.Store, startWidth int) *scoreboardState {
	plan := req.Plan
	mode := plan.Dispatch.Mode
	targets := make([]records.ScoreboardTarget, len(plan.Targets))
	index := make(map[string]int, len(plan.Targets))
	for i, t := range plan.Targets {
		targets[i] = records.ScoreboardTarget{Name: t.Device.CanonicalName, State: records.TargetQueued}
		index[t.TargetID] = i
	}
	inputs := plan.Sources.Inputs
	if inputs == nil {
		inputs = []executionplan.ScopeInput{}
	}
	st := &scoreboardState{index: index, store: store, collect: plan.Output.Collection != nil, inFlight: &output.InFlight{}}
	st.snap = records.ScoreboardSnapshot{
		SchemaVersion: records.ScoreboardSchemaVersion, ActivityID: id, JobID: jobID, Operator: osutil.RecordOperator(req.Operator),
		ActivityType: req.ActivityType, Mode: scoreboardMode(req), Status: "initializing", DispatchMode: &mode, Width: maxInt(1, startWidth),
		Counts:  records.Counts{Total: len(plan.Targets), NotStarted: len(plan.Targets)},
		Targets: targets, Inputs: inputs,
		Commands:  &records.ScoreboardCommands{Count: plan.CommandCount(), File: plan.CommandsFile},
		Metrics:   &records.ScoreboardMetrics{},
		Daemon:    req.Daemon,
		StartedAt: now, LastUpdatedAt: now, Producer: producer,
	}
	if c := plan.Output.Collection; c != nil {
		st.snap.Collection = &records.ScoreboardCollection{Directory: filepath.Base(c.Directory)}
	}
	return st
}

// apply records a device's start or end from the dispatcher's event; the
// counts, wave, and width come with every event as before.
func (st *scoreboardState) apply(e dispatch.Event, started time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.snap.Status = "running"
	st.snap.LastUpdatedAt = time.Now()
	st.snap.ElapsedNS = time.Since(started).Nanoseconds()
	st.snap.WaveNumber, st.snap.Width, st.snap.Depth = e.Wave, maxInt(1, e.Width), e.Depth
	st.snap.Counts = records.Counts{Total: e.Counts.Total, Completed: e.Counts.Terminal, Succeeded: e.Counts.Succeeded, Failed: e.Counts.Failed, NotStarted: e.Counts.NotStarted, InFlight: e.Counts.Inflight}
	if e.Task == nil {
		return
	}
	i, ok := st.index[e.Task.Key]
	if !ok {
		return
	}
	switch e.Kind {
	case "task_started":
		st.snap.Targets[i].State = records.TargetRunning
	case "task_terminal":
		// The executor's cancelStatus: a device the cancel interrupted
		// ends with the code cancelled, one the shutdown interrupted with
		// shutdown_incomplete; both are the counts' cancelled and
		// incomplete, not failures.
		state := records.TargetFailed
		switch {
		case e.Result != nil && e.Result.Success:
			state = records.TargetSucceeded
		case e.Result != nil && e.Result.ErrorCode == "cancelled":
			state = records.TargetCancelled
		case e.Result != nil && e.Result.ErrorCode == "shutdown_incomplete":
			state = records.TargetIncomplete
		}
		st.snap.Targets[i].State = state
		if e.Result != nil && !e.Result.EndedAt.Before(e.Result.StartedAt) {
			ms := e.Result.EndedAt.Sub(e.Result.StartedAt).Milliseconds()
			if st.finished == 0 || ms < st.minMS {
				st.minMS = ms
			}
			if ms > st.maxMS {
				st.maxMS = ms
			}
			st.finished++
			st.sumMS += ms
		}
	}
}

// finishQueued gives every device the job never started its final state:
// cancelled or incomplete when the job ended so, queued otherwise (a halt
// or a wave gate left it).
func (st *scoreboardState) finishQueued(unstartedStatus string) {
	st.mu.Lock()
	defer st.mu.Unlock()
	state := ""
	switch unstartedStatus {
	case "cancelled":
		state = records.TargetCancelled
	case "incomplete_shutdown":
		state = records.TargetIncomplete
	default:
		return
	}
	for i := range st.snap.Targets {
		if st.snap.Targets[i].State == records.TargetQueued {
			st.snap.Targets[i].State = state
		}
	}
}

// snapshot is a copy for the writer, with the metrics and the collection
// counts read from the store at this moment.
func (st *scoreboardState) snapshot() records.ScoreboardSnapshot {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.snap
	out.Targets = append([]records.ScoreboardTarget(nil), st.snap.Targets...)
	// The bytes of each device's command in flight, read now:
	// a device that is not running shows 0.
	m := *st.snap.Metrics
	m.InFlightBytes = 0
	for i := range out.Targets {
		out.Targets[i].Bytes = 0
		if out.Targets[i].State == records.TargetRunning {
			out.Targets[i].Bytes = st.inFlight.Bytes(out.Targets[i].Name)
			m.InFlightBytes += out.Targets[i].Bytes
		}
	}
	m.Finished, m.MinMS, m.MaxMS = st.finished, st.minMS, st.maxMS
	if st.finished > 0 {
		m.AvgMS = st.sumMS / int64(st.finished)
	}
	if st.store != nil {
		m.OutputBytes = st.store.Bytes()
	}
	out.Metrics = &m
	if st.collect && st.snap.Collection != nil {
		c := *st.snap.Collection
		if st.store != nil {
			if cs := st.store.CollectionSummary(); cs != nil {
				c.Replaced, c.Kept = cs.Replaced, cs.Kept
			}
		}
		out.Collection = &c
	}
	return out
}
