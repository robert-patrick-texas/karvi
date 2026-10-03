// Package display owns all human-facing formatting used by karvi's terminal
// output. Machine records and audit events deliberately do not use this
// package; they retain full RFC 3339 timestamps with timezone information.
package display

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

const (
	// DefaultTimestampPattern is intentionally readable by operators rather
	// than using Go's reference-time layout syntax.
	DefaultTimestampPattern = "hh:mm:ss yyyy-mm-dd"
	maxTemplateBytes        = 64 << 10
)

// Values contains the safe, non-secret fields available to header and footer
// templates. Empty fields render as empty strings rather than triggering an
// implicit fallback or exposing internal values.
type Values struct {
	Timestamp   time.Time
	Target      string
	Address     string
	Platform    string
	User        string
	AuthBackend string
	Transport   string
	ReferenceID string
	ExitStatus  string
	ExitCode    int
	Artifacts   string
	Status      string
	Elapsed     time.Duration
	// The ICMP gate's line (display.ping.header):
	// the two probes' results and the verdict, proceeding or skipped.
	RTT1   string
	RTT2   string
	Result string
	// A recorded login's transcript (display.record.header and
	// display.record.footer): the file's path.
	Transcript string
	// A job's collection (display.collection.footer): the directory and
	// how many devices' files were replaced and kept.
	Collection string
	Replaced   int
	Kept       int
}

var allowedPlaceholders = map[string]bool{
	"timestamp": true, "target": true, "address": true, "platform": true,
	"user": true, "auth-backend": true, "transport": true,
	"reference-id": true, "exit-status": true, "exit-code": true, "artifacts": true,
	"status": true, "elapsed": true,
	"rtt1": true, "rtt2": true, "result": true,
	"transcript": true,
	"collection": true, "replaced": true, "kept": true,
}

var repeatRE = regexp.MustCompile(`<repeat:([^:>]*)\:([0-9]+)>`)

// Formatter is immutable and safe for concurrent use.
type Formatter struct {
	pattern  string
	layout   string
	location *time.Location
}

// NewFormatter validates the human timestamp pattern and selects the requested
// display timezone. "auto" uses the host's local timezone.
func NewFormatter(pattern, zone string) (Formatter, error) {
	layout, err := CompileTimestampPattern(pattern)
	if err != nil {
		return Formatter{}, err
	}
	location, err := Location(zone)
	if err != nil {
		return Formatter{}, err
	}
	return Formatter{pattern: pattern, layout: layout, location: location}, nil
}

// Location resolves the effective timezone setting: "auto" or empty is the
// host's local timezone, anything else an IANA zone name.
func Location(zone string) (*time.Location, error) {
	zone = strings.TrimSpace(zone)
	if zone == "" || strings.EqualFold(zone, "auto") {
		return time.Local, nil
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return nil, errorcodes.Errorf("display_timezone_invalid", "display timezone %q: %w", zone, err)
	}
	return location, nil
}

// Timestamp formats one human-facing timestamp. Stored timestamps must use the
// original time.Time value and are not affected by this method.
func (f Formatter) Timestamp(value time.Time) string {
	if value.IsZero() {
		value = time.Now()
	}
	return value.In(f.location).Format(f.layout)
}

// RenderLine expands a validated one-line header or footer template.
func (f Formatter) RenderLine(template string, values Values) (string, error) {
	if err := ValidateLineTemplate(template); err != nil {
		return "", err
	}
	if template == "" {
		return "", nil
	}
	mapping := f.placeholderValues(values)
	var out strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '<' {
			out.WriteByte(template[i])
			i++
			continue
		}
		end := strings.IndexByte(template[i:], '>')
		if end < 0 {
			return "", errorcodes.Errorf("display_placeholder_unterminated", "display template contains an unterminated placeholder")
		}
		end += i
		name := template[i+1 : end]
		value, ok := mapping[name]
		if !ok {
			return "", errorcodes.Errorf("display_placeholder_unsupported", "unsupported display placeholder <%s>", name)
		}
		out.WriteString(value)
		i = end + 1
	}
	if out.Len() > maxTemplateBytes {
		return "", errorcodes.Errorf("display_line_too_long", "rendered display line exceeds %d bytes", maxTemplateBytes)
	}
	return out.String(), nil
}

