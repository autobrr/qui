import { RuleTester } from "eslint"
import { describe, it } from "node:test"

import { noNativeTitle } from "./no-native-title-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester({
  languageOptions: { parserOptions: { ecmaFeatures: { jsx: true } } },
})

const banned = [{ messageId: "banned" }]

tester.run("no-native-title", noNativeTitle, {
  valid: [
    "<span aria-label=\"Seeding\" />",
    "<svg><title>Seeding</title></svg>",
    "import { Section } from \"@/components/kit/Section\"\nconst view = <Section title=\"Indexers\" />",
    "import { MultiSelect } from \"@/components/ui/multi-select\"\nconst view = <MultiSelect title=\"Pick categories\" />",
    "import { Tooltip, TooltipContent, TooltipTrigger } from \"@/components/ui/tooltip\"\nconst view = <Tooltip><TooltipTrigger asChild><button aria-label=\"Copy\" /></TooltipTrigger><TooltipContent>Copy</TooltipContent></Tooltip>",
  ],
  invalid: [
    { code: "<span title=\"Seeding\" />", errors: banned },
    { code: "<button title={label} aria-label={label} />", errors: banned },
    { code: "import { Button } from \"@/components/ui/button\"\nconst view = <Button title=\"Copy\" />", errors: banned },
    { code: "import * as DropdownMenuPrimitive from \"@radix-ui/react-dropdown-menu\"\nconst view = <DropdownMenuPrimitive.RadioItem title=\"Dark\" />", errors: banned },
    { code: "import { Link } from \"@tanstack/react-router\"\nconst view = <Link to=\"/search\" title=\"Search\" />", errors: banned },
  ],
})
