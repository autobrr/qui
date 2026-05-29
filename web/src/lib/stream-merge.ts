/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Merge a freshly streamed first page into the currently displayed torrent list.
 *
 * The SSE stream only ever serves page 0 (paginated views fall back to polling), and
 * that page is authoritative for its window: any row the fresh page omits within the
 * first-page window (a torrent that was deleted or moved off page 0) is dropped and
 * never re-added. Rows beyond the first page - loaded earlier via pagination - are
 * preserved and de-duplicated against the fresh page so shifted rows do not appear
 * twice. The result is capped to `total` when provided.
 *
 * Generic over any record carrying a stable `hash` so it can be unit tested without
 * constructing full torrent objects.
 */
export function mergeStreamedFirstPage<T extends { hash: string }>(
  prev: T[],
  nextTorrents: T[],
  total?: number
): T[] {
  if (nextTorrents.length === 0) {
    return []
  }

  if (prev.length === 0) {
    return nextTorrents
  }

  const seen = new Set(nextTorrents.map(torrent => torrent.hash))

  // Rows past the streamed first page are pagination-loaded pages we want to keep.
  // Deduping against the fresh page drops any that shifted up into page 0.
  const trailing = prev.slice(nextTorrents.length).filter(torrent => !seen.has(torrent.hash))

  const merged = [...nextTorrents, ...trailing]

  if (typeof total === "number" && merged.length > total) {
    return merged.slice(0, total)
  }

  return merged
}
