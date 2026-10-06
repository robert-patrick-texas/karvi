package scoreboard

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestHeartbeatRewritesTheSnapshot: while an
// activity runs its snapshot is rewritten every interval with a fresh
// last_updated_at and elapsed time, from the activity's state now; stop
// ends the beats, and the activity's final write is the last word.
func TestHeartbeatRewritesTheSnapshot(t *testing.T) {
	w, err := NewWriter(t.TempDir(), "", "", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	w.Use("260927-120000-00")
	started := time.Now().Add(-time.Minute)
	state := records.ScoreboardSnapshot{ActivityID: "260927-120000-00", Operator: records.Operator{Username: "u"}, ActivityType: "login", Mode: "login", Status: "running", Counts: records.Counts{Total: 1, InFlight: 1}, Targets: []records.ScoreboardTarget{{Name: "r1", State: records.TargetRunning}}, StartedAt: started, LastUpdatedAt: started}
	if err := w.Write(state); err != nil {
		t.Fatal(err)
	}
	read := func() records.ScoreboardSnapshot {
		data, err := os.ReadFile(w.Path)
		if err != nil {
			t.Fatal(err)
		}
		var s records.ScoreboardSnapshot
		if err := json.Unmarshal(data, &s); err != nil {
			t.Fatalf("%v: %s", err, data)
		}
		return s
	}
	if got := read(); !got.LastUpdatedAt.Equal(started) {
		t.Fatalf("before the beat last_updated_at %v", got.LastUpdatedAt)
	}
	beats := 0
	stop := w.Heartbeat(20*time.Millisecond, func() records.ScoreboardSnapshot {
		beats++
		s := state
		s.Targets = []records.ScoreboardTarget{{Name: "r1", State: records.TargetRunning, Bytes: int64(beats) * 1000}}
		return s
	})
	time.Sleep(120 * time.Millisecond)
	beat := read()
	if !beat.LastUpdatedAt.After(started.Add(30*time.Second)) || beat.ElapsedNS < int64(59*time.Second) || beat.Status != "running" {
		t.Fatalf("the beat did not refresh the snapshot: last_updated_at %v elapsed %s status %s", beat.LastUpdatedAt, time.Duration(beat.ElapsedNS), beat.Status)
	}
	if beat.Targets[0].Bytes == 0 {
		t.Fatalf("the beat did not carry the activity's state now: %+v", beat.Targets)
	}
	stop()
	beatsAtStop := beats
	final := state
	ended := time.Now()
	final.Status, final.EndedAt, final.LastUpdatedAt = "completed", &ended, ended
	final.Counts = records.Counts{Total: 1, Completed: 1, Succeeded: 1}
	final.Targets[0].State = records.TargetSucceeded
	if err := w.Write(final); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	if got := read(); got.Status != "completed" || beats != beatsAtStop {
		t.Fatalf("after stop: status %s, beats %d then %d", got.Status, beatsAtStop, beats)
	}
	// A disabled writer beats nothing and stop is a no-op.
	off, _ := NewWriter("", "", "", false, nil)
	off.Heartbeat(time.Millisecond, func() records.ScoreboardSnapshot { t.Fatal("a disabled writer beat"); return state })()
}
