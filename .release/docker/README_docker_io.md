<!---
This is used for the Overview tab for the OpenDumpling container image on GHCR.
Image: ghcr.io/opendumpling/opendumpling
--->

# OpenDumpling

OpenDumpling provides simple and secure access to hosts and services. It is an
independent community fork of HashiCorp Boundary v0.13.1 (MPL-2.0). The command
line binary is named `dumpling`.

## Usage

See the latest Dockerfile for this image in our
[GitHub repository](https://github.com/opendumpling/opendumpling).

### Dev Mode

Due to the limitations of `dumpling dev` running and maintaining a postgres
docker container, it's not recommended to run `dev` mode inside docker.
To do so will require knowledge of running
[docker-in-docker](https://hub.docker.com/_/docker),
and the caveats associated with it.

### Default Configuration

The default behavior of the OpenDumpling container image is to run
`dumpling server -config /dumpling/config.hcl`.
The included `config.hcl` file is meant to serve as an example,
and is not suitable for actual deployment.
Please see the comments within the file for more information;
full configuration details can be found in the
[repository documentation](https://github.com/opendumpling/opendumpling).

### Postgres

The usage instructions in this README assume you have an external postgres
database (version 12 or greater) to run `dumpling server` with.
If you want to get started quickly, you can start a local postgres in docker:

```bash
docker run -it -p 5432:5432 -e POSTGRES_PASSWORD=postgres -e POSTGRES_USER=postgres postgres
```

The postgres URL setting is defined with `env://DUMPLING_POSTGRES_URL` so it can
be easily set with `-e` during `docker run`:

```bash
docker run \
    --network host
    -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable'
    ghcr.io/opendumpling/opendumpling
```

### Database Init

If you're starting with a new,
unused postgres instance,
initialize the database using the default `config.hcl`:

```bash
docker run \
  --network host \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  ghcr.io/opendumpling/opendumpling \
  dumpling database init -config /dumpling/config.hcl
```

If you want to run this with your own `config.hcl` (assuming `config.hcl` is
located at `$(pwd)/config.hcl`):

```bash
docker run \
  --network host \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  -v "$(pwd)":/dumpling/ \
  ghcr.io/opendumpling/opendumpling \
  dumpling database init -config /dumpling/config.hcl
```

### Database Migration

If you are updating to a newer version of dumpling with a database instance
that was initialized with an older version,
you will need to apply the database migrations:

```bash
docker run \
  --network host \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  ghcr.io/opendumpling/opendumpling \
  dumpling database migrate -config /dumpling/config.hcl
```

If you want to run this with your own `config.hcl` (assuming `config.hcl` is
located at `$(pwd)/config.hcl`):

```bash
docker run \
  --network host \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  -v "$(pwd)":/dumpling/ \
  ghcr.io/opendumpling/opendumpling \
  dumpling database migrate -config /dumpling/config.hcl
```

### Server

Start an OpenDumpling server using the default `config.hcl`:

```bash
docker run \
  --network host \
  -p 9200:9200 \
  -p 9201:9201 \
  -p 9202:9202 \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  ghcr.io/opendumpling/opendumpling
```

Start an OpenDumpling server using your own `config.hcl`, assuming it's located
at `$(pwd)/config.hcl`:

```bash
docker run \
  --network host \
  -p 9200:9200 \
  -p 9201:9201 \
  -p 9202:9202 \
  -v "$(pwd)":/dumpling/ \
  -e 'DUMPLING_POSTGRES_URL=postgresql://postgres:postgres@0.0.0.0:5432/postgres?sslmode=disable' \
  ghcr.io/opendumpling/opendumpling
```