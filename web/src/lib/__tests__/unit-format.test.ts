/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import commonDe from "@/i18n/locales/de/common.json"
import commonEn from "@/i18n/locales/en/common.json"
import commonFr from "@/i18n/locales/fr/common.json"
import commonKo from "@/i18n/locales/ko/common.json"
import commonUk from "@/i18n/locales/uk/common.json"
import { getSizeUnitOptions, getSpeedUnitOptions, convertSizeToBytes } from "@/lib/column-filter-utils"
import { type ByteUnit, BYTES_PER_UNIT, formatValueWithUnit, unitLabel } from "@/lib/unit-format"
import { getPieceSizeOptions } from "@/components/torrents/piece-size"
import { formatBytes } from "@/lib/utils"
import { formatSpeedWithUnit } from "@/lib/speedUnits"
import i18next from "i18next"
import { beforeAll, describe, expect, it } from "vitest"

beforeAll(async () => {
  await i18next.init({
    resources: {
      en: { common: commonEn },
      fr: { common: commonFr },
      de: { common: commonDe },
      ko: { common: commonKo },
      uk: { common: commonUk },
    },
    lng: "en",
    fallbackLng: "en",
    defaultNS: "common",
    interpolation: { escapeValue: false },
  })
})

/**
 * The implementation as it stood before units were localized. The parity tests below hold
 * new output to this, which is what makes "English is unchanged" a checkable claim rather
 * than a sentence in a PR body.
 */
function legacyFormatBytes(bytes: number): string {
  if (bytes === 0) return "0 B"
  const k = 1024
  const sizes = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"]
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(k)), sizes.length - 1)
  return `${parseFloat((bytes / Math.pow(k, i)).toFixed(2))} ${sizes[i]}`
}

describe("English output is unchanged", () => {
  it("matches the pre-localization formatter across the ladder", async () => {
    await i18next.changeLanguage("en")
    const values = [
      0, 1, 2, 999, 1000, 1023, 1024, 1025, 1536, 10240, 102400,
      1048576, 1073741824, 1099511627776, 1125899906842624,
      1610612736, 1052193331, 1094811766324, 1.3e21,
    ]
    for (const bytes of values) {
      expect(formatBytes(bytes)).toBe(legacyFormatBytes(bytes))
    }
  })

  it("keeps rounding halfway values the way toFixed did, not the way Intl would", async () => {
    await i18next.changeLanguage("en")
    // Intl rounds the shortest decimal representation, toFixed rounds the binary double,
    // so these disagree: 1.45 is stored as 1.4499... and toFixed gives "1.4" while Intl
    // alone gives "1.5". Delete the pre-round in formatValueWithUnit and this fails.
    const halfway: [number, number, string][] = [
      [1.45, 1, "1.4 MiB"],
      [0.15, 1, "0.1 MiB"],
      [2.05, 1, "2 MiB"],
      [16.45, 1, "16.4 MiB"],
      [4.005, 2, "4 MiB"],
    ]
    for (const [value, fractionDigits, expected] of halfway) {
      expect(formatValueWithUnit(value, "MiB", { fractionDigits })).toBe(expected)
    }
  })

  it("composes the English bit rate from one mechanism", async () => {
    await i18next.changeLanguage("en")
    // "Mbps" does not decompose into a unit plus a rate, so it used to need its own table of
    // complete strings beside the byte ladder's perSecond rule. One rule now serves both.
    expect(formatSpeedWithUnit(1572864, "bits")).toBe("12.6 Mb/s")
    expect(unitLabel("Mb", true)).toBe("Mb/s")
    expect(unitLabel("Mb")).toBe("Mb")
  })

  it("leaves grouping off, so four-digit values keep no separator", async () => {
    await i18next.changeLanguage("en")
    // 1000..1023.99 of a unit stays in that unit, so four digits are ordinary, not an edge
    // case: grouping here would change 8.5% of displayed values.
    expect(formatBytes(1052193331)).toBe("1003.45 MiB")
    await i18next.changeLanguage("de")
    expect(formatBytes(1052193331)).toBe("1003,45 MiB")
  })
})

