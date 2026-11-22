const effectHooks = new Set(["useEffect", "useLayoutEffect", "useInsertionEffect"])

function reactImport(scope, name) {
  for (; scope; scope = scope.upper) {
    const variable = scope.set.get(name)
    if (!variable) continue
    const binding = variable.defs.find((definition) => definition.type === "ImportBinding")
    return binding?.parent.source.value === "react" ? binding.node : undefined
  }
}

function memberName({ computed, property }) {
  if (!computed) return property.name
  if (property.type === "Literal") return property.value
  if (property.type === "TemplateLiteral" && property.expressions.length === 0) return property.quasis[0].value.cooked
}

export const noDirectEffect = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      banned: "An effect runs after render, and a wrong one fails without an error: a missed cleanup, a stale value, or a second render for state that render could compute. Do the job without one. Compute a value from props or state during render. Reset state with a key. Answer a click or a change in its handler, and when one event changes several values, change them in one useReducer action. Read an outside store with useSyncExternalStore, and fetch with useQuery. For a delay, a clock, or a screen size, call useDebounce, useDelayedVisibility, useNow, or useMediaQuery from @/hooks. When the job does need an effect, write one hook that hides it behind a named job, and add its file to effectHooks in web/eslint.config.js with that job.",
    },
  },
  create(context) {
    return {
      CallExpression(node) {
        const { callee } = node
        const scope = context.sourceCode.getScope(node)
        if (callee.type === "Identifier") {
          const specifier = reactImport(scope, callee.name)
          if (specifier?.type === "ImportSpecifier" && effectHooks.has(specifier.imported.name)) {
            context.report({ node: callee, messageId: "banned" })
          }
          return
        }
        if (callee.type !== "MemberExpression" || callee.object.type !== "Identifier") return
        if (!effectHooks.has(memberName(callee))) return
        const specifier = reactImport(scope, callee.object.name)
        if (specifier && specifier.type !== "ImportSpecifier") {
          context.report({ node: callee, messageId: "banned" })
        }
      },
    }
  },
}
