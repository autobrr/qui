---
status: superseded by 0084
date: 2026-11-22
---

# The lint ratchet bot runs a second lane for the frontend

`.github/workflows/ratchet.yml` has two lanes. The `go` lane is the bot of ADR 0020: it fixes up to 5 lint problems in one file of `scripts/ratchet/allowlist.json`. The `web` lane takes one file of `web/eslint-suppressions.json` and removes up to 3 of the effects that `qui/no-direct-effect` holds there. Each day, the schedule starts one run of each lane. Each lane has its own label, `ratchet` and `ratchet-web`, and opens no pull request while a pull request with its label is open. So at most two bot pull requests are open at a time, one for the backend and one for the frontend.

Before this decision, the bot fixed only Go files. ADR 0073 holds 226 effects in 106 files, and nothing lowered that number unless a person did the work.

The `web` lane uses what ADR 0020 set up: the `claude` environment, the same Console workspace, federation rule, and $5 stop per run, and the `qui-rabbit` app for the push. The agent never holds the app token. The lanes differ in four places:

- **Files.** The agent can edit the picked file, its test files (`<name>.test.ts` or `<name>.test.tsx`, next to the file or in `__tests__`), and `web/eslint-suppressions.json`. It leaves an effect that needs a new hook file or a change in another file, and names it in the pull request.
- **Commands.** The agent can run ESLint, `tsc`, and Vitest through `pnpm --dir web exec`, and `go run ./scripts/ratchet`. The workflow then saves the patch and runs `pnpm lint`, `tsc -b`, and the whole frontend test suite. If a check fails, the pull request still opens, as a draft that names the check.
- **Branch.** `ratchet/web/<rule>/<file>/<run id>`. A `web` pull request that is closed without a merge skips its rule and file for good, as in ADR 0020.
- **Title.** `refactor(web): drop effects in <file>`.

The pick step runs before the agent. It writes the allowed files, the branch, the title, and the label as job outputs. The job that opens the pull request reads them from there and not from the patch, so code from the agent run cannot change them.

The workflow installs the frontend packages with `--ignore-scripts`, so no install script of a package runs on the runner that holds the Console login.

## Consequences

- Two agent runs each day spend from the $120 monthly limit that the bot shares with triage (ADR 0017). Each run stops at $5.
- A maintainer reviews up to two bot pull requests each day. A removed effect can change when a component renders, so a `web` pull request needs a closer look than a Go lint fix.
- The `web` lane takes only `qui/no-direct-effect`. Each other held rule, such as the 28 imports against the folder order (ADR 0074), needs its name in the pick step and a prompt of its own. The comments stay with `/delete-comment`.
- The `ratchet-web` label must exist in the repository. Without it, the run pushes the branch and then fails to open the pull request.
- A manual run takes the lane as an input. With no input, it runs the `go` lane.

## Considered options

- **A second workflow file.** Rejected. The two jobs, the patch hand-over, and the path check would exist two times, and a fix to one copy would miss the other.
- **One label for both lanes, with the lane read from the branch name.** Rejected. The one-open-pull-request gate and the skip list both query by label, and a second label keeps each query to one lane.
- **One run that fixes a Go file and a frontend file.** Rejected. One pull request would then mix two reviews, and a failed check in one half would hold the other.
- **Take the other held frontend rules now.** Rejected. Each rule needs its own instructions, and the first run on `develop` is the real test of the lane.
