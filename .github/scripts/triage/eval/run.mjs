// Replays frozen discussions through the triage playbook and checks each outcome.
//
// Usage: node run.mjs <evals-dir> [--case <id>]... [--ref <git-ref>] [--runs 2] [--jobs 2] [--claude <path>] [--qbit <dir>] [--keep]
//
// <evals-dir> holds cases/*.json and corpus/ (from snapshot.mjs). Each run uses
// the Claude Code CLI on the signed-in subscription: --claude, or claude on
// PATH. Its Bash checks change between versions, so give it the version that
// the pinned Agent SDK in .github/scripts/triage bundles. The playbook, AGENTS.md,
// the triage scripts and the prompt come from this checkout, or from --ref.
// The discussion-write.sh stand-in and the Bash permissions always come from
// this checkout, so a --ref from before #4224 fails its writes.
// --jobs is the most Claude sessions that run at the same time. --qbit is a
// qBittorrent clone with origin/master and the release tags, for the
// qBittorrent source reads (default ~/github/oss/qBittorrent).
// Results go to <evals-dir>/results/<time>/.
import { execFileSync, spawn } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, readdirSync, readFileSync, realpathSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { check, verdict } from "./check.mjs";
import { cutWorld, loadCorpus } from "./world.mjs";

const here = dirname(fileURLToPath(import.meta.url));
const repoRoot = execFileSync("git", ["-C", here, "rev-parse", "--show-toplevel"], { encoding: "utf8" }).trim();

const { values: opts, positionals } = parseArgs({
  allowPositionals: true,
  options: {
    case: { type: "string", multiple: true },
    ref: { type: "string" },
    runs: { type: "string", default: "2" },
    jobs: { type: "string", default: "2" },
    claude: { type: "string" },
    qbit: { type: "string" },
    keep: { type: "boolean" },
  },
});
if (positionals.length !== 1) {
  console.error("usage: node run.mjs <evals-dir> [--case <id>]... [--ref <git-ref>] [--runs 2] [--jobs 2] [--claude <path>] [--qbit <dir>] [--keep]");
  process.exit(2);
}
const evalsDir = resolve(positionals[0]);
const qbitDir = resolve(opts.qbit ?? join(homedir(), "github/oss/qBittorrent"));
if (!existsSync(qbitDir)) console.error(`warning: no qBittorrent clone at ${qbitDir}, so each qBittorrent source read fails in the runs (see --qbit)`);
const stamp = new Date().toISOString().replace(/[:.]/g, "-");
const resultsDir = join(evalsDir, "results", stamp);
const workDir = join(homedir(), ".cache/qui-triage-eval", stamp);
mkdirSync(resultsDir, { recursive: true });
mkdirSync(workDir, { recursive: true });

const git = (args, input) => execFileSync("git", ["-C", repoRoot, ...args], { input, maxBuffer: 256 * 1024 * 1024 });

// --- the files that today's triage run reads, laid over the old code ---

const OVERLAY = [
  "AGENTS.md",
  "docs/agents/triage.md",
  ".github/workflows/triage.yml",
  ".github/scripts/qbit-source.sh",
  ".github/scripts/triage/run.mjs",
  ".github/scripts/triage/package.json",
  ".github/scripts/triage/package-lock.json",
];

function readSource(path) {
  if (!opts.ref) return readFileSync(join(repoRoot, path));
  try {
    return git(["show", `${opts.ref}:${path}`]);
  } catch {
    return null;
  }
}

function triagePrompt(number) {
  const yml = readSource(".github/workflows/triage.yml");
  const prompt = execFileSync(
    "python3",
    ["-I", "-c", "import sys,yaml; w=yaml.safe_load(sys.stdin); print(next(s['env']['TRIAGE_PROMPT'] for s in w['jobs']['triage-discussion']['steps'] if s.get('id')=='triage'), end='')"],
    { input: yml, encoding: "utf8" },
  );
  const filled = prompt.replaceAll("${{ inputs.discussion_number }}", String(number)).replaceAll("${{ github.repository }}", "autobrr/qui");
  if (filled.includes("${{")) throw new Error("the triage prompt has an expression that the eval cannot fill");
  return filled;
}

function buildTree(c, dir) {
  const commit = c.commit ?? git(["rev-list", "-1", `--before=${c.at}`, "origin/develop"]).toString().trim();
  mkdirSync(dir, { recursive: true });
  execFileSync("tar", ["-x", "-C", dir], { input: git(["archive", commit]), maxBuffer: 256 * 1024 * 1024 });
  for (const path of OVERLAY) {
    const data = readSource(path);
    if (data === null) continue;
    mkdirSync(dirname(join(dir, path)), { recursive: true });
    writeFileSync(join(dir, path), data);
  }
  rmSync(join(dir, "CLAUDE.md"), { force: true });
  symlinkSync("AGENTS.md", join(dir, "CLAUDE.md"));
  const write = join(dir, ".github/scripts/discussion-write.sh");
  writeFileSync(write, readFileSync(join(here, "discussion-write.sh")));
  chmodSync(write, 0o755);
  chmodSync(join(dir, ".github/scripts/qbit-source.sh"), 0o755);
  return commit;
}

