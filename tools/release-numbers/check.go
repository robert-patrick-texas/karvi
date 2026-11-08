package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// none stands for a value or a line a side lacks.
const none = "(none)"

// difference is the check's refusal: a document that disagrees with the
// releases.
type difference struct{ msg string }

func (d *difference) Error() string { return d.msg }

// differs names what differs, then stacks the two values, each after its
// source.
func differs(what string, a, b [2]string) error {
	w := max(utf8.RuneCountInString(a[0]), utf8.RuneCountInString(b[0])) + 2
	pad := func(s string) string { return s + strings.Repeat(" ", w-utf8.RuneCountInString(s)) }
	return &difference{fmt.Sprintf("%s differs\n  %s%s\n  %s%s", what, pad(a[0]), a[1], pad(b[0]), b[1])}
}

// checkTable compares the table in a document, the one headed by the two
// releases, with want.
func checkTable(name, text string, want table, prev, this release) error {
	got, rest, ok := findTable(text, prev.version, this.version)
	if !ok {
		return &difference{fmt.Sprintf("%s: no table headed %s and %s", name, prev.version, this.version)}
	}
	return compareTable(name, got, rest, want, prev, this)
}

// checkChangelog compares the table in the CHANGELOG's block of this
// release, and the line after it, with want.
func checkChangelog(text string, want table, prev, this release) error {
	block, ok := changelogBlock(text, this.version)
	if !ok {
		return &difference{fmt.Sprintf("CHANGELOG.md: no %s block", this.version)}
	}
	got, rest, ok := findTable(block, prev.version, this.version)
	if !ok {
		return &difference{fmt.Sprintf("CHANGELOG.md: no table headed %s and %s in the %s block", prev.version, this.version, this.version)}
	}
	return compareTable("CHANGELOG.md", got, rest, want, prev, this)
}

// changelogBlock is the text under the "## VERSION" heading, to the next
// "## " heading.
func changelogBlock(text, version string) (string, bool) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if l != "## "+version && !strings.HasPrefix(l, "## "+version+" ") {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			if strings.HasPrefix(lines[j], "## ") {
				end = j
				break
			}
		}
		return strings.Join(lines[i+1:end], "\n"), true
	}
	return "", false
}

// findTable finds the pipe table whose heading names the two releases: its
// lines, and the document's lines after it.
func findTable(text, prev, this string) (table, rest []string, ok bool) {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if c, ok := cells(l); !ok || len(c) != 3 || c[0] != "" || c[1] != prev || c[2] != this {
			continue
		}
		j := i
		for j < len(lines) && strings.HasPrefix(lines[j], "|") {
			j++
		}
		return lines[i:j], lines[j:], true
	}
	return nil, nil, false
}

// cells is a table line's cells, trimmed.
func cells(line string) ([]string, bool) {
	if len(line) < 2 || line[0] != '|' || line[len(line)-1] != '|' {
		return nil, false
	}
	c := strings.Split(line[1:len(line)-1], "|")
	for i := range c {
		c[i] = strings.TrimSpace(c[i])
	}
	return c, true
}

// compareTable stops at the first line of got that is not want's: a row's
// differing value stacked with its source, a row laid out otherwise, or a
// line one side lacks.
func compareTable(name string, got, rest []string, want table, prev, this release) error {
	line := func(lines []string, i int) string {
		if i < len(lines) {
			return lines[i]
		}
		return ""
	}
	orNone := func(s string) string {
		if s == "" {
			return none
		}
		return s
	}
	for i := 0; i < max(len(got), len(want.lines)); i++ {
		g, w := line(got, i), line(want.lines, i)
		if g == w {
			continue
		}
		gc, gok := cells(g)
		wc, wok := cells(w)
		if i >= 2 && gok && wok && len(gc) == 3 && len(wc) == 3 && gc[0] == wc[0] {
			row := strings.Trim(wc[0], "`")
			for j, source := range []string{"", prev.source, this.source} {
				if j > 0 && gc[j] != wc[j] {
					return differs(name+": "+row, [2]string{name, gc[j]}, [2]string{source, wc[j]})
				}
			}
			return differs(name+": "+row+"'s line", [2]string{name, g}, [2]string{"release-numbers", w})
		}
		return differs(fmt.Sprintf("%s: the table's line %d", name, i+1), [2]string{name, orNone(g)}, [2]string{"release-numbers", orNone(w)})
	}
	if want.closing == "" {
		return nil
	}
	next := ""
	for _, l := range rest {
		if strings.TrimSpace(l) != "" {
			next = l
			break
		}
	}
	if next != want.closing {
		return differs(name+": the line after the table", [2]string{name, orNone(next)}, [2]string{"release-numbers", want.closing})
	}
	return nil
}

// checkManifest compares the release manifest's numbers with this
// release's: each counter it records, build.go_version, vendored_modules,
// and source_statistics.go_lines_outside_vendor.
func checkManifest(data []byte, this release) error {
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return fmt.Errorf("%s: %w", manifest, err)
	}
	value := func(v any, text bool) string {
		if v == nil {
			return none
		}
		if s, ok := v.(string); ok && text {
			return s
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	field := func(object, key string) any {
		o, _ := m[object].(map[string]any)
		return o[key]
	}
	rows := map[string]row{}
	for _, r := range this.rows {
		rows[r.name] = r
	}
	for _, c := range counters {
		if c.manifest == "" {
			continue
		}
		if got, want := value(m[c.manifest], false), rows[c.row].value; got != want {
			return differs(manifest+": "+c.manifest, [2]string{manifest, got}, [2]string{c.pkg + "." + c.constant, want})
		}
	}
	if got, want := value(field("build", "go_version"), true), "go"+rows["Go"].value; got != want {
		return differs(manifest+": build.go_version", [2]string{manifest, got}, [2]string{"this toolchain", want})
	}
	vendored := map[string]string{}
	var order []string
	list, _ := m["vendored_modules"].([]any)
	for _, e := range list {
		path, version, _ := strings.Cut(value(e, true), " ")
		vendored[path] = version
		order = append(order, path)
	}
	inGoMod := map[string]bool{}
	for _, r := range this.rows {
		if r.kind != moduleRow {
			continue
		}
		inGoMod[r.name] = true
		got, ok := vendored[r.name]
		if !ok {
			got = none
		}
		if got != r.value {
			return differs(manifest+": vendored_modules' "+r.name, [2]string{manifest, got}, [2]string{"go.mod", r.value})
		}
	}
	for _, p := range order {
		if !inGoMod[p] {
			return differs(manifest+": vendored_modules' "+p, [2]string{manifest, vendored[p]}, [2]string{"go.mod", none})
		}
	}
	got := value(field("source_statistics", "go_lines_outside_vendor"), false)
	if want := strings.ReplaceAll(rows[locGo].value, ",", ""); got != want {
		return differs(manifest+": source_statistics.go_lines_outside_vendor", [2]string{manifest, got}, [2]string{"the tree", want})
	}
	return nil
}
