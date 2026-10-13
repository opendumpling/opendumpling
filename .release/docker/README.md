# OpenDumpling Docker Image

The root of this repository contains the Dockerfile that builds the
OpenDumpling container image from source. The image is published to
`ghcr.io/opendumpling/opendumpling`.

## Build

See the Makefile targets in the root of this repository for building
OpenDumpling container images:

  - `make docker-build` — build for the current platform from source
  - `make docker-multiarch-build` — build `linux/amd64` and `linux/arm64`
  - `make docker-build-dev` — build a local dev-tagged image

Releases are published automatically from the `Release` GitHub Action when a
`v*` tag is pushed; no manual Docker Hub configuration is needed.