// --- one Claude Code run ---

const claudeBin = opts.claude ? resolve(opts.claude) : execFileSync("sh", ["-c", "command -v claude"], { encoding: "utf8" }).trim();
const claudeVersion = execFileSync(claudeBin, ["--version"], { encoding: "utf8" }).trim();
const ALLOWED = [
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
];
// The same two forms as the triage profile in triage/run.mjs.
const scriptRules = (tree) => ["discussion-write.sh", "qbit-source.sh"].flatMap((s) => [
  `Bash(./.github/scripts/${s}:*)`,
  `Bash(${tree}/.github/scripts/${s}:*)`,
]);

// --setting-sources project still loads the user CLAUDE.md, which a CI run
// does not have.
const userMemory = [process.env.CLAUDE_CONFIG_DIR, join(homedir(), ".claude")].filter(Boolean);
for (const dir of [...userMemory]) {
  const file = join(dir, "CLAUDE.md");
  if (existsSync(file)) userMemory.push(dirname(realpathSync(file)));
}
const claudeSettings = JSON.stringify({ claudeMdExcludes: userMemory.map((d) => `${d}/**`) });

function runClaude({ tree, prompt, binDir, worldFile, writesFile, transcript, discussion }) {
  const deny = [evalsDir, repoRoot, workDir.replace(/\/$/, "") + "/worlds", join(homedir(), ".config/gh"), "/proc"].map((p) => `Read(/${p}/**)`);
  const args = [
    "-p",
    "--model", "claude-opus-5-5",
    "--permission-mode", "dontAsk",
    "--setting-sources", "project",
    "--settings", claudeSettings,
    "--strict-mcp-config",
    "--no-session-persistence",
    "--output-format", "stream-json",
    "--verbose",
    "--tools", "Read,Grep,Glob,Bash",
    "--allowedTools", ...ALLOWED, ...scriptRules(tree),
    "--disallowedTools", ...deny,
  ];
  const env = {
    HOME: homedir(),
    USER: process.env.USER,
    LANG: process.env.LANG ?? "C.UTF-8",
    // The gh stand-in runs on this node, which nvm keeps out of /usr/bin.
    PATH: `${binDir}:${dirname(process.execPath)}:/usr/local/bin:/usr/bin:/bin`,
    GH_CONFIG_DIR: join(binDir, "gh-config"),
    DISCUSSION_WRITE_ALLOWED_OPS: "add-label,remove-label,comment,close,create-issue",
    CLAUDE_CODE_SCRIPT_CAPS: '{"discussion-write.sh": 8}',
    CLAUDE_CODE_SUBPROCESS_ENV_SCRUB: "1",
    QUI_EVAL_WORLD: worldFile,
    QUI_EVAL_WRITES: writesFile,
    QUI_EVAL_DISCUSSION: String(discussion),
  };
  if (process.env.CLAUDE_CONFIG_DIR) env.CLAUDE_CONFIG_DIR = process.env.CLAUDE_CONFIG_DIR;
  const started = Date.now();
  return new Promise((done, fail) => {
    const child = spawn(claudeBin, args, { cwd: tree, env, stdio: ["pipe", "pipe", "pipe"] });
    const out = [];
    const err = [];
    child.stdout.on("data", (b) => out.push(b));
    child.stderr.on("data", (b) => err.push(b));
    const timer = setTimeout(() => child.kill("SIGTERM"), 20 * 60 * 1000);
    child.on("error", (err) => {
      clearTimeout(timer);
      fail(err);
    });
    child.stdin.end(prompt);
    child.on("close", (code) => {
      clearTimeout(timer);
      const text = Buffer.concat(out).toString();
      writeFileSync(transcript, text);
      if (err.length) writeFileSync(`${transcript}.stderr`, Buffer.concat(err));
      const result = text
        .split("\n")
        .filter(Boolean)
        .map((l) => {
          try {
            return JSON.parse(l);
          } catch {
            return null;
          }
        })
        .findLast((m) => m?.type === "result");
      done({ code, result, durationMs: Date.now() - started });
    });
  });
}

// --- cases ---

const corpus = loadCorpus(join(evalsDir, "corpus"));
let cases = readdirSync(join(evalsDir, "cases"))
  .filter((f) => f.endsWith(".json"))
  .map((f) => JSON.parse(readFileSync(join(evalsDir, "cases", f), "utf8")));
if (opts.case) cases = cases.filter((c) => opts.case.includes(c.id));
if (!cases.length) throw new Error("no cases selected");

const binDir = join(workDir, "bin");
mkdirSync(join(binDir, "gh-config"), { recursive: true });
writeFileSync(join(binDir, "gh"), `#!/bin/sh\nexec node ${JSON.stringify(join(here, "gh.mjs"))} "$@"\n`, { mode: 0o755 });

