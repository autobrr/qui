---
status: accepted
date: 2026-11-23
---

# The held effects go down in runs of a maintainer's coding agent, not in CI

A coding agent on the machine of a maintainer removes the effects that `qui/no-direct-effect` holds. Each run takes one file of `web/eslint-suppressions.json`, replaces up to 3 of its effects, opens the pages that show the file in a browser, and opens a normal pull request with the `ratchet-web` label. "Held Effects" in `docs/linting.md` has the steps. `.github/workflows/ratchet.yml` has one lane again, the Go lane of ADR 0020. This decision replaces ADR 0075.

ADR 0075 gave the bot a second lane for the frontend. Its first run, pull request 4397, changed nothing. The one effect in `web/src/App.tsx` needs a change in `main.tsx` or a new hook file, and the lane let the agent edit only the picked file, its tests, and `web/eslint-suppressions.json`. ADR 0073 moves an effect that a job needs into a hook of its own, with a line in `effectHooks`. So the file list of the lane ruled out the fix that the lint rule asks for.

Two more things did not fit a runner. A removed effect can change when a component renders, and that shows only in a browser. CodeRabbit skips a pull request that a bot opens, so the change with the highest risk of the two lanes got the fewest reviews.

The Go lane stays. A lint fix there stays in one file and its test, `go test` proves it, and each of its 31 pull requests merged.

## Consequences

- A run can add a hook file, its line in `effectHooks`, and each other change that the fix needs. No file list limits the agent, so the maintainer reads the whole diff. The review bots read it too, because a person opens the pull request.
- The runs spend nothing from the $120 monthly limit of the bot (ADR 0017). They cost the maintainer's own agent usage, and they run only while that machine is on.
- Nothing in the repository starts a run. If no maintainer runs the steps, the 225 held effects go down only in other work.
- One pull request with the `ratchet-web` label is open at a time. A file is skipped when its pull request from the branch `effects/<file>` closed without a merge.
- Pull request 4397 closes without a merge. Its branch has the name of the old lane, so `web/src/App.tsx` is still the first file of the next run.

## Considered options

- **Let the lane also edit new hook files and `web/eslint.config.js`.** Rejected. That file holds each lint rule of the frontend, so an agent that can edit it can turn off the checks of its own change. The lane would still have no browser.
- **Keep the lane for the files that it can fix.** Rejected. Each of its pull requests would still merge with no look at the page, and with no review from CodeRabbit.
- **Remove the lane and start no runs.** Rejected. ADR 0073 holds 225 effects in 106 files, and each one is an example that the next author copies.
