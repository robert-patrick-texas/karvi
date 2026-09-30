// Command textfile derives a device's output.TARGET.txt from a job's
// commands.jsonl: every recorded statement's
// block in the text file is derivable from its record, and this is the
// derivation, through the same functions the store writes the file with.
// The suites compare its output with the file karvi wrote.
//
//	textfile COMMANDS.JSONL TARGET [PATTERN [ZONE]]
//
// TARGET is a device's canonical name, as in the file's name before the
// colons of an IPv6 address became hyphens. PATTERN and ZONE are the job's
// display.timestamp and timezone, which the header's time is written with;
// the registered default and the host's
// zone when absent. The text goes to standard output: the header from the
// device's first record, then a block per record in the file's order. Exit
// 1 when no record is TARGET's, 2 on usage, an unreadable file, or a
// pattern or zone that does not compile.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/output"
	"github.com/robert-patrick-texas/karvi/records"
)

func main() {
	if len(os.Args) < 3 || len(os.Args) > 5 {
		fmt.Fprintln(os.Stderr, "usage: textfile COMMANDS.JSONL TARGET [PATTERN [ZONE]]")
		os.Exit(2)
	}
	pattern, zone := display.DefaultTimestampPattern, "auto"
	if len(os.Args) > 3 {
		pattern = os.Args[3]
	}
	if len(os.Args) > 4 {
		zone = os.Args[4]
	}
	formatter, err := display.NewFormatter(pattern, zone)
	if err != nil {
		fmt.Fprintln(os.Stderr, "textfile:", err)
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "textfile:", err)
		os.Exit(2)
	}
	defer f.Close()
	out := bufio.NewWriter(os.Stdout)
	lines := bufio.NewReader(f) // a record's line may be many megabytes
	found := false
	for {
		line, readErr := lines.ReadBytes('\n')
		if len(line) > 0 {
			var r records.CommandRecord
			if err := json.Unmarshal(line, &r); err != nil {
				fmt.Fprintln(os.Stderr, "textfile:", err)
				os.Exit(2)
			}
			if r.Device.CanonicalName == os.Args[2] {
				if !found {
					err = output.WriteTextHeader(out, &r, formatter.Timestamp)
					found = true
				}
				if err == nil {
					err = output.WriteTextBlock(out, &r, output.FromRecord(&r))
				}
				if err != nil {
					fmt.Fprintln(os.Stderr, "textfile:", err)
					os.Exit(2)
				}
			}
		}
		if readErr != nil {
			break
		}
	}
	if err := out.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "textfile:", err)
		os.Exit(2)
	}
	if !found {
		fmt.Fprintf(os.Stderr, "textfile: no record of %s\n", os.Args[2])
		os.Exit(1)
	}
}
