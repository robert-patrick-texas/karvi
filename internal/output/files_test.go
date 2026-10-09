package output

import (
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// TestSkippedFilesFromTheKeys: each output.files switch off skips its own file and no
// other; all eight off are AllFiles (no job folder); output.persist-command
// false is AllFiles whatever the switches say; a crun skips every device's
// output.TARGET.txt and nothing else.
func TestSkippedFilesFromTheKeys(t *testing.T) {
	load := func(sets ...string) configload.Snapshot {
		t.Helper()
		cfg, err := configload.Load(configload.Options{HomeDir: t.TempDir(), SkipAuto: true, Environment: []string{}, Sets: sets})
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	keys := []string{"commands-jsonl", "commands-txt", "errors-jsonl", "failed-devices-txt", "manifest-json", "metrics-json", "summary-json", "output-txt"}
	if got := SkippedFiles(load(), false); got != (FileSet{}) {
		t.Fatalf("the defaults skip %+v", got)
	}
	var all []string
	for i, key := range keys {
		all = append(all, "output.files."+key+"=false")
		got := reflect.ValueOf(SkippedFiles(load("output.files."+key+"=false"), false))
		for f := 0; f < got.NumField(); f++ {
			if got.Field(f).Bool() != (f == i) {
				t.Errorf("%s off: field %s is %v", key, got.Type().Field(f).Name, got.Field(f).Bool())
			}
		}
	}
	if got := SkippedFiles(load(all...), false); got != AllFiles {
		t.Fatalf("all eight off skip %+v, not AllFiles", got)
	}
	if got := SkippedFiles(load("output.persist-command=false"), false); got != AllFiles {
		t.Fatalf("persist false skips %+v, not AllFiles", got)
	}
	if got := SkippedFiles(load(), true); got != (FileSet{OutputTxt: true}) {
		t.Fatalf("a crun skips %+v", got)
	}
}
