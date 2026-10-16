---
status: accepted
date: 2026-10-16
---

# Discussion triage runs on a capped API key

Triage runs `claude-code-action` with an Anthropic API key from its own Console workspace. The workspace has a monthly spend limit of $120, and auto-reload is off, so a month of triage cannot cost more than that and never causes a charge. Each run also stops at $5. In five dry runs with repro on open bug reports (2026-10-16), a run cost $0.52 to $0.74, so only a runaway run reaches the stop.

Anyone who opens a discussion starts a triage run. The Anthropic terms say subscription limits assume ordinary, individual usage, and they forbid making an account available to other people. An API key falls under the Commercial Terms, which have no such limits.

## Considered options

- Keep the subscription token (ADR 0001): kept as a fallback. The repository variable `TRIAGE_AUTH=oauth` selects it, and in that mode triage does not run code.
- A GitHub App for the bot identity: rejected. The Anthropic terms follow the model credential, not the GitHub identity. An App adds a long-lived private key and gives only a bot name and a higher rate limit, which triage does not need.

## Consequences

When the workspace reaches its cap, triage runs fail until the credits renew. The daily sweep then sends the untriaged discussions again. A reply on a `needs-info` discussion during that time is not triaged again. A run that reaches the $5 stop before the agent applies an outcome adds `ready-for-human`, so the sweep does not send it again.
