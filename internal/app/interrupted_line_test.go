package app

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/credentials"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// The line of an interrupted follow (DECISIONS point 4, the fourth
// "Settled at S6", gather-2026-11): it names --gather, run "karvi job
// follow --gather ID" again, where the interrupted display was gathering
// and the option would take effect, display.run.gather not locked;
// otherwise the plain follow, as job cancel --follow's always is. The
// follows say whether their display gathered (Gathering).

// TestInterruptedLineNamesGather: one function builds the line, with the
// job's folder or without, and applies the rule itself: it names --gather
// where the display was gathering and display.run.gather is not locked; a
// site's lock on the key (a locked true gathers a plain follow, and the
// lock refuses the option, exit 3), or on display.run.*, keeps it plain,
// as does a display that was not gathering; a lock on another key does
// not. The lock is a hand-built configuration: no test outside configload
// can place a site's.
func TestInterruptedLineNamesGather(t *testing.T) {
	const id, dir = "261016-015356-00", "/srv/karvi/jobs/261016/261016-015356-00"
	lock := func(pattern string) configload.Snapshot {
		return configload.Snapshot{Locks: []configload.LockDecl{{Pattern: pattern, Source: configload.SourceRef{Layer: "global", Path: "/srv/karvi/config.toml", Line: 4}}}}
	}
	named := func(dir string) string {
		if dir == "" {
			return "interrupted: job " + id + " continues in the daemon; run \"karvi job follow --gather " + id + "\" again or read its summary.json when it ends"
		}
		return "interrupted: job " + id + " continues in the daemon; artifacts " + dir + "; run \"karvi job follow --gather " + id + "\" again or read " + dir + "/summary.json when it ends"
	}
	plain := func(dir string) string {
		if dir == "" {
			return "interrupted: job " + id + " continues in the daemon; run \"karvi job follow " + id + "\" again or read its summary.json when it ends"
		}
		return "interrupted: job " + id + " continues in the daemon; artifacts " + dir + "; run \"karvi job follow " + id + "\" again or read " + dir + "/summary.json when it ends"
	}
	for _, tc := range []struct {
		name      string
		cfg       configload.Snapshot
		gathering bool
		names     bool
	}{
		{"gathering", configload.Snapshot{}, true, true},
		{"not gathering", configload.Snapshot{}, false, false},
		{"gathering, the key locked", lock("display.run.gather"), true, false},
		{"gathering, display.run locked", lock("display.run.*"), true, false},
		{"gathering, another key locked", lock("display.run.prefix"), true, true},
		{"not gathering, another key locked", lock("display.run.prefix"), false, false},
	} {
		for _, d := range []string{dir, ""} {
			want := plain(d)
			if tc.names {
				want = named(d)
			}
			if got := InterruptedLine(id, d, "continues in the daemon", tc.cfg, tc.gathering); got != want {
				t.Errorf("%s, dir %q:\n got %q\nwant %q", tc.name, d, got, want)
			}
		}
	}
}

// TestFollowSaysWhetherItGathered: both follow paths say, through
// FollowRenderOptions.Gathering, whether the display gathered (the
// renderer's Gather took it): through the daemon and from the folder, the
// key true and the job's records kept; not where the key is false, the
// job kept none, or the format is jsonl.
func TestFollowSaysWhetherItGathered(t *testing.T) {
	dir := t.TempDir()
	jobID, lines, _ := labJob(t, filepath.Join(dir, "commands.jsonl"))
	summaryOfDevices(t, dir, 8, "")
	none := t.TempDir()
	summaryNamingNone(t, none)
	for _, tc := range []struct {
		name   string
		dir    string
		format string
		sets   []string
		want   bool
	}{
		{"kept, the key true", dir, "text", gatherSets, true},
		{"kept, the key false", dir, "text", append(append([]string{}, gatherSets...), "display.run.gather=false"), false},
		{"kept none", none, "text", gatherSets, false},
		{"jsonl", dir, "jsonl", gatherSets, false},
	} {
		home := t.TempDir()
		cfg, err := configload.Load(configload.Options{HomeDir: home, SkipAuto: true, Environment: []string{}, Sets: append([]string{`basedir="` + home + `"`, `sharedroot="none"`, `timezone="UTC"`}, tc.sets...)})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		var gathering bool
		var stdout, stderr bytes.Buffer
		_, _, err = FollowJobRendering(ctx, CommonOptions{Config: cfg, Operator: credentials.Operator{Home: home}}, DaemonRuntime{Config: cfg, Socket: followDaemon(t, jobID, tc.dir, lines, nil), MaxFrame: 1 << 24}, jobID, FollowRenderOptions{Format: tc.format, Gathering: &gathering}, &stdout, &stderr)
		cancel()
		if err != nil || gathering != tc.want {
			t.Errorf("through the daemon, %s: gathering %v, want %v (%v)", tc.name, gathering, tc.want, err)
		}
	}
	for _, tc := range []struct {
		name   string
		format string
		sets   []string
		want   bool
	}{
		{"the key true", "text", gatherSets, true},
		{"the key false", "text", append(append([]string{}, gatherSets...), "display.run.gather=false"), false},
		// The folder follow asks the renderer to gather under the key
		// whatever the format; the renderer declines jsonl and json, and
		// Gathering stays false, so an interrupted line never names
		// --gather, which both refuse.
		{"jsonl, the key true", "jsonl", gatherSets, false},
		{"json, the key true", "json", gatherSets, false},
	} {
		ff := newFolderFollow(t, tc.sets...)
		labJob(t, ff.commands)
		ff.summary(t, "complete")
		var gathering bool
		var stdout bytes.Buffer
		if _, err := FollowJobDirectory(context.Background(), ff.common, ff.folder, ff.id, FollowRenderOptions{Format: tc.format, Gathering: &gathering}, &stdout, &bytes.Buffer{}); err != nil || gathering != tc.want {
			t.Errorf("from the folder, %s: gathering %v (%v)", tc.name, gathering, err)
		}
	}
}
