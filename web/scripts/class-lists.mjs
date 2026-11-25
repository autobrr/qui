import { JSDOM } from "jsdom"
import { execFileSync } from "node:child_process"
import fs from "node:fs"
import path from "node:path"
import { parseArgs } from "node:util"
import ts from "typescript"
import { createServer, normalizePath } from "vite"

const scriptPath = fs.realpathSync(import.meta.filename)
const primitiveFolder = "@/components/ui/"
const optionProps = ["variant", "size"]
const mergeFunctions = new Set(["cn", "clsx", "twMerge"])

class Undecided extends Error {
  constructor(condition) {
    super(condition)
    this.condition = condition
  }
}

function eachBranch(run, decided = new Map()) {
  try {
    return [run(decided)]
  } catch (error) {
    if (!(error instanceof Undecided)) throw error
    return [true, false].flatMap((value) => eachBranch(run, new Map(decided).set(error.condition, value)))
  }
}

function holds(node, decided) {
  if (ts.isParenthesizedExpression(node)) return holds(node.expression, decided)
  if (ts.isPrefixUnaryExpression(node) && node.operator === ts.SyntaxKind.ExclamationToken) return !holds(node.operand, decided)
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) return holds(node.left, decided) && holds(node.right, decided)
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.BarBarToken) return holds(node.left, decided) || holds(node.right, decided)
  const condition = definedText(node)
  if (!decided.has(condition)) throw new Undecided(condition)
  return decided.get(condition)
}

function bindsName(binding, name) {
  if (ts.isIdentifier(binding)) return binding.text === name
  return binding.elements.some((element) => ts.isBindingElement(element) && bindsName(element.name, name))
}

function bindingsIn(scope) {
  if (ts.isFunctionLike(scope)) return scope.parameters
  if (ts.isForStatement(scope) || ts.isForOfStatement(scope) || ts.isForInStatement(scope)) return scope.initializer?.declarations ?? []
  if (ts.isCatchClause(scope)) return scope.variableDeclaration ? [scope.variableDeclaration] : []
  if (!ts.isSourceFile(scope) && !ts.isBlock(scope) && !ts.isCaseOrDefaultClause(scope)) return []
  return scope.statements.flatMap((statement) => {
    if (ts.isVariableStatement(statement)) return statement.declarationList.declarations
    return ts.isFunctionDeclaration(statement) && statement.name ? [statement] : []
  })
}

function definitionOf(identifier) {
  for (let scope = identifier.parent; scope; scope = scope.parent) {
    const binding = bindingsIn(scope).find((each) => bindsName(each.name, identifier.text))
    if (binding) return binding
  }
}

function constantOf(identifier) {
  const definition = definitionOf(identifier)
  const isConstant = definition && ts.isVariableDeclaration(definition) && ts.isVariableStatement(definition.parent.parent)
    && definition.parent.flags & ts.NodeFlags.Const && ts.isIdentifier(definition.name)
  return isConstant ? definition.initializer : undefined
}

function definedText(node, text = node.getText()) {
  const definitions = new Set()
  const collect = (each) => {
    if (ts.isIdentifier(each)) {
      const definition = definitionOf(each)
      if (definition && !definitions.has(definition)) {
        definitions.add(definition)
        collect(definition)
      }
    }
    ts.forEachChild(each, collect)
  }
  collect(node)
  return [text, ...[...definitions].map((each) => each.getText())].join("\n")
}

function classParts(node, decided) {
  const unread = [{ unread: definedText(node) }]
  if (ts.isStringLiteralLike(node)) return [node.text]
  if (ts.isParenthesizedExpression(node)) return classParts(node.expression, decided)
  if (node.kind === ts.SyntaxKind.NullKeyword) return [null]
  if (node.kind === ts.SyntaxKind.FalseKeyword || node.getText() === "undefined") return []
  if (ts.isConditionalExpression(node)) return classParts(holds(node.condition, decided) ? node.whenTrue : node.whenFalse, decided)
  if (ts.isBinaryExpression(node) && node.operatorToken.kind === ts.SyntaxKind.AmpersandAmpersandToken) return holds(node.left, decided) ? classParts(node.right, decided) : []
  if (ts.isCallExpression(node) && mergeFunctions.has(node.expression.getText())) return node.arguments.flatMap((argument) => classParts(argument, decided))
  if (ts.isIdentifier(node)) {
    const constant = constantOf(node)
    return constant ? classParts(constant, decided) : unread
  }
  if (ts.isTemplateExpression(node)) {
    const texts = [node.head.text, ...node.templateSpans.map((span) => span.literal.text)]
    const separate = texts.every((text, index) => (index === 0 || /^(\s|$)/.test(text)) && (index === texts.length - 1 || /(^|\s)$/.test(text)))
    if (!separate || texts.slice(1, -1).some((text) => text === "")) return unread
    return [node.head.text, ...node.templateSpans.flatMap((span) => [...classParts(span.expression, decided), span.literal.text])]
  }
  return unread
}

