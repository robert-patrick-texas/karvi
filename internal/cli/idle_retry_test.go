package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestRunRetriesOnceWhenTheDaemonLeftAfterTheProbe covers the client's
// side of the idle timer: the daemon is
// probed just before the first request, and when that request finds the
// socket gone, the client ensures a daemon once more and repeats the
// request with the plan it already made. The test's ensure stops the
// daemon on its first call, as an idle exit between the probe and the
// request would, and launches one on its second; the exercise then ends
// as it would have with no interruption, and ensure was called twice.
func TestRunRetriesOnceWhenTheDaemonLeftAfterTheProbe(t *testing.T) {
	_, sets, socket, maxFrame, stop := exerciseRuntime(t)
	defer stop()
	t.Setenv("NETPASS", "p")
	g := globalsFor(sets)
	var launch bytes.Buffer
	calls := 0
	ensure := func(ctx context.Context) error {
		calls++
		if calls == 1 {
			stopTestDaemon(t, socket, maxFrame) // the daemon leaves after the probe
			return nil
		}
		_, err := ensureTestDaemon(ctx, g, &launch)
		return err
	}
	// No ID is passed: the run reserves its own under the fixture's output
	// root, as the command line's run does.
	common, err := testCommon(g)
	if err != nil {
		t.Fatal(err)
	}
	opts := app.RunOptions{CommonOptions: common, Exercise: true, Follow: true, Format: "json",
		Targets: []records.TargetInput{{Kind: "target", Value: "127.0.0.1"}}, Transport: "system", Commands: []string{"show clock"}}
	var stdout, stderr bytes.Buffer
	result := app.RunViaDaemon(context.Background(), opts, socket, maxFrame, ensure, app.IO{Stdin: strings.NewReader(""), Stdout: &stdout, Stderr: &stderr})
	if result.ExitCode != 0 || result.Error != "" {
		t.Fatalf("exit=%d error=%q stderr=%q launch=%q", result.ExitCode, result.Error, stderr.String(), launch.String())
	}
	if calls != 2 {
		t.Fatalf("ensure was called %d times, want 2", calls)
	}
	if !strings.Contains(launch.String(), "daemon started socket="+socket) {
		t.Fatalf("the second ensure did not launch: %q", launch.String())
	}
	if !strings.Contains(stdout.String(), `"outcome":"exercised"`) && !strings.Contains(stdout.String(), `"outcome": "exercised"`) {
		t.Fatalf("stdout: %s", stdout.String())
	}
}
