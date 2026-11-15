/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { LIVE_SAMPLE_MS, useLiveSpeedSamples } from "@/hooks/useLiveSpeedSamples"
import type { DashboardInstanceStats } from "@/lib/dashboard-overview"

function instance(id: number, connected: boolean): DashboardInstanceStats {
  return {
    instance: { id, connected },
    stats: { totalUploadSpeed: 100 * id, totalDownloadSpeed: 10 * id },
    isLoading: false,
  } as unknown as DashboardInstanceStats
}

function setVisibility(state: DocumentVisibilityState) {
  Object.defineProperty(document, "visibilityState", { configurable: true, value: state })
}

afterEach(() => {
  cleanup()
  vi.useRealTimers()
  setVisibility("visible")
})

describe("useLiveSpeedSamples", () => {
  // regression: #4141
  it.each([
    ["a stream error", { streamError: "instance unreachable" }],
    ["a failed fallback poll", { error: new Error("fallback failed") }],
  ])("skips an instance with %s, so its last speed does not repeat", (_name, failure) => {
    vi.useFakeTimers()
    setVisibility("visible")
    const { result } = renderHook(() => useLiveSpeedSamples([instance(1, true), { ...instance(2, true), ...failure }]))

    act(() => {
      vi.advanceTimersByTime(LIVE_SAMPLE_MS)
    })
    expect(result.current.map(sample => [...sample.speeds.keys()])).toEqual([[1]])
  })


  // regression: #4141
  it("samples the instances that answer, and starts again while the page is hidden", () => {
    vi.useFakeTimers()
    setVisibility("visible")
    const { result } = renderHook(() => useLiveSpeedSamples([instance(1, true), instance(2, false)]))
    expect(result.current).toEqual([])

    act(() => {
      vi.advanceTimersByTime(2 * LIVE_SAMPLE_MS)
    })
    expect(result.current.map(sample => [...sample.speeds])).toEqual([
      [[1, { up: 100, down: 10 }]],
      [[1, { up: 100, down: 10 }]],
    ])

    setVisibility("hidden")
    act(() => {
      document.dispatchEvent(new Event("visibilitychange"))
    })
    expect(result.current).toEqual([])
  })

  // regression: #4141
  it("drops the samples that are older than 3 minutes after the browser paused the timer", () => {
    vi.useFakeTimers()
    setVisibility("visible")
    const { result } = renderHook(() => useLiveSpeedSamples([instance(1, true)]))
    act(() => {
      vi.advanceTimersByTime(2 * LIVE_SAMPLE_MS)
    })
    expect(result.current).toHaveLength(2)

    vi.setSystemTime(Date.now() + 10 * 60_000)
    act(() => {
      vi.advanceTimersByTime(LIVE_SAMPLE_MS)
    })
    expect(result.current).toHaveLength(1)
  })
})
