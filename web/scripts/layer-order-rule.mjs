import path from "node:path"

import { localImports } from "./local-imports.mjs"

function layerOf(root, layers, file) {
  const [first] = path.relative(root, file).split(path.sep)
  return layers.includes(first) ? first : undefined
}

export const layerOrder = {
  meta: {
    type: "problem",
    schema: [{
      type: "object",
      properties: {
        root: { type: "string" },
        layers: { type: "array", items: { type: "string" }, uniqueItems: true },
      },
      required: ["root", "layers"],
      additionalProperties: false,
    }],
    messages: {
      upward: "src/{{from}} may not import from src/{{to}}. The folders of web/src have an order, and a file imports only from its own folder and the folders before it: {{order}}. An import against the order lets two folders import each other, and it pulls screen code into a helper. Fix the cause: move the imported code down into a folder that both files may import, move this file up into the folder where its job belongs, or take the value as a parameter from the caller.",
    },
  },
  create(context) {
    const [{ root, layers }] = context.options
    const from = layerOf(root, layers, context.filename)
    if (!from) return {}
    return localImports(context, root, (node, target) => {
      const to = layerOf(root, layers, target)
      if (to && layers.indexOf(to) > layers.indexOf(from)) {
        context.report({ node, messageId: "upward", data: { from, to, order: layers.join(", ") } })
      }
    })
  },
}
