// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// The controller API reference is generated from the OpenAPI (swagger 2.0)
// description emitted alongside the protobuf definitions. Only the pieces shown
// in the rendered reference are decoded.

type swaggerDoc struct {
	Info struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Tags        []swaggerTag                `json:"tags"`
	Paths       map[string]map[string]swaggerOp `json:"paths"`
	Definitions map[string]swaggerSchema    `json:"definitions"`
}

type swaggerTag struct {
	Name string `json:"name"`
}

type swaggerOp struct {
	Tags        []string                   `json:"tags"`
	Summary     string                     `json:"summary"`
	Description string                     `json:"description"`
	OperationID string                     `json:"operationId"`
	Parameters  []swaggerParam             `json:"parameters"`
	Responses   map[string]swaggerResponse `json:"responses"`
	Deprecated  bool                       `json:"deprecated"`
}

type swaggerParam struct {
	Name        string         `json:"name"`
	In          string         `json:"in"`
	Description string         `json:"description"`
	Required    bool           `json:"required"`
	Type        string         `json:"type"`
	Format      string         `json:"format"`
	Schema      *swaggerSchema `json:"schema"`
}

type swaggerResponse struct {
	Description string         `json:"description"`
	Schema      *swaggerSchema `json:"schema"`
}

type swaggerSchema struct {
	Ref                  string                   `json:"$ref"`
	Type                 string                   `json:"type"`
	Format               string                   `json:"format"`
	Title                string                   `json:"title"`
	Description          string                   `json:"description"`
	Properties           map[string]swaggerSchema `json:"properties"`
	Items                *swaggerSchema           `json:"items"`
	Required             []string                 `json:"required"`
	AdditionalProperties *swaggerSchema           `json:"additionalProperties"`
}

// apiService is one group of operations, rendered as a page.
type apiService struct {
	Name      string // display name, e.g. "Scope"
	Slug      string // e.g. "scope-service"
	URL       string // e.g. "/api-docs/scope-service/"
	Endpoints []apiEndpoint
}

type apiEndpoint struct {
	Method      string
	Path        string
	Summary     string
	Description string
	Deprecated  bool
	Parameters  []swaggerParam
	Responses   []apiResponse
}

type apiResponse struct {
	Status      string
	Description string
	Ref         string
}

var camelRe = []*regexp.Regexp{
	regexp.MustCompile(`([a-z0-9])([A-Z])`),
	regexp.MustCompile(`([A-Z]+)([A-Z][a-z])`),
}

// loadAPI reads and groups the swagger description by service tag.
func loadAPI(path string) (*swaggerDoc, []*apiService, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var doc swaggerDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}

	order := []string{}
	byService := map[string]*apiService{}
	for _, t := range doc.Tags {
		s := doc.serviceFor(t.Name)
		if _, ok := byService[s.Slug]; !ok {
			byService[s.Slug] = s
			order = append(order, s.Slug)
		}
	}

	paths := make([]string, 0, len(doc.Paths))
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		ops := doc.Paths[p]
		methods := make([]string, 0, len(ops))
		for m := range ops {
			switch m {
			case "get", "post", "delete", "patch", "put":
				methods = append(methods, m)
			}
		}
		sort.Strings(methods)
		for _, m := range methods {
			op := ops[m]
			slug := doc.serviceFor(strings.Join(op.Tags, ",")).Slug
			s, ok := byService[slug]
			if !ok {
				s = doc.serviceFor(strings.Join(op.Tags, ","))
				byService[slug] = s
				order = append(order, slug)
			}
			s.Endpoints = append(s.Endpoints, apiEndpoint{
				Method:      strings.ToUpper(m),
				Path:        p,
				Summary:     op.Summary,
				Description: op.Description,
				Deprecated:  op.Deprecated,
				Parameters:  op.Parameters,
				Responses:   responses(op.Responses),
			})
		}
	}

	services := make([]*apiService, 0, len(order))
	for _, slug := range order {
		s := byService[slug]
		if len(s.Endpoints) == 0 {
			continue
		}
		sort.Slice(s.Endpoints, func(i, j int) bool {
			if s.Endpoints[i].Path != s.Endpoints[j].Path {
				return s.Endpoints[i].Path < s.Endpoints[j].Path
			}
			return s.Endpoints[i].Method < s.Endpoints[j].Method
		})
		services = append(services, s)
	}
	return &doc, services, nil
}

