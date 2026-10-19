# NOTICE

OpenDumpling is an independent community fork of **HashiCorp Boundary**.

* Upstream project: HashiCorp Boundary
* Upstream source: <https://github.com/hashicorp/boundary>
* Forked from release: **v0.13.1** (tag `v0.13.1`)
* Upstream copyright: Copyright (c) 2020 HashiCorp, Inc.
* Upstream license: Mozilla Public License 2.0 (MPL-2.0)

HashiCorp Boundary v0.13.1 is the last release that HashiCorp published under
the Mozilla Public License 2.0. Starting with v0.14.0, HashiCorp moved the
Boundary source code to the Business Source License (BUSL-1.1). OpenDumpling is
based on the MPL-2.0 v0.13.1 codebase so that the rights granted by that license
are preserved.

## Vendored upstream code

Two components were vendored into this repository so that OpenDumpling builds
entirely from its own source, with no dependency on a HashiCorp repository:

* `plugins/dumpling/mains/aws` — originally
  `github.com/hashicorp/boundary-plugin-aws` v0.1.6
* `plugins/dumpling/mains/azure` — originally
  `github.com/hashicorp/boundary-plugin-azure` v0.1.3

Both were MPL-2.0 and remain so. Their per-file `Copyright (c) HashiCorp, Inc.`
headers are retained, as are their own `LICENSE` texts under
`plugins/dumpling/mains/{aws,azure}/plugin/`. The vendored packages speak the
plugin protocol through this repository's own `sdk/` module rather than
`github.com/hashicorp/boundary/sdk`.

```text
Copyright (c) 2021 HashiCorp, Inc.
```

## Trademarks

OpenDumpling is **not affiliated with, endorsed by, or sponsored by
HashiCorp, Inc.**

"HashiCorp" and "Boundary" are trademarks of HashiCorp, Inc. They are used in
this repository only to identify and credit the upstream project from which
OpenDumpling is derived.

## What changed in this fork

* The product and command-line binary are named **`dumpling`**.
* The Go module path is `github.com/opendumpling/opendumpling`.
* Environment variables use the `DUMPLING_` prefix (for example
  `DUMPLING_ADDR`, `DUMPLING_TOKEN`, `DUMPLING_POSTGRES_URL`).
* The container image is published to
  `ghcr.io/opendumpling/opendumpling`.
* The upstream web admin UI (`hashicorp/boundary-ui`, licensed under
  BUSL-1.1) is **not** included; OpenDumpling ships the CLI and the
  controller/worker server only.
* Per-file `Copyright (c) HashiCorp, Inc.` headers are retained to preserve the
  provenance of the MPL-2.0 code.

## License

Except as noted below, the OpenDumpling source code is licensed under the
Mozilla Public License 2.0. See [LICENSE](./LICENSE).

```text
Copyright (c) 2020 HashiCorp, Inc.
Copyright (c) The OpenDumpling Authors
```

Third-party dependencies remain under their own licenses; see `go.mod`,
`go.sum` and the vendored license texts of those projects.
