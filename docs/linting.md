# Linting Notes

Internal reference for lint policy and interpretation. Read this when lint output is unclear, when deciding whether to fix or report a lint issue, or when changing lint-related tooling. The actual lint configuration and tool output remain the source of truth.

## Commands

- `make precommit`: `check-mojibake` and `generate`, then fmt + gofix on changed files, then `make lint`, `make lint-ratchet`, `make typecheck-frontend`, `make check-i18n`, and `make test-frontend-changed`.
- `make lint`: golangci-lint on Go issues that are new since the `origin/develop` merge-base, then the `unused` linter on the whole repo, then the full `pnpm lint`. A change can make unchanged code unused, so the `unused` run does not stop at changed lines. A frontend lint error in any file fails it, not only an error in a changed file. `pnpm lint` also fails on a warning, so each frontend rule is `error` or off. CI runs the same full `pnpm lint` when a pull request changes a file under `web/`.
- `make lint-ratchet`: run the linters in `scripts/ratchet/golangci.yml` and compare the problems in each file with `scripts/ratchet/allowlist.json`. The target first lowers each entry above the count of its file (rule 2): commit that change to `allowlist.json`. CI runs the same check on each pull request, without the lowering. The target compares with the allowlist at the merge base of `origin/develop` and your branch, so a count that `develop` lowered after your branch started is not growth. In a shallow clone without that merge base, the target compares with `origin/develop`. If a merge is not committed yet, or your `origin/develop` is behind, the target reports growth that is not real. Commit the merge first, or run `git fetch origin develop`.
- `make typecheck-frontend`: `tsc -b` in `web/`, the type check that the frontend build runs first. ESLint and Vitest do not report type errors.
- `make test-frontend-changed`: the whole Vitest suite, when the branch changes a file that the frontend reads since the `origin/develop` merge-base, an uncommitted or untracked file included. Those files are each file under `web/`, and each file outside `web/` that a file under `web/src` names by a relative path, for example the theme files in `internal/themes/assets/`. The target finds only a path that is one string and starts with `../`. CI runs the suite on each pull request that changes code. Without `origin/develop`, the suite always runs.
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

The linters in `scripts/ratchet/golangci.yml` are rules that existing code does not pass yet. `scripts/ratchet/allowlist.json` records the number of problems in each file that broke a rule when the rule was added. The total of a linter can only go down. `go run ./scripts/ratchet` applies seven rules:

1. A file reports no more problems than its entry allows. A file without an entry must have no problems.
2. An entry is not higher than the number that the file reports, and an entry at 0 is deleted. `make lint-ratchet` writes both changes.
3. The total of a linter is not higher than its total on the base branch. A linter that the allowlist of the base does not list is a new rule, which starts with the count of each file.
4. No `nolint` comment hides `gocognit`. Split the function.
5. A `nolint` comment names only linters that `.golangci.yml` or `scripts/ratchet/golangci.yml` runs. A name of a linter that is off hides nothing: delete it.
6. `web/eslint-suppressions.json` has entries only for the rules in `suppressedRules` of `scripts/ratchet/comments.go`, each of those rules has an entry, and the total of each rule does not grow. See "Suppressed ESLint Rules" below.
7. No code file crosses the size limit in `.file-size.json`, and that file does not get looser. See "File Size" below.

- The other linters in the ratchet accept `//nolint:<linter> // reason` when the code is correct on purpose.
- To get the count for a new or split file, run `go run ./scripts/ratchet` before you edit `allowlist.json`. Each "reports N problems, the allowlist allows M" line names the file, the linter, and N.
- If you move code that has ratchet problems into another file, or rename such a file, move its count to the entry of the new path in the same change. Rule 3 fails only when the total of a linter grows, and its message names each entry that grew. ADR 0077 has the reason.

