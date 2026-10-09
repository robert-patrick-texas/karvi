package jobexec

import (
	"bytes"
	"testing"
	"time"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/records"
)

// TestCollectionFooterFollowsTheFooter: a job with a collection ends its
// text display with display.collection.footer directly after the footer,
// on the same stream, for run and command alike; a job without one prints
// no such line; --quiet, an empty template, and json and jsonl print none.
func TestCollectionFooterFollowsTheFooter(t *testing.T) {
	collection := &records.CollectionSummary{Directory: "/srv/c", Replaced: 2, Kept: 1}
	render := func(activity, format string, quiet bool, c *records.CollectionSummary, sets ...string) string {
		t.Helper()
		sets = append([]string{"display." + activity + ".footer=\"! exit=<exit-code>\""}, sets...)
		cfg, err := configload.Load(configload.Options{InternalOnly: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		r, err := newRecordRenderer(&out, format, cfg, quiet, false, "activity-1", "/tmp/artifacts", activity, false, false, false)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.WriteFooter(time.Now(), 0, time.Second, c); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	for _, activity := range []string{"run", "command"} {
		if got, want := render(activity, "text", false, collection), "! exit=0\n! collection=/srv/c replaced=2 kept=1\n"; got != want {
			t.Errorf("%s: %q, want %q", activity, got, want)
		}
		if got, want := render(activity, "text", false, nil), "! exit=0\n"; got != want {
			t.Errorf("%s without a collection: %q, want %q", activity, got, want)
		}
	}
	for name, got := range map[string]string{
		"quiet":          render("run", "text", true, collection),
		"jsonl":          render("run", "jsonl", false, collection),
		"empty template": render("run", "text", false, collection, "display.collection.footer=", "display.run.footer="),
	} {
		if got != "" {
			t.Errorf("%s: %q, want nothing", name, got)
		}
	}
	if got, want := render("run", "text", false, collection, "display.run.footer="), "! collection=/srv/c replaced=2 kept=1\n"; got != want {
		t.Errorf("the footer empty, the line kept: %q, want %q", got, want)
	}
	if got, want := render("run", "text", false, collection, `display.collection.footer="<kept> kept of <replaced>+<kept> in <collection>"`), "! exit=0\n1 kept of 2+1 in /srv/c\n"; got != want {
		t.Errorf("a site's template: %q, want %q", got, want)
	}
	if got := render("run", "json", false, collection); got != "[]\n" {
		t.Errorf("json: %q, want the empty array alone", got)
	}
}

// TestScoreboardModeByWord: the screen's MODE is the operator's word; a
// run or command with --cd stays run or cmd, a crun is crun, an exercise
// is exercise whatever collects.
func TestScoreboardModeByWord(t *testing.T) {
	plan := func(word string) executionplan.ExecutionPlan {
		var p executionplan.ExecutionPlan
		if word != "" {
			p.Output.Collection = &executionplan.CollectionSettings{Directory: "/c", Word: word}
		}
		return p
	}
	for _, c := range []struct {
		activity, word string
		mode           executionplan.Mode
		want           string
	}{
		{"run", "crun", executionplan.ModeLive, "crun"},
		{"run", "run", executionplan.ModeLive, "run"},
		{"run", "", executionplan.ModeLive, "run"},
		{"command", "command", executionplan.ModeLive, "cmd"},
		{"command", "", executionplan.ModeLive, "cmd"},
		{"run", "crun", executionplan.ModeExercise, "exercise"},
	} {
		if got := scoreboardMode(Request{ActivityType: c.activity, Mode: c.mode, Plan: plan(c.word)}); got != c.want {
			t.Errorf("%s word %q mode %s: %s, want %s", c.activity, c.word, c.mode, got, c.want)
		}
	}
}
