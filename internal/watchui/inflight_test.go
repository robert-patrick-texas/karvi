package watchui

import (
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/records"
)

// TestPaneShowsTheBytesInFlight: a running target
// shows the settled bytes of its command beside its state, other targets
// do not, and the metrics line shows the in-flight total beside the output
// bytes when there is one.
func TestPaneShowsTheBytesInFlight(t *testing.T) {
	if got := targetCell(records.ScoreboardTarget{Name: "r1", State: records.TargetRunning, Bytes: 5256000}); got != "r1 running 5256000" {
		t.Fatalf("running cell %q", got)
	}
	if got := targetCell(records.ScoreboardTarget{Name: "r1", State: records.TargetRunning}); got != "r1 running" {
		t.Fatalf("running cell without bytes %q", got)
	}
	if got := targetCell(records.ScoreboardTarget{Name: "r2", State: records.TargetQueued, Bytes: 12}); got != "r2 queued" {
		t.Fatalf("queued cell %q", got)
	}
	m := model(120, 24)
	s := &m.Rows[1].Snapshot
	s.Metrics = &records.ScoreboardMetrics{OutputBytes: 15012, InFlightBytes: 5260096}
	s.Targets = []records.ScoreboardTarget{{Name: "gtn-a", State: "succeeded"}, {Name: "gtn-wan-9500-1", State: "running", Bytes: 5256000}, {Name: "gtn-c", State: "running", Bytes: 4096}}
	m.Frame()
	m.Key(kind(KeyEnter))
	frame := strings.Join(m.Frame(), "\n")
	for _, want := range []string{"output 15012 bytes  in flight 5260096 bytes", "gtn-wan-9500-1 running 5256000", "gtn-c running 4096", "gtn-a succeeded"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("the pane lacks %q:\n%s", want, frame)
		}
	}
	s.Metrics.InFlightBytes = 0
	if frame := strings.Join(m.Frame(), "\n"); strings.Contains(frame, "in flight") {
		t.Fatal("the metrics line shows an in-flight total of zero")
	}
}