const jobs = Math.max(1, Number(opts.jobs) || 1);
let freeSessions = jobs;
const waiting = [];
async function session(start) {
  if (freeSessions) freeSessions--;
  else await new Promise((wake) => waiting.push(wake));
  try {
    return await start();
  } finally {
    const next = waiting.shift();
    if (next) next();
    else freeSessions++;
  }
}

const qbitCommit = (at) => execFileSync("git", ["-C", qbitDir, "rev-list", "-1", `--before=${at}`, "origin/master"], { encoding: "utf8" }).trim();

// Waits for every run of a case, so that a failed run cannot leave a sibling
// running after the cleanup removes its tree.
async function settleAll(promises) {
  const failed = (await Promise.allSettled(promises)).find((s) => s.status === "rejected");
  if (failed) throw failed.reason;
}

async function runCase(c) {
  const tree = join(workDir, "trees", c.id);
  const commit = buildTree(c, tree);
  const world = cutWorld(corpus, { at: c.at, discussion: c.discussion, labels: c.labels, body: c.body });
  world.code = { "autobrr/qui": { dir: repoRoot, commit } };
  if (existsSync(qbitDir)) world.code["qbittorrent/qbittorrent"] = { dir: qbitDir, commit: qbitCommit(c.at) };
  mkdirSync(join(workDir, "worlds"), { recursive: true });
  const worldFile = join(workDir, "worlds", `${c.id}.json`);
  writeFileSync(worldFile, JSON.stringify(world));
  const prompt = triagePrompt(c.discussion);
  const caseDir = join(resultsDir, c.id);
  mkdirSync(caseDir, { recursive: true });

  const runs = [];
  const one = async () => {
    const n = runs.length + 1;
    const slot = { n };
    runs.push(slot);
    const writesFile = join(caseDir, `run${n}.writes.jsonl`);
    writeFileSync(writesFile, "");
    const r = await session(() => runClaude({ tree, prompt, binDir, worldFile, writesFile, transcript: join(caseDir, `run${n}.jsonl`), discussion: c.discussion }));
    const writes = readFileSync(writesFile, "utf8").split("\n").filter(Boolean).map((l) => JSON.parse(l));
    const outcome = check(c, writes);
    const ok = r.code === 0 && r.result?.subtype === "success" && !r.result.is_error;
    if (!ok) outcome.failures.unshift(`the run did not succeed (exit ${r.code}, result ${r.result?.subtype ?? "none"})`);
    Object.assign(slot, {
      pass: ok && outcome.pass,
      failures: outcome.failures,
      writes,
      subtype: r.result?.subtype ?? null,
      turns: r.result?.num_turns ?? null,
      costUsd: r.result?.total_cost_usd ?? null,
      durationMs: r.durationMs,
      report: r.result?.result ?? null,
    });
    console.log(`${c.id} run ${n}: ${slot.pass ? "pass" : "FAIL"}, ${slot.turns} turns, $${slot.costUsd?.toFixed(2)}${slot.pass ? "" : ` (${slot.failures.join("; ")})`}`);
  };
  const base = Number(opts.runs);
  await settleAll(Array.from({ length: base }, one));
  let v = verdict(runs.map((r) => r.pass));
  if (v === "rerun") {
    await one();
    v = verdict(runs.map((r) => r.pass));
  }
  const summary = { id: c.id, source: c.source, discussion: c.discussion, at: c.at, commit, ref: opts.ref ?? "working tree", verdict: v, runs };
  writeFileSync(join(caseDir, "summary.json"), JSON.stringify(summary, null, 2));
  return summary;
}

const results = [];
const queue = [...cases];
// A case that fails to start is a row in the table, so that the other cases
// finish and keep their results.
const workers = Array.from({ length: jobs }, async () => {
  while (queue.length) {
    const c = queue.shift();
    try {
      results.push(await runCase(c));
    } catch (err) {
      console.error(`${c.id}: ${err.stack}`);
      results.push({ id: c.id, source: c.source, verdict: "error", error: err.message, runs: [] });
    }
  }
});
try {
  await Promise.all(workers);
} finally {
  if (!opts.keep) rmSync(workDir, { recursive: true, force: true });
}

results.sort((a, b) => a.id.localeCompare(b.id));
const fmtRun = (r) => `${r.pass ? "pass" : "fail"} (${r.turns ?? "?"} turns, $${r.costUsd?.toFixed(2) ?? "?"})`;
const table = [
  "| Case | Source | Verdict | Runs |",
  "| --- | --- | --- | --- |",
  ...results.map((s) => `| ${s.id} | ${s.source} | ${s.verdict} | ${s.error ?? s.runs.map(fmtRun).join("; ")} |`),
].join("\n");
writeFileSync(join(resultsDir, "summary.md"), `Playbook: ${opts.ref ?? "working tree"}\nClaude Code: ${claudeVersion}\n\n${table}\n`);
writeFileSync(join(resultsDir, "summary.json"), JSON.stringify(results, null, 2));
console.log(`\nClaude Code: ${claudeVersion}\n\n${table}\n\nresults: ${resultsDir}`);
process.exitCode = results.every((s) => s.verdict === "pass") ? 0 : 1;
