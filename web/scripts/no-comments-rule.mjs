export const commentDirective = /^(\s*eslint-(disable|enable)\b|\s*@ts-(expect-error|nocheck)\b|\/\s*<reference\s|\s*i18n-namespace:\s|\s*regression: #\d+|[\s*]*Copyright \(c\) |[\s*]*@deprecated\b)/

export const noComments = {
  meta: {
    type: "suggestion",
    schema: [],
    messages: {
      banned: "Comments are banned. Let a name, a type, or a test carry the meaning, and record a decision in docs/adr/. See \"Comments\" in docs/standards/simplicity.md.",
    },
  },
  create(context) {
    return {
      Program() {
        for (const comment of context.sourceCode.getAllComments()) {
          if (comment.type !== "Shebang" && !commentDirective.test(comment.value)) {
            context.report({ loc: comment.loc, messageId: "banned" })
          }
        }
      },
    }
  },
}
