// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// NavNode is one entry in website/data/docs-nav-data.json.
type NavNode struct {
	Title   string     `json:"title"`
	Path    string     `json:"path"`
	Hidden  bool       `json:"hidden"`
	Divider bool       `json:"divider"`
	Routes  []*NavNode `json:"routes"`

	Href   string `json:"-"`
	Active bool   `json:"-"`
}

// loadNav reads the navigation tree.
func loadNav(path string) ([]*NavNode, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var nodes []*NavNode
	if err := json.Unmarshal(raw, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// resolveHrefs fills in every node's URL and drops entries whose page is
// missing, so a stale navigation entry cannot produce a dead link.
func resolveHrefs(nodes []*NavNode, contentDir string, havePage func(string) bool) []*NavNode {
	out := make([]*NavNode, 0, len(nodes))
	for _, n := range nodes {
		if n.Divider {
			out = append(out, n)
			continue
		}
		if n.Path != "" {
			if !havePage(n.Path) {
				continue
			}
			n.Href = pageURL(n.Path)
		}
		n.Routes = resolveHrefs(n.Routes, contentDir, havePage)
		// A section with no reachable children and no page of its own is
		// dropped.
		if n.Path == "" && len(n.Routes) == 0 {
			continue
		}
		out = append(out, n)
	}
	return out
}

// walk visits every node depth first.
func walk(nodes []*NavNode, fn func(*NavNode, int)) {
	var rec func([]*NavNode, int)
	rec = func(ns []*NavNode, depth int) {
		for _, n := range ns {
			fn(n, depth)
			rec(n.Routes, depth+1)
		}
	}
	rec(nodes, 0)
}

// leaves returns the navigable pages in document order, which drives the
// previous/next links at the foot of each page.
func leaves(nodes []*NavNode) []*NavNode {
	var out []*NavNode
	walk(nodes, func(n *NavNode, _ int) {
		if n.Href != "" && !n.Hidden {
			out = append(out, n)
		}
	})
	return out
}

// markActive flags every node on the path to the given URL so the sidebar can
// reveal the branch the reader is in.
func markActive(nodes []*NavNode, url string) bool {
	found := false
	for _, n := range nodes {
		if n.Href == url {
			n.Active = true
			found = true
		}
		if markActive(n.Routes, url) {
			n.Active = true
			found = true
		}
	}
	return found
}

// titleFor returns the navigation title for a URL.
func titleFor(nodes []*NavNode, url string) string {
	var title string
	walk(nodes, func(n *NavNode, _ int) {
		if title == "" && n.Href == url {
			title = n.Title
		}
	})
	return title
}

// pageURL maps a navigation path to its site URL.
func pageURL(path string) string {
	if path == "" {
		return "/docs/"
	}
	return "/docs/" + path + "/"
}

// findPage locates the source file for a navigation path. Pages are either
// "<path>.mdx" or "<path>/index.mdx".
func findPage(contentDir, path string) (string, bool) {
	if path == "" {
		path = "index"
	}
	candidates := []string{
		filepath.Join(contentDir, filepath.FromSlash(path)+".mdx"),
		filepath.Join(contentDir, filepath.FromSlash(path), "index.mdx"),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, true
		}
	}
	return "", false
}
