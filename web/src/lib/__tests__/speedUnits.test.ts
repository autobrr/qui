/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"

const STORAGE_KEY = "qui-speed-units"

beforeEach(() => {
  localStorage.clear()
})

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

describe("useSpeedUnits initial state", () => {
  it("defaults to 'bytes' when nothing is stored", () => {
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bytes")
  })

  it("initializes to 'bits' when 'bits' is stored", () => {
    localStorage.setItem(STORAGE_KEY, "bits")
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bits")
  })

  it("falls back to 'bytes' for an unknown stored value", () => {
    localStorage.setItem(STORAGE_KEY, "foo")
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bytes")
  })

  it("falls back to 'bytes' for an empty stored value", () => {
    localStorage.setItem(STORAGE_KEY, "")
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bytes")
  })
})

describe("useSpeedUnits persistence and round-trip", () => {
  it("setSpeedUnit('bits') flips the returned unit and persists to localStorage", () => {
    const { result } = renderHook(() => useSpeedUnits())

    act(() => result.current[1]("bits"))

    expect(result.current[0]).toBe("bits")
    expect(localStorage.getItem(STORAGE_KEY)).toBe("bits")
  })

  it("round-trips back to 'bytes' and persists", () => {
    const { result } = renderHook(() => useSpeedUnits())

    act(() => result.current[1]("bits"))
    expect(result.current[0]).toBe("bits")

    act(() => result.current[1]("bytes"))

    expect(result.current[0]).toBe("bytes")
    expect(localStorage.getItem(STORAGE_KEY)).toBe("bytes")
  })
})

describe("useSpeedUnits cross-component / cross-tab sync", () => {
  it("syncs a second hook instance via the same-tab custom event", () => {
    const hookA = renderHook(() => useSpeedUnits())
    const hookB = renderHook(() => useSpeedUnits())

    expect(hookB.result.current[0]).toBe("bytes")

    act(() => hookA.result.current[1]("bits"))

    // hookB never called its own setter; it only saw the custom window event.
    expect(hookB.result.current[0]).toBe("bits")
  })

  it("re-reads localStorage on a native 'storage' event (cross-tab)", () => {
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bytes")

    // Simulate another tab having written the new value.
    localStorage.setItem(STORAGE_KEY, "bits")
    act(() => {
      window.dispatchEvent(new Event("storage"))
    })

    expect(result.current[0]).toBe("bits")
  })
})

describe("useSpeedUnits listener cleanup", () => {
  it("removes its window listeners on unmount and stops reacting to events", () => {
    const removeSpy = vi.spyOn(window, "removeEventListener")
    const { result, unmount } = renderHook(() => useSpeedUnits())
    const last = () => result.current[0]

    unmount()

    expect(removeSpy).toHaveBeenCalledWith("storage", expect.any(Function))
    expect(removeSpy).toHaveBeenCalledWith("qui-client-setting-changed", expect.any(Function))

    // After unmount, external events must not throw or update the stale state.
    localStorage.setItem(STORAGE_KEY, "bits")
    expect(() => {
      window.dispatchEvent(new Event("storage"))
      window.dispatchEvent(new CustomEvent("qui-client-setting-changed", { detail: { key: STORAGE_KEY } }))
    }).not.toThrow()
    expect(last()).toBe("bytes")
  })
})

describe("formatSpeedWithUnit - bytes mode scaling and decimals", () => {
  it("scales across B/s -> KiB/s -> MiB/s -> GiB/s -> TiB/s with k=1024", () => {
    expect(formatSpeedWithUnit(512, "bytes")).toBe("512 B/s")
    expect(formatSpeedWithUnit(1024, "bytes")).toBe("1 KiB/s")
    expect(formatSpeedWithUnit(1024 * 1024, "bytes")).toBe("1 MiB/s")
    expect(formatSpeedWithUnit(1024 ** 3, "bytes")).toBe("1 GiB/s")
    expect(formatSpeedWithUnit(1024 ** 4, "bytes")).toBe("1 TiB/s")
  })

  it("applies the decimals rule (>=100 -> 0, >=10 -> 1, else 2)", () => {
    // value 256 -> 0 decimals
    expect(formatSpeedWithUnit(256, "bytes")).toBe("256 B/s")
    // value ~15.5 KiB -> 1 decimal
    expect(formatSpeedWithUnit(Math.round(15.5 * 1024), "bytes")).toBe("15.5 KiB/s")
    // value 1.5 KiB -> 2 decimals
    expect(formatSpeedWithUnit(Math.round(1.5 * 1024), "bytes")).toBe("1.5 KiB/s")
    expect(formatSpeedWithUnit(Math.round(1.25 * 1024), "bytes")).toBe("1.25 KiB/s")
  })

  it("applies the decimals rule at the exact thresholds (value == 10 -> 1, value == 100 -> 0)", () => {
    // value exactly 10 -> 1 decimal (guards >= vs > off-by-one)
    expect(formatSpeedWithUnit(10 * 1024, "bytes")).toBe("10 KiB/s")
    // value exactly 100 -> 0 decimals (guards >= vs > off-by-one)
    expect(formatSpeedWithUnit(100 * 1024, "bytes")).toBe("100 KiB/s")
  })

  it("compact omits the space and uses bare units (no /s suffix)", () => {
    expect(formatSpeedWithUnit(1024, "bytes", true)).toBe("1KiB")
    expect(formatSpeedWithUnit(512, "bytes", true)).toBe("512B")
  })
})

