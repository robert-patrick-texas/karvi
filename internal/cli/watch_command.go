package cli

import (
	"context"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/watchui"
)

func commandWatch(ctx context.Context, inv *Invocation, streams app.IO) int {
	g := inv.Global
	format := inv.String(optFormatWatch)
	if format == "" {
		format = "tui"
	}
	if format == "tui" {
		if f, ok := streams.Stdout.(*os.File); !ok || !osutil.IsTerminal(f) {
			return usageError(streams.Stderr, "watch_tui_requires_terminal", "noninteractive stdout requires --format table or --format json")
		}
	}
	op, err := osutil.CurrentOperator()
	if err != nil {
		return reportError(streams.Stderr, "operator_identity_unavailable", err)
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: g.configs, Sets: g.sets, FlagValues: g.common().ConfigFlags, HomeDir: op.Home})
	if err != nil {
		return reportError(streams.Stderr, "config_load_failed", app.ConfigLoadError(err))
	}
	dir := cfg.String("watch.directory")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		base, e := osutil.ResolveBaseDir(cfg.String("basedir"), op.Home, op.Username)
		if e == nil {
			fallback := filepath.Join(base, "state", "scoreboards")
			if _, e = os.Stat(fallback); e == nil {
				dir = fallback
			}
		}
	}
	refresh := inv.Duration(optRefresh)
	if refresh == 0 {
		refresh = cfg.Duration("watch.refresh")
	}
	stale := inv.Duration(optStaleAfter)
	if stale == 0 {
		stale = cfg.Duration("watch.stale-after")
	}
	theme := inv.String(optTheme)
	if theme == "" {
		theme = cfg.String("display.theme")
	}
	color := inv.String(optColor)
	if color == "" {
		color = cfg.String("display.color")
	}
	once := inv.Flag(optOnce) || format != "tui"
	// The twelve display.colors.* overrides by role.
	colors := map[string]string{}
	for _, role := range []string{"success", "warning", "error", "muted", "accent", "timestamp", "target", "address", "label", "value", "border", "dynamic-border"} {
		colors[role] = cfg.String("display.colors." + role)
	}
	if err := watchui.Run(ctx, watchui.Options{Directory: dir, Format: format, Theme: theme, Color: color, TimestampPattern: cfg.String("display.timestamp"), Timezone: cfg.String("timezone"), Colors: colors, Refresh: refresh, StaleAfter: stale, MaxFiles: cfg.Int("watch.max-files"), Once: once, Filter: inv.String(optWatchFilter), Sort: inv.String(optWatchSort)}, streams.Stdin, streams.Stdout); err != nil {
		return reportError(streams.Stderr, "watch_render_failed", err)
	}
	return 0
}
