// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import "strings"

// Frontmatter is the small subset of YAML frontmatter the documentation uses.
//
// Every page in website/content starts with a block delimited by "---" lines
// holding layout, page_title and description. Only those keys are read; the
// values are either plain scalars or the "|"/">" block scalars, so a full YAML
// parser is not warranted.
type Frontmatter struct {
	Layout      string
	PageTitle   string
	Description string
}

// splitFrontmatter separates the leading frontmatter block from the body. If
// there is no frontmatter the whole input is returned as the body.
func splitFrontmatter(src string) (Frontmatter, string) {
	src = strings.ReplaceAll(src, "\r\n", "\n")
	var fm Frontmatter

	if !strings.HasPrefix(src, "---\n") {
		return fm, src
	}
	end := strings.Index(src[4:], "\n---")
	if end < 0 {
		return fm, src
	}
	block := src[4 : 4+end]
	body := src[4+end+4:]
	body = strings.TrimPrefix(body, "\n")

	lines := strings.Split(block, "\n")
	for i := 0; i < len(lines); i++ {
		key, val, ok := splitKey(lines[i])
		if !ok {
			continue
		}
		// Block scalars ("|", "|-", ">", ">-") collect the indented lines
		// that follow.
		if isBlockScalar(val) {
			folded := strings.HasPrefix(val, ">")
			var parts []string
			for i+1 < len(lines) && (strings.HasPrefix(lines[i+1], "  ") || strings.TrimSpace(lines[i+1]) == "") {
				i++
				parts = append(parts, strings.TrimSpace(lines[i]))
			}
			val = strings.TrimSpace(joinBlock(parts, folded))
		} else {
			val = unquote(strings.TrimSpace(val))
		}

		switch key {
		case "layout":
			fm.Layout = val
		case "page_title":
			fm.PageTitle = val
		case "description":
			fm.Description = val
		}
	}
	return fm, body
}

func splitKey(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" || strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	return key, line[idx+1:], true
}

func isBlockScalar(v string) bool {
	v = strings.TrimSpace(v)
	return v == "|" || v == "|-" || v == "|+" || v == ">" || v == ">-" || v == ">+"
}

// joinBlock reassembles a block scalar. A folded scalar (">") turns single
// newlines into spaces and blank lines into paragraph breaks; a literal scalar
// ("|") keeps newlines as written.
func joinBlock(parts []string, folded bool) string {
	if !folded {
		return strings.Join(parts, "\n")
	}
	var b strings.Builder
	for i, p := range parts {
		switch {
		case i == 0:
			b.WriteString(p)
		case p == "":
			b.WriteString("\n")
		case parts[i-1] == "":
			b.WriteString(p)
		default:
			b.WriteString(" ")
			b.WriteString(p)
		}
	}
	return b.String()
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
