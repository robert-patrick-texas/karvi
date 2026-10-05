package errorcodes

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// These tests enforce the registry against the source tree: every code a non-test
// Go file emits must be registered and active, and every active code must be
// emitted somewhere (otherwise it belongs in Planned or Retired).

type emission struct {
	code string
	pos  string
}

var prefixedMessage = regexp.MustCompile(`^([a-z][a-z0-9]*(?:_[a-z0-9]+)+)(?::|$)`)

// codeArgs names helper calls whose argument at the given index is a code.
var codeArgs = map[string]int{
	"newError":        0,
	"semantic":        0,
	"semanticErr":     0,
	"dynamicErr":      0,
	"tableErr":        0,
	"malformed":       0,
	"executableError": 0,
	"failedResult":    0,
	"FailedResult":    0,
	"siteFailure":     0,
	"codedText":       0,
	"SetError":        0,
	"setErrorLocked":  0,
	"writeError":      1,
	"failed":          1,
	"Ensure":          1,
	"ExitAt":          1,
	"reportError":     1,
	"reportCoded":     1,
	// osutil: the not-writable code a folder check reports.
	"writableDirectory":     1,
	"EnsureOutputDirectory": 2,
	"usageError":            1,
	// cli: the conflict code of two flags given together (--border with
	// --noborder, --of with --nof).
	"checkExclusive": 2,
	"emitFailureSet": 5,
	// credfile: the parse-failure code a file backend gives an uncoded
	// load error.
	"Classify": 2,
}

// isCodeExpr reports whether e reads a code field, as in
// defaultString(result.ErrorCode, "fallback_code").
func isCodeExpr(e ast.Expr) bool {
	sel, ok := e.(*ast.SelectorExpr)
	return ok && (sel.Sel.Name == "ErrorCode" || sel.Sel.Name == "Code")
}

// codeKeys names struct fields or map keys whose literal value is a code.
var codeKeys = map[string]bool{"Code": true, "ErrorCode": true, "Error": true, "code": true}

// codeVars names variables whose assigned literal is a code.
var codeVars = map[string]bool{"code": true, "unstartedReason": true, "summaryCode": true}

func moduleRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(wd, "..", "..")
}

func stringLit(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}

func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func isClassifier(rel string, fn *ast.FuncDecl) bool {
	if fn == nil {
		return false
	}
	switch fn.Name.Name {
	case "classifyOpen", "validateRule":
		return true
	case "classify":
		return filepath.ToSlash(filepath.Dir(rel)) == "internal/transport/systemssh"
	case "CheckList":
		return filepath.ToSlash(filepath.Dir(rel)) == "internal/sshalgorithms"
	}
	return false
}

