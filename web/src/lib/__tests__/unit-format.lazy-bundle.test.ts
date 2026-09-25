/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Its own file because it needs i18next initialised with English alone, so that French is
// still missing when the first label is asked for. unit-format.test.ts initialises several
// languages up front, and vitest isolates modules per file.
import commonEn from "@/i18n/locales/en/common.json"
import commonFr from "@/i18n/locales/fr/common.json"
import { formatBytes } from "@/lib/utils"
import i18next from "i18next"
import { beforeAll, describe, expect, it } from "vitest"

beforeAll(async () => {
  await i18next.init({
    resources: { en: { common: commonEn } },
    lng: "en",
    fallbackLng: "en",
    defaultNS: "common",
    interpolation: { escapeValue: false },
  })
})

describe("a language whose bundle arrives late", () => {
  it("picks up its units once the bundle is added", async () => {
    // Every language but English is a lazily-loaded chunk, so a byte cell can be formatted
    // while the active language still has no resources. unit-format.ts caches resolved labels
    // per language for speed; without its store listener the English ladder would stay cached
    // under fr for the rest of the session. Note the number localizes either way, because that
    // comes from Intl and not from the bundle — the unit is what changes.
    await i18next.changeLanguage("fr")
    expect(formatBytes(1610612736)).toBe("1,5 GiB")

    i18next.addResourceBundle("fr", "common", commonFr, true, true)

    expect(formatBytes(1610612736)).toBe("1,5 Gio")
  })
})
