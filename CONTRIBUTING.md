# Contributing to OpenDumpling

Thanks for your interest in contributing to OpenDumpling! This is a community
fork of HashiCorp Boundary v0.13.1 (MPL-2.0).

## Code of conduct

Be respectful and inclusive. We expect contributors to use welcoming and
gender-neutral language and to treat others with empathy. Harassment or
disrespectful behaviour is not tolerated.

## Reporting issues

### Security vulnerabilities

Please **do not** open a public issue for security problems. See
[SECURITY.md](./SECURITY.md) for how to report them privately.

### Bugs

Before opening a bug report:

1. Build from the latest `main` to check whether the issue is already fixed.
2. Search the existing issues to avoid duplicates.

Then open a bug report using the issue template.

## Pull requests

* For non-trivial features, please open an issue first so we can align on scope
  and approach.
* Keep changes focused. One logical change per pull request.
* Add a changelog entry (see below) when the change is user-facing.
* Make sure the build passes: `make build` and, for the packages you touched,
  `go test ./...`.

## Changelog

User-facing changes should be accompanied by a changelog entry. Follow the
existing patterns in [CHANGELOG.md](./CHANGELOG.md) for referencing pull
requests and issues.

## Testing

Most tests require PostgreSQL, provided through Docker:

```shell
make test-database-up   # start the test database
make test               # run the full suite
make test-database-down # stop the test database
```

The default test container name is `dumpling-sql-tests`. You can run individual
tests directly, for example:

```shell
go test -run TestAuthTokenAuthenticator -v ./internal/auth
```

The SDK and API modules can be tested without a database:

```shell
make test-api
make test-sdk
```

## Additional docs

* [Adding fields to the API](internal/adding-a-new-field-readme.md)
