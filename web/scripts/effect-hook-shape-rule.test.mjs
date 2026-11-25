import { RuleTester } from "eslint"
import { describe, it } from "node:test"

import { effectHookShape } from "./effect-hook-shape-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester()

const second = { messageId: "second" }
const cleanup = { messageId: "cleanup" }
const react = "import { useEffect, useLayoutEffect } from \"react\"\nimport * as React from \"react\"\n"

tester.run("effect-hook-shape", effectHookShape, {
  valid: [
    `${react}useEffect(() => {\n  const timer = setTimeout(run, 100)\n  return () => clearTimeout(timer)\n}, [])`,
    `${react}useEffect(() => {\n  if (typeof document === "undefined") {\n    return\n  }\n  document.addEventListener("visibilitychange", run)\n  return () => {\n    document.removeEventListener("visibilitychange", run)\n  }\n}, [])`,
    `${react}useEffect(() => {\n  if (ready) {\n    return subscribe(run)\n  }\n}, [ready])`,
    `${react}React.useLayoutEffect(function () {\n  return subscribe(run)\n}, [])`,
    `${react}function useOther(useEffect) {\n  useEffect(() => {})\n  useEffect(() => {})\n}`,
    "import { useEffect } from \"@/test/harness\"\nuseEffect(() => {})\nuseEffect(() => {})",
  ],
  invalid: [
    { code: `${react}useEffect(() => {\n  document.title = title\n}, [title])`, errors: [cleanup] },
    { code: `${react}useEffect(() => {\n  if (!ready) {\n    return\n  }\n  run()\n}, [ready])`, errors: [cleanup] },
    { code: `${react}useEffect(() => {\n  const stop = () => {\n    return clearTimeout(timer)\n  }\n  listen(stop)\n}, [])`, errors: [cleanup] },
    { code: `${react}useEffect(() => {\n  function stop() {\n    return clearTimeout(timer)\n  }\n  listen(stop)\n}, [])`, errors: [cleanup] },
    { code: `${react}useEffect(() => subscribe(run), [])`, errors: [cleanup] },
    { code: `${react}useEffect(subscribeToStore, [])`, errors: [cleanup] },
    { code: `${react}useEffect()`, errors: [cleanup] },
    { code: `${react}const stop = () => {\n  return 1\n}\nuseEffect(() => {\n  run()\n}, [])`, errors: [cleanup] },
    {
      code: `${react}useEffect(() => {\n  return subscribe(run)\n}, [])\nReact.useLayoutEffect(() => {\n  return subscribe(measure)\n}, [])`,
      errors: [{ ...second, line: 6 }],
    },
    {
      code: `${react}useEffect(() => {\n  return subscribe(run)\n}, [])\nuseEffect(() => {\n  return subscribe(a)\n}, [])\nuseEffect(() => {\n  b()\n}, [])`,
      errors: [{ ...second, line: 6 }, { ...second, line: 9 }, { ...cleanup, line: 9 }],
    },
  ],
})
