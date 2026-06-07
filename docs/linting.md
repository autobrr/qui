# Linting Notes

Internal reference for lint policy and interpretation. Read this when lint output is unclear, when deciding whether to fix or report a lint issue, or when changing lint-related tooling. The actual lint configuration and tool output remain the source of truth.

## Commands

- `make precommit`: fmt + gofix changed files + lint changed files.
- `make lint`: lint changed files only.
- `make lint-json`: write lint output to `lint-report.json`.
- `make fmt`: gofmt + frontend eslint fix on changed files.
- `make gofix-changed`: apply `go fix` on changed Go files only.
- `make gofix-check-changed`: check `go fix` drift on changed Go files only.

## Linter Intent

The project uses golangci-lint v2 with strict settings intended to catch common maintainability issues in generated or hand-written code.

| Linter | Purpose | Threshold |
| --- | --- | --- |
| `dupl` | Catch code duplication | 100 tokens |
| `gocognit` | Cognitive complexity | 15 |
| `funlen` | Function length | 80 lines |
| `interfacebloat` | Interface size | 5 methods |
| `errcheck` | Unchecked errors | All, including type assertions |
| `gocritic` | Non-idiomatic patterns | diagnostic + style + performance |

## Policy

- Prefer `make precommit` during implementation for fast changed-file feedback.
- If lint/check output reveals a real issue, fix the smallest relevant scope.
- If a lint finding is outside task scope or appears to be existing unrelated debt, report it instead of broadening the change.
- Avoid repo-wide `pnpm format` or `eslint --fix` sweeps unless explicitly requested.
- Do not weaken/delete/skip tests or lint rules to hide failures.
