package jobexec

import (
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/output"
)

// outputFileFields is OutputFiles' fields, in FileSet's field order.
var outputFileFields = []string{"CommandsJSONL", "CommandsTxt", "ErrorsJSONL", "FailedDevicesTxt", "ManifestJSON", "MetricsJSON", "SummaryJSON", "OutputTxt"}

// outputWith is the plan's output settings with the named files off.
func outputWith(off ...string) executionplan.OutputSettings {
	o := executionplan.OutputSettings{Persist: true, Files: executionplan.AllOutputFiles}
	v := reflect.ValueOf(&o.Files).Elem()
	for _, name := range off {
		v.FieldByName(name).SetBool(false)
	}
	return o
}

// TestSkippedFilesFollowThePlan is the plan's file rules at the runner:
// each switch of the plan's files
// skips its own file and no other; all eight off are the store's AllFiles
// (no job folder); and persist false is AllFiles whatever the switches
// say, on every path.
func TestSkippedFilesFollowThePlan(t *testing.T) {
	if got := skippedFiles(outputWith()); got != (output.FileSet{}) {
		t.Fatalf("the defaults skip %+v", got)
	}
	for i, name := range outputFileFields {
		got := reflect.ValueOf(skippedFiles(outputWith(name)))
		for f := 0; f < got.NumField(); f++ {
			if got.Field(f).Bool() != (f == i) {
				t.Errorf("%s off: field %s is %v", name, got.Type().Field(f).Name, got.Field(f).Bool())
			}
		}
	}
	if got := skippedFiles(outputWith(outputFileFields...)); got != output.AllFiles {
		t.Fatalf("all eight off skip %+v, not AllFiles", got)
	}
	o := outputWith()
	o.Persist = false
	if got := skippedFiles(o); got != output.AllFiles {
		t.Fatalf("persist false skips %+v, not AllFiles", got)
	}
}

// TestSummaryFilesNameOnlyWrittenFiles: rule 5. The store leaves a skipped
// file's path empty; the summary then has no entry for it, and no
// output.commands_jsonl without the file.
func TestSummaryFilesNameOnlyWrittenFiles(t *testing.T) {
	files, out := summaryFiles(output.Paths{Root: "/j", CommandsJSONL: "/j/commands.jsonl", CommandsText: "/j/commands.txt", Summary: "/j/summary.json"}, 34)
	if want := map[string]string{"commands_jsonl": "/j/commands.jsonl", "commands_txt": "/j/commands.txt", "summary": "/j/summary.json"}; !reflect.DeepEqual(files, want) {
		t.Fatalf("paths %v, want %v", files, want)
	}
	if out["commands_jsonl"] != "/j/commands.jsonl" || out["bytes"] != int64(34) {
		t.Fatalf("output %v", out)
	}
	files, out = summaryFiles(output.Paths{Root: "/j", Summary: "/j/summary.json"}, 34)
	if _, named := out["commands_jsonl"]; named || len(files) != 1 {
		t.Fatalf("commands.jsonl not written: paths %v, output %v", files, out)
	}
}
