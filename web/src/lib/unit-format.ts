/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Locale forms of the IEC byte ladder and the bit-rate ladder, plus the number in front of
// them. CLDR has no IEC units at all — Intl.NumberFormat({ unit: "kibibyte" }) throws
// RangeError, only kB/MB/GB exist — so Intl formats the number and the symbols come from the
// dataUnits block in common.json.
//
// The bare i18next singleton, never @/i18n: importing @/i18n here would pull the English
// namespaces out of the entry chunk (measured: first load 7 -> 15 JS requests) and would reach
// every component, because lib/utils.ts imports this module and also exports cn().
import i18next from "i18next"

/** How many bytes each unit is: the one place the ladder is written down. */
export const BYTES_PER_UNIT = {
  B: 1,
  KiB: 1024,
  MiB: 1024 ** 2,
  GiB: 1024 ** 3,
  TiB: 1024 ** 4,
  PiB: 1024 ** 5,
} as const

// The bit ladder is only ever a speed, so it stops where BYTE_SPEED_LADDER does rather than
// where BYTE_LADDER does. A Pb/s rung would be a key in eleven locale files that can never render.
export const BIT_LADDER = ["b", "Kb", "Mb", "Gb", "Tb"] as const

export type ByteUnit = keyof typeof BYTES_PER_UNIT
export type BitUnit = (typeof BIT_LADDER)[number]

// Derived from the ladder rather than listed again; a Set because unitLabel is on the
// per-cell render path.
const BIT_UNITS: ReadonlySet<string> = new Set(BIT_LADDER)

function isBitUnit(unit: ByteUnit | BitUnit): unit is BitUnit {
  return BIT_UNITS.has(unit)
}

/** Size ladder, smallest first. Speeds stop at TiB/s, which no transfer will ever reach. */
export const BYTE_LADDER = Object.keys(BYTES_PER_UNIT) as ByteUnit[]
export const BYTE_SPEED_LADDER = BYTE_LADDER.slice(0, -1)

/**
 * Picks the largest unit that keeps the value at or above 1 and scales it to that unit.
 * qBittorrent reports an unknown size as -1, and log of a negative is NaN, so a non-finite
 * exponent falls back to the bottom of the ladder and the NaN still shows.
 */
export function scaleToUnit<T extends string>(
  value: number,
  ladder: readonly T[],
  base: number
): { value: number; unit: T } {
  const exponent = Math.floor(Math.log(value) / Math.log(base))
  const index = Number.isFinite(exponent) ? Math.min(Math.max(exponent, 0), ladder.length - 1) : 0
  return { value: value / Math.pow(base, index), unit: ladder[index] }
}

// An uninitialised i18next t() returns undefined even when given a defaultValue (measured on
// i18next 26.4.2), so every lookup needs a fallback. Each unit's own key is its English form,
// which unit-format.fallback.test.ts pins against the en locale.

const ENGLISH_PER_SECOND = "{{unit}}/s"
const ENGLISH_VALUE_WITH_UNIT = "{{value}} {{unit}}"
const JOIN_KEY = "dataUnits.valueWithUnit"

/**
 * Every locale-dependent string these formatters need, resolved once per language.
 *
 * formatBytes runs once per byte cell per refresh and a large table refreshes hundreds of
 * cells a second, so per-call i18next work is not affordable: measured over 200k calls,
 * resolving through t() each time cost 1,670 ms against 50 ms for the plain-string formatter
 * it replaced. Resolving per language instead costs one table build per switch.
 */
interface UnitLabels {
  byte: Record<ByteUnit, string>
  byteRate: Record<ByteUnit, string>
  bit: Record<BitUnit, string>
  bitRate: Record<BitUnit, string>
  join: string
}

let labels: UnitLabels | null = null
let labelsLanguage = ""
const numberFormatters = new Map<string, Intl.NumberFormat>()

// A non-English language arrives as a lazily-loaded bundle, so labels resolved before that
// bundle lands are English and must not stay cached under the new language. The listener
// attaches on first use rather than at module load, because i18next only has a store once it
// is initialised and this module is imported by lib/utils.ts, which loads long before i18n.
let resourceListenerAttached = false

function watchResourceChanges(): void {
  if (resourceListenerAttached || !i18next.store) return
  resourceListenerAttached = true
  i18next.store.on("added", () => {
    labels = null
  })
}

function activeLanguage(): string {
  return i18next.language || "en"
}