// serviceFor maps a swagger tag to a service page. Tags look like
// "controller.api.services.v1.ScopeService" and become "/api-docs/scope-service".
func (d *swaggerDoc) serviceFor(tag string) *apiService {
	name := tag
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return serviceFor(name)
}

func serviceFor(name string) *apiService {
	short := strings.TrimSuffix(name, "Service")
	slug := kebab(short) + "-service"
	return &apiService{
		Name: short,
		Slug: slug,
		URL:  "/api-docs/" + slug + "/",
	}
}

func kebab(s string) string {
	for _, re := range camelRe {
		s = re.ReplaceAllString(s, "$1-$2")
	}
	return strings.ToLower(s)
}

func responses(in map[string]swaggerResponse) []apiResponse {
	out := make([]apiResponse, 0, len(in))
	for status, r := range in {
		out = append(out, apiResponse{
			Status:      status,
			Description: r.Description,
			Ref:         refName(r.Schema),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Status < out[j].Status })
	return out
}

// refName returns the definition name referenced by a schema, if any.
func refName(s *swaggerSchema) string {
	if s == nil || s.Ref == "" {
		return ""
	}
	return s.Ref[strings.LastIndex(s.Ref, "/")+1:]
}

// typeName renders a short label for a schema node.
func typeName(s *swaggerSchema) string {
	switch {
	case s == nil:
		return ""
	case s.Ref != "":
		return refName(s)
	case s.Type == "array" && s.Items != nil:
		return "array of " + typeName(s.Items)
	case s.Type != "":
		return s.Type
	default:
		return ""
	}
}

func methodClass(m string) string { return "method-" + strings.ToLower(m) }

// renderAPIIndex renders /api-docs, the list of services.
func renderAPIIndex(doc *swaggerDoc, services []*apiService) string {
	var b strings.Builder
	b.WriteString(`<h1 id="api">Controller API reference</h1>` + "\n")
	b.WriteString(`<p>` + htmlEscape(doc.Info.Title) + `</p>` + "\n")
	b.WriteString(`<p>Every service exposed by the Dumpling controller is listed below. The API is served under <code>/v1</code>.</p>` + "\n")
	b.WriteString(`<table><thead><tr><th>Service</th><th>Operations</th></tr></thead><tbody>` + "\n")
	for _, s := range services {
		fmt.Fprintf(&b, `<tr><td><a href="%s">%s</a></td><td>%d</td></tr>`+"\n", s.URL, htmlEscape(s.Name), len(s.Endpoints))
	}
	b.WriteString(`</tbody></table>` + "\n")
	return b.String()
}

// renderAPIService renders one service page.
func renderAPIService(s *apiService) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<h1 id="%s">%s service</h1>`+"\n", s.Slug, htmlEscape(s.Name))
	fmt.Fprintf(&b, `<p><a href="/api-docs/">Back to all services</a></p>`+"\n")
	for _, e := range s.Endpoints {
		fmt.Fprintf(&b, `<section class="endpoint">`+"\n")
		fmt.Fprintf(&b, `<h2 id="%s">%s</h2>`+"\n", endpointAnchor(e), htmlEscape(firstNonEmpty(e.Summary, e.Method+" "+e.Path)))
		fmt.Fprintf(&b, `<p class="signature"><span class="method %s">%s</span> <code>%s</code></p>`+"\n", methodClass(e.Method), e.Method, htmlEscape(e.Path))
		if e.Deprecated {
			b.WriteString(`<p class="deprecated">Deprecated.</p>` + "\n")
		}
		if e.Description != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", htmlEscape(e.Description))
		}
		if len(e.Parameters) > 0 {
			b.WriteString(`<h3>Parameters</h3>` + "\n")
			b.WriteString(`<table><thead><tr><th>Name</th><th>In</th><th>Type</th><th>Required</th><th>Description</th></tr></thead><tbody>` + "\n")
			for _, p := range e.Parameters {
				typ := p.Type
				if typ == "" {
					typ = typeName(p.Schema)
				}
				req := ""
				if p.Required {
					req = "yes"
				}
				fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`+"\n",
					htmlEscape(p.Name), htmlEscape(p.In), htmlEscape(typ), req, htmlEscape(p.Description))
			}
			b.WriteString(`</tbody></table>` + "\n")
		}
		if len(e.Responses) > 0 {
			b.WriteString(`<h3>Responses</h3>` + "\n")
			b.WriteString(`<table><thead><tr><th>Status</th><th>Description</th><th>Schema</th></tr></thead><tbody>` + "\n")
			for _, r := range e.Responses {
				schema := r.Ref
				if schema != "" {
					schema = fmt.Sprintf(`<a href="/api-docs/definitions/#%s">%s</a>`, schema, htmlEscape(schema))
				}
				fmt.Fprintf(&b, `<tr><td>%s</td><td>%s</td><td>%s</td></tr>`+"\n", htmlEscape(r.Status), htmlEscape(r.Description), schema)
			}
			b.WriteString(`</tbody></table>` + "\n")
		}
		b.WriteString(`</section>` + "\n")
	}
	return b.String()
}

