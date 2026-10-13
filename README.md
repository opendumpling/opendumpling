<h1>
  <img src="./.github/assets/logo-opendumpling.svg" align="left" height="46px" alt="OpenDumpling logo"/>
  <span>OpenDumpling</span>
</h1>

[![License: MPL-2.0](https://img.shields.io/badge/License-MPL--2.0-brightgreen.svg)](LICENSE)

OpenDumpling provides simple and secure access to hosts and services.

> ### About this project
>
> OpenDumpling is an **independent community fork of HashiCorp Boundary**, based
> on **Boundary v0.13.1** — the last release that HashiCorp published under the
> Mozilla Public License 2.0 (MPL-2.0). The fork exists to keep that
> MPL-licensed codebase available and maintained as open source.
>
> OpenDumpling is **not affiliated with, endorsed by, or sponsored by
> HashiCorp**. "HashiCorp" and "Boundary" are trademarks of HashiCorp, Inc. and
> are used here only to identify and credit the upstream project. See
> [NOTICE.md](./NOTICE.md).
>
> The command-line binary is named **`dumpling`**.

OpenDumpling provides several key features:

* **Simple and secure access** — Users connect to hosts and critical systems
  through a single, authenticated entry point. No credentials are distributed
  to end users, and the private network is never exposed.

* **Identity-based authorization** — Access is granted per user and per session
  through a flexibly scoped domain model, rather than at the network layer.

* **Session recording** — Sessions can be recorded for audit, playback and
  retention. Storage backends are pluggable.

* **Dynamic credentials** — Credential brokering integrates with external
  secret stores so the credential used to reach a target is never shown to the
  user.

* **Scalable architecture** — Controllers and workers can be scaled
  independently. Control plane and data plane can run in separate trust
  domains.

* **Database-backed** — OpenDumpling uses PostgreSQL for the control plane and
  KMS for encryption.

## Getting started

### Build from source

Building requires [Go](https://go.dev/dl/) (the toolchain version is pinned in
[`.go-version`](./.go-version)) and, optionally, [Buf](https://buf.build/) for
regenerating protobuf bindings.

```shell
git clone https://github.com/opendumpling/opendumpling.git
cd opendumpling
make build          # produces ./bin/dumpling
./bin/dumpling version
```

### Try it locally

OpenDumpling ships a single-node dev mode that provisions an in-memory
PostgreSQL-compatible database and starts a controller, a worker and the CLI:

```shell
./bin/dumpling dev
```

### Container image

A multi-arch image is published to the GitHub Container Registry:

```shell
docker run --rm ghcr.io/opendumpling/opendumpling:latest version
```

## Releases

Every tagged release (`v*`) is built by GitHub Actions. Each release publishes
the same OS/architecture matrix that upstream Boundary v0.13.1 offered:

| OS | Architectures |
| --- | --- |
| macOS (darwin) | `amd64`, `arm64` |
| Linux | `386`, `amd64`, `arm`, `arm64` |
| FreeBSD | `386`, `amd64`, `arm` |
| NetBSD | `386`, `amd64`, `arm` |
| OpenBSD | `386`, `amd64`, `arm` |
| Solaris | `amd64` |
| Windows | `386`, `amd64` |

Assets are named `dumpling_<version>_<os>_<arch>.zip` and are accompanied by a
`SHA256SUMS` checksum file. See [RELEASING.md](./RELEASING.md) for details.

## Configuration

Configuration is provided through a config file and/or environment variables.
Environment variables use the `DUMPLING_` prefix, for example:

```shell
export DUMPLING_ADDR=https://127.0.0.1:9200
export DUMPLING_TOKEN=at_1234567890
dumpling authenticate
```

The full configuration reference is in the [documentation](./website/content/docs).

## Security

We take security seriously. If you believe you have found a security issue,
please disclose it responsibly — see [SECURITY.md](./SECURITY.md). Please do not
open a public issue for security problems.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md).

## License

OpenDumpling is licensed under the [Mozilla Public License 2.0](./LICENSE).
It is derived from HashiCorp Boundary v0.13.1 (MPL-2.0). See
[NOTICE.md](./NOTICE.md) for attribution and trademarks.
