package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/app"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/transportselect"
)

func configGenerate(inv *Invocation, streams app.IO) int {
	if inv.Flag(optMinimal) && inv.Flag(optFull) {
		return usageError(streams.Stderr, "config_generate_mode_conflict", "--minimal and --full are mutually exclusive")
	}
	content := configload.RenderReference()
	if inv.Flag(optMinimal) {
		content = minimalConfig
	}
	if len(inv.Positional) == 0 || inv.Positional[0] == "-" {
		fmt.Fprint(streams.Stdout, content)
		return 0
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	if !inv.Flag(optForce) {
		flags |= os.O_EXCL
	}
	f, err := os.OpenFile(inv.Positional[0], flags, 0600)
	if err != nil {
		return reportError(streams.Stderr, "config_generate_destination_unavailable", err)
	}
	if _, err = io.WriteString(f, content); err == nil {
		err = f.Sync()
	}
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err != nil {
		return reportError(streams.Stderr, "config_generate_write_failed", err)
	}
	return 0
}

func configValidate(inv *Invocation, streams app.IO) int {
	g := inv.Global
	format := inv.String(optFormatTJ)
	op, err := osutil.CurrentOperator()
	if err != nil {
		return reportError(streams.Stderr, "operator_identity_unavailable", err)
	}
	roots := append([]string(nil), g.configs...)
	skip := false
	if len(inv.Positional) == 1 {
		roots = append(roots, inv.Positional[0])
		skip = true
	}
	snap, err := configload.Load(configload.Options{ExplicitRoots: roots, Sets: g.sets, FlagValues: g.common().ConfigFlags, HomeDir: op.Home, SkipAuto: skip})
	if err == nil {
		err = transportselect.ValidateConfigured(snap)
	}
	if err != nil {
		err = app.ConfigLoadError(err)
		if format == "json" {
			_ = json.NewEncoder(streams.Stdout).Encode(map[string]any{"valid": false, "code": errorcodes.Of(err), "error": errorcodes.Message(err)})
			return errorcodes.ExitAt(err, "config_load_failed")
		}
		return reportError(streams.Stderr, "config_load_failed", err)
	}
	if format == "json" {
		_ = json.NewEncoder(streams.Stdout).Encode(map[string]any{"valid": true, "digest": snap.Digest, "sources": snap.Sources, "warnings": snap.Warnings})
	} else {
		fmt.Fprintf(streams.Stdout, "valid: true\ndigest: %s\nsources: %s\nwarnings: %d\n", snap.Digest, strings.Join(snap.Sources, ", "), len(snap.Warnings))
	}
	return 0
}

// loadConfigSnapshot loads the configuration the invocation names, as config
// show and config colors read it: the roots, --set values, and flag values
// of the global options, validated as a loaded configuration is. A failure
// is reported and its exit returned with ok false.
func loadConfigSnapshot(inv *Invocation, stderr io.Writer) (configload.Snapshot, int, bool) {
	g := inv.Global
	op, err := osutil.CurrentOperator()
	if err != nil {
		return configload.Snapshot{}, reportError(stderr, "operator_identity_unavailable", err), false
	}
	snap, err := configload.Load(configload.Options{ExplicitRoots: g.configs, Sets: g.sets, FlagValues: g.common().ConfigFlags, HomeDir: op.Home})
	if err == nil {
		err = transportselect.ValidateConfigured(snap)
	}
	if err != nil {
		return configload.Snapshot{}, reportError(stderr, "config_load_failed", app.ConfigLoadError(err)), false
	}
	return snap, 0, true
}

// colorRoles are the display.colors roles, in the order the colour test
// lists them.
var colorRoles = []string{"accent", "address", "border", "dynamic-border", "error", "label", "muted", "success", "target", "timestamp", "value", "warning"}

// configColors prints the colour test: every role in its colour, under the
// configured theme first and then the other, each line the key rendered as
// a display line renders that role (bold where the display is), the colour
// the role resolves to, what set it (the configured name, or default), and
// the escape a terminal receives. Colour follows the display's rule
// (display.color, display.theme, a terminal), so a pipe prints the words
// alone and the escape column still says what a terminal would get.
func configColors(inv *Invocation, streams app.IO) int {
	snap, code, ok := loadConfigSnapshot(inv, streams.Stderr)
	if !ok {
		return code
	}
	configuredTheme := snap.String("display.theme")
	first := display.EffectiveTheme(configuredTheme)
	themes := []string{"dark", "light"}
	if first == "light" {
		themes = []string{"light", "dark"}
	}
	enabled := display.ColorEnabled(snap.String("display.color"), configuredTheme, jobexec.DisplayTerminal(streams.Stdout))
	width := 0
	for _, role := range colorRoles {
		if n := len("display.colors." + role); n > width {
			width = n
		}
	}
	for i, theme := range themes {
		label := "theme " + theme
		if i == 0 {
			label += " (display.theme = " + strconv.Quote(configuredTheme) + ")"
		}
		fmt.Fprintln(streams.Stdout, label)
		for _, role := range colorRoles {
			key := "display.colors." + role
			configured := snap.String(key)
			color := display.RoleColor(theme, role, configured)
			setBy := "default"
			if c := strings.ToLower(strings.TrimSpace(configured)); c != "" && c != "default" {
				setBy = "configured"
			}
			escape := strings.ReplaceAll(display.ANSIPrefix(color, display.RoleBold(role)), "\x1b", "\\x1b")
			if escape == "" {
				escape = "(none)"
			}
			shown := display.ANSIStyle(key, color, enabled, display.RoleBold(role))
			fmt.Fprintf(streams.Stdout, "  %s%s  %-8s %-10s %s\n", shown, strings.Repeat(" ", width-len(key)), color, setBy, escape)
		}
	}
	return 0
}

func configShow(inv *Invocation, streams app.IO) int {
	format := inv.String(optFormatToml)
	if format == "" {
		format = "toml"
	}
	explain := inv.Flag(optExplain) || len(inv.Positional) == 1
	snap, code, ok := loadConfigSnapshot(inv, streams.Stderr)
	if !ok {
		return code
	}
	if inv.Flag(optShowSources) {
		for _, s := range snap.Sources {
			fmt.Fprintf(streams.Stderr, "source: %s\n", s)
		}
	}
	if explain {
		key := ""
		if len(inv.Positional) == 1 {
			key = inv.Positional[0]
		}
		fmt.Fprint(streams.Stdout, snap.Explain(key))
		return 0
	}
	switch format {
	case "json":
		values := map[string]any{}
		keys := make([]string, 0, len(snap.Values))
		for k := range snap.Values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := snap.Values[k].Data
			if entry, ok := configschema.Lookup(k); ok && entry.Sensitive {
				v = "<redacted>"
			}
			values[k] = v
		}
		enc := json.NewEncoder(streams.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(map[string]any{"config_schema_version": configschema.ConfigSchemaVersion, "digest": snap.Digest, "values": values, "sources": snap.Sources})
	default:
		fmt.Fprint(streams.Stdout, snap.RenderFlatTOML())
	}
	return 0
}

const minimalConfig = `# karvi starter configuration (schema 6)
[config]
schema-version = 6

# The default run selector resolves to the native slot (scrapligo-v1). This
# starter keeps run on system OpenSSH; remove this table to use the native
# transport.
[ssh.run]
transport = "system"

[ssh.transports]
system = "ssh"
native = "scrapligo-v1"

[audit]
# Keep true in managed Ubuntu deployments. Set false only for development hosts
# without a journald socket and configure audit.file instead.
journald-required = true

# Credential zero-config fallback reads NETUSER, NETPASS, and NETENABLE. Dynamic inventory examples are in configs/example.toml.
`
