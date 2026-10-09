package jobexec

import (
	"reflect"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/output"
)

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
