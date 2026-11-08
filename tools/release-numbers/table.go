package main

import (
	"strings"
	"unicode/utf8"
)

// absent is a module's value in a release that lacks it.
const absent = "—"

// table is a Markdown pipe table's lines, and the line after it when it has
// one (the CHANGELOG's).
type table struct {
	lines   []string
	closing string
}

func (t table) String() string {
	s := strings.Join(t.lines, "\n") + "\n"
	if t.closing != "" {
		s += "\n" + t.closing + "\n"
	}
	return s
}

// pair is one row of the table: its two releases' values.
type pair struct {
	kind       kind
	name       string
	prev, this string
}

func (p pair) moved() bool { return p.prev != p.this }

func (p pair) label() string {
	if p.kind == moduleRow {
		return "`" + p.name + "`"
	}
	return p.name
}

// pairs joins the two releases' rows in this release's order. A row only
// the previous release has stands where it stood there, before the first of
// its later rows this release has; a module gone from both has no row.
func pairs(prev, this release) []pair {
	type key struct {
		kind kind
		name string
	}
	var order []key
	at := func(k key) int {
		for i, o := range order {
			if o == k {
				return i
			}
		}
		return -1
	}
	values := [2]map[key]string{{}, {}}
	for _, r := range this.rows {
		order = append(order, key{r.kind, r.name})
		values[1][key{r.kind, r.name}] = r.value
	}
	for i, r := range prev.rows {
		k := key{r.kind, r.name}
		values[0][k] = r.value
		if at(k) >= 0 {
			continue
		}
		where := len(order)
		for _, q := range prev.rows[i+1:] {
			if j := at(key{q.kind, q.name}); j >= 0 {
				where = j
				break
			}
		}
		order = append(order[:where], append([]key{k}, order[where:]...)...)
	}
	out := make([]pair, len(order))
	for i, k := range order {
		p := pair{kind: k.kind, name: k.name, prev: absent, this: absent}
		if v, ok := values[0][k]; ok {
			p.prev = v
		}
		if v, ok := values[1][k]; ok {
			p.this = v
		}
		out[i] = p
	}
	return out
}

// fullTable is every value, for the release notes and BUILD-RESULT.md: a
// moved version or counter bold in this release's column, the two counts
// never.
func fullTable(prev, this release) table {
	var rows [][3]string
	for _, p := range pairs(prev, this) {
		cell := p.this
		if p.moved() && p.kind != countRow {
			cell = "**" + cell + "**"
		}
		rows = append(rows, [3]string{p.label(), p.prev, cell})
	}
	return table{lines: layout(prev, this, rows)}
}

// changelogTable is the CHANGELOG block's: what moved, and the two counts,
// then the line saying every other value is the previous release's.
func changelogTable(prev, this release) table {
	var rows [][3]string
	for _, p := range pairs(prev, this) {
		if p.moved() || p.kind == countRow {
			rows = append(rows, [3]string{p.label(), p.prev, p.this})
		}
	}
	return table{lines: layout(prev, this, rows), closing: "Every other value is " + prev.version + "'s."}
}

// layout writes the rows under a heading naming the two releases, each
// column as wide as its widest cell with a space on each side.
func layout(prev, this release, rows [][3]string) []string {
	all := append([][3]string{{"", prev.version, this.version}}, rows...)
	var width [3]int
	for _, r := range all {
		for i, c := range r {
			width[i] = max(width[i], utf8.RuneCountInString(c))
		}
	}
	line := func(r [3]string) string {
		var b strings.Builder
		b.WriteString("|")
		for i, c := range r {
			b.WriteString(" " + c + strings.Repeat(" ", width[i]-utf8.RuneCountInString(c)) + " |")
		}
		return b.String()
	}
	var rule [3]string
	for i, w := range width {
		rule[i] = strings.Repeat("-", w)
	}
	lines := []string{line(all[0]), line(rule)}
	for _, r := range rows {
		lines = append(lines, line(r))
	}
	return lines
}
