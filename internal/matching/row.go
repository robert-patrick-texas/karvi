package matching

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// This file is the credential CSV's row evaluator.
// It is built on the same compiled selectors and prefixes as the maps'
// EvaluateRule, so the grammar stays in this package, and differs from it
// in three rules a CSV needs:
//
//   - A map key holds a list and must hold a positive value
//     (config_match_rule_only_negated); a CSV cell holds one selector, so a
//     negated cell can never have a positive beside it in its column. A row
//     needs one positive cell anywhere, not one per column (a row such as
//     site=NYC device_group=!lab could never match under EvaluateRule).
//   - Rows are first-match in file order with no longest-prefix rule, so a
//     row reports only whether it matches.
//   - A sixth column, credkey, is a literal key matched against the
//     device's credkeyref.

// RowColumns are a credential CSV row's selector columns in evaluation
// order, named as the file's logical fields.
var RowColumns = []string{"device_name", "address_cidr", "platform", "site", "device_group", "credkey"}

// RowFields is the device view a row is evaluated against: the map rules'
// five fields and the inventory's credkeyref, blank for a device that pins
// no key.
type RowFields struct {
	Fields
	CredKeyRef string
}

// RowError is a row whose selectors do not stand. Column names the cell,
// blank when the fault is the row's as a whole; Reason never quotes a
// secret, since only selector cells reach this package.
type RowError struct {
	Column string
	Reason string
}

func (e *RowError) Error() string {
	if e.Column == "" {
		return e.Reason
	}
	return fmt.Sprintf("column %s: %s", e.Column, e.Reason)
}

// CheckKey reports why value cannot be a credkey or a credkeyref: a key is
// a nonblank literal with no pattern character and no leading "!", the rule
// of platform names, so it can be neither a glob nor a negation. It returns
// "" for a legal key.
func CheckKey(value string) string {
	switch {
	case strings.TrimSpace(value) == "":
		return "a key is not blank"
	case strings.HasPrefix(value, "!"):
		return "a key cannot begin with \"!\": a key is a literal and cannot be negated"
	case HasMeta(value):
		return "a key holds no pattern character (*, ?, [, \\): a key is a literal"
	}
	return ""
}

// FoldKey is a key's canonical form for comparison: keys compare without
// regard to case under the same simple folding as every
// selector, so two keys are one key exactly when their FoldKey is equal.
func FoldKey(value string) string {
	return strings.Map(func(r rune) rune {
		low := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			if f < low {
				low = f
			}
		}
		return low
	}, value)
}

type rowCell struct {
	column string
	compiled
	cidr bool
}

// Row is one compiled row: its filled selector cells and its key. The zero
// Row matches nothing.
type Row struct {
	cells    []rowCell
	key      string // as written; "" when the cell is blank
	foldKey  string
	positive bool // a positive cell other than the key
	text     string
}

// CompileRow compiles a row's selector cells, keyed by RowColumns; other
// keys are ignored and a blank cell is an absent dimension. Cells are
// trimmed of white space here, so a caller may pass them verbatim. The
// error is a *RowError: a malformed pattern or prefix, an illegal key, or
// a row with no positive cell (a row of only negations, or no selector at
// all). A key counts as a positive cell, so a row holding only a key
// stands.
func CompileRow(cells map[string]string) (Row, error) {
	var row Row
	var text []string
	for _, column := range RowColumns {
		raw := strings.TrimSpace(cells[column])
		if raw == "" {
			continue
		}
		text = append(text, column+"="+raw)
		if column == "credkey" {
			if reason := CheckKey(raw); reason != "" {
				return Row{}, &RowError{Column: column, Reason: reason}
			}
			row.key, row.foldKey = raw, FoldKey(raw)
			continue
		}
		c, cidr, err := compileValue(ruleKey(column), raw)
		if err != nil {
			return Row{}, &RowError{Column: column, Reason: cellReason(err)}
		}
		if !c.negated {
			row.positive = true
		}
		row.cells = append(row.cells, rowCell{column: column, compiled: c, cidr: cidr})
	}
	switch {
	case len(text) == 0:
		return Row{}, &RowError{Reason: "the row has no selector: it needs at least one positive selector cell or a credkey"}
	case !row.positive && row.key == "":
		return Row{}, &RowError{Reason: "the row holds only negated selectors: it needs at least one positive selector cell or a credkey"}
	}
	row.text = strings.Join(text, " ")
	return row, nil
}

// cellReason says why a cell did not compile without quoting the cell. A
// map rule's error quotes its value, which is configuration; a row's cell
// comes from a credential file, where a misplaced delimiter can shift a
// password into a selector column, so a row's error names the column and
// the fault and never the text (never a cell's value).
func cellReason(err error) string {
	var re *RuleError
	if errors.As(err, &re) && re.CIDR {
		return "the cell is not a CIDR prefix (address/bits, such as 10.1.0.0/16)"
	}
	var pe *PatternError
	if errors.As(err, &pe) {
		// The reversed-range reason quotes the range's two characters.
		if strings.HasPrefix(pe.Reason, "the range ") {
			return "the selector is invalid: a character range is reversed"
		}
		return "the selector is invalid: " + pe.Reason
	}
	return "the cell is not a valid selector"
}

// ruleKey is the map rule key whose compilation a column shares.
func ruleKey(column string) string {
	switch column {
	case "device_name":
		return "name"
	case "address_cidr":
		return "address-cidr"
	case "device_group":
		return "device-group"
	}
	return column
}

// Key is the row's credkey as written, "" for a blank cell.
func (r Row) Key() string { return r.key }

// String is the row's filled selector cells as written, in column order
// ("device_name=sw-nyc-* site=nyc"): the pattern of the row's match
// evidence. Selectors are not secret.
func (r Row) String() string { return r.text }

// Match reports whether the row matches the device. Every filled cell
// must agree: a positive cell matches, a negative
// cell does not match. A positive address_cidr cell never matches a device
// with no address, and a negative one excludes nothing there.
//
// The key column is a pin, not a filter. A device that pins a key
// (CredKeyRef set) matches only a row whose key equals it, the row's other
// filled cells still applying; it never matches a keyless row. A device
// that pins nothing ignores the key column, so a row matches it by its
// other cells alone; a row whose only positive cell is its key then has
// nothing positive left and matches no unpinned device, which keeps a
// key-only row from acting as a catch-all.
func (r Row) Match(f RowFields) bool {
	if f.CredKeyRef != "" {
		if r.key == "" || r.foldKey != FoldKey(strings.TrimSpace(f.CredKeyRef)) {
			return false
		}
	} else if !r.positive {
		return false
	}
	for _, c := range r.cells {
		if c.matches(f.Fields) == c.negated {
			return false
		}
	}
	return true
}

func (c rowCell) matches(f Fields) bool {
	switch {
	case c.cidr:
		return f.Address.IsValid() && c.prefix.Contains(f.Address)
	case c.column == "device_group":
		for _, g := range f.Groups {
			if c.selector.Match(g) {
				return true
			}
		}
		return false
	}
	return c.selector.Match(fieldValue(ruleKey(c.column), f))
}

// FirstRow is the index of the first row that matches f, in the order
// given, which is the file's: there is no longest-prefix
// rule, so an operator who wants a /16 to win over a /8 puts it first. It
// returns -1 when no row matches.
func FirstRow(rows []Row, f RowFields) int {
	for i, r := range rows {
		if r.Match(f) {
			return i
		}
	}
	return -1
}
