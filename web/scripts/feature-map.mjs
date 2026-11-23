import { execFileSync } from "node:child_process"
import fs from "node:fs"
import path from "node:path"
import { parseArgs } from "node:util"
import ts from "typescript"

const scriptPath = fs.realpathSync(import.meta.filename)
const saved = "web/feature-map.json"
const overlays = new Set(["AlertDialogContent", "DialogContent", "DrawerContent", "SheetContent"])

function load(config) {
  const host = {
    ...ts.sys,
    onUnRecoverableConfigFileDiagnostic: (diagnostic) => {
      throw new Error(ts.flattenDiagnosticMessageText(diagnostic.messageText, "\n"))
    },
  }
  const parsed = ts.getParsedCommandLineOfConfigFile(config, {}, host)
  return ts.createProgram({ rootNames: parsed.fileNames, options: parsed.options })
}

function searchOf(checker, type) {
  const search = {}
  for (const property of type.getProperties().toSorted((a, b) => a.name.localeCompare(b.name))) {
    const kind = checker.getNonNullableType(checker.getTypeOfSymbol(property))
    const parts = kind.isUnion() ? kind.types : [kind]
    search[property.name] = parts.every((part) => part.isStringLiteral()) ? parts.map((part) => part.value).sort() : checker.typeToString(kind)
  }
  return search
}

function routeOf(checker, file) {
  const module = checker.getSymbolAtLocation(file)
  const route = module && checker.getExportsOfModule(module).find((symbol) => symbol.name === "Route" && symbol.flags & ts.SymbolFlags.Value)
  if (!route) return undefined
  const type = checker.getTypeOfSymbol(route)
  const typeOf = (owner, name) => {
    const property = owner.getProperty(name)
    if (!property) throw new Error(`${file.fileName}: the type of Route has no "${name}". The feature map reads the id, the address, and the search parameters of a route from the types of the router.`)
    return checker.getTypeOfSymbol(property)
  }
  return {
    id: typeOf(type, "id").value,
    url: typeOf(type, "fullPath").value,
    search: searchOf(checker, typeOf(typeOf(type, "types"), "fullSearchSchema")),
  }
}

function importsOf(program, file) {
  const targets = []
  const add = (specifier) => {
    const resolved = program.getResolvedModuleFromModuleSpecifier(specifier, file)?.resolvedModule
    if (resolved && !resolved.isExternalLibraryImport) targets.push(resolved.resolvedFileName)
  }
  ts.forEachChild(file, function visit(node) {
    if (ts.isImportDeclaration(node) && !node.importClause?.isTypeOnly) add(node.moduleSpecifier)
    else if (ts.isExportDeclaration(node) && node.moduleSpecifier && !node.isTypeOnly) add(node.moduleSpecifier)
    else if (ts.isCallExpression(node) && node.expression.kind === ts.SyntaxKind.ImportKeyword && node.arguments[0] && ts.isStringLiteralLike(node.arguments[0])) add(node.arguments[0])
    ts.forEachChild(node, visit)
  })
  return targets
}

function reach(program, imports, start) {
  const seen = new Set([start.fileName])
  const queue = [start]
  for (const file of queue) {
    if (!imports.has(file.fileName)) imports.set(file.fileName, importsOf(program, file))
    for (const target of imports.get(file.fileName)) {
      const next = program.getSourceFile(target)
      if (seen.has(target) || !next) continue
      seen.add(target)
      queue.push(next)
    }
  }
  return seen
}

function dialogsIn(file) {
  const names = []
  for (const statement of file.statements) {
    const declarations = ts.isFunctionDeclaration(statement) ? [statement] : ts.isVariableStatement(statement) ? statement.declarationList.declarations : []
    for (const declaration of declarations) {
      let overlay = false
      ts.forEachChild(declaration, function visit(node) {
        if ((ts.isJsxOpeningElement(node) || ts.isJsxSelfClosingElement(node)) && overlays.has(node.tagName.getText(file))) overlay = true
        ts.forEachChild(node, visit)
      })
      if (overlay && declaration.name && ts.isIdentifier(declaration.name)) names.push(declaration.name.text)
    }
  }
  return names
}

