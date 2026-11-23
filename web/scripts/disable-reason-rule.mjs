const directive = /^\s*eslint-disable(?:-next-line|-line)?(?=\s|$)/
const reason = /\s-{2,}\s+\S/

export const disableReason = {
  meta: {
    type: "problem",
    schema: [],
    messages: {
      missing: "This eslint-disable gives no reason. Fix the report that it hides, and delete the comment. If the code is right on purpose, write the reason after the rule name: `eslint-disable-next-line <rule> -- <reason>`. See \"Exceptions\" in docs/linting.md.",
    },
  },
  create(context) {
    return {
      Program() {
        for (const comment of context.sourceCode.getAllComments()) {
          if (directive.test(comment.value) && !reason.test(comment.value)) {
            context.report({ loc: comment.loc, messageId: "missing" })
          }
        }
      },
    }
  },
}
