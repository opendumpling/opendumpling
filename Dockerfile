# syntax=docker/dockerfile:1
# Copyright (c) HashiCorp, Inc.
# SPDX-License-Identifier: MPL-2.0
# Copyright (c) The OpenDumpling Authors

# OpenDumpling container image.
#
# Build for the local platform:
#   docker build -t opendumpling .
#
# Build and push a multi-arch image to GHCR:
#   docker buildx build \
#     --platform linux/amd64,linux/arm64 \
#     --build-arg VERSION=0.13.1 \
#     -t ghcr.io/opendumpling/opendumpling:0.13.1 \
#     --push .

# ---------------------------------------------------------------------------
# Build stage
# ---------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build

# TARGETOS/TARGETARCH are provided automatically by BuildKit when --platform
# is used, and used here to cross-compile a static binary.
ARG TARGETOS TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown

WORKDIR /src

# The repository is split into several replace-based modules (api, sdk), so the
# whole tree is needed.
COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=${TARGETOS:-linux} \
    GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -tags=dumpling \
      -ldflags "-X github.com/opendumpling/opendumpling/version.GitCommit=${GIT_COMMIT} -X github.com/opendumpling/opendumpling/version.Version=${VERSION} -X github.com/opendumpling/opendumpling/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /out/dumpling ./cmd/dumpling

# ---------------------------------------------------------------------------
# Runtime stage
# ---------------------------------------------------------------------------
FROM alpine:3.20 AS release

ARG VERSION=dev

LABEL org.opencontainers.image.authors="OpenDumpling contributors" \
      org.opencontainers.image.url="https://github.com/opendumpling/opendumpling" \
      org.opencontainers.image.documentation="https://github.com/opendumpling/opendumpling#readme" \
      org.opencontainers.image.source="https://github.com/opendumpling/opendumpling" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.vendor="OpenDumpling" \
      org.opencontainers.image.title="dumpling" \
      org.opencontainers.image.description="OpenDumpling provides simple and secure access to hosts and services" \
      org.opencontainers.image.licenses="MPL-2.0" \
      name="OpenDumpling" \
      summary="OpenDumpling provides simple and secure access to hosts and services"

# libc6-compat is needed to symlink the shared libraries for ARM builds.
RUN apk add --no-cache \
        ca-certificates \
        dumb-init \
        libc6-compat \
        libcap \
        openssl \
        su-exec \
        tzdata \
 && addgroup -S dumpling \
 && adduser -S -G dumpling -h /home/dumpling -s /sbin/nologin dumpling

COPY --from=build /out/dumpling /bin/dumpling

# /dumpling holds the default configuration. Mount a volume elsewhere for any
# persistent state; the default config expects an external Postgres database.
RUN mkdir -p /dumpling \
 && chown -R dumpling:dumpling /dumpling

COPY .release/docker/config.hcl /dumpling/config.hcl
RUN chown -R dumpling:dumpling /dumpling \
 && chmod -R 640 /dumpling/*

# Set up nsswitch.conf for Go's "netgo" resolver, otherwise DNS supersedes the
# container's hosts file, which we don't want.
RUN test -e /etc/nsswitch.conf || echo 'hosts: files dns' > /etc/nsswitch.conf

# Controller API, cluster, and proxy listener ports.
EXPOSE 9200 9201 9202
VOLUME /dumpling

# OpenDumpling doesn't need root, so the entrypoint drops privileges to the
# "dumpling" user, and uses dumb-init as PID 1 to reap zombie processes.
COPY .release/docker/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENTRYPOINT ["docker-entrypoint.sh"]

CMD ["server", "-config", "/dumpling/config.hcl"]
