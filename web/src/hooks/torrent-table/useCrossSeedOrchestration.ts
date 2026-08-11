/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { anyTorrentHasTag } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { useMemo } from "react"

export interface UseCrossSeedOrchestrationParams {
  instanceId: number
  instanceName: string
  contextTorrents: Torrent[]
  blockCrossSeeds: boolean
}

/**
 * Resolves the cross-seed state for the current action context: the warning
 * (affected torrents), whether the selection carries the cross-seed tag, whether
 * cross-seeds should be blocked on delete, and the blocklist action.
 */
export function useCrossSeedOrchestration({
  instanceId,
  instanceName,
  contextTorrents,
  blockCrossSeeds,
}: UseCrossSeedOrchestrationParams) {
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    instanceName,
    torrents: contextTorrents,
  })
  const hasCrossSeedTag = useMemo(
    () => anyTorrentHasTag(contextTorrents, "cross-seed") || anyTorrentHasTag(crossSeedWarning.affectedTorrents, "cross-seed"),
    [contextTorrents, crossSeedWarning.affectedTorrents]
  )
  const shouldBlockCrossSeeds = hasCrossSeedTag && blockCrossSeeds
  const { blockCrossSeedHashes } = useCrossSeedBlocklistActions(instanceId)

  return { crossSeedWarning, hasCrossSeedTag, shouldBlockCrossSeeds, blockCrossSeedHashes }
}
