package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/records"
)

// closedTerminal is a terminal whose writes fail, as a closed stdout's do.
type closedTerminal struct{}

func (closedTerminal) Write([]byte) (int, error) {
	return 0, errors.New("write /dev/stdout: broken pipe")
}

// fakeLogin runs a login of 127.0.0.1 over a system transport whose ssh
// exits at once, with its trust store and scoreboards under a folder of
// the test's and its audit file at auditFile (required when required); it
// returns the result, the scoreboards folder, and the standard error.
func fakeLogin(t *testing.T, stdout io.Writer, auditFile string, required bool) (ActivityResult, string, string) {
	t.Helper()
	t.Setenv("NETUSER", "jsmith")
	t.Setenv("NETPASS", "S3cret")
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	fake := filepath.Join(dir, "fake-ssh")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	base, trust, boards := filepath.Join(dir, "base"), filepath.Join(dir, "trust"), filepath.Join(dir, "scoreboards")
	for _, d := range []string{base, trust} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := configload.Load(configload.Options{HomeDir: dir, SkipAuto: true, Environment: []string{}, Sets: []string{
		`basedir="` + base + `"`, `scoreboards="` + boards + `"`,
		`audit.journald-required=false`, `audit.file="` + auditFile + `"`, fmt.Sprintf("audit.file-required=%t", required),
		`ssh.known-hosts-file="` + filepath.Join(trust, "known_hosts") + `"`,
		`ssh.transports.system="` + fake + `"`, `display.color="never"`,
		"creds.interactive-prompt=false",
	}})
	if err != nil {
		t.Fatal(err)
	}
	common := CommonOptions{Config: cfg, Operator: credentials.Operator{Username: "jsmith", UID: os.Getuid(), Home: dir}}
	var stderr bytes.Buffer
	streams := IO{Stdin: strings.NewReader(""), Stdout: stdout, Stderr: &stderr}
	result := ExecuteLogin(context.Background(), LoginOptions{CommonOptions: common, Targets: []TargetInput{{Kind: "target", Value: "127.0.0.1"}}, Platform: "cisco_ios", Transport: "system"}, streams)
	return result, boards, stderr.String()
}

// loginSnapshot is the one scoreboard file in boards, decoded.
func loginSnapshot(t *testing.T, boards string) records.ScoreboardSnapshot {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(boards, "*.json"))
	if len(files) != 1 {
		t.Fatalf("scoreboard files %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var snap records.ScoreboardSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatal(err)
	}
	return snap
}

// TestLoginHeaderWriteFailureEndsTheLogin: a login whose header cannot be
// written after its login.start ends as a session does: terminal_write_failed,
// a login.end with the error, and the scoreboard's final snapshot, never a
// running one.
func TestLoginHeaderWriteFailureEndsTheLogin(t *testing.T) {
	auditFile := filepath.Join(t.TempDir(), "audit.jsonl")
	result, boards, stderr := fakeLogin(t, closedTerminal{}, auditFile, false)
	if !strings.HasPrefix(result.Error, "terminal_write_failed: ") || result.ExitCode == 0 {
		t.Errorf("exit=%d error=%q, want terminal_write_failed; stderr %q", result.ExitCode, result.Error, stderr)
	}
	data, err := os.ReadFile(auditFile)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r records.AuditRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		events = append(events, r.EventName+":"+r.Outcome)
	}
	if got := strings.Join(events, " "); got != "login.start: login.end:error" {
		t.Errorf("audit events %q, want login.start then login.end with the error", got)
	}
	snap := loginSnapshot(t, boards)
	summary, _ := snap.ErrorSummary.(map[string]any)
	if snap.Status != "error" || snap.EndedAt == nil || snap.Counts.Failed != 1 || snap.Counts.InFlight != 0 || summary["code"] != "terminal_write_failed" {
		t.Errorf("scoreboard: status=%q ended=%v counts=%+v errors=%v", snap.Status, snap.EndedAt, snap.Counts, snap.ErrorSummary)
	}
}

// TestLoginStartAuditFailureEndsTheScoreboard: a login whose login.start
// cannot be written (a required audit file that refuses every write) ends
// before its session starts, as one whose header cannot be written does:
// audit_write_failed, and the scoreboard's final snapshot at error with
// the target failed, never a running one.
func TestLoginStartAuditFailureEndsTheScoreboard(t *testing.T) {
	result, boards, stderr := fakeLogin(t, &bytes.Buffer{}, "/dev/full", true)
	if !strings.HasPrefix(result.Error, "audit_write_failed: ") || result.ExitCode == 0 {
		t.Fatalf("exit=%d error=%q, want audit_write_failed; stderr %q", result.ExitCode, result.Error, stderr)
	}
	snap := loginSnapshot(t, boards)
	summary, _ := snap.ErrorSummary.(map[string]any)
	entry, _ := errorcodes.Lookup("audit_write_failed")
	if snap.Status != "error" || snap.EndedAt == nil || snap.ElapsedNS < 0 || snap.Counts != (records.Counts{Total: 1, Completed: 1, Failed: 1}) || len(snap.Targets) != 1 || snap.Targets[0].State != records.TargetFail || summary["code"] != "audit_write_failed" || summary["category"] != entry.Category {
		t.Errorf("scoreboard: status=%q ended=%v counts=%+v targets=%+v errors=%v", snap.Status, snap.EndedAt, snap.Counts, snap.Targets, snap.ErrorSummary)
	}
}
