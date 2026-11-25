/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it } from "vitest"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import "@/i18n"

const STORAGE_KEY = "qui-speed-units"

beforeEach(() => {
  localStorage.clear()
})

afterEach(cleanup)

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

    expect(hookB.result.current[0]).toBe("bits")
  })

  it("re-reads localStorage on a native 'storage' event (cross-tab)", () => {
    const { result } = renderHook(() => useSpeedUnits())
    expect(result.current[0]).toBe("bytes")

    localStorage.setItem(STORAGE_KEY, "bits")
    act(() => {
      window.dispatchEvent(new Event("storage"))
    })

    expect(result.current[0]).toBe("bits")
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
    expect(formatSpeedWithUnit(256, "bytes")).toBe("256 B/s")
    expect(formatSpeedWithUnit(Math.round(15.5 * 1024), "bytes")).toBe("15.5 KiB/s")
    expect(formatSpeedWithUnit(Math.round(1.5 * 1024), "bytes")).toBe("1.5 KiB/s")
    expect(formatSpeedWithUnit(Math.round(1.25 * 1024), "bytes")).toBe("1.25 KiB/s")
  })

  it("compact omits the space and uses bare units (no /s suffix)", () => {
    expect(formatSpeedWithUnit(1024, "bytes", true)).toBe("1KiB")
    expect(formatSpeedWithUnit(512, "bytes", true)).toBe("512B")
  })
})

describe("formatSpeedWithUnit - bits mode scaling and decimals", () => {
  it("multiplies bytes by 8 and scales with decimal k=1000", () => {
    expect(formatSpeedWithUnit(125, "bits")).toBe("1 Kb/s")
    expect(formatSpeedWithUnit(125000, "bits")).toBe("1 Mb/s")
    expect(formatSpeedWithUnit(1, "bits")).toBe("8 b/s")
  })

  it("applies the bits-mode decimals rule across all branches (>=100 -> 0, >=10 -> 1, else 2)", () => {
    expect(formatSpeedWithUnit(1937.5, "bits")).toBe("15.5 Kb/s")
    expect(formatSpeedWithUnit(32000, "bits")).toBe("256 Kb/s")
    expect(formatSpeedWithUnit(156.25, "bits")).toBe("1.25 Kb/s")
  })

  it("bits-mode suffixes are identical for compact and non-compact (only zero short-circuit differs)", () => {
    expect(formatSpeedWithUnit(125, "bits", true)).toBe("1 Kb/s")
    expect(formatSpeedWithUnit(125, "bits", false)).toBe("1 Kb/s")
  })
})

describe("formatSpeedWithUnit - clamping to the largest unit", () => {
  it("never indexes past TiB/s for extremely large byte inputs", () => {
    const out = formatSpeedWithUnit(1024 ** 6, "bytes")
    expect(out.endsWith("TiB/s")).toBe(true)
  })

  it("never indexes past Tb/s for extremely large bit inputs", () => {
    const out = formatSpeedWithUnit(1e18, "bits")
    expect(out.endsWith("Tb/s")).toBe(true)
  })
})

describe("formatSpeedWithUnit - non-positive / non-finite short-circuit", () => {
  it("returns the zero string for 0, negative, NaN and Infinity (non-compact)", () => {
    for (const v of [0, -1, -Infinity, Infinity, NaN]) {
      expect(formatSpeedWithUnit(v, "bytes")).toBe("0 B/s")
      expect(formatSpeedWithUnit(v, "bits")).toBe("0 b/s")
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
    expect(formatSpeedWithUnit(0.001, "bytes")).toBe("0 B/s")
    expect(formatSpeedWithUnit(0.001, "bytes", true)).toBe("0")
  })

  it("returns the zero string for a tiny positive bits value that rounds to 0", () => {
    expect(formatSpeedWithUnit(0.0001, "bits")).toBe("0 b/s")
    expect(formatSpeedWithUnit(0.0001, "bits", true)).toBe("0")
  })
})
