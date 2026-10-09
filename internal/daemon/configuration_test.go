package daemon

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/internal/planner"
)

// TestJobRunsUnderThePlansConfiguration: the daemon runs a job under the
// configuration its plan carries, not its own: a client whose audit.file
// differs from the daemon's has the job's audit lines in its own file and
// none in the daemon's.
func TestJobRunsUnderThePlansConfiguration(t *testing.T) {
	f := newV5Fixture(t)
	daemonAudit := f.cfg.String("audit.file")
	clientAudit := filepath.Join(filepath.Dir(daemonAudit), "client-audit.jsonl")
	values := f.cfg.ValueMap()
	values["audit.file"] = clientAudit
	client, err := configload.FromValues(values)
	if err != nil {
		t.Fatal(err)
	}
	f.cfg = client
	jobID := mustID(t)
	sub := f.prepareAndPackage(jobID, []string{"show clock"})
	if _, err := f.provide(sub); err != nil {
		t.Fatal(err)
	}
	if _, err := CommitJob(f.ctx(), f.socket, 1<<20, sub.request); err != nil {
		t.Fatal(err)
	}
	if terminal := f.followToEnd(jobID); terminal.Outcome.ExitCode != 0 {
		t.Fatalf("outcome=%+v", terminal.Outcome)
	}
	mine, err := os.ReadFile(clientAudit)
	if err != nil || !bytes.Contains(mine, []byte(jobID)) {
		t.Fatalf("the client's audit file holds no line of the job: %v\n%s", err, mine)
	}
	if theirs, _ := os.ReadFile(daemonAudit); bytes.Contains(theirs, []byte(jobID)) {
		t.Fatalf("the daemon's own audit file holds the job's lines:\n%s", theirs)
	}
}

// TestPrepareRefusesAConfigurationNotItsDigest: a draft whose block is not
// the configuration sources.config_digest names is refused at prepare; the
// header is the changed draft's, so the plan's own digest holds (an
// unchanged header refuses it earlier, as plan_digest_mismatch).
func TestPrepareRefusesAConfigurationNotItsDigest(t *testing.T) {
	f := newV5Fixture(t)
	jobID := mustID(t)
	draft, _, _, _ := f.draft(jobID, []string{"show clock"})
	draft.Configuration["ssh.host-key-policy"] = "secure"
	header, err := planner.Header(draft, jobID, executionplan.ModeLive)
	if err != nil {
		t.Fatal(err)
	}
	_, err = PrepareJob(f.ctx(), f.socket, 1<<20, ipc.PrepareRequest{Header: header, Draft: draft})
	if errorcodes.Of(err) != "execution_plan_invalid" || !strings.Contains(err.Error(), "configuration: digest") {
		t.Fatalf("a changed block: %v", err)
	}
}
