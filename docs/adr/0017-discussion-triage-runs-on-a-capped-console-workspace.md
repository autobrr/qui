---
status: accepted
date: 2026-10-16
---

# Discussion triage runs on a capped Console workspace

Triage runs `claude-code-action` in its own Claude Console workspace. The action signs in with workload identity federation: it exchanges the GitHub OIDC token of the job for an Anthropic token that expires in 10 minutes. The federation rule in the Console matches only the `claude` environment of `autobrr/qui`, on `develop`, from `workflow_dispatch`. A token from the rule can spend only in the triage workspace. GitHub stores no Anthropic key, so a run has no long-lived key to leak.

The workspace has a monthly spend limit of $120, and auto-reload is off, so a month of triage cannot cost more than that. Each run also stops at $5. In five dry runs with repro on open bug reports (2026-10-16), a run cost $0.52 to $0.74, so only a runaway run reaches the stop.

Anyone who opens a discussion starts a triage run. The Anthropic terms say subscription limits assume ordinary, individual usage, and they forbid making an account available to other people. Console usage falls under the Commercial Terms, which have no such limits.

## Considered options

- Keep the subscription token (ADR 0001): kept as a fallback. The repository variable `TRIAGE_AUTH=oauth` selects it, and in that mode triage does not run code.
- An API key in the `claude` environment: rejected. A key does not expire, so a leaked key works until someone revokes it.
- The monthly API credits of a Max plan: rejected. Anthropic counts runs that the Claude Code GitHub Action starts as Claude Code usage, and the credits do not cover Claude Code. The workspace pays with purchased credits.
- A GitHub App for the bot identity: rejected. The Anthropic terms follow the model credential, not the GitHub identity. An App adds a long-lived private key and gives only a bot name and a higher rate limit, which triage does not need.

## Consequences

When the workspace reaches its cap or the organization runs out of credits, triage runs fail. The daily sweep then sends the untriaged discussions again. A reply on a `needs-info` discussion during that time is not triaged again. A run that reaches the $5 stop before the agent applies an outcome adds `ready-for-human`, so the sweep does not send it again.

The federation rule and the workflow must change together. If the job leaves the `claude` environment, or runs on another branch or event, the token exchange fails and every triage run fails.
