/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { refreshChangedCapabilities, useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { applyStreamDelta, mergeStreamedCrossInstanceFirstPage, normalizeStreamedSnapshot } from "@/lib/cross-instance-torrents"
import { isAllInstancesScope } from "@/lib/instances"
import { mergeStreamedFirstPage } from "@/lib/stream-merge"
import { isCrossSeedExpr } from "@/lib/torrent-filters"
import type {
  Category,
  CrossInstanceTorrent,
  QBittorrentAppInfo,
  Torrent,
  TorrentCounts,
  TorrentFilters,
  TorrentResponse,
  TorrentStreamPayload
} from "@/types"
import { hashKey, replaceEqualDeep, useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

export const TORRENT_STREAM_POLL_INTERVAL_MS = 3000
export const TORRENT_STREAM_POLL_INTERVAL_SECONDS = Math.max(
  1,
  Math.round(TORRENT_STREAM_POLL_INTERVAL_MS / 1000)
)
export const LOADED_WINDOW_POLL_INTERVAL_MS = 10000

export const STREAM_HIDDEN_PAUSE_DELAY_MS = 30000

const NO_CATEGORIES: Record<string, Category> = {}
const NO_TAGS: string[] = []

function dedupeRows<T>(rows: T[], keyOf: (row: T) => string): T[] {
  return [...new Map(rows.map(row => [keyOf(row), row] as const)).values()]
}

const responseRevisions = new WeakMap<TorrentResponse, object | null>()

function shareResponse(oldData: unknown, newData: unknown): TorrentResponse {
  const previous = oldData as TorrentResponse | undefined
  const incoming = newData as TorrentResponse
  const revision = responseRevisions.get(incoming)
  let shared = replaceEqualDeep(previous, incoming)
  if (shared === previous && revision !== (previous && responseRevisions.get(previous))) {
    shared = { ...shared }
  }
  if (revision === undefined) {
    responseRevisions.delete(shared)
  } else {
    responseRevisions.set(shared, revision)
  }
  return shared
}

function combineWindowPages(
  pages: TorrentResponse[]
): TorrentResponse {
  const last = pages[pages.length - 1]

  if (last.isCrossInstance) {
    return {
      ...last,
      windowPageCount: pages.length,
      crossInstanceTorrents: dedupeRows(
        pages.flatMap(page => page.crossInstanceTorrents ?? page.cross_instance_torrents ?? []),
        row => `${row.instanceId}:${row.hash}`
      ),
    }
  }

  return {
    ...last,
    windowPageCount: pages.length,
    torrents: dedupeRows(pages.flatMap(page => page.torrents ?? []), row => row.hash),
  }
}

function mergeStreamedRows(rows: Torrent[], snapshot: TorrentResponse, crossInstance: true): CrossInstanceTorrent[]
function mergeStreamedRows(rows: Torrent[], snapshot: TorrentResponse, crossInstance: boolean): Torrent[]
function mergeStreamedRows(rows: Torrent[], snapshot: TorrentResponse, crossInstance: boolean): Torrent[] {
  return crossInstance
    ? mergeStreamedCrossInstanceFirstPage(rows, snapshot)
    : mergeStreamedFirstPage(rows, snapshot.torrents ?? [], snapshot.total)
}

function overlayStreamedFirstPage(
  windowResponse: TorrentResponse,
  streamSnapshot: TorrentResponse,
  crossInstance: boolean
): TorrentResponse {
  const windowFields = {
    hasMore: windowResponse.hasMore,
    windowPageCount: windowResponse.windowPageCount,
  }

  if (crossInstance) {
    const rows = mergeStreamedRows(
      windowResponse.crossInstanceTorrents ?? windowResponse.cross_instance_torrents ?? [],
      streamSnapshot,
      true
    )
    return {
      ...windowResponse,
      ...streamSnapshot,
      ...windowFields,
      crossInstanceTorrents: rows,
      cross_instance_torrents: rows,
    }
  }

  return {
    ...windowResponse,
    ...streamSnapshot,
    ...windowFields,
    torrents: mergeStreamedRows(
      windowResponse.torrents ?? [],
      streamSnapshot,
      false
    ),
  }
}

interface UseTorrentsListOptions {
  enabled?: boolean
  pollingEnabled?: boolean
  refetchIntervalInBackground?: boolean
  search?: string
  filters?: TorrentFilters
  sort?: string
  order?: "asc" | "desc"
  instanceIds?: number[]
  groupCopies?: boolean
}

export function useTorrentsList(
  instanceId: number,
  options: UseTorrentsListOptions = {}
) {
  const {
    enabled = true,
    pollingEnabled = true,
    refetchIntervalInBackground = false,
    search,
    filters,
    sort = "added_on",
    order = "desc",
    instanceIds,
    groupCopies = false,
  } = options
  const isAllInstancesView = isAllInstancesScope(instanceId)

  const [currentPage, setCurrentPage] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const lastRequestTimeRef = useRef(0)
  const [lastKnownTotal, setLastKnownTotal] = useState(0)
  const [lastProcessedPage, setLastProcessedPage] = useState(-1)
  const [lastStreamSnapshot, setLastStreamSnapshot] = useState<TorrentResponse | null>(null)
  const [lastCountsSnapshot, setLastCountsSnapshot] = useState<{ scopeKey: string; counts?: TorrentCounts }>({ scopeKey: "" })
  const committedCountsSnapshotRef = useRef(lastCountsSnapshot)
  const lastStreamSnapshotScopeRef = useRef("")
  const lastFullSnapshotRef = useRef<TorrentResponse | null>(null)
  const streamRevisionRef = useRef<object | null>(null)
  const lastAppliedDataRef = useRef<TorrentResponse | null>(null)
  const pageSize = 300
  const queryClient = useQueryClient()

  useEffect(() => {
    committedCountsSnapshotRef.current = lastCountsSnapshot
  }, [lastCountsSnapshot])

  const { isHiddenDelayed } = useDelayedVisibility(STREAM_HIDDEN_PAUSE_DELAY_MS)

  const metadataQueryKey = useMemo(
    () => ["instance-metadata", instanceId] as const,
    [instanceId]
  )

  const appInfoQueryKey = useMemo(
    () => ["qbittorrent-app-info", instanceId] as const,
    [instanceId]
  )

  const updateMetadataCache = useCallback(
    (source?: TorrentResponse | null) => {
      if (!source || source.isCrossInstance === true) {
        return
      }

      queryClient.setQueryData<InstanceMetadata>(metadataQueryKey, {
        categories: source.categories ?? {},
        tags: source.tags ?? [],
      })
    },
    [metadataQueryKey, queryClient]
  )

  const updateAppInfoCache = useCallback(
    (source?: Pick<TorrentResponse, "appInfo"> | null) => {
      if (!source?.appInfo) {
        return
      }

      queryClient.setQueryData<QBittorrentAppInfo | undefined>(appInfoQueryKey, source.appInfo)
    },
    [appInfoQueryKey, queryClient]
  )

  const rememberCountsSnapshot = useCallback((
    scopeKey: string,
    counts: TorrentCounts | undefined,
    fallbackSnapshot?: { scopeKey: string; counts?: TorrentCounts }
  ) => {
    const nextCounts = counts ?? (
      fallbackSnapshot?.scopeKey === scopeKey ? fallbackSnapshot.counts : undefined
    )
    if (nextCounts === undefined) {
      return
    }

    setLastCountsSnapshot(previous =>
      previous.scopeKey === scopeKey && previous.counts === nextCounts? previous: { scopeKey, counts: nextCounts }
    )
  }, [])

  const isCrossSeedFiltering = isCrossSeedExpr(filters?.expr)
  const useCrossInstanceEndpoint = isAllInstancesView || isCrossSeedFiltering
  const requestGroups = groupCopies && !isCrossSeedFiltering

  const instanceIdsKey = useMemo(
    () => (instanceIds && instanceIds.length > 0 ? [...instanceIds].sort((left, right) => left - right).join(",") : ""),
    [instanceIds]
  )
  const filterKey = JSON.stringify(filters)
  const searchKey = search || ""
  const viewScopeKey = `${instanceId}|${instanceIdsKey}|${filterKey}|${searchKey}|${sort}|${order}|${useCrossInstanceEndpoint}|${isCrossSeedFiltering}|${requestGroups}`
  const currentViewScopeKeyRef = useRef(viewScopeKey)
  useEffect(() => {
    currentViewScopeKeyRef.current = viewScopeKey
  }, [viewScopeKey])

  const streamQueryKey = useMemo(
    () => ["torrents-list", instanceId, instanceIdsKey, 0, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering, requestGroups] as const,
    [instanceId, instanceIdsKey, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering, requestGroups]
  )

  const { instances } = useInstances()
  const activeInstanceIds = useMemo(
    () => (instances ?? []).filter(current => current.isActive).map(current => current.id).filter(id => id > 0),
    [instances]
  )

  const streamInstanceIds = useMemo(() => {
    if (!useCrossInstanceEndpoint) {
      return undefined
    }
    const base = instanceIds && instanceIds.length > 0 ? instanceIds : activeInstanceIds
    const filtered = Array.from(new Set(base.filter(id => id > 0)))
    return filtered.length > 0 ? filtered : undefined
  }, [useCrossInstanceEndpoint, instanceIds, activeInstanceIds])

  const streamParams = useMemo(() => {
    if (!enabled) {
      return null
    }

    if (useCrossInstanceEndpoint) {
      if (isCrossSeedFiltering) {
        return null
      }
      if (!streamInstanceIds || streamInstanceIds.length === 0) {
        return null
      }
      return {
        instanceId: 0,
        instanceIds: streamInstanceIds,
        page: 0,
        limit: pageSize,
        sort,
        order,
        search: search || undefined,
        filters,
        groupCopies: requestGroups,
      }
    }

    return {
      instanceId,
      page: 0,
      limit: pageSize,
      sort,
      order,
      search: search || undefined,
      filters,
      groupCopies: requestGroups,
    }
  }, [enabled, filters, instanceId, useCrossInstanceEndpoint, isCrossSeedFiltering, streamInstanceIds, order, pageSize, search, sort, requestGroups])

  const listQueryKey = useMemo(
    () => ["torrents-list", instanceId, instanceIdsKey, currentPage, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering, requestGroups] as const,
    [instanceId, instanceIdsKey, currentPage, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering, requestGroups]
  )

  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data) {
        return
      }

      if (viewScopeKey !== currentViewScopeKeyRef.current) {
        return
      }

      let data: TorrentResponse
      let rowsChanged = true
      if (payload.type === "delta") {
        const base = lastFullSnapshotRef.current
        if (!base) {
          return
        }
        const applied = applyStreamDelta(base, payload, Boolean(useCrossInstanceEndpoint))
        data = applied.data
        rowsChanged = applied.changed
      } else {
        data = normalizeStreamedSnapshot(payload.data)
      }

      streamRevisionRef.current = {}
      void queryClient.cancelQueries({ queryKey: streamQueryKey, exact: true })

      data = queryClient.setQueryData<TorrentResponse>(streamQueryKey, data) ?? data

      lastFullSnapshotRef.current = data
      lastStreamSnapshotScopeRef.current = viewScopeKey

      setLastStreamSnapshot(data)
      rememberCountsSnapshot(viewScopeKey, data.counts, committedCountsSnapshotRef.current)
      updateAppInfoCache(data)
      updateMetadataCache(data)
      refreshChangedCapabilities(queryClient, data)

      if (rowsChanged) {
        setAllTorrents(prev => mergeStreamedRows(prev, data, Boolean(useCrossInstanceEndpoint)))
      }

      if (typeof data.total === "number") {
        setLastKnownTotal(data.total)
      }

      if (currentPage === 0 && typeof data.hasMore === "boolean") {
        setHasLoadedAll(!data.hasMore)
      }
    },
    [currentPage, queryClient, rememberCountsSnapshot, streamQueryKey, updateAppInfoCache, updateMetadataCache, useCrossInstanceEndpoint, viewScopeKey]
  )

  const streamState = useSyncStream(streamParams, {
    enabled: Boolean(streamParams) && !isHiddenDelayed,
    onMessage: handleStreamPayload,
  })

  const hasBaselineForCurrentView =
    lastStreamSnapshotScopeRef.current === viewScopeKey &&
    lastFullSnapshotRef.current !== null
  const shouldDisablePolling =
    Boolean(streamParams) &&
    isStreamUsable(streamState) &&
    hasBaselineForCurrentView
  const preferCachedQuery = shouldDisablePolling
  const queryEnabled = enabled && (currentPage > 0 || !shouldDisablePolling)

  useEffect(() => {
    setCurrentPage(0)
    lastRequestTimeRef.current = 0
    setAllTorrents([])
    setHasLoadedAll(false)
    setLastKnownTotal(0)
    setLastProcessedPage(-1)
    setLastStreamSnapshot(null)
    lastStreamSnapshotScopeRef.current = ""
    lastFullSnapshotRef.current = null
    lastAppliedDataRef.current = null
  }, [viewScopeKey])

  useEffect(() => {
    if (lastKnownTotal <= 0) {
      return
    }

    setHasLoadedAll(previous => {
      const next = allTorrents.length >= lastKnownTotal
      return previous === next ? previous : next
    })
  }, [allTorrents.length, lastKnownTotal])

  const getNewerStreamSnapshot = useCallback((response?: TorrentResponse) => {
    if (!response || lastStreamSnapshotScopeRef.current !== viewScopeKey || streamRevisionRef.current === null ||
      !responseRevisions.has(response) || responseRevisions.get(response) === streamRevisionRef.current) {
      return null
    }
    return lastFullSnapshotRef.current
  }, [viewScopeKey])

  const { data, isLoading, isFetching, isPlaceholderData, isError, refetch } = useQuery<TorrentResponse>({
    queryKey: listQueryKey,
    queryFn: async ({ signal }) => {
      const revisionAtStart = streamRevisionRef.current
      const fetchPage = (page: number): Promise<TorrentResponse> => {
        if (useCrossInstanceEndpoint) {
          return api.getCrossInstanceTorrents({
            page,
            limit: pageSize,
            sort,
            order,
            search,
            filters,
            instanceIds,
            groupCopies: requestGroups,
          }, signal)
        }

        return api.getTorrents(instanceId, {
          page,
          limit: pageSize,
          sort,
          order,
          search,
          filters,
          preferCached: preferCachedQuery,
          groupCopies: requestGroups,
        }, signal)
      }

      if (currentPage === 0 || queryClient.getQueryData(listQueryKey) === undefined) {
        const page = await fetchPage(currentPage)
        const response = { ...page }
        responseRevisions.set(response, revisionAtStart)
        return response
      }

      const fetchWindow = () => Promise.all(Array.from({ length: currentPage + 1 }, (_, page) => fetchPage(page)))
      let pages = await fetchWindow()
      if (pages.some(page => page.copyGroupsReady !== pages[0].copyGroupsReady)) {
        pages = await fetchWindow()
      }
      const combined = combineWindowPages(pages)
      responseRevisions.set(combined, revisionAtStart)
      const snapshot = getNewerStreamSnapshot(combined)
      const response = snapshot
        ? overlayStreamedFirstPage(combined, snapshot, Boolean(useCrossInstanceEndpoint))
        : combined
      responseRevisions.set(response, revisionAtStart)
      return response
    },
    structuralSharing: shareResponse,
    staleTime: currentPage > 0 ? LOADED_WINDOW_POLL_INTERVAL_MS : 0,
    gcTime: 300000,
    placeholderData: currentPage > 0
      ? (previousData, previousQuery) =>
        previousQuery && hashKey(previousQuery.queryKey.map((part, index) => index === 3 ? 0 : part)) === hashKey(streamQueryKey)
          ? previousData
          : undefined
      : undefined,
    refetchInterval: !pollingEnabled
      ? false
      : shouldDisablePolling
        ? (currentPage > 0 ? LOADED_WINDOW_POLL_INTERVAL_MS : false)
        : streamFallbackPollInterval(
          streamState,
          useCrossInstanceEndpoint ? LOADED_WINDOW_POLL_INTERVAL_MS : TORRENT_STREAM_POLL_INTERVAL_MS
        ),
    refetchIntervalInBackground,
    refetchOnWindowFocus: pollingEnabled,
    enabled: queryEnabled,
  })

  const isFetchingNewPage = currentPage !== lastProcessedPage && isFetching
  const isLoadingMore = currentPage > 0 && isFetchingNewPage

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: enabled && !isAllInstancesView })

  const scopedStreamSnapshot = lastStreamSnapshotScopeRef.current === viewScopeKey ? lastStreamSnapshot : null
  const activeData = scopedStreamSnapshot && (shouldDisablePolling || getNewerStreamSnapshot(data))
    ? scopedStreamSnapshot
    : data ?? scopedStreamSnapshot

  const copyGroupsReady = activeData?.copyGroupsReady
  const lastCopyGroupsReadyRef = useRef<{ scopeKey: string; ready: boolean } | null>(null)
  const windowRepairPendingRef = useRef(false)
  const readyFromWindow = activeData === data && Boolean(data?.windowPageCount)
  const pageLoaded = data !== undefined && !isPlaceholderData
  useEffect(() => {
    if (copyGroupsReady !== undefined) {
      const previous = lastCopyGroupsReadyRef.current
      lastCopyGroupsReadyRef.current = { scopeKey: viewScopeKey, ready: copyGroupsReady }
      if (previous?.scopeKey === viewScopeKey && previous.ready !== copyGroupsReady) {
        windowRepairPendingRef.current = true
      }
    }
    if (currentPage === 0 || readyFromWindow) {
      windowRepairPendingRef.current = false
    } else if (windowRepairPendingRef.current && pageLoaded) {
      windowRepairPendingRef.current = false
      void refetch()
    }
  }, [copyGroupsReady, currentPage, pageLoaded, readyFromWindow, refetch, viewScopeKey])

  useEffect(() => {
    if (isPlaceholderData && (lastProcessedPage === -1 || currentPage === 0)) {
      return
    }

    if (currentPage > 0 && isFetching && isPlaceholderData) {
      return
    }

    if (!data) {
      return
    }

    if (data === lastAppliedDataRef.current) {
      return
    }
    lastAppliedDataRef.current = data

    const newerStreamSnapshot = getNewerStreamSnapshot(data)
    const newerStreamSnapshotExists = newerStreamSnapshot !== null

    if (!newerStreamSnapshotExists) {
      updateAppInfoCache(data)
      updateMetadataCache(data)
      refreshChangedCapabilities(queryClient, data)
      rememberCountsSnapshot(viewScopeKey, data.counts, committedCountsSnapshotRef.current)

      if (data.total !== undefined) {
        setLastKnownTotal(data.total)
      }
    }

    if (currentPage === 0 && !newerStreamSnapshotExists && data.total === 0) {
      setAllTorrents([])
      setHasLoadedAll(true)
      setLastProcessedPage(currentPage)
      return
    }

    const torrentsData = data.isCrossInstance? (data.crossInstanceTorrents || data.cross_instance_torrents): data.torrents

    if (!torrentsData) {
      return
    }

    if (currentPage === 0) {
      if (newerStreamSnapshotExists) {
        return
      }
      setAllTorrents(torrentsData)
      setHasLoadedAll(!data.hasMore)
      setLastProcessedPage(currentPage)
    } else if (currentPage !== lastProcessedPage) {
      const streamSnapshot = newerStreamSnapshot
      setAllTorrents(prev => {
        const rows = dedupeRows(
          [...prev, ...torrentsData],
          data.isCrossInstance? row => `${(row as CrossInstanceTorrent).instanceId}:${row.hash}`: row => row.hash
        )
        if (!streamSnapshot) {
          return rows
        }
        return mergeStreamedRows(rows, streamSnapshot, Boolean(data.isCrossInstance))
      })
      if (!newerStreamSnapshotExists && !data.hasMore) {
        setHasLoadedAll(true)
      }
      setLastProcessedPage(currentPage)
    } else if (data.windowPageCount) {
      const streamSnapshot = newerStreamSnapshot
      setAllTorrents(streamSnapshot
        ? mergeStreamedRows(torrentsData, streamSnapshot, Boolean(data.isCrossInstance))
        : torrentsData)
      if (!newerStreamSnapshotExists) {
        setHasLoadedAll(!data.hasMore)
      }
    }

  }, [data, currentPage, lastProcessedPage, isFetching, isPlaceholderData, getNewerStreamSnapshot, queryClient, rememberCountsSnapshot, updateAppInfoCache, updateMetadataCache, viewScopeKey])

  const loadMore = () => {
    const now = Date.now()

    if (hasLoadedAll || isFetchingNewPage) {
      return
    }

    if (now - lastRequestTimeRef.current < 500) {
      return
    }

    lastRequestTimeRef.current = now
    if (isError && currentPage !== lastProcessedPage) {
      void refetch()
      return
    }
    setCurrentPage(prev => prev + 1)
  }

  const stats = useMemo(() => {
    const source = activeData ?? data

    if (source?.stats) {
      return {
        total: source.total || source.stats.total || 0,
        downloading: source.stats.downloading || 0,
        seeding: source.stats.seeding || 0,
        paused: source.stats.paused || 0,
        error: source.stats.error || 0,
        totalDownloadSpeed: source.stats.totalDownloadSpeed || 0,
        totalUploadSpeed: source.stats.totalUploadSpeed || 0,
        totalDownloadData: source.stats.totalDownloadData || 0,
        totalUploadData: source.stats.totalUploadData || 0,
        totalSize: source.stats.totalSize || 0,
      }
    }

    return {
      total: source?.total || 0,
      downloading: 0,
      seeding: 0,
      paused: 0,
      error: 0,
      totalDownloadSpeed: 0,
      totalUploadSpeed: 0,
      totalDownloadData: 0,
      totalUploadData: 0,
      totalSize: source?.stats?.totalSize || 0,
    }
  }, [activeData, data])

  const cacheMetadata = activeData?.cacheMetadata ?? data?.cacheMetadata
  const isCachedData = cacheMetadata?.source === "cache"
  const isStaleData = cacheMetadata?.isStale === true

  const isInitialStreamLoading =
    currentPage === 0 &&
    enabled &&
    Boolean(streamParams) &&
    !streamState.error &&
    !lastStreamSnapshot &&
    !data

  const effectiveIsLoading =
    currentPage === 0 ? (isInitialStreamLoading || (queryEnabled && isLoading)) : isLoading

  const effectiveIsFetching =
    currentPage === 0 ? (queryEnabled && isFetching) : isFetching

  const effectiveTotalCount =
    currentPage > 0 ? lastKnownTotal : activeData?.total ?? lastKnownTotal

  const responseUseSubcategories = activeData?.useSubcategories ?? activeData?.serverState?.use_subcategories ?? data?.useSubcategories ?? data?.serverState?.use_subcategories ?? false
  const supportsSubcategories = isAllInstancesView ? responseUseSubcategories : (capabilities?.supportsSubcategories ?? false)
  const countsScopeKey = viewScopeKey
  const responseCounts = activeData?.counts ?? data?.counts

  const effectiveCounts = responseCounts ?? (
    lastCountsSnapshot.scopeKey === countsScopeKey ? lastCountsSnapshot.counts : undefined
  )

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: effectiveCounts,
    appInfo: activeData?.appInfo ?? data?.appInfo ?? null,
    categories: activeData ? activeData.categories ?? NO_CATEGORIES : undefined,
    tags: activeData ? activeData.tags ?? NO_TAGS : undefined,
    trackerHealthSupported: activeData?.trackerHealthSupported ?? data?.trackerHealthSupported ?? false,
    supportsTorrentCreation: isAllInstancesView ? false : capabilities?.supportsTorrentCreation ?? true,
    capabilities: isAllInstancesView ? undefined : capabilities,
    serverState: activeData?.serverState ?? data?.serverState ?? null,
    useSubcategories: isAllInstancesView? responseUseSubcategories: (supportsSubcategories ? responseUseSubcategories : false),
    isLoading: effectiveIsLoading,
    isFetching: effectiveIsFetching,
    isLoadingMore,
    hasLoadedAll,
    loadMore,
    isCrossInstance: data?.isCrossInstance ?? useCrossInstanceEndpoint,
    isCrossSeedFiltering,
    isAllInstancesView,
    isCrossInstanceEndpoint: useCrossInstanceEndpoint,
    isFreshData: !isCachedData || !isStaleData,
    isCachedData,
    isStaleData,
    cacheAge: cacheMetadata?.age,
    isStreaming: shouldDisablePolling,
    streamConnected: isStreamUsable(streamState),
    streamError: streamState.error,
    streamMeta: streamState.lastMeta,
    streamRetrying: streamState.retrying,
    streamNextRetryAt: streamState.nextRetryAt,
    streamRetryAttempt: streamState.retryAttempt,
    copyGroupsReady,
  }
}