Each day, `.github/workflows/ratchet.yml` fixes up to 5 problems in one file of `allowlist.json` and opens a pull request with the `ratchet` label. ADR 0020 describes it. It does not take the `comments`, `offlog`, `mapany`, or `testsleep` entries, and it opens no pull request while one with its label is open. The held effects of the frontend go down outside CI. See "Held Effects" below.

### Comments

`docs/standards/simplicity.md` bans comments. The ratchet holds the comments from before the ban, in two places:

- **Go:** the `comments` key of `allowlist.json` is not a golangci-lint linter. The ratchet command counts the comments in each tracked Go file itself, with `scripts/comments/gocomments`. Rules 1 to 3 apply to it as to each linter, so a comment that moves to another file with its code does not fail the check.
- **TypeScript:** ESLint reports each comment as `qui/no-comments`, one of the suppressed ESLint rules below.

`web/scripts/no-comments-rule.mjs` and `scripts/comments/gocomments` list the markers that are not comments for this rule. The ESLint rule has no fix, because `make fmt` runs `eslint --fix` and must not delete a comment that nobody read. The `/delete-comment` skill sorts the comments of a file and deletes them.

### Lines About an Off Feature

`docs/standards/logging.md` bans a DEBUG or INFO line about a feature that is off or not set up (ADR 0074). The `offlog` key of `allowlist.json` holds the lines from before the ban. Like `comments`, it is not a golangci-lint linter: the ratchet command counts the lines in each tracked Go file itself, with `scripts/offlog`. It skips `_test.go` files. Rules 1 to 3 apply to it as to each linter.

A line counts when its level is DEBUG or INFO, and one of these is true:

- A message literal says `disabled`, `not enabled`, `not configured`, `turned off`, or `is off`.
- The line is in the body of an `if` that tests an off switch: `!x.Enabled`, `x.Disabled`, `disabled()`, a name that ends in `Off`, or the `else` of `if x.Enabled`. A name that ends in `Active` or `Configured` counts like `Enabled`. With `&&` and `||`, the line counts only when the condition proves that the switch is off: `!x.Enabled && y` does, and `x.Disabled || y` does not.

When a file has more lines than its entry allows, the command prints the position of each counted line in that file. Delete the line, or write it at TRACE. A warning or an error never counts, so this rule has no `nolint` marker.

The counter reads only the syntax tree. It cannot read the text of a message that a variable holds, and it does not take a `switch` case for a test of an off switch, so a line there counts only by the other rule. It does not see a line that a helper function writes, or a notification. The silence test that `docs/standards/logging.md` asks for is the first guard, and the counter is the second.

### Maps of Any

Go code uses a struct, not a `map[string]any` or a `map[string]interface{}`. The `mapany` key of `allowlist.json` holds the maps from before the rule, also the ones that decode JSON of an unknown shape on purpose. Like `comments`, it is not a golangci-lint linter: `scripts/ratchet/mapany.go` counts each such map type in each tracked Go file. It skips `_test.go` files. Rules 1 to 3 apply to it as to each linter. ADR 0080 has the reason.

### Sleeps in Tests

`docs/standards/tests.md` bans a real `time.Sleep` in a test, because it makes the test slow, and the test fails at random on a slow CI runner. The `testsleep` key of `allowlist.json` holds the sleeps from before the rule. Like `comments`, it is not a golangci-lint linter: `scripts/ratchet/testsleep.go` counts each `time.Sleep` in each tracked `_test.go` file. A sleep does not count in the function that a test passes to `synctest.Test`, or in a function that calls `synctest.Wait`, because `synctest.Wait` works only in a bubble. A sleep in code that runs while Go evaluates the arguments of `synctest.Test`, for example in a function that builds the test function, still counts. Rules 1 to 3 apply to it as to each linter, and it has no `nolint` marker.

The counter reads one file at a time. It counts a sleep in a function that runs in a bubble but does not call `synctest.Wait`, for example a body that a helper passes to `synctest.Test`. Call `synctest.Wait` after the sleep. It returns once every other goroutine of the bubble has ended or is blocked, for example on a channel or on a timer that has not fired yet. A goroutine can block before its work is done, so a test that checks the result of that work also waits for the result on a channel. The counter does not see a sleep in a file that is not a `_test.go` file.

