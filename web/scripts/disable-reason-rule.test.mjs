import { RuleTester } from "eslint"
import { describe, it } from "node:test"

import { disableReason } from "./disable-reason-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester({
  languageOptions: { parserOptions: { ecmaFeatures: { jsx: true } } },
})

const missing = [{ messageId: "missing" }]

tester.run("disable-reason", disableReason, {
  valid: [
    "// eslint-disable-next-line no-console -- the CLI prints its result\nconsole.log(1)",
    "console.log(1) // eslint-disable-line no-console -- the CLI prints its result",
    "/* eslint-disable no-console -- the CLI prints its result */\nconsole.log(1)\n/* eslint-enable no-console */",
    "const view = <div>\n  {/* eslint-disable-next-line no-console -- the CLI prints its result */}\n  {console.log(1)}\n</div>",
    "// eslint-disable-next-line no-console --- the CLI prints its result\nconsole.log(1)",
    "// eslint-disabled is not a directive\nconsole.log(1)",
    "// the docs name eslint-disable-next-line\nconsole.log(1)",
    "const text = \"// eslint-disable-next-line no-console\"",
  ],
  invalid: [
    { code: "// eslint-disable-next-line no-console\nconsole.log(1)", errors: missing },
    { code: "console.log(1) // eslint-disable-line no-console", errors: missing },
    { code: "/* eslint-disable no-console */\nconsole.log(1)", errors: missing },
    { code: "// eslint-disable-next-line\nconsole.log(1)", errors: missing },
    { code: "// eslint-disable-next-line no-console -- \nconsole.log(1)", errors: missing },
    { code: "const view = <div>\n  {/* eslint-disable-next-line no-console */}\n  {console.log(1)}\n</div>", errors: missing },
    { code: "// eslint-disable-next-line no-console\nconsole.log(1)\n// eslint-disable-next-line no-alert\nalert(1)", errors: [...missing, ...missing] },
  ],
})
