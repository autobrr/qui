import fs from "node:fs"
import path from "node:path"

import ts from "typescript"
import tseslint from "typescript-eslint"

import { commentDirective } from "./no-comments-rule.mjs"

const scriptPath = fs.realpathSync(import.meta.filename)
const webRoot = path.resolve(path.dirname(scriptPath), "..")
const suppressionsPath = path.join(webRoot, "eslint-suppressions.json")

function emptyJsxContainers(node, visitorKeys, found = []) {
  if (node.type === "JSXExpressionContainer" && node.expression.type === "JSXEmptyExpression") found.push(node.range)
  for (const key of visitorKeys[node.type] ?? []) {
    for (const child of [node[key]].flat()) {
      if (child) emptyJsxContainers(child, visitorKeys, found)
    }
  }
  return found
}

function bannedComments(file, source) {
  const { ast, visitorKeys } = tseslint.parser.parseForESLint(source, {
    filePath: file,
    comment: true,
    loc: true,
    range: true,
    ecmaFeatures: { jsx: /\.[jt]sx$/.test(file) },
  })
  const containers = emptyJsxContainers(ast, visitorKeys)
  const inside = (comment, [start, end]) => start < comment.range[0] && comment.range[1] < end

  return ast.comments.filter(comment => !commentDirective.test(comment.value)).map(comment => {
    const container = containers.find(range => inside(comment, range))
    const fillsContainer = container && ast.comments.filter(other => inside(other, container)).length === 1
    const [start, end] = fillsContainer ? container : comment.range
    return {
      start,
      end,
      line: comment.loc.start.line,
      endLine: comment.loc.end.line,
      text: comment.type === "Line" ? `//${comment.value}` : `/*${comment.value}*/`,
    }
  })
}

function cut(source, { start, end }) {
  const lineStart = start === 0 ? 0 : source.lastIndexOf("\n", start - 1) + 1
  const newline = source.indexOf("\n", end)
  const lineEnd = newline < 0 ? source.length : newline + 1
  const before = source.slice(lineStart, start)
  const after = source.slice(end, lineEnd)

  if (before.trim() === "" && after.trim() === "") return { start: lineStart, end: lineEnd, wholeLines: true }
  if (after.trim() === "") return { start: lineStart + before.trimEnd().length, end, wholeLines: false }
  return { start, end: end + after.length - after.trimStart().length, wholeLines: false }
}

export function blocks(file, source) {
  const found = []
  for (const comment of bannedComments(file, source)) {
    const alone = cut(source, comment).wholeLines
    const last = found.at(-1)
    if (last && alone && !last.trailing && last.end + 1 === comment.line) {
      last.end = comment.endLine
      last.text += `\n${comment.text}`
      last.comments.push(comment)
      continue
    }
    found.push({ file, line: comment.line, end: comment.endLine, trailing: !alone, text: comment.text, comments: [comment] })
  }
  return found
}

const opensBlock = line => /[{([:>]$/.test(line)
const closesBlock = line => /^([})\]]|<\/)/.test(line)

function lineAt(source, pos) {
  if (pos >= source.length) return { text: "}", end: pos }
  const newline = source.indexOf("\n", pos)
  return newline < 0 ? { text: source.slice(pos).trim(), end: source.length } : { text: source.slice(pos, newline).trim(), end: newline + 1 }
}

function closeGap(kept, source, pos) {
  const prev = kept === "" ? "{" : kept.slice(kept.lastIndexOf("\n", kept.length - 2) + 1).trim()
  const next = lineAt(source, pos)
  const after = lineAt(source, next.end)

  if (next.text === "" && (prev === "" || opensBlock(prev) || closesBlock(after.text))) return { kept, pos: next.end }
  if (prev === "" && closesBlock(next.text)) return { kept: kept.slice(0, kept.lastIndexOf("\n", kept.length - 2) + 1), pos }
  return { kept, pos }
}

function remove(source, spans) {
  let kept = ""
  let pos = 0
  for (const span of spans) {
    kept += source.slice(pos, span.start)
    pos = span.end
    if (span.wholeLines) ({ kept, pos } = closeGap(kept, source, pos))
  }
  return kept + source.slice(pos)
}

