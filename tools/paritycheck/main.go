// Command paritycheck compares the requested-command records of several
// karvi jsonl streams path by path: the same command list run by `command`
// and by `run`, over
// `system` and over `scrapligo-v1`, must give the same records but for the
// paths excluded below. Every other path is compared, so a new record field
// is compared until it is excluded here on purpose.
//
//	paritycheck [-expect STATUS[:CODE],...] STREAM STREAM...
//
// -expect lists the status, and the error code when there is one, of each
// record of the first stream in order. Exit 0 with one summary line; 1 with
// one line per finding; 2 on usage or an unreadable stream.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// excluded are the paths that differ by activity, by run, or by transport
// by design. A name ending in "." excludes everything under it.
var excluded = []string{
	"record_id", "job_id", "activity_id", // identifiers
	"credential.credential_id",
	"activity_type", "candidate_count", // the activity's own
	"timing.",
	"transport",
}

// openStatuses are the statuses of a failure at open, where each transport
// reports in its own words (OpenSSH's diagnostic, x/crypto's handshake
// error); the code, category, and retryable still compare.
var openStatuses = map[string]bool{"connection_error": true, "authentication_error": true}

// observedCount is the byte count in the output limit's message: what had
// arrived when the limit tripped, which follows read timing.
var observedCount = regexp.MustCompile(`\(\d+ observed\)`)

func isExcluded(path string) bool {
	for _, x := range excluded {
		if path == x || (strings.HasSuffix(x, ".") && strings.HasPrefix(path, x)) {
			return true
		}
	}
	return false
}

// flatten writes v's leaves into out as path -> JSON text; an empty object
// or list is a leaf, so its presence compares.
func flatten(v any, path string, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if len(t) == 0 {
			out[path] = "{}"
		}
		for k, x := range t {
			p := k
			if path != "" {
				p = path + "." + k
			}
			flatten(x, p, out)
		}
	case []any:
		if len(t) == 0 {
			out[path] = "[]"
		}
		for i, x := range t {
			flatten(x, path+"["+strconv.Itoa(i)+"]", out)
		}
	default:
		// Without HTML escaping, so a finding shows "Router>" as recorded.
		var b strings.Builder
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(t)
		out[path] = strings.TrimSuffix(b.String(), "\n")
	}
}

// compared is one record's compared paths.
func compared(record map[string]any) map[string]string {
	flat := map[string]string{}
	flatten(record, "", flat)
	status, _ := record["status"].(string)
	for path := range flat {
		if isExcluded(path) || (path == "error.message" && openStatuses[status]) {
			delete(flat, path)
		}
	}
	if status == "output_limit_exceeded" {
		if m, ok := flat["error.message"]; ok {
			flat["error.message"] = observedCount.ReplaceAllString(m, "(N observed)")
		}
	}
	return flat
}

// records reads a jsonl stream's requested-command records; other lines of
// the stream are not records and are skipped.
func records(r io.Reader) ([]map[string]any, error) {
	var out []map[string]any
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		var rec map[string]any
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if _, ok := rec["command_index"]; ok {
			out = append(out, rec)
		}
	}
	return out, sc.Err()
}

// expectation is "STATUS" or "STATUS:CODE" of one record.
func expectation(record map[string]any) string {
	status, _ := record["status"].(string)
	if e, ok := record["error"].(map[string]any); ok {
		if code, _ := e["code"].(string); code != "" {
			return status + ":" + code
		}
	}
	return status
}

// compare lists the findings of the streams against the first.
func compare(names []string, streams [][]map[string]any, expect []string) []string {
	var findings []string
	first := streams[0]
	if expect != nil {
		var got []string
		for _, rec := range first {
			got = append(got, expectation(rec))
		}
		if strings.Join(got, ",") != strings.Join(expect, ",") {
			findings = append(findings, fmt.Sprintf("%s: records are %s, expected %s", names[0], strings.Join(got, ","), strings.Join(expect, ",")))
		}
	}
	if len(first) == 0 {
		findings = append(findings, fmt.Sprintf("%s: no requested-command records", names[0]))
	}
	for s := 1; s < len(streams); s++ {
		if len(streams[s]) != len(first) {
			findings = append(findings, fmt.Sprintf("%s: %d records, %s has %d", names[s], len(streams[s]), names[0], len(first)))
			continue
		}
		for i := range first {
			a, b := compared(first[i]), compared(streams[s][i])
			paths := map[string]bool{}
			for p := range a {
				paths[p] = true
			}
			for p := range b {
				paths[p] = true
			}
			sorted := make([]string, 0, len(paths))
			for p := range paths {
				sorted = append(sorted, p)
			}
			sort.Strings(sorted)
			for _, p := range sorted {
				av, aok := a[p]
				bv, bok := b[p]
				if !aok {
					av = "<absent>"
				}
				if !bok {
					bv = "<absent>"
				}
				if av != bv {
					findings = append(findings, fmt.Sprintf("record %d %s: %s=%s, %s=%s", i, p, names[0], av, names[s], bv))
				}
			}
		}
	}
	return findings
}

func main() {
	expectFlag := flag.String("expect", "", "STATUS[:CODE] of each record of the first stream, comma-separated")
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: paritycheck [-expect STATUS[:CODE],...] STREAM STREAM...")
		os.Exit(2)
	}
	var expect []string
	if *expectFlag != "" {
		expect = strings.Split(*expectFlag, ",")
	}
	names := flag.Args()
	streams := make([][]map[string]any, 0, len(names))
	for _, name := range names {
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		recs, err := records(f)
		f.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			os.Exit(2)
		}
		streams = append(streams, recs)
	}
	findings := compare(names, streams, expect)
	for _, f := range findings {
		fmt.Println(f)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Printf("parity: %d records equal in %d streams\n", len(streams[0]), len(streams))
}
