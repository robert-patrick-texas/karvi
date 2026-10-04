package main

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// markdown is GitHub's dialect as the documents are written for it: tables,
// strikethrough, task lists, and bare URLs as links, with the tree's raw
// HTML (the README's image) let through. The headings take GitHub's ids,
// a relative link to a document names its page, and each heading carries
// a link to itself.
var markdown = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithASTTransformers(
		util.Prioritized(headingIDs{}, 100),
		util.Prioritized(pageLinks{}, 100),
	)),
	goldmark.WithRendererOptions(
		html.WithUnsafe(),
		renderer.WithNodeRenderers(util.Prioritized(headingRenderer{}, 100)),
	),
)

// heading is one heading of a parsed document: its level, its text as it
// reads, and its id.
type heading struct {
	level int
	text  string
	id    string
}

// parsed is a document parsed once: its tree, its source, and its headings
// in order.
type parsed struct {
	root     ast.Node
	source   []byte
	headings []heading
}

// parse parses a document and collects its headings with the ids the
// transformer gave them.
func parse(source []byte) parsed {
	root := markdown.Parser().Parse(text.NewReader(source))
	p := parsed{root: root, source: source}
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			id, _ := h.AttributeString("id")
			p.headings = append(p.headings, heading{level: h.Level, text: plainText(h, source), id: string(id.([]byte))})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return p
}

// render renders a node of a parsed document.
func (p parsed) render(n ast.Node) (string, error) {
	var b bytes.Buffer
	if err := markdown.Renderer().Render(&b, p.source, n); err != nil {
		return "", err
	}
	return b.String(), nil
}

// title is the document's first top-level heading, as it reads.
func (p parsed) title() string {
	for _, h := range p.headings {
		if h.level == 1 {
			return h.text
		}
	}
	return ""
}

// plainText is a node's text as it reads once rendered: the words of its
// emphasis, code spans, and link texts, without their markup.
func plainText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(source))
			if t.SoftLineBreak() || t.HardLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		case *ast.AutoLink:
			b.Write(t.Label(source))
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

// slugger gives headings GitHub's ids: the heading's text lowercased, every
// character but a letter, a digit, a mark, a hyphen, an underscore, or a
// space removed, each space a hyphen; a repeated id takes -1, -2, and so on,
// counted per first form, as github-slugger counts them.
type slugger map[string]int

func (s slugger) slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(heading)) {
		switch {
		case r == ' ':
			b.WriteByte('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r):
			b.WriteRune(r)
		}
	}
	id, base := b.String(), b.String()
	for {
		if _, used := s[id]; !used {
			break
		}
		s[base]++
		id = fmt.Sprintf("%s-%d", base, s[base])
	}
	s[id] = 0
	return id
}

// headingIDs sets every heading's id from its rendered text, GitHub's way,
// where goldmark's own ids are made from the source line and turn an
// underscore into a hyphen.
type headingIDs struct{}

func (headingIDs) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	s := slugger{}
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			h.SetAttributeString("id", []byte(s.slug(plainText(h, source))))
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
}

// pageLinks makes a relative link to a Markdown file name its page: the
// same path with .html for .md, its anchor kept. Links with a scheme, and
// anchors alone, are left as they are; the check refuses what does not
// resolve.
type pageLinks struct{}

func (pageLinks) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if l, ok := n.(*ast.Link); ok && entering {
			l.Destination = []byte(pageHref(string(l.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

// pageHref is a link's destination with a Markdown file's name made its
// page's.
func pageHref(dest string) string {
	if dest == "" || strings.HasPrefix(dest, "#") || strings.Contains(dest, ":") {
		return dest
	}
	path, anchor, hasAnchor := strings.Cut(dest, "#")
	if strings.HasSuffix(path, ".md") {
		path = strings.TrimSuffix(path, ".md") + ".html"
	}
	if hasAnchor {
		return path + "#" + anchor
	}
	return path
}

// headingRenderer writes a heading with its id and, after its text, a link
// to itself that shows on hover.
type headingRenderer struct{}

func (headingRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindHeading, func(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		h := n.(*ast.Heading)
		id, _ := h.AttributeString("id")
		if entering {
			fmt.Fprintf(w, "<h%d id=\"%s\">", h.Level, util.EscapeHTML(id.([]byte)))
			return ast.WalkContinue, nil
		}
		fmt.Fprintf(w, " <a class=\"anchor\" href=\"#%s\" aria-label=\"Link to this section\">#</a></h%d>\n", util.EscapeHTML(id.([]byte)), h.Level)
		return ast.WalkContinue, nil
	})
}
