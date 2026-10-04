package main

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark/ast"
)

//go:embed style.css
var styleCSS []byte

// marker is the file that names a directory as this tool's output, the
// only kind of directory it replaces.
const marker = ".md-to-html"

const markerText = "Made by tools/md-to-html from the karvi tree. The whole directory is\nreplaced at each run; a file added here by hand is removed with it.\n"

// page is one document as the site carries it.
type page struct {
	doc      document
	out      string // the page's path in the site, as the tree's with .html
	title    string
	sections []heading // the level-2 headings, the column's in-page links
	body     template.HTML
}

// site is every file of the output, by its path from the site's root.
type site map[string][]byte

// build converts the tree at src into the site's files. commit names the
// tree's state in each page's footer; empty leaves it out.
func build(src, commit string) (site, error) {
	if err := sameDocuments(src); err != nil {
		return nil, err
	}
	var pages []*page
	var readme parsed
	for _, d := range documents {
		source, err := os.ReadFile(filepath.Join(src, filepath.FromSlash(d.path)))
		if err != nil {
			return nil, err
		}
		p := parse(source)
		body, err := p.render(p.root)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", d.path, err)
		}
		pg := &page{doc: d, out: strings.TrimSuffix(d.path, ".md") + ".html", title: p.title(), body: template.HTML(body)}
		if pg.title == "" {
			return nil, fmt.Errorf("%s: no top-level heading to title its page", d.path)
		}
		for _, h := range p.headings {
			if h.level == 2 {
				pg.sections = append(pg.sections, h)
			}
		}
		pages = append(pages, pg)
		if d.path == "README.md" {
			readme = p
		}
	}
	out := site{marker: []byte(markerText), "style.css": styleCSS}
	for _, pg := range pages {
		b, err := renderPage(pages, pg, commit)
		if err != nil {
			return nil, err
		}
		out[pg.out] = b
	}
	index, err := renderIndex(pages, readme, commit)
	if err != nil {
		return nil, err
	}
	out["index.html"] = index
	out["images/index.html"] = imagesIndex
	if err := addImages(out, src); err != nil {
		return nil, err
	}
	return out, nil
}

