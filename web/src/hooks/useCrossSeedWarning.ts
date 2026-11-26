/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import { toCompatibleMatch, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { Torrent, TorrentFilters, UncheckedInstance } from "@/types"

export interface CrossSeedSelectAllScope {
  filters?: TorrentFilters
  search?: string
  excludeHashes?: string[]
  excludeTargets?: TorrentActionTarget[]
  instanceIds?: number[]
}

interface UseCrossSeedWarningOptions {
  instanceId: number
  torrents: Torrent[]
  selectAll?: CrossSeedSelectAllScope
}

export type CrossSeedSearchState = "idle" | "searching" | "complete" | "error"

export interface CrossSeedWarningResult {
  affectedTorrents: CrossSeedTorrent[]
  uncheckedInstances: UncheckedInstance[]
  searchState: CrossSeedSearchState
  hasWarning: boolean
  search: () => void
  reset: () => void
}

export function useCrossSeedWarning({
  instanceId,
  torrents,
  selectAll,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  const [searchState, setSearchState] = useState<CrossSeedSearchState>("idle")
  const [affectedTorrents, setAffectedTorrents] = useState<CrossSeedTorrent[]>([])
  const [uncheckedInstances, setUncheckedInstances] = useState<UncheckedInstance[]>([])
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => () => abortRef.current?.abort(), [])

  const search = useCallback(async () => {
    abortRef.current?.abort()
    if (!selectAll && torrents.length === 0) return

    const controller = new AbortController()
    abortRef.current = controller

    setSearchState("searching")
    setAffectedTorrents([])
    setUncheckedInstances([])

    try {
      const selection = selectAll ? { selectAll: true, ...selectAll } : { targets: buildTorrentActionTargets(torrents, instanceId) }
      const result = await api.getDeleteCrossSeeds(instanceId, selection, controller.signal)
      if (controller.signal.aborted) return
      setAffectedTorrents(result.matches.map(toCompatibleMatch))
      setUncheckedInstances(result.uncheckedInstances)
      setSearchState("complete")
    } catch (error) {
      if (controller.signal.aborted) return
      console.error("[CrossSeedWarning] Search failed:", error)
      setSearchState("error")
    }
  }, [instanceId, torrents, selectAll])

  const reset = useCallback(() => {
    abortRef.current?.abort()
    abortRef.current = null
    setSearchState("idle")
    setAffectedTorrents([])
    setUncheckedInstances([])
  }, [])

  return {
    affectedTorrents,
    uncheckedInstances,
    searchState,
    hasWarning: affectedTorrents.length > 0,
    search,
    reset,
  }
}
