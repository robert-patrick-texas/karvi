// Command errorcodegen writes docs/ERROR-CODES.md from the error-code
// registry. The output is deterministic.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
)

func main() {
	output := flag.String("output", "docs/ERROR-CODES.md", "output Markdown table")
	flag.Parse()
	if errs := errorcodes.Validate(); len(errs) > 0 {
		for _, err := range errs {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, []byte(errorcodes.RenderMarkdown()), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
