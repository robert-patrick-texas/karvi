package cli

// HelpPage is one help text and the command word it is for; Word is empty
// for the top help.
type HelpPage struct {
	Word string
	Text string
}

// HelpPages is every help text of the parser table, in its order: the top
// help first, then each visible top-level word's (a subcommand shares its
// word's text). The manual's pages are these, one each (tools/mangen), so
// a new word is a new page.
func HelpPages() []HelpPage {
	pages := []HelpPage{{Text: topHelp}}
	for _, c := range commandTable {
		if !c.hidden {
			pages = append(pages, HelpPage{Word: c.word, Text: c.help()})
		}
	}
	return pages
}
