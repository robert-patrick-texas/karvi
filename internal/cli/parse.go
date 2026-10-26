package cli

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/matching"
)

// TargetInput is one target input in command-line order. Kind is
// the option name: target, tf, tfr, site, device-group, platform (run only),
// or all. Positional is true for a device given without --target; Shortcut
// is true for the device of a first word that is no command (parseShortcut).
type TargetInput struct {
	Kind       string
	Value      string
	Positional bool
	Shortcut   bool
}

// Declaration is one declaration in command-line order: an --expect,
// --blind, --blind-return, --timeout, or --maxbytes, recorded beside the
// --cmd it follows. The parser keeps the five in one positional record, as
// it keeps the target inputs, because Invocation.values loses the order
// between options and a declaration is meaningful only for the command it
// follows. Command is the one-based index into Invocation.Sources of that
// --cmd, or 1 for the one freeform command; the parser refuses a declaration
// before the first source or after a --cf, so a handler never sees Command
// 0 or a file. Value is the option's raw value: the PATTERN=RESPONSE text,
// N, DURATION, BYTES, or "true"; declarationLists (work_commands.go)
// interprets it.
type Declaration struct {
	Option  string // "expect", "blind", "blind-return", "timeout", or "maxbytes"
	Value   string
	Command int
}

// CommandSource is one source of device commands in command-line order: a
// --cmd, or the one joined freeform command, its text in Value; or a --cf,
// File set and its path or - in Value. The device commands are the sources'
// in this order (commandPlan).
type CommandSource struct {
	File  bool
	Value string
}

// Invocation is the parsed command line. Handlers read option values through
// the accessors; parser diagnostics never reach a handler.
type Invocation struct {
	Global       globalOptions
	Path         string // "login", "daemon stop", ...; "" when only global options were given
	Help         bool   // help was requested at some position; Path names the level
	Targets      []TargetInput
	Sources      []CommandSource // --cmd and --cf in order, or the one joined freeform command
	Freeform     bool            // Sources is the freeform text's one command
	Declarations []Declaration
	Positional   []string // plain commands: positional arguments (login: none; devices are Targets)
	Record       *string  // login: nil when not recording; "" for --record without PATH
	Pending      []*option

	cmd    *command
	values map[*option][]string
}

// Flag reports whether an on/off option is on. An inline --name=false turns it
// off; the last spelling wins.
func (inv *Invocation) Flag(o *option) bool {
	v, ok := inv.values[o]
	return ok && len(v) > 0 && v[len(v)-1] != "false"
}

// Set reports whether the option appeared at all.
func (inv *Invocation) Set(o *option) bool { _, ok := inv.values[o]; return ok }

// String returns the last value of a value option, or "".
func (inv *Invocation) String(o *option) string {
	v := inv.values[o]
	if len(v) == 0 {
		return ""
	}
	return v[len(v)-1]
}

// Strings returns every value of a repeatable option in order.
func (inv *Invocation) Strings(o *option) []string { return append([]string(nil), inv.values[o]...) }

// Int returns the last value of an integer option, or 0. The parser has
// already rejected a value that is not an integer.
func (inv *Invocation) Int(o *option) int {
	n, _ := strconv.Atoi(inv.String(o))
	return n
}

// Duration returns the last value of a duration option, or 0.
func (inv *Invocation) Duration(o *option) time.Duration {
	d, _ := time.ParseDuration(inv.String(o))
	return d
}

func (inv *Invocation) helpText() string {
	if inv.cmd == nil {
		return topHelp
	}
	return inv.cmd.help()
}

func (inv *Invocation) record(o *option, value string) {
	if inv.values == nil {
		inv.values = map[*option][]string{}
	}
	inv.values[o] = append(inv.values[o], value)
	if o.pending {
		inv.Pending = append(inv.Pending, o)
	}
}

type wordEntry struct{ word, canon string }

