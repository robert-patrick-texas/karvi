package cli

import (
	"context"
	"io"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/inventoryload"
	"github.com/robert-patrick-texas/karvi/internal/osutil"
	"github.com/robert-patrick-texas/karvi/internal/scoreboard"
	"github.com/robert-patrick-texas/karvi/platform"
)

// Tab completion. The shell knows nothing of
// karvi's grammar: a small function, installed by `sudo karvi setup tab`,
// hands the line typed so far to the hidden word `karvi __complete`, and the
// executable answers with the candidates, one per line, from the parser
// table the real parser reads (parse_table.go). The word stands outside the
// grammar: Main takes it before parsing, it is in no help and abbreviates
// to nothing, and it creates no state, contacts no daemon, resolves no
// credential, and opens no terminal. Whatever fails, it prints nothing and
// exits 0, so a broken configuration never turns Tab into an error.
//
// What is offered follows the position the walker reaches, as the parser
// would reach it: the command words and the global options before the
// command; a command's subcommand words; the options valid for the command;
// an enum option's values; platform names for --platform; the registry's
// keys for --set, each followed by =; device names from the inventory for a
// target; job IDs from the scoreboard directory for job follow and job
// cancel; and, for a path option, the directive that lets the shell complete
// file names. Once device text has begun in command, run, or crun, nothing
// is offered: that is the device's vocabulary.

// completeMain answers `karvi __complete CURSOR WORD...` (the line's shape,
// the filter, and the printer are the completion package's, shared with
// karvi-prune's hidden word, 17.1).
func completeMain(args []string, stdout io.Writer) int {
	defer func() { _ = recover() }()
	line, current, ok := completion.Line(args)
	if !ok {
		return 0
	}
	cands, files := complete(line, current)
	completion.Print(stdout, cands, files)
	return 0
}

// completer carries what the walk learns before the cursor: the global
// --config and --set values, which decide the configuration the names come
// from, loaded once and only when a candidate needs it.
type completer struct {
	configs, sets []string
	cfg           *configload.Snapshot
	home          string
	loaded        bool
}

// complete walks line, the words after the program name and before the
// cursor, and returns the candidates for current, the word under the cursor,
// with files true when the shell should offer file names instead or as well.
func complete(line []string, current string) (cands []string, files bool) {
	c := &completer{}
	i := 0
	// The global position: options until the command word.
	for i < len(line) && isOptionArg(line[i]) {
		o, wantsValue := c.take(globalOptionTable, line, &i)
		if wantsValue {
			return c.values(o, nil, current)
		}
	}
	if i == len(line) {
		if o, prefix, ok := inlineValue(globalOptionTable, current); ok {
			return c.inline(o, nil, prefix, current)
		}
		return completion.Filter(append(words(commandTable), optionNames(globalOptionTable)...), current), false
	}
	cmd := resolveCommand(commandTable, line[i])
	if cmd == nil {
		return nil, false
	}
	i++
	if cmd.subs != nil {
		// The parent's own options (--help) may stand before the subcommand.
		for i < len(line) && isOptionArg(line[i]) {
			o, wantsValue := c.take(cmd.options, line, &i)
			if wantsValue {
				return c.values(o, cmd, current)
			}
		}
		if i == len(line) {
			return completion.Filter(append(words(cmd.subs), optionNames(cmd.options)...), current), false
		}
		sub := resolveCommand(cmd.subs, line[i])
		if sub == nil {
			return nil, false
		}
		cmd = sub
		i++
	}
	switch cmd.shape {
	case shapeFreeformDevice, shapeFreeform:
		return c.freeform(cmd, line[i:], current)
	default:
		return c.plain(cmd, line[i:], current)
	}
}