function tokens(file, source) {
  const { ast } = tseslint.parser.parseForESLint(source, { filePath: file, tokens: true, ecmaFeatures: { jsx: /\.[jt]sx$/.test(file) } })
  const found = []
  for (const { type, value } of ast.tokens) {
    if (type === "JSXText") continue
    if (value === "}" && found.at(-1) === "Punctuator:{") {
      found.pop()
      continue
    }
    found.push(`${type}:${value}`)
  }
  return found.join("\n")
}

function emitted(file, source) {
  if (file.endsWith(".d.ts")) return ""
  return ts.transpileModule(source, {
    fileName: file,
    compilerOptions: { removeComments: true, jsx: ts.JsxEmit.ReactJSX, target: ts.ScriptTarget.ESNext, module: ts.ModuleKind.ESNext },
  }).outputText
}

export function sameCode(file, before, after) {
  return tokens(file, before) === tokens(file, after) && emitted(file, before) === emitted(file, after)
}

export function strip(file, source, doomed) {
  const current = blocks(file, source)
  const missed = []
  const spans = []
  for (const block of doomed) {
    const index = current.findIndex(candidate => candidate.line === block.line && candidate.text === block.text)
    if (index < 0) {
      missed.push(block)
      continue
    }
    const [match] = current.splice(index, 1)
    spans.push(...match.comments.map(comment => cut(source, comment)))
  }
  const output = remove(source, spans.sort((a, b) => a.start - b.start))
  if (!sameCode(file, source, output)) throw new Error(`${file}: the code changed`)
  return { output, missed }
}

function list() {
  const suppressions = JSON.parse(fs.readFileSync(suppressionsPath, "utf8"))
  for (const [file, rules] of Object.entries(suppressions)) {
    if (!rules["qui/no-comments"]) continue
    for (const { line, end, trailing, text } of blocks(`web/${file}`, fs.readFileSync(path.join(webRoot, file), "utf8"))) {
      console.log(JSON.stringify({ file: `web/${file}`, line, end, trailing, text }))
    }
  }
}

function replaceFile(target, output) {
  const temp = path.join(path.dirname(target), `.${path.basename(target)}.${process.pid}.tmp`)
  try {
    fs.writeFileSync(temp, output, { flush: true })
    fs.chmodSync(temp, fs.statSync(target).mode)
    fs.renameSync(temp, target)
  } finally {
    fs.rmSync(temp, { force: true })
  }
}

function stripListed(listPath) {
  const doomed = new Map()
  for (const line of fs.readFileSync(listPath, "utf8").split("\n")) {
    if (line.trim() === "") continue
    const block = JSON.parse(line)
    if (!block.file.startsWith("web/") || block.file.endsWith(".go")) continue
    doomed.set(block.file, [...(doomed.get(block.file) ?? []), block])
  }

  let deleted = 0
  let skipped = 0
  for (const [file, listed] of doomed) {
    const target = path.join(webRoot, "..", file)
    let result
    try {
      result = strip(file, fs.readFileSync(target, "utf8"), listed)
    } catch (error) {
      console.log(`${error.message}, file left as it is`)
      skipped += listed.length
      continue
    }
    for (const block of result.missed) console.log(`${block.file}:${block.line}: no comment block with this text starts here, skipped`)
    deleted += listed.length - result.missed.length
    skipped += result.missed.length
    replaceFile(target, result.output)
  }
  console.log(`comments: deleted ${deleted} blocks, skipped ${skipped}`)
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const [command, listPath] = process.argv.slice(2)
  if (command === "list" && !listPath) {
    list()
  } else if (command === "strip" && listPath) {
    stripListed(listPath)
  } else {
    console.error("usage:\n  node scripts/comments.mjs list            print each comment block of the linted web files as a JSON line\n  node scripts/comments.mjs strip <file>    delete the web comment blocks that the JSON lines in <file> name")
    process.exit(2)
  }
}
