/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18next, { type ResourceKey, type ResourceLanguage, type i18n } from "i18next"
import { describe, expect, it } from "vitest"

// Asks i18next, not a CLDR table, whether each locale can answer each count: i18next
// never falls back between plural categories, and an unsuffixed base key answers all.
const COUNTS = [0, 1, 2, 3, 4, 5, 11, 21, 101]

const modules = import.meta.glob("./locales/**/*.json", { eager: true, import: "default" }) as Record<string, ResourceKey>

const resources: Record<string, ResourceLanguage> = {}
for (const [path, data] of Object.entries(modules)) {
  const match = path.match(/\.\/locales\/([^/]+)\/([^/]+)\.json$/)
  if (!match) continue
  const [, lng, ns] = match
  ;(resources[lng] ??= {})[ns] = data
}

const locales = Object.keys(resources).filter((lng) => lng !== "en").sort()

function flatten(value: ResourceKey, prefix = ""): string[] {
  if (typeof value !== "object" || value === null) return [prefix]

  return Object.entries(value).flatMap(([key, nested]) => flatten(nested as ResourceKey, prefix ? `${prefix}.${key}` : key))
}

// Every English key carrying a CLDR suffix, reduced to the base the app passes to t().
// Bases whose only English forms are unsuffixed + _other count too: they are the shape
// most likely to lose a locale's plural without any key looking missing.
function pluralKeysOf(english: ResourceLanguage): string[] {
  return Object.keys(english).flatMap((ns) => {
    const bases = new Set<string>()
    for (const key of flatten(english[ns])) {
      const suffix = key.match(/_(zero|one|two|few|many|other)$/)
      if (suffix) bases.add(key.slice(0, -suffix[0].length))
    }
    return [...bases].map((base) => `${ns}:${base}`)
  })
}

async function createInstance(lng: string, bundles: Record<string, ResourceLanguage>, fallbackLng: string | false): Promise<i18n> {
  const instance = i18next.createInstance()
  // Copies the fallback settings from src/i18n/index.ts, not its plugins or postProcess.
  await instance.init({
    resources: bundles,
    lng,
    fallbackLng,
    defaultNS: "common",
    ns: Object.keys(bundles.en ?? bundles[lng]),
    interpolation: { escapeValue: false },
  })
  return instance
}

// A count leaks when nothing in the locale answers it, so i18next serves the English
// resource. Each hit is confirmed against the English rendering before it is reported,
// which is what separates a real fallback from a translation that merely reads the same.
async function findLeaks(locale: string, english: ResourceLanguage, translated: ResourceLanguage): Promise<string[]> {
  const en = await createInstance("en", { en: english }, false)
  const app = await createInstance(locale, { en: english, [locale]: translated }, "en")
  // No English bundle and no fallback, so t() resolves only if the locale itself has a
  // form for the count — including through an unsuffixed base key.
  const inLanguage = await createInstance(locale, { [locale]: translated }, false)

  const leaks: string[] = []
  for (const key of pluralKeysOf(english)) {
    for (const count of COUNTS) {
      if (inLanguage.exists(key, { count })) continue
      if (app.t(key, { count }) !== en.t(key, { count })) continue
      leaks.push(`${key}@${count}`)
    }
  }

  return leaks
}

describe("plural leak detection", () => {
  const english = { common: { items_one: "{{count}} item", items_other: "{{count}} items" } }

  it("reports a Czech base with no _few", async () => {
    const cs = { common: { items_one: "{{count}} položka", items_other: "{{count}} položek" } }

    expect(await findLeaks("cs", english, cs)).toEqual([
      "common:items@2", "common:items@3", "common:items@4",
    ])
  })

  it("clears a Czech base with _few", async () => {
    const cs = { common: { items_one: "{{count}} položka", items_few: "{{count}} položky", items_other: "{{count}} položek" } }

    expect(await findLeaks("cs", english, cs)).toEqual([])
  })

  it("clears an unsuffixed catch-all standing in for the missing categories", async () => {
    const cs = { common: { items: "{{count}} položek", items_one: "{{count}} položka", items_other: "{{count}} položek" } }

    expect(await findLeaks("cs", english, cs)).toEqual([])
  })

  it("reports an omitted _one where the locale has the category", async () => {
    const it = { common: { items_other: "{{count}} elementi" } }

    expect(await findLeaks("it", english, it)).toEqual(["common:items@1"])
  })

  it("does not report an omitted _one in a language without that category", async () => {
    const ko = { common: { items_other: "{{count}}개 항목" } }

    expect(await findLeaks("ko", english, ko)).toEqual([])
  })

  it("does not report a translation that reads the same as English", async () => {
    const it = { common: { items_one: "{{count}} item", items_other: "{{count}} items" } }

    expect(await findLeaks("it", english, it)).toEqual([])
  })
})

describe("plural forms resolve in-language", () => {
  it("finds plural bases to check", () => {
    expect(pluralKeysOf(resources.en).length).toBeGreaterThan(100)
  })

  it.each(locales)("%s never falls back to English for a count", async (locale) => {
    const leaks = await findLeaks(locale, resources.en, resources[locale])

    expect(leaks).toEqual([])
  })
})
