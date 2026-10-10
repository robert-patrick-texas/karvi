package app

import (
	"io"
	"sync"

	"github.com/robert-patrick-texas/karvi/internal/configload"
)

// LoadWarnings says a process's configuration-load warnings ("optional
// include missing", "ignored unknown environment variable") at the load,
// each once: an invocation that loads its configuration several times (a
// run through a daemon three times, a stream once per job) says each warning
// at the first load that raised it. One is made per invocation and carried
// in CommonOptions; a nil LoadWarnings says nothing (a recorded login's
// child, whose wrapper said them).
type LoadWarnings struct {
	mu   sync.Mutex
	say  func(string)
	said map[string]bool
}

// NewLoadWarnings returns a reporter that passes each warning not yet said
// to say.
func NewLoadWarnings(say func(string)) *LoadWarnings {
	return &LoadWarnings{say: say, said: map[string]bool{}}
}

// WarningLines is the client's sink: a "warning: …" line on w.
func WarningLines(w io.Writer) func(string) {
	return func(msg string) { warning(w, msg) }
}

// Say says each of cfg's load warnings not yet said by l.
func (l *LoadWarnings) Say(cfg configload.Snapshot) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, w := range cfg.Warnings {
		if !l.said[w] {
			l.said[w] = true
			l.say(w)
		}
	}
}
