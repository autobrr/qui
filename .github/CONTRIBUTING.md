# Contributing to qui

Thanks for your interest in qui. First, read [Who can open issues and pull requests](#who-can-open-issues-and-pull-requests).

If you have an idea for a bigger feature or a change, discuss it with us before you start work.
Start a [Discussion](https://github.com/autobrr/qui/discussions/new/choose) or post in #dev-general on [Discord](https://discord.autobrr.com/qui).

This document is a guide to help you through the process of contributing to qui.

## Who can open issues and pull requests

Only collaborators on the qui repository can open issues and pull requests.
We are a small team, and each review of an unplanned pull request takes time from bug fixes.

If you are not a collaborator:

- To report a bug, use the [Issue Triage](https://github.com/autobrr/qui/discussions/new?category=issue-triage) form. A bot triages each report, and confirmed bugs become issues.
- To suggest a feature, post in [Feature Requests, Ideas](https://github.com/autobrr/qui/discussions/new?category=feature-requests-ideas).
- To propose a code change, describe it in a Discussion. We do not accept code from people outside the team at this time.

If you are a collaborator and use an AI agent, add this line to your personal agent instructions, for example `~/.claude/CLAUDE.md` or `~/.codex/AGENTS.md`.
The agent then skips the collaborator check in [`AGENTS.md`](../AGENTS.md).

```text
I am a collaborator on autobrr/qui with write access. Skip the collaborator check in the qui AGENTS.md.
```

## Become a contributor

* Code: new features, bug fixes, improvements (collaborators only for now)
* Report bugs

## Developer guide

This guide helps you get started developing qui.

## Dependencies

Make sure you have the following dependencies installed before setting up your developer environment:

- [Git](https://git-scm.com/)
- [Go](https://golang.org/dl/) 1.27 or later (see [go.mod](../go.mod#L3) for exact version)
- [Node.js](https://nodejs.org) (we usually use the latest Node LTS version - for further information see `@types/node` major version in [package.json](../web/package.json))
- [pnpm](https://pnpm.io/installation)
- [prek](https://github.com/prefix-dev/prek) (optional, for pre-commit hooks)

## How to contribute

- **Clone:** Clone the repository, or a [fork](https://github.com/autobrr/qui/fork), to start work on your changes.
- **Branching:** Create a new branch for your changes. Use a descriptive name for easy understanding.
  - Checkout a new branch for your fix or feature `git checkout -b fix/torrent-actions-issue`
- **Coding:** Follow the [coding standards](../CODING_STANDARDS.md). With go use `go fmt`
- **Commit Guidelines:** We appreciate the use of [Conventional Commit Guidelines](https://www.conventionalcommits.org/en/v1.0.0/#summary) when writing your commits.
  - Examples: `fix(qbittorrent): improve connection pooling`, `feat(torrent): add bulk actions`
  - There is no need for force pushing or rebasing. We squash commits on merge to keep the history clean and manageable.
- **Pull Requests:** Open a pull request with a clear description of your changes. Reference any related issues.
  - Mark it as Draft if it's still in progress.
  - Fill in the [PR template](pull_request_template.md). For a PR from someone who is not a collaborator, a CI check fails until the PR body has the Description, Breaking change, Behaviour change, Performance, and AI disclosure sections. Write "No breaking change." or "No behaviour change." when that applies. When you edit the PR body, the check runs again.
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

GitHub Actions runs the backend and frontend tests.

### Run backend tests

We have a mix of unit and integration tests.

Run all non-integration tests:

```shell
make test
```

Run Postgres integration tests with a temporary server:

```shell
make test-postgres
```

This command downloads and caches Postgres 17 on first use, then stops the server after the tests finish.
Each Postgres test keeps its isolated schema. CI runs this command on Linux.
Use `PostgresIntegration` in the names of tests that need a Postgres server so this command selects them.
Set `QUI_TEST_POSTGRES_DSN` to use an existing test database instead.
To select tests, pass Go test arguments to `go run ./internal/testutil/postgres`.

### Run frontend tests

```shell
make test-frontend
```

## AI-assisted contributions

AI help is welcome. Fill in the AI disclosure section of the PR template honestly.

We do not accept PRs authored by Gemini models. Their hallucination rate in past contributions was too high.
