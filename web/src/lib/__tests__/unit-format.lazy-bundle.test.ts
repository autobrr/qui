/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
    await i18next.changeLanguage("fr")
    expect(formatBytes(1610612736)).toBe("1,5 GiB")

    i18next.addResourceBundle("fr", "common", commonFr, true, true)

    expect(formatBytes(1610612736)).toBe("1,5 Gio")
  })
})