// freeform is command, run, and crun: options and, in command, the device,
// until device text begins (parseFreeform's rule); then nothing.
func (c *completer) freeform(cmd *command, line []string, current string) ([]string, bool) {
	deviceFirst := cmd.shape == shapeFreeformDevice
	haveTarget := false
	for i := 0; i < len(line); {
		arg := line[i]
		if arg == "--" {
			// After -- the first word is the device when none is named yet;
			// everything else is device text.
			if deviceFirst && !haveTarget && i+1 == len(line) {
				return c.devices(current), false
			}
			return nil, false
		}
		if isOptionArg(arg) {
			o, wantsValue := c.take(cmd.options, line, &i)
			if o != nil && (o.role == roleTarget || o.role == roleTargetInput) {
				haveTarget = true
			}
			if wantsValue {
				return c.values(o, cmd, current)
			}
			continue
		}
		if deviceFirst && !haveTarget {
			haveTarget = true
			i++
			continue
		}
		return nil, false
	}
	if o, prefix, ok := inlineValue(cmd.options, current); ok {
		return c.inline(o, cmd, prefix, current)
	}
	cands := optionNames(cmd.options)
	if deviceFirst && !haveTarget {
		cands = append(cands, c.devices("")...)
	}
	return completion.Filter(cands, current), false
}

// plain is every other command: options anywhere, positionals counted, and
// -- making the rest positional (parsePlain's rule). The positional a
// command takes decides its candidates: login's device, job follow's and
// job cancel's ID, config show's key, and a file for config generate and
// config validate.
func (c *completer) plain(cmd *command, line []string, current string) ([]string, bool) {
	positional := 0
	optionsEnded := false
	for i := 0; i < len(line); {
		arg := line[i]
		if arg == "--" && !optionsEnded {
			optionsEnded = true
			i++
			continue
		}
		if isOptionArg(arg) && !optionsEnded {
			o, wantsValue := c.take(cmd.options, line, &i)
			if wantsValue {
				return c.values(o, cmd, current)
			}
			continue
		}
		positional++
		i++
	}
	var cands []string
	if !optionsEnded {
		if o, prefix, ok := inlineValue(cmd.options, current); ok {
			return c.inline(o, cmd, prefix, current)
		}
		cands = optionNames(cmd.options)
	}
	files := false
	if cmd.maxPos < 0 || positional < cmd.maxPos {
		switch cmd.path {
		case "login":
			cands = append(cands, c.devices("")...)
		case "job follow", "job cancel":
			cands = append(cands, c.jobs()...)
		case "config show":
			cands = append(cands, configschema.SortedPaths()...)
		case "config generate", "config validate":
			files = true
		}
	}
	return completion.Filter(cands, current), files
}

// take consumes the option at line[*i] as takeOption would, without the
// checks: an unknown or ambiguous word is passed over. wantsValue is true
// when the option takes a value the line does not hold yet, which is then
// the word under the cursor.
func (c *completer) take(opts []*option, line []string, i *int) (o *option, wantsValue bool) {
	_, name, inline, hasInline := splitOptionArg(line[*i])
	canon, _ := resolveWord(name, "--", optionWords(opts))
	*i++
	for _, x := range opts {
		if x.name == canon {
			o = x
		}
	}
	if o == nil || o.kind != kindValue {
		return o, false
	}
	if hasInline {
		c.record(o, inline)
		return o, false
	}
	if *i == len(line) {
		return o, true
	}
	c.record(o, line[*i])
	*i++
	return o, false
}

// record keeps the global values a later candidate depends on.
func (c *completer) record(o *option, value string) {
	switch o {
	case optConfig:
		c.configs = append(c.configs, value)
	case optSet:
		c.sets = append(c.sets, value)
	}
}

// inlineValue recognises a current word of the form --opt=prefix (the
// shell keeps = inside the word): the option and the text after =.
func inlineValue(opts []*option, current string) (o *option, prefix string, ok bool) {
	if !isOptionArg(current) || !strings.Contains(current, "=") {
		return nil, "", false
	}
	dashes, name, inline, _ := splitOptionArg(current)
	canon, _ := resolveWord(name, "--", optionWords(opts))
	for _, x := range opts {
		if x.name == canon && x.kind != kindFlag {
			return x, dashes + name + "=", true
		}
	}
	_ = inline
	return nil, "", false
}

