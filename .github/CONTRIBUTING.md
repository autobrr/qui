# Contributing to qui

Thanks for taking interest in contribution! We welcome anyone who wants to contribute.

If you have an idea for a bigger feature or a change then we are happy to discuss it before you start working on it.
It is usually a good idea to make sure it aligns with the project and is a good fit.
Open an issue or post in #dev-general on [Discord](https://discord.autobrr.com/qui).

This document is a guide to help you through the process of contributing to qui.

## Become a contributor

* Code: new features, bug fixes, improvements
* Report bugs

## Developer guide

This guide helps you get started developing qui.

## Dependencies

Make sure you have the following dependencies installed before setting up your developer environment:

- [Git](https://git-scm.com/)
- [Go](https://golang.org/dl/) 1.25 or later (see [go.mod](go.mod#L3) for exact version)
- [Node.js](https://nodejs.org) (we usually use the latest Node LTS version - for further information see `@types/node` major version in [package.json](web/package.json))
- [pnpm](https://pnpm.io/installation)
- [prek](https://github.com/prefix-dev/prek) (optional, for pre-commit hooks)

## How to contribute

- **Fork and Clone:** [Fork the qui repository](https://github.com/autobrr/qui/fork) and clone it to start working on your changes.
- **Branching:** Create a new branch for your changes. Use a descriptive name for easy understanding.
  - Checkout a new branch for your fix or feature `git checkout -b fix/torrent-actions-issue`
- **Coding:** Ensure your code is well-commented for clarity. With go use `go fmt`
- **Commit Guidelines:** We appreciate the use of [Conventional Commit Guidelines](https://www.conventionalcommits.org/en/v1.0.0/#summary) when writing your commits.
  - Examples: `fix(qbittorrent): improve connection pooling`, `feat(torrent): add bulk actions`
  - There is no need for force pushing or rebasing. We squash commits on merge to keep the history clean and manageable.
- **Pull Requests:** Submit a pull request from your Fork with a clear description of your changes. Reference any related issues.
  - Mark it as Draft if it's still in progress.
- **Code Review:** Be open to feedback during the code review process.

## Development environment

The backend is written in Go and the frontend is written in TypeScript using React.

You need to have the Go toolchain installed and Node.js with `pnpm` as the package manager.

Clone the project and change dir:

```shell
git clone github.com/YOURNAME/qui && cd qui
```

## Frontend

First install the web dependencies:

```shell
cd web && pnpm install
```

Run the project:

```shell
pnpm dev
```

This should make the frontend available at [http://localhost:5173](http://localhost:5173). It's setup to communicate with the API at [http://localhost:7476](http://localhost:7476).

### Build

In order to build binaries of the full application you need to first build the frontend.

To build the frontend, run:

```shell
pnpm --dir web run build
```

## Backend

Install Go dependencies:

```shell
go mod tidy
```

Run the project:

```shell
go run cmd/qui/main.go
```

This uses the default `config.toml` and runs the API on [http://localhost:7476](http://localhost:7476).

### Build

To build the backend, run:

```shell
make backend
```

This will output a binary named `qui` in the current directory

You can also build the frontend and the backend at once with:

```shell
make build
```

### Build cross-platform binaries

You can optionally build it with [GoReleaser](https://goreleaser.com/) which makes it easy to build cross-platform binaries.

Install it with `go install` or check the [docs for alternatives](https://goreleaser.com/install/):

```shell
go install github.com/goreleaser/goreleaser/v2@latest
```

Then to build binaries, run:

```shell
goreleaser build --snapshot --clean
```

## Tests

The test suite consists of only backend tests at this point. All tests run per commit with GitHub Actions.

### Run backend tests

We have a mix of unit and integration tests.

Run all non-integration tests:

```shell
go test -v ./...
```
