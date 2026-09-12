/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { existsSync, readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { describe, expect, it } from "vitest"

// web/ root, one level up from src/
const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const html = readFileSync(resolve(webRoot, "index.html"), "utf8")
const iconLink = html.match(/<link[^>]*\brel="icon"[^>]*>/)?.[0] ?? ""

describe("static favicon in index.html", () => {
  // The pre-JS icon must be a real file, not the old blank "data:," placeholder:
  // Firefox defers useDynamicFavicon's setTimeout in background tabs, so whatever
  // index.html ships is what a background tab shows until it gains focus.
  it("ships a real static icon the browser can show before JS runs", () => {
    const href = iconLink.match(/\bhref="([^"]+)"/)?.[1]
    expect(href).toBe("/favicon.png")
    expect(iconLink).toContain("image/png")
    expect(existsSync(resolve(webRoot, "public", "favicon.png"))).toBe(true)
  })

  // useDynamicFavicon upgrades this same node to the themed icon by finding it via
  // data-dynamic-favicon, so the attribute must survive for the upgrade to still work.
  it("keeps the data-dynamic-favicon hook target", () => {
    expect(iconLink).toContain("data-dynamic-favicon=\"svg\"")
  })
})
