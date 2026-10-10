package app

import (
	"io"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// SayWarnings passes each of cfg's load warnings ("optional include
// missing", "ignored unknown environment variable") to say, at the
// invocation's one read; a nil say says nothing (a recorded login's child,
// whose wrapper said them).
func SayWarnings(say func(string), cfg configload.Snapshot) {
	if say == nil {
		return
	}
	for _, w := range cfg.Warnings {
		say(w)
	}
}

// WarningLines is the client's sink: a "warning: …" line on w.
func WarningLines(w io.Writer) func(string) {
	return func(msg string) { warning(w, msg) }
}
