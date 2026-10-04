package main

import (
	"fmt"
	"html"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// attribute is an href, src, or id in the pages this tool writes, whose
// attributes are always double-quoted and escaped.
var attribute = regexp.MustCompile(`\s(href|src|id)="([^"]*)"`)

// check reads every page under dir and reports each link or image that
// does not resolve inside the site: a relative path to a file the site
// holds, its anchor an id on that page. A link must be relative, so the
// site reads the same wherever it is copied; http, https, and mailto
// links are not fetched. An id given twice on a page is reported too.
func check(dir string) []string {
	ids := map[string]map[string]bool{}
	links := map[string][][2]string{}
	var problems []string
	_ = filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		ids[rel] = map[string]bool{}
		for _, m := range attribute.FindAllStringSubmatch(string(b), -1) {
			v := html.UnescapeString(m[2])
			if m[1] == "id" {
				if ids[rel][v] {
					problems = append(problems, fmt.Sprintf("%s: id %q is given twice", rel, v))
				}
				ids[rel][v] = true
				continue
			}
			links[rel] = append(links[rel], [2]string{m[1], v})
		}
		return nil
	})
	for page, ls := range links {
		for _, l := range ls {
			if p := resolve(dir, ids, page, l[0], l[1]); p != "" {
				problems = append(problems, fmt.Sprintf("%s: %s %q %s", page, l[0], l[1], p))
			}
		}
	}
	sort.Strings(problems)
	return problems
}

// resolve says what is wrong with one link of page, or "" when it resolves.
func resolve(dir string, ids map[string]map[string]bool, page, attr, ref string) string {
	u, err := url.Parse(ref)
	switch {
	case err != nil:
		return "does not parse"
	case u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto":
		return ""
	case u.Scheme != "" || u.Host != "" || strings.HasPrefix(u.Path, "/"):
		return "is not relative"
	}
	target := page
	if u.Path != "" {
		target = path.Clean(path.Join(path.Dir(page), u.Path))
		if target == ".." || strings.HasPrefix(target, "../") {
			return "leaves the site"
		}
		if fi, err := os.Stat(filepath.Join(dir, filepath.FromSlash(target))); err != nil || !fi.Mode().IsRegular() {
			return "names no file of the site"
		}
	}
	if u.Fragment == "" {
		if attr == "href" && u.Path == "" && ref != "" {
			return "is an empty anchor"
		}
		return ""
	}
	pageIDs, isPage := ids[target]
	if !isPage {
		return "has an anchor on a file that is not a page"
	}
	if !pageIDs[u.Fragment] {
		return "names no heading of " + target
	}
	return ""
}