func emittedCodes(t *testing.T) []emission {
	t.Helper()
	root := moduleRoot(t)
	fset := token.NewFileSet()
	var out []emission
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "testdata", "bin", "release":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		add := func(code string, at token.Pos) {
			// A single word counts only when registered, so literals such as a
			// category ("device") in a code position are not mistaken for codes.
			_, registered := Lookup(code)
			if codePattern.MatchString(code) && (strings.Contains(code, "_") || registered) {
				p := fset.Position(at)
				out = append(out, emission{code: code, pos: rel + ":" + strconv.Itoa(p.Line)})
			}
		}
		for _, decl := range file.Decls {
			fn, _ := decl.(*ast.FuncDecl)
			classifier := isClassifier(rel, fn)
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					name := calleeName(x)
					switch name {
					case "Errorf", "New", "Sprintf":
						if len(x.Args) > 0 {
							if s, ok := stringLit(x.Args[0]); ok {
								if m := prefixedMessage.FindStringSubmatch(s); m != nil && (name != "Sprintf" || strings.HasPrefix(s, m[1]+":")) {
									add(m[1], x.Pos())
								}
							}
						}
					}
					if name == "defaultString" && len(x.Args) == 2 && isCodeExpr(x.Args[0]) {
						if s, ok := stringLit(x.Args[1]); ok {
							add(s, x.Args[1].Pos())
						}
					}
					if i, ok := codeArgs[name]; ok && len(x.Args) > i {
						if s, ok := stringLit(x.Args[i]); ok {
							add(s, x.Args[i].Pos())
						}
					}
				case *ast.KeyValueExpr:
					key := ""
					switch k := x.Key.(type) {
					case *ast.Ident:
						key = k.Name
					case *ast.BasicLit:
						key, _ = strconv.Unquote(k.Value)
					}
					if codeKeys[key] {
						if s, ok := stringLit(x.Value); ok {
							add(s, x.Pos())
						}
					}
				case *ast.AssignStmt:
					for i, lhs := range x.Lhs {
						name := ""
						switch l := lhs.(type) {
						case *ast.Ident:
							name = l.Name
						case *ast.SelectorExpr:
							if l.Sel.Name == "ErrorCode" || l.Sel.Name == "Code" {
								name = "code"
							}
						}
						if codeVars[name] && i < len(x.Rhs) {
							if s, ok := stringLit(x.Rhs[i]); ok {
								add(s, x.Rhs[i].Pos())
							}
						}
					}
				case *ast.ReturnStmt:
					if classifier && len(x.Results) > 0 {
						if s, ok := stringLit(x.Results[0]); ok {
							add(s, x.Pos())
						}
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// detailFuncs lists functions whose uncoded error messages are details of a
// registered code attached by their caller. A key is "path:Function" or
// "path:*" for a whole file.
var detailFuncs = map[string]string{
	"internal/configload/tomlmini/parser.go:*":                            "TOML syntax diagnostics reported under config_toml_syntax",
	"internal/configload/include.go:parseDirectiveTarget":                 "include directive syntax reported under config_include_syntax",
	"internal/configload/lock.go:winningLock":                             "reported under config_lock_equal_specificity",
	"internal/configload/validate.go:validateInheritance":                 "reported under config_policy_cycle",
	"internal/configload/validate.go:validateRule":                        "returns its rule code alongside the message",
	"configschema/registry.go:ValidateScalar":                             "reported under config_type_error",
	"internal/credentialbackend/cloginrc/backend.go:flatten":              ".cloginrc syntax reported under cloginrc_malformed",
	"internal/credentialbackend/cloginrc/backend.go:parseBraced":          ".cloginrc syntax reported under cloginrc_malformed",
	"internal/credentialbackend/cloginrc/backend.go:parseBare":            ".cloginrc syntax reported under cloginrc_malformed",
	"internal/termtext/timing.go:ParseTiming":                             "reported under transcript_timing_unreadable",
	"internal/hostkey/policy.go:Parse":                                    "reported under host_key_policy_invalid",
	"internal/hostkey/inspect.go:inspectRemoteWithBinary":                 "ssh-keyscan diagnostics reported in the insecure-policy mismatch warning",
	"internal/ipc/protocol.go:CallForSchema":                              "relays the daemon's registered error code",
	"internal/ipc/protocol.go:Stream":                                     "relays the daemon's registered error code",
	"internal/canary/exercise.go:Exercise":                                "the harness formats a probe error to scan it; the error is never returned",
	"executionplan/digest.go:UnmarshalText":                               "malformed digest text is reported under the decoding operation's code (job_request_malformed at the daemon)",
	"internal/transport/systemssh/command_session.go:startCommandSession": "message begins with the code classified from OpenSSH diagnostics",
	"internal/transport/systemssh/systemssh.go:Interactive":               "message begins with the code classified from OpenSSH diagnostics",
	"internal/adapters/scrapligov1/dial.go:password":                      "x/crypto reports a failed password callback under authentication_failed",
	"internal/sshalgorithms/sshalgorithms.go:CheckList":                   "returns its rule code alongside the message",
	"internal/sshalgorithms/sshalgorithms.go:CheckProfile":                "returns a ProfileError carrying its rule code",
	"internal/fakedevice/server.go:*":                                     "the engineering fixture's server-side diagnostics; never an operator error",
}

// TestUncodedErrorsAreDetails fails when an error is constructed without a
// code outside a listed detail function, unless it wraps with %w, is attached
// to a coded composite (Err beside Code), is assigned to a result's Err beside
// its ErrorCode, or is passed to a helper that attaches a code.
func TestUncodedErrorsAreDetails(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			switch d.Name() {
			case "vendor", ".git", "testdata", "bin", "release":
				return filepath.SkipDir
			}
			if rel == "tools" || rel == "internal/errorcodes" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return true
			}
			stack = append(stack, n)
			call, ok := n.(*ast.CallExpr)
			if !ok || !isPlainErrorConstructor(call) || len(call.Args) == 0 {
				return true
			}
			msg, ok := stringLit(call.Args[0])
			if !ok || prefixedMessage.MatchString(msg) || strings.Contains(msg, "%w") {
				return true
			}
			if attachedToCode(stack) {
				return true
			}
			fn := enclosingFunc(stack)
			if _, ok := detailFuncs[rel+":*"]; ok {
				return true
			}
			if _, ok := detailFuncs[rel+":"+fn]; ok {
				return true
			}
			t.Errorf("%s:%d (%s) constructs an uncoded error %q; attach a registered code or list the function in detailFuncs", rel, fset.Position(call.Pos()).Line, fn, msg)
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func isPlainErrorConstructor(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && ((pkg.Name == "fmt" && sel.Sel.Name == "Errorf") || (pkg.Name == "errors" && sel.Sel.Name == "New"))
}

func attachedToCode(stack []ast.Node) bool {
	if len(stack) < 2 {
		return false
	}
	child := stack[len(stack)-1]
	switch parent := stack[len(stack)-2].(type) {
	case *ast.CallExpr:
		name := calleeName(parent)
		if _, ok := codeArgs[name]; ok {
			return true
		}
		return name == "unavailable"
	case *ast.KeyValueExpr:
		key, ok := parent.Key.(*ast.Ident)
		if !ok || key.Name != "Err" || len(stack) < 3 {
			return false
		}
		if lit, ok := stack[len(stack)-3].(*ast.CompositeLit); ok {
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "Code" || k.Name == "ErrorCode") {
						return true
					}
				}
			}
		}
	case *ast.AssignStmt:
		for i, rhs := range parent.Rhs {
			if rhs == child && i < len(parent.Lhs) {
				if sel, ok := parent.Lhs[i].(*ast.SelectorExpr); ok && sel.Sel.Name == "Err" {
					return true
				}
			}
		}
	}
	return false
}

