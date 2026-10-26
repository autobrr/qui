/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import { toCompatibleMatch, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { Torrent, TorrentFilters } from "@/types"

/** The select-all scope of a delete, as the bulk action sends it. */
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
  /** Set in Select all mode: the check then covers every selected torrent, not only `torrents`. */
  selectAll?: CrossSeedSelectAllScope
}

export type CrossSeedSearchState = "idle" | "searching" | "complete" | "error"

export interface CrossSeedWarningResult {
  /** Cross-seed torrents that share files with torrents being deleted */
  affectedTorrents: CrossSeedTorrent[]
  /** Current search state */
  searchState: CrossSeedSearchState
  /** Whether there are cross-seeds that would be affected */
  hasWarning: boolean
  /** Trigger the cross-seed search */
  search: () => void
  /** Reset the search state and stop a running search */
  reset: () => void
}

/**
 * Hook to detect cross-seeded torrents that would be affected when deleting files.
 *
 * Search is opt-in - call `search()` to check for cross-seeds. One request
 * checks every selected torrent; the backend leaves the selected torrents out
 * of the result. `reset()` and unmount stop a running search.
 */
export function useCrossSeedWarning({
  instanceId,
  torrents,
  selectAll,
}: UseCrossSeedWarningOptions): CrossSeedWarningResult {
  const [searchState, setSearchState] = useState<CrossSeedSearchState>("idle")
  const [affectedTorrents, setAffectedTorrents] = useState<CrossSeedTorrent[]>([])
  const abortRef = useRef<AbortController | null>(null)

  useEffect(() => () => abortRef.current?.abort(), [])

  const search = useCallback(async () => {
    if (!selectAll && torrents.length === 0) return

    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller

    setSearchState("searching")
    setAffectedTorrents([])

    try {
      const selection = selectAll ? { selectAll: true, ...selectAll } : { targets: buildTorrentActionTargets(torrents, instanceId) }
      const matches = await api.getDeleteCrossSeeds(instanceId, selection, controller.signal)
      setAffectedTorrents(matches.map(toCompatibleMatch))
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
  }, [])

  return {
    affectedTorrents,
    searchState,
    hasWarning: affectedTorrents.length > 0,
    search,
    reset,
  }
}