### Suppressed ESLint Rules

`web/eslint-suppressions.json` records the number of reports that each file had when a rule was added. ESLint fails a file that has more reports of the rule, or fewer. Nine rules have entries:

- `@typescript-eslint/consistent-type-assertions` reports each type cast (`value as Type`) outside the tests and `src/test`. `as const` and `satisfies` pass. Get the type from a type guard, a zod schema, or a type on the source of the value. ADR 0089 has the reason.
- `@typescript-eslint/no-non-null-assertion` reports each `value!` outside the tests and `src/test`. Check the value, or give its source a type without `null` and `undefined`. ADR 0089 has the reason.
- `qui/disable-reason` reports an `eslint-disable` comment that gives no reason. See "Exceptions" below.
- `qui/entry-files` reports an import into a subfolder of a feature folder, such as `components/instances/marks`, from a file outside that feature. The files at the root of a feature folder are its entry files, and the tests are outside the rule. ADR 0081 has the reason.
- `qui/layer-order` reports an import from a folder of `web/src` that comes later in the folder order than the folder of the file. `layers` in `web/eslint.config.js` holds the order, and the tests are outside the rule. ADR 0076 has the reason.
- `qui/no-comments` reports each comment.
- `qui/no-direct-effect` reports each call of `useEffect`, `useLayoutEffect`, or `useInsertionEffect` outside the tests and outside the hooks in `effectHooks` of `web/eslint.config.js`. ADR 0073 has the reason.
- `react-hooks/exhaustive-deps` reports a hook whose dependency list does not match the values that the hook reads.
- `react-refresh/only-export-components` reports a file that exports a component and another value, because fast refresh then reloads the whole file.

After you fix a report of one of these rules, run `pnpm exec eslint --prune-suppressions <file>` in `web/`. Do not add to the file with `--suppress-rule`. Rule 6 compares the total of each rule with the total on the base branch, so code that moves to another file with its count does not fail the check. When the last entry of a rule goes, delete the rule from `suppressedRules` in the same change, or rule 6 fails. A rule that is not in that list may have no entries, so its reports cannot come back.

A new rule that old code breaks starts the same way: add its name to `suppressedRules`, and write its reports to the file one time with `pnpm exec eslint --suppress-rule <rule>` and the paths of the `lint` script in `web/package.json`.

### Held Effects

A run of a coding agent on the machine of a maintainer removes the effects that `qui/no-direct-effect` holds, one file in each pull request. Nothing in the repository starts a run. ADR 0084 has the reason. One run does these steps:

1. Run `gh pr list --label ratchet-web --state open`. If it lists a pull request, stop.
2. List the files, the fewest effects first: `jq -r 'to_entries | map({f: "web/\(.key)", n: (.value["qui/no-direct-effect"].count // 0)} | select(.n > 0)) | sort_by(.n, .f)[] | "\(.n) \(.f)"' web/eslint-suppressions.json`.
3. Take the first file. If `gh pr list --head "effects/<file>" --state closed --json number,mergedAt` lists a pull request that did not merge, take the next file.
4. Replace up to 3 of its effects as the message of the rule says, and keep the behaviour the same. An effect that the job needs moves into a hook of its own, with a line in `effectHooks`.
5. Lower the entry of the file with `pnpm --dir web exec eslint --prune-suppressions <path of the file below web/>`, for example `src/App.tsx`.
6. Open each page that shows the file in a browser, on the base branch and on the change, and compare what the replaced effects did. A removed effect can change when a component renders, and no test shows that.
7. Open the pull request from the branch `effects/<file>`, with the title `refactor(web): drop effects in <name of the file>` and the labels `ratchet-web` and `web`.

### File Size

