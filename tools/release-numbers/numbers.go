package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

// counter is one schema counter: its row in the table, the package
// directory that owns it, its constant there, and the release manifest's
// field for it ("" where the manifest has none).
type counter struct {
	row      string
	pkg      string
	constant string
	manifest string
}

// counters is every schema counter, in the table's order. The tests check
// each against the constant the program imports, and that the tree holds no
// SchemaVersion constant missing here.
var counters = []counter{
	{"configuration schema", "configschema", "ConfigSchemaVersion", "configuration_schema_version"},
	{"configuration registry", "configschema", "RegistrySchemaVersion", "configuration_registry_schema_version"},
	{"execution plan", "executionplan", "SchemaVersion", "execution_plan_schema_version"},
	{"command record", "records", "CommandSchemaVersion", "command_record_schema_version"},
	{"plan report", "records", "PlanReportSchemaVersion", "plan_report_schema_version"},
	{"scoreboard", "records", "ScoreboardSchemaVersion", "scoreboard_schema_version"},
	{"audit", "records", "AuditSchemaVersion", "audit_schema_version"},
	{"job manifest and summary", "records", "JobSchemaVersion", "job_schema_version"},
	{"metrics", "records", "MetricsSchemaVersion", ""},
	{"daemon IPC", "internal/ipc", "SchemaVersion", "daemon_ipc_schema_version"},
	{"credential package", "credentialpackage", "SchemaVersion", "credential_package_schema_version"},
	{"inventory", "inventory", "SchemaVersion", ""},
}

const (
	locGo    = "Lines of Code: Go"
	locOther = "Lines of Code: Other"

	buildResult = "release/BUILD-RESULT.md"
	manifest    = "release/release-manifest.json"
)

type kind int

const (
	goRow kind = iota
	moduleRow
	counterRow
	countRow // never bold: the two counts differ at every release
)

type row struct {
	kind  kind
	name  string
	value string
}

// release is one column of the table: the version heading it, where its
// values were read (named beside a value in a refusal), and its rows in
// order.
type release struct {
	version string
	source  string
	rows    []row
}

// tree is one release's files: the working tree, or a tag read through git.
type tree interface {
	list(dir string) ([]string, error) // the paths of the files directly in dir
	all() ([]string, error)            // every tracked file's path
	read(path string) ([]byte, error)
}

// readRelease reads a release's rows from its tree; goVersion is read
// elsewhere, the source not recording the toolchain.
func readRelease(t tree, version, source, goVersion string) (release, error) {
	r := release{version: version, source: source, rows: []row{{goRow, "Go", goVersion}}}
	gomod, err := t.read("go.mod")
	if err != nil {
		return r, err
	}
	modules, err := requires(gomod)
	if err != nil {
		return r, err
	}
	r.rows = append(r.rows, modules...)
	for _, c := range counters {
		v, err := readCounter(t, c)
		if err != nil {
			return r, err
		}
		r.rows = append(r.rows, row{counterRow, c.row, v})
	}
	paths, err := t.all()
	if err != nil {
		return r, err
	}
	goLines, other := 0, 0
	for _, p := range paths {
		if strings.HasPrefix(p, "vendor/") {
			continue
		}
		data, err := t.read(p)
		if err != nil {
			return r, err
		}
		g, o := countLines(p, data)
		goLines, other = goLines+g, other+o
	}
	return withCounts(r, goLines, other), nil
}

// withCounts is r with the two counts as its last rows.
func withCounts(r release, goLines, other int) release {
	r.rows = append(append([]row(nil), r.rows...),
		row{countRow, locGo, thousands(goLines)}, row{countRow, locOther, thousands(other)})
	return r
}

// readCounter reads c's constant from the package's files, tests aside: an
// integer literal, declared once.
func readCounter(t tree, c counter) (string, error) {
	paths, err := t.list(c.pkg)
	if err != nil {
		return "", fmt.Errorf("%s: %w", c.pkg, err)
	}
	var found []string
	for _, p := range paths {
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			continue
		}
		data, err := t.read(p)
		if err != nil {
			return "", err
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, data, parser.SkipObjectResolution)
		if err != nil {
			return "", err
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				spec := s.(*ast.ValueSpec)
				for i, n := range spec.Names {
					if n.Name != c.constant {
						continue
					}
					var lit *ast.BasicLit
					if i < len(spec.Values) {
						lit, _ = spec.Values[i].(*ast.BasicLit)
					}
					v, err := int64(0), error(nil)
					if lit != nil && lit.Kind == token.INT {
						v, err = strconv.ParseInt(lit.Value, 0, 64)
					}
					if lit == nil || lit.Kind != token.INT || err != nil {
						return "", fmt.Errorf("%s: %s is not an integer literal", c.pkg, c.constant)
					}
					found = append(found, strconv.FormatInt(v, 10))
				}
			}
		}
	}
	switch len(found) {
	case 0:
		return "", fmt.Errorf("%s: no constant %s", c.pkg, c.constant)
	case 1:
		return found[0], nil
	}
	return "", fmt.Errorf("%s: %s is declared %d times", c.pkg, c.constant, len(found))
}

