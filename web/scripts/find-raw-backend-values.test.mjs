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

function scan(t, source) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), "qui-raw-backend-values-"))
  t.after(() => fs.rmSync(root, { recursive: true, force: true }))
  fs.mkdirSync(path.join(root, "scripts"))
  fs.mkdirSync(path.join(root, "src"))
  const script = path.join(root, "scripts", "find-raw-backend-values.mjs")
  fs.copyFileSync(scanner, script)
  fs.writeFileSync(path.join(root, "src", "Example.tsx"), source)
  const result = spawnSync(process.execPath, [script, "--strict"], { encoding: "utf8", timeout: 10_000 })
  assert.ifError(result.error)
  return result
}

test("ignores a raw value that is only a key prop", (t) => {
  const result = scan(t, "<Row key={notice.kind} label={t(\"row\")} />\n<li key={`${issue.instanceId}-${issue.kind}`}>\n")

  assert.equal(result.status, 0, result.stdout + result.stderr)
})

test("still reports a raw value rendered beside a key prop", (t) => {
  const result = scan(t, "<span key={notice.kind}>{notice.kind}</span>\n")

  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /Line 1: \[RAW_BACKEND_VALUE\] \(kind\)/)
  assert.match(result.stdout, /Code: <span key=\{notice.kind\}>\{notice.kind\}<\/span>/)
})

test("still reports key={...} that a tag renders as text", (t) => {
  const result = scan(t, "<span>key={notice.kind}</span>\n")

  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /Line 1: \[RAW_BACKEND_VALUE\] \(kind\)/)
})

test("still reports key={...} after a space in tag text", (t) => {
  const result = scan(t, "<span> key={notice.kind}</span>\n")

  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /Line 1: \[RAW_BACKEND_VALUE\] \(kind\)/)
})

test("still reports key={...} on its own line between tags", (t) => {
  const result = scan(t, "<span>\n  key={notice.kind}\n</span>\n")

  assert.equal(result.status, 1, result.stdout + result.stderr)
  assert.match(result.stdout, /Line 2: \[RAW_BACKEND_VALUE\] \(kind\)/)
})

test("ignores a key prop after an arrow function prop", (t) => {
  const result = scan(t, "<Row onOpen={() => open(notice)} key={notice.kind} />\n")

  assert.equal(result.status, 0, result.stdout + result.stderr)
})

test("ignores a key prop after a comparison or a quoted bracket", (t) => {
  const result = scan(t, "<Row hidden={count > 0} key={notice.kind} />\n<Row title=\"a > b\" key={notice.kind} />\n")

  assert.equal(result.status, 0, result.stdout + result.stderr)
})
