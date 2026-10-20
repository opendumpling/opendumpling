// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"bytes"
	stdhtml "html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// Heading is a document heading, used to build the table of contents.
type Heading struct {
	Level int
	ID    string
	Text  string
}

// rendered holds the result of converting one MDX document to HTML.
type rendered struct {
	HTML     string
	Headings []Heading
}

var (
	openCalloutRe  = regexp.MustCompile(`<(Note|Tip)>\s*`)
	closeCalloutRe = regexp.MustCompile(`\s*</(Note|Tip)>`)
	openTagRe      = regexp.MustCompile(`<([A-Z][A-Za-z]*)((?:\s[^<>]*)?)>`)
	closeTagRe     = regexp.MustCompile(`</([A-Z][A-Za-z]*)>`)
	attrRe         = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_-]*)\s*=\s*"([^"]*)"`)
	htmlTagRe      = regexp.MustCompile(`<[^>]+>`)
	nonSlugRe      = regexp.MustCompile(`[^\p{L}\p{N}_\s-]+`)
	spaceRe        = regexp.MustCompile(`\s+`)
	dashRe         = regexp.MustCompile(`-+`)
)

// markdown is the shared goldmark instance. Raw HTML is passed through so the
// hand-written tables in the content are preserved, and the table, strikethrough
// and typographer extensions cover the Markdown the docs actually use.
var markdown = goldmark.New(
	goldmark.WithExtensions(
		extension.Table,
		extension.Strikethrough,
	),
	goldmark.WithParserOptions(
		parser.WithAttribute(),
	),
	goldmark.WithRendererOptions(
		ghhtml.WithUnsafe(),
	),
)

// renderMarkdown converts an MDX body to HTML and collects its headings.
func renderMarkdown(body string) (rendered, error) {
	src := []byte(preprocess(body))

	doc := markdown.Parser().Parse(text.NewReader(src))
	assignHeadingIDs(doc, src)

	var buf bytes.Buffer
	if err := markdown.Renderer().Render(&buf, src, doc); err != nil {
		return rendered{}, err
	}

	return rendered{
		HTML:     postprocess(buf.String()),
		Headings: collectHeadings(doc, src),
	}, nil
}

// preprocess translates the MDX-specific constructs into plain Markdown or raw
// HTML that goldmark understands.
func preprocess(body string) string {
	// Callouts become a wrapping div. Goldmark treats a bare HTML block as
	// ending at the next blank line, so force one after the opening tag and
	// before the closing tag; that keeps the inner text parsed as Markdown.
	body = openCalloutRe.ReplaceAllStringFunc(body, func(m string) string {
		kind := strings.ToLower(openCalloutRe.FindStringSubmatch(m)[1])
		return `<div class="callout callout-` + kind + `" role="note">` + "\n\n"
	})
	body = closeCalloutRe.ReplaceAllString(body, "\n\n</div>")

	body = openTagRe.ReplaceAllStringFunc(body, func(m string) string {
		sub := openTagRe.FindStringSubmatch(m)
		name, attrs := sub[1], sub[2]
		switch name {
		case "CodeBlockConfig":
			// Keep the code, drop the wrapper, but surface the filename or
			// heading as a caption so the information is not lost.
			a := parseAttrs(attrs)
			label := a["filename"]
			if label == "" {
				label = a["heading"]
			}
			if label == "" {
				return "\n\n"
			}
			return `<div class="code-caption">` + htmlEscape(label) + "</div>\n\n"
		case "Tabs":
			return "\n\n"
		case "Tab":
			a := parseAttrs(attrs)
			if h := a["heading"]; h != "" {
				return "\n\n<p><strong>" + htmlEscape(h) + "</strong></p>\n\n"
			}
			return "\n\n"
		default:
			return m
		}
	})
	body = closeTagRe.ReplaceAllStringFunc(body, func(m string) string {
		switch closeTagRe.FindStringSubmatch(m)[1] {
		case "CodeBlockConfig", "Tabs", "Tab":
			return "\n\n"
		default:
			return m
		}
	})
	return body
}

// postprocess rewrites the generated HTML: internal page links gain a trailing
// slash so they resolve to the directory index on any static host, and external
// links open in a new tab.
func postprocess(html string) string {
	html = pageHrefRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := pageHrefRe.FindStringSubmatch(m)
		href := sub[1]
		if strings.HasSuffix(href, "/") || strings.Contains(lastSegment(href), ".") {
			return m
		}
		return `href="` + href + `/"`
	})
	html = extHrefRe.ReplaceAllString(html, `href="$1" target="_blank" rel="noopener noreferrer"`)
	return html
}

var (
	pageHrefRe = regexp.MustCompile(`href="(/(?:docs|api-docs)(?:/[^"#]*)?)"`)
	extHrefRe  = regexp.MustCompile(`href="(https?://[^"]+)"`)
)

func lastSegment(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[i+1:]
	}
	return path
}

func parseAttrs(s string) map[string]string {
	out := map[string]string{}
	for _, m := range attrRe.FindAllStringSubmatch(s, -1) {
		out[m[1]] = m[2]
	}
	return out
}

func htmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

// assignHeadingIDs gives every heading a stable anchor, matching the slug rules
// the documentation links rely on.
func assignHeadingIDs(doc ast.Node, src []byte) {
	seen := map[string]int{}
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		id := slugify(string(h.Text(src)))
		if id == "" {
			return ast.WalkContinue, nil
		}
		if c := seen[id]; c > 0 {
			id = id + "-" + itoa(c)
		}
		seen[id]++
		h.SetAttributeString("id", []byte(id))
		return ast.WalkContinue, nil
	})
}

func collectHeadings(doc ast.Node, src []byte) []Heading {
	var out []Heading
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		id, _ := h.AttributeString("id")
		text := string(h.Text(src))
		out = append(out, Heading{
			Level: h.Level,
			ID:    toString(id),
			Text:  strings.TrimSpace(stdhtml.UnescapeString(text)),
		})
		return ast.WalkContinue, nil
	})
	return out
}

// slugify mirrors the anchor generation used when the links were written:
// lowercase, drop punctuation other than word characters, underscore, space and
// hyphen, then turn runs of whitespace into single hyphens.
func slugify(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = strings.ToLower(s)
	s = nonSlugRe.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)
	s = spaceRe.ReplaceAllString(s, "-")
	s = dashRe.ReplaceAllString(s, "-")
	return s
}

func toString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case []byte:
		return string(t)
	case string:
		return t
	default:
		return ""
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
