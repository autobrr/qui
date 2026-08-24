/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Speed units utilities for toggling between B/s and bps display

import { useClientSetting } from "@/lib/client-settings"

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

// Format speed with unit preference
export function formatSpeedWithUnit(
  bytesPerSecond: number,
  unit: SpeedUnit,
  compact: boolean = false
): string {
  if (!Number.isFinite(bytesPerSecond) || bytesPerSecond <= 0) {
    if (compact) return "0"
    return unit === "bits" ? "0 bps" : "0 B/s"
  }

  if (unit === "bits") {
    // Convert bytes to bits (multiply by 8)
    const bitsPerSecond = bytesPerSecond * 8
    const k = 1000 // Use decimal for bits (standard networking convention)
    const sizes = compact ? ["bps", "Kbps", "Mbps", "Gbps", "Tbps"] : ["bps", "Kbps", "Mbps", "Gbps", "Tbps"]
    const rawIndex = Math.log(bitsPerSecond) / Math.log(k)
    const i = Math.min(sizes.length - 1, Math.max(0, Math.floor(rawIndex)))
    const value = bitsPerSecond / Math.pow(k, i)
    const decimals = value >= 100 ? 0 : value >= 10 ? 1 : 2
    const formatted = Number(value.toFixed(decimals))
    if (formatted === 0) {
      return compact ? "0" : "0 bps"
    }
    return `${formatted} ${sizes[i]}`
  } else {
    // Use existing bytes format
    const k = 1024
    const sizes = compact ? ["B", "KiB", "MiB", "GiB", "TiB"] : ["B/s", "KiB/s", "MiB/s", "GiB/s", "TiB/s"]
    const rawIndex = Math.log(bytesPerSecond) / Math.log(k)
    const i = Math.min(sizes.length - 1, Math.max(0, Math.floor(rawIndex)))
    const value = bytesPerSecond / Math.pow(k, i)
    const decimals = value >= 100 ? 0 : value >= 10 ? 1 : 2
    const formatted = Number(value.toFixed(decimals))
    if (formatted === 0) {
      if (compact) return "0"
      return "0 B/s"
    }
    return `${formatted}${compact ? "" : " "}${sizes[i]}`
  }
}
