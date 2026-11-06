---
status: accepted
date: 2026-11-06
---

# The maintainer starts each Implementer run, and each pull request opens as a draft

The Implementer picks the oldest eligible bug when s0up4200 starts a run with no issue number. It has no schedule. A schedule would pass the start check, because GitHub lists the user who last changed the cron line as the actor of a scheduled run. But triage turns public reports into `ready-for-agent` issues, so a schedule would let a stranger's report spend the subscription of the maintainer with no choice by the maintainer. ADR 0017 moved triage to Console billing for that reason, and Console billing costs too much for this work.

Each pull request opens as a draft, because GitHub refuses to merge a draft, also for an admin. The run cannot do a live test, so the maintainer marks the pull request ready after a field test. One open Implementer pull request blocks the next run.

## Considered options

- A required status check that fails until the pull request reports a live test: rejected. A required check applies to every pull request into `develop`, and 5 of the last 100 merged pull requests ran no checks. An admin can also bypass it.
- A status check that is not required: rejected. A merge past a failed check needs one click.
- A field test by an agent in CI: rejected. The agent would run its own code with the subscription token in reach. ADR 0021 added the sandbox to prevent that.

## Consequences

Codex posted no review on the draft pull requests that we checked. The maintainer can comment `@codex review`. A schedule needs a new decision, after a review of the first 10 pull requests.
