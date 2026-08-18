/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { join } from "node:path"
import { expect, it } from "vitest"
import { PHONE_LANDSCAPE } from "./useMediaQuery"

// The CSS variants in index.css and the matchMedia queries in this hook
// must describe the same viewports, or the shell and the page content
// disagree about which layout is active. This test pins the sync.
// import.meta.url is not a file: URL under jsdom, so resolve from cwd
// (vitest runs with web/ as the root).
it("index.css variants stay in sync with useMediaQuery", () => {
  const css = readFileSync(join(process.cwd(), "src", "index.css"), "utf8")
  expect(css).toContain(`@custom-variant phone-land (@media ${PHONE_LANDSCAPE});`)
  expect(css).toContain(`@custom-variant desk (@media (min-width: 768px) and (not (${PHONE_LANDSCAPE})));`)
})
