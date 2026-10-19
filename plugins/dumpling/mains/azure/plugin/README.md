# Azure host plugin

This package is vendored from `github.com/hashicorp/boundary-plugin-azure` v0.1.3
so that OpenDumpling builds without depending on a HashiCorp repository. It is
unchanged apart from its import paths, which now point at this repository's own
`sdk/` module.

It remains licensed under the Mozilla Public License 2.0; see [LICENSE](./LICENSE).
The `Copyright (c) HashiCorp, Inc.` headers in the source are retained.
