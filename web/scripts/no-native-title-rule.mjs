const forwardsToDom = /^(@\/components\/ui\/|@radix-ui\/|@tanstack\/react-router$)/
const ownTitleProp = new Set(["MultiSelect", "ResponsiveCommandPopover"])

function importBinding(scope, name) {
  for (; scope; scope = scope.upper) {
    const variable = scope.set.get(name)
    if (variable) return variable.defs.find((definition) => definition.type === "ImportBinding")
  }
}

export const noNativeTitle = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      banned: "A title attribute shows the tooltip of the browser, which never shows on a touch screen and does not look like qui. Use Tooltip from @/components/ui/tooltip, or show no tooltip. An icon-only control keeps its name in aria-label.",
    },
  },
  create(context) {
    return {
      JSXAttribute(node) {
        if (node.name.type !== "JSXIdentifier" || node.name.name !== "title") return
        const element = node.parent.name
        const root = element.type === "JSXMemberExpression" ? element.object : element
        if (root.type !== "JSXIdentifier") return
        const intrinsic = element.type === "JSXIdentifier" && /^[a-z]/.test(element.name)
        const binding = importBinding(context.sourceCode.getScope(node), root.name)
        const component = element.type === "JSXMemberExpression" ? element.property.name : binding?.node.imported?.name
        const domComponent = !ownTitleProp.has(component) && forwardsToDom.test(binding?.parent.source.value ?? "")
        if (intrinsic || domComponent) {
          context.report({ node, messageId: "banned" })
        }
      },
    }
  },
}
