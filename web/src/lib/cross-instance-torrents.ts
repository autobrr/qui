/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossInstanceTorrent, TorrentResponse } from "@/types"

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
