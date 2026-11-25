/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { topTorrents, type StatisticsPeriod, type TopTorrent } from "@/lib/statistics"
import type { TransferHistoryGrouping, TransferHistoryInstance } from "@/types"
import { useCallback, useMemo } from "react"

export function useTorrentName(): (torrent: TopTorrent) => string {
  const [incognitoMode] = useIncognitoMode()
  return useCallback((torrent: TopTorrent) => incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name, [incognitoMode])
}

export function useTopTorrents(instances: TransferHistoryInstance[], period: StatisticsPeriod, limit: number, group?: { grouping: TransferHistoryGrouping; name: string }) {
  const transfer = useTransferHistoryRows(instances, {
    from: period.from,
    to: period.to,
    bucket: "total",
    groupBy: "torrent",
    groupOf: group?.grouping,
    group: group?.name,
    limit,
  })
  const ids = useMemo(() => instances.map(instance => instance.instanceId), [instances])
  const torrents = useMemo(() => transfer.rows && topTorrents(transfer.rows, ids, !period.from, limit), [transfer.rows, ids, period.from, limit])
  return { torrents, error: transfer.error }
}
