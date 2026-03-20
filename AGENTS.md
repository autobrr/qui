# AGENTS.md

Repository guidelines for AI coding agents (Codex, Claude Code, etc.) working on qui.

## Owner Collaboration Notes

- Do not implement review-suggested or extra changes outside requested scope without explicit user approval first.
- Treat other agent/Codex/CodeRabbit feedback as input to discuss, not automatic action.

## Project Structure & Module Organization

The Go backend lives in `cmd/qui` (entrypoint) and `internal/` modules for configuration, qBittorrent, metrics, and API routing; shared helpers sit in `pkg/`. The React/Vite client is in `web/src` with static assets in `web/public`, and its production bundle must stay synced to `internal/web/dist`. Reference docs live under `docs/`, while Docker and compose files in the repository root support container workflows.

End-user docs live in the Docusaurus project under `documentation/docs/`. Prefer updating those for user-facing copy; `docs/` is mostly internal/engineering notes.

Keep `README.md` concise. Feature deep-dives belong in `documentation/docs/`, not the root README.

## Build, Test, and Development Commands

```bash
# Build
make build              # Frontend bundle + Go binary with version metadata
make backend            # Go binary only
make frontend           # Frontend bundle only

# Development
make dev                # Starts air (hot-reload) + pnpm dev
make dev-backend        # Backend only with hot-reload
make dev-frontend       # Frontend only

# Testing
make test               # go test -race -count=1 -v ./...
make test-openapi       # Validate OpenAPI spec after touching internal/web/swagger

# Linting
make lint               # Changed files only (fast, use during iteration)
make lint-json          # JSON output to lint-report.json

# Pre-commit
make precommit          # fmt + gofix + lint on changed files

# Formatting
make fmt                # gofmt + frontend eslint --fix on changed files
make gofix-changed      # Apply go fix on changed Go files only
make gofix-check-changed # Check go fix drift on changed Go files only
```

## Linting Strategy

The project uses golangci-lint v2 with strict configuration targeting AI-generated code patterns:

| Linter | Purpose | Threshold |
|--------|---------|-----------|
| dupl | Catch code duplication | 100 tokens |
| gocognit | Cognitive complexity | 15 |
| funlen | Function length | 80 lines |
| interfacebloat | Interface size | 5 methods |
| errcheck | Unchecked errors | All, including type assertions |
| gocritic | Non-idiomatic patterns | diagnostic + style + performance |

**Workflow:**
1. During implementation: `make precommit` (changed files only, fast feedback)
2. To fix issues: `make lint-fix` then address remaining manually

**Guardrail (web formatting):** avoid repo-wide `pnpm format` / `eslint --fix` sweeps unless explicitly requested. Prefer fixing only the files reported by lint for the current task/PR.

## Coding Style & Naming Conventions

Keep Go code `gofmt`-clean with PascalCase exports, camelCase locals, and package-level interfaces grouped by domain inside `internal/<area>`. The frontend follows ESLint @stylistic defaults: two-space indentation, double quotes, trailing commas on multiline literals, and Unix line endings. Organize React modules by feature within `web/src/{pages,routes,components}` and choose descriptive file names (e.g., `torrent-table.tsx`).

**Critical conventions:**
- Prefer explicit error handling over silent failures
- Keep interfaces small (≤5 methods)
- Avoid `map[string]interface{}` — use proper structs
- No backward compatibility shims unless explicitly requested
- **Loop variables (Go 1.22+):** Don't use `tt := tt` in parallel subtests — Go 1.22+ creates a new variable per iteration, so the old workaround is unnecessary and flagged by linters

**Single-user self-hosted context:** qui runs on someone's home server, not as a multi-tenant SaaS with untrusted input and complex failure modes. Skip paranoid defensive programming for impossible or purely theoretical scenarios. Code that guards against states that can't happen adds complexity without value. Prioritize readable, maintainable code over excessive robustness.

## Code Shape

