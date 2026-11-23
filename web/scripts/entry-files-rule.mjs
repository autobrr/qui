import fs from "node:fs"
import path from "node:path"

import { localImports } from "./local-imports.mjs"

const extensions = [".mjs", ".js", ".mts", ".ts", ".jsx", ".tsx", ".json"]

function placeOf(root, features, file) {
  const [first, feature, ...inside] = path.relative(root, file).split(path.sep)
  return first === features && inside.length > 0 ? { feature, inside } : undefined
}

export const entryFiles = {
  meta: {
    type: "problem",
    schema: [{
      type: "object",
      properties: {
        root: { type: "string" },
        features: { type: "string" },
      },
      required: ["root", "features"],
      additionalProperties: false,
    }],
    messages: {
      inner: "src/{{target}} is in a subfolder of src/{{feature}}, and this file is outside that feature. The files at the root of a feature folder are its entry files, and code outside the feature imports only those. The subfolders are the inside of the feature, so a change there needs no search through the rest of the app. Fix the cause: if code outside the feature needs this file, move the file to the root of src/{{feature}}. If it needs only a part of the file, move that part to a file at the root. Add no file that only re-exports it.",
    },
  },
  create(context) {
    const [{ root, features }] = context.options
    const own = placeOf(root, features, context.filename)?.feature
    return localImports(context, root, (node, target) => {
      const to = placeOf(root, features, target)
      if (!to || to.feature === own) return
      const folder = fs.statSync(target, { throwIfNoEntry: false })?.isDirectory() && !extensions.some((extension) => fs.existsSync(target + extension))
      if (to.inside.length > 1 || folder) {
        context.report({ node, messageId: "inner", data: { target: path.relative(root, target).split(path.sep).join("/"), feature: `${features}/${to.feature}` } })
      }
    })
  },
}