// resolveWord applies the word rule: a full name or alias matches exactly; otherwise
// a prefix must identify exactly one canonical word. Candidates for an
// ambiguous prefix are rendered with prefix (for example "--") and name each
// alias that matched, as "--cmd (as --command)".
func resolveWord(in, prefix string, words []wordEntry) (string, []string) {
	for _, w := range words {
		if w.word == in {
			return w.canon, nil
		}
	}
	matched := map[string][]string{}
	var order []string
	if in != "" {
		for _, w := range words {
			if strings.HasPrefix(w.word, in) {
				if _, seen := matched[w.canon]; !seen {
					order = append(order, w.canon)
				}
				matched[w.canon] = append(matched[w.canon], w.word)
			}
		}
	}
	if len(order) == 1 {
		return order[0], nil
	}
	sort.Strings(order)
	var out []string
	for _, canon := range order {
		label := prefix + canon
		var viaAliases []string
		for _, w := range matched[canon] {
			if w != canon {
				viaAliases = append(viaAliases, prefix+w)
			}
		}
		if len(viaAliases) == len(matched[canon]) {
			label += " (as " + strings.Join(viaAliases, ", ") + ")"
		}
		out = append(out, label)
	}
	return "", out
}

func optionWords(opts []*option) []wordEntry {
	var out []wordEntry
	for _, o := range opts {
		out = append(out, wordEntry{o.name, o.name})
		for _, a := range o.aliases {
			out = append(out, wordEntry{a, o.name})
		}
	}
	return out
}

func commandWords(cmds []*command) []wordEntry {
	var out []wordEntry
	for _, c := range cmds {
		out = append(out, wordEntry{c.word, c.word})
		for _, a := range c.aliases {
			out = append(out, wordEntry{a, c.word})
		}
	}
	return out
}

func findCommand(cmds []*command, word string) *command {
	for _, c := range cmds {
		if c.word == word {
			return c
		}
	}
	return nil
}

// isOptionArg reports whether arg is spelled as an option: one or two leading
// dashes and at least one more character. "-" and "--" are not options.
func isOptionArg(arg string) bool {
	return len(arg) > 1 && arg[0] == '-' && arg != "--"
}

// splitOptionArg separates the dashes, the name, and an inline value.
func splitOptionArg(arg string) (dashes, name, inline string, hasInline bool) {
	body := strings.TrimPrefix(arg, "-")
	body = strings.TrimPrefix(body, "-")
	dashes = arg[:len(arg)-len(body)]
	name, inline, hasInline = strings.Cut(body, "=")
	return
}

type parser struct {
	args []string
	inv  *Invocation
}

// takeOption parses args[*i] as an option from opts, consuming a following
// value argument when needed. where names the position for messages.
func (p *parser) takeOption(opts []*option, where string, i *int) (*option, string, error) {
	arg := p.args[*i]
	dashes, name, inline, hasInline := splitOptionArg(arg)
	canon, candidates := resolveWord(name, "--", optionWords(opts))
	if canon == "" {
		if len(candidates) > 1 {
			return nil, "", errorcodes.Errorf("cli_option_ambiguous", "%s%s is ambiguous in %s: %s", dashes, name, where, strings.Join(candidates, ", "))
		}
		return nil, "", errorcodes.Errorf("cli_option_unknown", "unknown option %s%s in %s", dashes, name, where)
	}
	o := findOption(opts, canon)
	switch o.kind {
	case kindFlag:
		if hasInline {
			if inline != "true" && inline != "false" {
				return nil, "", errorcodes.Errorf("cli_option_value_invalid", "--%s takes true or false, not %q", canon, inline)
			}
			return o, inline, nil
		}
		return o, "true", nil
	case kindOptional:
		if hasInline {
			return o, inline, nil
		}
		// The value attaches with = alone: a following word of a
		// path's form was meant as the value and is refused, never taken
		// for a device or command text; any other word keeps its
		// position's meaning.
		if *i+1 < len(p.args) && pathForm(p.args[*i+1]) {
			return nil, "", detachedValue(o, p.args[*i+1])
		}
		return o, "", nil
	}
	value := inline
	if !hasInline {
		if *i+1 >= len(p.args) {
			return nil, "", errorcodes.Errorf("cli_option_value_missing", "--%s requires a value %s", canon, o.placeholder)
		}
		*i++
		value = p.args[*i]
	}
	if err := checkValueType(o, value); err != nil {
		return nil, "", err
	}
	return o, value, nil
}

func findOption(opts []*option, name string) *option {
	for _, o := range opts {
		if o.name == name {
			return o
		}
	}
	return nil
}

