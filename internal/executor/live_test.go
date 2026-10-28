package executor

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/fakedevice"
	"github.com/robert-patrick-texas/karvi/records"
)

// liveRecorder is a LiveView that keeps what it is given: the bytes, each
// secret with the byte count when it came, the record it was opened from,
// and how often it was ended.
type liveRecorder struct {
	mu       sync.Mutex
	data     bytes.Buffer
	secrets  []string
	secretAt []int
	opened   records.CommandRecord
	ended    int
}

func (l *liveRecorder) open(rec records.CommandRecord) LiveView {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.opened = rec
	return l
}

func (l *liveRecorder) Write(p []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data.Write(p)
}

func (l *liveRecorder) Secret(v []byte) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.secrets = append(l.secrets, string(v))
	l.secretAt = append(l.secretAt, l.data.Len())
}

func (l *liveRecorder) End() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ended++
}

func (l *liveRecorder) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.data.String()
}

// waitFor waits until the view holds want, up to the deadline.
func (l *liveRecorder) waitFor(want string, deadline time.Duration) bool {
	for end := time.Now().Add(deadline); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if strings.Contains(l.text(), want) {
			return true
		}
	}
	return false
}

func liveGrant(enable string) grants {
	return grants{credentialpackage.CredentialGrant{CredentialID: "grant-live", Method: credentialpackage.MethodEmbeddedSecret, Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString("p"), EnablePassword: credentials.NewSecretString(enable), Policy: "default", Backend: "env", MatchedOn: credentials.Match{Category: "operator", SafeValue: "netops"}}}
}

// TestLiveShowsTheSessionAsItArrives: the view is opened from the device's
// record before the transport, is given the device's bytes from the first
// (the setup, the echoed enable secret, the command) while the command
// still runs, the secret before it was written, and is ended once.
func TestLiveShowsTheSessionAsItArrives(t *testing.T) {
	const enable = "Zq7-live-SECRET"
	gate := make(chan struct{})
	srv, err := fakedevice.Start(fakedevice.Options{Username: "u", Password: "p", Enable: enable, EchoSecret: true, DripGate: gate})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := newGateHarness(t, nil, nil)
	h.withAlgorithmConfig(`platform.c9300.driver="cisco_ios"`)
	h.target.Device.Transport = "native"
	h.target.Device.Platform = "c9300"
	h.target.Device.Port = uint16(srv.Port())
	h.exec.opts.Grants = liveGrant(enable)
	h.exec.opts.Commands = []string{"show drip"}
	live := &liveRecorder{}
	h.exec.opts.Live = live.open
	// The second line is held until the first has been shown: a view that
	// waited for the command's end would never see it.
	seen := make(chan bool, 1)
	go func() {
		ok := live.waitFor("drip 1", 10*time.Second)
		seen <- ok
		gate <- struct{}{}
		gate <- struct{}{}
	}()
	res, recs := h.run(context.Background())
	if !<-seen {
		t.Fatalf("drip 1 was not shown while the command ran: %q", live.text())
	}
	if !res.Success || len(recs) != 1 || recs[0].Output != "drip 1\ndrip 2\ndrip 3\n" {
		t.Fatalf("result %+v records %+v", res, recs)
	}
	got := live.text()
	for _, want := range []string{"Router>enable", "Password:", "terminal length 0", "show drip", "drip 1\r\ndrip 2\r\ndrip 3\r\nRouter#"} {
		if !strings.Contains(got, want) {
			t.Fatalf("view lacks %q:\n%q", want, got)
		}
	}
	echoed := strings.Index(got, enable)
	if len(live.secrets) != 1 || live.secrets[0] != enable || echoed < 0 || live.secretAt[0] > echoed {
		t.Fatalf("secrets %q at %v, echoed at %d", live.secrets, live.secretAt, echoed)
	}
	if live.ended != 1 || live.opened.Device.CanonicalName != h.target.Device.CanonicalName || live.opened.Transport != "native" || live.opened.SelectedAddress != "127.0.0.1" {
		t.Fatalf("ended %d, opened from %+v", live.ended, live.opened)
	}
}

// TestLiveShowsAnExecCommandAtItsPrompt: an exec channel echoes nothing, so
// each command is shown at the prompt --echo shows it at, its two streams
// after it.
func TestLiveShowsAnExecCommandAtItsPrompt(t *testing.T) {
	srv, err := fakedevice.Start(fakedevice.Options{Persona: fakedevice.PersonaLinux, Username: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	h := newGateHarness(t, nil, nil)
	h.target.Device.Transport = "native"
	h.target.Device.Platform = "linux"
	h.target.Channel = executionplan.ChannelExec
	h.target.Device.Port = uint16(srv.Port())
	h.exec.opts.Commands = []string{"both"}
	live := &liveRecorder{}
	h.exec.opts.Live = live.open
	res, recs := h.run(context.Background())
	if !res.Success || len(recs) != 1 {
		t.Fatalf("result %+v records %+v", res, recs)
	}
	got := live.text()
	prompt := records.ExecPrompt(h.target.Device.CanonicalName) + "both\n"
	if !strings.HasPrefix(got, prompt) || !strings.Contains(got, "to stdout\n") || !strings.Contains(got, "to stderr\n") || len(got) != len(prompt)+len("to stdout\nto stderr\n") {
		t.Fatalf("view %q", got)
	}
	if live.ended != 1 {
		t.Fatalf("ended %d", live.ended)
	}
}
