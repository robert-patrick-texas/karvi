package dispatch

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fake struct {
	fail      map[string]bool
	errorCode map[string]string
}

func (f fake) Execute(_ context.Context, t Task, _ Context) Result {
	return Result{Task: t, Success: !f.fail[t.Key], ErrorCode: f.errorCode[t.Key], StartedAt: time.Now(), EndedAt: time.Now()}
}

func TestHostKeyMismatchPolicyHaltsNewScheduling(t *testing.T) {
	tasks := []Task{{Key: "changed-key", Position: 1}, {Key: "not-started", Position: 2}}
	s := LocalDispatcher{}.Execute(
		context.Background(),
		Plan{Mode: "serial", Tasks: tasks, HaltErrorCodes: []string{"host_key_changed"}},
		fake{fail: map[string]bool{"changed-key": true}, errorCode: map[string]string{"changed-key": "host_key_changed"}},
		nil,
	)
	if s.HaltReason != "host_key_mismatch" {
		t.Fatalf("halt reason = %q", s.HaltReason)
	}
	if s.Counts.Terminal != 1 || s.Counts.NotStarted != 1 {
		t.Fatalf("unexpected counts: %+v", s.Counts)
	}
}

type inflightMismatchExecutor struct {
	mu       sync.Mutex
	started  []string
	bStarted chan struct{}
	releaseB chan struct{}
	once     sync.Once
}

func (e *inflightMismatchExecutor) Execute(_ context.Context, task Task, _ Context) Result {
	e.mu.Lock()
	e.started = append(e.started, task.Key)
	e.mu.Unlock()
	switch task.Key {
	case "a":
		<-e.bStarted
		return Result{Task: task, Success: false, ErrorCode: "host_key_changed", StartedAt: time.Now(), EndedAt: time.Now()}
	case "b":
		e.once.Do(func() { close(e.bStarted) })
		<-e.releaseB
		return Result{Task: task, Success: true, StartedAt: time.Now(), EndedAt: time.Now()}
	default:
		return Result{Task: task, Success: true, StartedAt: time.Now(), EndedAt: time.Now()}
	}
}

func TestHostKeyMismatchLetsInflightFinishButStartsNoMore(t *testing.T) {
	executor := &inflightMismatchExecutor{bStarted: make(chan struct{}), releaseB: make(chan struct{})}
	done := make(chan Summary, 1)
	go func() {
		done <- LocalDispatcher{}.Execute(
			context.Background(),
			Plan{Mode: "parallel", Width: 2, Tasks: []Task{
				{Key: "a", Position: 1}, {Key: "b", Position: 2},
				{Key: "c", Position: 3}, {Key: "d", Position: 4},
			}, HaltErrorCodes: []string{"host_key_changed"}},
			executor,
			nil,
		)
	}()
	<-executor.bStarted
	// Give the mismatch result time to set the scheduling stop before allowing
	// the already-running peer to finish.
	time.Sleep(25 * time.Millisecond)
	close(executor.releaseB)
	summary := <-done
	executor.mu.Lock()
	started := append([]string(nil), executor.started...)
	executor.mu.Unlock()
	if len(started) != 2 {
		t.Fatalf("started=%v, want exactly the two in-flight tasks", started)
	}
	if summary.HaltReason != "host_key_mismatch" {
		t.Fatalf("halt reason=%q", summary.HaltReason)
	}
	if summary.Counts.Terminal != 2 || summary.Counts.Succeeded != 1 || summary.Counts.Failed != 1 || summary.Counts.NotStarted != 2 {
		t.Fatalf("unexpected counts: %+v", summary.Counts)
	}
}
func TestRamp(t *testing.T) {
	r := BoundedRamp{Threshold: 75, Zone: 10, UpPercent: 50, DownPercent: 10}
	if got := r.NextWidth(20, 40, 100, 20); got != 30 {
		t.Fatalf("up=%d", got)
	}
	if got := r.NextWidth(100, 95, 100, 20); got != 90 {
		t.Fatalf("down=%d", got)
	}
	if got := r.NextWidth(20, 95, 100, 20); got != 20 {
		t.Fatalf("floor=%d", got)
	}
}
func TestParallelHalt(t *testing.T) {
	tasks := []Task{}
	for i := 0; i < 20; i++ {
		tasks = append(tasks, Task{Key: string(rune('a' + i)), Position: i + 1})
	}
	s := LocalDispatcher{}.Execute(context.Background(), Plan{Mode: "parallel", Tasks: tasks, Width: 2, HaltErrorCount: 1}, fake{fail: map[string]bool{"a": true}}, nil)
	if s.Counts.Failed < 1 || s.HaltReason != "error_count" {
		t.Fatalf("%+v", s)
	}
	if s.Counts.NotStarted == 0 {
		t.Fatalf("expected unstarted: %+v", s.Counts)
	}
}

func TestSerialFailureContinuesWithoutHaltPolicy(t *testing.T) {
	tasks := []Task{
		{Key: "changed-key", Position: 1},
		{Key: "next-device", Position: 2},
	}

	s := LocalDispatcher{}.Execute(
		context.Background(),
		Plan{Mode: "serial", Tasks: tasks},
		fake{fail: map[string]bool{"changed-key": true}},
		nil,
	)

	if s.HaltReason != "" {
		t.Fatalf("unexpected halt after a device-local failure: %q", s.HaltReason)
	}
	if s.Counts.Terminal != 2 || s.Counts.Failed != 1 || s.Counts.Succeeded != 1 || s.Counts.NotStarted != 0 {
		t.Fatalf("unexpected counts: %+v", s.Counts)
	}
	if len(s.Results) != 2 || s.Results[0].Task.Key != "changed-key" || s.Results[1].Task.Key != "next-device" {
		t.Fatalf("dispatcher did not continue in task order: %+v", s.Results)
	}
}

