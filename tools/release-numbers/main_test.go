package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/robert-patrick-texas/karvi/configschema"
	"github.com/robert-patrick-texas/karvi/credentialpackage"
	"github.com/robert-patrick-texas/karvi/executionplan"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
	"github.com/robert-patrick-texas/karvi/inventory"
	"github.com/robert-patrick-texas/karvi/records"
)

// root is the karvi tree this package sits in.
const root = "../.."

// imported is each counter's value as the program sees it.
var imported = map[string]int{
	"configuration schema":     configschema.ConfigSchemaVersion,
	"configuration registry":   configschema.RegistrySchemaVersion,
	"execution plan":           executionplan.SchemaVersion,
	"command record":           records.CommandSchemaVersion,
	"plan report":              records.PlanReportSchemaVersion,
	"scoreboard":               records.ScoreboardSchemaVersion,
	"audit":                    records.AuditSchemaVersion,
	"job manifest and summary": records.JobSchemaVersion,
	"metrics":                  records.MetricsSchemaVersion,
	"daemon IPC":               ipc.SchemaVersion,
	"credential package":       credentialpackage.SchemaVersion,
	"inventory":                inventory.SchemaVersion,
}

func TestEachCounterIsItsOwnersConstant(t *testing.T) {
	if len(counters) != len(imported) {
		t.Fatalf("the list has %d counters, the test imports %d", len(counters), len(imported))
	}
	tr := diskTree{root: root}
	for _, c := range counters {
		want, ok := imported[c.row]
		if !ok {
			t.Errorf("%s: no imported constant in the test", c.row)
			continue
		}
		got, err := readCounter(tr, c)
		if err != nil {
			t.Errorf("%s: %v", c.row, err)
			continue
		}
		if got != strconv.Itoa(want) {
			t.Errorf("%s: %s.%s read %s, the program's is %d", c.row, c.pkg, c.constant, got, want)
		}
	}
}

