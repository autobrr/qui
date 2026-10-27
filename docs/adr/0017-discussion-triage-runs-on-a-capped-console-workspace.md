---
status: accepted
date: 2026-10-16
---

# Discussion triage runs on a capped Console workspace

Triage runs `.github/scripts/triage/run.mjs`, a script on the Claude Agent SDK, in its own Claude Console workspace. The script signs in with workload identity federation: Claude Code exchanges the GitHub OIDC token of the job for an Anthropic token that expires in 10 minutes. The federation rule in the Console matches only the `claude` environment of `autobrr/qui`, on `develop`, from `workflow_dispatch`. A token from the rule can spend only in the "qui CI" workspace. GitHub stores no Anthropic key, so a run has no long-lived key to leak.

The monthly API credits of the Max plan pay for the runs. Anthropic gives them to the Console organization that is linked to the plan, and they cover Agent SDK usage. The workspace has a monthly spend limit of $120, and auto-reload is off, so a month of triage cannot cost more than that. Each run also stops at $5. A triage run cost $0.38 to $0.74 at list price (2026-10-16), so only a runaway run reaches the stop.

Anyone who opens a discussion starts a triage run. The Anthropic terms say subscription limits assume ordinary, individual usage, and they forbid making an account available to other people. Console usage falls under the Commercial Terms, which have no such limits.

## Considered options

- Keep the subscription token (ADR 0001): kept as a fallback. The repository variable `TRIAGE_AUTH=oauth` selects it.
- An API key in the `claude` environment: rejected. A key does not expire, so a leaked key works until someone revokes it.
- `claude-code-action`: rejected. The action is built on the same SDK, but it tags its runs as Claude Code usage, and the Max plan API credits do not cover Claude Code.
- A GitHub App for the bot identity: rejected. The Anthropic terms follow the model credential, not the GitHub identity. An App adds a long-lived private key and gives only a bot name and a higher rate limit, which triage does not need.

## Consequences

When the workspace reaches its cap or the organization runs out of credits, triage runs fail. Other usage in the organization spends the same credits. The daily sweep then sends the untriaged discussions again. A reply on a `needs-info` discussion during that time is not triaged again. A run that reaches the $5 stop before the agent applies an outcome adds `ready-for-human`, so the sweep does not send it again.

The federation rule and the workflow must change together. If the job leaves the `claude` environment, or runs on another branch or event, the token exchange fails and every triage run fails.
