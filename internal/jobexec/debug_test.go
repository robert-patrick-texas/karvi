package jobexec

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/canary"
	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func TestDebugLoggerIsOneLineBoundedAndConcurrencySafe(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: []string{"display.timestamp=hh:mm:ss"}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	debug := DebugLogger(true, cfg, &out)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			debug("message\nwith\ttabs " + strings.Repeat("x", 5000))
		}()
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	if len(lines) != 16 {
		t.Fatalf("line count = %d, want 16", len(lines))
	}
	for _, line := range lines {
		if len(line)+1 > maxDebugLineBytes {
			t.Fatalf("debug line is %d bytes, limit %d", len(line)+1, maxDebugLineBytes)
		}
		if !strings.Contains(line, " DEBUG message with tabs ") || !strings.HasSuffix(line, "...") {
			t.Fatalf("unexpected debug line: %q", line)
		}
	}
}

func TestDebugLoggerDisabledWritesNothing(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	DebugLogger(false, cfg, &out)("must not appear")
	if out.Len() != 0 {
		t.Fatalf("disabled debug wrote %q", out.String())
	}
}

// The debug writer is one of the sinks that must redact a grant: a
// grant formatted into a debug event must leave nothing but the marker.
func TestDebugLoggerRedactsACredentialGrant(t *testing.T) {
	cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	seed := canarytest.Seed(t)
	g := credentialpackage.CredentialGrant{CredentialID: "x", Username: credentials.NewSecretString("u"), Password: credentials.NewSecretString(seed.Raw)}
	var out bytes.Buffer
	debug := DebugLogger(true, cfg, &out)
	debug(fmt.Sprintf("device command start grant=%v %+v %#v %s", g, g, g, g.Password))
	debug(fmt.Sprintf("wrapped %+v", struct {
		G credentialpackage.CredentialGrant
	}{g}))
	debug(fmt.Sprint(g.Password, g))
	if hits := canary.Scan("debug", out.Bytes(), seed); len(hits) != 0 {
		t.Fatalf("debug output leaks the canary: %v\n%s", hits, out.String())
	}
	if !strings.Contains(out.String(), "<redacted>") {
		t.Fatalf("debug output lacks the marker: %s", out.String())
	}
}
