/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { existsSync, readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { afterEach, describe, expect, it } from "vitest"

const webRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const html = readFileSync(resolve(webRoot, "index.html"), "utf8")
const head = html.match(/<head>([\s\S]*)<\/head>/)?.[1] ?? ""
const inlineScript = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? ""

// Mounts index.html's <head> and runs its inline anti-FOUC script against the seeded theme cache.
function loadIcon(themeCache?: object): HTMLLinkElement {
  if (themeCache) localStorage.setItem("theme-cache", JSON.stringify(themeCache))
  document.head.innerHTML = head
  new Function(inlineScript)()
  const link = document.querySelector<HTMLLinkElement>("link[data-dynamic-favicon=\"svg\"]")
  if (!link) throw new Error("index.html lost the data-dynamic-favicon link useDynamicFavicon upgrades")
  return link
}

afterEach(() => {
  localStorage.clear()
  document.head.innerHTML = ""
})

describe("static favicon in index.html", () => {
  // Firefox defers useDynamicFavicon in background tabs, so whatever index.html ships is what shows until focus.
  it("ships a real icon before JS runs", () => {
    expect(loadIcon().getAttribute("href")).toBe("/favicon.png")
    expect(existsSync(resolve(webRoot, "public", "favicon.png"))).toBe(true)
  })

  it("blanks the icon for the spreadsheet disguise", () => {
    expect(loadIcon({ id: "spreadsheet" }).getAttribute("href")).toBe("data:,")
    expect(loadIcon({ id: "spreadsheet-classic" }).getAttribute("href")).toBe("data:,")
    expect(loadIcon({ id: "minimal" }).getAttribute("href")).toBe("/favicon.png")
  })
})
