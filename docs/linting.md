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

## Policy

- Prefer `make precommit` during implementation for fast feedback.
- If a lint finding is outside task scope or appears to be existing unrelated debt, report it instead of broadening the change.
