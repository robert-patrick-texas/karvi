package output

import "github.com/robert-patrick-texas/karvi/internal/configload"

// SkippedFiles is the files a job under cfg does not write: each
// output.files switch that is false, or every file when
// output.persist-command is false (`--nof`), and, for a crun, every
// device's output.TARGET.txt, since the collection file is the text
// rendered once more. All eight skipped is AllFiles, for which the store
// makes no folder. The planner and the job read the keys through this one
// rule.
func SkippedFiles(cfg configload.Snapshot, crun bool) FileSet {
	if !cfg.Bool("output.persist-command") {
		return AllFiles
	}
	off := func(file string) bool { return !cfg.Bool("output.files." + file) }
	return FileSet{
		CommandsJSONL: off("commands-jsonl"), CommandsTxt: off("commands-txt"), ErrorsJSONL: off("errors-jsonl"), FailedDevicesTxt: off("failed-devices-txt"),
		ManifestJSON: off("manifest-json"), MetricsJSON: off("metrics-json"), SummaryJSON: off("summary-json"), OutputTxt: off("output-txt") || crun,
	}
}
