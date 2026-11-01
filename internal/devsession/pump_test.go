package devsession

import (
	"bytes"
	"context"
	"io"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

// lateOutput is a device stream that gives its prompt, if any, then nothing
// until the session closes or aborts it; then it gives what the transport
// still held, more chunks than the pump's channel holds, and ends.
type lateOutput struct {
	prompt string
	left   int
	closed chan struct{}
	once   sync.Once
}

func (l *lateOutput) Read(p []byte) (int, error) {
	if l.prompt != "" {
		n := copy(p, l.prompt)
		l.prompt = l.prompt[n:]
		return n, nil
	}
	<-l.closed
	if l.left == 0 {
		return 0, io.EOF
	}
	l.left--
	return copy(p, bytes.Repeat([]byte("x"), len(p))), nil
}

func (l *lateOutput) Write(p []byte) (int, error) { return len(p), nil }

func (l *lateOutput) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}

// pumpsRunning is the goroutine profile's groups of Session.pump carrying
// the label, and how many goroutines they hold.
func pumpsRunning(label string) (int, string) {
	var b bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&b, 1)
	n, groups := 0, ""
	for _, group := range strings.Split(b.String(), "\n\n") {
		if !strings.Contains(group, `"probe":"`+label+`"`) || !strings.Contains(group, ".(*Session).pump") {
			continue
		}
		if fields := strings.Fields(group); len(fields) > 0 {
			count, _ := strconv.Atoi(fields[0])
			n += count
		}
		groups += group + "\n"
	}
	return n, groups
}

// TestPumpEndsOnceTheSessionFailsOrCloses: the device's output that
// arrives after the session failed or closed is more than the pump's
// channel holds and nothing reads it; the pump stops handing it on and
// ends.
func TestPumpEndsOnceTheSessionFailsOrCloses(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(t *testing.T, stream *lateOutput)
	}{
		{"the login fails", func(t *testing.T, stream *lateOutput) {
			_, err := Open(context.Background(), stream, Options{Definition: iosxe(t), LoginTimeout: 50 * time.Millisecond})
			if errorcodes.Of(err) != "command_session_prompt_timeout" {
				t.Fatalf("open: %v", err)
			}
		}},
		{"the session is closed", func(t *testing.T, stream *lateOutput) {
			stream.prompt = "\r\nRouter#"
			s, err := Open(context.Background(), stream, Options{Definition: iosxe(t), LoginTimeout: 3 * time.Second})
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if err := s.Close(); err != nil {
				t.Fatalf("close: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			label := strings.ReplaceAll(t.Name(), " ", "_")
			stream := &lateOutput{left: 4 * 32, closed: make(chan struct{})}
			pprof.Do(context.Background(), pprof.Labels("probe", label), func(context.Context) {
				tc.run(t, stream)
			})
			deadline := time.Now().Add(5 * time.Second)
			for {
				n, groups := pumpsRunning(label)
				if n == 0 {
					return
				}
				if time.Now().After(deadline) {
					t.Fatalf("%d pump goroutines still run 5s after the session ended:\n%s", n, groups)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
