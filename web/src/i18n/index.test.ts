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
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  localStorage.clear()
})

describe("language preferences with blocked storage", () => {
  it.each([
    ["fr-FR", "fr", "Tableau de bord"],
    ["sv-SE", "en", "Dashboard"],
  ])("initializes translations for %s when storage reads fail", async (browserLanguage, expectedLanguage, label) => {
    vi.resetModules()
    vi.stubGlobal("navigator", { languages: [browserLanguage], language: browserLanguage })
    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new DOMException("Storage is blocked", "SecurityError")
    })

    const { initI18n } = await import("./index")
    const i18n = await initI18n()

    expect(i18n.resolvedLanguage).toBe(expectedLanguage)
    expect(i18n.t("nav.dashboard")).toBe(label)
  })

  it.each(["getItem", "setItem"] as const)("changes the language when storage %s fails", async (operation) => {
    vi.resetModules()
    localStorage.setItem("qui.language", "en")
    const { initI18n, changeLanguage } = await import("./index")
    const i18n = await initI18n()
    expect(i18n.t("nav.dashboard")).toBe("Dashboard")

    vi.spyOn(console, "error").mockImplementation(() => {})
    vi.spyOn(Storage.prototype, operation).mockImplementation(() => {
      throw new DOMException("Storage is blocked", "SecurityError")
    })

    await changeLanguage("fr")

    expect(i18n.resolvedLanguage).toBe("fr")
    expect(i18n.t("nav.dashboard")).toBe("Tableau de bord")
  })
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
