// Command configgen emits deterministic configuration artifacts from the
// project-owned registry. It intentionally writes no timestamps or host paths.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/internal/configload"
)

func main() {
	schemaPath := flag.String("schema", "schema/config-schema.json", "output JSON registry artifact")
	referencePath := flag.String("reference", "configs/reference.toml", "output reference TOML")
	flag.Parse()
	artifact := configschema.ArtifactValue()
	data, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		fatal(err)
	}
	data = append(data, '\n')
	if err := write(*schemaPath, data); err != nil {
		fatal(err)
	}
	if err := write(*referencePath, []byte(configload.RenderReference())); err != nil {
		fatal(err)
	}
}

func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
