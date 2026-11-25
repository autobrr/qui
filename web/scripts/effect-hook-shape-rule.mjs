import { effectCallee } from "./no-direct-effect-rule.mjs"

const functions = new Set(["ArrowFunctionExpression", "FunctionExpression", "FunctionDeclaration"])

export const effectHookShape = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      second: "A hook in effectHooks hides one effect behind one named job, and this file already has an effect. If both effects do the same job, write them as one effect. If this one does a second job, give it a hook in its own file, and add that file to effectHooks in web/eslint.config.js with its job.",
      cleanup: "The effect of a hook in effectHooks cleans up, and this one returns nothing. Write the effect as an inline function with a block body, and return a function from it that undoes what the effect set up: clear the timer, remove the listener, or put the old value back.",
    },
  },
  create(context) {
    const { sourceCode } = context
    const cleans = new WeakSet()
    let effects = 0
    return {
      "ReturnStatement[argument]"(node) {
        cleans.add(sourceCode.getAncestors(node).findLast(({ type }) => functions.has(type)))
      },
      "CallExpression:exit"(node) {
        const callee = effectCallee(sourceCode, node)
        if (!callee) return
        effects += 1
        if (effects > 1) context.report({ node: callee, messageId: "second" })
        const [effect] = node.arguments
        if (!effect || !cleans.has(effect)) context.report({ node: callee, messageId: "cleanup" })
      },
    }
  },
}
