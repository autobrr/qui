import { RuleTester } from "eslint"
import { describe, it } from "node:test"

import { noDirectEffect } from "./no-direct-effect-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester()

const banned = [{ messageId: "banned" }]

tester.run("no-direct-effect", noDirectEffect, {
  valid: [
    "import { useMemo, useState } from \"react\"\nconst total = useMemo(() => 1, [])",
    "import { useSyncExternalStore } from \"react\"\nuseSyncExternalStore(subscribe, read)",
    "import { useEffect } from \"@/test/harness\"\nuseEffect(() => {}, [])",
    "import * as Harness from \"@/test/harness\"\nHarness.useEffect(() => {}, [])",
    "import { useEffect } from \"react\"\nconst run = (useEffect) => useEffect(() => {}, [])",
    "import { useDebounce } from \"@/hooks/useDebounce\"\nconst term = useDebounce(text, 300)",
    "import { useEffect } from \"react\"\nconst hook = useEffect",
    "import * as React from \"react\"\nReact[hook](() => {}, [])",
    "import * as React from \"react\"\nReact[`useEffect${suffix}`](() => {}, [])",
    "import * as React from \"react\"\nconst run = (React) => React[\"useEffect\"](() => {}, [])",
  ],
  invalid: [
    { code: "import { useEffect } from \"react\"\nuseEffect(() => {}, [])", errors: banned },
    { code: "import { useLayoutEffect } from \"react\"\nuseLayoutEffect(() => {}, [])", errors: banned },
    { code: "import { useInsertionEffect } from \"react\"\nuseInsertionEffect(() => {}, [])", errors: banned },
    { code: "import { useEffect as useSync } from \"react\"\nuseSync(() => {}, [])", errors: banned },
    { code: "import * as React from \"react\"\nReact.useEffect(() => {}, [])", errors: banned },
    { code: "import React from \"react\"\nReact.useLayoutEffect(() => {}, [])", errors: banned },
    { code: "import * as React from \"react\"\nReact[\"useEffect\"](() => {}, [])", errors: banned },
    { code: "import React from \"react\"\nReact[`useInsertionEffect`](() => {}, [])", errors: banned },
    { code: "import { useEffect } from \"react\"\nfunction Row() { useEffect(() => {}, []); useEffect(() => {}, []) }", errors: [...banned, ...banned] },
  ],
})
