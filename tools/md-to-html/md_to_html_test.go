package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// githubIDs pairs awkward headings with the ids GitHub gave them: its
// Markdown API (POST /markdown, mode "markdown") rendered this list on
// 2026-10-03, and the converter must give the same, so a link written for
// GitHub reaches the same heading in the HTML.
var githubIDs = [][2]string{
	{"Operations guide", "operations-guide"},
	{"16. `NO_COLOR` (2026-10-03)", "16-no_color-2026-10-03"},
	{"3.1 `cisco_iosxe`: Cisco IOS and IOS XE", "31-cisco_iosxe-cisco-ios-and-ios-xe"},
	{"The collection run: `karvi crun`", "the-collection-run-karvi-crun"},
	{"karvi-prune: retention", "karvi-prune-retention"},
	{"1.1 A run's collection: `--cd` on run and command", "11-a-runs-collection---cd-on-run-and-command"},
	{"12. `--cd` and `--fs` for run and command (2026-10-03)", "12---cd-and---fs-for-run-and-command-2026-10-03"},
	{"§5 The evidence — and *emphasis*, **strong**, _under_", "5-the-evidence--and-emphasis-strong-under"},
	{"Ümlaut café & naïve", "ümlaut-café--naïve"},
	{"Two  spaces and a tab\there", "two--spaces-and-a-tabhere"},
	{"A [link](OPERATIONS.md) in a heading", "a-link-in-a-heading"},
	{"Repeated", "repeated"},
	{"Repeated", "repeated-1"},
	{"Repeated 1", "repeated-1-1"},
	{"Repeated", "repeated-2"},
	{"4.1 Official native-enabled build", "41-official-native-enabled-build"},
	{"Numbers: 1,000 devices at 2.06 GiB (50 %)", "numbers-1000-devices-at-206-gib-50-"},
}

func TestHeadingIDsAreGitHubs(t *testing.T) {
	var src strings.Builder
	for _, h := range githubIDs {
		src.WriteString("## " + h[0] + "\n\n")
	}
	p := parse([]byte(src.String()))
	if len(p.headings) != len(githubIDs) {
		t.Fatalf("%d headings parsed, want %d", len(p.headings), len(githubIDs))
	}
	for i, h := range p.headings {
		if h.id != githubIDs[i][1] {
			t.Errorf("%q: id %q, GitHub's %q", githubIDs[i][0], h.id, githubIDs[i][1])
		}
	}
}

