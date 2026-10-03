// Package errorcodes is the single registry of karvi error codes.
// Every distinct cause karvi can report has exactly one registered code; the
// registry generates docs/ERROR-CODES.md, and tests fail when source emits an
// unregistered code or the generated table is stale.
package errorcodes

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/exitcode"
)

// Kind separates operator-facing errors from notices and record reasons.
type Kind string

const (
	// KindError is a failure cause.
	KindError Kind = "error"
	// KindNotice is a warning that does not fail the activity.
	KindNotice Kind = "notice"
	// KindReason explains why a record was not started or was stopped.
	KindReason Kind = "reason"
)

// Status is the lifecycle state of a code. Codes are never reused.
type Status string

const (
	// Active codes are emitted by the current source.
	Active Status = "active"
	// Planned codes are specified but not yet emitted.
	Planned Status = "planned"
	// Retired codes are no longer emitted; ReplacedBy names their successors.
	Retired Status = "retired"
)

// Entry describes one registered code.
type Entry struct {
	Code     string
	Kind     Kind
	Status   Status
	Category string
	// Exit is the process exit status when this code decides the result of
	// a direct activity; 0 means the code never sets the exit by itself.
	Exit      int
	Retryable bool
	// Unclassified marks a fallback used only when no specific cause is known.
	Unclassified bool
	Cause        string
	// RetiredIn names the release, or the span of releases, that retired the code.
	RetiredIn  string
	ReplacedBy []string
}

var (
	codePattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	prefixRE    = regexp.MustCompile(`^([a-z][a-z0-9]*(?:_[a-z0-9]+)+)(?::|$)`)
	index       = buildIndex()
)

func buildIndex() map[string]Entry {
	m := make(map[string]Entry, len(entries))
	for _, e := range entries {
		m[e.Code] = e
	}
	return m
}

