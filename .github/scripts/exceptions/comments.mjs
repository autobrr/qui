import { readFileSync } from "node:fs"
import { forEachComment } from "ts-api-utils"
import ts from "typescript"

const kinds = {
  ".ts": ts.ScriptKind.TS,
  ".mts": ts.ScriptKind.TS,
  ".cts": ts.ScriptKind.TS,
  ".tsx": ts.ScriptKind.TSX,
  ".jsx": ts.ScriptKind.JSX,
}

export function list({ name, text }) {
  const kind = kinds[name.slice(name.lastIndexOf("."))] ?? ts.ScriptKind.JS
  const file = ts.createSourceFile(name, text, ts.ScriptTarget.Latest, true, kind)
  const comments = []
  forEachComment(file, (full, range) => {
    comments.push({ line: file.getLineAndCharacterOfPosition(range.pos).line + 1, text: full.slice(range.pos, range.end) })
  })
  return { comments, broken: file.parseDiagnostics.length > 0 }
}

if (import.meta.main) {
  const sources = JSON.parse(readFileSync(0, "utf8"))
  process.stdout.write(JSON.stringify(sources.map(list)))
}
