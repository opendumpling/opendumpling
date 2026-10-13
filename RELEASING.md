# Releasing OpenDumpling

Releases are intentionally simple: there is no external release pipeline to set
up. Everything is driven by [`.github/workflows/release.yml`](./.github/workflows/release.yml)
and the automatically provided `GITHUB_TOKEN`.

## Cut a release

1. Make sure the version in [`version/VERSION`](./version/VERSION) is what you
   want, and that `main` is green.
2. Create and push a tag:

   ```shell
   git tag -a v0.13.1 -m "OpenDumpling v0.13.1"
   git push origin v0.13.1
   ```

3. That's it. The workflow will:

   * cross-compile `dumpling` for the full OS/architecture matrix;
   * package each binary as `dumpling_<version>_<os>_<arch>.zip`;
   * generate a `dumpling_<version>_SHA256SUMS` checksum file;
   * create (or update) the GitHub Release for the tag and upload the archives;
   * build and push a multi-arch image (`linux/amd64`, `linux/arm64`) to
     `ghcr.io/opendumpling/opendumpling:<version>` and `:latest`.

You can also run the workflow manually from the **Actions** tab
(`workflow_dispatch`) to do a dry run without publishing a tag.

## Asset matrix

The release assets mirror upstream HashiCorp Boundary v0.13.1:

| OS | Architectures |
| --- | --- |
| darwin | `amd64`, `arm64` |
| linux | `386`, `amd64`, `arm`, `arm64` |
| freebsd | `386`, `amd64`, `arm` |
| netbsd | `386`, `amd64`, `arm` |
| openbsd | `386`, `amd64`, `arm` |
| solaris | `amd64` |
| windows | `386`, `amd64` |

## Container image permissions

The image is pushed to the GitHub Container Registry with the workflow's
`GITHUB_TOKEN` (`packages: write`). If the package is created under a private
repository you may need to grant the repository access to the package in
*Organization settings → Packages* the first time.

## Local builds

```shell
make build                 # ./bin/dumpling for the current platform
make docker-build          # local container image
make docker-multiarch-build
```
