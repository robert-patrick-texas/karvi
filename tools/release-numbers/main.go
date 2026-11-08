// Command release-numbers prints a release's numbers table and checks the
// release's documents against it. The table holds every value, in the same
// order at each release, the previous release's beside this one's: Go, each
// module go.mod requires (the indirect ones included), the twelve schema
// counters, each from its owner's constant (the list in numbers.go, the one
// place it is written), and two counts: "Lines of Code: Go", every line of
// the tracked .go files outside vendor/, tests included, and "Lines of Code:
// Other", every line of the other tracked programs outside vendor/ (a file
// whose first line starts "#!", or a file named Makefile).
//
//	release-numbers [-previous TAG] [-changelog | -check [NOTES]]
//
// This release is VERSION, its values read from the working tree and Go
// from the toolchain running the tool; the previous release is the highest
// karvi-vMAJOR.MINOR.PATCH tag below it, or -previous TAG, its values read
// from the tag through git and Go from its release manifest. With no flag
// the full table is printed, for the release notes and BUILD-RESULT.md: a
// moved version or counter is bold in this release's column, the two counts
// never. -changelog prints the CHANGELOG block's table, the rows that moved
// and the two counts, then "Every other value is X's.". A module one
// release lacks is "—" there, its row moved; each column is as wide as its
// widest cell, with a space on each side.
//
// -check compares, for the release VERSION names, CHANGELOG.md's block of
// that version (its table and the line after it equal -changelog's),
// release/BUILD-RESULT.md's table (equal to the full one), and
// release/release-manifest.json's counters, build.go_version,
// vendored_modules, and source_statistics.go_lines_outside_vendor; NOTES, a
// release-notes draft, adds its table. The first difference stops it: exit
// 1, the row named and the two values stacked. Exit 0 names what it
// compared; exit 2 on usage or on what it cannot read. It needs git and the
// tags, so it runs in the release tree, not in a bundle.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release-numbers", flag.ContinueOnError)
	fs.SetOutput(stderr)
	previous := fs.String("previous", "", "the previous release's `TAG` (default: the highest karvi-v tag below VERSION)")
	changelog := fs.Bool("changelog", false, "print the CHANGELOG block's table: what moved, and the two counts")
	check := fs.Bool("check", false, "compare the release's documents, and NOTES when given, with the tree")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: release-numbers [-previous TAG] [-changelog | -check [NOTES]]")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if (*changelog && *check) || fs.NArg() > 1 || (fs.NArg() == 1 && !*check) {
		fs.Usage()
		return 2
	}
	root, prev, this, err := load(*previous)
	if err != nil {
		fmt.Fprintln(stderr, "release-numbers:", err)
		return 2
	}
	switch {
	case *changelog:
		fmt.Fprint(stdout, changelogTable(prev, this))
	case *check:
		docs, err := checkDocuments(root, fs.Arg(0), prev, this)
		if err != nil {
			fmt.Fprintln(stderr, "release-numbers:", err)
			var d *difference
			if errors.As(err, &d) {
				return 1
			}
			return 2
		}
		fmt.Fprintf(stdout, "release-numbers: %s agree with the tree and %s\n", docs, prev.source)
	default:
		fmt.Fprint(stdout, fullTable(prev, this))
	}
	return 0
}

// load reads the two releases: this one from the working tree, the
// previous one from its tag.
func load(previous string) (root string, prev, this release, err error) {
	out, err := git(".", "rev-parse", "--show-toplevel")
	if err != nil {
		return "", prev, this, err
	}
	root = strings.TrimSpace(string(out))
	data, err := os.ReadFile(filepath.Join(root, "VERSION"))
	if err != nil {
		return "", prev, this, err
	}
	version := strings.TrimSpace(string(data))
	if previous == "" {
		out, err := git(root, "tag", "--list", "karvi-v*")
		if err != nil {
			return "", prev, this, err
		}
		if previous, err = previousTag(strings.Fields(string(out)), version); err != nil {
			return "", prev, this, err
		}
	} else if _, err := git(root, "rev-parse", "--verify", "--quiet", "refs/tags/"+previous); err != nil {
		return "", prev, this, fmt.Errorf("no tag %s", previous)
	}
	tag := &tagTree{root: root, tag: previous}
	defer tag.close()
	goVersion, err := manifestGo(tag)
	if err != nil {
		return "", prev, this, err
	}
	if prev, err = readRelease(tag, strings.TrimPrefix(previous, "karvi-v"), previous, goVersion); err != nil {
		return "", prev, this, fmt.Errorf("%s: %w", previous, err)
	}
	if this, err = readRelease(diskTree{root: root}, version, "the tree", strings.TrimPrefix(runtime.Version(), "go")); err != nil {
		return "", prev, this, fmt.Errorf("the tree: %w", err)
	}
	return root, prev, this, nil
}

// checkDocuments compares each of the release's documents, and the notes
// draft when one is named, with the two releases, and names what it
// compared.
func checkDocuments(root, notes string, prev, this release) (string, error) {
	read := func(name string) (string, error) {
		data, err := os.ReadFile(filepath.Join(root, name))
		return string(data), err
	}
	text, err := read("CHANGELOG.md")
	if err != nil {
		return "", err
	}
	if err := checkChangelog(text, changelogTable(prev, this), prev, this); err != nil {
		return "", err
	}
	full := fullTable(prev, this)
	if text, err = read(buildResult); err != nil {
		return "", err
	}
	if err := checkTable(buildResult, text, full, prev, this); err != nil {
		return "", err
	}
	if text, err = read(manifest); err != nil {
		return "", err
	}
	if err := checkManifest([]byte(text), this); err != nil {
		return "", err
	}
	docs := fmt.Sprintf("CHANGELOG.md's %s block, %s, and %s", this.version, buildResult, manifest)
	if notes != "" {
		data, err := os.ReadFile(notes)
		if err != nil {
			return "", err
		}
		if err := checkTable(notes, string(data), full, prev, this); err != nil {
			return "", err
		}
		docs = fmt.Sprintf("CHANGELOG.md's %s block, %s, %s, and %s", this.version, buildResult, manifest, notes)
	}
	return docs, nil
}