// TestStartWidthAndDescribe: the width a job starts at and its one-line
// description, after Execute's normalization: serial is one worker
// whatever the pool says, parallel its pool cut to the absolute ceiling,
// wave its start width (and neither names the parallel pool, which a wave
// job never uses).
func TestStartWidthAndDescribe(t *testing.T) {
	for _, c := range []struct {
		plan     Plan
		width    int
		describe string
	}{
		{Plan{Mode: "serial", Width: 4}, 1, "serial width=1"},
		{Plan{Mode: "parallel", Width: 4}, 4, "parallel width=4"},
		{Plan{Mode: "parallel", Width: 600, AbsoluteMaxWidth: 512}, 512, "parallel width=512"},
		{Plan{Mode: "wave", Width: 4, WaveStartWidth: 16, WaveMaxWidth: 32}, 16, "wave start-width=16 max-width=32 depth-multiplier=4"},
		{Plan{Mode: "wave", Width: 4, WaveStartWidth: 16, WaveMaxWidth: 32, WaveDepthMultiplier: 2}, 16, "wave start-width=16 max-width=32 depth-multiplier=2"},
	} {
		if got := c.plan.StartWidth(); got != c.width {
			t.Errorf("%+v: start width %d, want %d", c.plan, got, c.width)
		}
		if got := c.plan.Describe(); got != c.describe {
			t.Errorf("%+v: %q, want %q", c.plan, got, c.describe)
		}
	}
}

// TestWaveDecisionReasons: each decision names where the CPU stood and
// what the width did, with the width before it: a signal under the band
// steps up, then at the ceiling holds as cpu_below_zone_at_ceiling (not
// cpu_in_zone); a signal over the band at the start width holds as
// cpu_above_zone_at_floor; in the band it holds as cpu_in_zone.
func TestWaveDecisionReasons(t *testing.T) {
	run := func(signal float64, start, ceiling, devices int) []Event {
		tasks := make([]Task, devices)
		for i := range tasks {
			tasks[i] = Task{Key: string(rune('a' + i%26)), Position: i + 1}
		}
		var mu sync.Mutex
		var decisions []Event
		sink := EventSinkFunc(func(e Event) {
			if e.Kind == "wave_decision" {
				mu.Lock()
				decisions = append(decisions, e)
				mu.Unlock()
			}
		})
		LocalDispatcher{Signal: SignalFunc(func() float64 { return signal })}.Execute(context.Background(), Plan{Mode: "wave", Tasks: tasks, WaveStartWidth: start, WaveMaxWidth: ceiling, WaveDepthMultiplier: 1}, fake{}, sink)
		return decisions
	}
	type step struct {
		previous, width int
		reason          string
	}
	steps := func(es []Event) []step {
		out := make([]step, len(es))
		for i, e := range es {
			out[i] = step{e.PreviousWidth, e.Width, e.Reason}
		}
		return out
	}
	for _, c := range []struct {
		name   string
		signal float64
		want   []step
	}{
		{"under the band", 10, []step{{2, 3, "cpu_below_zone"}, {3, 4, "cpu_below_zone"}, {4, 4, "cpu_below_zone_at_ceiling"}}},
		{"over the band at the floor", 95, []step{{2, 2, "cpu_above_zone_at_floor"}, {2, 2, "cpu_above_zone_at_floor"}, {2, 2, "cpu_above_zone_at_floor"}}},
		{"in the band", 75, []step{{2, 2, "cpu_in_zone"}, {2, 2, "cpu_in_zone"}, {2, 2, "cpu_in_zone"}}},
	} {
		// Depth equals width (multiplier 1); devices for three decisions
		// and a last wave: 2+3+4+4 under the band, 2+2+2+2 otherwise.
		devices := 8
		if c.signal < 65 {
			devices = 13
		}
		got := steps(run(c.signal, 2, 4, devices))
		if len(got) < 3 {
			t.Errorf("%s: decisions %+v", c.name, got)
			continue
		}
		for i, w := range c.want {
			if got[i] != w {
				t.Errorf("%s: decision %d %+v, want %+v (all %+v)", c.name, i+1, got[i], w, got)
			}
		}
	}
	// A step up, then over the band: a step down, two cooldown decisions
	// that keep the width, a step down to the start width with its own two,
	// and the floor.
	signals := []float64{10, 95, 95, 95, 95, 95}
	var n int
	var seq []Event
	tasks := make([]Task, 40)
	for i := range tasks {
		tasks[i] = Task{Key: "d", Position: i + 1}
	}
	LocalDispatcher{Signal: SignalFunc(func() float64 {
		v := signals[min(n, len(signals)-1)]
		n++
		return v
	})}.Execute(context.Background(), Plan{Mode: "wave", Tasks: tasks, WaveStartWidth: 4, WaveMaxWidth: 8, WaveDepthMultiplier: 1, CooldownWaves: 2}, fake{}, EventSinkFunc(func(e Event) {
		if e.Kind == "wave_decision" {
			seq = append(seq, e)
		}
	}))
	want := []step{{4, 6, "cpu_below_zone"}, {6, 5, "cpu_above_zone"}, {5, 5, "cooldown"}, {5, 5, "cooldown"}, {5, 4, "cpu_above_zone"}, {4, 4, "cooldown"}, {4, 4, "cooldown"}, {4, 4, "cpu_above_zone_at_floor"}}
	if got := steps(seq); len(got) < len(want) {
		t.Fatalf("decisions %+v", got)
	} else {
		for i, w := range want {
			if got[i] != w {
				t.Errorf("down and cooldown: decision %d %+v, want %+v (all %+v)", i+1, got[i], w, got)
			}
		}
	}
}
