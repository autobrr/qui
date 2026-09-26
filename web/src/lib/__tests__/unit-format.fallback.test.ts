/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// This file must never initialise i18next and must never import @/i18n. An uninitialised
// t() returns undefined even when given a defaultValue, so the English fallback tables in
// unit-format.ts are the only thing standing between an uninitialised import and a raw key
// on screen. It lives apart from unit-format.test.ts, which does initialise, because
// vitest isolates modules per file and the two would otherwise fight over the singleton.
import commonEn from "@/i18n/locales/en/common.json"
import { formatValueWithUnit, unitLabel } from "@/lib/unit-format"
import { formatBytes } from "@/lib/utils"
import { describe, expect, it } from "vitest"

const byteUnits = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"] as const
const bitUnits = ["b", "Kb", "Mb", "Gb", "Tb"] as const

describe("unit-format without i18next initialised", () => {
  it("falls back to the same strings the en locale carries", () => {
    // Pins the fallback tables to en/common.json. This is the guard the hardcoded-literal
    // checker's file-level skip for unit-format.ts is traded against: the skip is safe only
    // while every literal in that file is provably the English locale value.
    for (const unit of byteUnits) {
      expect(unitLabel(unit)).toBe(commonEn.dataUnits.byte[unit])
    }
    for (const unit of bitUnits) {
      expect(unitLabel(unit)).toBe(commonEn.dataUnits.bit[unit])
    }
  })

  it("covers every unit the en locale declares, with nothing left over", () => {
    expect(Object.keys(commonEn.dataUnits.byte)).toEqual([...byteUnits])
    expect(Object.keys(commonEn.dataUnits.bit)).toEqual([...bitUnits])
  })

  it("applies the per-second wrapper", () => {
    expect(unitLabel("KiB", true)).toBe("KiB/s")
  })

  it("still formats a value and a unit together", () => {
    expect(formatValueWithUnit(1.5, "GiB")).toBe("1.5 GiB")
    expect(formatBytes(1610612736)).toBe("1.5 GiB")
  })
})