// sameDocuments refuses a tree whose Markdown files outside vendor/ and the
// hidden directories are not the table's, so no document is left off the
// index and no entry names a file that is gone.
func sameDocuments(src string) error {
	found := map[string]bool{}
	err := filepath.WalkDir(src, func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		rel = filepath.ToSlash(rel)
		if e.IsDir() && rel != "." && (rel == "vendor" || strings.HasPrefix(e.Name(), ".")) {
			return filepath.SkipDir
		}
		if !e.IsDir() && strings.HasSuffix(rel, ".md") {
			found[rel] = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	var problems []string
	listed := map[string]bool{}
	for _, d := range documents {
		if listed[d.path] {
			problems = append(problems, d.path+" is listed twice")
		}
		listed[d.path] = true
		if !found[d.path] {
			problems = append(problems, d.path+" is listed and not in the tree")
		}
	}
	for f := range found {
		if !listed[f] {
			problems = append(problems, f+" is in the tree and not in the table (tools/md-to-html/docs.go)")
		}
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

// addImages copies the tree's images/ into the site's, which mirrors it.
func addImages(out site, src string) error {
	root := filepath.Join(src, "images")
	return filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
		if errors.Is(err, fs.ErrNotExist) && p == root {
			return nil
		}
		if err != nil || e.IsDir() || !e.Type().IsRegular() {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		b, err := os.ReadFile(p)
		out[filepath.ToSlash(rel)] = b
		return err
	})
}

// relHref is the relative link from the page at from to the site path to,
// both from the site's root, so the site reads the same wherever it is
// copied.
func relHref(from, to string) string {
	r, _ := filepath.Rel(path.Dir(from), to)
	return filepath.ToSlash(r)
}

// navGroup is one group of the left column.
type navGroup struct {
	Name  string
	Items []navItem
}

// navItem is one document in the column; the current page carries its
// sections.
type navItem struct {
	Href     string
	Title    string
	Current  bool
	Sections []heading
}

func nav(pages []*page, from string) []navGroup {
	var out []navGroup
	for _, g := range groups {
		ng := navGroup{Name: g}
		for _, pg := range pages {
			if pg.doc.group != g {
				continue
			}
			it := navItem{Href: relHref(from, pg.out), Title: pg.title, Current: pg.out == from}
			if it.Current {
				it.Sections = pg.sections
			}
			ng.Items = append(ng.Items, it)
		}
		out = append(out, ng)
	}
	return out
}

// pageData is what the page template fills.
type pageData struct {
	Title, Home, Style, Source, Commit, Images string
	Nav                                        []navGroup
	Body                                       template.HTML
}

var funcs = template.FuncMap{
	"id":   func(h heading) string { return h.id },
	"text": func(h heading) string { return h.text },
}

var pageTemplate = template.Must(template.New("page").Funcs(funcs).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="{{.Style}}">
</head>
<body>
{{define "nav"}}<p class="home"><a href="{{.Home}}">karvi</a></p>
{{range .Nav}}<p class="group">{{.Name}}</p>
<ul>
{{range .Items}}<li><a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Title}}</a>{{if .Sections}}
<ul class="sections">
{{range .Sections}}<li><a href="#{{id .}}">{{text .}}</a></li>
{{end}}</ul>{{end}}</li>
{{end}}</ul>
{{end}}{{end}}<details class="nav-narrow">
<summary>Documents</summary>
<nav aria-label="Documents">
{{template "nav" .}}</nav>
</details>
<nav class="nav-wide" aria-label="Documents">
{{template "nav" .}}</nav>
<main>
{{.Body}}
<footer>{{if .Source}}Generated from <code>{{.Source}}</code>{{if .Commit}} at <code>{{.Commit}}</code>{{end}}.{{else}}Generated by <code>tools/md-to-html</code>{{if .Commit}} at <code>{{.Commit}}</code>{{end}}. <a href="{{.Images}}">Images</a>.{{end}}</footer>
</main>
</body>
</html>
`))

func renderPage(pages []*page, pg *page, commit string) ([]byte, error) {
	var b bytes.Buffer
	err := pageTemplate.Execute(&b, pageData{
		Title: pg.title + " — karvi", Home: relHref(pg.out, "index.html"), Style: relHref(pg.out, "style.css"),
		Source: pg.doc.path, Commit: commit, Nav: nav(pages, pg.out), Body: pg.body,
	})
	return b.Bytes(), err
}

// renderIndex is the main page: the README's image, the name, the README's
// opening paragraph, and every document by group with its line.
func renderIndex(pages []*page, readme parsed, commit string) ([]byte, error) {
	var image, intro string
	var afterTitle bool
	for n := readme.root.FirstChild(); n != nil; n = n.NextSibling() {
		switch t := n.(type) {
		case *ast.HTMLBlock:
			if image == "" && !afterTitle {
				image = blockText(t, readme.source)
			}
		case *ast.Heading:
			afterTitle = afterTitle || t.Level == 1
		case *ast.Paragraph:
			if afterTitle && intro == "" {
				s, err := readme.render(t)
				if err != nil {
					return nil, err
				}
				intro = s
			}
		}
	}
	if image == "" || intro == "" {
		return nil, errors.New("README.md: the index takes its image and its opening paragraph, and one is missing")
	}
	var b strings.Builder
	b.WriteString(`<div class="banner">` + image + "</div>\n")
	b.WriteString("<h1 id=\"karvi\">karvi</h1>\n" + intro)
	s := slugger{"karvi": 0}
	for _, g := range groups {
		fmt.Fprintf(&b, "<h2 id=\"%s\">%s</h2>\n<dl class=\"documents\">\n", s.slug(g), template.HTMLEscapeString(g))
		for _, pg := range pages {
			if pg.doc.group == g {
				fmt.Fprintf(&b, "<dt><a href=\"%s\">%s</a></dt>\n<dd>%s</dd>\n", pg.out, template.HTMLEscapeString(pg.title), inlineCode(pg.doc.about))
			}
		}
		b.WriteString("</dl>\n")
	}
	var out bytes.Buffer
	err := pageTemplate.Execute(&out, pageData{
		Title: "karvi documentation", Home: "index.html", Style: "style.css", Commit: commit,
		Images: "images/index.html", Nav: nav(pages, "index.html"), Body: template.HTML(b.String()),
	})
	return out.Bytes(), err
}

// blockText is a raw HTML block's lines as written.
func blockText(n *ast.HTMLBlock, source []byte) string {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		s := n.Lines().At(i)
		b.Write(s.Value(source))
	}
	return strings.TrimSpace(b.String())
}

var codeSpan = regexp.MustCompile("`([^`]+)`")

// inlineCode escapes an index line and renders its code spans.
func inlineCode(s string) string {
	return codeSpan.ReplaceAllString(template.HTMLEscapeString(s), "<code>$1</code>")
}

// imagesIndex stands in images/ so a server shows it and not a listing.
var imagesIndex = []byte(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>karvi documentation images</title>
<link rel="stylesheet" href="../style.css">
</head>
<body class="plain">
<main>
<p>The images of <a href="../index.html">karvi's documentation</a>.</p>
</main>
</body>
</html>
`)
