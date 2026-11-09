package configload

import (
	"errors"
	"strings"
	"testing"
)

// TestRecordTemplatesCheckedAtLoad: display.record.header and
// display.record.footer are checked at load as the other headers and
// footers are, so config validate refuses an unknown placeholder before
// login --record does; the record's own placeholder loads.
func TestRecordTemplatesCheckedAtLoad(t *testing.T) {
	load := func(set string) error {
		_, err := Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: []string{set}})
		return err
	}
	for _, key := range []string{"display.record.header", "display.record.footer"} {
		err := load(key + `="! <transcrpt>"`)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != "display_placeholder_unsupported" || ce.Key != key || ce.Source.Path != "--set[1]" {
			t.Errorf("%s: err=%v, want display_placeholder_unsupported for %s at --set[1]", key, err, key)
		}
		if err := load(key + `="! <target> transcript=<transcript>"`); err != nil {
			t.Errorf("%s: %v", key, err)
		}
	}
}

// TestGatherKeys: display.run.gather and display.run.prefix are false by
// default and set by --set and by their KARVI__ names; display.gather.header
// defaults to "! <targets> (<count>)", is checked at load as the other
// headers are (an unknown placeholder, a second line, above 64 KiB), by
// --set and by its KARVI__ name, and empty loads, disabling it.
func TestGatherKeys(t *testing.T) {
	load := func(env []string, sets ...string) (Snapshot, error) {
		return Load(Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: env, Sets: sets})
	}
	snap, err := load([]string{})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Bool("display.run.gather") || snap.Bool("display.run.prefix") || snap.String("display.gather.header") != "! <targets> (<count>)" {
		t.Fatalf("defaults: gather=%v prefix=%v header=%q", snap.Bool("display.run.gather"), snap.Bool("display.run.prefix"), snap.String("display.gather.header"))
	}
	for _, tc := range []struct {
		env, set string
	}{
		{"KARVI__DISPLAY__RUN__PREFIX=true", "display.run.gather=true"},
		{"KARVI__DISPLAY__RUN__GATHER=true", "display.run.prefix=true"},
	} {
		snap, err = load([]string{tc.env}, tc.set)
		if err != nil {
			t.Fatal(err)
		}
		if !snap.Bool("display.run.gather") || !snap.Bool("display.run.prefix") {
			t.Fatalf("%s and --set %s: gather=%v prefix=%v", tc.env, tc.set, snap.Bool("display.run.gather"), snap.Bool("display.run.prefix"))
		}
	}
	for set, code := range map[string]string{
		`display.gather.header="! <targets> (<cnt>)"`:                   "display_placeholder_unsupported",
		`display.gather.header="! <targets>\n(<count>)"`:                "display_template_multiline",
		`display.gather.header="` + strings.Repeat("x", 64<<10+1) + `"`: "display_template_too_long",
	} {
		_, err := load([]string{}, set)
		var ce *Error
		if !errors.As(err, &ce) || ce.Code != code || ce.Key != "display.gather.header" || ce.Source.Path != "--set[1]" {
			t.Errorf("%s: err=%v, want %s for display.gather.header at --set[1]", set, err, code)
		}
	}
	_, err = load([]string{"KARVI__DISPLAY__GATHER__HEADER=! <targets> (<cnt>)"})
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != "display_placeholder_unsupported" || ce.Key != "display.gather.header" || ce.Source.Path != "KARVI__DISPLAY__GATHER__HEADER" {
		t.Errorf("KARVI__DISPLAY__GATHER__HEADER: err=%v, want display_placeholder_unsupported for display.gather.header at its KARVI__ name", err)
	}
	for _, set := range []string{`display.gather.header=""`, `display.gather.header="<count> devices: <targets>"`} {
		if _, err := load([]string{}, set); err != nil {
			t.Errorf("%s: %v", set, err)
		}
	}
}
