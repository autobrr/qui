/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Locale forms of the IEC byte ladder and the bit-rate ladder, plus the number that
// precedes them. CLDR has no IEC units at all — Intl.NumberFormat({unit:"kibibyte"})
// throws RangeError, only kB/MB/GB exist — so Intl formats the number and the symbols
// come from the dataUnits block in common.json.
//
// The bare i18next singleton, never @/i18n: importing @/i18n here would pull the English
// namespaces out of the entry chunk (measured: first load 7 -> 15 JS requests) and reach
// every component through lib/utils.ts, which also exports cn().
import i18next from "i18next"

export type ByteUnit = "B" | "KiB" | "MiB" | "GiB" | "TiB" | "PiB"
export type BitRateUnit = "bps" | "Kbps" | "Mbps" | "Gbps" | "Tbps"

// English fallbacks, needed because an uninitialised i18next t() returns undefined even
// when given a defaultValue (measured on i18next 26.4.2). Exempt from the hardcoded-literal
// checker by file; unit-format.test.ts asserts every entry has a matching en key.
const ENGLISH_BYTE_UNITS: Record<ByteUnit, string> = {
  B: "B",
  KiB: "KiB",
  MiB: "MiB",
  GiB: "GiB",
  TiB: "TiB",
  PiB: "PiB",
}

const ENGLISH_BIT_RATE_UNITS: Record<BitRateUnit, string> = {
  bps: "bps",
  Kbps: "Kbps",
  Mbps: "Mbps",
  Gbps: "Gbps",
  Tbps: "Tbps",
}

const ENGLISH_PER_SECOND = "{{unit}}/s"
const ENGLISH_VALUE_WITH_UNIT = "{{value}} {{unit}}"

function isBitRateUnit(unit: ByteUnit | BitRateUnit): unit is BitRateUnit {
  return unit in ENGLISH_BIT_RATE_UNITS
}

// Intl.NumberFormat construction dominates the cost of formatting, and formatBytes runs
// per row per refresh in the virtualised tables, so instances are reused. The language is
// part of the key, so a language switch needs no invalidation.
const numberFormatters = new Map<string, Intl.NumberFormat>()

function activeLanguage(): string {
  return i18next.language || "en"
}

function numberFormatter(fractionDigits: number): Intl.NumberFormat {
  const language = activeLanguage()
  const cacheKey = `${language}|${fractionDigits}`
  let formatter = numberFormatters.get(cacheKey)
  if (!formatter) {
    formatter = new Intl.NumberFormat(language, {
      maximumFractionDigits: fractionDigits,
      // These ladders never exceed four digits, and switching English from "1003.45 MiB"
      // to "1,003.45 MiB" would change 8.5% of displayed values — a product change, not
      // localization. CLDR's own it locale groups nothing either.
      useGrouping: false,
    })
    numberFormatters.set(cacheKey, formatter)
  }
  return formatter
}

/**
 * The localized symbol for a unit: "KiB" is "Kio" in French and "КіБ" in Ukrainian.
 * `perSecond` wraps a byte unit in the locale's rate form ("Kio/s", "КіБ/с"); bit-rate
 * units already carry the rate in the symbol, so it does nothing for them.
 */
export function unitLabel(unit: ByteUnit | BitRateUnit, perSecond = false): string {
  if (isBitRateUnit(unit)) {
    return i18next.t(`dataUnits.bitrate.${unit}`, { ns: "common" }) ?? ENGLISH_BIT_RATE_UNITS[unit]
  }

  const symbol = i18next.t(`dataUnits.byte.${unit}`, { ns: "common" }) ?? ENGLISH_BYTE_UNITS[unit]
  if (!perSecond) return symbol

  const rate = i18next.t("dataUnits.perSecond", { ns: "common", unit: symbol })
  return rate ?? ENGLISH_PER_SECOND.replace("{{unit}}", symbol)
}

/**
 * A value and its unit, in the active locale: 1610612736 bytes as "1,5 Gio" in French,
 * "1.5GiB" in Korean, which spaces nothing. The spacing lives in the locale's
 * `dataUnits.valueWithUnit`, not in a branch here.
 *
 * `fractionDigits` is a maximum, so trailing zeros are dropped. `compact` joins the number
 * and the symbol with no separator and no rate suffix ("1.5MiB"), which is how the narrow
 * speed columns have always printed; it is a layout choice, so it overrides the locale's
 * spacing rather than reading it.
 */
export function formatValueWithUnit(
  value: number,
  unit: ByteUnit | BitRateUnit,
  opts: { fractionDigits?: number; perSecond?: boolean; compact?: boolean } = {}
): string {
  const { fractionDigits = 2, perSecond = false, compact = false } = opts
  // Intl rounds the shortest decimal representation while toFixed rounds the binary
  // double, so they disagree on halfway values: 4.005 is stored as 4.00499... and gives
  // "4" here but "4.01" from Intl alone. Rounding first keeps every English string
  // byte-identical to what qui printed before units were localized.
  const rounded = Number(value.toFixed(fractionDigits))
  const formattedValue = numberFormatter(fractionDigits).format(rounded)
  if (compact) return `${formattedValue}${unitLabel(unit)}`

  const symbol = unitLabel(unit, perSecond)
  const combined = i18next.t("dataUnits.valueWithUnit", { ns: "common", value: formattedValue, unit: symbol })
  return combined ?? ENGLISH_VALUE_WITH_UNIT.replace("{{value}}", formattedValue).replace("{{unit}}", symbol)
}