describe("localized units", () => {
  it("switches French to octets and a comma", async () => {
    await i18next.changeLanguage("fr")
    expect(formatBytes(1610612736)).toBe("1,5 Gio")
    expect(unitLabel("KiB", true)).toBe("Kio/s")
    expect(formatSpeedWithUnit(1572864, "bytes")).toBe("1,5 Mio/s")
    expect(formatSpeedWithUnit(1572864, "bits")).toBe("12,6 Mbit/s")
  })

  it("keeps German on KiB but moves the separator", async () => {
    await i18next.changeLanguage("de")
    expect(formatBytes(1610612736)).toBe("1,5 GiB")
    expect(formatSpeedWithUnit(1572864, "bytes")).toBe("1,5 MiB/s")
  })

  it("switches Ukrainian to Cyrillic, including the rate suffix", async () => {
    await i18next.changeLanguage("uk")
    expect(formatBytes(1610612736)).toBe("1,5 ГіБ")
    expect(unitLabel("KiB", true)).toBe("КіБ/с")
  })

  it("omits the space for Korean, which CLDR writes closed up", async () => {
    await i18next.changeLanguage("ko")
    expect(formatBytes(1610612736)).toBe("1.5GiB")
  })

  it("formats zero in the active locale", async () => {
    await i18next.changeLanguage("fr")
    expect(formatBytes(0)).toBe("0 o")
    expect(formatSpeedWithUnit(0, "bytes")).toBe("0 o/s")
  })
})

describe("derived labels", () => {
  it("renders every piece size from the byte count it already stores", async () => {
    await i18next.changeLanguage("en")
    const labels = getPieceSizeOptions().map((option) => option.label)
    expect(labels).toEqual([
      "Auto (recommended)",
      "16 KiB", "32 KiB", "64 KiB", "128 KiB", "256 KiB", "512 KiB",
      "1 MiB", "2 MiB", "4 MiB", "8 MiB", "16 MiB", "32 MiB", "64 MiB", "128 MiB",
    ])
    await i18next.changeLanguage("fr")
    expect(getPieceSizeOptions()[1].label).toBe("16 Kio")
  })

  it("keeps the filter multipliers in step with the ladder", async () => {
    // convertSizeToBytes reads BYTES_PER_UNIT, so a wrong magnitude there would silently
    // change what a saved filter matches rather than fail to resolve.
    expect(BYTES_PER_UNIT.B).toBe(1)
    expect(BYTES_PER_UNIT.KiB).toBe(1024)
    expect(BYTES_PER_UNIT.PiB).toBe(1024 ** 5)
    for (const { value } of getSizeUnitOptions()) {
      expect(convertSizeToBytes(1, value)).toBe(BYTES_PER_UNIT[value])
    }
    for (const { value } of getSpeedUnitOptions()) {
      expect(convertSizeToBytes(1, value)).toBe(BYTES_PER_UNIT[value.replace("/s", "") as ByteUnit])
    }
  })
})

describe("the cached join template", () => {
  it("carries both placeholders in every locale, so filling it with a plain replace is safe", async () => {
    // formatValueWithUnit fills this template itself instead of paying i18next interpolation
    // per call. That is only safe while every locale writes both placeholders; a translator who
    // dropped one would silently lose the number or the unit.
    const locales = import.meta.glob<{ dataUnits: { valueWithUnit: string } }>(
      "../../i18n/locales/*/common.json",
      { eager: true, import: "default" }
    )
    const paths = Object.keys(locales)
    expect(paths.length).toBe(11)
    for (const [path, common] of Object.entries(locales)) {
      expect(common.dataUnits.valueWithUnit, path).toContain("{{value}}")
      expect(common.dataUnits.valueWithUnit, path).toContain("{{unit}}")
    }
  })
})

describe("filter units keep their stored value", () => {
  it("localizes only the label, never the value", async () => {
    await i18next.changeLanguage("fr")
    expect(getSizeUnitOptions()).toEqual([
      { value: "B", label: "o" },
      { value: "KiB", label: "Kio" },
      { value: "MiB", label: "Mio" },
      { value: "GiB", label: "Gio" },
      { value: "TiB", label: "Tio" },
    ])
    expect(getSpeedUnitOptions().map((option) => option.value)).toEqual([
      "B/s", "KiB/s", "MiB/s", "GiB/s", "TiB/s",
    ])
    expect(getSpeedUnitOptions()[2].label).toBe("Mio/s")
  })

  it("converts every offered value, so a filter stored in one language still works in another", async () => {
    // A persisted ColumnFilter carries these strings verbatim. If a localized label ever
    // reached the value, convertSizeToBytes would miss unitMultipliers and the filter
    // expression would read "Size > NaN".
    await i18next.changeLanguage("fr")
    for (const { value } of getSizeUnitOptions()) {
      expect(Number.isFinite(convertSizeToBytes(1, value))).toBe(true)
    }
    for (const { value } of getSpeedUnitOptions()) {
      expect(Number.isFinite(convertSizeToBytes(1, value))).toBe(true)
    }
    expect(convertSizeToBytes(10, "GiB")).toBe(10 * 1024 ** 3)
  })
})