// LineStyle defines semantic colors for generated header and footer fields.
// Literal template text is treated as a label, which makes brackets and labels
// such as "platform=" consistently gray without forcing operators to embed ANSI
// escapes in configuration.
type LineStyle struct {
	Enabled   bool
	Target    string
	Address   string
	Label     string
	Value     string
	Timestamp string
	Accent    string
	Success   string
	Warning   string
	Error     string
	Muted     string
}

// RenderStyledLines renders a header or footer using terminal-width-aware
// element boundaries. Values are treated as indivisible units, so a long
// hostname, address, timestamp, or path is never split by karvi itself. When
// the rendered line exceeds width, karvi first moves an artifacts element to
// a second line; otherwise it chooses the latest literal-whitespace boundary
// that best avoids terminal wrapping. Width <= 0 disables layout changes.
func (f Formatter) RenderStyledLines(template string, values Values, style LineStyle, width int) ([]string, error) {
	templates, err := f.layoutTemplates(template, values, width)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(templates))
	for _, lineTemplate := range templates {
		line, renderErr := f.RenderStyledLine(lineTemplate, values, style)
		if renderErr != nil {
			return nil, renderErr
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

// RenderLines is the non-ANSI companion to RenderStyledLines.
func (f Formatter) RenderLines(template string, values Values, width int) ([]string, error) {
	templates, err := f.layoutTemplates(template, values, width)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, len(templates))
	for _, lineTemplate := range templates {
		line, renderErr := f.RenderLine(lineTemplate, values)
		if renderErr != nil {
			return nil, renderErr
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines, nil
}

func (f Formatter) layoutTemplates(template string, values Values, width int) ([]string, error) {
	plain, err := f.RenderLine(template, values)
	if err != nil {
		return nil, err
	}
	if template == "" || width <= 0 || VisibleWidth(plain) <= width {
		return []string{template}, nil
	}

	// Artifact paths are commonly the longest footer value. Keep the entire
	// labeled element intact and move it to a dedicated line before considering
	// generic element-boundary splitting.
	if values.Artifacts != "" {
		if placeholder := strings.Index(template, "<artifacts>"); placeholder >= 0 {
			start := elementStart(template, placeholder)
			left := strings.TrimSpace(template[:start])
			right := strings.TrimSpace(template[start:])
			if left != "" && right != "" {
				return []string{left, right}, nil
			}
		}
	}

	type candidate struct {
		left, right           string
		leftWidth, rightWidth int
	}
	candidates := make([]candidate, 0)
	for _, boundary := range templateBoundaries(template) {
		left := strings.TrimSpace(template[:boundary.start])
		right := strings.TrimSpace(template[boundary.end:])
		if left == "" || right == "" {
			continue
		}
		leftPlain, leftErr := f.RenderLine(left, values)
		if leftErr != nil {
			return nil, leftErr
		}
		rightPlain, rightErr := f.RenderLine(right, values)
		if rightErr != nil {
			return nil, rightErr
		}
		candidates = append(candidates, candidate{
			left: left, right: right,
			leftWidth: VisibleWidth(leftPlain), rightWidth: VisibleWidth(rightPlain),
		})
	}
	if len(candidates) == 0 {
		// A line containing one indivisible value cannot be safely split without
		// changing operator data. Let the terminal decide how to display it.
		return []string{template}, nil
	}

	best := -1
	for i := range candidates {
		if candidates[i].leftWidth <= width && candidates[i].rightWidth <= width {
			if best < 0 || candidates[i].leftWidth > candidates[best].leftWidth {
				best = i
			}
		}
	}
	if best < 0 {
		for i := range candidates {
			if candidates[i].leftWidth <= width {
				if best < 0 || candidates[i].leftWidth > candidates[best].leftWidth {
					best = i
				}
			}
		}
	}
	if best < 0 {
		best = 0
		for i := 1; i < len(candidates); i++ {
			if candidates[i].leftWidth < candidates[best].leftWidth {
				best = i
			}
		}
	}
	return []string{candidates[best].left, candidates[best].right}, nil
}

type splitBoundary struct{ start, end int }

func templateBoundaries(template string) []splitBoundary {
	boundaries := []splitBoundary{}
	insidePlaceholder := false
	for i := 0; i < len(template); {
		switch template[i] {
		case '<':
			insidePlaceholder = true
			i++
			continue
		case '>':
			insidePlaceholder = false
			i++
			continue
		}
		if insidePlaceholder {
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(template[i:])
		if !unicode.IsSpace(r) {
			i += size
			continue
		}
		start := i
		for i < len(template) {
			r, size = utf8.DecodeRuneInString(template[i:])
			if !unicode.IsSpace(r) {
				break
			}
			i += size
		}
		boundaries = append(boundaries, splitBoundary{start: start, end: i})
	}
	return boundaries
}

func elementStart(template string, placeholder int) int {
	boundaries := templateBoundaries(template[:placeholder])
	if len(boundaries) == 0 {
		return 0
	}
	return boundaries[len(boundaries)-1].end
}

// RenderStyledLine expands the same safe placeholders as RenderLine while
// applying semantic ANSI colors. Machine output must continue to use
// RenderLine or structured records and never this terminal projection.
func (f Formatter) RenderStyledLine(template string, values Values, style LineStyle) (string, error) {
	if err := ValidateLineTemplate(template); err != nil {
		return "", err
	}
	if template == "" {
		return "", nil
	}
	mapping := f.placeholderValues(values)
	var out strings.Builder
	for i := 0; i < len(template); {
		if template[i] != '<' {
			next := strings.IndexByte(template[i:], '<')
			if next < 0 {
				next = len(template) - i
			}
			literal := template[i : i+next]
			out.WriteString(ANSIStyle(literal, style.Label, style.Enabled, false))
			i += next
			continue
		}
		end := strings.IndexByte(template[i:], '>')
		if end < 0 {
			return "", errorcodes.Errorf("display_placeholder_unterminated", "display template contains an unterminated placeholder")
		}
		end += i
		name := template[i+1 : end]
		value, ok := mapping[name]
		if !ok {
			return "", errorcodes.Errorf("display_placeholder_unsupported", "unsupported display placeholder <%s>", name)
		}
		color, bold := placeholderStyle(name, value, style)
		out.WriteString(ANSIStyle(value, color, style.Enabled, bold))
		i = end + 1
	}
	if out.Len() > maxTemplateBytes*4 {
		// ANSI escapes add bounded overhead. Keep a hard limit even for an
		// operator who chooses an unusually placeholder-dense template.
		return "", errorcodes.Errorf("display_line_too_long", "rendered styled display line exceeds %d bytes", maxTemplateBytes*4)
	}
	return out.String(), nil
}

func (f Formatter) placeholderValues(values Values) map[string]string {
	return map[string]string{
		"timestamp":    f.Timestamp(values.Timestamp),
		"target":       values.Target,
		"address":      values.Address,
		"platform":     values.Platform,
		"user":         values.User,
		"auth-backend": values.AuthBackend,
		"transport":    values.Transport,
		"reference-id": values.ReferenceID,
		"exit-status":  values.ExitStatus,
		"exit-code":    strconv.Itoa(values.ExitCode),
		"artifacts":    values.Artifacts,
		"status":       values.Status,
		"elapsed":      formatElapsed(values.Elapsed),
		"rtt1":         values.RTT1,
		"rtt2":         values.RTT2,
		"result":       values.Result,
		"transcript":   values.Transcript,
		"collection":   values.Collection,
		"replaced":     strconv.Itoa(values.Replaced),
		"kept":         strconv.Itoa(values.Kept),
	}
}

// RoleBold is true of the roles the display renders in bold as well as in
// their colour (the target and the address, and the warning, which a status
// line always rendered bold), so a colour
// test shows a role as a line shows it.
func RoleBold(role string) bool { return role == "target" || role == "address" || role == "warning" }

func placeholderStyle(name, value string, style LineStyle) (string, bool) {
	switch name {
	case "target":
		return style.Target, RoleBold(name)
	case "address":
		return style.Address, RoleBold(name)
	case "timestamp":
		return style.Timestamp, false
	case "reference-id":
		return style.Accent, false
	case "result":
		// The gate's verdict (28.3): proceeding in the success role, skipped
		// in the error role, bold as a status is.
		if value == "proceeding" {
			return style.Success, true
		}
		return style.Error, true
	case "exit-status", "status":
		lower := strings.ToLower(value)
		switch {
		case strings.Contains(lower, "success"), strings.Contains(lower, "completed"):
			return style.Success, true
		case strings.Contains(lower, "halt"), strings.Contains(lower, "warning"), strings.Contains(lower, "partial"):
			return style.Warning, true
		default:
			return style.Error, true
		}
	default:
		return style.Value, false
	}
}

var ansiSequenceRE = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)

// StripANSI removes terminal control sequences introduced by karvi's display
// renderer. It is also used by width calculations so color never consumes a
// reported terminal column.
func StripANSI(value string) string {
	return ansiSequenceRE.ReplaceAllString(value, "")
}

// VisibleWidth returns the approximate terminal-cell width of text after ANSI
// removal. Combining marks consume no cells and common East Asian wide ranges
// consume two. Newlines reset the current line and return the widest line.
func VisibleWidth(value string) int {
	value = StripANSI(value)
	current, widest := 0, 0
	for _, r := range value {
		switch r {
		case '\r':
			continue
		case '\n':
			if current > widest {
				widest = current
			}
			current = 0
			continue
		}
		current += runeCellWidth(r)
	}
	if current > widest {
		widest = current
	}
	return widest
}

// CropLines truncates only lines wider than width. It preserves newline
// placement and never pads shorter lines. Callers use it only for generated
// borders; device and echo output must not be modified to fit the terminal.
func CropLines(value string, width int) string {
	if width <= 0 || value == "" {
		return value
	}
	parts := strings.SplitAfter(value, "\n")
	var out strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		hasNewline := strings.HasSuffix(part, "\n")
		line := strings.TrimSuffix(part, "\n")
		line = strings.TrimSuffix(line, "\r")
		out.WriteString(cropVisible(line, width))
		if hasNewline {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

// cropVisible cuts one line at width terminal cells. An escape sequence
// counts no cells and is kept whole, and a cut line that carried one ends
// with a reset, so a colour opened before the cut cannot run on into what
// follows (the watch screen writes its lines with cursor addressing, not
// newlines, so nothing else would close it).
func cropVisible(value string, width int) string {
	if width <= 0 || VisibleWidth(value) <= width {
		return value
	}
	var out strings.Builder
	used := 0
	escaped := false
	rest := value
	for rest != "" {
		if rest[0] == '\x1b' {
			if loc := ansiSequenceRE.FindStringIndex(rest); loc != nil && loc[0] == 0 {
				out.WriteString(rest[:loc[1]])
				rest = rest[loc[1]:]
				escaped = true
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(rest)
		cells := runeCellWidth(r)
		if used+cells > width {
			break
		}
		out.WriteRune(r)
		used += cells
		rest = rest[size:]
	}
	if escaped {
		out.WriteString("\x1b[0m")
	}
	return out.String()
}

func runeCellWidth(r rune) int {
	if r == 0 || r < 0x20 || (r >= 0x7f && r < 0xa0) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWideRune(r) {
		return 2
	}
	return 1
}

func isWideRune(r rune) bool {
	return r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) ||
		(r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) ||
		(r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) ||
		(r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f300 && r <= 0x1f64f) ||
		(r >= 0x1f900 && r <= 0x1f9ff) ||
		(r >= 0x20000 && r <= 0x3fffd))
}

// RenderBorder expands <repeat:TEXT:COUNT> directives in an operator-supplied
// border. Literal newlines are preserved exactly. Example:
//
//	<repeat:-:40>\n
func RenderBorder(template string) (string, error) {
	if err := ValidateBorder(template); err != nil {
		return "", err
	}
	if template == "" {
		return "", nil
	}
	var renderErr error
	out := repeatRE.ReplaceAllStringFunc(template, func(match string) string {
		parts := repeatRE.FindStringSubmatch(match)
		count, err := strconv.Atoi(parts[2])
		if err != nil || count < 1 || count > 4096 {
			renderErr = errorcodes.Errorf("display_repeat_count_invalid", "invalid repeat count %q", parts[2])
			return ""
		}
		return strings.Repeat(parts[1], count)
	})
	if renderErr != nil {
		return "", renderErr
	}
	if len(out) > maxTemplateBytes {
		return "", errorcodes.Errorf("display_border_render_too_long", "rendered display border exceeds %d bytes", maxTemplateBytes)
	}
	return out, nil
}

// ValidateLineTemplate rejects multiline headers/footers and unknown fields.
func ValidateLineTemplate(template string) error {
	if len(template) > maxTemplateBytes {
		return errorcodes.Errorf("display_template_too_long", "display template exceeds %d bytes", maxTemplateBytes)
	}
	if strings.ContainsAny(template, "\r\n") {
		return errorcodes.Errorf("display_template_multiline", "display header/footer templates must be one line")
	}
	for i := 0; i < len(template); {
		if template[i] != '<' {
			i++
			continue
		}
		end := strings.IndexByte(template[i:], '>')
		if end < 0 {
			return errorcodes.Errorf("display_placeholder_unterminated", "display template contains an unterminated placeholder")
		}
		end += i
		name := template[i+1 : end]
		if !allowedPlaceholders[name] {
			return errorcodes.Errorf("display_placeholder_unsupported", "unsupported display placeholder <%s>", name)
		}
		i = end + 1
	}
	return nil
}

// ValidateBorder checks repeat directives and size without altering literals.
func ValidateBorder(template string) error {
	if len(template) > maxTemplateBytes {
		return errorcodes.Errorf("display_border_template_too_long", "display border template exceeds %d bytes", maxTemplateBytes)
	}
	for offset := 0; ; {
		idx := strings.Index(template[offset:], "<repeat:")
		if idx < 0 {
			break
		}
		idx += offset
		end := strings.IndexByte(template[idx:], '>')
		if end < 0 {
			return errorcodes.Errorf("display_repeat_unterminated", "display border contains an unterminated repeat directive")
		}
		end += idx
		match := template[idx : end+1]
		parts := repeatRE.FindStringSubmatch(match)
		if len(parts) != 3 || parts[1] == "" {
			return errorcodes.Errorf("display_repeat_syntax_invalid", "display repeat syntax is <repeat:TEXT:COUNT>")
		}
		count, err := strconv.Atoi(parts[2])
		if err != nil || count < 1 || count > 4096 {
			return errorcodes.Errorf("display_repeat_count_invalid", "display repeat count must be 1..4096")
		}
		offset = end + 1
	}
	_, err := RenderBorderUnchecked(template)
	return err
}

func RenderBorderUnchecked(template string) (string, error) {
	var renderErr error
	out := repeatRE.ReplaceAllStringFunc(template, func(match string) string {
		parts := repeatRE.FindStringSubmatch(match)
		count, err := strconv.Atoi(parts[2])
		if err != nil || count < 1 || count > 4096 {
			renderErr = errorcodes.Errorf("display_repeat_count_invalid", "invalid repeat count %q", parts[2])
			return ""
		}
		return strings.Repeat(parts[1], count)
	})
	if renderErr != nil {
		return "", renderErr
	}
	if len(out) > maxTemplateBytes {
		return "", errorcodes.Errorf("display_border_render_too_long", "rendered display border exceeds %d bytes", maxTemplateBytes)
	}
	return out, nil
}

// CompileTimestampPattern translates the documented operator tokens to a Go
// layout. Capital MM is the unambiguous month token. Lowercase mm is accepted
// as a month when adjacent to date tokens and as minutes when adjacent to time
// tokens, which intentionally supports the examples in the operator request.
func CompileTimestampPattern(pattern string) (string, error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", errorcodes.Errorf("display_timestamp_blank", "display.timestamp must not be blank")
	}
	if len(pattern) > 256 {
		return "", errorcodes.Errorf("display_timestamp_too_long", "display.timestamp exceeds 256 bytes")
	}
	tokens, err := tokenizeTimestamp(pattern)
	if err != nil {
		return "", err
	}
	hasField := false
	var out strings.Builder
	for i, token := range tokens {
		if !token.field {
			out.WriteString(token.text)
			continue
		}
		hasField = true
		switch token.text {
		case "YYYY", "yyyy":
			out.WriteString("2006")
		case "YY", "yy":
			out.WriteString("06")
		case "MM":
			out.WriteString("01")
		case "DD", "dd":
			out.WriteString("02")
		case "HH", "hh":
			out.WriteString("15")
		case "ss":
			out.WriteString("05")
		case "SSS", "sss":
			out.WriteString("000")
		case "mm":
			kind := classifyLowerMM(tokens, i)
			if kind == "month" {
				out.WriteString("01")
			} else if kind == "minute" {
				out.WriteString("04")
			} else {
				return "", errorcodes.Errorf("display_timestamp_mm_ambiguous", "ambiguous mm in display.timestamp; use MM for month or place mm between hh and ss for minutes")
			}
		default:
			return "", errorcodes.Errorf("display_timestamp_token_unsupported", "unsupported display timestamp token %q", token.text)
		}
	}
	if !hasField {
		return "", errorcodes.Errorf("display_timestamp_token_missing", "display.timestamp must contain at least one timestamp token")
	}
	return out.String(), nil
}

type timestampToken struct {
	text  string
	field bool
}

var timestampFields = []string{"YYYY", "yyyy", "SSS", "sss", "YY", "yy", "MM", "DD", "dd", "HH", "hh", "mm", "ss"}

func tokenizeTimestamp(pattern string) ([]timestampToken, error) {
	out := []timestampToken{}
	var literal strings.Builder
	flush := func() {
		if literal.Len() > 0 {
			out = append(out, timestampToken{text: literal.String()})
			literal.Reset()
		}
	}
	for i := 0; i < len(pattern); {
		if pattern[i] == '\\' {
			if i+1 >= len(pattern) {
				return nil, errorcodes.Errorf("display_timestamp_trailing_escape", "display.timestamp ends with an escape")
			}
			literal.WriteByte(pattern[i+1])
			i += 2
			continue
		}
		matched := ""
		for _, candidate := range timestampFields {
			if strings.HasPrefix(pattern[i:], candidate) {
				matched = candidate
				break
			}
		}
		if matched == "" {
			literal.WriteByte(pattern[i])
			i++
			continue
		}
		flush()
		out = append(out, timestampToken{text: matched, field: true})
		i += len(matched)
	}
	flush()
	return out, nil
}

func classifyLowerMM(tokens []timestampToken, index int) string {
	prev := nearestField(tokens, index, -1)
	next := nearestField(tokens, index, 1)
	isTime := func(value string) bool {
		return value == "HH" || value == "hh" || value == "ss" || value == "SSS" || value == "sss"
	}
	isDate := func(value string) bool {
		return value == "YYYY" || value == "yyyy" || value == "YY" || value == "yy" || value == "DD" || value == "dd" || value == "MM"
	}
	if isTime(prev) || isTime(next) {
		return "minute"
	}
	if isDate(prev) || isDate(next) {
		return "month"
	}
	return ""
}

func nearestField(tokens []timestampToken, index, direction int) string {
	for i := index + direction; i >= 0 && i < len(tokens); i += direction {
		if tokens[i].field {
			return tokens[i].text
		}
	}
	return ""
}

func formatElapsed(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	if value < time.Second {
		return value.Round(time.Millisecond).String()
	}
	return value.Round(10 * time.Millisecond).String()
}

// ColorEnabled applies the central terminal color policy.
func ColorEnabled(mode, theme string, terminal bool) bool {
	if strings.EqualFold(theme, "nocolor") || strings.EqualFold(mode, "never") {
		return false
	}
	if strings.EqualFold(mode, "always") {
		return true
	}
	return terminal
}

// EffectiveTheme resolves auto using the widely supported COLORFGBG hint.
// When the terminal exposes no usable hint, karvi chooses dark because it is
// the documented release default and gives the most legible warning palette
// on common operations-server terminals.
func EffectiveTheme(theme string) string {
	theme = strings.ToLower(strings.TrimSpace(theme))
	if theme == "" {
		theme = "dark"
	}
	if theme != "auto" {
		return theme
	}
	parts := strings.Split(os.Getenv("COLORFGBG"), ";")
	if len(parts) > 0 {
		if background, err := strconv.Atoi(parts[len(parts)-1]); err == nil && background >= 7 {
			return "light"
		}
	}
	return "dark"
}

// RoleColor combines a theme palette with an optional role-specific override.
// The configured value "default" delegates to the palette; every other
// validated color name wins directly. The dark palette's address is magenta
// as the light palette's (bold magenta in the command header under both
// themes, where bold blue sat
// beside the blue of a light-theme accent) and its label is blue (in place
// of gray, which the muted, border, and timestamp roles already take).
func RoleColor(theme, role, configured string) string {
	configured = strings.ToLower(strings.TrimSpace(configured))
	if configured != "" && configured != "default" {
		return configured
	}
	palettes := map[string]map[string]string{
		"dark": {
			"success": "green", "warning": "orange", "error": "red",
			"muted": "gray", "timestamp": "gray", "accent": "cyan", "target": "yellow",
			"address": "magenta", "label": "blue", "value": "white", "border": "gray", "dynamic-border": "gray",
		},
		"light": {
			"success": "green", "warning": "orange", "error": "red",
			"muted": "gray", "timestamp": "gray", "accent": "blue", "target": "blue",
			"address": "magenta", "label": "gray", "value": "black", "border": "gray", "dynamic-border": "gray",
		},
	}
	palette := palettes[EffectiveTheme(theme)]
	if palette == nil {
		palette = palettes["dark"]
	}
	if value := palette[strings.ToLower(strings.TrimSpace(role))]; value != "" {
		return value
	}
	return "default"
}

// ANSI wraps text in a named foreground color. Unknown and default names leave
// text unchanged so a display preference can never corrupt operator output.
func ANSI(text, color string, enabled bool) string {
	return ANSIStyle(text, color, enabled, false)
}

// ANSIStyle applies a named foreground color and optional bold emphasis.
func ANSIStyle(text, color string, enabled, bold bool) string {
	if !enabled || text == "" {
		return text
	}
	prefix := ANSIPrefix(color, bold)
	if prefix == "" {
		return text
	}
	return prefix + text + "\x1b[0m"
}

// ANSIPrefix is the escape that opens a named foreground color with optional
// bold emphasis, "" for an unknown or default name without bold: what
// ANSIStyle writes before its text, and what the colour test shows.
func ANSIPrefix(color string, bold bool) string {
	// orange is the 256-colour index 208 (the 16-colour set has none): a
	// terminal without 256-colour support shows its nearest colour.
	codes := map[string]string{
		"black": "30", "red": "31", "green": "32", "yellow": "33",
		"blue": "34", "magenta": "35", "cyan": "36", "white": "37",
		"gray": "90", "grey": "90", "orange": "38;5;208", "default": "39",
	}
	code, ok := codes[strings.ToLower(strings.TrimSpace(color))]
	parts := []string{}
	if bold {
		parts = append(parts, "1")
	}
	if ok && code != "39" {
		parts = append(parts, code)
	}
	if len(parts) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
}
