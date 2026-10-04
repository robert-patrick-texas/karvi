// Command md-to-html writes karvi's documentation as HTML: every Markdown
// file of the tree outside vendor/ (the table in docs.go) as a page at the
// same path with .html, an index page (index.html) with the README's image
// and opening paragraph and every document by group, the tree's images/,
// and one stylesheet; every directory below the root has an index.html that
// sends the browser to the parent's, so no server lists it. The pages are
// GitHub's Markdown with GitHub's heading ids, so a link written for GitHub
// reaches the same heading here; a link to a document names its page. Every
// link is relative, so the directory reads the same wherever it is copied,
// and no document page needs a script or the network.
//
//	md-to-html [-src DIR] [-out DIR] [-commit ID]
//
// The site is written beside -out and checked there (every relative link a
// file of the site, every anchor a heading of its page), then put in -out's
// place whole. -out is replaced only when it is missing, empty, or this
// tool's (its .md-to-html file); any other directory is refused and left as
// it is, and so is -out when the build or the check fails. The default -out
// is the tree's parent's html/ (make html).
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	src := flag.String("src", ".", "the karvi tree")
	out := flag.String("out", "../html", "the site's directory, replaced whole")
	commit := flag.String("commit", "", "the tree's state, named in each page's footer")
	flag.Parse()
	files, err := build(*src, *commit)
	if err == nil {
		err = publish(*out, files)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "md-to-html:", err)
		os.Exit(1)
	}
	fmt.Printf("md-to-html: %d pages and %d other files in %s\n", countPages(files), len(files)-countPages(files), *out)
}

func countPages(files site) int {
	n := 0
	for name := range files {
		if len(name) > 5 && name[len(name)-5:] == ".html" {
			n++
		}
	}
	return n
}
