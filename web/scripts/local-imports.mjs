import path from "node:path"

function targetOf(root, file, source) {
  if (source.startsWith("@/")) return path.join(root, source.slice(2))
  if (source.startsWith(".")) return path.resolve(path.dirname(file), source)
}

export function localImports(context, root, visit) {
  const check = (node) => {
    const source = node.source
    const specifier = source?.type === "TemplateLiteral" && source.expressions.length === 0 ? source.quasis[0].value.cooked : source?.value
    if (typeof specifier !== "string") return
    const target = targetOf(root, context.filename, specifier)
    if (target) visit(source, target)
  }
  return {
    ImportDeclaration: check,
    ImportExpression: check,
    ExportNamedDeclaration: check,
    ExportAllDeclaration: check,
    TSImportType: check,
  }
}
