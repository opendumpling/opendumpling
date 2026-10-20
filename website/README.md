# OpenDumpling documentation website

This directory holds the documentation source and the static assets that ship
with it. The site itself is rendered by an in-repo Go program, so no Node.js
toolchain or external hosting service is required.

## Layout

| Path | Purpose |
| --- | --- |
| `content/docs/` | Documentation sources written in MDX. The directory layout mirrors the URLs: `content/docs/concepts/iam.mdx` becomes `/docs/concepts/iam/`. |
| `data/docs-nav-data.json` | The navigation tree. Each entry has a `title` and either a `path` (a page) or `routes` (a nested group). |
| `public/` | Static assets copied verbatim to the root of the output, for example `public/img/logo.svg` → `/img/logo.svg`. |
| `build/` | Generated output. Not checked in. |

## Rendering

From the repository root:

```shell-session
$ make website          # render to website/build
$ make website-start    # render and serve on http://localhost:3000
```

Or call the renderer directly:

```shell-session
$ go run ./internal/website/render build
$ go run ./internal/website/render serve -addr :3000
```

The renderer reads `content/docs`, `data/docs-nav-data.json` and
`internal/gen/controller.swagger.json` (for the API reference) and writes a
complete static site to `website/build`.

## Authoring

Pages are Markdown with a small amount of MDX on top:

- Frontmatter with an optional `page_title` and `description`.
- `<Note>` and `<Tip>` blocks become callouts.
- `<CodeBlockConfig filename="...">` (or `heading="..."`) adds a caption above
  the code block it wraps.
- `<Tabs>` and `<Tab heading="...">` collapse to a bold heading plus the tab
  body; the site is CLI-first and does not ship a hosted UI.
- Raw HTML is passed through, which is how the generated permissions table in
  `content/docs/concepts/security/permissions/resource-table.mdx` is included.

Keep documentation in English.

## Permissions table

`content/docs/concepts/security/permissions/resource-table.mdx` is generated
from the code. Regenerate it with:

```shell-session
$ make perms-table
```
