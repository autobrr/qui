---
status: accepted
date: 2026-08-29
---

# Discussion triage runs on claude-code-action with the Claude subscription

The triage workflow (`.github/workflows/triage.yml`) runs `anthropics/claude-code-action` with a subscription OAuth token, so every triage run is covered by the Claude subscription that is already paid for. The playbook, the write helper, the labels, and the sweep are model-agnostic; only the action step and its MCP setup name Claude.

## Considered options

- **`openai/codex-action`**: rejected. Its only auth input is `openai-api-key`, which the action feeds through a Responses API proxy. There is no ChatGPT subscription login, so each triage run would bill per token on the OpenAI API, on top of any ChatGPT plan. Checked against the README on 2026-08-29.
- **`claude-code-action` with `anthropic_api_key`**: not taken. Same action, per-token billing.

## Consequences

If the Claude subscription ends, the workflow stops until the action step is switched to `anthropic_api_key`, and the sweep pace in `triage.yml` must drop before the cron fires again. A port to Codex rewrites the action step, the MCP setup step, and the tool allowlist as a permission profile, at API cost either way.
