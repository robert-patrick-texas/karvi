package prune

import (
	"flag"
	"io"
	"strings"

	"github.com/robert-patrick-texas/karvi/internal/completion"
)

// Tab completion for the helper. The bash function
// karvi installs serves karvi-prune as well and hands the line typed so far
// to the helper's own hidden word, `karvi-prune __complete CURSOR WORD...`,
// which answers from the flag set the helper parses (flags.go), so the
// candidates are the flags the helper takes and nothing else. As karvi's
// word, it is taken before parsing, is in no help,
// creates no state (the private root is never resolved, since resolving
// it would create it), reads nothing, and prints nothing and exits 0
// whatever fails.
//
// What is offered follows the flag package's own grammar: the flags, in
// their double-dash spelling, when the word under the cursor is empty or
// begins with a dash; a flag's words after a flag that takes a value
// (`--format` text or jsonl, `--sharedroot` auto or none, `--basedir` auto)
// and, for a flag that names a path, the directive on which the shell
// offers file names once no word matches; a value typed inline
// (`--format=js`) completed with the flag spelled before it; nothing after
// `--` or after a positional word, which the helper refuses.

// Complete answers the hidden word; args are its arguments, CURSOR WORD....
func Complete(args []string, stdout io.Writer) int {
	defer func() { _ = recover() }()
	line, current, ok := completion.Line(args)
	if !ok {
		return 0
	}
	cands, files := complete(line, current)
	completion.Print(stdout, cands, files)
	return 0
}

// complete walks line, the words after the program name and before the
// cursor, and returns the candidates for current, with files true when the
// shell should offer file names as well.
func complete(line []string, current string) (cands []string, files bool) {
	fs := flag.NewFlagSet("karvi-prune", flag.ContinueOnError)
	DefineFlags(fs)
	for i := 0; i < len(line); i++ {
		arg := line[i]
		if arg == "--" || !isFlagArg(arg) {
			// The rest is positional to the flag package, and the helper
			// takes no positional: the line is already refused.
			return nil, false
		}
		name, _, hasInline := splitFlagArg(arg)
		f := fs.Lookup(name)
		if f == nil || hasInline || isBoolFlag(f) {
			continue
		}
		if i+1 == len(line) {
			// The value is the word under the cursor.
			return values(f, current)
		}
		i++ // the value given on the line
	}
	if name, inline, hasInline := splitFlagArg(current); isFlagArg(current) && hasInline {
		// A value typed inline: the shell replaces the whole word, so each
		// candidate carries the flag before it.
		f := fs.Lookup(name)
		if f == nil || isBoolFlag(f) {
			return nil, false
		}
		prefix := current[:len(current)-len(inline)]
		cands, files = values(f, inline)
		for i := range cands {
			cands[i] = prefix + cands[i]
		}
		return cands, files
	}
	if current != "" && current[0] != '-' {
		// A positional word begun: the helper takes none.
		return nil, false
	}
	var names []string
	fs.VisitAll(func(f *flag.Flag) { names = append(names, "--"+f.Name) })
	return completion.Filter(names, current), false
}

// values are the candidates for the value of f, filtered to current, and
// whether the shell may complete a path.
func values(f *flag.Flag, current string) ([]string, bool) {
	v := flagValues[f.Name]
	return completion.Filter(v.words, current), v.path
}

// isFlagArg reports whether arg is spelled as a flag to the flag package:
// one or two leading dashes and at least one more character.
func isFlagArg(arg string) bool {
	return len(arg) > 1 && arg[0] == '-' && arg != "--"
}

// splitFlagArg separates a flag argument's name from an inline value.
func splitFlagArg(arg string) (name, inline string, hasInline bool) {
	body := strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-")
	return strings.Cut(body, "=")
}

// isBoolFlag reports whether f takes no value word (--dry-run, --verbose).
func isBoolFlag(f *flag.Flag) bool {
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
