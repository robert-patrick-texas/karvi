package jobexec

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/robert-patrick-texas/karvi/internal/configload"
	"github.com/robert-patrick-texas/karvi/internal/display"
)

const maxDebugLineBytes = 4096

// DebugLogger returns a concurrency-safe, human-facing diagnostic sink. Debug
// events are intentionally one-line and bounded. Callers must pass only
// provenance, timing, identifiers, counts, and hashes--never secret values or
// raw device command text.
func DebugLogger(enabled bool, cfg configload.Snapshot, out io.Writer) func(string) {
	if !enabled || out == nil {
		return func(string) {}
	}
	formatter, err := display.NewFormatter(cfg.String("display.timestamp"), cfg.String("timezone"))
	if err != nil {
		return func(string) {}
	}
	var mu sync.Mutex
	lineBreaks := strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ", "\t", " ")
	return func(message string) {
		// One line per event: line breaks and tabs become spaces, but runs of
		// spaces stay, so a quoted command keeps its spacing.
		message = strings.TrimSpace(lineBreaks.Replace(message))
		prefix := formatter.Timestamp(time.Now()) + " DEBUG "
		available := maxDebugLineBytes - len(prefix) - 1 // Reserve the newline.
		if available < 0 {
			available = 0
		}
		message = truncateDebugMessage(message, available)
		mu.Lock()
		defer mu.Unlock()
		_, _ = fmt.Fprintf(out, "%s%s\n", prefix, message)
	}
}

func truncateDebugMessage(message string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(message) <= limit {
		return message
	}
	const marker = "..."
	if limit <= len(marker) {
		return marker[:limit]
	}
	end := limit - len(marker)
	for end > 0 && !utf8.RuneStart(message[end]) {
		end--
	}
	return message[:end] + marker
}
