// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Scopes":                                      "scopes",
		"`tcp` listener":                              "tcp-listener",
		"Custom API response headers":                 "custom-api-response-headers",
		"LDAP auth method attributes <sup>Beta</sup>": "ldap-auth-method-attributes-beta",
		"Multi-hop workers":                           "multi-hop-workers",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestServiceSlug(t *testing.T) {
	cases := map[string]string{
		"controller.api.services.v1.SessionRecordingService": "session-recording-service",
		"controller.api.services.v1.ScopeService":            "scope-service",
		"controller.api.services.v1.HostCatalogService":      "host-catalog-service",
	}
	d := &swaggerDoc{}
	for in, want := range cases {
		got := d.serviceFor(in)
		if got.Slug != want {
			t.Errorf("serviceFor(%q).Slug = %q, want %q", in, got.Slug, want)
		}
		if got.URL != "/api-docs/"+want+"/" {
			t.Errorf("serviceFor(%q).URL = %q", in, got.URL)
		}
	}
}

func TestPageURL(t *testing.T) {
	cases := map[string]string{
		"":                     "/docs/",
		"concepts/iam":         "/docs/concepts/iam/",
		"configuration/worker": "/docs/configuration/worker/",
	}
	for in, want := range cases {
		if got := pageURL(in); got != want {
			t.Errorf("pageURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderMarkdownCallout(t *testing.T) {
	res, err := renderMarkdown("## Heading\n\n<Note>\nSome **text**.\n</Note>\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := `<div class="callout callout-note" role="note">`; !strings.Contains(res.HTML, want) {
		t.Errorf("callout wrapper missing:\n%s", res.HTML)
	}
	if want := "<strong>text</strong>"; !strings.Contains(res.HTML, want) {
		t.Errorf("callout body was not rendered as Markdown:\n%s", res.HTML)
	}
	if len(res.Headings) != 1 || res.Headings[0].ID != "heading" {
		t.Errorf("unexpected headings: %+v", res.Headings)
	}
}

func TestRenderMarkdownCodeCaption(t *testing.T) {
	res, err := renderMarkdown("<CodeBlockConfig filename=\"/etc/dumpling.d/worker.hcl\">\n\n```hcl\nkey = \"value\"\n```\n\n</CodeBlockConfig>\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := `<div class="code-caption">/etc/dumpling.d/worker.hcl</div>`; !strings.Contains(res.HTML, want) {
		t.Errorf("code caption missing:\n%s", res.HTML)
	}
	if strings.Contains(res.HTML, "CodeBlockConfig") {
		t.Errorf("wrapper was not removed:\n%s", res.HTML)
	}
}

func TestRenderMarkdownAttributesInCodeFence(t *testing.T) {
	res, err := renderMarkdown("```text\n<Expression 1>\n```\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.HTML, "&lt;Expression 1&gt;") {
		t.Errorf("code fence contents were altered:\n%s", res.HTML)
	}
}

func TestRenderMarkdownRawHTMLTable(t *testing.T) {
	res, err := renderMarkdown("<table><tr><td>a</td></tr></table>\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.HTML, "<table>") {
		t.Errorf("raw HTML was escaped:\n%s", res.HTML)
	}
}

func TestRenderMarkdownInternalLinkTrailingSlash(t *testing.T) {
	res, err := renderMarkdown("See [scopes](/docs/concepts/domain-model/scopes) and [releases](https://example.com/x).\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.HTML, `href="/docs/concepts/domain-model/scopes/"`) {
		t.Errorf("internal link missing trailing slash:\n%s", res.HTML)
	}
	if !strings.Contains(res.HTML, `rel="noopener noreferrer"`) {
		t.Errorf("external link missing rel:\n%s", res.HTML)
	}
}

func TestRenderMarkdownDuplicateHeadingIDs(t *testing.T) {
	res, err := renderMarkdown("## Overview\n\n## Overview\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Headings) != 2 {
		t.Fatalf("got %d headings", len(res.Headings))
	}
	if res.Headings[0].ID == res.Headings[1].ID {
		t.Errorf("duplicate heading IDs: %q", res.Headings[0].ID)
	}
}

func TestSplitFrontmatter(t *testing.T) {
	fm, body := splitFrontmatter("---\npage_title: Hello\ndescription: |-\n  A thing.\n---\n\n# Hi\n")
	if fm.PageTitle != "Hello" {
		t.Errorf("PageTitle = %q", fm.PageTitle)
	}
	if fm.Description != "A thing." {
		t.Errorf("Description = %q", fm.Description)
	}
	if strings.TrimSpace(body) != "# Hi" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterAbsent(t *testing.T) {
	fm, body := splitFrontmatter("# Hi\n")
	if fm.PageTitle != "" || fm.Description != "" {
		t.Errorf("unexpected frontmatter: %+v", fm)
	}
	if body != "# Hi\n" {
		t.Errorf("body = %q", body)
	}
}
