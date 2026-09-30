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
