// Command karvi-audit shows karvi's audit trail, the events every job and
// every login writes to journald, one event per row: it runs journalctl with
// the trail's matches and lays each entry out as a row (docs/EXAMPLES.md,
// chapter 64). It reads no configuration: its flags are journalctl's window,
// pattern, follow, and order, and the trail's operator and device matches,
// defined once in the auditview package, which the reader's help, its tab
// completion, and its man page (tools/mangen) read too. Who reads whose
// events is journalctl's. The exit is 0 when journalctl ended 0, 1 when
// journalctl is missing or ended otherwise, 2 for a usage error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/robert-patrick-texas/karvi/internal/auditview"
	"github.com/robert-patrick-texas/karvi/internal/completion"
	"github.com/robert-patrick-texas/karvi/internal/flagspec"
	"github.com/robert-patrick-texas/karvi/internal/helplayout"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	// The hidden word stands outside the flags: it is taken before parsing,
	// since the words it carries are the line being completed, not this
	// invocation's.
	if len(args) > 0 && args[0] == completion.Word {
		return auditview.Spec.Complete(args[1:], stdout)
	}
	fs := flag.NewFlagSet(auditview.Spec.Program, flag.ContinueOnError)
	// The flag package's own text is the refusal's, written here in the
	// error colour; the help is printed here too: -h and --help to standard
	// output, a usage error's after its message on standard error, laid out
	// as karvi's help is and coloured as the table is.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	f := auditview.DefineFlags(fs)
	help := func(w *os.File) {
		fmt.Fprint(w, helplayout.Layout(auditview.Spec.Help(fs), flagspec.HelpStyle(auditview.Colours(w, *f.NoColor))))
	}
	usageError := func(text string) int {
		auditview.Message(stderr, auditview.Colours(stderr, *f.NoColor), "error", text)
		help(stderr)
		return 2
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			help(stdout)
			return 0
		}
		return usageError(err.Error())
	}
	if len(fs.Args()) != 0 {
		return usageError("no positional argument is taken")
	}
	return auditview.Run(f, stdout, stderr)
}
