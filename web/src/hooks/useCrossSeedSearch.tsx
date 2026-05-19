/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { toast } from "sonner"

import { CrossSeedDialog } from "@/components/torrents/CrossSeedDialog"
import { api } from "@/lib/api"
import type {
  CrossSeedApplyResponse,
  CrossSeedTorrentSearchResponse,
  CrossSeedTorrentSearchSelection,
  Torrent,
  TorznabIndexer
} from "@/types"

const CROSS_SEED_REFRESH_COOLDOWN_MS = 30_000

export function useCrossSeedSearch(instanceId: number) {
  const queryClient = useQueryClient()

  const { data: torznabIndexers } = useQuery({
    queryKey: ["torznab", "indexers"],
    queryFn: () => api.listTorznabIndexers(),
    staleTime: 5 * 60 * 1000,
  })

  const enabledTorznabIndexers = useMemo(
    () => (torznabIndexers ?? []).filter(indexer => indexer.enabled),
    [torznabIndexers]
  )

  const sortedEnabledIndexers = useMemo(() => {
    if (enabledTorznabIndexers.length === 0) {
      return [] as TorznabIndexer[]
    }

    return [...enabledTorznabIndexers].sort((a, b) => {
      if (b.priority !== a.priority) {
        return b.priority - a.priority
      }
      return a.name.localeCompare(b.name)
    })
  }, [enabledTorznabIndexers])

  const [crossSeedDialogOpen, setCrossSeedDialogOpen] = useState(false)
  const [crossSeedTorrent, setCrossSeedTorrent] = useState<Torrent | null>(null)
  const [crossSeedSearchResponse, setCrossSeedSearchResponse] = useState<CrossSeedTorrentSearchResponse | null>(null)
  const [crossSeedSearchLoading, setCrossSeedSearchLoading] = useState(false)
  const [crossSeedSearchError, setCrossSeedSearchError] = useState<string | null>(null)
  const [crossSeedSelectedKeys, setCrossSeedSelectedKeys] = useState<Set<string>>(new Set())
  const [crossSeedUseTag, setCrossSeedUseTag] = useState(true)
  const [crossSeedTagName, setCrossSeedTagName] = useState("cross-seed")
  const [crossSeedStartPaused, setCrossSeedStartPaused] = useState(true)
  const [crossSeedSubmitting, setCrossSeedSubmitting] = useState(false)
  const [crossSeedApplyResult, setCrossSeedApplyResult] = useState<CrossSeedApplyResponse | null>(null)
  const [crossSeedIndexerMode, setCrossSeedIndexerMode] = useState<"all" | "custom">("all")
  const [crossSeedIndexerSelection, setCrossSeedIndexerSelection] = useState<number[]>([])
  const [crossSeedHasSearched, setCrossSeedHasSearched] = useState(false)
  const [crossSeedRefreshCooldownUntil, setCrossSeedRefreshCooldownUntil] = useState(0)

  const crossSeedPollingRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const crossSeedPollingInFlightRef = useRef(false)

  const { data: crossSeedSettings } = useQuery({
    queryKey: ["cross-seed", "settings"],
    queryFn: () => api.getCrossSeedSettings(),
    staleTime: 5 * 60 * 1000,
  })

  const crossSeedIndexerOptions = useMemo(() => {
    const allowedIds = crossSeedSearchResponse?.sourceTorrent?.availableIndexers
    const filteredIds = crossSeedSearchResponse?.sourceTorrent?.filteredIndexers
    const excludedIds = crossSeedSearchResponse?.sourceTorrent?.excludedIndexers

    let candidateIds: Set<number> | null = null
    if (allowedIds && allowedIds.length > 0) {
      candidateIds = new Set(allowedIds)
    } else if (filteredIds && filteredIds.length > 0) {
      candidateIds = new Set(filteredIds)
    }

    const excludedIdSet = excludedIds? new Set(
      Object.keys(excludedIds)
        .map(id => Number(id))
        .filter(id => !Number.isNaN(id))
    ): null

    return sortedEnabledIndexers
      .filter(indexer => (!candidateIds || candidateIds.has(indexer.id)) && (!excludedIdSet || !excludedIdSet.has(indexer.id)))
      .map(indexer => ({ id: indexer.id, name: indexer.name }))
  }, [
    crossSeedSearchResponse?.sourceTorrent?.availableIndexers,
    crossSeedSearchResponse?.sourceTorrent?.filteredIndexers,
    crossSeedSearchResponse?.sourceTorrent?.excludedIndexers,
    sortedEnabledIndexers,
  ])

  const crossSeedIndexerNameMap = useMemo(() => {
    const map: Record<number, string> = {}
    for (const indexer of sortedEnabledIndexers) {
      map[indexer.id] = indexer.name
    }
    return map
  }, [sortedEnabledIndexers])

  const excludedIndexerIds = useMemo(
    () =>
      Object.keys(crossSeedSearchResponse?.sourceTorrent?.excludedIndexers ?? {})
        .map(id => Number(id))
        .filter(id => !Number.isNaN(id)),
    [crossSeedSearchResponse?.sourceTorrent?.excludedIndexers]
  )

  const getCrossSeedResultKey = useCallback(
    (result: CrossSeedTorrentSearchResponse["results"][number], index: number) =>
      result.guid || result.downloadUrl || `${result.indexer}-${index}`,
    []
  )

  const resetCrossSeedState = useCallback((preserveError = false) => {
    setCrossSeedTorrent(null)
    setCrossSeedSearchResponse(null)
    if (!preserveError) {
      setCrossSeedSearchError(null)
    }
    setCrossSeedSearchLoading(false)
    setCrossSeedSelectedKeys(new Set())
    setCrossSeedUseTag(true)
    setCrossSeedTagName("cross-seed")
    setCrossSeedStartPaused(true)
    setCrossSeedSubmitting(false)
    setCrossSeedApplyResult(null)
    setCrossSeedIndexerMode("all")
    setCrossSeedIndexerSelection([])
    setCrossSeedHasSearched(false)
  }, [])

  const toggleCrossSeedIndexer = useCallback((indexerId: number) => {
    setCrossSeedIndexerSelection(prev => {
      if (prev.includes(indexerId)) {
        return prev.filter(id => id !== indexerId)
      }
      return [...prev, indexerId]
    })
  }, [])

  const selectAllCrossSeedIndexers = useCallback(() => {
    setCrossSeedIndexerSelection(crossSeedIndexerOptions.map(option => option.id))
  }, [crossSeedIndexerOptions])

  const clearCrossSeedIndexerSelection = useCallback(() => {
    setCrossSeedIndexerSelection([])
  }, [])

  const handleCrossSeedIndexerModeChange = useCallback((mode: "all" | "custom") => {
    setCrossSeedIndexerMode(mode)
    if (mode === "all") {
      setCrossSeedIndexerSelection([])
    }
  }, [])

  const runCrossSeedSearch = useCallback(
    (torrent: Torrent, indexerOverride?: number[] | null, options?: { bypassCache?: boolean }) => {
      if (!torrent) {
        return
      }

      let resolvedIndexerIds: number[] | null | undefined
      if (indexerOverride !== undefined) {
        resolvedIndexerIds = indexerOverride
      } else if (crossSeedIndexerMode === "custom") {
        resolvedIndexerIds = crossSeedIndexerSelection
      } else {
        resolvedIndexerIds = sortedEnabledIndexers.map(indexer => indexer.id)
      }

      if (Array.isArray(resolvedIndexerIds) && excludedIndexerIds.length > 0) {
        resolvedIndexerIds = resolvedIndexerIds.filter(id => !excludedIndexerIds.includes(id))
      }

      setCrossSeedSearchLoading(true)
      setCrossSeedSearchError(null)
      setCrossSeedSearchResponse(null)
      setCrossSeedApplyResult(null)
      setCrossSeedHasSearched(true)

      void api
        .searchCrossSeedTorrent(instanceId, torrent.hash, {
          findIndividualEpisodes: crossSeedSettings?.findIndividualEpisodes ?? false,
          indexerIds: Array.isArray(resolvedIndexerIds) && resolvedIndexerIds.length > 0 ? resolvedIndexerIds : undefined,
          cacheMode: options?.bypassCache ? "bypass" : undefined,
        })
        .then(response => {
          setCrossSeedSearchResponse(response)

          const defaultSelection = new Set<string>()
          response.results.forEach((result, index) => {
            defaultSelection.add(getCrossSeedResultKey(result, index))
          })
          setCrossSeedSelectedKeys(defaultSelection)

          if (response.results.length === 0) {
            toast.info("No cross-seed matches found")
          }
        })
        .catch((error: unknown) => {
          let message = error instanceof Error ? error.message : "Failed to search for cross-seeds"

          if (
            message.includes("429") ||
            message.includes("rate limit") ||
            message.includes("too many requests") ||
            message.includes("rate-limited") ||
            message.includes("cooldown")
          ) {
            message = `Rate limit active: ${message}. This protects against tracker bans. Some indexers are temporarily unavailable.`
          }

          setCrossSeedSearchError(message)
          toast.error(message, {
            duration: message.includes("rate limit") || message.includes("cooldown") ? 10000 : 5000,
          })
        })
        .finally(() => {
          setCrossSeedSearchLoading(false)
        })
    },
    [
      crossSeedIndexerMode,
      crossSeedIndexerSelection,
      crossSeedSettings?.findIndividualEpisodes,
      getCrossSeedResultKey,
      excludedIndexerIds,
      sortedEnabledIndexers,
      instanceId,
    ]
  )

  const handleCrossSeedForceRefresh = useCallback(() => {
    if (!crossSeedTorrent) {
      return
    }
    setCrossSeedRefreshCooldownUntil(Date.now() + CROSS_SEED_REFRESH_COOLDOWN_MS)
    runCrossSeedSearch(crossSeedTorrent, undefined, { bypassCache: true })
  }, [crossSeedTorrent, runCrossSeedSearch])

  const hasEnabledCrossSeedIndexers = sortedEnabledIndexers.length > 0

  const handleCrossSeedSearch = useCallback(
    (torrent: Torrent) => {
      if (!hasEnabledCrossSeedIndexers) {
        toast.error("Configure at least one Torznab indexer to search for cross-seeds")
        return
      }

      if (typeof torrent.progress === "number" && torrent.progress < 1) {
        toast.info("Only completed torrents can be cross-seeded")
        return
      }

      setCrossSeedTorrent(torrent)
      setCrossSeedDialogOpen(true)
      setCrossSeedSelectedKeys(new Set())
      setCrossSeedUseTag(true)
      setCrossSeedTagName("cross-seed")
      setCrossSeedIndexerMode("all")
      setCrossSeedIndexerSelection([])
      setCrossSeedSearchError(null)
      setCrossSeedSearchResponse(null)
      setCrossSeedHasSearched(false)

      void api
        .analyzeTorrentForCrossSeedSearch(instanceId, torrent.hash)
        .then(torrentInfo => {
          setCrossSeedSearchResponse({
            sourceTorrent: torrentInfo,
            results: [],
          })
        })
        .catch((error: unknown) => {
          let message = error instanceof Error ? error.message : "Failed to analyze torrent"

          if (message.includes("429") || message.includes("rate limit") || message.includes("too many requests")) {
            message = `Rate limit encountered during analysis: ${message}. Some indexers may be temporarily unavailable. This is normal and protects against being banned. Try again in 30-60 minutes.`
          }

          setCrossSeedSearchError(message)
          toast.error(message, {
            duration: message.includes("rate limit") ? 10000 : 5000,
          })
        })
    },
    [hasEnabledCrossSeedIndexers, instanceId]
  )

  const handleRetryCrossSeedSearch = useCallback(() => {
    if (crossSeedTorrent) {
      runCrossSeedSearch(crossSeedTorrent)
    }
  }, [crossSeedTorrent, runCrossSeedSearch])

  const handleCrossSeedScopeSearch = useCallback(() => {
    if (!crossSeedTorrent) {
      return
    }

    if (crossSeedIndexerMode === "custom" && crossSeedIndexerSelection.length === 0) {
      toast.warning("Select at least one tracker to run a custom search")
      return
    }

    runCrossSeedSearch(crossSeedTorrent)
  }, [crossSeedIndexerMode, crossSeedIndexerSelection.length, crossSeedTorrent, runCrossSeedSearch])

  const toggleCrossSeedSelection = useCallback(
    (result: CrossSeedTorrentSearchResponse["results"][number], index: number) => {
      setCrossSeedSelectedKeys(prev => {
        const next = new Set(prev)
        const key = getCrossSeedResultKey(result, index)
        if (next.has(key)) {
          next.delete(key)
        } else {
          next.add(key)
        }
        return next
      })
    },
    [getCrossSeedResultKey]
  )

  const selectAllCrossSeedResults = useCallback(() => {
    if (!crossSeedSearchResponse) {
      return
    }
    const next = new Set<string>()
    crossSeedSearchResponse.results.forEach((result, index) => {
      next.add(getCrossSeedResultKey(result, index))
    })
    setCrossSeedSelectedKeys(next)
  }, [crossSeedSearchResponse, getCrossSeedResultKey])

  const clearCrossSeedSelection = useCallback(() => {
    setCrossSeedSelectedKeys(new Set())
  }, [])

  const closeCrossSeedDialog = useCallback(() => {
    setCrossSeedDialogOpen(false)
    const shouldPreserveError = Boolean(
      crossSeedSearchError && (
        crossSeedSearchError.includes("429") ||
        crossSeedSearchError.includes("rate limit") ||
        crossSeedSearchError.includes("too many requests")
      )
    )
    resetCrossSeedState(shouldPreserveError)
  }, [crossSeedSearchError, resetCrossSeedState])

  const handleCrossSeedDialogOpenChange = useCallback(
    (open: boolean) => {
      if (!open) {
        closeCrossSeedDialog()
      } else {
        setCrossSeedDialogOpen(true)
      }
    },
    [closeCrossSeedDialog]
  )

  const handleApplyCrossSeed = useCallback(async () => {
    if (!crossSeedTorrent || !crossSeedSearchResponse) {
      return
    }

    const selections: CrossSeedTorrentSearchSelection[] = []
    crossSeedSearchResponse.results.forEach((result, index) => {
      const key = getCrossSeedResultKey(result, index)
      if (crossSeedSelectedKeys.has(key)) {
        selections.push({
          indexerId: result.indexerId,
          indexer: result.indexer,
          downloadUrl: result.downloadUrl,
          title: result.title,
          guid: result.guid,
        })
      }
    })

    if (selections.length === 0) {
      toast.warning("Select at least one result to add")
      return
    }

    try {
      setCrossSeedSubmitting(true)
      setCrossSeedApplyResult(null)

      const response = await api.applyCrossSeedSearchResults(instanceId, crossSeedTorrent.hash, {
        selections,
        useTag: crossSeedUseTag,
        tagName: crossSeedUseTag ? (crossSeedTagName.trim() || "cross-seed") : undefined,
        startPaused: crossSeedStartPaused,
        findIndividualEpisodes: crossSeedSettings?.findIndividualEpisodes ?? false,
      })

      setCrossSeedApplyResult(response)

      // Count successes and failures from instance results
      let addedCount = 0
      let failedCount = 0
      let completedWithoutDetails = 0
      for (const result of response.results) {
        const instanceResults = result.instanceResults ?? []
        if (instanceResults.length > 0) {
          for (const ir of instanceResults) {
            if (ir.status === "added" || ir.status === "added_hardlink" || ir.status === "added_reflink") {
              addedCount++
            } else if (!ir.success) {
              failedCount++
            }
          }
        } else if (result.success) {
          completedWithoutDetails++
        } else {
          failedCount++
        }
      }

      // Build toast message based on results
      const hasAdded = addedCount > 0
      const hasFailed = failedCount > 0
      const hasCompleted = completedWithoutDetails > 0
      const plural = addedCount > 1 ? "s" : ""

      if (hasAdded && !hasFailed) {
        const completedSuffix = hasCompleted ? ` (+${completedWithoutDetails} completed)` : ""
        toast.success(`Added ${addedCount} cross-seed${plural}${completedSuffix}`)
      } else if (hasAdded && hasFailed) {
        const completedPart = hasCompleted ? `, ${completedWithoutDetails} completed` : ""
        toast.warning(`Added ${addedCount}, ${failedCount} failed${completedPart} - check results for details`)
      } else if (hasFailed) {
        const completedPrefix = hasCompleted ? `${completedWithoutDetails} completed, ` : ""
        toast.error(`${completedPrefix}${failedCount} failed - check results for details`)
      } else if (hasCompleted) {
        toast.success("Cross-seed request completed (details unavailable)")
      } else {
        toast.info("No cross-seeds were added")
      }

      queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId], exact: false })
      queryClient.invalidateQueries({ queryKey: ["torrent-counts", instanceId], exact: false })
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to add cross-seeds"
      toast.error(message)
    } finally {
      setCrossSeedSubmitting(false)
    }
  }, [
    crossSeedSearchResponse,
    crossSeedSelectedKeys,
    crossSeedStartPaused,
    crossSeedTagName,
    crossSeedTorrent,
    crossSeedUseTag,
    crossSeedSettings?.findIndividualEpisodes,
    getCrossSeedResultKey,
    instanceId,
    queryClient,
  ])

  const crossSeedResults = useMemo(() => crossSeedSearchResponse?.results ?? [], [crossSeedSearchResponse?.results])
  const crossSeedSourceTorrent = crossSeedSearchResponse?.sourceTorrent
  const crossSeedSelectionCount = crossSeedSelectedKeys.size
  const crossSeedRefreshRemaining = Math.max(0, crossSeedRefreshCooldownUntil - Date.now())
  const crossSeedRefreshLabel = crossSeedRefreshRemaining > 0 ? `Ready in ${Math.ceil(crossSeedRefreshRemaining / 1000)}s` : undefined
  const canForceCrossSeedRefresh = !!crossSeedTorrent && !crossSeedSearchLoading && crossSeedRefreshRemaining <= 0

  useEffect(() => {
    const sourceTorrent = crossSeedSearchResponse?.sourceTorrent

    if (
      !sourceTorrent ||
      sourceTorrent.contentFilteringCompleted ||
      !crossSeedDialogOpen ||
      crossSeedPollingRef.current
    ) {
      return
    }

    const pollForUpdates = async () => {
      if (crossSeedPollingInFlightRef.current) {
        return
      }

      crossSeedPollingInFlightRef.current = true
      try {
        if (!sourceTorrent.hash) {
          if (crossSeedPollingRef.current) {
            clearInterval(crossSeedPollingRef.current)
            crossSeedPollingRef.current = null
          }
          return
        }

        const status = await api.getAsyncFilteringStatus(instanceId, sourceTorrent.hash)

        if (status.contentCompleted) {
          const updatedAnalysis = await api.analyzeTorrentForCrossSeedSearch(instanceId, sourceTorrent.hash)

          setCrossSeedSearchResponse(prev => (prev ? { ...prev, sourceTorrent: updatedAnalysis } : null))

          if (crossSeedPollingRef.current) {
            clearInterval(crossSeedPollingRef.current)
            crossSeedPollingRef.current = null
          }
        }
      } catch (error) {
        console.warn("Failed to poll async filtering status:", error)
        if (crossSeedPollingRef.current) {
          clearInterval(crossSeedPollingRef.current)
          crossSeedPollingRef.current = null
        }
      } finally {
        crossSeedPollingInFlightRef.current = false
      }
    }

    crossSeedPollingRef.current = setInterval(pollForUpdates, 500)

    return () => {
      if (crossSeedPollingRef.current) {
        clearInterval(crossSeedPollingRef.current)
        crossSeedPollingRef.current = null
      }
      crossSeedPollingInFlightRef.current = false
    }
  }, [crossSeedDialogOpen, crossSeedSearchResponse, instanceId])

  useEffect(() => {
    if (!crossSeedDialogOpen && crossSeedPollingRef.current) {
      clearInterval(crossSeedPollingRef.current)
      crossSeedPollingRef.current = null
      crossSeedPollingInFlightRef.current = false
    }
  }, [crossSeedDialogOpen])

  const crossSeedDialog = (
    <CrossSeedDialog
      open={crossSeedDialogOpen}
      onOpenChange={handleCrossSeedDialogOpenChange}
      torrent={crossSeedTorrent}
      sourceTorrent={crossSeedSourceTorrent}
      results={crossSeedResults}
      selectedKeys={crossSeedSelectedKeys}
      selectionCount={crossSeedSelectionCount}
      isLoading={crossSeedSearchLoading}
      isSubmitting={crossSeedSubmitting}
      error={crossSeedSearchError}
      applyResult={crossSeedApplyResult}
      indexerOptions={crossSeedIndexerOptions}
      indexerMode={crossSeedIndexerMode}
      selectedIndexerIds={crossSeedIndexerSelection}
      indexerNameMap={crossSeedIndexerNameMap}
      onIndexerModeChange={handleCrossSeedIndexerModeChange}
      onToggleIndexer={toggleCrossSeedIndexer}
      onSelectAllIndexers={selectAllCrossSeedIndexers}
      onClearIndexerSelection={clearCrossSeedIndexerSelection}
      onScopeSearch={handleCrossSeedScopeSearch}
      getResultKey={getCrossSeedResultKey}
      onToggleSelection={toggleCrossSeedSelection}
      onSelectAll={selectAllCrossSeedResults}
      onClearSelection={clearCrossSeedSelection}
      onRetry={handleRetryCrossSeedSearch}
      onClose={closeCrossSeedDialog}
      onApply={handleApplyCrossSeed}
      useTag={crossSeedUseTag}
      onUseTagChange={setCrossSeedUseTag}
      tagName={crossSeedTagName}
      onTagNameChange={setCrossSeedTagName}
      startPaused={crossSeedStartPaused}
      onStartPausedChange={setCrossSeedStartPaused}
      hasSearched={crossSeedHasSearched}
      cacheMetadata={crossSeedSearchResponse?.cache ?? null}
      onForceRefresh={canForceCrossSeedRefresh ? handleCrossSeedForceRefresh : undefined}
      canForceRefresh={canForceCrossSeedRefresh}
      refreshCooldownLabel={crossSeedRefreshLabel}
    />
  )

  return {
    canCrossSeedSearch: hasEnabledCrossSeedIndexers,
    isCrossSeedSearching: crossSeedSearchLoading || crossSeedSubmitting,
    openCrossSeedSearch: handleCrossSeedSearch,
    crossSeedDialog,
  }
}
