import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { after, test } from "node:test";
import { fileURLToPath } from "node:url";

const gh = join(dirname(fileURLToPath(import.meta.url)), "gh.mjs");

// A code tree with three files: git grep reads a tree as well as a commit.
const dir = mkdtempSync(join(tmpdir(), "eval-gh-test-"));
after(() => rmSync(dir, { recursive: true, force: true }));
const git = (...a) => execFileSync("git", ["-C", dir, ...a], { encoding: "utf8" }).trim();
git("init", "-q");
writeFileSync(join(dir, "alpha.go"), "func alpha() {}\n");
writeFileSync(join(dir, "beta.go"), "func beta() {}\n");
writeFileSync(join(dir, "both.go"), "func alpha() { beta() }\n");
git("add", ".");
const tree = git("write-tree");

const worldFile = join(dir, "world.json");
writeFileSync(
  worldFile,
  JSON.stringify({
    at: "2026-10-01T00:00:00Z",
    discussion: 1,
    labels: [],
    discussions: [1, 2].map((number) => ({
      number,
      title: `report ${number}`,
      body: "",
      author: { login: "reporter" },
      category: { name: "Q&A", slug: "q-a" },
      labels: [],
      comments: { totalCount: 0, nodes: [] },
      closed: false,
      createdAt: "2026-09-01T00:00:00Z",
      updatedAt: "2026-09-01T00:00:00Z",
      answered: number === 1,
    })),
    issues: [],
    pulls: [],
    releases: {},
    code: { "autobrr/qui": { dir, commit: tree } },
  }),
);

function run(...argv) {
  const r = spawnSync(process.execPath, [gh, ...argv], { encoding: "utf8", env: { QUI_EVAL_WORLD: worldFile } });
  return { status: r.status, stdout: r.stdout, stderr: r.stderr };
}

const paths = (r) => r.stdout.split("\n").filter(Boolean).map((l) => l.split(":")[1]);

// regression: #4224
test("search code matches either term of an OR group, and drops excluded files", () => {
  assert.deepEqual(paths(run("search", "code", "alpha OR beta")), ["alpha.go", "beta.go", "both.go"]);
  assert.deepEqual(paths(run("search", "code", "alpha OR beta -beta")), ["alpha.go"]);
  assert.deepEqual(paths(run("search", "code", "alpha beta")), ["both.go"]);
});

// regression: #4224
test("a read of a qBittorrent issue, discussion or label fails, not returns qui data", () => {
  for (const argv of [
    ["issue", "view", "1"],
    ["issue", "list"],
    ["discussion", "view", "1"],
    ["discussion", "list"],
    ["label", "list"],
  ]) {
    const r = run(...argv, "--repo", "qbittorrent/qBittorrent");
    assert.equal(r.status, 1, argv.join(" "));
    assert.match(r.stderr, /no labels, issues or discussions of qbittorrent\/qbittorrent/);
  }
});

// regression: #4224
test("discussion list --answered keeps the answered discussions", () => {
  const r = run("discussion", "list", "--answered", "--json", "number,answered");
  assert.equal(r.status, 0, r.stderr);
  assert.deepEqual(JSON.parse(r.stdout), [{ number: 1, answered: true }]);
});
