/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { mergeStreamedFirstPage } from "@/lib/stream-merge"
import type { CrossInstanceTorrent, Torrent, TorrentResponse } from "@/types"

// RawCrossInstanceTorrent models a cross-instance torrent before normalization.
// The backend's CrossInstanceTorrentView serializes instance metadata as
// snake_case (instance_id / instance_name) over both the REST endpoint and the
// SSE stream, so callers may receive either casing.
export type RawCrossInstanceTorrent = Omit<CrossInstanceTorrent, "instanceId" | "instanceName"> & {
  instanceId?: number
  instanceName?: string
  instance_id?: number
  instance_name?: string
}

// normalizeCrossInstanceTorrents promotes snake_case instance metadata to the
// camelCase instanceId / instanceName the UI consumes. It is a no-op when the
// torrents already carry camelCase fields, and drops rows that lack instance
// identity entirely rather than render blank Instance cells. Shared by the REST
// query and the SSE stream handler so both paths produce identical row shapes.
export function normalizeCrossInstanceTorrents(
  torrents?: RawCrossInstanceTorrent[] | null
): CrossInstanceTorrent[] | undefined {
  if (!torrents) {
    return undefined
  }

  let needsNormalization = false

  for (const torrent of torrents) {
    if (torrent.instanceId === undefined || torrent.instanceName === undefined) {
      needsNormalization = true
      break
    }
  }

  if (!needsNormalization) {
    return torrents as CrossInstanceTorrent[]
  }

  const normalizedTorrents: CrossInstanceTorrent[] = []

  torrents.forEach(torrent => {
    const instanceId = torrent.instanceId ?? torrent.instance_id
    const instanceName = torrent.instanceName ?? torrent.instance_name

    if (instanceId === undefined || instanceName === undefined) {
      console.error("Missing instance fields in cross-instance torrent:", torrent)
      return
    }

    normalizedTorrents.push({
      ...torrent,
      instanceId,
      instanceName,
    })
  })

  return normalizedTorrents
}

// normalizeStreamedSnapshot returns the SSE snapshot with its cross-instance
// torrents promoted to camelCase, leaving every other field untouched. The
// stream handler feeds this single normalized object to all of its sinks (the
// React Query cache, the retained snapshot, and the table rows) so they never
// disagree. Writing a raw snake_case snapshot into the query cache would let the
// REST-processing effect overwrite the table with un-normalized rows on the next
// tick, flickering the Instance column. Returns the input unchanged when there
// are no cross-instance torrents to normalize (e.g. single-instance streams).
export function normalizeStreamedSnapshot(data: TorrentResponse): TorrentResponse {
  const normalized = normalizeCrossInstanceTorrents(
    (data.crossInstanceTorrents ?? data.cross_instance_torrents) as RawCrossInstanceTorrent[] | undefined
  )

  if (!normalized) {
    return data
  }

  return {
    ...data,
    crossInstanceTorrents: normalized,
    cross_instance_torrents: normalized,
  }
}

// resolveStreamedCrossInstanceTorrents turns an SSE stream snapshot into the row
// list the unified table renders. Aggregated streams deliver the full first page,
// so the snapshot is authoritative: an empty result (or total 0) clears the table,
// and the torrents are normalized to camelCase before they reach the Instance
// column. Keeping this beside the normalizer makes the stream call site testable
// and prevents regressing back to setting raw snake_case rows.
export function resolveStreamedCrossInstanceTorrents(
  data: Pick<TorrentResponse, "total" | "crossInstanceTorrents" | "cross_instance_torrents">
): CrossInstanceTorrent[] {
  const normalized = normalizeCrossInstanceTorrents(
    (data.crossInstanceTorrents ?? data.cross_instance_torrents) as RawCrossInstanceTorrent[] | undefined
  ) ?? []

  if (data.total === 0 || normalized.length === 0) {
    return []
  }

  return normalized
}

// A cross-instance row's identity is instanceId+hash, not hash alone: the same
// torrent cross-seeded onto two instances shares a hash but is two distinct rows.
const crossInstanceRowKey = (torrent: CrossInstanceTorrent): string =>
  `${torrent.instanceId}:${torrent.hash}`

// mergeStreamedCrossInstanceFirstPage folds an aggregated SSE snapshot into the
// list the unified table already displays. The stream only ever serves page 0, so
// a wholesale replace would wipe any later pages the user paginated in via REST and
// the unified view could never scroll past the first page (issue #1983). Instead we
// merge: page 0 stays authoritative for its own window, while pagination-loaded
// trailing pages are preserved and de-duplicated by instanceId+hash. `prev` is the
// state list typed as the Torrent supertype; in aggregated scope every row is in
// fact a CrossInstanceTorrent (it carries instanceId+instanceName), so we treat it
// as such for the identity key.
export function mergeStreamedCrossInstanceFirstPage(
  prev: Torrent[],
  data: Pick<TorrentResponse, "total" | "crossInstanceTorrents" | "cross_instance_torrents">
): CrossInstanceTorrent[] {
  return mergeStreamedFirstPage(
    prev as CrossInstanceTorrent[],
    resolveStreamedCrossInstanceTorrents(data),
    typeof data.total === "number" ? data.total : undefined,
    crossInstanceRowKey
  )
}
