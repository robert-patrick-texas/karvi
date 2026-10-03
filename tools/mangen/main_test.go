package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const src = "../../packaging/man"

// TestCommittedPagesAreCurrent: every page of the table is in
// packaging/man, its generated regions are what the definitions give now
// (make generate has been run), and its hand-written lines are kept byte
// for byte.
func TestCommittedPagesAreCurrent(t *testing.T) {
	out, err := render(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range pages() {
		b, err := os.ReadFile(filepath.Join(src, p.file))
		if err != nil {
			t.Fatal(err)
		}
		if out[p.file] != string(b) {
			t.Errorf("packaging/man/%s is stale; run make generate", p.file)
		}
	}
}

// TestPageTable: prune's page, the top page, and one page per command word
// with its two regions, the top page with the exit statuses as well; a
// word without its page is refused by name.
func TestPageTable(t *testing.T) {
	ps := pages()
	if ps[0].file != "karvi-prune.8" || ps[1].file != "karvi.1" || len(ps[1].regions) != 3 || ps[1].regions[2].name != "EXIT STATUS" {
		t.Fatalf("table head: %+v", ps[:2])
	}
	words := map[string]bool{}
	for _, p := range ps[2:] {
		words[p.file] = true
		if len(p.regions) != 2 {
			t.Errorf("%s: %d regions", p.file, len(p.regions))
		}
	}
	for _, w := range []string{"login", "command", "run", "crun", "stream", "daemon", "job", "config", "setup", "watch", "version"} {
		if !words["karvi-"+w+".1"] {
			t.Errorf("no page for %s", w)
		}
	}
	dir := t.TempDir()
	for _, p := range ps {
		b, err := os.ReadFile(filepath.Join(src, p.file))
		if err != nil {
			t.Fatal(err)
		}
		if p.file != "karvi-watch.1" {
			if err := os.WriteFile(filepath.Join(dir, p.file), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := render(dir); err == nil || !strings.Contains(err.Error(), "karvi-watch.1 does not exist") {
		t.Errorf("a missing page: %v", err)
	}
}

// TestGenerateMarkers: a region's text replaces the lines between its
// markers alone; a marker missing, doubled, or out of order is refused.
func TestGenerateMarkers(t *testing.T) {
	syn, opt := region{"SYNOPSIS", "x", "syn\n"}, region{"OPTIONS", "x", "opt\n"}
	regions := []region{syn, opt}
	in := ".TH X 8\n" + begin(syn) + "\nold\n" + end(syn) + "\nhand\n" + begin(opt) + "\n" + end(opt) + "\ntail"
	want := ".TH X 8\n" + begin(syn) + "\nsyn\n" + end(syn) + "\nhand\n" + begin(opt) + "\nopt\n" + end(opt) + "\ntail"
	if got, err := generate(in, regions); err != nil || got != want {
		t.Fatalf("got %q %v\nwant %q", got, err, want)
	}
	for name, bad := range map[string]string{
		"missing":      ".TH X 8\n" + begin(syn) + "\n" + end(syn) + "\n",
		"doubled":      in + "\n" + begin(opt) + "\n",
		"out of order": begin(opt) + "\n" + end(opt) + "\n" + begin(syn) + "\n" + end(syn) + "\n",
		"end first":    end(syn) + "\n" + begin(syn) + "\n" + begin(opt) + "\n" + end(opt) + "\n",
		"other source": strings.ReplaceAll(in, "from x;", "from y;"),
	} {
		if _, err := generate(bad, regions); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestPageLints: groff reads every page in packaging/man without a
// warning, the hand-written ones as well as the generated ones.
func TestPageLints(t *testing.T) {
	groff, err := exec.LookPath("groff")
	if err != nil {
		t.Skip("groff is not on the path; the pages are not linted")
	}
	pages, _ := filepath.Glob(src + "/*.[1-8]")
	if len(pages) == 0 {
		t.Fatal("no page in packaging/man")
	}
	for _, p := range pages {
		out, err := exec.Command(groff, "-man", "-Tutf8", "-ww", "-z", p).CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != "" {
			t.Errorf("groff %s: %v\n%s", filepath.Base(p), err, out)
		}
	}
}
