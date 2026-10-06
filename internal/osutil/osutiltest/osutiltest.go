// Package osutiltest keeps a package's tests off the host. A test binary
// whose dependencies reach osutil.ResolveSharedTree (through planner.draft,
// app.reserve, app.jobdir, or cli.login_record: the app, cli, daemon, and
// planner packages) calls Isolate from its TestMain, so that no test
// consults the host's shared roots or its scratch root, and every activity
// a test starts writes its scoreboard and its capacity leases under
// directories of the run. A `sudo karvi setup shared`
// on the development host must take no test's job: the first verifier run
// after the operator's found the daemon package's jobs in /opt/karvi/jobs,
// its TestMain being the one without the guard. Both verifiers now count
// the host's shared entries before the Go tests and after the suites
// (scripts/lib/host.sh).
package osutiltest

import (
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/internal/osutil"
)

// Isolate points osutil.SharedRoots at a directory that does not exist,
// osutil.ScratchRoot at a scratch root of this process (under /tmp and not
// TMPDIR, so an askpass or control socket in an operator's folder there
// stays within the socket path limit however long TMPDIR is, as the release's
// 145-byte TMPDIR run requires; a test whose configuration reads no
// environment resolves the auto chain), KARVI__SCOREBOARDS at a
// scoreboard directory and
// KARVI__SESSIONS__SHARED_CAPACITY_ROOT at a capacity root of this run,
// and the
// spool directory's auto chain (osutil.SpoolRoots; /tmp/karvi-<uid> on
// the host, the operator's own daemon's place) and
// KARVI__SPOOLDIR at a spool directory of this run. A process the binary
// re-executes (the cli's `daemon serve`, the daemon package's fake device)
// inherits the variables and keeps them, so each is set only when absent.
// The returned function removes the directories this call made; a
// TestMain calls it after m.Run.
func Isolate() func() {
	osutil.SharedRoots = []string{filepath.Join(os.TempDir(), "karvi-test-no-shared-root")}
	scratch, err := os.MkdirTemp("/tmp", "karvi-test-scratch-")
	if err != nil {
		panic(err)
	}
	osutil.ScratchRoot = scratch
	made := []string{scratch}
	for name, pattern := range map[string]string{"KARVI__SCOREBOARDS": "karvi-test-scoreboards-", "KARVI__SESSIONS__SHARED_CAPACITY_ROOT": "karvi-test-capacity-"} {
		if os.Getenv(name) != "" {
			continue
		}
		dir, err := os.MkdirTemp("", pattern)
		if err != nil {
			panic(err)
		}
		os.Setenv(name, dir)
		made = append(made, dir)
	}
	spool := os.Getenv("KARVI__SPOOLDIR")
	if spool == "" {
		dir, err := os.MkdirTemp("", "karvi-test-spool-")
		if err != nil {
			panic(err)
		}
		os.Setenv("KARVI__SPOOLDIR", dir)
		spool, made = dir, append(made, dir)
	}
	// A test that loads with an explicit empty environment reads no
	// variable, so the chain itself is moved under the run's directory.
	osutil.SpoolRoots = []string{spool}
	return func() {
		for _, dir := range made {
			os.RemoveAll(dir)
		}
	}
}
