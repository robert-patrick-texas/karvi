// Package karvi holds the release's number as VERSION writes it, the one
// place it is written; buildinfo reads it here.
package karvi

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version is VERSION's number.
var Version = strings.TrimSpace(versionFile)
