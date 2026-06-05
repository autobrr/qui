# AGENTS.md

Repo rules for AI agents working on qui.

## Collaboration

- Stay inside requested scope. Do not implement review-suggested/extra changes without explicit user approval.
- Treat other agent/Codex/CodeRabbit feedback as input to discuss, not automatic action.
- qui is single-user self-hosted software. Prefer readable, maintainable code over paranoid guards for impossible states.

## Repo Map

- Backend: `cmd/qui`, `internal/`, shared `pkg/`
- Frontend: `web/src`, assets `web/public`, bundle output `internal/web/dist`
- User docs: `documentation/docs/`; internal notes: `docs/`
- Docker/compose/release files: repo root

Keep `README.md` concise; put feature deep-dives in `documentation/docs/`.

Before changing cross-module data flow, service boundaries, API routing, or long-lived architecture, read `docs/architecture.md`.

## Required Commands

- Build: `make build` (frontend bundle + Go binary)
- Backend only: `make backend`
- Frontend only: `make frontend`
- Dev: `make dev`, `make dev-backend`, `make dev-frontend`
- Required before final for code changes: `make precommit`, targeted tests for touched packages, `make build`
- Go tests: always use `-race -count=1`
- Full Go suite: `make test` (`go test -race -count=1 -v ./...`)
- OpenAPI changes under `internal/web/swagger`: run `make test-openapi`

For changes under `internal/services/crossseed` or `internal/qbittorrent`, run targeted package tests first. Skip local full `make test` by default; CI covers it unless requested.

## Lint / Format

- `make precommit` = fmt + gofix changed files + lint changed files.
- `make lint` = changed files only.
- `make lint-json` writes `lint-report.json`.
- `make fmt` = gofmt + frontend eslint fix on changed files.
- Avoid repo-wide `pnpm format` / `eslint --fix` sweeps unless explicitly requested.
- If lint/check output reveals a real issue, fix the smallest relevant scope or report why blocked.
- If lint output is unclear or requires policy judgment, read `docs/linting.md`; otherwise treat tool output and config as source of truth.

## Go / Backend

- Keep Go `gofmt` clean.
- Exports: PascalCase. Locals: camelCase.
- Group package interfaces by domain under `internal/<area>`.
- Prefer explicit error handling.
- Keep interfaces small (<=5 methods).
- Avoid `map[string]interface{}`; use structs.
- No backward compatibility shims unless requested.
- Go 1.22+: do not add `tt := tt` in parallel subtests.
- Tests live beside code as `*_test.go`; prefer table-driven tests and existing fixtures.
- Test file writes should use `os.WriteFile(..., 0o600)` unless broader mode is required.

## Code Shape

- Prefer behavior-bearing branches only.
- If multiple `switch` cases equal `default`, collapse them.
- Boolean classifiers should list exceptional `true`/error cases; let `default` handle common path.
- Do not add documentation-only branches unless compiler/linter/tests enforce value.

## Paths / Security

qui must work on Windows and Unix-like hosts.

- Local filesystem paths: `filepath.Join`, `filepath.Clean`, `filepath.Rel`, `filepath.Separator`.
- Slash-delimited formats only: `path` for torrent-internal file names, URLs, API payloads.
- At torrent/API -> local FS boundaries: validate slash paths, then convert with `filepath.FromSlash`.
- Traversal checks must reject POSIX + Windows escaping on every OS: leading `/`, leading `\`, drive letters, UNC, `..`.
- Cross-platform tests: avoid raw `"/foo/"` local path assertions; use `filepath.ToSlash` or `filepath.Join`.
- Path traversal tests should include POSIX and Windows cases.

## Frontend

Frontend-specific rules live in `web/AGENTS.md`. Read that file before editing `web/`, i18n, React components, or frontend tests.

## API / Database

- DB schema changes need SQLite + Postgres migrations, matching model/store updates, same PR.
- Open PRs: consolidate schema work to at most one new SQLite migration and one new Postgres migration; edit draft migrations before merge.
- API contract changes must update `internal/web/swagger` and pass `make test-openapi`.
- Keep diffs minimal in high-churn areas: `internal/services/crossseed`, `internal/qbittorrent`, `internal/models`.

## Commits / PRs

- Conventional commits: `feat(scope):`, `fix(scope):`, etc.
- Keep commits focused; split backend/frontend when practical.
- Never add AI advertising/attribution/co-author lines.
- PRs need clear summary, testing checklist, and screenshots for visual UI changes.

## Final Report

State required checks run, skipped/deferred checks with reason, and unresolved failures. Do not claim complete while a required repo check is known failing unless user accepts the risk.
