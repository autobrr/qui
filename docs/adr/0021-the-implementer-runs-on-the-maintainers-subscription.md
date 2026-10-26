---
status: accepted
date: 2026-10-22
---

# The implementer runs on the maintainer's subscription

`.github/workflows/implement.yml` builds one `ready-for-agent` issue and opens a pull request for it. Only s0up4200 can start a run, from `workflow_dispatch`, with the issue number as input. The run uses Claude Code with the Claude subscription of s0up4200, through a `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token`. Console billing (ADR 0017) costs too much for this work.

ADR 0017 moved triage off the subscription because anyone who opens a discussion starts a triage run. The Anthropic terms do not let one person make an account available to other people. The implementer is different, because nobody but the owner of the subscription can start it. 14 collaborators can dispatch a workflow in qui, so the gate has three parts:

- The `implementer` environment holds the token and allows only `develop`. To change the workflow, a collaborator needs a reviewed pull request to `develop`.
- The job runs only when `github.actor` and `github.triggering_actor` are both `s0up4200`. The second check stops a re-run by another collaborator.
- The run calls the Claude Code binary, not the Agent SDK. The terms allow a person to sign in to the unmodified Claude Code binary with their own subscription, and they tell developers who build on the Agent SDK to use an API key.

## The token stays away from code that the agent writes

A token from `claude setup-token` is valid for one year. Code that runs as the runner user can read the token in `/proc/<pid>/environ` of the Claude Code process. The runner user also has `sudo` without a password. An environment scrub does not stop either of these. So the agent runs commands only through `qui-sandbox`, a Docker container that has these properties:

- It has no network, no capabilities, and a user that is not root.
- It cannot see the processes of the job, and it gets no environment from the job.
- The repo is read-only in it, except `web/node_modules`. A command cannot change a file that Claude Code or a later step runs, for example `.git/config` or `.claude/settings.json`.

The workflow installs the script at `/usr/local/bin/qui-sandbox`, outside the repo. The permission rules let the agent read only the repo and its skills, with `/proc` denied, and edit only the repo. The checks after the agent run also run in the sandbox. As in ADR 0020, a second job applies the patch on a new runner and opens the pull request as the `qui-rabbit` app. That job runs no code from the change.

The agent cannot change `.github/`, `.claude/`, `docs/agents/`, the `AGENTS.md` and `CLAUDE.md` files, `go.mod`, `go.sum`, or the `web/` package and lock files. These files steer later agent runs, or they add dependencies. The agent can change `GLOSSARY.md` and `docs/adr/`, because `/implement` writes the Domain Docs of the issue there. Both jobs drop a change that touches a forbidden path.

## /implement without a person

The run uses the `/implement` skill from `s0up4200/skills`, at the main branch at the time of the run. The pull request body records the commit. `.github/scripts/implement/prompt.md` changes the steps that need a person:

- The workflow refuses an issue with open sub-issues, so the issue is the spec.
- The workflow refuses an issue that open work blocks: an open issue in its "blocked by" links, or an open issue or pull request in a body line that starts with `Blocked by #N` or `Blocked by: #N, #M`. GitHub accepts only an issue in a "blocked by" link, so the body line is the way to name a pull request.
- The agent picks its own answer to each departure from the spec, builds it, and lists it in the pull request body.
- The agent posts no `Brief amendment` comment and runs no field test and no code review. The review bots review the pull request.

A run whose checks pass opens a ready pull request, and Codex and Greptile review it. CodeRabbit skips a pull request from a bot user. A run with a change that fails its checks opens a draft with its problems. A run that makes no change opens no pull request and fails, so GitHub sends s0up4200 an email and the issue stays free for the next run. A run refuses an issue that has an open `implement/<issue>/` pull request, and one concurrency group allows one run at a time.

## Considered options

- The Agent SDK script of triage and the ratchet (`.github/scripts/triage/run.mjs`) on the subscription: rejected, because of the terms above.
- Console billing: rejected, because it costs too much.
- A Claude Code cloud session that s0up4200 starts: rejected. Its pull requests have s0up4200 as the author, and s0up4200 cannot approve them.
- An `in progress` label to claim an issue: rejected. The branch prefix finds an open pull request for the issue, and the label would need cleanup after a failed run.
- The env scrub with bubblewrap, as in the ratchet: rejected. The ratchet runs tests on one file. The implementer runs much more agent-written code while the token is in reach.

## Consequences

Each run uses the plan limits of s0up4200, and so does interactive use. If a run reaches the limit, it stops. It opens a draft pull request if it changed files before it stopped. The job has a 60-minute timeout and a limit of 150 turns.

The sandbox has no golangci-lint, no Postgres binary, and no network. CI runs those checks on the pull request. A test that writes into the repo fails in the sandbox.

When the token expires, runs fail at sign-in. Run `claude setup-token` again and replace the secret in the `implementer` environment. The Claude Code docs give no way to revoke one token, so the token stays only in the `implementer` environment and in the step that runs the agent.
