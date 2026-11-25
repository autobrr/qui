import { RuleTester } from "eslint"
import { describe, it } from "node:test"

import { noComments } from "./no-comments-rule.mjs"

RuleTester.describe = describe
RuleTester.it = it

const tester = new RuleTester()

const banned = [{ messageId: "banned" }]

tester.run("no-comments", noComments, {
  valid: [
    "/*\n * Copyright (c) 2025-2026, s0up and the autobrr contributors.\n * SPDX-License-Identifier: GPL-2.0-or-later\n */\nconst a = 1",
    "const a = 1 // eslint-disable-line no-console -- the CLI prints its result",
    "// @ts-expect-error the types miss this overload\nconst a = 1",
    "/// <reference types=\"vite/client\" />",
    "// i18n-namespace: torrents\nconst a = 1",
    "/** @deprecated use b */\nconst a = 1",
    "#!/usr/bin/env node\nconst a = 1",
  ],
  invalid: [
    { code: "const a = 1 // one", errors: banned },
    { code: "/* the docs name eslint-disable */\nconst a = 1", errors: banned },
    { code: "// regression: #1\nconst a = 1", errors: banned },
  ],
})
