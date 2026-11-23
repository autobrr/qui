import { RuleTester } from "eslint"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { after, describe, it } from "node:test"
import tseslint from "typescript-eslint"

import { entryFiles } from "./entry-files-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester()

const root = fs.mkdtempSync(path.join(os.tmpdir(), "entry-files-"))
fs.mkdirSync(path.join(root, "components/torrents/details"), { recursive: true })
fs.mkdirSync(path.join(root, "components/torrents/filters"), { recursive: true })
fs.writeFileSync(path.join(root, "components/torrents/filters.tsx"), "")
after(() => fs.rmSync(root, { recursive: true }))

const options = [{ root, features: "components" }]
const typescript = { parser: tseslint.parser }
const file = (name) => path.join(root, name)
const inner = (target, feature) => [{ messageId: "inner", data: { target, feature: `components/${feature}` } }]

tester.run("entry-files", entryFiles, {
  valid: [
    { code: "import { InstanceCard } from \"@/components/instances/InstanceCard\"", filename: file("components/layout/Header.tsx"), options },
    { code: "import { Icon } from \"@/components/instances/marks/Icon\"", filename: file("components/instances/InstanceCard.tsx"), options },
    { code: "import { Icon } from \"./marks/Icon\"", filename: file("components/instances/InstanceCard.tsx"), options },
    { code: "import { Icon } from \"../marks/Icon\"", filename: file("components/instances/preferences/Dialog.tsx"), options },
    { code: "import { Panel } from \"@/components/torrents/details\"", filename: file("components/torrents/Table.tsx"), options },
    { code: "import { Filters } from \"@/components/torrents/filters\"", filename: file("components/layout/Shell.tsx"), options },
    { code: "import { Footer } from \"@/components/Footer\"", filename: file("pages/Home.tsx"), options },
    { code: "import { Table } from \"@/components/torrents\"", filename: file("pages/Home.tsx"), options },
    { code: "import { row } from \"@/lib/torrent-table/rows/display\"", filename: file("components/torrents/Table.tsx"), options },
    { code: "import { useState } from \"react\"", filename: file("pages/Home.tsx"), options },
    { code: "const load = (name) => import(`@/components/instances/marks/${name}`)", filename: file("pages/Home.tsx"), options },
  ],
  invalid: [
    { code: "import { Icon } from \"@/components/instances/marks/Icon\"", filename: file("components/layout/Header.tsx"), options, errors: inner("components/instances/marks/Icon", "instances") },
    { code: "import { Icon } from \"@/components/instances/marks/Icon\"", filename: file("components/torrents/table/Row.tsx"), options, errors: inner("components/instances/marks/Icon", "instances") },
    { code: "import { Icon } from \"@/components/instances/marks/Icon\"", filename: file("components/Footer.tsx"), options, errors: inner("components/instances/marks/Icon", "instances") },
    { code: "import { Overview } from \"@/components/instances/preferences/labels/Overview\"", filename: file("pages/Automations.tsx"), options, errors: inner("components/instances/preferences/labels/Overview", "instances") },
    { code: "import { Dialog } from \"../instances/preferences/Dialog\"", filename: file("components/settings/Instances.tsx"), options, errors: inner("components/instances/preferences/Dialog", "instances") },
    { code: "import { Panel } from \"@/components/torrents/details\"", filename: file("components/layout/Shell.tsx"), options, errors: inner("components/torrents/details", "torrents") },
    { code: "import type { Props } from \"@/components/instances/marks/Icon\"", filename: file("pages/Home.tsx"), options, languageOptions: typescript, errors: inner("components/instances/marks/Icon", "instances") },
    { code: "const load = () => import(\"@/components/instances/marks/Icon\")", filename: file("pages/Home.tsx"), options, errors: inner("components/instances/marks/Icon", "instances") },
  ],
})