An agent reads a whole file into its context, so a large file leaves less room for the task. `.file-size.json` at the root of the repo sets the limit. ADR 0078 has the reason.

- `max_bytes` is the limit: 20,000 bytes, which is about 5,000 tokens. A CRLF line end counts as one byte.
- `extensions` lists the files that count as code: Go, TypeScript, and scripts. Test files count.
- `skip` lists the generated files. An entry is a path, or a folder that ends in `/`.

The ratchet command fails a file that crosses the limit, and a new file that starts over it. A file that is over the limit on the base branch can still grow, and a rename of such a file passes.

When the check fails, move code out of the file. The message of the check gives the five rules for the move. If the moved code has ratchet problems, move their counts in `allowlist.json` in the same change (rule 3).

Only a maintainer makes `.file-size.json` looser: a higher `max_bytes`, a new `skip` entry, or one extension less. The command fails on such a change, also when a maintainer makes it, and the maintainer merges it past the failed check. If a file is generated, or a move needs a new folder or a new exported name, stop and ask the maintainer.

## Feature Map

`web/feature-map.json` lists each route of the frontend and each dialog that a route loads, with the steps to use it. ADR 0082 has the reason. `make check-feature-map` fails when the list does not match the code, when a route has no steps, and when more dialogs have no steps than on the base branch. It compares with the merge base of `origin/develop`, and the Lint workflow with the base branch of the pull request. `pnpm check:feature-map` in `web/` compares the number of dialogs only when you give it a base, as `--base <commit>`.

- `pnpm feature-map` in `web/` writes the list again and keeps the steps. Run it after you add, remove, rename, or move a route or a dialog, then write the steps of each new entry.
- A step is one action of a user, with the address or the name of the control. Follow the steps on a running qui before you commit them.
- A rename or a move of a dialog gives it a new id. Copy its steps from the old id before you run `pnpm feature-map`.
- `node web/scripts/feature-map.mjs --where <file>` prints the routes that load a file, with their steps.

## Exceptions

An exception is a comment that hides the report of a check on its line: `//nolint`, `#nosec`, `//gosec:disable`, and `//exhaustive:ignore` in Go, and `eslint-disable`, `@ts-expect-error`, `@ts-ignore`, and `@ts-nocheck` in the frontend. Two rules apply, and ADR 0079 has the reason.

- **An exception gives its reason on the line.** golangci-lint demands `//nolint:<linter> // reason`. ESLint demands `eslint-disable-next-line <rule> -- <reason>` with the rule `qui/disable-reason`, and a description after `@ts-expect-error`. The 13 `eslint-disable` comments from before the rule are held as a suppressed ESLint rule. `qui/disable-reason` cannot report `/* eslint-disable */` without a rule name, because that comment turns the rule off too. The check below counts that comment.
- **A pull request that adds a new exception needs the approval of a maintainer.** `.github/workflows/exceptions.yml` runs `.github/scripts/count_exceptions.py` on each pull request. The script reads each changed Go, TypeScript, and JavaScript file whole, as it is before and after the pull request, finds its comments with the Go and TypeScript parsers, and compares the exceptions of the two sides. An exception is new when the pull request removes no comment with the same text. So an exception that moves with its code, in its file or to another file, is not new. An exception with a changed text is new, and so is one that takes the place of another exception. When the pull request adds a new exception, the check fails and lists the new ones. The check passes when a maintainer sets the label `exceptions-approved`.

When the check fails, fix the report that each new exception hides, and delete the exception. If the code is right on purpose, keep the exception with its reason, and ask a maintainer for the label. Only a maintainer sets the label, also on a pull request of their own. A coding agent never sets it.

The workflow runs the script of the base branch and reads the files of the pull request only as text, so a pull request cannot change its own check. A change to the script or the workflow takes effect after it merges.

## Policy

- Prefer `make precommit` during implementation for fast feedback.
- If a lint finding is outside task scope or appears to be existing unrelated debt, report it instead of broadening the change.
