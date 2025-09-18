/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// Speed units utilities for toggling between B/s and bps display

import { useState, useEffect } from "react"

// Speed unit types
export type SpeedUnit = "bytes" | "bits"

// Storage key for speed units preference
const SPEED_UNITS_STORAGE_KEY = "qui-speed-units"

// Custom hook for managing speed units state with localStorage persistence
export function useSpeedUnits(): [SpeedUnit, (unit: SpeedUnit) => void] {
  const [speedUnit, setSpeedUnitState] = useState<SpeedUnit>(() => {
    const stored = localStorage.getItem(SPEED_UNITS_STORAGE_KEY) as SpeedUnit
    return stored === "bits" ? "bits" : "bytes" // Default to bytes
  })

  // Listen for storage changes to sync speed units across components
  useEffect(() => {
    const handleStorageChange = () => {
      const stored = localStorage.getItem(SPEED_UNITS_STORAGE_KEY) as SpeedUnit
      setSpeedUnitState(stored === "bits" ? "bits" : "bytes")
    }

    // Listen for both storage events (cross-tab) and custom events (same-tab)
    window.addEventListener("storage", handleStorageChange)
    window.addEventListener("speed-units-changed", handleStorageChange)

    return () => {
      window.removeEventListener("storage", handleStorageChange)
      window.removeEventListener("speed-units-changed", handleStorageChange)
    }
  }, [])

  const setSpeedUnit = (unit: SpeedUnit) => {
    setSpeedUnitState(unit)
    localStorage.setItem(SPEED_UNITS_STORAGE_KEY, unit)
    // Dispatch custom event for same-tab updates
    window.dispatchEvent(new Event("speed-units-changed"))
  }

  return [speedUnit, setSpeedUnit]
}

// Format speed with unit preference
export function formatSpeedWithUnit(
  bytesPerSecond: number,
  unit: SpeedUnit,
  compact: boolean = false
): string {
  if (!bytesPerSecond || bytesPerSecond === 0) {
    if (compact) return "0"
    return unit === "bits" ? "0 bps" : "0 B/s"
  }

  if (unit === "bits") {
    // Convert bytes to bits (multiply by 8)
    const bitsPerSecond = bytesPerSecond * 8
    const k = 1000 // Use decimal for bits (standard networking convention)
    const sizes = compact ? ["bps", "Kbps", "Mbps", "Gbps"] : ["bps", "Kbps", "Mbps", "Gbps"]
    const i = Math.floor(Math.log(bitsPerSecond) / Math.log(k))
    const value = parseFloat((bitsPerSecond / Math.pow(k, i)).toFixed(i > 0 ? 1 : 0))
    return `${value} ${sizes[i]}`
  } else {
    // Use existing bytes format
    const k = 1024
    const sizes = compact ? ["B", "KiB", "MiB", "GiB"] : ["B/s", "KiB/s", "MiB/s", "GiB/s"]
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k))
    const value = parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(i > 0 ? 1 : 0))
    return `${value}${compact ? "" : " "}${sizes[i]}`
  }
}