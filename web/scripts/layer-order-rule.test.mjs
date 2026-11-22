import { RuleTester } from "eslint"
import { describe, it } from "node:test"
import path from "node:path"
import tseslint from "typescript-eslint"

import { layerOrder } from "./layer-order-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester()

const root = path.resolve("src")
const options = [{ root, layers: ["types", "lib", "hooks", "components", "pages"] }]
const typescript = { parser: tseslint.parser }
const file = (name) => path.join(root, name)
const upward = (from, to) => [{ messageId: "upward", data: { from, to, order: "types, lib, hooks, components, pages" } }]

tester.run("layer-order", layerOrder, {
  valid: [
    { code: "import { api } from \"@/lib/api\"", filename: file("hooks/useThing.ts"), options },
    { code: "import type { Torrent } from \"@/types\"", filename: file("lib/format.ts"), options, languageOptions: typescript },
    { code: "import { Row } from \"@/components/torrents/Row\"", filename: file("components/layout/Shell.tsx"), options },
    { code: "import { format } from \"../lib/format\"", filename: file("hooks/useThing.ts"), options },
    { code: "import { format } from \"./format\"", filename: file("lib/dates.ts"), options },
    { code: "import { useState } from \"react\"", filename: file("lib/dates.ts"), options },
    { code: "import { Page } from \"@/pages/Home\"", filename: file("router.tsx"), options },
    { code: "import { Page } from \"@/pages/Home\"", filename: file("demo/setup.ts"), options },
    { code: "import { router } from \"@/router\"", filename: file("lib/links.ts"), options },
    { code: "import { harness } from \"@/test/harness\"", filename: file("lib/links.ts"), options },
    { code: "const load = (name) => import(name)", filename: file("lib/links.ts"), options },
    { code: "const load = (name) => import(`@/pages/${name}`)", filename: file("lib/links.ts"), options },
    { code: "export const one = 1", filename: file("lib/links.ts"), options },
    { code: "import { Page } from \"@/pages/Home\"", filename: path.resolve("scripts/tool.mjs"), options },
  ],
  invalid: [
    { code: "import { Badge } from \"@/components/ui/badge\"", filename: file("lib/tracker.tsx"), options, errors: upward("lib", "components") },
    { code: "import type { Props } from \"@/components/ui/badge\"", filename: file("types/app.ts"), options, languageOptions: typescript, errors: upward("types", "components") },
    { code: "import { useThing } from \"../hooks/useThing\"", filename: file("lib/dates.ts"), options, errors: upward("lib", "hooks") },
    { code: "import { useThing } from \"../../hooks/useThing\"", filename: file("lib/table/row.ts"), options, errors: upward("lib", "hooks") },
    { code: "import pages from \"@/pages\"", filename: file("components/Nav.tsx"), options, errors: upward("components", "pages") },
    { code: "export { Badge } from \"@/components/ui/badge\"", filename: file("hooks/index.ts"), options, errors: upward("hooks", "components") },
    { code: "export * from \"@/components/ui/badge\"", filename: file("hooks/index.ts"), options, errors: upward("hooks", "components") },
    { code: "const load = () => import(\"@/pages/Home\")", filename: file("components/Nav.tsx"), options, errors: upward("components", "pages") },
    { code: "const load = () => import(`@/pages/Home`)", filename: file("components/Nav.tsx"), options, errors: upward("components", "pages") },
    { code: "type Props = import(\"@/components/ui/badge\").Props", filename: file("lib/tracker.ts"), options, languageOptions: typescript, errors: upward("lib", "components") },
    { code: "let loaded: typeof import(\"../hooks/useThing\") | undefined", filename: file("lib/dates.ts"), options, languageOptions: typescript, errors: upward("lib", "hooks") },
    { code: "import { a } from \"@/hooks/a\"\nimport { b } from \"@/components/b\"", filename: file("lib/both.ts"), options, errors: [...upward("lib", "hooks"), ...upward("lib", "components")] },
  ],
})