// pathForm reports whether a word has a path's form: it begins with /, ~,
// ./, or ../, or is . or .. whole. A device name or a command never does.
func pathForm(word string) bool {
	return word == "." || word == ".." || strings.HasPrefix(word, "/") || strings.HasPrefix(word, "~") || strings.HasPrefix(word, "./") || strings.HasPrefix(word, "../")
}

// detachedValue is the refusal of a value given to an option that takes
// its value with = alone (--of, --record, --cd, --fs) as a separate word: on the
// command line a word of a path's form, on a stream line any text after a
// space.
func detachedValue(o *option, value string) error {
	name := strings.Trim(o.placeholder, "[]=")
	return errorcodes.Errorf("cli_option_value_detached", "--%s takes its %s with =: --%s=%s", o.name, name, o.name, value)
}

func checkValueType(o *option, value string) error {
	switch o.typ {
	case typeInt:
		if _, err := strconv.Atoi(value); err != nil {
			return errorcodes.Errorf("cli_option_value_invalid", "--%s takes an integer, not %q", o.name, value)
		}
	case typeDuration:
		if _, err := time.ParseDuration(value); err != nil {
			return errorcodes.Errorf("cli_option_value_invalid", "--%s takes a duration such as 30s or 5m, not %q", o.name, value)
		}
	case typeEnum:
		for _, v := range o.enum {
			if v == value {
				return nil
			}
		}
		return errorcodes.Errorf("cli_option_value_invalid", "--%s takes %s, not %q", o.name, orList(o.enum), value)
	}
	return nil
}

func orList(values []string) string {
	switch len(values) {
	case 0:
		return ""
	case 1:
		return values[0]
	case 2:
		return values[0] + " or " + values[1]
	}
	return strings.Join(values[:len(values)-1], ", ") + ", or " + values[len(values)-1]
}

// apply records an option value and its role. It returns true when help was
// requested, which ends parsing.
func (p *parser) apply(o *option, value string) (bool, error) {
	inv := p.inv
	if o.standsFor != nil {
		// A shortcut is the option it stands for, given with its value:
		// --pi is --platform cisco_iosxe. --pi=false gives nothing.
		inv.record(o, value)
		if value == "false" {
			return false, nil
		}
		return p.apply(o.standsFor, o.standsValue)
	}
	switch o.role {
	case roleHelp:
		inv.Help = value != "false"
		inv.record(o, value)
		return inv.Help, nil
	case roleTarget:
		if err := p.addTarget(o, value); err != nil {
			return false, err
		}
	case roleTargetList:
		// --tl LIST: each name is a --target at the list's position. Commas
		// and whitespace separate, in any mix; an empty list is refused as an
		// empty --target is, and a name the selector grammar refuses is
		// named with --tl.
		names := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
		if len(names) == 0 {
			return false, errorcodes.Errorf("cli_device_empty", "--%s requires at least one device name", o.name)
		}
		for _, name := range names {
			if err := p.addTarget(o, name); err != nil {
				return false, err
			}
		}
	case roleTargetInput:
		switch o.name {
		case "site", "device-group", "select-platform":
			if err := checkSelector("--"+o.name, value, true); err != nil {
				return false, err
			}
		}
		// --all=false contributes nothing and is not a target input.
		if o.kind != kindFlag || value != "false" {
			inv.Targets = append(inv.Targets, TargetInput{Kind: o.inputKind(), Value: value})
		}
	case roleCmd:
		inv.Sources = append(inv.Sources, CommandSource{Value: value})
	case roleCf:
		inv.Sources = append(inv.Sources, CommandSource{File: true, Value: value})
	case roleDeclaration:
		if o == optExpect {
			if err := checkExpect(value); err != nil {
				return false, err
			}
		}
		if err := checkDeclaredBound(o, value); err != nil {
			return false, err
		}
		// --blind=false declares nothing, as --all=false selects nothing.
		if o.kind != kindFlag || value != "false" {
			inv.Declarations = append(inv.Declarations, Declaration{Option: o.name, Value: value, Command: len(inv.Sources)})
		}
	}
	if o == optExclude {
		if err := checkSelector("--exclude", value, false); err != nil {
			return false, err
		}
	}
	if o == optRecord {
		v := value
		inv.Record = &v
	}
	inv.record(o, value)
	return false, nil
}

