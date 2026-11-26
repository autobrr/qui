/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { StreamState } from "@/contexts/SyncStreamContext"

export const STREAM_ERROR_UNSUPPORTED = "client:eventsource-unsupported"
export const STREAM_ERROR_DISCONNECTED = "client:disconnected"
export const STREAM_ERROR_RETRY_EXHAUSTED = "client:retry-exhausted"

const CLIENT_CONNECTION_ERROR_CODES: ReadonlySet<string> = new Set([
  STREAM_ERROR_UNSUPPORTED,
  STREAM_ERROR_DISCONNECTED,
  STREAM_ERROR_RETRY_EXHAUSTED,
])

const STALLED_STREAM_POLL_INTERVAL_MS = 30000

export function isClientConnectionErrorCode(error: string | null | undefined): boolean {
  return error != null && CLIENT_CONNECTION_ERROR_CODES.has(error)
}

export function isStreamUsable(state: StreamState): boolean {
  return state.connected && state.initialized && !state.dataStalled && !state.error
}

export function streamFallbackPollInterval(state: StreamState, intervalMs: number): number | false {
  if (state.error && !isClientConnectionErrorCode(state.error)) {
    return false
  }
  if (state.dataStalled) {
    return Math.max(intervalMs, STALLED_STREAM_POLL_INTERVAL_MS)
  }
  return intervalMs
}
