// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed templates/*.html static/*.css
var assets embed.FS

// pageData is the template input for one rendered page.
type pageData struct {
	Title       string
	Description string
	Content     template.HTML
	Headings    []Heading
	Nav         []*NavNode
	Trail       []*NavNode
	Prev        *NavNode
	Next        *NavNode
	CurrentURL  string
}

// site holds everything needed to render the documentation.
type site struct {
	root       string
	outDir     string
	contentDir string

	nav     []*NavNode
	tmpl    *template.Template
	pageMap map[string]string   // nav path -> source file
	fileFor map[string]string   // nav path -> source file (same, kept for clarity)
	ordered []*NavNode          // navigation order for prev/next
}

// buildSite renders the documentation site into outDir.
func buildSite(root, outDir, swaggerPath string) error {
	s := &site{
		root:       root,
		outDir:     outDir,
		contentDir: filepath.Join(root, "website", "content", "docs"),
		pageMap:    map[string]string{},
		fileFor:    map[string]string{},
	}

	navPath := filepath.Join(root, "website", "data", "docs-nav-data.json")
	nav, err := loadNav(navPath)
	if err != nil {
		return fmt.Errorf("load nav: %w", err)
	}

	if err := s.indexPages(); err != nil {
		return err
	}

	nav = resolveHrefs(nav, s.contentDir, func(p string) bool {
		_, ok := s.pageMap[p]
		return ok
	})
	s.nav = nav
	s.ordered = leaves(nav)

	if s.tmpl, err = s.parseTemplates(); err != nil {
		return err
	}

	if err := os.RemoveAll(outDir); err != nil {
		return err
	}

	if err := s.renderPages(); err != nil {
		return err
	}
	if err := s.renderAPIDocs(swaggerPath); err != nil {
		return err
	}
	if err := s.copyPublic(); err != nil {
		return err
	}
	if err := s.writeStatic(); err != nil {
		return err
	}
	return s.writeRedirect()
}

// indexPages maps every documentation path to its source file.
func (s *site) indexPages() error {
	return filepath.WalkDir(s.contentDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".mdx") {
			return nil
		}
		rel, err := filepath.Rel(s.contentDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		var key string
		switch {
		case rel == "index.mdx":
			key = ""
		case strings.HasSuffix(rel, "/index.mdx"):
			key = strings.TrimSuffix(rel, "/index.mdx")
		default:
			key = strings.TrimSuffix(rel, ".mdx")
		}
		s.pageMap[key] = p
		s.fileFor[key] = p
		return nil
	})
}

func (s *site) parseTemplates() (*template.Template, error) {
	return template.New("").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).ParseFS(assets, "templates/*.html")
}

// renderPages renders every page found on disk, using the navigation tree for
// titles, ordering and breadcrumbs where available.
func (s *site) renderPages() error {
	keys := make([]string, 0, len(s.pageMap))
	for k := range s.pageMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		if err := s.renderPage(key); err != nil {
			return fmt.Errorf("render %q: %w", key, err)
		}
	}
	return nil
}

func (s *site) renderPage(key string) error {
	src, err := os.ReadFile(s.pageMap[key])
	if err != nil {
		return err
	}
	fm, body := splitFrontmatter(string(src))
	res, err := renderMarkdown(body)
	if err != nil {
		return err
	}

	url := pageURL(key)
	navCopy := copyNav(s.nav)
	markActive(navCopy, url)

	title := fm.PageTitle
	if title == "" {
		title = titleFor(navCopy, url)
	}
	if title == "" {
		title = fallbackTitle(res.Headings)
	}

	data := pageData{
		Title:       title,
		Description: fm.Description,
		Content:     template.HTML(res.HTML), //nolint:gosec // trusted, generated from the repo's own Markdown
		Headings:    tocHeadings(res.Headings),
		Nav:         navCopy,
		Trail:       trailFor(navCopy, url),
		Prev:        s.neighbour(url, -1),
		Next:        s.neighbour(url, +1),
		CurrentURL:  url,
	}
	return s.execPage(key, data)
}

func (s *site) execPage(key string, data pageData) error {
	var b strings.Builder
	if err := s.tmpl.ExecuteTemplate(&b, "layout.html", data); err != nil {
		return err
	}
	out := filepath.Join(s.outDir, "docs", filepath.FromSlash(key), "index.html")
	if key == "" {
		out = filepath.Join(s.outDir, "docs", "index.html")
	}
	return writeFile(out, b.String())
}

// neighbour returns the previous (-1) or next (+1) page in navigation order.
func (s *site) neighbour(url string, delta int) *NavNode {
	for i, n := range s.ordered {
		if n.Href != url {
			continue
		}
		j := i + delta
		if j < 0 || j >= len(s.ordered) {
			return nil
		}
		return s.ordered[j]
	}
	return nil
}