// checkExpect applies the --expect grammar at parse time: the value splits
// at its first "=", PATTERN before it is a
// nonempty expression that compiles (RE2, the syntax the device session
// matches with; a literal "=" in a pattern is written "[=]", see
// splitExpect), and RESPONSE
// after it is any text, empty for a bare return. A pattern that compiles but
// does not match the device's line is not the parser's to judge: it fails at
// the device as command_timeout with the last line seen in its message.
func checkExpect(value string) error {
	pattern, _, ok := splitExpect(value)
	if !ok || pattern == "" {
		return errorcodes.Errorf("expect_malformed", "--expect takes PATTERN=RESPONSE with a nonempty PATTERN (a literal = in the pattern is written [=]), not %q", value)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return errorcodes.Errorf("expect_pattern_invalid", "--expect pattern %q does not compile: %v", pattern, err)
	}
	return nil
}

// declaredBoundKeys names the configuration key each command bound replaces:
// the declaration takes the key's form and range, read from its registry
// row, so the option and the key never disagree.
var declaredBoundKeys = map[*option]string{optTimeout: "execution.command-timeout", optMaxBytes: "output.max-command-bytes"}

// checkDeclaredBound refuses a --timeout or --maxbytes outside the range of
// the key it replaces for its command (cli_option_value_invalid, as a value
// of the wrong form is); the value's form was checked by checkValueType.
func checkDeclaredBound(o *option, value string) error {
	key, ok := declaredBoundKeys[o]
	if !ok {
		return nil
	}
	e, _ := configschema.Lookup(key)
	var v, lo, hi int64
	if o.typ == typeDuration {
		d, _ := time.ParseDuration(value)
		min, _ := time.ParseDuration(e.Min)
		max, _ := time.ParseDuration(e.Max)
		v, lo, hi = int64(d), int64(min), int64(max)
	} else {
		v, _ = strconv.ParseInt(value, 10, 64)
		lo, _ = strconv.ParseInt(e.Min, 10, 64)
		hi, _ = strconv.ParseInt(e.Max, 10, 64)
	}
	if v < lo || v > hi {
		return errorcodes.Errorf("cli_option_value_invalid", "--%s takes %s..%s, the range of %s, not %s", o.name, e.Min, e.Max, key, value)
	}
	return nil
}

// splitExpect divides an --expect value into PATTERN and RESPONSE at its
// first "=" outside a bracket class and not escaped, so "[=]" (and "\=")
// keeps a literal "=" in the pattern and the response keeps every later
// "=". ok is false without a dividing "=".
func splitExpect(value string) (pattern, response string, ok bool) {
	inClass := false
	for i := 0; i < len(value); i++ {
		switch value[i] {
		case '\\':
			i++ // the escaped character is never the divider
		case '[':
			if !inClass {
				inClass = true
				// A "]" first in the class ("[]a]", "[^]a]") is a member.
				if i+1 < len(value) && value[i+1] == '^' {
					i++
				}
				if i+1 < len(value) && value[i+1] == ']' {
					i++
				}
			}
		case ']':
			inClass = false
		case '=':
			if !inClass {
				return value[:i], value[i+1:], true
			}
		}
	}
	return value, "", false
}

// placeDeclarations applies the placement rule once the command sources are
// known: each declaration attaches to the source written right before it,
// which must be a --cmd, or in freeform to the one command. Before the first
// source it has no command and is refused rather than attached to the first,
// so a reader never guesses; after a --cf it is refused, though a --cmd
// stands further back, since it would read as the file's and a file line
// takes \r at its end (EXAMPLES chapter 79). A file's lines carry none. The
// message says where to write the declaration.
func (p *parser) placeDeclarations() error {
	inv := p.inv
	for i := range inv.Declarations {
		d := &inv.Declarations[i]
		switch {
		case inv.Freeform:
			d.Command = 1
		case d.Command == 0:
			return errorcodes.Errorf("declaration_before_command", "--%s is given before the first --cmd; write it after the command it belongs to", d.Option)
		case inv.Sources[d.Command-1].File:
			return errorcodes.Errorf("declaration_with_commands_file", "--%s follows --cf; write it after its --cmd (a file line takes \\r at its end)", d.Option)
		}
	}
	return nil
}

