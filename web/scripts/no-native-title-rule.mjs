const forwardsToDom = /^(@\/components\/ui\/|@radix-ui\/|@tanstack\/react-router$)/
const ownTitleProp = new Set(["MultiSelect", "ResponsiveCommandPopover"])

export const noNativeTitle = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      banned: "A title attribute shows the tooltip of the browser, which never shows on a touch screen and does not look like qui. Use Tooltip from @/components/ui/tooltip, or show no tooltip. An icon-only control keeps its name in aria-label.",
    },
  },
  create(context) {
    const importedFrom = new Map()
    return {
      ImportDeclaration(node) {
        for (const specifier of node.specifiers) {
          importedFrom.set(specifier.local.name, node.source.value)
        }
      },
      JSXAttribute(node) {
        if (node.name.type !== "JSXIdentifier" || node.name.name !== "title") return
        const element = node.parent.name
        const root = element.type === "JSXMemberExpression" ? element.object : element
        if (root.type !== "JSXIdentifier") return
        const intrinsic = element.type === "JSXIdentifier" && /^[a-z]/.test(element.name)
        const domComponent = !ownTitleProp.has(root.name) && forwardsToDom.test(importedFrom.get(root.name) ?? "")
        if (intrinsic || domComponent) {
          context.report({ node, messageId: "banned" })
        }
      },
    }
  },
}