// inline completes the value after --opt= and returns each candidate with
// the option spelled before it, since the shell replaces the whole word.
func (c *completer) inline(o *option, cmd *command, prefix, current string) ([]string, bool) {
	cands, files := c.values(o, cmd, strings.TrimPrefix(current, prefix))
	for i := range cands {
		cands[i] = prefix + cands[i]
	}
	return cands, files
}

// values are the candidates for the value of o, filtered to current.
func (c *completer) values(o *option, cmd *command, current string) ([]string, bool) {
	switch {
	case o == nil:
		return nil, false
	case o.typ == typeEnum:
		return completion.Filter(o.enum, current), false
	case o == optSet:
		keys := configschema.SortedPaths()
		cands := make([]string, len(keys))
		for i, k := range keys {
			cands[i] = k + "="
		}
		return completion.Filter(cands, current), false
	case o == optPlatform:
		return completion.Filter(c.platforms(), current), false
	case o.role == roleTarget:
		return completion.Filter(c.devices(""), current), false
	case pathOption(o):
		return nil, true
	}
	return nil, false
}

// pathOption is an option whose value names a file or a directory, by its
// placeholder: the shell completes those.
func pathOption(o *option) bool {
	p := o.placeholder
	return strings.Contains(p, "PATH") || strings.Contains(p, "FILE") || strings.Contains(p, "DIR")
}

// config loads the configuration the line's --config and --set name, as
// help does (helpStyleFor), once; nil when it cannot be loaded.
func (c *completer) config() *configload.Snapshot {
	if c.loaded {
		return c.cfg
	}
	c.loaded = true
	op, err := osutil.CurrentOperator()
	if err != nil {
		return nil
	}
	cfg, err := configload.Load(configload.Options{ExplicitRoots: c.configs, Sets: c.sets, HomeDir: op.Home})
	if err != nil {
		return nil
	}
	c.cfg, c.home = &cfg, op.Home
	return c.cfg
}

// devices are the inventory's device names, as its sources list them.
func (c *completer) devices(current string) []string {
	cfg := c.config()
	if cfg == nil {
		return nil
	}
	loader := inventoryload.Loader{Config: *cfg, Home: c.home, Warn: func(string) {}}
	devices, _, err := loader.Load(context.Background())
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, d.Name)
	}
	return completion.Filter(names, current)
}

// jobs are the job IDs the scoreboard directory holds, running and finished
// alike, as job follow and job cancel name them; the shell sorts what it
// shows, so they carry no order of their own.
func (c *completer) jobs() []string {
	cfg := c.config()
	if cfg == nil {
		return nil
	}
	rows, err := scoreboard.Read(cfg.String("watch.directory"), cfg.Int("watch.max-files"), 0)
	if err != nil {
		return nil
	}
	var ids []string
	for _, r := range rows {
		id := r.Snapshot.JobID
		if id == "" {
			id = r.Snapshot.ActivityID
		}
		if r.Error == "" && id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// platforms are the known platform names: the built-ins, then the site's
// tables when the configuration loads.
func (c *completer) platforms() []string {
	var tables map[string]map[string]any
	if cfg := c.config(); cfg != nil {
		tables = cfg.NamedTables("platform")
	}
	return platform.KnownNames(tables)
}

// resolveCommand is the word's command by full name, alias, or unique
// prefix, as the parser resolves it; nil otherwise.
func resolveCommand(cmds []*command, word string) *command {
	canon, _ := resolveWord(word, "", commandWords(cmds))
	if canon == "" {
		return nil
	}
	return findCommand(cmds, canon)
}

// words are the visible command words of cmds.
func words(cmds []*command) []string {
	var out []string
	for _, c := range cmds {
		if !c.hidden {
			out = append(out, c.word)
		}
	}
	return out
}

// optionNames are the options' canonical names with their dashes, the ones
// not yet built left out; aliases are not offered, since abbreviation
// reaches them and the list stays short.
func optionNames(opts []*option) []string {
	var out []string
	for _, o := range opts {
		if !o.pending {
			out = append(out, "--"+o.name)
		}
	}
	return out
}
