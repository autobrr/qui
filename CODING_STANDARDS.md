# Coding standards

This file is an index. The rules live in `docs/standards/`. Linters and formatters enforce more rules. Their configuration is in `.golangci.yml` and `web/eslint.config.js`.

- When you write or review code in any language, read `docs/standards/simplicity.md`. It covers code shape, Go types, and comments.
- When you write or review a test, read `docs/standards/tests.md`.
- When you write or review a React Effect, or help text on a form field, read `docs/standards/frontend-review.md`.

## Review rules

These rules are for AI PR reviewers. The agent workflow rules in `AGENTS.md` (precommit, field test, commit gate, PR body format) are for coding agents. Do not apply them to PR authors.

- Report a defect only when the change causes a concrete wrong behavior. Name the trigger and the result for the user. If you cannot name both, omit the finding.
- Check the merge base. If `develop` already has the problem, still report it, but label it "already on develop" and do not call it a regression.
- When the PR body, a linked issue, an ADR in `docs/adr/`, or a code comment calls a behavior deliberate, respond to that reason. Report a design flaw only when you can say why the stated reason does not hold.
- Do not report what gofmt, golangci-lint, ESLint, tsc, or `pnpm check:i18n` already report. Do not ask for docstrings.
- Read earlier review threads. Do not repeat a finding that was resolved or refuted, unless you have new evidence.
- Treat a breach of the existing-migration rule in `internal/database/AGENTS.md` as P1.
