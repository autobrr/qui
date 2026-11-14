/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { resolve } from "node:path"

import { describe, expect, it } from "vitest"

import { THEME_AREAS } from "@/lib/theme-areas"
import { buildThemePrompt } from "@/lib/theme-prompt"

const docsTokens = (() => {
  const docs = readFileSync(resolve(__dirname, "../../../documentation/docs/features/custom-themes.md"), "utf8")
  const table = docs.slice(docs.indexOf("| Group | Tokens |"))
  const rows = table.slice(0, table.indexOf("\n\n")).split("\n").slice(2)
  const tokens: string[] = []
  for (const row of rows) {
    for (const [, token] of row.matchAll(/`(--[a-z0-9-]+)(?:\(-foreground\))?`/g)) {
      tokens.push(token)
      if (row.includes(`\`${token}(-foreground)\``)) tokens.push(`${token}-foreground`)
    }
  }
  return tokens
})()

describe("buildThemePrompt", () => {
  const prompt = buildThemePrompt()

  it("lists every theme area with its purpose", () => {
    for (const { name, purpose } of THEME_AREAS) {
      expect(prompt).toContain(`[data-qui-area="${name}"]: ${purpose}`)
    }
  })

  it("lists every design token of the docs table", () => {
    expect(docsTokens.length).toBeGreaterThan(30)
    for (const token of docsTokens) {
      expect(prompt).toMatch(new RegExp(`${token}(,|\\n|$)`))
    }
  })

  it("names every rule", () => {
    for (const rule of [
      "@name",
      "@description",
      ":root block for light mode",
      ".dark block for dark mode",
      "Do not use @import.",
      "Do not use image-set().",
      "data: address",
      "#fragment",
      "backslash escapes",
      "1 MiB",
      "height of a torrent row",
    ]) {
      expect(prompt).toContain(rule)
    }
  })
})
