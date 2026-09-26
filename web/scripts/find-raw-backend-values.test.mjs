import assert from "node:assert/strict"
import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import test from "node:test"

const scanner = new URL("./find-raw-backend-values.mjs", import.meta.url)

test("reports a line once when it renders the same raw value twice", (t) => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-raw-backend-values-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  fs.mkdirSync(path.join(root, "scripts"))
  fs.mkdirSync(path.join(root, "src"))
  const script = path.join(root, "scripts", "find-raw-backend-values.mjs")
  fs.copyFileSync(scanner, script)
  fs.writeFileSync(
    path.join(root, "src", "Example.tsx"),
    "<span>{item.status}</span><span>{item.status}</span>\n"
  )

  const result = spawnSync(process.execPath, [script, "--strict"], { encoding: "utf8", timeout: 10_000 })

  assert.ifError(result.error)
  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.equal(result.stdout.match(/Line 1:/g)?.length, 1, result.stdout)
  assert.match(result.stdout, /Found 1 potential raw backend value/)
})
