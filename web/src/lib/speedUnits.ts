/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Speed units utilities for toggling between B/s and bps display

import { useClientSetting } from "@/lib/client-settings"
import { BIT_LADDER, BYTE_SPEED_LADDER, formatValueWithUnit, scaleToUnit } from "@/lib/unit-format"

// Speed unit types
export type SpeedUnit = "bytes" | "bits"

// Storage key for speed units preference
const SPEED_UNITS_STORAGE_KEY = "qui-speed-units"

const parseSpeedUnit = (raw: string): SpeedUnit => (raw === "bits" ? "bits" : "bytes")

// Custom hook for managing the DB-backed speed units preference
export function useSpeedUnits(): [SpeedUnit, (unit: SpeedUnit) => void] {
  return useClientSetting<SpeedUnit>(SPEED_UNITS_STORAGE_KEY, {
    defaultValue: "bytes",
    parse: parseSpeedUnit,
    serialize: String,
  })
}

// Narrower values get more decimals so a speed column stays the same width.
const speedDecimals = (value: number): number => (value >= 100 ? 0 : value >= 10 ? 1 : 2)

// Format speed with unit preference
export function formatSpeedWithUnit(
  bytesPerSecond: number,
  unit: SpeedUnit,
  compact: boolean = false
): string {
  const zero = unit === "bits" ? () => formatValueWithUnit(0, "b", { perSecond: true }) : () => formatValueWithUnit(0, "B", { perSecond: true })
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) {
    return compact ? "0" : zero()
  }

  // Bits are decimal by networking convention; bytes stay binary.
  const scaled = unit === "bits"? scaleToUnit(bytesPerSecond * 8, BIT_LADDER, 1000): scaleToUnit(bytesPerSecond, BYTE_SPEED_LADDER, 1024)
  const decimals = speedDecimals(scaled.value)
  if (Number(scaled.value.toFixed(decimals)) === 0) {
    return compact ? "0" : zero()
  }

  // compact drops the rate suffix and the space, which only the byte columns are narrow
  // enough to need.
  return formatValueWithUnit(scaled.value, scaled.unit, {
    fractionDigits: decimals,
    perSecond: true,
    compact: compact && unit === "bytes",
  })
}
