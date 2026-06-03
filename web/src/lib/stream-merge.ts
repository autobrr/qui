/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

/**
 * Merge a freshly streamed first page into the currently displayed torrent list.
 *
 * The SSE stream only ever serves page 0 (paginated views fall back to polling).
 * That page is authoritative for its window: any row the fresh page omits from the
 * page-0 window (a torrent that was deleted or moved off page 0) is dropped and never
 * re-added. Rows beyond the page-0 window, loaded earlier via pagination, are kept and
 * de-duplicated against the fresh page so a row that reflowed up into page 0 is not
 * shown twice.
 *
 * `total` (the server's total match count) tells us whether page 0 is the whole list
 * or only the first slice of a paginated result:
 *
 *   - When the fresh page already covers every row (`nextTorrents.length >= total`),
 *     it is the complete, authoritative truth and is returned as-is. This correctly
 *     drops a row deleted off a single visible page, including the last-sorted one.
 *
 *   - When the result is paginated (`nextTorrents.length < total`), page 0 cannot
 *     reveal which later-page row was removed when `total` shrinks, so we must NOT
 *     tail-trim the merged list against `total`: doing so would drop a genuinely
 *     surviving highest-sorted row while leaving the deleted one in place. We keep
 *     every later-page row the client already holds; at worst one deleted later-page
 *     row lingers until the user re-paginates or the page is refetched, which is far
 *     less harmful than hiding a row that still exists.
 *
 * Generic over any record carrying a stable `hash` so it can be unit tested without
 * constructing full torrent objects. `keyOf` selects the identity used for the page-0
 * window dedup; it defaults to `hash` for single-instance rows, but aggregated
 * (cross-instance) views pass `instanceId+hash` so cross-seeded copies of one torrent
 * living on different instances stay distinct rows instead of collapsing into one.
 */
export function mergeStreamedFirstPage<T extends { hash: string }>(
  prev: T[],
  nextTorrents: T[],
  total?: number,
  keyOf: (torrent: T) => string = torrent => torrent.hash
): T[] {
  if (nextTorrents.length === 0) {
    return []
  }

  if (prev.length === 0) {
    return nextTorrents
  }

  // The fresh page already covers the entire result set, so it is the whole truth.
  // Returning it as-is drops any row deleted off the single visible page (no later
  // pages exist to preserve).
  if (typeof total === "number" && nextTorrents.length >= total) {
    return nextTorrents
  }

  const seen = new Set(nextTorrents.map(keyOf))

  // Rows past the streamed page-0 window are pagination-loaded pages we want to keep.
  // De-duping against the fresh page drops any that reflowed up into page 0.
  const trailing = prev.slice(nextTorrents.length).filter(torrent => !seen.has(keyOf(torrent)))

  return [...nextTorrents, ...trailing]
}