// renderAPIDefinitions renders /api-docs/definitions, the schema reference.
func renderAPIDefinitions(doc *swaggerDoc) string {
	names := make([]string, 0, len(doc.Definitions))
	for n := range doc.Definitions {
		names = append(names, n)
	}
	sort.Strings(names)

	var b strings.Builder
	b.WriteString(`<h1 id="definitions">Schema definitions</h1>` + "\n")
	b.WriteString(`<p>Objects referenced by the controller API.</p>` + "\n")
	for _, n := range names {
		s := doc.Definitions[n]
		fmt.Fprintf(&b, `<h2 id="%s">%s</h2>`+"\n", n, htmlEscape(n))
		if s.Description != "" {
			fmt.Fprintf(&b, "<p>%s</p>\n", htmlEscape(s.Description))
		}
		if len(s.Properties) == 0 {
			fmt.Fprintf(&b, `<p class="muted">Type: %s</p>`+"\n", htmlEscape(firstNonEmpty(s.Type, "object")))
			continue
		}
		required := map[string]bool{}
		for _, r := range s.Required {
			required[r] = true
		}
		props := make([]string, 0, len(s.Properties))
		for p := range s.Properties {
			props = append(props, p)
		}
		sort.Strings(props)
		b.WriteString(`<table><thead><tr><th>Field</th><th>Type</th><th>Required</th><th>Description</th></tr></thead><tbody>` + "\n")
		for _, p := range props {
			req := ""
			if required[p] {
				req = "yes"
			}
			prop := s.Properties[p]
			typ := typeName(&prop)
			if ref := refName(&prop); ref != "" {
				typ = fmt.Sprintf(`<a href="#%s">%s</a>`, ref, htmlEscape(ref))
			} else {
				typ = htmlEscape(typ)
			}
			fmt.Fprintf(&b, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td>%s</td></tr>`+"\n",
				htmlEscape(p), typ, req, htmlEscape(prop.Description))
		}
		b.WriteString(`</tbody></table>` + "\n")
	}
	return b.String()
}

func endpointAnchor(e apiEndpoint) string {
	return slugify(strings.ToLower(e.Method) + " " + e.Path)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
