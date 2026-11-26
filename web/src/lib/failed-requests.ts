/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

export interface FailedRequest {
  method: string
  path: string
  status?: number
  error: string
  time: string
}

const limit = 20
let entries: readonly FailedRequest[] = []

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

export function failedRequests(): readonly FailedRequest[] {
  return entries
}