func TestNoSchemaVersionConstantIsMissingFromTheList(t *testing.T) {
	listed := map[string]bool{}
	for _, c := range counters {
		listed[c.pkg+"."+c.constant] = true
	}
	var found []string
	err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() {
			if rel == "vendor" || rel == ".git" || e.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		for _, d := range f.Decls {
			g, ok := d.(*ast.GenDecl)
			if !ok || g.Tok != token.CONST {
				continue
			}
			for _, s := range g.Specs {
				for _, n := range s.(*ast.ValueSpec).Names {
					if strings.HasSuffix(n.Name, "SchemaVersion") {
						found = append(found, path.Dir(rel)+"."+n.Name)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) == 0 {
		t.Fatal("no SchemaVersion constant found in the tree")
	}
	for _, f := range found {
		if !listed[f] {
			t.Errorf("%s is not in the counter list", f)
		}
	}
}

func TestReadCounterTakesTheIntegerLiteral(t *testing.T) {
	tr := mapTree{
		"a/one.go":      "package a\n\nconst (\n\tOther = 9\n\tFirstSchemaVersion, SecondSchemaVersion = 3, 41 // two\n)\n",
		"a/one_test.go": "package a\n\nconst ThirdSchemaVersion = 7\n",
		"a/b/two.go":    "package b\n\nconst ThirdSchemaVersion = 8\n",
		"a/iota.go":     "package a\n\nconst (\n\tFourthSchemaVersion = iota\n)\n",
	}
	for _, tc := range []struct{ constant, want, err string }{
		{"SecondSchemaVersion", "41", ""},
		{"ThirdSchemaVersion", "", "a: no constant ThirdSchemaVersion"},
		{"FourthSchemaVersion", "", "a: FourthSchemaVersion is not an integer literal"},
	} {
		got, err := readCounter(tr, counter{row: "x", pkg: "a", constant: tc.constant})
		if got != tc.want || errText(err) != tc.err {
			t.Errorf("%s: got %q, %q; want %q, %q", tc.constant, got, errText(err), tc.want, tc.err)
		}
	}
}

func TestRequiresReadsEveryModuleInOrder(t *testing.T) {
	got, err := requires([]byte("module m\n\ngo 1.27.0\n\nrequire (\n\tgithub.com/a/b v1.4.3\n\tgolang.org/x/c v0.58.0 // why\n)\n\nrequire (\n\tgithub.com/d/e v1.1.24 // indirect\n)\n\nrequire example.com/f v0.0.0-20240101000000-abcdefabcdef\n\nreplace (\n\texample.com/f => ../f\n)\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []row{
		{moduleRow, "github.com/a/b", "v1.4.3"},
		{moduleRow, "golang.org/x/c", "v0.58.0"},
		{moduleRow, "github.com/d/e", "v1.1.24"},
		{moduleRow, "example.com/f", "v0.0.0-20240101000000-abcdefabcdef"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestCountLinesTakesGoAndTheOtherPrograms(t *testing.T) {
	for _, tc := range []struct {
		path, data string
		goLines    int
		other      int
	}{
		{"internal/a/a.go", "package a\n\nfunc A() {}\n", 3, 0},
		{"internal/a/a_test.go", "package a\n", 1, 0},
		{"scripts/x.sh", "#!/bin/sh\necho x\n", 0, 2},
		{"scripts/lib/y.sh", "# sourced, no #! line\necho y\n", 0, 0},
		{"Makefile", "all:\n\ttrue\n", 0, 2},
		{"debian/rules", "#!/usr/bin/make -f\n%:\n\tdh $@\n", 0, 3},
		{"vendor/m/m.go", "package m\n", 0, 0},
		{"vendor/m/run.sh", "#!/bin/sh\n", 0, 0},
	} {
		g, o := countLines(tc.path, []byte(tc.data))
		if g != tc.goLines || o != tc.other {
			t.Errorf("%s: got %d, %d; want %d, %d", tc.path, g, o, tc.goLines, tc.other)
		}
	}
}

func TestReadReleaseOrdersItsRows(t *testing.T) {
	tr := mapTree{
		"go.mod":        "module m\n\ngo 1.27.0\n\nrequire (\n\tgolang.org/x/b v0.2.0\n\tgithub.com/a/a v1.0.0\n)\n",
		"Makefile":      "all:\n",
		"run.sh":        "#!/bin/sh\necho\n",
		"main.go":       "package main\n\nfunc main() {}\n",
		"vendor/v/v.go": "package v\n\n\n\n",
	}
	for i, c := range counters {
		tr[c.pkg+"/"+strings.ToLower(c.constant)+".go"] = "package p\n\nconst " + c.constant + " = " + strconv.Itoa(i+1) + "\n"
	}
	got, err := readRelease(tr, "0.1.0", "the tree", "1.27.3")
	if err != nil {
		t.Fatal(err)
	}
	var names, values []string
	for _, r := range got.rows {
		names = append(names, r.name)
		values = append(values, r.value)
	}
	wantNames := []string{"Go", "golang.org/x/b", "github.com/a/a"}
	wantValues := []string{"1.27.3", "v0.2.0", "v1.0.0"}
	for i, c := range counters {
		wantNames = append(wantNames, c.row)
		wantValues = append(wantValues, strconv.Itoa(i+1))
	}
	// 12 counter files of 3 lines and main.go's 3; run.sh and the Makefile.
	wantNames = append(wantNames, locGo, locOther)
	wantValues = append(wantValues, "39", "3")
	if got.version != "0.1.0" || got.source != "the tree" || !reflect.DeepEqual(names, wantNames) || !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("got %s %s\n%q\n%q", got.version, got.source, names, values)
	}
}

func TestPreviousTagIsTheHighestBelowThisRelease(t *testing.T) {
	tags := []string{"karvi-v0.9.0", "karvi-v0.34.0", "karvi-v0.34.1", "karvi-v0.41.0", "karvi-v0.10.0", "v0.50.0", "karvi-v0.42.0-rc1", "karvi-v0.43.0"}
	for _, tc := range []struct{ version, want, err string }{
		{"0.42.0", "karvi-v0.41.0", ""},
		{"0.41.0", "karvi-v0.34.1", ""},
		{"0.34.1", "karvi-v0.34.0", ""},
		{"0.11.0", "karvi-v0.10.0", ""},
		{"0.9.0", "", "no karvi-v tag below 0.9.0"},
		{"0.42", "", `VERSION "0.42" is not MAJOR.MINOR.PATCH`},
	} {
		got, err := previousTag(tags, tc.version)
		if got != tc.want || errText(err) != tc.err {
			t.Errorf("%s: got %q, %q; want %q, %q", tc.version, got, errText(err), tc.want, tc.err)
		}
	}
}

// The values of 0.40.0 and 0.41.0, as their tags hold them.
func v0400() release {
	return mk("0.40.0", "karvi-v0.40.0", "1.27.3",
		[]string{"github.com/yuin/goldmark v1.8.7", "golang.org/x/crypto v0.58.0", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0"},
		[]string{"6", "33", "17", "5", "4", "5", "3", "5", "1", "12", "2", "1"}, 126766, 8247)
}

func v0410() release {
	return mk("0.41.0", "the tree", "1.27.3",
		[]string{"github.com/yuin/goldmark v1.8.7", "golang.org/x/crypto v0.58.0", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0"},
		[]string{"6", "34", "17", "5", "4", "5", "4", "5", "1", "12", "2", "1"}, 128401, 9694)
}

func mk(version, source, goVersion string, modules, values []string, goLines, other int) release {
	r := release{version: version, source: source, rows: []row{{goRow, "Go", goVersion}}}
	for _, m := range modules {
		f := strings.Fields(m)
		r.rows = append(r.rows, row{moduleRow, f[0], f[1]})
	}
	for i, c := range counters {
		r.rows = append(r.rows, row{counterRow, c.row, values[i]})
	}
	return withCounts(r, goLines, other)
}

const full0410 = `|                            | 0.40.0  | 0.41.0  |
| -------------------------- | ------- | ------- |
| Go                         | 1.27.3  | 1.27.3  |
| ` + "`github.com/yuin/goldmark`" + ` | v1.8.7  | v1.8.7  |
| ` + "`golang.org/x/crypto`" + `      | v0.58.0 | v0.58.0 |
| ` + "`golang.org/x/sys`" + `         | v0.49.0 | v0.49.0 |
| ` + "`golang.org/x/term`" + `        | v0.47.0 | v0.47.0 |
| configuration schema       | 6       | 6       |
| configuration registry     | 33      | **34**  |
| execution plan             | 17      | 17      |
| command record             | 5       | 5       |
| plan report                | 4       | 4       |
| scoreboard                 | 5       | 5       |
| audit                      | 3       | **4**   |
| job manifest and summary   | 5       | 5       |
| metrics                    | 1       | 1       |
| daemon IPC                 | 12      | 12      |
| credential package         | 2       | 2       |
| inventory                  | 1       | 1       |
| Lines of Code: Go          | 126,766 | 128,401 |
| Lines of Code: Other       | 8,247   | 9,694   |
`

const changelog0410 = `|                        | 0.40.0  | 0.41.0  |
| ---------------------- | ------- | ------- |
| configuration registry | 33      | 34      |
| audit                  | 3       | 4       |
| Lines of Code: Go      | 126,766 | 128,401 |
| Lines of Code: Other   | 8,247   | 9,694   |

Every other value is 0.40.0's.
`

func TestTablesOfARelease(t *testing.T) {
	if got := fullTable(v0400(), v0410()).String(); got != full0410 {
		t.Errorf("full table:\n%s\nwant:\n%s", got, full0410)
	}
	if got := changelogTable(v0400(), v0410()).String(); got != changelog0410 {
		t.Errorf("CHANGELOG table:\n%s\nwant:\n%s", got, changelog0410)
	}
}

func TestWideValuesKeepASpaceOnEachSide(t *testing.T) {
	wide := mk("10.100.0", "the tree", "1.30.12",
		[]string{"github.com/yuin/goldmark v1.8.7", "golang.org/x/crypto v0.100.12", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0"},
		[]string{"6", "1000", "17", "5", "4", "5", "4", "5", "1", "100", "2", "1"}, 1234567, 12345)
	want := `|                            | 0.40.0  | 10.100.0      |
| -------------------------- | ------- | ------------- |
| Go                         | 1.27.3  | **1.30.12**   |
| ` + "`github.com/yuin/goldmark`" + ` | v1.8.7  | v1.8.7        |
| ` + "`golang.org/x/crypto`" + `      | v0.58.0 | **v0.100.12** |
| ` + "`golang.org/x/sys`" + `         | v0.49.0 | v0.49.0       |
| ` + "`golang.org/x/term`" + `        | v0.47.0 | v0.47.0       |
| configuration schema       | 6       | 6             |
| configuration registry     | 33      | **1000**      |
| execution plan             | 17      | 17            |
| command record             | 5       | 5             |
| plan report                | 4       | 4             |
| scoreboard                 | 5       | 5             |
| audit                      | 3       | **4**         |
| job manifest and summary   | 5       | 5             |
| metrics                    | 1       | 1             |
| daemon IPC                 | 12      | **100**       |
| credential package         | 2       | 2             |
| inventory                  | 1       | 1             |
| Lines of Code: Go          | 126,766 | 1,234,567     |
| Lines of Code: Other       | 8,247   | 12,345        |
`
	if got := fullTable(v0400(), wide).String(); got != want {
		t.Errorf("full table:\n%s\nwant:\n%s", got, want)
	}
	wantChangelog := `|                        | 0.40.0  | 10.100.0  |
| ---------------------- | ------- | --------- |
| Go                     | 1.27.3  | 1.30.12   |
| ` + "`golang.org/x/crypto`" + `  | v0.58.0 | v0.100.12 |
| configuration registry | 33      | 1000      |
| audit                  | 3       | 4         |
| daemon IPC             | 12      | 100       |
| Lines of Code: Go      | 126,766 | 1,234,567 |
| Lines of Code: Other   | 8,247   | 12,345    |

Every other value is 0.40.0's.
`
	if got := changelogTable(v0400(), wide).String(); got != wantChangelog {
		t.Errorf("CHANGELOG table:\n%s\nwant:\n%s", got, wantChangelog)
	}
}

// 0.37.0 dropped scrapligo and its two indirect modules; each is "—" there,
// its row where it stood in 0.36.0.
func TestAModuleOneReleaseLacksIsADash(t *testing.T) {
	prev := mk("0.36.0", "karvi-v0.36.0", "1.27.3",
		[]string{"github.com/scrapli/scrapligo v1.4.3", "github.com/yuin/goldmark v1.8.6", "golang.org/x/crypto v0.58.0", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0", "github.com/creack/pty v1.1.24", "github.com/sirikothe/gotextfsm v1.2.0"},
		[]string{"6", "31", "15", "4", "4", "5", "2", "5", "1", "12", "2", "1"}, 112174, 7968)
	this := mk("0.37.0", "the tree", "1.27.3",
		[]string{"github.com/yuin/goldmark v1.8.6", "golang.org/x/crypto v0.58.0", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0"},
		[]string{"6", "32", "15", "4", "4", "5", "2", "5", "1", "12", "2", "1"}, 112243, 8151)
	want := `|                                  | 0.36.0  | 0.37.0  |
| -------------------------------- | ------- | ------- |
| Go                               | 1.27.3  | 1.27.3  |
| ` + "`github.com/scrapli/scrapligo`" + `   | v1.4.3  | **—**   |
| ` + "`github.com/yuin/goldmark`" + `       | v1.8.6  | v1.8.6  |
| ` + "`golang.org/x/crypto`" + `            | v0.58.0 | v0.58.0 |
| ` + "`golang.org/x/sys`" + `               | v0.49.0 | v0.49.0 |
| ` + "`golang.org/x/term`" + `              | v0.47.0 | v0.47.0 |
| ` + "`github.com/creack/pty`" + `          | v1.1.24 | **—**   |
| ` + "`github.com/sirikothe/gotextfsm`" + ` | v1.2.0  | **—**   |
| configuration schema             | 6       | 6       |
| configuration registry           | 31      | **32**  |
| execution plan                   | 15      | 15      |
| command record                   | 4       | 4       |
| plan report                      | 4       | 4       |
| scoreboard                       | 5       | 5       |
| audit                            | 2       | 2       |
| job manifest and summary         | 5       | 5       |
| metrics                          | 1       | 1       |
| daemon IPC                       | 12      | 12      |
| credential package               | 2       | 2       |
| inventory                        | 1       | 1       |
| Lines of Code: Go                | 112,174 | 112,243 |
| Lines of Code: Other             | 7,968   | 8,151   |
`
	if got := fullTable(prev, this).String(); got != want {
		t.Errorf("full table:\n%s\nwant:\n%s", got, want)
	}
	wantChangelog := `|                                  | 0.36.0  | 0.37.0  |
| -------------------------------- | ------- | ------- |
| ` + "`github.com/scrapli/scrapligo`" + `   | v1.4.3  | —       |
| ` + "`github.com/creack/pty`" + `          | v1.1.24 | —       |
| ` + "`github.com/sirikothe/gotextfsm`" + ` | v1.2.0  | —       |
| configuration registry           | 31      | 32      |
| Lines of Code: Go                | 112,174 | 112,243 |
| Lines of Code: Other             | 7,968   | 8,151   |

Every other value is 0.36.0's.
`
	if got := changelogTable(prev, this).String(); got != wantChangelog {
		t.Errorf("CHANGELOG table:\n%s\nwant:\n%s", got, wantChangelog)
	}

	// A module this release adds is "—" in the previous release's column,
	// which is never bold; a module gone from both has no row.
	next := mk("0.38.0", "the tree", "1.27.3",
		[]string{"github.com/yuin/goldmark v1.8.6", "example.com/new v1.0.0", "golang.org/x/crypto v0.58.0", "golang.org/x/sys v0.49.0", "golang.org/x/term v0.47.0"},
		[]string{"6", "32", "15", "4", "4", "5", "2", "5", "1", "12", "2", "1"}, 112243, 8151)
	got := fullTable(this, next).String()
	if !strings.Contains(got, "| `github.com/yuin/goldmark` | v1.8.6  | v1.8.6     |\n| `example.com/new`          | —       | **v1.0.0** |\n| `golang.org/x/crypto`      |") || strings.Contains(got, "scrapligo") {
		t.Errorf("full table:\n%s", got)
	}
}

func TestAReleaseWithNoMoveHasTheTwoCountsAlone(t *testing.T) {
	this := v0400()
	this.version, this.source = "0.40.1", "the tree"
	this = withCounts(release{version: this.version, source: this.source, rows: this.rows[:len(this.rows)-2]}, 126800, 8247)
	full := fullTable(v0400(), this).String()
	if strings.Contains(full, "**") {
		t.Errorf("full table has a bold cell:\n%s", full)
	}
	want := `|                      | 0.40.0  | 0.40.1  |
| -------------------- | ------- | ------- |
| Lines of Code: Go    | 126,766 | 126,800 |
| Lines of Code: Other | 8,247   | 8,247   |

Every other value is 0.40.0's.
`
	if got := changelogTable(v0400(), this).String(); got != want {
		t.Errorf("CHANGELOG table:\n%s\nwant:\n%s", got, want)
	}
}

// notes is a release-notes draft holding the full table.
func notes(table string) string {
	return "**New features.** Ranges.\n\n**Other notes.** Twenty-two changes.\n\n" + table +
		"\n`CHANGELOG.md` has every entry.\n\n**Assets.** The bundle.\n"
}

func TestCheckTableAcceptsTheSameTable(t *testing.T) {
	if err := checkTable("NOTES.md", notes(full0410), fullTable(v0400(), v0410()), v0400(), v0410()); err != nil {
		t.Fatal(err)
	}
}

func TestCheckTableStacksTheDifferingValues(t *testing.T) {
	tbl := fullTable(v0400(), v0410())
	for _, tc := range []struct{ old, new, want string }{
		{"| audit                      | 3       | **4**   |", "| audit                      | 3       | 3       |",
			"NOTES.md: audit differs\n  NOTES.md  3\n  the tree  **4**"},
		{"| `golang.org/x/sys`         | v0.49.0 | v0.49.0 |", "| `golang.org/x/sys`         | v0.48.0 | v0.49.0 |",
			"NOTES.md: golang.org/x/sys differs\n  NOTES.md       v0.48.0\n  karvi-v0.40.0  v0.49.0"},
		{"| audit                      | 3       | **4**   |", "| audit | 3 | **4** |",
			"NOTES.md: audit's line differs\n  NOTES.md         | audit | 3 | **4** |\n  release-numbers  | audit                      | 3       | **4**   |"},
		{"| metrics                    | 1       | 1       |\n", "",
			"NOTES.md: the table's line 16 differs\n  NOTES.md         | daemon IPC                 | 12      | 12      |\n  release-numbers  | metrics                    | 1       | 1       |"},
		{"| Lines of Code: Other       | 8,247   | 9,694   |\n", "",
			"NOTES.md: the table's line 21 differs\n  NOTES.md         (none)\n  release-numbers  | Lines of Code: Other       | 8,247   | 9,694   |"},
		{"| Lines of Code: Other       | 8,247   | 9,694   |\n", "| Lines of Code: Other       | 8,247   | 9,694   |\n| Lines of Code: Docs        | 1       | 2       |\n",
			"NOTES.md: the table's line 22 differs\n  NOTES.md         | Lines of Code: Docs        | 1       | 2       |\n  release-numbers  (none)"},
		{"|                            | 0.40.0  | 0.41.0  |", "|                            | 0.39.0  | 0.41.0  |",
			"NOTES.md: no table headed 0.40.0 and 0.41.0"},
	} {
		doc := strings.Replace(notes(full0410), tc.old, tc.new, 1)
		if doc == notes(full0410) {
			t.Fatalf("%q is not in the table", tc.old)
		}
		if err := checkTable("NOTES.md", doc, tbl, v0400(), v0410()); errText(err) != tc.want {
			t.Errorf("got:\n%s\nwant:\n%s", errText(err), tc.want)
		}
	}
}

func changelogDoc(block string) string {
	return "# Changelog\n\n## Unreleased\n\n### Other notes\n\n- **A later entry.** Text.\n\n## 0.41.0 - 2026-11-08\n\n" + block +
		"## 0.40.0 - 2026-11-05\n\n" + changelog0410
}

func TestCheckChangelogReadsTheReleasesBlock(t *testing.T) {
	tbl := changelogTable(v0400(), v0410())
	block := "### New features\n\n- **Ranges.** Text.\n\n### Bugs fixed\n\nNone.\n\n### Breaking changes\n\nNone.\n\n" + changelog0410 + "\n"
	for _, tc := range []struct{ doc, want string }{
		{changelogDoc(block), ""},
		{changelogDoc(strings.Replace(block, "| audit                  | 3       | 4       |\n", "", 1)),
			"CHANGELOG.md: the table's line 4 differs\n  CHANGELOG.md     | Lines of Code: Go      | 126,766 | 128,401 |\n  release-numbers  | audit                  | 3       | 4       |"},
		{changelogDoc(strings.Replace(block, "Every other value is 0.40.0's.", "Every other value is unchanged.", 1)),
			"CHANGELOG.md: the line after the table differs\n  CHANGELOG.md     Every other value is unchanged.\n  release-numbers  Every other value is 0.40.0's."},
		{changelogDoc(strings.Replace(block, "\nEvery other value is 0.40.0's.\n", "", 1)),
			"CHANGELOG.md: the line after the table differs\n  CHANGELOG.md     (none)\n  release-numbers  Every other value is 0.40.0's."},
		{changelogDoc("### New features\n\n- **Ranges.** Text.\n\n"),
			"CHANGELOG.md: no table headed 0.40.0 and 0.41.0 in the 0.41.0 block"},
		{strings.Replace(changelogDoc(block), "## 0.41.0 - 2026-11-08", "## 0.41.1 - 2026-11-08", 1),
			"CHANGELOG.md: no 0.41.0 block"},
	} {
		if err := checkChangelog(tc.doc, tbl, v0400(), v0410()); errText(err) != tc.want {
			t.Errorf("got:\n%s\nwant:\n%s", errText(err), tc.want)
		}
	}
}

const manifest0410 = `{
 "version": "0.41.0",
 "configuration_schema_version": 6,
 "configuration_registry_schema_version": 34,
 "command_record_schema_version": 5,
 "daemon_ipc_schema_version": 12,
 "scoreboard_schema_version": 5,
 "audit_schema_version": 4,
 "job_schema_version": 5,
 "execution_plan_schema_version": 17,
 "credential_package_schema_version": 2,
 "plan_report_schema_version": 4,
 "build": {"go_version": "go1.27.3"},
 "source_statistics": {"go_files_outside_vendor": 573, "go_lines_outside_vendor": 128401},
 "vendored_modules": [
  "github.com/yuin/goldmark v1.8.7",
  "golang.org/x/crypto v0.58.0",
  "golang.org/x/sys v0.49.0",
  "golang.org/x/term v0.47.0"
 ]
}`

func TestCheckManifestComparesItsNumbers(t *testing.T) {
	for _, tc := range []struct{ old, new, want string }{
		{"", "", ""},
		{`"audit_schema_version": 4`, `"audit_schema_version": 3`,
			"release/release-manifest.json: audit_schema_version differs\n  release/release-manifest.json  3\n  records.AuditSchemaVersion     4"},
		{` "plan_report_schema_version": 4,` + "\n", "",
			"release/release-manifest.json: plan_report_schema_version differs\n  release/release-manifest.json    (none)\n  records.PlanReportSchemaVersion  4"},
		{`"daemon_ipc_schema_version": 12`, `"daemon_ipc_schema_version": "12"`,
			"release/release-manifest.json: daemon_ipc_schema_version differs\n  release/release-manifest.json  \"12\"\n  internal/ipc.SchemaVersion     12"},
		{`"go1.27.3"`, `"go1.27.2"`,
			"release/release-manifest.json: build.go_version differs\n  release/release-manifest.json  go1.27.2\n  this toolchain                 go1.27.3"},
		{`"golang.org/x/sys v0.49.0"`, `"golang.org/x/sys v0.48.0"`,
			"release/release-manifest.json: vendored_modules' golang.org/x/sys differs\n  release/release-manifest.json  v0.48.0\n  go.mod                         v0.49.0"},
		{`  "golang.org/x/term v0.47.0"`, `  "golang.org/x/term v0.47.0",` + "\n" + `  "example.com/extra v1.0.0"`,
			"release/release-manifest.json: vendored_modules' example.com/extra differs\n  release/release-manifest.json  v1.0.0\n  go.mod                         (none)"},
		{`"go_lines_outside_vendor": 128401`, `"go_lines_outside_vendor": 128400`,
			"release/release-manifest.json: source_statistics.go_lines_outside_vendor differs\n  release/release-manifest.json  128400\n  the tree                       128401"},
	} {
		doc := strings.Replace(manifest0410, tc.old, tc.new, 1)
		if tc.old != "" && doc == manifest0410 {
			t.Fatalf("%q is not in the manifest", tc.old)
		}
		if err := checkManifest([]byte(doc), v0410()); errText(err) != tc.want {
			t.Errorf("got:\n%s\nwant:\n%s", errText(err), tc.want)
		}
	}
}

// mapTree is a tree held in memory, path to contents.
type mapTree map[string]string

func (m mapTree) list(dir string) ([]string, error) {
	var out []string
	for p := range m {
		if path.Dir(p) == dir {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out, nil
}

func (m mapTree) all() ([]string, error) {
	var out []string
	for p := range m {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func (m mapTree) read(p string) ([]byte, error) {
	s, ok := m[p]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(s), nil
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
