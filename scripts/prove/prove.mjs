#!/usr/bin/env node
// prove.mjs <scenario> <base commit> <head commit>
// prove.mjs <scenario> <commit>          run one side only, while you write a scenario
//
// Builds qui at each commit (build.sh keeps the binary), runs the scenario on each build,
// and prints one verdict. Exit 0: proven. Exit 1: not proven. Exit 2: could not run.
// Each side runs in its own loopback-only network namespace with a stub qBittorrent.
// Needs the global Playwright (PW, or found through mise) and /usr/bin/chromium (or CHROMIUM).
import { execFileSync, spawn } from "node:child_process"
import { existsSync, readFileSync, realpathSync, writeFileSync } from "node:fs"
import { basename, dirname, join, relative, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const HERE = dirname(fileURLToPath(import.meta.url))
const [scenarioArg, ...commits] = process.argv.slice(2)
if (!scenarioArg || commits.length < 1 || commits.length > 2) {
  console.error("usage: prove.mjs <scenario> <base commit> <head commit>")
  process.exit(2)
}
const scenario = resolve(scenarioArg)
const sides = commits.length === 2 ? ["base", "head"] : ["head"]
const started = Date.now()
const elapsed = () => `${((Date.now() - started) / 1000).toFixed(0)}s`

function playwrightDir() {
  if (process.env.PW) return process.env.PW
  for (const [cmd, args] of [["mise", ["which", "playwright"]], ["sh", ["-c", "command -v playwright"]]]) {
    try {
      const bin = execFileSync(cmd, args, { encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] }).trim()
      return join(dirname(realpathSync(bin)), "..", "playwright")
    } catch { /* try the next lookup */ }
  }
  return null
}

const active = new Set()
// A cancel stops the builds and the runners too. Each runner then stops its own stub, qui, and browser.
for (const sig of ["SIGINT", "SIGTERM"]) {
  process.on(sig, async () => {
    const closing = [...active].map(c => new Promise(r => { c.once("close", r); c.kill("SIGTERM") }))
    await Promise.all(closing)
    process.exit(2)
  })
}

function run(cmd, args, opts = {}) {
  return new Promise(res => {
    const child = spawn(cmd, args, { stdio: ["ignore", "pipe", "pipe"], ...opts })
    active.add(child)
    child.on("close", () => active.delete(child))
    let stdout = "", stderr = ""
    child.stdout.on("data", d => { stdout += d })
    child.stderr.on("data", d => { stderr += d })
    child.on("error", err => res({ code: -1, stdout, stderr: String(err) }))
    // "close", not "exit": the output streams may still hold data at "exit".
    child.on("close", code => res({ code, stdout: stdout.trim(), stderr: stderr.trim() }))
  })
}

const PW = playwrightDir()
if (!PW || !existsSync(join(PW, "index.mjs"))) {
  console.error(`could not run: no Playwright package${PW ? ` at ${PW}` : " found"}. Set PW to its folder.`)
  process.exit(2)
}
const cache = process.env.QUI_PROVE_CACHE || join(process.env.XDG_CACHE_HOME || join(process.env.HOME, ".cache"), "qui-prove")
const runDir = join(cache, "runs", `${basename(scenario, ".mjs")}-${new Date().toISOString().replace(/[:.]/g, "-")}`)

const results = await Promise.all(sides.map(async (side, i) => {
  const commit = commits[i]
  const build = await run(join(HERE, "build.sh"), [commit])
  if (build.code !== 0) return { side, commit, result: "error", message: `build failed: ${build.stderr}`, shots: [] }
  console.log(`[${elapsed()}] ${side} ${commit} built`)
  const out = join(runDir, side)
  // A loopback-only network namespace: the run can reach nothing but itself.
  const runner = await run("unshare", ["-rn", "sh", "-c", 'ip link set lo up && exec node "$@"', "sh",
    join(HERE, "run.mjs"), scenario, build.stdout, out], { env: { ...process.env, PW } })
  const file = join(out, "result.json")
  const r = existsSync(file)
    ? JSON.parse(readFileSync(file, "utf8"))
    : { result: "error", message: `the runner wrote no result (exit ${runner.code}):\n${runner.stderr}`, shots: [] }
  console.log(`[${elapsed()}] ${side} ${commit}: ${r.result}`)
  return { side, commit, out, ...r }
}))

const by = Object.fromEntries(results.map(r => [r.side, r]))
let verdict, code
if (results.some(r => r.result === "error")) [verdict, code] = ["COULD NOT RUN", 2]
else if (sides.length === 1) [verdict, code] = [by.head.result === "pass" ? "PASSES" : "FAILS", by.head.result === "pass" ? 0 : 1]
else if (by.base.result === "fail" && by.head.result === "pass") [verdict, code] = ["PROVEN", 0]
else [verdict, code] = ["NOT PROVEN", 1]

console.log("")
for (const r of results) console.log(`${r.side.padEnd(4)} ${r.commit.padEnd(12)} ${r.result.padEnd(5)} ${r.message.split("\n")[0]}`)
for (const r of results.filter(r => r.result === "error")) console.log(`\n${r.side} error:\n${r.message}`)

const shot = (r, label) => r.shots.find(s => s.label === label)?.file
const labels = [...new Set(results.flatMap(r => r.shots.map(s => s.label)))]
if (labels.length) {
  console.log("\nscreenshots:")
  for (const label of labels) {
    console.log(`  ${label}`)
    for (const r of results) console.log(`    ${r.side}: ${shot(r, label) ?? "(none)"}`)
  }
  const cell = (r, label) => `<td>${shot(r, label) ? `<img src="${relative(runDir, shot(r, label))}">` : "(none)"}</td>`
  writeFileSync(join(runDir, "pairs.html"), `<!doctype html><meta charset="utf-8"><title>${basename(scenario)}</title>
<style>body{font:14px sans-serif;margin:16px}img{max-width:100%}td{vertical-align:top;width:50%}</style>
<h1>${basename(scenario)}: ${verdict}</h1><table><tr>${results.map(r => `<th>${r.side} ${r.commit}: ${r.result}</th>`).join("")}</tr>
${labels.map(l => `<tr><th colspan="${results.length}">${l}</th></tr><tr>${results.map(r => cell(r, l)).join("")}</tr>`).join("\n")}</table>\n`)
  console.log(`  side by side: ${join(runDir, "pairs.html")}`)
}
console.log(`\n${verdict} (${elapsed()})`)
process.exit(code)