func enclosingFunc(stack []ast.Node) string {
	for i := len(stack) - 1; i >= 0; i-- {
		if fn, ok := stack[i].(*ast.FuncDecl); ok {
			return fn.Name.Name
		}
	}
	return ""
}

// TestNoRawErrorPrints fails when command-line code prints an error value
// directly instead of through reportError, reportCoded, or reportUsage.
func TestNoRawErrorPrints(t *testing.T) {
	root := moduleRoot(t)
	fset := token.NewFileSet()
	for _, dir := range []string{"internal/cli", "cmd/karvi"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			rel := dir + "/" + name
			file, err := parser.ParseFile(fset, filepath.Join(root, rel), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range file.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok || fn.Name.Name == "reportError" || fn.Name.Name == "reportCoded" || fn.Name.Name == "reportUsage" {
					continue
				}
				ast.Inspect(fn, func(n ast.Node) bool {
					call, ok := n.(*ast.CallExpr)
					if !ok || !strings.HasPrefix(calleeName(call), "Fprint") {
						return true
					}
					for _, arg := range call.Args[1:] {
						if id, ok := arg.(*ast.Ident); ok && (id.Name == "err" || id.Name == "e" || strings.HasSuffix(id.Name, "Err")) {
							t.Errorf("%s:%d prints error %s directly; use reportError or reportCoded", rel, fset.Position(call.Pos()).Line, id.Name)
						}
					}
					return true
				})
			}
		}
	}
}

func TestEmittedCodesAreRegistered(t *testing.T) {
	for _, e := range emittedCodes(t) {
		entry, ok := Lookup(e.code)
		switch {
		case !ok:
			t.Errorf("%s emits unregistered code %s", e.pos, e.code)
		case entry.Status != Active:
			t.Errorf("%s emits %s code %s", e.pos, entry.Status, e.code)
		}
	}
}

func TestActiveCodesAreEmitted(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range emittedCodes(t) {
		seen[e.code] = true
	}
	var missing []string
	for _, entry := range All() {
		if entry.Status == Active && !seen[entry.Code] {
			missing = append(missing, entry.Code)
		}
	}
	sort.Strings(missing)
	for _, code := range missing {
		t.Errorf("active code %s is not emitted by source; mark it planned or retired", code)
	}
}
