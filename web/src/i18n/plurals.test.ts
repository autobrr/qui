/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import i18next, { type ResourceKey, type ResourceLanguage, type i18n } from "i18next"
import { describe, expect, it } from "vitest"

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

async function localeOnlyInstance(lng: string, translated: ResourceLanguage): Promise<i18n> {
  const instance = i18next.createInstance()
  await instance.init({
    resources: { [lng]: translated },
    lng,
    fallbackLng: false,
    defaultNS: "common",
    ns: Object.keys(translated),
    interpolation: { escapeValue: false },
  })
  return instance
}

async function findLeaks(locale: string, english: ResourceLanguage, translated: ResourceLanguage): Promise<string[]> {
  const inLanguage = await localeOnlyInstance(locale, translated)

  const leaks: string[] = []
  for (const key of pluralKeysOf(english)) {
    for (const count of COUNTS) {
      if (!inLanguage.exists(key, { count })) leaks.push(`${key}@${count}`)
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

  it("checks a base that English carries as unsuffixed + _other", async () => {
    const bareEnglish = { common: { items: "{{count}} item", items_other: "{{count}} items" } }
    const it = { common: { items_one: "{{count}} elemento" } }

    expect(await findLeaks("it", bareEnglish, it)).toEqual([
      "common:items@0", "common:items@2", "common:items@3", "common:items@4",
      "common:items@5", "common:items@11", "common:items@21", "common:items@101",
    ])
  })

  it("reports an omitted _one where the locale has the category", async () => {
    const it = { common: { items_other: "{{count}} elementi" } }

    expect(await findLeaks("it", english, it)).toEqual(["common:items@1"])
  })

  it("does not report an omitted _one in a language without that category", async () => {
    const ko = { common: { items_other: "{{count}}개 항목" } }

    expect(await findLeaks("ko", english, ko)).toEqual([])
  })

  it("ignores a locale whose text matches English", async () => {
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
