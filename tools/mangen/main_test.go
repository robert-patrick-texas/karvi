package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const page = "../../packaging/man/karvi-prune.8"

// TestCommittedPageIsCurrent: the committed page's generated regions are
// what the flag definition gives now (make generate has been run), and the
// hand-written lines are kept byte for byte.
func TestCommittedPageIsCurrent(t *testing.T) {
	b, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	got, err := generate(string(b), pruneRegions())
	if err != nil {
		t.Fatal(err)
	}
	if got != string(b) {
		t.Fatal("packaging/man/karvi-prune.8 is stale; run make generate")
	}
}

// TestGenerateMarkers: a region's text replaces the lines between its
// markers alone; a marker missing, doubled, or out of order is refused.
func TestGenerateMarkers(t *testing.T) {
	regions := []region{{"SYNOPSIS", "syn\n"}, {"OPTIONS", "opt\n"}}
	in := ".TH X 8\n" + begin("SYNOPSIS") + "\nold\n" + end("SYNOPSIS") + "\nhand\n" + begin("OPTIONS") + "\n" + end("OPTIONS") + "\ntail"
	want := ".TH X 8\n" + begin("SYNOPSIS") + "\nsyn\n" + end("SYNOPSIS") + "\nhand\n" + begin("OPTIONS") + "\nopt\n" + end("OPTIONS") + "\ntail"
	if got, err := generate(in, regions); err != nil || got != want {
		t.Fatalf("got %q %v\nwant %q", got, err, want)
	}
	for name, bad := range map[string]string{
		"missing":      ".TH X 8\n" + begin("SYNOPSIS") + "\n" + end("SYNOPSIS") + "\n",
		"doubled":      in + "\n" + begin("OPTIONS") + "\n",
		"out of order": begin("OPTIONS") + "\n" + end("OPTIONS") + "\n" + begin("SYNOPSIS") + "\n" + end("SYNOPSIS") + "\n",
		"end first":    end("SYNOPSIS") + "\n" + begin("SYNOPSIS") + "\n" + begin("OPTIONS") + "\n" + end("OPTIONS") + "\n",
	} {
		if _, err := generate(bad, regions); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestEscape: a hyphen is \-, a backslash \e, and a line beginning with a
// dot or an apostrophe is protected, so a usage string cannot make a
// request.
func TestEscape(t *testing.T) {
	if got := escape(`--a-b \x`); got != `\-\-a\-b \ex` {
		t.Errorf("escape: %q", got)
	}
	for in, want := range map[string]string{".so /etc/x": `\&.so /etc/x` + "\n", "'br": `\&'br` + "\n", "plain": "plain\n"} {
		if got := line(in); got != want {
			t.Errorf("line(%q) = %q, want %q", in, got, want)
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
	pages, _ := filepath.Glob("../../packaging/man/*.[1-8]")
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
