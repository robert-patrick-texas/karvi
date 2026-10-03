package exitcode

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

// TestStatusesDefineEveryConstant: every Exit constant of this package has
// exactly one entry in Statuses, under its own name, with a meaning; no
// entry stands for a number without a constant; the list is in order.
func TestStatusesDefineEveryConstant(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "exitcode.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	constants := map[string]int{}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, s := range g.Specs {
			v := s.(*ast.ValueSpec)
			n, err := strconv.Atoi(v.Values[0].(*ast.BasicLit).Value)
			if err != nil {
				t.Fatal(err)
			}
			constants[v.Names[0].Name] = n
		}
	}
	seen := map[int]bool{}
	for i, s := range Statuses {
		if n, ok := constants[s.Name]; !ok || n != s.Code {
			t.Errorf("%d %s: no constant of that name and number", s.Code, s.Name)
		}
		if seen[s.Code] {
			t.Errorf("%d listed twice", s.Code)
		}
		seen[s.Code] = true
		if s.Meaning == "" {
			t.Errorf("%d %s has no meaning", s.Code, s.Name)
		}
		if i > 0 && Statuses[i-1].Code >= s.Code {
			t.Errorf("%d out of order", s.Code)
		}
	}
	if len(Statuses) != len(constants) {
		t.Errorf("%d statuses, %d constants", len(Statuses), len(constants))
	}
	if ExitName(101) != "ExitPartialFailure" || ExitName(42) != "ExitUnknown" {
		t.Errorf("ExitName(101)=%s ExitName(42)=%s", ExitName(101), ExitName(42))
	}
}