// All returns every registered entry sorted by code.
func All() []Entry {
	out := append([]Entry(nil), entries...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Lookup returns the entry for code.
func Lookup(code string) (Entry, bool) {
	e, ok := index[code]
	return e, ok
}

// ActiveWithPrefix returns the sorted active codes that start with prefix.
func ActiveWithPrefix(prefix string) []string {
	var out []string
	for _, e := range All() {
		if e.Status == Active && strings.HasPrefix(e.Code, prefix) {
			out = append(out, e.Code)
		}
	}
	return out
}

// Coded is implemented by error types that carry a registered code.
type Coded interface {
	ErrorCode() string
}

// Of returns the first active registered code carried by err or any error it
// wraps, either through Coded or as a leading "code:" message prefix. It
// returns "" when err carries no registered code.
func Of(err error) string {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if c, ok := e.(Coded); ok && isActive(c.ErrorCode()) {
			return c.ErrorCode()
		}
		if m := prefixRE.FindStringSubmatch(e.Error()); m != nil && isActive(m[1]) {
			return m[1]
		}
	}
	return ""
}

// ExitFor returns the registered exit status for the code carried by err, or
// fallback when err carries no code or the code does not set an exit.
func ExitFor(err error, fallback int) int {
	if e, ok := Lookup(Of(err)); ok && e.Exit != 0 {
		return e.Exit
	}
	return fallback
}

// ExitAt returns the exit status for err reported at a site whose cause is
// code: the exit of err's own code when it sets one, then the exit of code,
// then the generic error exit.
func ExitAt(err error, code string) int {
	for e := err; e != nil; e = errors.Unwrap(e) {
		if s, ok := e.(*exitError); ok {
			return s.exit
		}
	}
	if e, ok := Lookup(Of(err)); ok && e.Exit != 0 {
		return e.Exit
	}
	if e, ok := Lookup(code); ok && e.Exit != 0 {
		return e.Exit
	}
	return exitcode.ExitGenericError
}

// exitError fixes the exit status for a stage whose contract defines one
// exit for every cause, such as configuration loading (exit 2, or 3 for a lock
// violation). The wrapped error keeps its own code and message.
type exitError struct {
	err  error
	exit int
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// WithExit returns err with its exit status fixed to exit by ExitAt.
func WithExit(err error, exit int) error {
	if err == nil {
		return nil
	}
	return &exitError{err: err, exit: exit}
}

// codedError attaches a registered code to an error.
type codedError struct {
	code string
	err  error
}

func (e *codedError) Error() string     { return e.code + ": " + e.err.Error() }
func (e *codedError) Unwrap() error     { return e.err }
func (e *codedError) ErrorCode() string { return e.code }

// Errorf formats like fmt.Errorf, including %w, and attaches code.
func Errorf(code, format string, args ...any) error {
	return &codedError{code: code, err: fmt.Errorf(format, args...)}
}

// Ensure returns err unchanged when it already carries an active registered
// code. Otherwise it attaches code, which names the cause at the reporting
// site, so no error reaches an operator without a code.
func Ensure(err error, code string) error {
	if err == nil || Of(err) != "" {
		return err
	}
	return &codedError{code: code, err: err}
}

// Message renders err for an operator with its code first and exactly once.
func Message(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	code := Of(err)
	if code == "" {
		return msg
	}
	return code + ": " + strings.TrimSpace(strings.ReplaceAll(msg, code+": ", ""))
}

func isActive(code string) bool {
	e, ok := index[code]
	return ok && e.Status == Active
}

// Validate reports registry defects: malformed or duplicate codes, missing
// fields, and retired codes whose successors are not registered.
func Validate() []error {
	var errs []error
	seen := map[string]bool{}
	for _, e := range entries {
		if !codePattern.MatchString(e.Code) {
			errs = append(errs, fmt.Errorf("%q is not a lowercase code of letters, digits, and underscores", e.Code))
		}
		if seen[e.Code] {
			errs = append(errs, fmt.Errorf("%s is registered more than once", e.Code))
		}
		seen[e.Code] = true
		if strings.TrimSpace(e.Cause) == "" {
			errs = append(errs, fmt.Errorf("%s has no cause", e.Code))
		}
		switch e.Kind {
		case KindError, KindNotice, KindReason:
		default:
			errs = append(errs, fmt.Errorf("%s has unknown kind %q", e.Code, e.Kind))
		}
		if _, ok := exitcode.Lookup(e.Exit); e.Exit != 0 && !ok {
			errs = append(errs, fmt.Errorf("%s names exit %d, which internal/exitcode does not define", e.Code, e.Exit))
		}
		switch e.Status {
		case Active, Planned:
			if e.Category == "" {
				errs = append(errs, fmt.Errorf("%s has no category", e.Code))
			}
			if e.RetiredIn != "" || len(e.ReplacedBy) > 0 {
				errs = append(errs, fmt.Errorf("%s is %s but has retirement data", e.Code, e.Status))
			}
		case Retired:
			if e.RetiredIn == "" || len(e.ReplacedBy) == 0 {
				errs = append(errs, fmt.Errorf("retired %s needs RetiredIn and ReplacedBy", e.Code))
			}
		default:
			errs = append(errs, fmt.Errorf("%s has unknown status %q", e.Code, e.Status))
		}
	}
	for _, e := range entries {
		for _, r := range e.ReplacedBy {
			if s, ok := index[r]; !ok || s.Status == Retired {
				errs = append(errs, fmt.Errorf("%s is replaced by %s, which is not an active or planned code", e.Code, r))
			}
		}
	}
	return errs
}

// RenderMarkdown renders docs/ERROR-CODES.md deterministically.
func RenderMarkdown() string {
	var b strings.Builder
	b.WriteString("# karvi error codes\n\n")
	b.WriteString("<!-- Generated by tools/errorcodegen from internal/errorcodes. Do not edit. -->\n\n")
	b.WriteString("This table is generated from the error-code registry. Every distinct\n")
	b.WriteString("cause has exactly one code, and codes are never reused. A code emitted by source\n")
	b.WriteString("but missing here is a defect.\n\n")
	b.WriteString("**Exit** is the process exit status when the code decides the result of a direct\n")
	b.WriteString("activity (`login`, `command`, `config`, `daemon`, or a job rejected before\n")
	b.WriteString("dispatch). Inside `run`, per-device failures are recorded and the job exits 101\n")
	b.WriteString("unless a halt or wave gate applies. Configuration loading exits 2 for every\n")
	b.WriteString("cause (3 for `config_lock_violation`), whatever the code's own exit, because the\n")
	b.WriteString("configuration stage has one exit contract. `—` means the code does not set the exit by\n")
	b.WriteString("itself. **Unclassified** codes are fallbacks used only when no specific cause is\n")
	b.WriteString("known.\n\n")

	b.WriteString("## Exit statuses\n\n")
	b.WriteString("Every status karvi exits with, from `internal/exitcode`; the name is the one\n")
	b.WriteString("the records and the audit carry. When more than one applies to a job, the first\n")
	b.WriteString("of 111, 106, 113, 114, 102, 103, 104, and 105 is the exit.\n\n")
	b.WriteString("| Exit | Name | Meaning |\n|---:|---|---|\n")
	for _, s := range exitcode.Statuses {
		fmt.Fprintf(&b, "| %d | `%s` | %s |\n", s.Code, s.Name, s.Meaning)
	}
	b.WriteString("\n")

	section := func(title, intro string, match func(Entry) bool) {
		var rows []Entry
		for _, e := range All() {
			if match(e) {
				rows = append(rows, e)
			}
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", title, intro)
		b.WriteString("| Code | Category | Exit | Retryable | Cause |\n|---|---|---:|:---:|---|\n")
		for _, e := range rows {
			cause := e.Cause
			if e.Unclassified {
				cause = "Unclassified. " + cause
			}
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n", e.Code, e.Category, exitText(e.Exit), yesNo(e.Retryable), cause)
		}
		b.WriteString("\n")
	}
	section("Errors", "Active failure causes.", func(e Entry) bool { return e.Status == Active && e.Kind == KindError })
	section("Record reasons", "Codes recorded for commands that were not started or were stopped.", func(e Entry) bool { return e.Status == Active && e.Kind == KindReason })
	section("Notices", "Warnings that do not fail the activity.", func(e Entry) bool { return e.Status == Active && e.Kind == KindNotice })
	section("Planned", "Specified and not yet emitted.", func(e Entry) bool { return e.Status == Planned })

	b.WriteString("## Retired\n\nNo longer emitted. A retired code is never reused for another cause.\n\n")
	b.WriteString("| Code | Retired in | Replaced by | Former cause |\n|---|---|---|---|\n")
	for _, e := range All() {
		if e.Status != Retired {
			continue
		}
		repl := make([]string, len(e.ReplacedBy))
		for i, r := range e.ReplacedBy {
			repl[i] = "`" + r + "`"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", e.Code, e.RetiredIn, strings.Join(repl, ", "), e.Cause)
	}
	return b.String()
}

func exitText(code int) string {
	if code == 0 {
		return "—"
	}
	return fmt.Sprint(code)
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}