function translate(key: string, fallback: string, options?: Record<string, unknown>): string {
  return i18next.t(key, { ns: "common", ...options }) ?? fallback
}

function perSecondForm(symbol: string): string {
  return translate("dataUnits.perSecond", ENGLISH_PER_SECOND.replace("{{unit}}", symbol), { unit: symbol })
}

function buildLabels(): UnitLabels {
  const byte = {} as Record<ByteUnit, string>
  const byteRate = {} as Record<ByteUnit, string>
  for (const unit of BYTE_LADDER) {
    byte[unit] = translate(`dataUnits.byte.${unit}`, unit)
    byteRate[unit] = perSecondForm(byte[unit])
  }

  const bit = {} as Record<BitUnit, string>
  const bitRate = {} as Record<BitUnit, string>
  for (const unit of BIT_LADDER) {
    bit[unit] = translate(`dataUnits.bit.${unit}`, unit)
    bitRate[unit] = perSecondForm(bit[unit])
  }

  // getResource, not t(), because t() would interpolate and hand back a filled string with the
  // placeholders gone. It does no fallback resolution of its own, hence the explicit English
  // second look. unit-format.test.ts pins every locale's template to those two placeholders,
  // which is what makes filling them with a plain replace safe.
  const language = activeLanguage()
  const rawJoin = i18next.isInitialized
    ? ((i18next.getResource(language, "common", JOIN_KEY) ??
      i18next.getResource("en", "common", JOIN_KEY)) as string | undefined)
    : undefined

  return { byte, byteRate, bit, bitRate, join: rawJoin ?? ENGLISH_VALUE_WITH_UNIT }
}

function unitLabels(): UnitLabels {
  watchResourceChanges()
  const language = activeLanguage()
  if (labels && labelsLanguage === language) return labels

  labels = buildLabels()
  labelsLanguage = language
  return labels
}

function numberFormatter(fractionDigits: number): Intl.NumberFormat {
  const language = activeLanguage()
  const cacheKey = `${language}|${fractionDigits}`
  let formatter = numberFormatters.get(cacheKey)
  if (!formatter) {
    formatter = new Intl.NumberFormat(language, {
      maximumFractionDigits: fractionDigits,
      // These ladders never exceed four digits, and switching English from "1003.45 MiB" to
      // "1,003.45 MiB" would change 8.5% of displayed values — a product change, not
      // localization. CLDR's own it locale groups nothing either.
      useGrouping: false,
    })
    numberFormatters.set(cacheKey, formatter)
  }
  return formatter
}

/**
 * The localized symbol for a unit: "KiB" is "Kio" in French and "КіБ" in Ukrainian.
 * `perSecond` gives the locale's rate form, and applies to both ladders: "Kio/s", "Mbit/s".
 */
export function unitLabel(unit: ByteUnit | BitUnit, perSecond = false): string {
  const resolved = unitLabels()
  if (isBitUnit(unit)) return perSecond ? resolved.bitRate[unit] : resolved.bit[unit]
  return perSecond ? resolved.byteRate[unit] : resolved.byte[unit]
}

/**
 * A value and its unit in the active locale: 1610612736 bytes reads "1,5 Gio" in French and
 * "1.5GiB" in Korean, which spaces nothing. The spacing lives in the locale's
 * `dataUnits.valueWithUnit`, not in a branch here.
 *
 * `fractionDigits` is a maximum, so trailing zeros are dropped. `compact` joins the number and
 * the symbol with no separator and no rate suffix ("1.5MiB"), which is how the narrow speed
 * columns have always printed; it is a layout choice, so it overrides the locale's spacing
 * rather than reading it.
 */
export function formatValueWithUnit(
  value: number,
  unit: ByteUnit | BitUnit,
  opts: { fractionDigits?: number; perSecond?: boolean; compact?: boolean } = {}
): string {
  const { fractionDigits = 2, perSecond = false, compact = false } = opts
  // Intl rounds the shortest decimal representation while toFixed rounds the binary double, so
  // they disagree on halfway values: 1.45 is stored as 1.4499… and gives "1.4" here but "1.5"
  // from Intl alone. Rounding first keeps every English string byte-identical to what qui
  // printed before units were localized.
  const rounded = Number(value.toFixed(fractionDigits))
  const formattedValue = numberFormatter(fractionDigits).format(rounded)
  if (compact) return `${formattedValue}${unitLabel(unit)}`

  return unitLabels().join.replace("{{value}}", formattedValue).replace("{{unit}}", unitLabel(unit, perSecond))
}
