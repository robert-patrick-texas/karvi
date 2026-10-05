// Command paritycheck compares the requested-command records of several
// karvi jsonl streams path by path: the same command list run by `command`
// and by `run`, over
// `system` and over `scrapligo-v1`, must give the same records but for the
// paths excluded below. Every other path is compared, so a new record field
// is compared until it is excluded here on purpose.
//
//	paritycheck [-expect STATUS[:CODE],...] [-pin PIN]... STREAM STREAM...
//
// -expect lists the status, and the error code when there is one, of each
// record of the first stream in order. -pin states a difference between
// the transports by design: N.PATH=TRANSPORT:JSON;TRANSPORT:JSON, N the
// record's position in each stream from 0, PATH a path and everything
// under it, and for each transport (the record's own `transport`) the
// value it must hold there, JSON or absent. A pinned path is checked
// against its transport's value in every stream and is not compared across
// them. Exit 0 with one summary line; 1 with one line per finding; 2 on
// usage or an unreadable stream.
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

// pin is one -pin: record Index's Path holds, on each transport, the
// leaves of that transport's value (none for absent).
type pin struct {
	Index  int
	Path   string
	Values map[string]map[string]string
}

// parsePin reads N.PATH=TRANSPORT:JSON;TRANSPORT:JSON. Each JSON value is
// read by the decoder, so a ';' inside one does not end it.
func parsePin(text string) (pin, error) {
	head, rest, ok := strings.Cut(text, "=")
	n, path, dot := strings.Cut(head, ".")
	index, err := strconv.Atoi(n)
	if !ok || !dot || err != nil || index < 0 || path == "" {
		return pin{}, fmt.Errorf("pin %q: want N.PATH=TRANSPORT:JSON;TRANSPORT:JSON", text)
	}
	p := pin{Index: index, Path: path, Values: map[string]map[string]string{}}
	for rest != "" {
		transport, value, ok := strings.Cut(rest, ":")
		if !ok || transport == "" {
			return pin{}, fmt.Errorf("pin %q: %q has no TRANSPORT:", text, rest)
		}
		leaves := map[string]string{}
		if strings.HasPrefix(value, "absent") {
			rest = value[len("absent"):]
		} else {
			dec := json.NewDecoder(strings.NewReader(value))
			var v any
			if err := dec.Decode(&v); err != nil {
				return pin{}, fmt.Errorf("pin %q: %s's value: %v", text, transport, err)
			}
			flatten(v, path, leaves)
			rest = value[dec.InputOffset():]
		}
		p.Values[transport] = leaves
		if rest != "" {
			if rest[0] != ';' {
				return pin{}, fmt.Errorf("pin %q: %q follows %s's value", text, rest, transport)
			}
			rest = rest[1:]
		}
	}
	if len(p.Values) == 0 {
		return pin{}, fmt.Errorf("pin %q: no transport's value", text)
	}
	return p, nil
}

// under takes from flat the leaves at path or below it.
func under(flat map[string]string, path string) map[string]string {
	out := map[string]string{}
	for p, v := range flat {
		if p == path || strings.HasPrefix(p, path+".") || strings.HasPrefix(p, path+"[") {
			out[p] = v
			delete(flat, p)
		}
	}
	return out
}

// leaves renders a subtree's leaves for a finding.
func leaves(m map[string]string) string {
	if len(m) == 0 {
		return "<absent>"
	}
	parts := make([]string, 0, len(m))
	for p, v := range m {
		parts = append(parts, p+"="+v)
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

// compare lists the findings of the streams against the first.
func compare(names []string, streams [][]map[string]any, expect []string, pins ...pin) []string {
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
	// Each record's compared paths, its pinned paths checked against its
	// transport's value and taken out.
	flats := make([][]map[string]string, len(streams))
	for s, stream := range streams {
		for i, rec := range stream {
			flat := compared(rec)
			for _, p := range pins {
				if p.Index != i {
					continue
				}
				got := under(flat, p.Path)
				transport, _ := rec["transport"].(string)
				want, ok := p.Values[transport]
				if !ok {
					findings = append(findings, fmt.Sprintf("record %d %s: %s's transport %q has no pinned value", i, p.Path, names[s], transport))
				} else if leaves(got) != leaves(want) {
					findings = append(findings, fmt.Sprintf("record %d %s: %s=%s, pinned %s=%s", i, p.Path, names[s], leaves(got), transport, leaves(want)))
				}
			}
			flats[s] = append(flats[s], flat)
		}
	}
	for _, p := range pins {
		if p.Index >= len(first) {
			findings = append(findings, fmt.Sprintf("record %d %s: pinned, %s has %d records", p.Index, p.Path, names[0], len(first)))
		}
	}
	for s := 1; s < len(streams); s++ {
		if len(streams[s]) != len(first) {
			findings = append(findings, fmt.Sprintf("%s: %d records, %s has %d", names[s], len(streams[s]), names[0], len(first)))
			continue
		}
		for i := range first {
			a, b := flats[0][i], flats[s][i]
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
	var pins []pin
	flag.Func("pin", "N.PATH=TRANSPORT:JSON;TRANSPORT:JSON, a difference by design (repeatable)", func(text string) error {
		p, err := parsePin(text)
		pins = append(pins, p)
		return err
	})
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: paritycheck [-expect STATUS[:CODE],...] [-pin PIN]... STREAM STREAM...")
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
	findings := compare(names, streams, expect, pins...)
	for _, f := range findings {
		fmt.Println(f)
	}
	if len(findings) > 0 {
		os.Exit(1)
	}
	fmt.Printf("parity: %d records equal in %d streams\n", len(streams[0]), len(streams))
}