function propsOf(element) {
  const props = new Map()
  for (const attribute of element.attributes.properties) {
    if (ts.isJsxAttribute(attribute)) {
      const value = attribute.initializer
      const node = value && ts.isJsxExpression(value) ? value.expression : value
      props.set(attribute.name.getText(), { node, text: node?.getText() })
      continue
    }
    for (const name of [...optionProps, "className"]) {
      const earlier = props.get(name)
      const text = `${attribute.expression.getText()}.${name}`
      const unread = definedText(attribute.expression, text)
      props.set(name, earlier
        ? { text: `${earlier.text}\n${text}`, unread: `${earlier.unread ?? definedText(earlier.node)}\n${unread}` }
        : { text, unread })
    }
  }
  return props
}

function optionOf(prop, decided) {
  if (!prop) return { value: undefined }
  if (prop.unread) return prop
  const parts = classParts(prop.node, decided)
  return parts[0]?.unread ? parts[0] : { value: parts[0] }
}

function segmentsOf(prop, decided) {
  if (!prop) return { texts: [""], unread: [] }
  const parts = prop.unread ? [{ unread: prop.unread }] : classParts(prop.node, decided)
  const texts = [""]
  const unread = []
  for (const part of parts) {
    if (part?.unread) {
      unread.push(part.unread)
      texts.push("")
    } else {
      texts[texts.length - 1] += ` ${part ?? ""}`
    }
  }
  return { texts, unread }
}

