// run.mjs <scenario> <qui binary> <out dir>
//
// Runs one scenario against one qui binary and writes <out dir>/result.json:
// {"result": "pass" | "fail" | "error", "message": ..., "shots": [{"label", "file"}]}.
// "fail" means the scenario's assertion failed. Every other problem is "error".
// prove.mjs starts it in a loopback-only network namespace, so its ports clash with nothing.
import assert from "node:assert/strict"
import { spawn } from "node:child_process"
import { randomBytes } from "node:crypto"
import { mkdirSync, openSync, writeFileSync } from "node:fs"
import { createServer } from "node:net"
import { dirname, join, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { inspect } from "node:util"

const oneLine = v => inspect(v, { depth: 6, breakLength: Infinity })

const HERE = dirname(fileURLToPath(import.meta.url))
const [scenarioPath, quiBin, out] = process.argv.slice(2).map(p => resolve(p))
const USER = { username: "prove", password: "provepassword123" }
mkdirSync(out, { recursive: true })

const children = []
const shots = []
let browser
let page

function freePort() {
  return new Promise((res, rej) => {
    const s = createServer().listen(0, "127.0.0.1", () => { const { port } = s.address(); s.close(() => res(port)) })
    s.on("error", rej)
  })
}

function start(name, cmd, args, env = {}) {
  const log = openSync(join(out, `${name}.log`), "w")
  // An inherited QUI__ setting, such as QUI__DATA_DIR, could point qui at a real installation.
  const inherited = Object.fromEntries(Object.entries(process.env).filter(([k]) => !k.startsWith("QUI__")))
  const child = spawn(cmd, args, { detached: true, stdio: ["ignore", log, log], env: { ...inherited, ...env } })
  children.push(child)
  return child
}

function stopAll() {
  for (const c of children) {
    try { process.kill(-c.pid, "SIGTERM") } catch { /* already gone */ }
  }
}
// The children run in their own process groups, so stop them on every way out, a crash included.
process.on("exit", stopAll)
for (const sig of ["SIGINT", "SIGTERM"]) process.on(sig, () => process.exit(1))

async function waitFor(check, what, ms = 60000) {
  const end = Date.now() + ms
  for (;;) {
    try {
      const v = await check()
      if (v) return v
    } catch { /* not ready */ }
    if (Date.now() > end) throw new Error(`timed out after ${ms} ms waiting for ${what}`)
    await new Promise(r => setTimeout(r, 250))
  }
}

async function rig() {
  const stubPort = await freePort()
  const quiPort = await freePort()
  const stubURL = `http://127.0.0.1:${stubPort}`
  const base = `http://127.0.0.1:${quiPort}`

  start("stub", "python3", [join(HERE, "stub-qbit.py"), String(stubPort)])
  const qui = start("qui", quiBin, ["serve", "--config-dir", join(out, "config")], {
    QUI__HOST: "127.0.0.1", QUI__PORT: String(quiPort), QUI__CHECK_FOR_UPDATES: "false",
    QUI__SESSION_SECRET: randomBytes(24).toString("hex"),
  })
  let quiExit = null
  qui.on("exit", code => { quiExit = code })
  qui.on("error", err => { quiExit = String(err) })
  await waitFor(async () => (await fetch(`${stubURL}/_calls`)).ok, "the stub qBittorrent", 15000)
  await waitFor(async () => {
    if (quiExit !== null) throw new Error(`qui exited with code ${quiExit}; see qui.log`)
    return (await fetch(`${base}/`)).ok
  }, "qui to serve")

  const { chromium } = await import(process.env.PW + "/index.mjs")
  browser = await chromium.launch({ executablePath: process.env.CHROMIUM || "/usr/bin/chromium" })
  const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 }, colorScheme: "dark", serviceWorkers: "block" })
  page = await ctx.newPage()
  page.on("dialog", d => d.dismiss())

  // ctx.request shares the cookie jar of the browser, so the sign-in below also signs in the page.
  async function api(method, path, body) {
    const r = await ctx.request.fetch(base + path, {
      method, data: body, headers: { "X-Requested-With": "XMLHttpRequest" },
    })
    if (!r.ok()) throw new Error(`${method} ${path}: ${r.status()} ${(await r.text()).slice(0, 300)}`)
    const text = await r.text()
    return text ? JSON.parse(text) : null
  }
  await api("POST", "/api/auth/setup", USER)
  // The auth routes answer 429 to a second request within a second.
  await waitFor(() => api("POST", "/api/auth/login", USER).then(() => true), "the sign-in", 30000)
  const instance = await api("POST", "/api/instances", {
    name: "Stub", host: stubURL, username: "admin", password: "adminadmin",
  })
  // The "Group your cross-seeds" tip covers the top right of the torrent list in screenshots.
  await api("PUT", "/api/client-settings", { "qui-feature-notice-seen:group-copies": "true" })

  let asserted = false
  const checked = new Proxy(assert, {
    get(target, key) {
      const v = target[key]
      return typeof v === "function" ? (...a) => { asserted = true; return v(...a) } : v
    },
    apply(target, self, args) {
      asserted = true
      return Reflect.apply(target, self, args)
    },
  })

  const t = {
    page, api, waitFor,
    instanceId: instance.id,
    assert: checked,
    goto: path => page.goto(base + path),
    stub: {
      seed: async seed => {
        const r = await fetch(`${stubURL}/_seed`, { method: "POST", body: JSON.stringify(seed) })
        return r.json()
      },
      calls: async path => (await (await fetch(`${stubURL}/_calls`)).json()).filter(c => !path || c.path === path),
    },
    shot: async label => {
      const file = join(out, `${label.replace(/[^\w.-]+/g, "-")}.png`)
      // Let dialogs and menus finish their open and close animations. A spinner loops forever, so skip those.
      await page.evaluate(() => Promise.race([
        Promise.all(document.getAnimations()
          .filter(a => a.effect?.getTiming().iterations !== Infinity)
          .map(a => a.finished.catch(() => {}))),
        new Promise(r => setTimeout(r, 2000)),
      ]))
      await page.screenshot({ path: file })
      shots.push({ label, file })
    },
    // Waits until the instance has synced at least `count` torrents from the stub.
    synced: (count = 1) => waitFor(async () => {
      const r = await api("GET", `/api/instances/${instance.id}/torrents?limit=1`)
      return r.total >= count
    }, `qui to sync ${count} torrents from the stub`),
  }
  return { t, asserted: () => asserted }
}

let result
try {
  const scenario = (await import(scenarioPath)).default
  const { t, asserted } = await rig()
  await scenario(t)
  result = asserted()
    ? { result: "pass", message: "the assertion held" }
    : { result: "error", message: "the scenario ended without an assertion" }
} catch (err) {
  result = err instanceof assert.AssertionError
    ? { result: "fail", message: `expected ${oneLine(err.expected)}, got ${oneLine(err.actual)}` }
    : { result: "error", message: String(err?.stack || err) }
  // Shows where an error stopped the run. A failed assertion has its own screenshots.
  if (result.result === "error") await page?.screenshot({ path: join(out, "error.png") }).catch(() => {})
} finally {
  await browser?.close().catch(() => {})
  stopAll()
}
writeFileSync(join(out, "result.json"), JSON.stringify({ ...result, shots }, null, 2))
console.log(`${result.result}: ${result.message.split("\n")[0]}`)
