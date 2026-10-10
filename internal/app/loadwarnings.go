package app

import (
	"io"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/jobexec"
)

// WarningSink says one of cfg's load warnings: the client's writes its
// warning line in cfg's colours (WarningLines), daemon serve's logs it.
type WarningSink func(cfg configload.Snapshot, warning string)

// SayWarnings passes each of cfg's load warnings ("optional include
// missing", "ignored unknown environment variable") to say, at the
// invocation's one read; a nil say says nothing (a recorded login's child,
// whose wrapper said them).
func SayWarnings(say WarningSink, cfg configload.Snapshot) {
	if say == nil {
		return
	}
	for _, w := range cfg.Warnings {
		say(cfg, w)
	}
}

// WarningLines is the client's sink: the warning's line on w, in cfg's
// colours (jobexec.WriteWarning).
func WarningLines(w io.Writer) WarningSink {
	return func(cfg configload.Snapshot, msg string) { jobexec.WriteWarning(w, cfg, msg) }
}
