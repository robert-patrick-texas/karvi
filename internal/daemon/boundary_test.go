package daemon

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/internal/canary/canarytest"
)

// TestDaemonReachesNoLoaderOrBackend is the transitive import exclusion
// for the daemon: it reaches no inventory
// loader, tabular reader, target source, credential backend, planner, or
// application package at any depth.
func TestDaemonReachesNoLoaderOrBackend(t *testing.T) {
	const m = "github.com/robert-patrick-texas/karvi/"
	canarytest.AssertUnreachable(t, ".", m+"internal/inventoryload", m+"tabular", m+"internal/targetsource", m+"internal/credentialbackend", m+"internal/credentialbackend/", m+"internal/app", m+"internal/planner")
	t.Logf("daemon reaches %d module packages", len(canarytest.Reachable(t, ".")))
}

// TestNoPackageLevelSecretHolders:
// no package-level variable in the daemon, the job executor, the
// executor, or the transports names a secret-bearing type, so nothing can
// hold a grant across jobs. A statement of the property over the source,
// not a proof of it.
func TestNoPackageLevelSecretHolders(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := []string{"credentialpackage.", "credentials.Secret", "credentials.Credential", "credentials.Resolved", "credentials.Material", "secrets."}
	fset := token.NewFileSet()
	checked := 0
	for _, dir := range []string{"internal/daemon", "internal/jobexec", "internal/executor", "internal/transport"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs := spec.(*ast.ValueSpec)
					checked++
					text := ""
					if vs.Type != nil {
						text = types.ExprString(vs.Type)
					}
					for _, v := range vs.Values {
						text += " " + types.ExprString(v)
					}
					for _, f := range forbidden {
						if strings.Contains(text, f) {
							t.Errorf("%s: package-level var %s names %s: %s", path, vs.Names[0].Name, f, text)
						}
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked == 0 {
		t.Fatal("no package-level variable was checked")
	}
	t.Logf("%d package-level variables checked", checked)
}