// addTarget records value as a --target input at this position, for
// --target itself and for each name of a --tl list: an empty name and a
// selector the shared grammar does not compile are refused at parse time,
// the message naming the option that carried the name.
func (p *parser) addTarget(o *option, value string) error {
	if strings.TrimSpace(value) == "" {
		return errorcodes.Errorf("cli_device_empty", "--%s requires a nonempty device name", o.name)
	}
	if err := checkSelector("--"+o.name, strings.TrimSpace(value), true); err != nil {
		return err
	}
	p.inv.Targets = append(p.inv.Targets, TargetInput{Kind: optTarget.name, Value: value})
	return nil
}

func (p *parser) positionalDevice(value string) error {
	if strings.TrimSpace(value) == "" {
		return errorcodes.Errorf("cli_device_empty", "the device name must not be empty")
	}
	if err := checkSelector("DEVICE", strings.TrimSpace(value), true); err != nil {
		return err
	}
	p.inv.Targets = append(p.inv.Targets, TargetInput{Kind: "target", Value: value, Positional: true})
	return nil
}

// checkSelector refuses a selector the shared grammar does not compile, at
// parse time. A leading "!" is a "not"
// filter where negate allows it; --exclude already means "not" and refuses
// it.
func checkSelector(option, value string, negate bool) error {
	s, err := matching.ParseSelector(value)
	if err != nil {
		var pe *matching.PatternError
		if errors.As(err, &pe) {
			return errorcodes.Errorf("target_selector_pattern_invalid", "%s value %q is not a valid selector: %s", option, value, pe.Reason)
		}
		return errorcodes.Errorf("target_selector_pattern_invalid", "%s value %q is not a valid selector: %v", option, value, err)
	}
	if s.Negated && !negate {
		return errorcodes.Errorf("target_selector_pattern_invalid", "%s value %q is not a valid selector: --exclude already removes the devices it matches, so a leading \"!\" is refused; write \\! for a literal \"!\"", option, value)
	}
	return nil
}

// Parse parses one karvi command line. A returned error carries the parser
// rule's registered code; every such error exits with
// ExitUsageError before any connection.
func Parse(args []string) (*Invocation, error) {
	p := &parser{args: args, inv: &Invocation{}}
	inv := p.inv
	// The global options are gathered once the global position ends, also
	// when a global --help ends parsing: the help's layout reads the
	// configuration the --config and --set before it name (help_layout.go).
	setGlobal := func() {
		inv.Global = globalOptions{
			configs: inv.Strings(optConfig), sets: inv.Strings(optSet),
			quiet: inv.Flag(optQuiet), debug: inv.Flag(optDebug), debugShow: inv.Flag(optDebugShow),
			version: inv.Flag(optVersionFlag), ipv4: inv.Flag(optIPv4), ipv6: inv.Flag(optIPv6),
			timezone: inv.String(optTimezone), ansi: inv.String(optAnsi), noColor: inv.Flag(optNoColor),
		}
	}
	i := 0
	for ; i < len(args); i++ {
		if args[i] == "--" {
			i++
			break
		}
		if !isOptionArg(args[i]) {
			break
		}
		// A refusal here is said under the global options taken so far
		// (--nocolor), as one after them is.
		o, value, err := p.takeOption(globalOptionTable, "global options", &i)
		if err != nil {
			setGlobal()
			return inv, err
		}
		if done, err := p.apply(o, value); err != nil {
			setGlobal()
			return inv, err
		} else if done {
			setGlobal()
			return inv, nil
		}
	}
	setGlobal()
	if inv.Global.version || i >= len(args) {
		return inv, nil
	}
	word, candidates := resolveWord(args[i], "", commandWords(commandTable))
	if word == "" {
		if len(candidates) > 1 {
			return inv, errorcodes.Errorf("cli_command_ambiguous", "%q is ambiguous: %s", args[i], strings.Join(candidates, ", "))
		}
		if !plainName(args[i]) {
			return inv, errorcodes.Errorf("cli_command_unknown", "unknown command %q", args[i])
		}
		return inv, p.parseShortcut(args[i:])
	}
	cmd := findCommand(commandTable, word)
	inv.cmd, inv.Path = cmd, cmd.path
	i++
	if cmd.subs != nil {
		if i < len(args) && isOptionArg(args[i]) {
			o, value, err := p.takeOption(cmd.options, cmd.path, &i)
			if err != nil {
				return inv, err
			}
			if done, err := p.apply(o, value); err != nil {
				return inv, err
			} else if done {
				return inv, nil
			}
			i++
		}
		if i >= len(args) {
			return inv, errorcodes.Errorf("cli_subcommand_missing", "%s requires one of: %s", cmd.path, strings.Join(visibleWords(cmd.subs), ", "))
		}
		sub, candidates := resolveWord(args[i], "", commandWords(cmd.subs))
		if sub == "" {
			if len(candidates) > 1 {
				return inv, errorcodes.Errorf("cli_subcommand_ambiguous", "%s %q is ambiguous: %s", cmd.path, args[i], strings.Join(candidates, ", "))
			}
			return inv, errorcodes.Errorf("cli_subcommand_unknown", "unknown %s operation %q; expected one of: %s", cmd.path, args[i], strings.Join(visibleWords(cmd.subs), ", "))
		}
		cmd = findCommand(cmd.subs, sub)
		inv.cmd, inv.Path = cmd, cmd.path
		i++
	}
	if cmd.hidden && strings.HasSuffix(cmd.path, "help") {
		inv.Help = true
		return inv, nil
	}
	rest := args[i:]
	switch cmd.shape {
	case shapeFreeformDevice:
		return inv, p.parseFreeform(cmd, rest, true)
	case shapeFreeform:
		return inv, p.parseFreeform(cmd, rest, false)
	default:
		return inv, p.parsePlain(cmd, rest)
	}
}

