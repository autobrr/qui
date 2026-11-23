import assert from "node:assert/strict"
import { execFileSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { after, test } from "node:test"

import { atBase, merge, problems, read, render, where } from "./feature-map.mjs"

const root = fs.mkdtempSync(path.join(os.tmpdir(), "feature-map-"))
after(() => fs.rmSync(root, { recursive: true }))

const config = { compilerOptions: { strict: true, jsx: "preserve", module: "esnext", moduleResolution: "bundler", noEmit: true, paths: { "@/*": ["./src/*"] } }, include: ["src"] }
const router = `export function createFileRoute<const Id extends string>(id: Id) {
  return <Search = object>(options: { validateSearch?: (input: unknown) => Search }) => ({ id, fullPath: id, types: {} as { fullSearchSchema: Search }, options })
}
`

function project(name, files) {
  const dir = path.join(root, name)
  for (const [file, text] of Object.entries({ "tsconfig.json": JSON.stringify(config), "src/router.ts": router, ...files })) {
    fs.mkdirSync(path.dirname(path.join(dir, file)), { recursive: true })
    fs.writeFileSync(path.join(dir, file), text)
  }
  return read(path.join(dir, "tsconfig.json"), dir)
}

test("read lists each route with its address and its search parameters", () => {
  const { routes } = project("routes", {
    "src/routes/home.tsx": "import { createFileRoute } from \"@/router\"\nexport const Route = createFileRoute(\"/home\")({ validateSearch: (input: unknown) => input as { tab?: \"b\" | \"a\"; page?: number; q?: string } })\n",
    "src/routes/plain.tsx": "import { createFileRoute } from \"../router\"\nexport const Route = createFileRoute(\"/plain\")({})\n",
    "src/lib/names.ts": "export type Route = string\nexport const route = \"/not-a-route\"\n",
    "src/declared.d.ts": "export declare const Route: { id: \"/declared\" }\n",
  })
  assert.equal(JSON.stringify(routes), JSON.stringify({
    "/home": { url: "/home", source: "src/routes/home.tsx", search: { page: "number", q: "string", tab: ["a", "b"] } },
    "/plain": { url: "/plain", source: "src/routes/plain.tsx", search: {} },
  }))
})

test("read fails on a Route that the router did not make", () => {
  assert.throws(() => project("broken", { "src/routes/home.tsx": "export const Route = { id: \"/home\" }\n" }), /the type of Route has no "fullPath"/)
})

test("read lists each dialog with the routes that load its file", () => {
  const { dialogs, loadedBy } = project("dialogs", {
    "src/routes/home.tsx": "import { createFileRoute } from \"@/router\"\nimport { Shared } from \"@/components/Shared\"\nimport type { Props } from \"@/components/TypeOnly\"\nexport const Route = createFileRoute(\"/home\")({})\nexport const page = (props: Props) => <Shared {...props} />\n",
    "src/routes/settings.tsx": "import { createFileRoute } from \"@/router\"\nimport { Shared } from \"@/components/Shared\"\nexport { Moved } from \"@/components/Moved\"\nexport type { Props } from \"@/components/TypeOnly\"\nexport const Route = createFileRoute(\"/settings\")({})\nexport const load = () => import(\"@/components/Lazy\")\nexport const page = <Shared />\n",
    "src/components/Shared.tsx": "import { helper } from \"./helper\"\nexport function Shared() { return <DialogContent>{helper}</DialogContent> }\nexport const Second = () => <div><SheetContent /></div>, plain = () => <div />\nexport function Card() { return <CardContent /> }\n",
    "src/components/helper.ts": "export const helper = 1\n",
    "src/components/Moved.tsx": "export const Moved = () => <DrawerContent />\n",
    "src/components/Lazy.tsx": "export default function LazyDialog() { return <AlertDialogContent /> }\n",
    "src/components/TypeOnly.tsx": "export type Props = object\nexport function Hidden() { return <DialogContent /> }\n",
    "src/components/Unused.tsx": "export function Unused() { return <DialogContent /> }\n",
  })
  assert.deepEqual(dialogs, {
    "src/components/Shared.tsx#Shared": { routes: ["/home", "/settings"] },
    "src/components/Shared.tsx#Second": { routes: ["/home", "/settings"] },
    "src/components/Moved.tsx#Moved": { routes: ["/settings"] },
    "src/components/Lazy.tsx#LazyDialog": { routes: ["/settings"] },
  })
  assert.deepEqual(loadedBy["src/components/helper.ts"], ["/home", "/settings"])
  assert.equal(loadedBy["src/components/TypeOnly.tsx"], undefined)
})

const found = {
  routes: { "/b": { url: "/b", source: "b.tsx", search: {} }, "/a": { url: "/a", source: "a.tsx", search: {} } },
  dialogs: { "d.tsx#Two": { routes: ["/a"] }, "d.tsx#One": { routes: ["/a", "/b"] } },
}
const steps = ["Open /a.", "Click Save."]

test("merge keeps the steps of each entry that is still there, in the order of the ids", () => {
  const old = { routes: { "/a": { url: "/old", steps }, "/gone": { steps } }, dialogs: { "d.tsx#One": { routes: [], steps } } }
  assert.equal(render(merge(found, old)), render({
    routes: { "/a": { url: "/a", source: "a.tsx", search: {}, steps }, "/b": { url: "/b", source: "b.tsx", search: {}, steps: [] } },
    dialogs: { "d.tsx#One": { routes: ["/a", "/b"], steps }, "d.tsx#Two": { routes: ["/a"], steps: [] } },
  }))
  assert.equal(render(merge(found, {})), render(merge(found, undefined)))
})

function mapWith(routeSteps, dialogSteps) {
  return merge(found, {
    routes: { "/a": { steps: routeSteps }, "/b": { steps } },
    dialogs: { "d.tsx#One": { steps: dialogSteps }, "d.tsx#Two": { steps: [] } },
  })
}

test("problems is empty for a current map with steps on each route", () => {
  const map = mapWith(steps, [])
  assert.deepEqual(problems(map, render(map), undefined), [])
  assert.deepEqual(problems(map, render(map), map), [])
})

test("problems reports a saved file that does not match the code", () => {
  const map = mapWith(steps, [])
  const [problem, ...rest] = problems(map, render({ ...map, dialogs: {} }), undefined)
  assert.match(problem, /^web\/feature-map\.json does not match the code\..*Run "pnpm feature-map" in web\//)
  assert.deepEqual(rest, [])
})

test("problems reports each route without steps", () => {
  for (const bad of [[], [""], ["Open /a.", " "], "Open /a.", undefined, ["Open /a.", 3]]) {
    const map = mapWith(bad, [])
    const list = problems(map, render(map), undefined)
    assert.equal(list.length, 1, JSON.stringify(bad))
    assert.match(list[0], /^web\/feature-map\.json: the route \/a has no steps\./)
  }
})

test("problems reports more dialogs without steps than the base branch has", () => {
  const map = mapWith(steps, [])
  const base = { dialogs: { "d.tsx#One": { steps: [] }, "d.tsx#Old": { steps }, "d.tsx#Renamed": { steps: [] } } }
  assert.deepEqual(problems(map, render(map), base), [])
  const [problem, ...rest] = problems(map, render(map), { dialogs: { "d.tsx#One": { steps: [] }, "d.tsx#Two": { steps } } })
  assert.match(problem, /^web\/feature-map\.json: 2 dialogs have no steps, and the base branch has 1\./)
  assert.match(problem, /does not have:\n {2}d\.tsx#Two$/)
  assert.deepEqual(rest, [])
  assert.equal(problems(map, render(map), {}).length, 1)
  assert.deepEqual(problems(mapWith(steps, steps), render(mapWith(steps, steps)), { dialogs: { "d.tsx#One": { steps: [] } } }), [])
})

test("where prints the steps of each route that loads a file, and of the dialogs in it", () => {
  const map = mapWith(steps, [])
  const loadedBy = { "d.tsx": ["/b", "/a"], "d.ts": ["/a"] }
  assert.equal(where(map, loadedBy, "d.tsx"), [
    "route /a  /a", "  - Open /a.", "  - Click Save.",
    "route /b  /b", "  - Open /a.", "  - Click Save.",
    "dialog d.tsx#One", "  - no steps yet",
    "dialog d.tsx#Two", "  - no steps yet",
  ].join("\n"))
  assert.equal(where(mapWith(steps, steps), loadedBy, "d.ts"), ["route /a  /a", "  - Open /a.", "  - Click Save."].join("\n"))
  assert.match(where(mapWith(steps, steps), loadedBy, "d.tsx"), /dialog d\.tsx#One\n {2}- Open \/a\.\n/)
  assert.equal(where(map, loadedBy, "gone.tsx"), "No route loads gone.tsx.")
})

test("atBase reads the map at a commit, and fails on a map that it cannot read", () => {
  const repo = path.join(root, "git")
  const file = path.join(repo, "web", "feature-map.json")
  fs.mkdirSync(path.dirname(file), { recursive: true })
  const env = { ...process.env, GIT_CONFIG_GLOBAL: os.devNull, GIT_CONFIG_NOSYSTEM: "1", GIT_AUTHOR_NAME: "test", GIT_AUTHOR_EMAIL: "test@example.invalid", GIT_COMMITTER_NAME: "test", GIT_COMMITTER_EMAIL: "test@example.invalid" }
  const git = (...args) => execFileSync("git", args, { cwd: repo, env, encoding: "utf8" }).trim()
  const commit = (text) => {
    if (text === undefined) fs.rmSync(file, { force: true })
    else fs.writeFileSync(file, text)
    git("add", "--all")
    git("commit", "--allow-empty", "--quiet", "--message", "map")
    return git("rev-parse", "HEAD")
  }
  git("init", "--quiet", "--initial-branch=main")
  const none = commit(undefined)
  const good = commit(render({ dialogs: {} }))
  const bad = commit("{")
  assert.equal(atBase(repo, none), undefined)
  assert.deepEqual(atBase(repo, good), { dialogs: {} })
  assert.throws(() => atBase(repo, bad), SyntaxError)
})

test("the script runs its command line when node starts it", () => {
  assert.throws(() => execFileSync(process.execPath, [path.join(import.meta.dirname, "feature-map.mjs"), "--unknown"], { stdio: "pipe" }), /Unknown option '--unknown'/)
})