// renderAPIDocs generates the controller API reference from the swagger
// description.
func (s *site) renderAPIDocs(swaggerPath string) error {
	doc, services, err := loadAPI(swaggerPath)
	if err != nil {
		return fmt.Errorf("load swagger: %w", err)
	}

	apiNav := []*NavNode{{
		Title: "API reference",
		Routes: []*NavNode{
			{Title: "Overview", Href: "/api-docs/"},
			{Title: "Definitions", Href: "/api-docs/definitions/"},
		},
	}}
	for _, svc := range services {
		apiNav[0].Routes = append(apiNav[0].Routes, &NavNode{
			Title: svc.Name + " service",
			Href:  svc.URL,
		})
	}

	pages := []struct {
		url   string
		title string
		desc  string
		html  string
		heads []Heading
	}{
		{"/api-docs/", "Controller API reference", "Every service exposed by the Dumpling controller.", renderAPIIndex(doc, services), nil},
		{"/api-docs/definitions/", "Schema definitions", "Objects referenced by the controller API.", renderAPIDefinitions(doc), nil},
	}
	for _, svc := range services {
		pages = append(pages, struct {
			url   string
			title string
			desc  string
			html  string
			heads []Heading
		}{
			svc.URL,
			svc.Name + " service",
			"REST operations for the " + svc.Name + " service.",
			renderAPIService(svc),
			apiHeadings(svc),
		})
	}

	for _, p := range pages {
		nav := copyNav(apiNav)
		markActive(nav, p.url)
		data := pageData{
			Title:       p.title,
			Description: p.desc,
			Content:     template.HTML(p.html), //nolint:gosec // generated from the repo's own swagger description
			Headings:    p.heads,
			Nav:         nav,
			Trail:       trailFor(nav, p.url),
			CurrentURL:  p.url,
		}
		var b strings.Builder
		if err := s.tmpl.ExecuteTemplate(&b, "layout.html", data); err != nil {
			return err
		}
		out := filepath.Join(s.outDir, filepath.FromSlash(strings.TrimPrefix(p.url, "/")), "index.html")
		if err := writeFile(out, b.String()); err != nil {
			return err
		}
	}
	return nil
}

func apiHeadings(svc *apiService) []Heading {
	out := []Heading{{Level: 1, ID: svc.Slug, Text: svc.Name + " service"}}
	for _, e := range svc.Endpoints {
		out = append(out, Heading{
			Level: 2,
			ID:    endpointAnchor(e),
			Text:  firstNonEmpty(e.Summary, e.Method+" "+e.Path),
		})
	}
	return out
}

// copyPublic copies the static assets that ship alongside the documentation.
func (s *site) copyPublic() error {
	return filepath.WalkDir(filepath.Join(s.root, "website", "public"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(filepath.Join(s.root, "website", "public"), p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return writeFile(filepath.Join(s.outDir, rel), string(data))
	})
}

func (s *site) writeStatic() error {
	data, err := assets.ReadFile("static/style.css")
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(s.outDir, "style.css"), string(data))
}

func (s *site) writeRedirect() error {
	page := `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8">
<title>Dumpling documentation</title>
<link rel="canonical" href="/docs/">
<meta http-equiv="refresh" content="0; url=/docs/">
</head><body><p>Continue to the <a href="/docs/">Dumpling documentation</a>.</p></body></html>
`
	return writeFile(filepath.Join(s.outDir, "index.html"), page)
}

// tocHeadings keeps the levels that make a useful table of contents.
func tocHeadings(in []Heading) []Heading {
	out := make([]Heading, 0, len(in))
	for _, h := range in {
		if h.Level >= 2 && h.Level <= 4 && h.ID != "" {
			out = append(out, h)
		}
	}
	return out
}

func fallbackTitle(in []Heading) string {
	for _, h := range in {
		if h.Level == 1 {
			return h.Text
		}
	}
	if len(in) > 0 {
		return in[0].Text
	}
	return "Documentation"
}

// copyNav deep copies the navigation so per-page active flags do not leak.
func copyNav(in []*NavNode) []*NavNode {
	out := make([]*NavNode, 0, len(in))
	for _, n := range in {
		c := *n
		c.Active = false
		c.Routes = copyNav(n.Routes)
		out = append(out, &c)
	}
	return out
}

// trailFor returns the chain of navigation nodes leading to url.
func trailFor(nodes []*NavNode, url string) []*NavNode {
	var rec func([]*NavNode) []*NavNode
	rec = func(ns []*NavNode) []*NavNode {
		for _, n := range ns {
			if n.Href == url {
				return []*NavNode{n}
			}
			if sub := rec(n.Routes); sub != nil {
				return append([]*NavNode{n}, sub...)
			}
		}
		return nil
	}
	return rec(nodes)
}

func writeFile(name, contents string) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return os.WriteFile(name, []byte(contents), 0o644)
}

// cleanURL is a small helper used by tests and the dev server.
func cleanURL(p string) string {
	if p == "" {
		return "/"
	}
	p = path.Clean(p)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}
