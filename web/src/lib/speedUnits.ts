/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useClientSetting } from "@/lib/client-settings"
import { BIT_LADDER, BYTE_SPEED_LADDER, formatValueWithUnit, scaleToUnit } from "@/lib/unit-format"

export type SpeedUnit = "bytes" | "bits"

const SPEED_UNITS_STORAGE_KEY = "qui-speed-units"

const parseSpeedUnit = (raw: string): SpeedUnit => (raw === "bits" ? "bits" : "bytes")

export function useSpeedUnits(): [SpeedUnit, (unit: SpeedUnit) => void] {
  return useClientSetting<SpeedUnit>(SPEED_UNITS_STORAGE_KEY, {
    defaultValue: "bytes",
    parse: parseSpeedUnit,
    serialize: String,
  })
}

const speedDecimals = (value: number): number => (value >= 100 ? 0 : value >= 10 ? 1 : 2)

const zeroSpeed = (unit: SpeedUnit, compact: boolean): string =>
  compact ? "0" : formatValueWithUnit(0, unit === "bits" ? "b" : "B", { perSecond: true })

export function formatSpeedWithUnit(
  bytesPerSecond: number,
  unit: SpeedUnit,
  compact: boolean = false
): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) {
    return zeroSpeed(unit, compact)
  }

  const scaled = unit === "bits"? scaleToUnit(bytesPerSecond * 8, BIT_LADDER, 1000): scaleToUnit(bytesPerSecond, BYTE_SPEED_LADDER, 1024)
  const decimals = speedDecimals(scaled.value)
  if (Number(scaled.value.toFixed(decimals)) === 0) {
    return zeroSpeed(unit, compact)
  }

  return formatValueWithUnit(scaled.value, scaled.unit, {
    fractionDigits: decimals,
    perSecond: true,
    compact: compact && unit === "bytes",
  })
}
