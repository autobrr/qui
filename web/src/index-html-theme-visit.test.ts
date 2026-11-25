/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { readFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { asksForDefaultTheme } from "@/lib/theme-storage"

const html = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), "..", "index.html"), "utf8")
const inlineScript = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? ""

function paintsStoredTheme(search: string): boolean {
  window.history.replaceState(null, "", `/${search}`)
  localStorage.setItem("theme-critical-vars", JSON.stringify({ background: "navy", foreground: "white" }))
  document.documentElement.style.backgroundColor = ""
  new Function(inlineScript)()
  return document.documentElement.style.backgroundColor === "navy"
}

beforeEach(() => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: false })))
})

afterEach(() => {
  window.history.replaceState(null, "", "/")
  localStorage.clear()
  document.documentElement.style.backgroundColor = ""
  vi.unstubAllGlobals()
})

describe("index.html paint script", () => {
  it.each(["", "?theme=default", "?theme=Default", "?x=1&theme=default", "?theme=minimal", "?theme=default&theme=x", "?theme=x&theme=default"])(
    "skips the stored theme at %j exactly when the app starts a default theme visit",
    (search) => {
      expect(paintsStoredTheme(search)).toBe(!asksForDefaultTheme(search))
    }
  )
})
