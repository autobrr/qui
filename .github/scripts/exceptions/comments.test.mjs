import assert from "node:assert/strict"
import { test } from "node:test"
import { list } from "./comments.mjs"

const cases = [
  ["a regex literal with a backtick", "a.ts", "const tick = /`/\n// eslint-disable-next-line no-console -- x\nconsole.log(1)\n", [[2, "// eslint-disable-next-line no-console -- x"]]],
  ["a division after n++", "a.ts", "let n = 0\nconst half = n++ / 2 // eslint-disable-line no-plusplus -- x\n", [[2, "// eslint-disable-line no-plusplus -- x"]]],
  ["a keyword-named property before a division", "a.ts", "const half: any = obj.return / 2 // eslint-disable-line @typescript-eslint/no-explicit-any -- x\n", [[1, "// eslint-disable-line @typescript-eslint/no-explicit-any -- x"]]],
  ["the identifier of before a division", "a.ts", "const y = x.of / 2 // @ts-ignore\nconst z = of / 2 // @ts-ignore\n", [[1, "// @ts-ignore"], [2, "// @ts-ignore"]]],
  ["a comment in a template substitution", "a.ts", "const t = `a ${\n// eslint-disable-next-line no-console -- x\nf()} b`\n", [[2, "// eslint-disable-next-line no-console -- x"]]],
  ["directive text in a template", "a.ts", "const help = `\n// eslint-disable-next-line no-console -- x\n`\n", []],
  ["a continued string", "a.js", "const text = \"the help says \\\nand then \\\n// eslint-disable-line no-console\"\n", []],
  ["jsx text with an apostrophe", "a.tsx", "const view = <div>Don't use C:\\\n{/* eslint-disable-next-line no-console -- x */}\n<p>press ` to search</p></div>\n", [[2, "/* eslint-disable-next-line no-console -- x */"]]],
  ["jsx text that looks like a comment", "a.tsx", "const view = <p>// eslint-disable-next-line no-console</p>\n", []],
  ["a doc comment with a ts directive", "a.ts", "/** @ts-expect-error the type is wrong */\nconst s: string = 1\n/// @ts-ignore\nconst t: string = 1\n", [[1, "/** @ts-expect-error the type is wrong */"], [3, "/// @ts-ignore"]]],
  ["a string that names a directive", "a.ts", "const name = \"// @ts-ignore\"\n", []],
  ["a block comment over several lines", "a.js", "/*\neslint-disable no-console -- x\n*/\nconsole.log(1)\n", [[1, "/*\neslint-disable no-console -- x\n*/"]]],
  ["jsx in a js file", "a.js", "const view = <p>// @ts-ignore</p> // eslint-disable-line no-console -- x\n", [[1, "// eslint-disable-line no-console -- x"]]],
]

for (const [label, name, text, want] of cases) {
  test(label, () => {
    const got = list({ name, text })
    assert.deepEqual(got.comments.map((comment) => [comment.line, comment.text]), want)
    assert.equal(got.broken, false)
  })
}

test("a syntax error marks the source as broken", () => {
  assert.equal(list({ name: "a.ts", text: "const = = 1 // @ts-ignore\n" }).broken, true)
})

test("an unclosed template marks the source as broken", () => {
  assert.equal(list({ name: "a.ts", text: "const help = `\n// @ts-ignore\n" }).broken, true)
})
