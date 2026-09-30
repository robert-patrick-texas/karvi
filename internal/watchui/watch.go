// Package watchui is the watch screen: the
// cells and the table its three formats share, and karvi's own screen
// model, a frame diffed line by line against the terminal, with no terminal
// library vendored. The TUI runs on a live terminal or, in the tests, on a
// fake one of a fixed size.
package watchui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/records"
)

type Options struct {
	Directory        string
	Format           string
	Theme            string
	Color            string
	TimestampPattern string
	Timezone         string
	// Colors holds the display.colors.* overrides by role.
	Colors     map[string]string
	Refresh    time.Duration
	StaleAfter time.Duration
	MaxFiles   int
	Once       bool
	// Rows and Columns size the fake terminal the TUI runs on when out is
	// not a terminal (the tests); 24 by 80 when zero.
	Rows    int
	Columns int
	// Filter and Sort are --filter and --sort: the screen's
	// starting state, and the table form's rule; refused with json.
	Filter string
	Sort   string
}

// Run shows the scoreboard directory in the format asked for: json and
// table print once to out; tui runs the screen until q, Ctrl-C, or the
// context ends it, and prints nothing else. The keys come
// from in.
func Run(ctx context.Context, opts Options, in io.Reader, out io.Writer) error {
	if opts.Format == "" {
		opts.Format = "tui"
	}
	if opts.Refresh <= 0 {
		opts.Refresh = time.Second
	}
	location, err := display.Location(opts.Timezone)
	if err != nil {
		return err
	}
	file, isFile := out.(*os.File)
	colour := display.ColorEnabled(opts.Color, opts.Theme, isFile && opts.Format == "tui")
	if opts.Sort != "" && !ValidSortKey(opts.Sort) {
		return errorcodes.Errorf("watch_sort_unknown", "unknown watch sort key %q: one of %s", opts.Sort, strings.Join(SortKeys, ", "))
	}
	if opts.Format == "json" && (opts.Filter != "" || opts.Sort != "") {
		return errorcodes.Errorf("watch_json_filter_unsupported", "--filter and --sort do not apply to --format json, which prints every snapshot for the script to filter")
	}
	model := &Model{Location: location, Directory: opts.Directory, Refresh: opts.Refresh, Theme: display.EffectiveTheme(opts.Theme), Colors: opts.Colors, Colour: colour, Filter: opts.Filter, SortKey: opts.Sort}
	// read fills the model from the directory and stamps the reading.
	read := func() error {
		rows, err := scoreboard.Read(opts.Directory, opts.MaxFiles, opts.StaleAfter)
		if err != nil {
			return err
		}
		model.Rows, model.Now = rows, time.Now()
		return nil
	}
	// table prints the --format table form once: the rows, or a line
	// saying the directory holds none.
	table := func() error {
		if err := read(); err != nil {
			return err
		}
		if len(model.Rows) == 0 {
			_, err := fmt.Fprintln(out, "(no retained activities)")
			return err
		}
		for _, line := range model.TableLines() {
			if _, err := fmt.Fprintln(out, line); err != nil {
				return err
			}
		}
		return nil
	}
	switch opts.Format {
	case "json":
		if err := read(); err != nil {
			return err
		}
		values := make([]any, 0, len(model.Rows))
		for _, row := range model.Rows {
			if row.Error != "" {
				values = append(values, map[string]any{"path": row.Path, "error": row.Error})
				continue
			}
			values = append(values, row.Snapshot)
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(values)
	case "table":
		return table()
	case "tui":
	default:
		return errorcodes.Errorf("watch_format_unsupported", "unsupported watch format %q", opts.Format)
	}
	if opts.Once {
		return table()
	}
	var term *terminal
	if isFile && osutil.IsTerminal(file) {
		term = liveTerminal(in, file)
	} else {
		rows, cols := opts.Rows, opts.Columns
		if rows <= 0 {
			rows = 24
		}
		if cols <= 0 {
			cols = 80
		}
		term = fakeTerminal(in, rows, cols)
	}
	frame := func(lines, cols int) []string {
		model.Lines, model.Cols = lines, cols
		return model.Frame()
	}
	return runScreen(ctx, term, out, opts.Refresh, read, model.Key, frame)
}

// runScreen is the TUI's loop: the screen entered, the directory read and
// the frame drawn at once and at every refresh (frame takes the screen's
// size), redrawn at the terminal's new size on a resize and after every
// key, and left on q, Ctrl-C (key says so), or the context's end, when
// the terminal's mode and its screen come back whatever ended the loop.
func runScreen(ctx context.Context, term *terminal, out io.Writer, refresh time.Duration, read func() error, key func(Key) bool, frame func(lines, cols int) []string) (err error) {
	rows, cols := term.size()
	screen := NewScreen(out, rows, cols)
	if err := term.enter(); err != nil {
		return err
	}
	if err := screen.Enter(); err != nil {
		term.leave()
		return err
	}
	defer func() {
		if e := screen.Leave(); err == nil {
			err = e
		}
		if e := term.leave(); err == nil {
			err = e
		}
	}()
	draw := func() error { return screen.Draw(frame(screen.Size())) }
	refreshed := func() error {
		if err := read(); err != nil {
			return err
		}
		return draw()
	}
	if err := refreshed(); err != nil {
		return err
	}
	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	keys := term.keys
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := refreshed(); err != nil {
				return err
			}
		case <-term.resized:
			screen.Resize(term.size())
			if err := draw(); err != nil {
				return err
			}
		case k, ok := <-keys:
			if !ok {
				// The key stream ended (a pipe on stdin): the refresh goes on.
				keys = nil
				continue
			}
			if key(k) {
				return nil
			}
			if err := draw(); err != nil {
				return err
			}
		}
	}
}

// TimeCell is the TIME column: the running duration until
// the job ends, then the time it ended as HH:MM:SS in the effective
// timezone.
func TimeCell(s records.ScoreboardSnapshot, now time.Time, loc *time.Location) string {
	if s.EndedAt != nil {
		return s.EndedAt.In(loc).Format("15:04:05")
	}
	return Duration(now.Sub(s.StartedAt))
}

// Duration is a running time as an operator reads it: 4m12s, 1h02m, 37m02s.
func Duration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	h := int(d / time.Hour)
	m := int(d % time.Hour / time.Minute)
	sec := int(d % time.Minute / time.Second)
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm%02ds", m, sec)
}

// DoneCell is the DONE column's x/y: completed over total,
// x right-aligned in three and y left-aligned in three, so the slash
// stands in one place down the column; a total past 999 widens both.
func DoneCell(c records.Counts) string {
	w := 3
	if n := len(strconv.Itoa(c.Total)); n > w {
		w = n
	}
	return fmt.Sprintf("%*d/%-*d", w, c.Completed, w, c.Total)
}

// TargetCell is the TARGET column: a file input's base name
// first, then the devices in flight, then the rest, cut at width with an
// ellipsis; width 0 is the whole text (the table and json forms).
func TargetCell(s records.ScoreboardSnapshot, width int) string {
	parts := []string{}
	for _, in := range s.Inputs {
		if in.Kind == "tf" {
			parts = append(parts, in.Value)
		}
	}
	for _, t := range s.Targets {
		if t.State == records.TargetRunning {
			parts = append(parts, t.Name)
		}
	}
	for _, t := range s.Targets {
		if t.State != records.TargetRunning {
			parts = append(parts, t.Name)
		}
	}
	text := strings.Join(parts, " ")
	if width <= 0 || len([]rune(text)) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	return string([]rune(text)[:width-1]) + "…"
}