export function read(config, root) {
  const program = load(config)
  const checker = program.getTypeChecker()
  const name = (fileName) => path.relative(root, fileName).split(path.sep).join("/")
  const imports = new Map()
  const routes = {}
  const loadedBy = {}
  for (const file of program.getSourceFiles()) {
    if (file.isDeclarationFile || program.isSourceFileFromExternalLibrary(file)) continue
    const route = routeOf(checker, file)
    if (!route) continue
    routes[route.id] = { url: route.url, source: name(file.fileName), search: route.search }
    for (const loaded of reach(program, imports, file)) (loadedBy[name(loaded)] ??= []).push(route.id)
  }
  const dialogs = {}
  for (const [fileName, ids] of Object.entries(loadedBy)) {
    for (const dialog of dialogsIn(program.getSourceFile(path.resolve(root, fileName)))) dialogs[`${fileName}#${dialog}`] = { routes: ids.toSorted() }
  }
  return { routes, dialogs, loadedBy }
}

export function merge(found, old) {
  const withSteps = (kind) => Object.fromEntries(Object.keys(found[kind]).sort().map((id) => [id, { ...found[kind][id], steps: old?.[kind]?.[id]?.steps ?? [] }]))
  return { routes: withSteps("routes"), dialogs: withSteps("dialogs") }
}

export function render(map) {
  return `${JSON.stringify(map, null, 2)}\n`
}

function hasSteps(entry) {
  return Array.isArray(entry.steps) && entry.steps.length > 0 && entry.steps.every((step) => typeof step === "string" && step.trim() !== "")
}

function withoutSteps(entries) {
  return Object.keys(entries ?? {}).filter((id) => !hasSteps(entries[id]))
}

export function problems(map, text, base) {
  const list = []
  if (render(map) !== text) {
    list.push(`${saved} does not match the code. The file lists each route and each dialog of the frontend, and this change added, removed, or moved one. Run "pnpm feature-map" in web/. It writes the list again and keeps the steps that are there. Then write the steps of each new entry.`)
  }
  for (const id of withoutSteps(map.routes)) {
    list.push(`${saved}: the route ${id} has no steps. Write in "steps" how a user gets to this page and what the user does there, one action in each string, as the other routes do. Follow the steps on a running qui before you commit them, so that each one is true.`)
  }
  const held = withoutSteps(map.dialogs)
  const before = base && withoutSteps(base.dialogs)
  if (before && held.length > before.length) {
    const fresh = held.filter((id) => !before.includes(id))
    list.push(`${saved}: ${held.length} dialogs have no steps, and the base branch has ${before.length}. The number only shrinks, so a new dialog needs steps. Write in "steps" how a user opens the dialog and what the user does in it, one action in each string, and follow the steps on a running qui first. The dialogs without steps that the base branch does not have:\n${fresh.map((id) => `  ${id}`).join("\n")}`)
  }
  return list
}

export function where(map, loadedBy, file) {
  const ids = loadedBy[file] ?? []
  if (!ids.length) return `No route loads ${file}.`
  const entry = (title, { steps }) => [title, ...(steps.length ? steps : ["no steps yet"]).map((step) => `  - ${step}`)]
  const dialogs = Object.keys(map.dialogs).filter((id) => id.startsWith(`${file}#`))
  return [
    ...ids.toSorted().flatMap((id) => entry(`route ${id}  ${map.routes[id].url}`, map.routes[id])),
    ...dialogs.flatMap((id) => entry(`dialog ${id}`, map.dialogs[id])),
  ].join("\n")
}

export function atBase(root, base) {
  const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8", stdio: ["ignore", "pipe", "ignore"] })
  git("rev-parse", "--verify", `${base}^{commit}`)
  if (!git("ls-tree", "--name-only", base, "--", saved)) {
    console.log(`feature-map: no ${saved} at ${base}, so the number of dialogs without steps has no limit yet`)
    return undefined
  }
  return JSON.parse(git("show", `${base}:${saved}`))
}

if (fs.existsSync(process.argv[1] ?? "") && fs.realpathSync(process.argv[1]) === scriptPath) {
  const { values } = parseArgs({ options: { write: { type: "boolean" }, base: { type: "string" }, where: { type: "string" } } })
  const web = path.resolve(import.meta.dirname, "..")
  const root = path.dirname(web)
  const found = read(path.join(web, "tsconfig.app.json"), root)
  const target = path.join(root, saved)
  const text = fs.existsSync(target) ? fs.readFileSync(target, "utf8") : "{}"
  const map = merge(found, JSON.parse(text))
  if (values.where) {
    console.log(where(map, found.loadedBy, path.relative(root, path.resolve(values.where)).split(path.sep).join("/")))
  } else if (values.write) {
    fs.writeFileSync(target, render(map))
  } else {
    const list = problems(map, text, values.base && atBase(root, values.base))
    for (const problem of list) console.error(`${problem}\n`)
    process.exitCode = list.length ? 1 : 0
  }
}
