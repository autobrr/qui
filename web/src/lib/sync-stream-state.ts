/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { StreamState } from "@/contexts/SyncStreamContext"

// Client-side connection-state error codes. These are stable machine identifiers,
// not human-readable prose: the UI maps them to localized streamStatus.* copy so
// non-English locales never see English text. entry.error stays truthy for these
// so the existing polling-fallback gates keep treating the stream as unhealthy.
// Genuine backend payload errors (stream-error events) are stored verbatim because
// they are dynamic server text that cannot be translated client-side.
export const STREAM_ERROR_UNSUPPORTED = "client:eventsource-unsupported"
export const STREAM_ERROR_DISCONNECTED = "client:disconnected"
export const STREAM_ERROR_RETRY_EXHAUSTED = "client:retry-exhausted"

const CLIENT_CONNECTION_ERROR_CODES: ReadonlySet<string> = new Set([
  STREAM_ERROR_UNSUPPORTED,
  STREAM_ERROR_DISCONNECTED,
  STREAM_ERROR_RETRY_EXHAUSTED,
])

const STALLED_STREAM_POLL_INTERVAL_MS = 30000

// isClientConnectionErrorCode reports whether a StreamState.error value is one of
// the client-side connection-state codes above (as opposed to backend payload
// error text). Components use it to decide whether to render the localized
// connection-status message or the raw backend error, and
// streamFallbackPollInterval uses it to keep polling after a dropped connection.
export function isClientConnectionErrorCode(error: string | null | undefined): boolean {
  return error != null && CLIENT_CONNECTION_ERROR_CODES.has(error)
}

export function isStreamUsable(state: StreamState): boolean {
  return state.connected && state.initialized && !state.dataStalled && !state.error
}

// streamFallbackPollInterval gives the interval of a poll that stands in for the
// stream. A stalled view polls slowly, because a hung sync returns the same old
// data. A view with a server error does not poll: the open stream sends the new
// data when qBittorrent comes back, and each poll can start a sync attempt.
export function streamFallbackPollInterval(state: StreamState, intervalMs: number): number | false {
  if (state.error && !isClientConnectionErrorCode(state.error)) {
    return false
  }
  if (state.dataStalled) {
    return Math.max(intervalMs, STALLED_STREAM_POLL_INTERVAL_MS)
  }
  return intervalMs
}
