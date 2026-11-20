import assert from "node:assert/strict"
import test from "node:test"

import { blocks, sameCode, strip } from "./comments.mjs"

const shape = found => found.map(({ line, end, trailing, text }) => ({ line, end, trailing, text }))

test("blocks groups the lines of one comment and skips each directive", () => {
  const source = [
    "/*",
    " * Copyright (c) 2025-2026, s0up and the autobrr contributors.",
    " */",
    "// one",
    "// two",
    "const a = 1 // three",
    "// eslint-disable-next-line no-console",
    "console.log(a)",
    "/** @deprecated use b */",
    "export const c = a",
    "",
  ].join("\n")

  assert.deepEqual(shape(blocks("web/src/a.ts", source)), [
    { line: 4, end: 5, trailing: false, text: "// one\n// two" },
    { line: 6, end: 6, trailing: true, text: "// three" },
  ])
})

test("strip deletes the named blocks and the blank line at the edge of a block", () => {
  const source = "function f() {\n  // first\n\n  g() // call\n\n  // keep\n  g()\n}\n"
  const { output, missed } = strip("web/src/a.ts", source, [{ line: 2, text: "// first" }, { line: 4, text: "// call" }])

  assert.deepEqual(missed, [])
  assert.equal(output, "function f() {\n  g()\n\n  // keep\n  g()\n}\n")
})

test("strip deletes a JSX comment with its braces", () => {
  const source = "export const A = () => (\n  <div>\n    {/* the title */}\n    <h1>qui</h1>\n    <p>{/* empty */}</p>\n  </div>\n)\n"
  const { output } = strip("web/src/a.tsx", source, [{ line: 3, text: "/* the title */" }, { line: 5, text: "/* empty */" }])

  assert.equal(output, "export const A = () => (\n  <div>\n    <h1>qui</h1>\n    <p></p>\n  </div>\n)\n")
})

test("strip refuses a JSX comment that separates two texts", () => {
  const source = "export const A = () => (\n  <p>\n    left\n    {/* gap */}\n    right\n  </p>\n)\n"

  assert.throws(() => strip("web/src/a.tsx", source, [{ line: 4, text: "/* gap */" }]), /the code changed/)
})

test("strip deletes each of two equal comments on one line", () => {
  const source = "export const n = /* same */ 1 + /* same */ 2\n"
  const { output, missed } = strip("web/src/a.ts", source, [{ line: 1, text: "/* same */" }, { line: 1, text: "/* same */" }])

  assert.deepEqual(missed, [])
  assert.equal(output, "export const n = 1 + 2\n")
})

test("strip refuses a deletion that joins two type names", () => {
  const source = "type Model = { a: number }\ntype keyofModel = boolean\nexport type Keys = keyof/* note */Model\n"

  assert.throws(() => strip("web/src/a.ts", source, [{ line: 3, text: "/* note */" }]), /the code changed/)
})

test("strip skips a block whose text moved", () => {
  const source = "// now different\nexport const a = 1\n"
  const doomed = [{ line: 1, text: "// old text" }]

  assert.deepEqual(strip("web/src/a.ts", source, doomed), { output: source, missed: doomed })
})

test("sameCode ignores comments and sees a changed value", () => {
  assert.equal(sameCode("web/src/a.ts", "// one\nexport const a = 1 // two\n", "export const a = 1\n"), true)
  assert.equal(sameCode("web/src/a.ts", "export const a = 1\n", "export const a = 2\n"), false)
  assert.equal(sameCode("web/src/a.d.ts", "// one\ndeclare const a: number\n", "declare const a: number\n"), true)
  assert.equal(sameCode("web/src/a.d.ts", "declare const a: number\n", "declare const a: string\n"), false)
})