// requires reads go.mod's required modules in the file's order, the
// indirect ones included.
func requires(gomod []byte) ([]row, error) {
	var rows []row
	block := false
	for _, line := range strings.Split(string(gomod), "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		f := strings.Fields(line)
		switch {
		case block && len(f) == 1 && f[0] == ")":
			block = false
		case block && len(f) == 2:
			rows = append(rows, row{moduleRow, f[0], f[1]})
		case block && len(f) != 0:
			return nil, fmt.Errorf("go.mod: %q is not a requirement", strings.TrimSpace(line))
		case len(f) == 2 && f[0] == "require" && f[1] == "(":
			block = true
		case len(f) == 3 && f[0] == "require":
			rows = append(rows, row{moduleRow, f[1], f[2]})
		}
	}
	if block {
		return nil, fmt.Errorf("go.mod: a require block is not closed")
	}
	return rows, nil
}

// countLines is a file's lines in each count: a .go file's in Go's, a
// program's that is not Go (a first line starting "#!", or a file named
// Makefile) in Other's; vendor/ counts in neither.
func countLines(p string, data []byte) (goLines, other int) {
	if strings.HasPrefix(p, "vendor/") {
		return 0, 0
	}
	n := bytes.Count(data, []byte("\n"))
	switch {
	case strings.HasSuffix(p, ".go"):
		return n, 0
	case bytes.HasPrefix(data, []byte("#!")) || path.Base(p) == "Makefile":
		return 0, n
	}
	return 0, 0
}

// previousTag is the highest karvi-vMAJOR.MINOR.PATCH tag below version.
func previousTag(tags []string, version string) (string, error) {
	v, ok := semver(version)
	if !ok {
		return "", fmt.Errorf("VERSION %q is not MAJOR.MINOR.PATCH", version)
	}
	best, bv := "", [3]int{}
	for _, tag := range tags {
		s, ok := strings.CutPrefix(tag, "karvi-v")
		if !ok {
			continue
		}
		if tv, ok := semver(s); ok && less(tv, v) && (best == "" || less(bv, tv)) {
			best, bv = tag, tv
		}
	}
	if best == "" {
		return "", fmt.Errorf("no karvi-v tag below %s", version)
	}
	return best, nil
}

func semver(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		if p == "" || strings.Trim(p, "0123456789") != "" {
			return v, false
		}
		v[i], _ = strconv.Atoi(p)
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// manifestGo is the Go a tag's release manifest records it was built with.
func manifestGo(t tree) (string, error) {
	data, err := t.read(manifest)
	if err != nil {
		return "", err
	}
	var m struct {
		Build struct {
			GoVersion string `json:"go_version"`
		} `json:"build"`
	}
	if err := json.Unmarshal(data, &m); err != nil || m.Build.GoVersion == "" {
		return "", fmt.Errorf("%s: no build.go_version", manifest)
	}
	return strings.TrimPrefix(m.Build.GoVersion, "go"), nil
}

// diskTree is the working tree: its tracked files as git lists them, each
// read from the disk.
type diskTree struct{ root string }

func (d diskTree) list(dir string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(d.root, filepath.FromSlash(dir)))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.Type().IsRegular() {
			out = append(out, path.Join(dir, e.Name()))
		}
	}
	return out, nil
}

func (d diskTree) all() ([]string, error) {
	out, err := git(d.root, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }), nil
}

func (d diskTree) read(p string) ([]byte, error) {
	return os.ReadFile(filepath.Join(d.root, filepath.FromSlash(p)))
}

// tagTree is a tag's files, read through one git cat-file process.
type tagTree struct {
	root, tag string
	cmd       *exec.Cmd
	in        io.WriteCloser
	out       *bufio.Reader
}

// blobs lists the files under the tag's treeish, recursively or not.
func (t *tagTree) blobs(treeish string, recursive bool) ([]string, error) {
	args := []string{"ls-tree", "-z"}
	if recursive {
		args = append(args, "-r")
	}
	out, err := git(t.root, append(args, treeish)...)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 }) {
		meta, name, ok := strings.Cut(entry, "\t")
		if f := strings.Fields(meta); ok && len(f) == 3 && f[1] == "blob" {
			paths = append(paths, name)
		}
	}
	return paths, nil
}

func (t *tagTree) list(dir string) ([]string, error) {
	names, err := t.blobs(t.tag+":"+dir, false)
	for i, n := range names {
		names[i] = path.Join(dir, n)
	}
	return names, err
}

func (t *tagTree) all() ([]string, error) { return t.blobs(t.tag, true) }

func (t *tagTree) read(p string) ([]byte, error) {
	if t.cmd == nil {
		t.cmd = exec.Command("git", "-C", t.root, "cat-file", "--batch")
		in, err := t.cmd.StdinPipe()
		if err != nil {
			return nil, err
		}
		out, err := t.cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := t.cmd.Start(); err != nil {
			return nil, fmt.Errorf("git cat-file: %w", err)
		}
		t.in, t.out = in, bufio.NewReader(out)
	}
	if _, err := fmt.Fprintf(t.in, "%s:%s\n", t.tag, p); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	head, err := t.out.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	f := strings.Fields(head)
	if len(f) != 3 {
		return nil, fmt.Errorf("%s:%s: %s", t.tag, p, strings.TrimSpace(head))
	}
	size, err := strconv.Atoi(f[2])
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %q", strings.TrimSpace(head))
	}
	data := make([]byte, size+1) // the blob and its closing newline
	if _, err := io.ReadFull(t.out, data); err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	return data[:size], nil
}

func (t *tagTree) close() {
	if t.cmd != nil {
		t.in.Close()
		_ = t.cmd.Wait()
	}
}

// git runs git in dir; a failure carries git's own message.
func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", args[0], msg)
	}
	return out, nil
}
