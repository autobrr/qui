/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect } from "vitest"
import { readdirSync, readFileSync } from "node:fs"
import { join, resolve } from "node:path"
import { FONT_MAP, extractFontName } from "./fontLoader"

// Bundled theme CSS lives in the backend assets tree. The premium subdirectory
// only exists after `make build` fetches it, so locally this covers premium
// themes too while CI covers the free set.
const THEMES_DIR = resolve(import.meta.dirname, "../../../internal/themes/assets")

// CSS generic keywords, UA stacks, and nested variable references never need loading.
const GENERIC = /^(?:ui-|var\(|serif$|sans-serif$|monospace$)/

describe("FONT_MAP", () => {
  it("covers every font family the bundled themes use", () => {
    const cssFiles = readdirSync(THEMES_DIR, { recursive: true, encoding: "utf8" })
      .filter((f) => f.endsWith(".css"))
    expect(cssFiles.length).toBeGreaterThan(0)

    const missing = new Set<string>()
    for (const file of cssFiles) {
      const css = readFileSync(join(THEMES_DIR, file), "utf8")
      for (const [, value] of css.matchAll(/--font-(?:sans|serif|mono):\s*([^;]+);/g)) {
        const family = extractFontName(value)
        if (!GENERIC.test(family) && !(family in FONT_MAP)) {
          missing.add(`${family} (${file})`)
        }
      }
    }

    // A failure here means a theme names a font the loader cannot load, so the
    // browser silently falls back. Add the family to FONT_MAP in fontLoader.ts:
    // a Google Fonts spec like "Name:wght@300;400;500;600;700", or "" for a
    // system font.
    expect([...missing]).toEqual([])
  })
})