- Prefer behavior-bearing branches only. If multiple `switch` cases return the same value as `default`, collapse them.
- In boolean classifiers, list only the exceptional cases (`true` cases or error cases). Let `default` handle the common path.
- Do not add documentation-only branches unless they enforce something mechanically via compiler, linter, or tests.
- When a branch only enumerates known states, ask whether it changes behavior, improves safety, or provides exhaustiveness checking. If not, delete it.

## React Effects

- Use `useEffect` only to sync with external systems (DOM, subscriptions, network).
- Avoid derived state in Effects; calculate during render, or `useMemo` for expensive compute.
- Put user-driven logic in event handlers, not Effects.
- To reset state, prefer a `key` or render-time adjustments instead of Effects.
- Fetch Effects must guard against stale responses (cleanup/abort).
- Source: https://react.dev/learn/you-might-not-need-an-effect

## Testing Guidelines

Place backend tests beside implementations as `*_test.go`, mirroring paths such as `internal/qbittorrent/pool_test.go`. Prefer table-driven cases and reuse the integration fixtures already in `internal/qbittorrent/`. Run `make test` before every push and add `make test-openapi` when contracts change. Frontend work should include Vitest + React Testing Library specs named `*.test.tsx` near the component.

When running tests, always use `-race` and `-count=1`.

For changes under `internal/services/crossseed` or `internal/qbittorrent`, run targeted package tests first, then run the full `make test` suite.

When adding Go tests that create files with `os.WriteFile`, use `0o600` or tighter permissions unless the test explicitly needs broader mode bits. This avoids `gosec` `G306` lint failures.

## Commit & Pull Request Guidelines

Follow the conventional commit style in history (`feat(scope):`, `fix(scope):`, etc.) and link issues or PR numbers in the body when relevant. Keep commits focused—split backend and frontend changes when practical.

**Never add:**
- "🤖 Generated with [Claude Code](https://claude.com/claude-code)"
- "Co-Authored-By: Claude" or any AI co-author credits
- Any advertising or attribution in commit messages

PRs need a clear summary, testing checklist, and UI screenshots for visual tweaks. Confirm `make lint`, `make test`, and a fresh `make build` succeed before requesting review.

## Pre-Commit Checklist

1. `make precommit` passes (`fmt` + `gofix-changed` + `lint`, changed files only)
2. `make test` passes
3. `make build` succeeds
4. If touched `internal/web/swagger`, run `make test-openapi`

## Security & Configuration Tips

Load secrets such as `THEMES_REPO_TOKEN` via `.env` so the Makefile can fetch premium themes, and keep the file out of version control. Record configuration defaults in `config.toml` but evolve runtime schema through Go migrations rather than editing `qui.db` directly. Drop cached databases and logs (`qui.db*`, `logs/`) from commits to avoid leaking local data.

## API & Database Change Rules

- Database schema changes must ship as migrations under `internal/database/migrations`, include matching model/store updates in the same PR, and add both SQLite and Postgres migrations.
- API contract changes must update OpenAPI content under `internal/web/swagger` and pass `make test-openapi`.
- Prefer minimal, reviewable diffs in high-churn areas (`internal/services/crossseed`, `internal/qbittorrent`, `internal/models`).

## Architecture Quick Reference

```text
cmd/qui/main.go              CLI entrypoint (serve, generate-config, create-user, etc.)
internal/api/                HTTP handlers + middleware (chi router)
internal/qbittorrent/        Client pool, sync manager
internal/services/           Domain services (crossseed, jackett, reannounce, trackerrules)
internal/proxy/              Reverse proxy for external apps
internal/backups/            Scheduled snapshots
internal/database/           SQLite + migrations
internal/models/             Data models + store interfaces
pkg/                         Shared utilities
web/src/                     React 19 + Vite + TypeScript + Tailwind v4
```

**Key data flow:**
1. `SyncManager` polls qBittorrent instances via `ClientPool`
2. Torrent state cached in-memory with delta updates
3. Frontend fetches via REST API, real-time updates via SSE
4. Cross-seed service listens for torrent completion events