// plainName reports whether a first word that is no command may name a
// device for the shortcut (EXAMPLES chapter 71, rule 2): a word login takes as
// one literal name, holding none of the selector grammar's * ? [ \ ^ $ and
// no leading "!". A bracketed IPv6 address is a pattern.
func plainName(word string) bool {
	return strings.TrimSpace(word) != "" && !strings.ContainsAny(word, `*?[\^$`) && !strings.HasPrefix(word, "!")
}

// parseShortcut parses a line whose first word is no command but a plain
// name (EXAMPLES chapter 71): the word is a positional device marked as the
// shortcut's, and the line is a login at it, or a command at it when
// shortcutCommand says so. The mode chosen parses the rest of the line, so an
// abbreviation means what it means in that mode.
func (p *parser) parseShortcut(args []string) error {
	cmd := lookupCommand("login")
	if shortcutCommand(args[1:]) {
		cmd = lookupCommand("command")
	}
	p.inv.cmd, p.inv.Path = cmd, cmd.path
	if err := p.positionalDevice(args[0]); err != nil {
		return err
	}
	p.inv.Targets[0].Shortcut = true
	if cmd.shape == shapeFreeformDevice {
		return p.parseFreeform(cmd, args[1:], true)
	}
	return p.parsePlain(cmd, args[1:])
}

// shortcutCommand reports whether the words after a shortcut's device make
// the line a command (rule 3). They are read as login's options; a command
// begins at the first word of command text, at -- with words after it, or at
// an option login does not know and command knows or finds ambiguous.
// Anything else is a login, a refusal of login's among it.
func shortcutCommand(args []string) bool {
	login, command := lookupCommand("login"), lookupCommand("command")
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--":
			return i+1 < len(args)
		case !isOptionArg(args[i]):
			return true
		}
		_, name, _, hasInline := splitOptionArg(args[i])
		canon, candidates := resolveWord(name, "--", optionWords(login.options))
		if canon == "" {
			if len(candidates) > 1 {
				return false
			}
			canon, candidates = resolveWord(name, "--", optionWords(command.options))
			return canon != "" || len(candidates) > 1
		}
		o := findOption(login.options, canon)
		switch {
		case o.kind == kindValue && !hasInline:
			i++ // the option's value
		case o.kind == kindOptional && !hasInline && i+1 < len(args) && pathForm(args[i+1]):
			return false // login refuses the detached value
		}
	}
	return false
}

// nearCommand is the command word of four letters or more one slip from a
// shortcut's word, ignoring case, or "" (rule 4): the mode refuses such a
// word unless it is an inventory name.
func nearCommand(word string) string {
	word = strings.ToLower(word)
	for _, c := range commandTable {
		if len(c.word) >= 4 && oneSlip(word, c.word) {
			return c.word
		}
	}
	return ""
}

