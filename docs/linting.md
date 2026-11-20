# Linting Notes

Internal reference for lint policy and interpretation. Read this when lint output is unclear, when deciding whether to fix or report a lint issue, or when changing lint-related tooling. The actual lint configuration and tool output remain the source of truth.

## Commands

- `make precommit`: `check-mojibake` and `generate`, then fmt + gofix on changed files, then `make lint`, `make lint-ratchet`, `make typecheck-frontend`, `make check-i18n`, and `make test-frontend-changed`.
- `make lint`: golangci-lint on Go issues that are new since the `origin/develop` merge-base, then the `unused` linter on the whole repo, then the full `pnpm lint`. A change can make unchanged code unused, so the `unused` run does not stop at changed lines. A frontend lint error in any file fails it, not only an error in a changed file.
- `make lint-ratchet`: run the linters in `scripts/ratchet/golangci.yml` and compare the problems in each file with `scripts/ratchet/allowlist.json`. CI runs it on each pull request. The target compares with the allowlist at the merge base of `origin/develop` and your branch, so a count that `develop` lowered after your branch started is not growth. In a shallow clone without that merge base, the target compares with `origin/develop`. If a merge is not committed yet, or your `origin/develop` is behind, the target reports growth that is not real. Commit the merge first, or run `git fetch origin develop`.
- `make typecheck-frontend`: `tsc -b` in `web/`, the type check that the frontend build runs first. ESLint and Vitest do not report type errors.
- `make test-frontend-changed`: the whole Vitest suite, when the branch changes a file under `web/` or `internal/` since the `origin/develop` merge-base, an uncommitted or untracked file included. Some frontend tests read backend files, for example the theme files in `internal/themes/assets/`. Without `origin/develop`, the suite always runs.
- `make lint-json`: write lint output to `lint-report.json`.
- `make fmt`: gofmt + frontend eslint fix on changed files.
- `make gofix-changed`: apply `go fix` on changed Go files only.
- `make gofix-check-changed`: check `go fix` drift on changed Go files only.

Frontend lint covers `web/src`, the config files at the web root except `web/vite.config.ts`, and `web/scripts/**/*.mjs`. The scripts run under Node, so they get the recommended rules and Node globals, not the React or stylistic rules.

## Linter Intent

The project uses golangci-lint v2. `.golangci.yml` lists the enabled linters and their settings.

## Version Pin

CI pins the golangci-lint version in `.github/workflows/lint.yml`. A local golangci-lint must read the export data that your Go toolchain writes. A golangci-lint that is older than your Go toolchain can fail to read that export data. With such a pair, lint reports false `typecheck` errors in packages that compile, and these errors can hide real issues.

A local golangci-lint that is newer than the pin can find problems that the pinned version does not find. Then `make lint-ratchet` can fail on a file that your branch does not change, while the ratchet job in CI passes. `make lint` and `make lint-ratchet` print a warning when your golangci-lint is not the CI pin. To get the counts that CI gets, install the pinned version with `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<pin>`, where `<pin>` is the version in `lint.yml`.

Bump the CI pin in its own PR. Run `make lint-full` first, because `make lint` reports only new issues, and a new golangci-lint version can find issues in old code.

## Prove a New Rule

To prove that a new forbidigo or depguard rule fires, add a temporary violation and lint the package.

- Put the violation on its own line. golangci-lint keeps one issue per line, so a hit from another linter on the same line hides the hit that you want.
- If a forbid pattern must cover each entry point of a package, read the full output of `go doc -short <pkg>`. Do not filter it with a word. A filter misses entry points with other names.
- Use an inline `//nolint:<linter> // reason` where the line can carry one. A `source:` exclusion in `.golangci.yml` also exempts each exact copy of the line in the same file. Keep `source:` exclusions for lines that cannot carry a nolint.

## Ratchet

The linters in `scripts/ratchet/golangci.yml` are rules that existing code does not pass yet. `scripts/ratchet/allowlist.json` records the number of problems in each file that broke a rule when the rule was added. The count for a file can only go down. `go run ./scripts/ratchet` applies five rules:

1. A file reports no more problems than its entry allows. A file without an entry must have no problems.
2. An entry is not higher than the number that the file reports. When you fix a problem, lower the count of its file in the same change, and delete the entry at 0.
3. No entry is higher than the entry on the base branch. A linter that the allowlist of the base does not list is a new rule, which starts with the count of each file.
4. No `nolint` comment hides `gocognit`. Split the function.
5. The comment totals do not grow. See "Comments" below.

- The other linters in the ratchet accept `//nolint:<linter> // reason` when the code is correct on purpose.
- To get the count for a new or split file, run `go run ./scripts/ratchet` before you edit `allowlist.json`. Each "reports N problems, the allowlist allows M" line names the file, the linter, and N.
- If you move code that has ratchet problems into a new file, or rename such a file, the new path needs an allowlist entry. Rule 3 then fails with "the entry grew from 0 to N", also when the totals do not change. No flag skips rule 3, so a maintainer merges that pull request past the failed check.

Each day, `.github/workflows/ratchet.yml` fixes up to 5 problems in one file and opens a pull request with the `ratchet` label. ADR 0020 describes it. It does not take the `comments` entries.

### Comments

`docs/standards/simplicity.md` bans comments. The ratchet holds the comments from before the ban, in two places:

- **Go:** the `comments` key of `allowlist.json` is not a golangci-lint linter. The ratchet command counts the comments in each tracked Go file itself, with `scripts/comments/gocomments`. Rules 1 and 2 apply to each file. For rule 3, the command compares the total of all files with the total on the base branch, so a comment that moves to another file with its code does not fail the check. Move its count to the entry of the new file.
- **TypeScript:** ESLint reports each comment as `qui/no-comments`. `web/eslint-suppressions.json` records the number of reports that each file had when the rule was added, and ESLint fails a file that has more reports, or fewer. After you delete a comment, run `pnpm exec eslint --prune-suppressions <file>` in `web/`. Rule 5 compares the total in that file with the total on the base branch. Do not add to it with `--suppress-rule`.

`web/scripts/no-comments-rule.mjs` and `scripts/comments/gocomments` list the markers that are not comments for this rule. The ESLint rule has no fix, because `make fmt` runs `eslint --fix` and must not delete a comment that nobody read. The `/delete-comment` skill sorts the comments of a file and deletes them.

## Policy

- Prefer `make precommit` during implementation for fast feedback.
- If a lint finding is outside task scope or appears to be existing unrelated debt, report it instead of broadening the change.