function elementsOf({ source, primitives }) {
  const file = ts.createSourceFile("file.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const local = new Map()
  for (const statement of file.statements) {
    if (!ts.isImportDeclaration(statement) || !statement.moduleSpecifier.text.startsWith(primitiveFolder)) continue
    const named = statement.importClause?.namedBindings
    if (!named || !ts.isNamedImports(named)) continue
    for (const each of named.elements) {
      const name = (each.propertyName ?? each.name).text
      if (primitives[name]) local.set(each.name.text, name)
    }
  }
  const elements = []
  ts.forEachChild(file, function visit(node) {
    if ((ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) && local.has(node.tagName.getText())) {
      const props = propsOf(node)
      const text = [...optionProps, "className"].map((name) => props.get(name)?.text ?? "").join("\n")
      elements.push({ name: local.get(node.tagName.getText()), props, text, line: file.getLineAndCharacterOfPosition(node.getStart()).line + 1 })
    }
    ts.forEachChild(node, visit)
  })
  return Object.groupBy(elements, ({ name }) => name)
}

function optionValues(before, after, primitives) {
  if (!before.unread && !after.unread) return [[before.value, after.value]]
  if (before.unread !== after.unread) return []
  return [undefined, ...primitives].map((value) => [value, value])
}

function listsOf(element, options, decided, { primitives, cn }) {
  const { texts, unread } = segmentsOf(element.props.get("className"), decided)
  const [first, ...rest] = texts
  const merged = [primitives[element.name].classes({ ...options, className: first }), ...rest.map((text) => cn(text))]
  return { lists: merged.map((text) => text.split(/\s+/).filter(Boolean).sort()), unread }
}

function compareBranch(old, now, before, after, decided) {
  const [variants, sizes] = optionProps.map((prop) => optionValues(
    optionOf(old.props.get(prop), decided),
    optionOf(now.props.get(prop), decided),
    before.primitives[old.name].options[prop] ?? []
  ))
  const pairs = variants.flatMap((variant) => sizes.map((size) => [variant, size]))
  if (!pairs.length) return { lost: ["an option that the script cannot read"], gained: [] }
  for (const [variant, size] of pairs) {
    const left = listsOf(old, { variant: variant[0], size: size[0] }, decided, before)
    const right = listsOf(now, { variant: variant[1], size: size[1] }, decided, after)
    if (JSON.stringify(left) === JSON.stringify(right)) continue
    const classes = ({ lists, unread }) => [...lists.flat(), ...unread]
    return {
      lost: classes(left).filter((name) => !classes(right).includes(name)),
      gained: classes(right).filter((name) => !classes(left).includes(name)),
    }
  }
}

export function compare(before, after) {
  const [olds, nows] = [elementsOf(before), elementsOf(after)]
  return Object.keys({ ...olds, ...nows }).flatMap((name) => {
    const [old, now] = [olds[name] ?? [], nows[name] ?? []]
    if (old.length !== now.length) throw new Error(`${name}: ${old.length} before, ${now.length} after. The script pairs the elements by their order.`)
    return old.map((element, index) => {
      const difference = eachBranch((decided) => compareBranch(element, now[index], before, after, decided)).find(Boolean)
      return { name, line: now[index].line, changed: element.text !== now[index].text, same: !difference, difference }
    })
  })
}

function optionsIn(source) {
  const options = {}
  const file = ts.createSourceFile("file.tsx", source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  ts.forEachChild(file, function visit(node) {
    if (ts.isPropertyAssignment(node) && node.name.getText() === "variants" && ts.isObjectLiteralExpression(node.initializer)) {
      for (const option of node.initializer.properties) options[option.name.getText()] = option.initializer.properties.map((value) => value.name.getText())
    }
    ts.forEachChild(node, visit)
  })
  return options
}

async function primitivesAt(web, read, names) {
  const src = normalizePath(path.join(web, "src"))
  const server = await createServer({
    root: web,
    configFile: false,
    logLevel: "error",
    appType: "custom",
    server: { middlewareMode: true, hmr: false, watch: null },
    optimizeDeps: { noDiscovery: true },
    resolve: { alias: { "@": src } },
    plugins: [{
      name: "sources",
      enforce: "pre",
      load: (id) => id.startsWith(src) && /\.tsx?$/.test(id) ? read(path.relative(web, id)) : undefined,
    }],
  })
  try {
    const { cn } = await server.ssrLoadModule("/src/lib/utils.ts")
    const primitives = {}
    for (const [name, file] of Object.entries(names)) {
      const component = (await server.ssrLoadModule(`/${file}`))[name]
      const render = component.render ?? component
      primitives[name] = { options: optionsIn(read(file)), classes: (props) => render(props, null).props.className }
    }
    return { cn, primitives }
  } finally {
    await server.close()
  }
}

async function main(givenBase, names) {
  const { window } = new JSDOM("", { url: "http://localhost/" })
  Object.assign(globalThis, { window, document: window.document, localStorage: window.localStorage })
  const web = path.resolve(import.meta.dirname, "..")
  const git = (...args) => execFileSync("git", args, { cwd: web, encoding: "utf8", maxBuffer: 1 << 28 })
  const base = givenBase ?? git("merge-base", "origin/develop", "HEAD").trim()
  const atBase = (file) => git("show", `${base}:./${file.split(path.sep).join("/")}`)
  const now = (file) => fs.readFileSync(path.join(web, file), "utf8")
  const files = git("ls-tree", "-r", "--name-only", base, "--", "src").split("\n").filter((file) => file.endsWith(".tsx") && fs.existsSync(path.join(web, file)))
  const before = await primitivesAt(web, atBase, names)
  const after = await primitivesAt(web, now, names)
  const results = files.flatMap((file) => {
    try {
      return compare({ ...before, source: atBase(file) }, { ...after, source: now(file) }).map((result) => ({ file, ...result }))
    } catch (error) {
      throw new Error(`${file}: ${error.message}`, { cause: error })
    }
  })
  for (const name of Object.keys(names)) {
    const mine = results.filter((result) => result.name === name)
    const changed = mine.filter((result) => result.changed)
    console.log(`${name}: ${mine.length} elements, ${changed.length} changed, ${mine.filter((result) => !result.same).length} with another merged class list`)
  }
  const other = results.filter((result) => !result.same)
  for (const { file, line, name, difference } of other) console.error(`${file}:${line} <${name}> lost: ${difference.lost.join(" ") || "none"}; gained: ${difference.gained.join(" ") || "none"}`)
  return { results, other }
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const { values, positionals } = parseArgs({ allowPositionals: true, options: { base: { type: "string" }, changed: { type: "boolean" } } })
  const names = Object.fromEntries(positionals.map((each) => {
    const [name, file] = each.split("=")
    return [name, `src/components/ui/${file}.tsx`]
  }))
  const { results, other } = await main(values.base, names)
  if (values.changed) for (const { file, line, name } of results.filter((result) => result.changed)) console.log(`${file}:${line} <${name}>`)
  process.exitCode = other.length ? 1 : 0
}
