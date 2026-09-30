package prune

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// The report: one line per item on
// standard output and the summary last, in one of two forms. `text` is the
// operator's form, an event word and its key=value fields in order; `jsonl`
// is one JSON document per line with the same fields under the same keys,
// the event word under "event", for a site's log pipeline. One function
// writes both, so a field added to a line reaches both forms at once.

// Format words of --format.
const (
	FormatText  = "text"
	FormatJSONL = "jsonl"
)

// field is one key and value of a report line. An age is given as a
// time.Duration under the key "age": the text form renders it as an
// operator reads one (40d, 3h, 12m), the jsonl form as "age_days", a
// whole number of days.
type field struct {
	key   string
	value any
}

func f(key string, value any) field { return field{key, value} }

// report writes one line of the run: the event word (removed, would-remove,
// failed, kept, skipped, walk) and its fields in order.
func report(out io.Writer, opts Options, event string, fields ...field) {
	if opts.Format == FormatJSONL {
		doc := map[string]any{"event": event}
		for _, fl := range fields {
			key, value := fl.key, fl.value
			switch v := value.(type) {
			case time.Duration:
				key, value = "age_days", int(v.Hours()/24)
			case error:
				value = v.Error()
			}
			doc[key] = value
		}
		line, _ := json.Marshal(doc)
		fmt.Fprintf(out, "%s\n", line)
		return
	}
	fmt.Fprint(out, event)
	for _, fl := range fields {
		value := fl.value
		if d, ok := value.(time.Duration); ok {
			value = ageString(d)
		}
		fmt.Fprintf(out, " %s=%v", fl.key, value)
	}
	fmt.Fprintln(out)
}

// Summary writes the run's last line: the counts and whether the run was a
// dry run, `examined= removed= …` in the text form (no event word, as the
// line has always read) and the document {"event":"summary",…} in jsonl.
func Summary(out io.Writer, opts Options, r Result) {
	fields := []field{f("examined", r.Examined), f("removed", r.Removed), f("skipped", r.Skipped), f("not_owned", r.NotOwned), f("failed", r.Failed), f("bytes", r.BytesEstimated), f("dry_run", opts.DryRun)}
	if opts.Format == FormatJSONL {
		report(out, opts, "summary", fields...)
		return
	}
	for i, fl := range fields {
		if i > 0 {
			fmt.Fprint(out, " ")
		}
		fmt.Fprintf(out, "%s=%v", fl.key, fl.value)
	}
	fmt.Fprintln(out)
}