// oneSlip reports whether a and b differ by at most one slip: a letter
// added, dropped, or changed, or two neighbours swapped.
func oneSlip(a, b string) bool {
	if len(a) < len(b) {
		a, b = b, a
	}
	i := 0
	for i < len(b) && a[i] == b[i] {
		i++
	}
	switch {
	case i == len(a):
		return true
	case len(a) == len(b)+1:
		return a[i+1:] == b[i:]
	case len(a) != len(b):
		return false
	}
	return a[i+1:] == b[i+1:] || (i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && a[i+2:] == b[i+2:])
}

func visibleWords(cmds []*command) []string {
	var out []string
	for _, c := range cmds {
		if !c.hidden {
			out = append(out, c.word)
		}
	}
	return out
}

// parseFreeform parses a freeform command line. Options are recognized
// until freeform text begins. In command mode the first positional word is
// the device when no target input precedes it; otherwise it begins freeform.
func (p *parser) parseFreeform(cmd *command, args []string, deviceFirst bool) error {
	inv := p.inv
	p.args = args
	var free []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest := args[i+1:]
			if deviceFirst && len(inv.Targets) == 0 && len(rest) > 0 {
				if err := p.positionalDevice(rest[0]); err != nil {
					return err
				}
				rest = rest[1:]
			}
			free = rest
			break
		}
		if isOptionArg(arg) {
			o, value, err := p.takeOption(cmd.options, cmd.path, &i)
			if err != nil {
				return err
			}
			if done, err := p.apply(o, value); err != nil {
				return err
			} else if done {
				return nil
			}
			continue
		}
		if deviceFirst && len(inv.Targets) == 0 {
			if err := p.positionalDevice(arg); err != nil {
				return err
			}
			continue
		}
		free = args[i:]
		break
	}
	explicit := len(inv.Sources) > 0
	switch {
	case explicit && len(free) > 0:
		return errorcodes.Errorf("cli_command_text_mixed", "freeform text %q cannot be combined with --cmd or --cf", strings.Join(free, " "))
	case deviceFirst && len(inv.Targets) == 0:
		return errorcodes.Errorf("cli_device_missing", "%s requires a device: DEVICE, --target, or another target input", cmd.path)
	case !explicit && len(free) == 0 && !cmd.textOptional:
		return errorcodes.Errorf("cli_command_text_missing", "%s requires device command text: freeform words, --cmd TEXT, or --cf PATH", cmd.path)
	case !explicit && len(free) == 0:
		// crun without command text sends each device its platform's list.
	case !explicit:
		inv.Sources = []CommandSource{{Value: strings.Join(free, " ")}}
		inv.Freeform = true
	}
	return p.placeDeclarations()
}

// parsePlain parses a plain command line: options before or after positional
// arguments, and -- makes every later argument positional.
func (p *parser) parsePlain(cmd *command, args []string) error {
	inv := p.inv
	p.args = args
	var positional []string
	// login: a positional device counts as a --target at its position
	// and is recorded where it appears.
	take := func(arg string) error {
		if cmd.path == "login" {
			return p.positionalDevice(arg)
		}
		positional = append(positional, arg)
		return nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			for _, rest := range args[i+1:] {
				if err := take(rest); err != nil {
					return err
				}
			}
			break
		}
		if isOptionArg(arg) {
			o, value, err := p.takeOption(cmd.options, cmd.path, &i)
			if err != nil {
				return err
			}
			if done, err := p.apply(o, value); err != nil {
				return err
			} else if done {
				return nil
			}
			continue
		}
		if err := take(arg); err != nil {
			return err
		}
	}
	if cmd.path == "login" {
		if len(inv.Targets) == 0 {
			return errorcodes.Errorf("cli_device_missing", "login requires a device: DEVICE, --target, or another target input")
		}
		return nil
	}
	if cmd.maxPos >= 0 && len(positional) > cmd.maxPos {
		switch cmd.maxPos {
		case 0:
			return errorcodes.Errorf("cli_positional_unexpected", "%s accepts no positional arguments, got %q", cmd.path, positional[0])
		default:
			return errorcodes.Errorf("cli_positional_unexpected", "%s accepts at most %d positional argument(s), got %q", cmd.path, cmd.maxPos, positional)
		}
	}
	if len(positional) < cmd.minPos {
		return errorcodes.Errorf("cli_positional_missing", "%s requires %d positional argument(s)", cmd.path, cmd.minPos)
	}
	inv.Positional = positional
	return nil
}
