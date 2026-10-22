---
status: accepted
date: 2026-10-22
---

# The lint ratchet bot opens pull requests as a GitHub App

Each day, `.github/workflows/ratchet.yml` takes one file off `scripts/ratchet/allowlist.json`. An agent fixes up to 5 lint problems in it, and the workflow opens one pull request with the `ratchet` label. The workflow commits and pushes as the `qui-rabbit` GitHub App. The app can write contents and pull requests in qui and nothing else.

GitHub starts no workflow for a push or a pull request made with `GITHUB_TOKEN`. A ratchet pull request made with that token gets no CI, so a maintainer cannot see if the fix builds and passes the tests. A push with an app token starts CI as usual. ADR 0017 rejected an app for triage because triage only labels and comments, and needs no CI. The ratchet needs CI on each pull request, so here the app is worth its private key.

The agent never holds the app token. The checks run the tests that the agent wrote, and a test can leave code on the runner, for example a git hook that runs at the next commit. So the job that runs the agent never gets the token. It saves the change as a patch. A second job on a new runner applies the patch, makes sure that it changes only the three allowed files, and then commits and pushes. That job runs no code from the change. The agent uses the same Console workspace, federation rule, and $5 stop per run as triage. The checkout leaves no token in `.git/config`.

The bot opens no pull request while a ratchet pull request is open. A maintainer who closes a ratchet pull request without merging it makes the bot skip that linter in that file for good. A closed draft does not skip the file, because a failed run opens a draft, and a failure from the spend limit or an outage says nothing about the file. To skip a file after a failed run, mark the draft ready for review, then close it. The branch name, `ratchet/<linter>/<file>/<run id>`, records the skip, so the bot needs no other state.

## Considered options

- `GITHUB_TOKEN`, with a maintainer who starts CI by hand: rejected. Each pull request then needs an extra step before review.
- A personal access token of a maintainer: rejected. The token gives the bot all the access of that person, and the pull requests show that person as the author.
- A separate Console workspace with its own spend limit: deferred. The ratchet and triage share the $120 monthly limit of ADR 0017.

## Consequences

Ratchet runs spend from the same monthly limit as triage. A month with many ratchet runs can leave less for triage. If the limit stops a run, the bot opens a draft pull request that says the agent run failed.

The private key of the app is in the `claude` environment. If someone leaks it, revoke the key on the app settings page and make a new one.
