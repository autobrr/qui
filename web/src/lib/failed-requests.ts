/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/** A request of the API client that failed in this browser session. */
export interface FailedRequest {
  method: string
  /** The path without the query. */
  path: string
  /** Missing when the request got no answer. */
  status?: number
  error: string
  time: string
}

const limit = 20
let entries: readonly FailedRequest[] = []

/** Keeps the request in memory only, and drops the oldest past the last 20. */
export function recordFailedRequest(
  url: string,
  method: string | undefined,
  status: number | undefined,
  error: string,
  time = new Date().toISOString()
) {
  const entry: FailedRequest = {
    method: (method ?? "GET").toUpperCase(),
    path: new URL(url, window.location.href).pathname,
    status,
    error,
    time,
  }
  entries = [...entries.slice(1 - limit), entry]
}

/** The last 20 failed requests of this browser session, oldest first. */
export function failedRequests(): readonly FailedRequest[] {
  return entries
}
