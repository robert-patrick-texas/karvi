package configload

import (
	"errors"
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
