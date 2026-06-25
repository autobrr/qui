// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

export type QueryFreshnessState = "loading" | "fresh" | "stale" | "cold-error"

export interface QueryFreshnessInput {
  hasData: boolean
  isError: boolean
  dataUpdatedAt: number
  now: number
}

export interface QueryFreshness {
  state: QueryFreshnessState
  ageMs: number
}

/**
 * resolveQueryFreshness classifies a TanStack Query v5 result into a freshness
 * state for cached-data UI.
 *
 * TanStack Query retains cached data across a failed refetch, so `isError` can be
 * true while `hasData` is also true. In that case we treat the data as stale but
 * still usable rather than a cold error. Only a failure with no data on hand is a
 * true cold error; no data and no error means we are still loading.
 *
 * Order matters: cold-error (no data + error) is checked before loading (no data),
 * then stale (data + error), then fresh.
 */
export function resolveQueryFreshness(input: QueryFreshnessInput): QueryFreshness {
  const { hasData, isError, dataUpdatedAt, now } = input

  if (!hasData && isError) {
    return { state: "cold-error", ageMs: 0 }
  }

  if (!hasData) {
    return { state: "loading", ageMs: 0 }
  }

  const ageMs = Math.max(0, now - dataUpdatedAt)

  if (isError) {
    return { state: "stale", ageMs }
  }

  return { state: "fresh", ageMs }
}
