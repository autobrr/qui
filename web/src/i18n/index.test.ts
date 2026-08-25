/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { afterEach, describe, expect, it, vi } from "vitest"

// i18n/index.ts resolves the initial language at import time, so the detected
// language is observable as i18n.language on a freshly imported module.
async function detectedLanguage(languages: string[]): Promise<string | undefined> {
  vi.resetModules()
  localStorage.clear()
  vi.stubGlobal("navigator", { languages, language: languages[0] })
  const { default: i18n } = await import("./index")
  return i18n.language
}

afterEach(() => {
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe("browser language detection", () => {
  it.each([
    ["zh-TW", "zh-TW"],
    ["zh-Hant", "zh-TW"],
    ["zh-Hant-TW", "zh-TW"],
    ["zh-HK", "zh-TW"],
    ["zh-MO", "zh-TW"],
    ["zh-CN", "zh-CN"],
    ["zh-Hans", "zh-CN"],
    ["zh-Hans-CN", "zh-CN"],
    ["zh-SG", "zh-CN"],
    ["zh", "zh-CN"],
    ["pt-PT", "pt-BR"],
    ["fr-FR", "fr"],
    ["en-US", "en"],
    ["sv-SE", "en"],
  ])("maps %s to %s", async (tag, expected) => {
    expect(await detectedLanguage([tag])).toBe(expected)
  })

  it("uses the first supported tag in the list", async () => {
    expect(await detectedLanguage(["sv-SE", "zh-HK", "de"])).toBe("zh-TW")
  })

  it("prefers a stored preference over detection", async () => {
    vi.resetModules()
    localStorage.setItem("qui.language", "zh-CN")
    vi.stubGlobal("navigator", { languages: ["zh-HK"], language: "zh-HK" })
    const { default: i18n } = await import("./index")
    expect(i18n.language).toBe("zh-CN")
  })
})
