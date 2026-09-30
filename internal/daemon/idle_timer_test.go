package daemon

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/testsocket"
)

// TestIdleTimer covers daemon.shutdown-idle-timer on a clock the test
// moves: the check on the ticker is stopIfIdle,
// called here directly. The daemon stays while the timer has not run out,
// while a job is active, while a preparation is live, and when a request
// other than ping or status restarted the clock; ping and status restart
// nothing; and it leaves, exit 0 as "daemon stop" would, once the timer has
// run out with nothing in flight. A zero timer never leaves.
func TestIdleTimer(t *testing.T) {
	// The clock the test moves, read by the server's goroutines.
	var mu sync.Mutex
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
	advance := func(d time.Duration) { mu.Lock(); defer mu.Unlock(); now = now.Add(d) }
	dir := testsocket.Dir(t)
	socket := filepath.Join(dir, "daemon.sock")
	s := &Server{Socket: socket, StatePath: filepath.Join(dir, "state.json"), UID: os.Geteuid(), IdleTimeout: time.Hour, Clock: clock}
	done := make(chan error, 1)
	go func() { done <- s.Serve(context.Background()) }()
	waitCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := ipc.Wait(waitCtx, socket); err != nil {
		t.Fatal(err)
	}
	running := func(step string) {
		t.Helper()
		s.stopIfIdle()
		if s.draining.Load() {
			t.Fatalf("%s: the daemon is draining", step)
		}
	}
	advance(59 * time.Minute)
	running("59 minutes idle")
	advance(2 * time.Minute)
	// An hour past the start with a job active: stays, and the job's end
	// restarts the clock.
	s.active.Store(1)
	running("61 minutes, one active job")
	s.active.Store(0)
	s.touch()
	running("the job just ended")
	// A live preparation is a job coming: stays.
	advance(2 * time.Hour)
	s.preparations.mu.Lock()
	s.preparations.entries["p1"] = &preparation{expiresAt: clock().Add(time.Hour)}
	s.preparations.mu.Unlock()
	running("2 hours idle, one live preparation")
	s.preparations.mu.Lock()
	delete(s.preparations.entries, "p1")
	s.preparations.mu.Unlock()
	// ping and status over the socket restart nothing.
	ctx, cancelPing := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelPing()
	if _, err := Ping(ctx, socket, 1<<20); err != nil {
		t.Fatal(err)
	}
	if got := time.Unix(0, s.lastActive.Load()); !got.Equal(clock().Add(-2 * time.Hour)) {
		t.Fatalf("ping moved the idle clock to %s", got)
	}
	// Any other request does: a cancel of a job the daemon does not hold is
	// refused, and is still a request.
	if _, err := CancelJob(ctx, socket, 1<<20, ipc.CancelRequest{JobID: "none"}); err == nil {
		t.Fatal("a cancel of an unknown job succeeded")
	}
	if got := time.Unix(0, s.lastActive.Load()); !got.Equal(clock()) {
		t.Fatalf("a request left the idle clock at %s, want %s", got, clock())
	}
	running("just after a request")
	// A zero timer never leaves.
	s.IdleTimeout = 0
	advance(30 * 24 * time.Hour)
	running("30 days idle, timer 0")
	// The timer runs out with nothing in flight: the daemon leaves, exit 0.
	s.IdleTimeout = time.Hour
	s.stopIfIdle()
	if !s.draining.Load() {
		t.Fatal("the daemon did not stop")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve returned %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return")
	}
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatalf("the socket remains: %v", err)
	}
}
