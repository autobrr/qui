// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

import { describe, expect, it } from "vitest"

import { resolveQueryFreshness } from "@/lib/stale-query"

describe("resolveQueryFreshness", () => {
  it("returns loading when there is no data and no error", () => {
    const result = resolveQueryFreshness({
      hasData: false,
      isError: false,
      dataUpdatedAt: 1_000,
      now: 5_000,
    })

    expect(result).toEqual({ state: "loading", ageMs: 0 })
  })

  it("returns cold-error when there is no data and an error", () => {
    const result = resolveQueryFreshness({
      hasData: false,
      isError: true,
      dataUpdatedAt: 1_000,
      now: 5_000,
    })

    expect(result).toEqual({ state: "cold-error", ageMs: 0 })
  })

  it("returns stale with computed age when data is present and an error occurred", () => {
    const result = resolveQueryFreshness({
      hasData: true,
      isError: true,
      dataUpdatedAt: 1_000,
      now: 5_000,
    })

    expect(result).toEqual({ state: "stale", ageMs: 4_000 })
  })

  it("returns fresh with computed age when data is present and there is no error", () => {
    const result = resolveQueryFreshness({
      hasData: true,
      isError: false,
      dataUpdatedAt: 1_000,
      now: 5_000,
    })

    expect(result).toEqual({ state: "fresh", ageMs: 4_000 })
  })

  it("clamps a negative age to 0 when now precedes dataUpdatedAt", () => {
    const fresh = resolveQueryFreshness({
      hasData: true,
      isError: false,
      dataUpdatedAt: 5_000,
      now: 1_000,
    })

    expect(fresh).toEqual({ state: "fresh", ageMs: 0 })

    const stale = resolveQueryFreshness({
      hasData: true,
      isError: true,
      dataUpdatedAt: 5_000,
      now: 1_000,
    })

    expect(stale).toEqual({ state: "stale", ageMs: 0 })
  })

  it("reports ageMs of 0 whenever there is no data regardless of timestamps", () => {
    const loading = resolveQueryFreshness({
      hasData: false,
      isError: false,
      dataUpdatedAt: 1_000,
      now: 9_999,
    })

    expect(loading.ageMs).toBe(0)

    const coldError = resolveQueryFreshness({
      hasData: false,
      isError: true,
      dataUpdatedAt: 1_000,
      now: 9_999,
    })

    expect(coldError.ageMs).toBe(0)
  })
})
