/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { describe, it, expect } from "vitest"
import { readdirSync, readFileSync } from "node:fs"
import { join, resolve } from "node:path"
import { FONT_MAP, extractFontName } from "./fontLoader"

const THEMES_DIR = resolve(import.meta.dirname, "../../../internal/themes/assets")

const GENERIC = /^(?:serif|sans-serif|cursive|fantasy|monospace|system-ui|math|emoji|fangsong|ui-(?:serif|sans-serif|monospace|rounded))$/

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
        const normalized = family.toLowerCase()
        const isReference = normalized.startsWith("var(") || normalized.startsWith("generic(")
        if (!isReference && !GENERIC.test(normalized) && !(family in FONT_MAP)) {
          missing.add(`${family} (${file})`)
        }
      }
    }

    expect([...missing]).toEqual([])
  })
})
