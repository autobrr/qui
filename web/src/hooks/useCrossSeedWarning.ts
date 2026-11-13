/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useRef, useState } from "react"

import { api } from "@/lib/api"
import { toCompatibleMatch, type CrossSeedTorrent } from "@/lib/cross-seed-utils"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import type { Torrent, TorrentFilters, UncheckedInstance } from "@/types"

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
  /** Instances that the check could not read, so the list can miss torrents on them */
  uncheckedInstances: UncheckedInstance[]
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
      // The body can finish reading after the abort.
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