func TestLinksNameTheirPages(t *testing.T) {
	for in, want := range map[string]string{
		"OPERATIONS.md":                 "OPERATIONS.html",
		"../docs/SCALE.md#the-width":    "../docs/SCALE.html#the-width",
		"#upgrade":                      "#upgrade",
		"https://example.com/README.md": "https://example.com/README.md",
		"images/karvi.png":              "images/karvi.png",
	} {
		if got := pageHref(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
	p := parse([]byte("# T\n\nSee [`docs/SCALE.md` \"The width\"](docs/SCALE.md#the-width).\n"))
	body, err := p.render(p.root)
	if err != nil || !strings.Contains(body, `href="docs/SCALE.html#the-width"`) || !strings.Contains(body, `<h1 id="t">T <a class="anchor" href="#t"`) {
		t.Fatalf("err=%v body=%s", err, body)
	}
}

// site writes the named files into a directory and returns it.
func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCheckReportsWhatDoesNotResolve(t *testing.T) {
	dir := writeSite(t, map[string]string{
		"index.html": `<a href="docs/A.html#one">a</a> <a href="docs/A.html#two">missing anchor</a>
<a href="docs/B.html">missing page</a> <img src="images/x.png"> <img src="images/gone.png">
<a href="/docs/A.html">absolute</a> <a href="file:///etc/passwd">a file URL</a>
<a href="../outside.html">outside</a> <a href="https://example.com/">fine</a> <a href="mailto:a@b">fine</a>
<a href="README.md">a Markdown file</a> <a href="#top">no such id</a> <h1 id="dup"></h1><h2 id="dup"></h2>`,
		"docs/A.html":  `<h2 id="one">One</h2> <a href="../index.html">home</a> <a href="#one">self</a>`,
		"images/x.png": "png",
	})
	got := strings.Join(check(dir), "\n")
	for _, want := range []string{
		`index.html: href "docs/A.html#two" names no heading of docs/A.html`,
		`index.html: href "docs/B.html" names no file of the site`,
		`index.html: src "images/gone.png" names no file of the site`,
		`index.html: href "/docs/A.html" is not relative`,
		`index.html: href "file:///etc/passwd" is not relative`,
		`index.html: href "../outside.html" leaves the site`,
		`index.html: href "README.md" names no file of the site`,
		`index.html: href "#top" names no heading of index.html`,
		`index.html: id "dup" is given twice`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("not reported: %s", want)
		}
	}
	if n := len(check(dir)); n != 9 {
		t.Errorf("%d problems, want 9:\n%s", n, got)
	}
}

func TestPublishReplacesOnlyItsOwn(t *testing.T) {
	files := site{marker: []byte(markerText), "index.html": []byte(`<a href="index.html">home</a>`)}
	parent := t.TempDir()
	out := filepath.Join(parent, "html")

	// Missing: made.
	if err := publish(out, files); err != nil {
		t.Fatal(err)
	}
	// Ours, with a stale page: replaced whole, the stale page gone.
	os.WriteFile(filepath.Join(out, "stale.html"), []byte("old"), 0o644)
	if err := publish(out, files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "stale.html")); err == nil {
		t.Fatal("a page of the previous build survived")
	}
	// A failed check: the site as it was, no new directory left.
	if err := publish(out, site{marker: []byte(markerText), "index.html": []byte(`<a href="gone.html">x</a>`)}); err == nil || !strings.Contains(err.Error(), "gone.html") {
		t.Fatalf("err=%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "index.html")); string(b) != `<a href="index.html">home</a>` {
		t.Fatalf("the site changed after a failed check: %s", b)
	}
	if entries, _ := os.ReadDir(parent); len(entries) != 1 {
		t.Fatalf("the parent holds %d entries, want html alone", len(entries))
	}
	// Someone else's directory: refused and untouched.
	foreign := filepath.Join(parent, "notes")
	os.MkdirAll(foreign, 0o755)
	os.WriteFile(filepath.Join(foreign, "mine.txt"), []byte("keep"), 0o644)
	if err := publish(foreign, files); err == nil || !strings.Contains(err.Error(), "not made by md-to-html") {
		t.Fatalf("err=%v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(foreign, "mine.txt")); string(b) != "keep" {
		t.Fatal("a foreign directory was changed")
	}
	// A symbolic link: refused.
	link := filepath.Join(parent, "link")
	os.Symlink(out, link)
	if err := publish(link, files); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err=%v", err)
	}
	// Empty: used.
	empty := filepath.Join(parent, "empty")
	os.MkdirAll(empty, 0o700)
	if err := publish(empty, files); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(empty); fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, want 0755", fi.Mode().Perm())
	}
}

// TestTreeConverts converts the tree itself: every Markdown file is in the
// table, and every link and anchor of the site resolves, so a document
// edit that breaks a link fails here.
func TestTreeConverts(t *testing.T) {
	files, err := build(filepath.Join("..", ".."), "test")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "html")
	if err := publish(out, files); err != nil {
		t.Fatal(err)
	}
	index := string(files["index.html"])
	for _, d := range documents {
		href := strings.TrimSuffix(d.path, ".md") + ".html"
		if !strings.Contains(index, `href="`+href+`"`) {
			t.Errorf("the index does not link %s", href)
		}
	}
	for _, want := range []string{`src="images/karvi-viking-fleet-command.png"`, `href="images/index.html"`} {
		if !strings.Contains(index, want) {
			t.Errorf("the index lacks %s", want)
		}
	}
}

func TestTableNamesEveryDocumentOnce(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs"), 0o755)
	os.MkdirAll(filepath.Join(dir, "vendor", "m"), 0o755)
	for _, d := range documents {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(d.path)), 0o755)
		os.WriteFile(filepath.Join(dir, d.path), nil, 0o644)
	}
	os.WriteFile(filepath.Join(dir, "vendor", "m", "README.md"), nil, 0o644)
	if err := sameDocuments(dir); err != nil {
		t.Fatalf("vendor/ is not the documentation: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "docs", "NEW.md"), nil, 0o644)
	os.Remove(filepath.Join(dir, "docs", "SCALE.md"))
	err := sameDocuments(dir)
	if err == nil || !strings.Contains(err.Error(), "docs/NEW.md is in the tree and not in the table") || !strings.Contains(err.Error(), "docs/SCALE.md is listed and not in the tree") {
		t.Fatalf("err=%v", err)
	}
	for _, d := range documents {
		found := false
		for _, g := range groups {
			found = found || g == d.group
		}
		if !found || d.about == "" {
			t.Errorf("%s: group %q or its line is not the index's", d.path, d.group)
		}
	}
}
