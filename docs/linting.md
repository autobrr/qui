# Linting Notes

Internal reference for lint policy and interpretation. Read this when lint output is unclear, when deciding whether to fix or report a lint issue, or when changing lint-related tooling. The actual lint configuration and tool output remain the source of truth.

## Commands

- `make precommit`: fmt + gofix on changed files, then `make lint`.
- `make lint`: golangci-lint on Go issues that are new since the `develop` merge-base, then the full `pnpm lint`. A frontend lint error in any file fails it, not only an error in a changed file.
- `make lint-json`: write lint output to `lint-report.json`.
- `make fmt`: gofmt + frontend eslint fix on changed files.
- `make gofix-changed`: apply `go fix` on changed Go files only.
- `make gofix-check-changed`: check `go fix` drift on changed Go files only.

Frontend lint covers `web/src`, the config files at the web root except `web/vite.config.ts`, and `web/scripts/**/*.mjs`. The scripts run under Node, so they get the recommended rules and Node globals, not the React or stylistic rules.

## Linter Intent

The project uses golangci-lint v2. `.golangci.yml` lists the enabled linters and their settings.

## Version Pin

CI pins the golangci-lint version in `.github/workflows/lint.yml`. A local golangci-lint must read the export data that your Go toolchain writes. golangci-lint v2.13.0 reads export data up to version 4, and Go 1.27.2 writes version 5. With that pair, lint reports false `typecheck` errors in packages that compile, and these errors can hide real issues.

Bump the CI pin in its own PR. Run `make lint-full` first, because `make lint` reports only new issues, and a new golangci-lint version can find issues in old code.

## Prove a New Rule

To prove that a new forbidigo or depguard rule fires, add a temporary violation and lint the package.

- Put the violation on its own line. golangci-lint keeps one issue per line, so a hit from another linter on the same line hides the hit that you want.
- If a forbid pattern must cover each entry point of a package, read the full output of `go doc -short <pkg>`. Do not filter it with a word. A filter misses entry points with other names.
- Use an inline `//nolint:<linter> // reason` where the line can carry one. A `source:` exclusion in `.golangci.yml` also exempts each exact copy of the line in the same file. Keep `source:` exclusions for lines that cannot carry a nolint.

## Policy

- Prefer `make precommit` during implementation for fast feedback.
- If a lint finding is outside task scope or appears to be existing unrelated debt, report it instead of broadening the change.
