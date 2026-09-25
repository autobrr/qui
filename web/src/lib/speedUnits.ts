/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Speed units utilities for toggling between B/s and bps display

import { useClientSetting } from "@/lib/client-settings"
import { type BitRateUnit, type ByteUnit, formatValueWithUnit } from "@/lib/unit-format"

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

const BYTE_RATE_LADDER: ByteUnit[] = ["B", "KiB", "MiB", "GiB", "TiB"]
const BIT_RATE_LADDER: BitRateUnit[] = ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]

// Narrower values get more decimals so a speed column stays the same width.
const speedDecimals = (value: number): number => (value >= 100 ? 0 : value >= 10 ? 1 : 2)

// Format speed with unit preference
export function formatSpeedWithUnit(
  bytesPerSecond: number,
  unit: SpeedUnit,
  compact: boolean = false
): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) {
    if (compact) return "0"
    return unit === "bits" ? formatValueWithUnit(0, "bps") : formatValueWithUnit(0, "B", { perSecond: true })
  }

  if (unit === "bits") {
    // Convert bytes to bits (multiply by 8)
    const bitsPerSecond = bytesPerSecond * 8
    const k = 1000 // Use decimal for bits (standard networking convention)
    const rawIndex = Math.log(bitsPerSecond) / Math.log(k)
    const i = Math.min(BIT_RATE_LADDER.length - 1, Math.max(0, Math.floor(rawIndex)))
    const value = bitsPerSecond / Math.pow(k, i)
    const decimals = speedDecimals(value)
    if (Number(value.toFixed(decimals)) === 0) {
      return compact ? "0" : formatValueWithUnit(0, "bps")
    }
    // The bit-rate symbols carry the rate already ("Mbps", "Mbit/s"), so compact changes
    // nothing here beyond the zero above — as it did before units were localized.
    return formatValueWithUnit(value, BIT_RATE_LADDER[i], { fractionDigits: decimals })
  } else {
    // Use existing bytes format
    const k = 1024
    const rawIndex = Math.log(bytesPerSecond) / Math.log(k)
    const i = Math.min(BYTE_RATE_LADDER.length - 1, Math.max(0, Math.floor(rawIndex)))
    const value = bytesPerSecond / Math.pow(k, i)
    const decimals = speedDecimals(value)
    if (Number(value.toFixed(decimals)) === 0) {
      if (compact) return "0"
      return formatValueWithUnit(0, "B", { perSecond: true })
    }
    return formatValueWithUnit(value, BYTE_RATE_LADDER[i], { fractionDigits: decimals, perSecond: true, compact })
  }
}
