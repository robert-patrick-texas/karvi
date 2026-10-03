package cli

import "testing"

// TestHelpPagesAreThePageSet: the top help, then one text per visible
// top-level word in the table's order; every subcommand shares its word's
// text, so a word's page holds all of its subcommands.
func TestHelpPagesAreThePageSet(t *testing.T) {
	pages := HelpPages()
	if pages[0].Word != "" || pages[0].Text != topHelp {
		t.Fatalf("first page: %q", pages[0].Word)
	}
	n := 1
	for _, c := range commandTable {
		if c.hidden {
			continue
		}
		if pages[n].Word != c.word || pages[n].Text != c.help() {
			t.Errorf("page %d: %q, want %q", n, pages[n].Word, c.word)
		}
		for _, s := range c.subs {
			if s.help() != c.help() {
				t.Errorf("%s has a text of its own; it would need a page", s.path)
			}
		}
		n++
	}
	if n != len(pages) {
		t.Errorf("%d pages, %d words", len(pages), n)
	}
}
