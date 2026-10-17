// Runs one discussion triage session with the Claude Agent SDK. The workflow
// calls this script instead of claude-code-action, so the run counts as Agent
// SDK usage. See docs/adr/0017-discussion-triage-runs-on-a-capped-console-workspace.md.
import { appendFileSync, mkdirSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { query } from "@anthropic-ai/claude-agent-sdk";

const env = { ...process.env };
// Only this script fetches GitHub OIDC tokens. Claude must not mint them.
const oidcUrl = env.ACTIONS_ID_TOKEN_REQUEST_URL;
const oidcToken = env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;
delete env.ACTIONS_ID_TOKEN_REQUEST_URL;
delete env.ACTIONS_ID_TOKEN_REQUEST_TOKEN;

const oauth = Boolean(env.CLAUDE_CODE_OAUTH_TOKEN);
const identityDir = join(env.RUNNER_TEMP, "triage-identity");
let refresh;
if (oauth) {
  // Without a rule ID, Claude Code does not try federation.
  delete env.ANTHROPIC_FEDERATION_RULE_ID;
} else {
  delete env.CLAUDE_CODE_OAUTH_TOKEN;
  // Workload identity federation. Claude Code exchanges the GitHub OIDC token
  // in this file for an Anthropic token, and reads the file again when that
  // token expires. A GitHub OIDC token expires after a few minutes and the
  // exchange accepts it once, so write a new one every 4 minutes.
  mkdirSync(identityDir, { recursive: true, mode: 0o700 });
  env.ANTHROPIC_IDENTITY_TOKEN_FILE = join(identityDir, "token");
  const writeToken = async () => {
    const res = await fetch(`${oidcUrl}&audience=https://api.anthropic.com`, {
      headers: { Authorization: `bearer ${oidcToken}` },
    });
    if (!res.ok) throw new Error(`GitHub OIDC token request failed: ${res.status}`);
    writeFileSync(env.ANTHROPIC_IDENTITY_TOKEN_FILE, (await res.json()).value, { mode: 0o600 });
  };
  await writeToken();
  refresh = setInterval(() => writeToken().catch((err) => console.warn(err.message)), 4 * 60 * 1000);
}

let result;
try {
  for await (const message of query({
    prompt: env.TRIAGE_PROMPT,
    options: {
      model: "claude-opus-5-5",
      systemPrompt: { type: "preset", preset: "claude_code" },
      // Loads AGENTS.md, which holds the privacy rules for public comments.
      settingSources: ["project"],
      permissionMode: "dontAsk",
      // The only built-in tools that exist in the run. Some tools, such as
      // Agent, need no permission, so allowedTools alone does not remove them.
      tools: ["Read", "Grep", "Glob", "Bash"],
      allowedTools: [
        "Read",
        "Grep",
        "Glob",
        "Bash(gh label list:*)",
        "Bash(gh release list:*)",
        "Bash(gh search:*)",
        "Bash(gh issue list:*)",
        "Bash(gh issue view:*)",
        "Bash(gh discussion list:*)",
        "Bash(gh discussion view:*)",
        "Bash(./.github/scripts/discussion-write.sh:*)",
        "mcp__github__get_discussion",
        "mcp__github__get_discussion_comments",
        "mcp__github__list_discussions",
      ],
      // A prompt injection could make Claude post a credential in a comment.
      // Hide the GitHub OIDC token and the Anthropic token that Claude Code
      // caches after the exchange. Read rules also apply to Grep and Glob.
      disallowedTools: [`Read(/${identityDir}/**)`, `Read(/${join(homedir(), ".config/anthropic")}/**)`],
      mcpServers: {
        github: {
          command: "docker",
          args: ["run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server:v0.26.3"],
          env: { GITHUB_PERSONAL_ACCESS_TOKEN: env.GH_TOKEN },
        },
      },
      // The subscription has its own limits. Only Console usage costs money.
      maxBudgetUsd: oauth ? undefined : 5,
      env,
    },
  })) {
    if (message.type === "assistant") {
      for (const block of message.message.content) {
        if (block.type === "text") console.log(block.text);
        if (block.type === "tool_use") console.log(`> ${block.name} ${JSON.stringify(block.input)}`);
      }
    }
    if (message.type === "result") {
      result = message;
      // Write the output at once: the SDK can throw after an error result.
      appendFileSync(env.GITHUB_OUTPUT, `subtype=${result.subtype}\n`);
    }
  }
} catch (err) {
  console.error(err.message);
  process.exitCode = 1;
} finally {
  clearInterval(refresh);
}

if (!result) process.exit(1);
console.log(`result: ${result.subtype}, ${result.num_turns} turns, $${result.total_cost_usd.toFixed(2)}`);
if (result.subtype !== "success" || result.is_error) process.exit(1);