describe("formatSpeedWithUnit - bits mode scaling and decimals", () => {
  it("multiplies bytes by 8 and scales with decimal k=1000", () => {
    // 125 bytes/s -> 1000 bits/s -> 1 Kbps
    expect(formatSpeedWithUnit(125, "bits")).toBe("1 Kbps")
    // 125000 bytes/s -> 1,000,000 bits/s -> 1 Mbps
    expect(formatSpeedWithUnit(125000, "bits")).toBe("1 Mbps")
    // 1 byte/s -> 8 bps
    expect(formatSpeedWithUnit(1, "bits")).toBe("8 bps")
  })

  it("applies the bits-mode decimals rule across all branches (>=100 -> 0, >=10 -> 1, else 2)", () => {
    // 1937.5 bytes/s -> 15500 bps -> 15.5 Kbps (value ~15.5 -> 1 decimal)
    expect(formatSpeedWithUnit(1937.5, "bits")).toBe("15.5 Kbps")
    // 32000 bytes/s -> 256000 bps -> 256 Kbps (value 256 -> 0 decimals)
    expect(formatSpeedWithUnit(32000, "bits")).toBe("256 Kbps")
    // 156.25 bytes/s -> 1250 bps -> 1.25 Kbps (value <10 -> 2 decimals)
    expect(formatSpeedWithUnit(156.25, "bits")).toBe("1.25 Kbps")
  })

  it("uses 'X Ybps' with a space in non-compact mode", () => {
    const out = formatSpeedWithUnit(125000000, "bits")
    expect(out).toBe("1 Gbps")
    expect(out).toContain(" ")
  })

  it("bits-mode suffixes are identical for compact and non-compact (only zero short-circuit differs)", () => {
    // The compact flag must NOT change the suffix in bits mode.
    expect(formatSpeedWithUnit(125, "bits", true)).toBe("1 Kbps")
    expect(formatSpeedWithUnit(125, "bits", false)).toBe("1 Kbps")
  })
})

describe("formatSpeedWithUnit - clamping to the largest unit", () => {
  it("never indexes past TiB/s for extremely large byte inputs", () => {
    const out = formatSpeedWithUnit(1024 ** 6, "bytes")
    expect(out.endsWith("TiB/s")).toBe(true)
  })

  it("never indexes past Tbps for extremely large bit inputs", () => {
    const out = formatSpeedWithUnit(1e18, "bits")
    expect(out.endsWith("Tbps")).toBe(true)
  })
})

describe("formatSpeedWithUnit - non-positive / non-finite short-circuit", () => {
  it("returns the zero string for 0, negative, NaN and Infinity (non-compact)", () => {
    for (const v of [0, -1, -Infinity, Infinity, NaN]) {
      expect(formatSpeedWithUnit(v, "bytes")).toBe("0 B/s")
      expect(formatSpeedWithUnit(v, "bits")).toBe("0 bps")
    }
  })

  it("returns '0' for the zero/edge cases in compact mode", () => {
    for (const v of [0, -1, Infinity, NaN]) {
      expect(formatSpeedWithUnit(v, "bytes", true)).toBe("0")
      expect(formatSpeedWithUnit(v, "bits", true)).toBe("0")
    }
  })
})

describe("formatSpeedWithUnit - sub-unit rounding underflow", () => {
  it("returns the zero string when a tiny positive value rounds to 0", () => {
    // 0.001 bytes/s is positive (passes the finite/>0 guard) but rounds to 0.
    expect(formatSpeedWithUnit(0.001, "bytes")).toBe("0 B/s")
    expect(formatSpeedWithUnit(0.001, "bytes", true)).toBe("0")
  })

  it("returns the zero string for a tiny positive bits value that rounds to 0", () => {
    // 0.0001 bytes -> 0.0008 bps, rounds to 0 at 2 decimals.
    expect(formatSpeedWithUnit(0.0001, "bits")).toBe("0 bps")
    expect(formatSpeedWithUnit(0.0001, "bits", true)).toBe("0")
  })
})
