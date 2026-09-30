// Command configtable prints the fixed-key configuration table from the
// configuration registry (the registry is the source and the table its
// rendering), one row per entry in registry order, for the revision that
// regenerates the table: go run ./tools/configtable > table.md, then the rows
// replace the table's rows and the dynamic slot ssh.transports.<name> is
// kept as a row of its own.
package main

import (
	"fmt"
	"strings"

	"github.com/robert-patrick-texas/karvi/configschema"
)

func main() {
	for _, e := range configschema.Entries() {
		lock := "no"
		if e.LockEligible {
			lock = "yes"
		}
		doc := strings.ReplaceAll(e.Documentation, "|", "\\|")
		def := strings.ReplaceAll(e.DefaultLiteral, "|", "\\|")
		fmt.Printf("| `%s` | %v | `%s` | %s | %s |\n", e.Path, e.Kind, def, lock, doc)
	}
}
