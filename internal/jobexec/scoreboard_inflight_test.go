package jobexec

import (
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestScoreboardSnapshotCarriesTheInFlightBytes is the in-flight counter
// at the job's board: a running target's row carries its device's count
// at the snapshot, a target that is not running 0 whatever its counter
// holds, and the metrics' in_flight_bytes is the rows' sum.
func TestScoreboardSnapshotCarriesTheInFlightBytes(t *testing.T) {
	st := &scoreboardState{inFlight: &output.InFlight{}, index: map[string]int{"a": 0, "b": 1, "c": 2}}
	st.snap = records.ScoreboardSnapshot{Targets: []records.ScoreboardTarget{{Name: "a", State: records.TargetRunning}, {Name: "b", State: records.TargetRunning}, {Name: "c", State: records.TargetQueued}}, Metrics: &records.ScoreboardMetrics{}}
	st.inFlight.Counter("a").Store(5256000)
	st.inFlight.Counter("b").Store(4096)
	st.inFlight.Counter("c").Store(99) // stale: a queued target has no command in flight
	snap := st.snapshot()
	if snap.Targets[0].Bytes != 5256000 || snap.Targets[1].Bytes != 4096 || snap.Targets[2].Bytes != 0 {
		t.Fatalf("target bytes %+v", snap.Targets)
	}
	if snap.Metrics.InFlightBytes != 5256000+4096 {
		t.Fatalf("in_flight_bytes %d", snap.Metrics.InFlightBytes)
	}
	if snap.SchemaVersion != 0 && snap.SchemaVersion != records.ScoreboardSchemaVersion {
		t.Fatalf("schema %d", snap.SchemaVersion)
	}
	// The board's own state is untouched: the copy carried the counts.
	if st.snap.Targets[0].Bytes != 0 || st.snap.Metrics.InFlightBytes != 0 {
		t.Fatal("the snapshot wrote into the board's state")
	}
}
