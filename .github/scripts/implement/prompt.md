# Implementer run

This run is not interactive. Nobody answers questions, and nobody reads your output until the pull request opens. These rules change the `/implement` skill and the rules in AGENTS.md for this run.

## The issue

The workflow saved the issue in `.implement/issue.json` and its parent issue, if it has one, in `.implement/parent.json`. You have no GitHub token, so do not run `gh`. The workflow refused the run if the issue has open sub-issues, so the issue is the spec.

## What you do differently

- If `/implement` tells you to ask about a departure from the spec, pick your recommended answer and build it. Record each departure in the pull request body, as the last section of this file says.
- Do not post a `Brief amendment` comment.
- Do not run the `field-test` skill or the `code-review` skill. The review bots review the pull request.
- Do not run `git`, and do not commit. The workflow commits your change and opens the pull request.
- The AGENTS.md rules about the collaborator check, commits, pull requests, the field test, and the final report do not apply. The performance checks still apply.

## Commands

You can run only `qui-sandbox <command>`. It runs the command at the repo root in a container with no network. In the container, the repo is read-only, except `web/node_modules`. Examples:

- `qui-sandbox go test -count=1 -run TestName ./internal/services/foo/`
- `qui-sandbox go build ./...`
- `qui-sandbox sh -c 'cd web && pnpm lint && pnpm exec tsc -b && pnpm test'`

The container has no golangci-lint and no Postgres binary, and a test cannot write into the repo. You cannot delete a file, so put a temporary probe in an existing `_test.go` file and take it out with Edit when you are done. CI runs the lint and the Postgres tests on the pull request. If a test must write a file, use `t.TempDir()`.

## Files that you cannot change

You cannot change `.github/`, `.claude/`, `docs/agents/`, an `AGENTS.md` or `CLAUDE.md` file, `go.mod`, `go.sum`, `web/package.json`, `web/pnpm-lock.yaml`, or `web/pnpm-workspace.yaml`. The workflow drops the whole change if it touches one of them. If the work needs a new dependency, stop and say so in the pull request body.

## The pull request

End the run with the title and the body as your structured output:

- `title`: a Conventional Commits title of 64 characters or less.
- `body`: the pull request body. Follow the `pr` skill and `.github/pull_request_template.md`. Replace `Fixes # (issue)` with `Closes #<issue number>`. Under "How has this been tested?", write that the run did no live test. Under "AI disclosure", write that Claude Code (claude-opus-5-5) wrote the code in the implementer workflow. Add a last section, `## Departures`. List each departure from the spec in the question format that `/implement` gives, with the answer that you built. If there are none, write "None."

The workflow adds the run link to the body.
