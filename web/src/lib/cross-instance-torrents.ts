/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { applyOrderedDelta, mergeStreamedFirstPage } from "@/lib/stream-merge"
import type { CrossInstanceTorrent, Torrent, TorrentResponse, TorrentStreamPayload } from "@/types"

export type RawCrossInstanceTorrent = Omit<CrossInstanceTorrent, "instanceId" | "instanceName"> & {
  instanceId?: number
  instanceName?: string
  instance_id?: number
  instance_name?: string
}

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

const crossInstanceRowKey = (torrent: CrossInstanceTorrent): string =>
  `${torrent.instanceId}:${torrent.hash}`

const singleInstanceRowKey = (torrent: Torrent): string => torrent.hash

export function applyStreamDelta(
  prev: TorrentResponse,
  payload: TorrentStreamPayload,
  isCrossInstance: boolean
): { data: TorrentResponse; changed: boolean } {
  const order = payload.delta?.order
  const frame = payload.data ?? ({} as TorrentResponse)
  const snapshot = {
    counts: prev.counts,
    ...frame,
  }

  if (isCrossInstance) {
    const prevRows = prev.crossInstanceTorrents ?? []
    const changedRows = normalizeCrossInstanceTorrents(
      (frame.crossInstanceTorrents ?? frame.cross_instance_torrents) as RawCrossInstanceTorrent[] | undefined
    ) ?? []

    const { rows, changed } = applyOrderedDelta(prevRows, changedRows, order, crossInstanceRowKey)

    return {
      data: {
        ...snapshot,
        crossInstanceTorrents: rows,
        cross_instance_torrents: rows,
      },
      changed,
    }
  }

  const prevRows = prev.torrents ?? []
  const changedRows = frame.torrents ?? []
  const { rows, changed } = applyOrderedDelta(prevRows, changedRows, order, singleInstanceRowKey)

  return {
    data: {
      ...snapshot,
      torrents: rows,
    },
    changed,
  }
}

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
