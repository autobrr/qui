/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { CrossInstanceTorrent, Torrent } from "@/types"

export interface TorrentActionTarget {
  instanceId: number
  hash: string
}

export function getTorrentTargetInstanceId(torrent: Torrent, fallbackInstanceId: number): number {
  const crossInstanceId = (torrent as Partial<CrossInstanceTorrent>).instanceId
  if (typeof crossInstanceId === "number" && crossInstanceId > 0) {
    return crossInstanceId
  }

  return fallbackInstanceId
}

export function buildTorrentActionTargets(
  torrents: Torrent[],
  fallbackInstanceId: number
): TorrentActionTarget[] {
  const seen = new Set<string>()
  const targets: TorrentActionTarget[] = []

  for (const torrent of torrents) {
    const hash = torrent.hash?.trim()
    if (!hash) {
      continue
    }

    const instanceId = getTorrentTargetInstanceId(torrent, fallbackInstanceId)
    if (instanceId <= 0) {
      continue
    }

    const dedupeKey = `${instanceId}:${hash.toLowerCase()}`
    if (seen.has(dedupeKey)) {
      continue
    }

    seen.add(dedupeKey)
    targets.push({ instanceId, hash })
  }

  return targets
}
