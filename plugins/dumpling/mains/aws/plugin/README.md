# AWS host and storage plugin

This package is vendored from `github.com/hashicorp/boundary-plugin-aws` v0.1.6
so that OpenDumpling builds without depending on a HashiCorp repository. It is
unchanged apart from its import paths, which now point at this repository's own
`sdk/` module, and a guard that makes the Terraform-based end to end test
explicitly opt-in.

It remains licensed under the Mozilla Public License 2.0; see [LICENSE](./LICENSE).
The `Copyright (c) HashiCorp, Inc.` headers in the source are retained.
