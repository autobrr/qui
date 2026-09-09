/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useSyncStream } from "@/contexts/SyncStreamContext"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import type { InstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import { applyStreamDelta, mergeStreamedCrossInstanceFirstPage, normalizeStreamedSnapshot } from "@/lib/cross-instance-torrents"
import { isAllInstancesScope } from "@/lib/instances"
import { mergeStreamedFirstPage } from "@/lib/stream-merge"
import type {
  AppPreferences,
  CrossInstanceTorrent,
  QBittorrentAppInfo,
  Torrent,
  TorrentCounts,
  TorrentFilters,
  TorrentResponse,
  TorrentStreamPayload
} from "@/types"
import { useQuery, useQueryClient } from "@tanstack/react-query"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"

/** Fallback REST polling interval used while the torrent SSE stream is unavailable. */
export const TORRENT_STREAM_POLL_INTERVAL_MS = 3000
/** Fallback REST polling interval in whole seconds for stream request metadata. */
export const TORRENT_STREAM_POLL_INTERVAL_SECONDS = Math.max(
  1,
  Math.round(TORRENT_STREAM_POLL_INTERVAL_MS / 1000)
)

// While the tab is hidden the table is invisible, so streaming a full page-0
// snapshot (up to 300 torrents) every couple of seconds is pure waste: the work is
// deferred and then burst-processed by a throttled background tab, and each frame is
// retained by anyone running DevTools with "Persist Logs" on. Pause the heavy list
// subscription once the tab has been hidden this long; the title-bar speed stream
// (limit:1) stays live so background transfer rates keep updating. The grace delay
// avoids tearing the stream down on quick tab switches; on refocus it resumes at once
// and refetchOnWindowFocus pulls fresh data immediately.
export const STREAM_HIDDEN_PAUSE_DELAY_MS = 30000

// Self-resolving qBittorrent states with continuously advancing progress. A
// recheck or move holds a row in one of these for minutes; rows past the
// stream window get no live updates, so the window query keeps polling while
// any loaded row is in one of these states.
const TRANSIENT_TORRENT_STATES = new Set([
  "checkingDL",
  "checkingUP",
  "checkingResumeData",
  "allocating",
  "moving",
])

// Drop duplicate rows, keeping the first occurrence's position with the last
// occurrence's data (Map insertion order is first-set, values are last-set): in
// append mode the later page is the fresher fetch, so the row stays put but
// carries the newest snapshot. Pages are fetched (or appended) against a live
// cache, so a row can reflow across a page boundary between requests and show up
// twice; identity matches the stream merge (hash, or instanceId+hash for
// cross-instance rows).
function dedupeRows<T>(rows: T[], keyOf: (row: T) => string): T[] {
  return [...new Map(rows.map(row => [keyOf(row), row] as const)).values()]
}

// Combine the per-page responses of a loaded window (pages 0..N of one view) into a
// single response shaped like one oversized page. The last page is the base so hasMore
// reflects whether rows remain past the window; list metadata (counts, categories,
// tags, serverState, preferences) is computed from the full filtered set on the
// backend regardless of offset, so the base carries it for the whole window.
// windowPageCount marks the response as window-shaped so the processing effect knows
// it may replace the whole list with it.
function combineWindowPages(pages: TorrentResponse[]): TorrentResponse {
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

interface UseTorrentsListOptions {
  enabled?: boolean
  pollingEnabled?: boolean
  refetchIntervalInBackground?: boolean
  search?: string
  filters?: TorrentFilters
  sort?: string
  order?: "asc" | "desc"
  instanceIds?: number[]
}

/**
 * Loads a paginated torrent list and keeps page 0 current with SSE when possible.
 * The hook updates shared instance metadata from stream/REST responses, preserves
 * cached preferences and sidebar counts when stream frames omit them, clears
 * preferences on explicit null responses, and falls back to REST polling when
 * streaming is disabled or disconnected. Stream frames are scoped to the last
 * committed view identity so frames from a previous filter/search/sort cannot
 * repopulate rows after a reset, while abandoned renders cannot suppress the
 * active stream callback.
 */
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
  } = options
  const isAllInstancesView = isAllInstancesScope(instanceId)

  const [currentPage, setCurrentPage] = useState(0)
  const [allTorrents, setAllTorrents] = useState<Torrent[]>([])
  const [hasLoadedAll, setHasLoadedAll] = useState(false)
  const [isLoadingMore, setIsLoadingMore] = useState(false)
  const [lastRequestTime, setLastRequestTime] = useState(0)
  const [lastKnownTotal, setLastKnownTotal] = useState(0)
  const [lastProcessedPage, setLastProcessedPage] = useState(-1)
  const [lastStreamSnapshot, setLastStreamSnapshot] = useState<TorrentResponse | null>(null)
  // Last committed counts for the current view identity. Stream snapshots may
  // omit counts while tracker hydration catches up, so render-time counts can
  // fall back to this same-scope snapshot without deriving state in an effect.
  const [lastCountsSnapshot, setLastCountsSnapshot] = useState<{ scopeKey: string; counts?: TorrentCounts }>({ scopeKey: "" })
  // Mirrors the committed counts snapshot for stream callbacks. A callback from
  // a render that later suspends must not publish uncommitted counts as fallback.
  const committedCountsSnapshotRef = useRef(lastCountsSnapshot)
  const lastStreamSnapshotScopeRef = useRef("")
  // The last full page-0 snapshot, retained as the base that incoming SSE deltas are
  // applied against. Held in a ref (not state) so a delta can read the just-applied
  // snapshot synchronously without re-running this callback. Seeded by every full
  // frame (init/update/keyframe) and reset when the view identity changes.
  const lastFullSnapshotRef = useRef<TorrentResponse | null>(null)
  // The last REST/query-cache data object already applied to the list. The
  // processing effect re-runs when its own state writes (lastProcessedPage) change;
  // re-applying the same data would re-assert hasLoadedAll from hasMore and override
  // the length>=total fallback effect below, so identical data is applied only once.
  const lastAppliedDataRef = useRef<TorrentResponse | null>(null)
  const pageSize = 300 // Load 300 at a time (backend default)
  const queryClient = useQueryClient()

  useEffect(() => {
    committedCountsSnapshotRef.current = lastCountsSnapshot
  }, [lastCountsSnapshot])

  // Pause the heavy list stream while the tab is backgrounded (see
  // STREAM_HIDDEN_PAUSE_DELAY_MS). isHiddenDelayed only trips after a grace period,
  // so quick tab switches don't churn the subscription.
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
      if (!source) {
        return
      }

      const hasPreferences = Object.prototype.hasOwnProperty.call(source, "preferences")
      const isCrossInstanceSource = source.isCrossInstance === true

      if (isCrossInstanceSource && !hasPreferences) {
        return
      }

      queryClient.setQueryData<InstanceMetadata | undefined>(
        metadataQueryKey,
        previous => {
          // Treat omitted tags/categories as empty for regular instance responses:
          // backend omitempty drops empty collections, and those should clear stale
          // sidebar values. Omitted preferences are different because stale stream
          // frames may not carry them; only an explicit preferences property updates
          // or clears preference caches.
          const nextCategories = isCrossInstanceSource? (previous?.categories ?? {}): (source.categories ?? {})
          const nextTags = isCrossInstanceSource? (previous?.tags ?? []): (source.tags ?? [])
          const nextPreferences =
            hasPreferences? ((source.preferences as AppPreferences | null | undefined) ?? undefined): previous?.preferences

          const next: InstanceMetadata = {
            categories: nextCategories,
            tags: nextTags,
            preferences: nextPreferences,
          }

          return next
        }
      )

      if (hasPreferences) {
        const nextPreferences = (source.preferences as AppPreferences | null | undefined) ?? undefined
        if (nextPreferences !== undefined) {
          queryClient.setQueryData<AppPreferences | undefined>(
            ["instance-preferences", instanceId],
            nextPreferences
          )
        } else if (!isCrossInstanceSource) {
          queryClient.removeQueries({ queryKey: ["instance-preferences", instanceId], exact: true })
        }
      }
    },
    [instanceId, metadataQueryKey, queryClient]
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

  // Records counts only from committed data write paths. If a stream frame omits
  // counts, reuse the last committed same-scope snapshot rather than deriving
  // fallback state from the current render.
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

  // Detect if this is cross-seed filtering based on expression content
  const isCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes("Hash ==") && filters?.expr?.includes("||")
  }, [filters?.expr])
  const useCrossInstanceEndpoint = isAllInstancesView || isCrossSeedFiltering

  const instanceIdsKey = useMemo(
    () => (instanceIds && instanceIds.length > 0 ? [...instanceIds].sort((left, right) => left - right).join(",") : ""),
    [instanceIds]
  )
  const filterKey = JSON.stringify(filters)
  const searchKey = search || ""
  const viewScopeKey = `${instanceId}|${instanceIdsKey}|${filterKey}|${searchKey}|${sort}|${order}|${useCrossInstanceEndpoint}|${isCrossSeedFiltering}`
  // Lets old stream callbacks detect committed scope changes without observing
  // abandoned render state.
  const currentViewScopeKeyRef = useRef(viewScopeKey)
  useEffect(() => {
    currentViewScopeKeyRef.current = viewScopeKey
  }, [viewScopeKey])

  const streamQueryKey = useMemo(
    () => ["torrents-list", instanceId, instanceIdsKey, 0, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering] as const,
    [instanceId, instanceIdsKey, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering]
  )

  const { instances } = useInstances()
  const activeInstanceIds = useMemo(
    () => (instances ?? []).filter(current => current.isActive).map(current => current.id).filter(id => id > 0),
    [instances]
  )

  // Concrete member set for an aggregated (all-instances / cross-instance) stream:
  // an explicit subset selection when provided, otherwise all active instances.
  // The stream needs concrete ids; if none can be resolved we fall back to polling.
  const streamInstanceIds = useMemo(() => {
    if (!useCrossInstanceEndpoint) {
      return undefined
    }
    const base = instanceIds && instanceIds.length > 0 ? instanceIds : activeInstanceIds
    const filtered = Array.from(new Set(base.filter(id => id > 0)))
    return filtered.length > 0 ? filtered : undefined
  }, [useCrossInstanceEndpoint, instanceIds, activeInstanceIds])

  // Single-instance views stream directly; aggregated views stream the cross-instance
  // endpoint once a concrete member set is known (otherwise fall back to polling below).
  const streamParams = useMemo(() => {
    if (!enabled) {
      return null
    }

    if (useCrossInstanceEndpoint) {
      // Cross-seed filtering encodes large `Hash == ... || ...` expressions in the
      // filters. The SSE subscription is sent as an EventSource GET URL, so those
      // expressions would risk request-line/proxy limits and reconnect churn; keep
      // cross-seed views on cross-instance polling. Only the all-instances view streams.
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
    }
  }, [enabled, filters, instanceId, useCrossInstanceEndpoint, isCrossSeedFiltering, streamInstanceIds, order, pageSize, search, sort])

  const listQueryKey = useMemo(
    () => ["torrents-list", instanceId, instanceIdsKey, currentPage, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering] as const,
    [instanceId, instanceIdsKey, currentPage, filters, search, sort, order, useCrossInstanceEndpoint, isCrossSeedFiltering]
  )

  const handleStreamPayload = useCallback(
    (payload: TorrentStreamPayload) => {
      if (!payload?.data) {
        return
      }

      // Drop full and delta frames from a superseded subscription before they can
      // write snapshots, query cache, rows, totals, or pagination flags.
      if (viewScopeKey !== currentViewScopeKeyRef.current) {
        return
      }

      // Resolve the frame to a full page-0 snapshot. Full frames (init/update and the
      // periodic delta keyframe) carry the whole page; a delta frame carries only the
      // changed rows and is reconstructed against the retained baseline. Either way the
      // result is normalized to camelCase once here so every sink — the query cache
      // (read by the REST-processing effect below), the retained snapshot, and the
      // table rows — sees identical metadata and the Instance column never flickers.
      let data: TorrentResponse
      // Aggregate-only delta ticks (speeds/counts changed but no row added, removed,
      // reordered, or changed) leave the page untouched, so the table list is left
      // referentially stable and only the stats/server-state sinks run. This skip is
      // safe because the server change-detects rows by fingerprinting the WHOLE row
      // JSON: any table-visible field (including cross-instance instance_name) that
      // changes forces the row into the delta, so changed:false guarantees no
      // row-visible change. Per-row styling derived from aggregate maps (category/tag)
      // still refreshes every tick via the unconditional metadata sinks below.
      let rowsChanged = true
      if (payload.type === "delta") {
        const base = lastFullSnapshotRef.current
        if (!base) {
          // No full baseline yet (a delta raced ahead of the init snapshot, or arrived
          // after a view reset). The next full frame reseeds; drop this one rather than
          // apply it against nothing.
          return
        }
        const applied = applyStreamDelta(base, payload, Boolean(useCrossInstanceEndpoint))
        data = applied.data
        rowsChanged = applied.changed
      } else {
        data = normalizeStreamedSnapshot(payload.data)
      }

      // REST owns the list until this listener has a usable stream baseline. Once
      // a frame arrives, abort any in-flight fallback before committing the frame
      // so a slower REST response cannot overwrite the newer stream snapshot.
      void queryClient.cancelQueries({ queryKey: streamQueryKey, exact: true })

      // Publish through the query cache FIRST and adopt its structurally-shared
      // result as the single object lineage for every sink. setQueryData applies
      // replaceEqualDeep against the cached response, so a row whose values did not
      // change keeps its existing object identity even across full frames and the
      // REST/stream boundary. Without this the delta baseline and the query cache
      // hold equal-but-distinct row objects, and the processing effect below (which
      // sees every cache write through the subscribed list query) swaps the page-0
      // window between the two lineages on every tick — so `torrent === torrent`
      // never holds and row-level memoization downstream can never take effect.
      data = queryClient.setQueryData<TorrentResponse>(streamQueryKey, data) ?? data

      // Retain the reconstructed full snapshot as the base for the next delta.
      lastFullSnapshotRef.current = data
      lastStreamSnapshotScopeRef.current = viewScopeKey

      setLastStreamSnapshot(data)
      rememberCountsSnapshot(viewScopeKey, data.counts, committedCountsSnapshotRef.current)
      updateAppInfoCache(data)
      updateMetadataCache(data)

      if (useCrossInstanceEndpoint) {
        // Aggregated streams only ever deliver the first page of cross-instance
        // torrents. Merge it into the displayed list keyed on instanceId+hash so
        // pages the user paginated in via REST survive: a wholesale replace would
        // reset the unified view to page 0 on every snapshot, so it could never
        // scroll past the first page (issue #1983). Page 0 stays authoritative for
        // its own window. See mergeStreamedCrossInstanceFirstPage.
        if (rowsChanged) {
          setAllTorrents(prev => mergeStreamedCrossInstanceFirstPage(prev, data))
        }

        if (typeof data.total === "number") {
          setLastKnownTotal(data.total)
        }
        if (currentPage === 0 && typeof data.hasMore === "boolean") {
          setHasLoadedAll(!data.hasMore)
        }
        return
      }

      if (rowsChanged) {
        setAllTorrents(prev => {
          const nextTorrents = data.torrents ?? []

          if (data.total === 0 || nextTorrents.length === 0) {
            return []
          }

          // Page 0 is authoritative for its window (a row it omits was deleted or moved
          // off page 0, so it must not be re-added); pagination-loaded later pages are
          // preserved. See mergeStreamedFirstPage.
          return mergeStreamedFirstPage(
            prev,
            nextTorrents,
            typeof data.total === "number" ? data.total : undefined
          )
        })
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
    streamState.connected &&
    streamState.initialized &&
    !streamState.error &&
    hasBaselineForCurrentView
  const preferCachedQuery = currentPage === 0 && shouldDisablePolling
  // Polls the scrolled-in window while a recheck/move is in progress anywhere
  // in it; stops on its own once every row settles. Not gated on the stream:
  // the stream only covers the first page.
  const hasTransientRows = useMemo(
    () => allTorrents.some(t => TRANSIENT_TORRENT_STATES.has(t.state)),
    [allTorrents]
  )
  // Keep the REST query (initial fetch + fallback polling) enabled until the
  // stream is actually connected, not just until it errors. While the stream is
  // still connecting (e.g. behind a buffering reverse proxy that delays the init
  // event) streamState.error is null but no data is arriving, so gating on error
  // alone would disable REST entirely and the first page would never load.
  const queryEnabled = enabled && (currentPage > 0 || !shouldDisablePolling)

  // Reset state when instanceId, filters, search, or sort changes
  // Use JSON.stringify to avoid resetting on every object reference change during polling
  useEffect(() => {
    setCurrentPage(0)
    setAllTorrents([])
    setHasLoadedAll(false)
    setLastKnownTotal(0)
    setLastProcessedPage(-1)
    setLastStreamSnapshot(null)
    lastStreamSnapshotScopeRef.current = ""
    // Drop the delta baseline so a delta from the previous view can never be applied
    // against the new one; the new subscription's init reseeds it.
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

  // Query for torrents - backend handles stale-while-revalidate
  const { data, isLoading, isFetching, isPlaceholderData } = useQuery<TorrentResponse>({
    queryKey: listQueryKey,
    queryFn: async ({ signal }) => {
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
        }, signal)
      }

      // The first fetch of a page's key is an ordinary pagination step: the earlier
      // pages are already displayed, so fetch only the new page (the effect below
      // appends it). A refetch of the same key — the post-mutation refetchQueries in
      // useTorrentActions, or any invalidation — must instead deliver the whole
      // loaded window (pages 0..currentPage): once the user has paginated this query
      // is the only active torrents-list observer, and the SSE stream only refreshes
      // the page-0 window, so scrolled-in rows would otherwise stay stale until a
      // full reload. Each page is served from the backend's in-memory sync cache, so
      // the window costs no qBittorrent calls.
      if (currentPage === 0 || queryClient.getQueryData(listQueryKey) === undefined) {
        return fetchPage(currentPage)
      }

      const pages = await Promise.all(
        Array.from({ length: currentPage + 1 }, (_, page) => fetchPage(page))
      )
      return combineWindowPages(pages)
    },
    // Trust backend cache - it returns immediately with stale data if needed
    staleTime: 0, // Always check with backend (it decides if cache is fresh)
    gcTime: 300000, // Keep in React Query cache for 5 minutes for navigation
    // Reuse the previous page's data while the next page is loading so the UI doesn't flash empty state
    placeholderData: currentPage > 0 ? ((previousData) => previousData) : undefined,
    // Only poll the first page to get fresh data - don't poll pagination pages
    // Reduce polling frequency for cross-instance calls since they're more expensive.
    // When the SSE stream is connected we disable polling entirely on the first page.
    refetchInterval:
      currentPage === 0? (
        pollingEnabled && !shouldDisablePolling? (useCrossInstanceEndpoint ? 10000 : TORRENT_STREAM_POLL_INTERVAL_MS): false
      ): (
        pollingEnabled && hasTransientRows? (useCrossInstanceEndpoint ? 10000 : TORRENT_STREAM_POLL_INTERVAL_MS): false
      ),
    refetchIntervalInBackground, // Controls background polling behavior
    refetchOnWindowFocus: currentPage === 0 && pollingEnabled,
    enabled: queryEnabled,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: enabled && !isAllInstancesView })

  const activeData = useMemo(() => {
    const scopedStreamSnapshot =
      lastStreamSnapshotScopeRef.current === viewScopeKey ? lastStreamSnapshot : null

    if (shouldDisablePolling && scopedStreamSnapshot) {
      return scopedStreamSnapshot
    }

    return data ?? scopedStreamSnapshot ?? null
  }, [data, lastStreamSnapshot, shouldDisablePolling, viewScopeKey])

  // Update torrents when data arrives or changes (including optimistic updates)
  useEffect(() => {
    // When filters/search/sort change we reset lastProcessedPage to -1. Skip placeholder
    // data in that window so we don't repopulate the table with stale results from the
    // previous query while the new request is in-flight.
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

    updateAppInfoCache(data)
    updateMetadataCache(data)
    rememberCountsSnapshot(viewScopeKey, data.counts, committedCountsSnapshotRef.current)

    if (data.total !== undefined) {
      setLastKnownTotal(data.total)
    }

    // When the first page reports zero results, immediately clear the list so
    // downstream UIs don't render stale rows from the previous query.
    if (currentPage === 0 && data.total === 0) {
      setAllTorrents([])
      setHasLoadedAll(true)
      setLastProcessedPage(currentPage)
      setIsLoadingMore(false)
      return
    }

    // Handle both regular torrents and cross-instance torrents
    const torrentsData = data.isCrossInstance? (data.crossInstanceTorrents || data.cross_instance_torrents): data.torrents

    if (!torrentsData) {
      setIsLoadingMore(false)
      return
    }

    if (currentPage === 0) {
      // First page: replace all (covers polling updates and optimistic cache writes).
      setAllTorrents(torrentsData)
      setHasLoadedAll(!data.hasMore)
      setLastProcessedPage(currentPage)
    } else if (currentPage !== lastProcessedPage) {
      // Ordinary pagination step: queryFn fetched only the new page; append it.
      setAllTorrents(prev => dedupeRows(
        [...prev, ...torrentsData],
        data.isCrossInstance? row => `${(row as CrossInstanceTorrent).instanceId}:${row.hash}`: row => row.hash
      ))
      if (!data.hasMore) {
        setHasLoadedAll(true)
      }
      setLastProcessedPage(currentPage)
    } else if (data.windowPageCount) {
      // Same-key refetch delivered the whole loaded window (pages 0..currentPage —
      // see queryFn): replace the list wholesale. This is what lets a post-mutation
      // refetch update rows on every loaded page instead of only page 0.
      setAllTorrents(torrentsData)
      setHasLoadedAll(!data.hasMore)
    }

    setIsLoadingMore(false)
  }, [data, currentPage, lastProcessedPage, isFetching, isPlaceholderData, rememberCountsSnapshot, updateAppInfoCache, updateMetadataCache, viewScopeKey])

  // Load more function for pagination - following TanStack Query best practices
  const loadMore = () => {
    const now = Date.now()

    // TanStack Query pattern: check hasNextPage && !isFetching before calling fetchNextPage
    // Our equivalent: check !hasLoadedAll && !(isLoadingMore || isFetching)
    if (hasLoadedAll) {
      return
    }

    if (isLoadingMore || isFetching) {
      return
    }

    // Enhanced throttling: 500ms for rapid scroll scenarios (up from 300ms)
    // This helps prevent race conditions during very fast scrolling
    if (now - lastRequestTime < 500) {
      return
    }

    setLastRequestTime(now)
    setIsLoadingMore(true)
    setCurrentPage(prev => prev + 1)
  }

  // Extract stats from response or calculate defaults
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

  // Check if data is from cache or fresh (backend provides this info)
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

  // Use lastKnownTotal when loading more pages to prevent flickering
  const effectiveTotalCount =
    currentPage > 0 && typeof activeData?.total !== "number"? lastKnownTotal: activeData?.total ?? lastKnownTotal

  const responseUseSubcategories = activeData?.useSubcategories ?? activeData?.serverState?.use_subcategories ?? data?.useSubcategories ?? data?.serverState?.use_subcategories ?? false
  const supportsSubcategories = isAllInstancesView ? responseUseSubcategories : (capabilities?.supportsSubcategories ?? false)
  const countsScopeKey = viewScopeKey
  const responseCounts = activeData?.counts ?? data?.counts

  // Stream snapshots can omit counts when tracker hydration is intentionally
  // skipped. Keep the latest counts for the same view so the sidebar does not
  // regress to stale/empty state while the connected stream owns activeData.
  const effectiveCounts = responseCounts ?? (
    lastCountsSnapshot.scopeKey === countsScopeKey ? lastCountsSnapshot.counts : undefined
  )

  return {
    torrents: allTorrents,
    totalCount: effectiveTotalCount,
    stats,
    counts: effectiveCounts,
    appInfo: activeData?.appInfo ?? data?.appInfo ?? null,
    categories: activeData?.categories ?? data?.categories,
    tags: activeData?.tags ?? data?.tags,
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
    // Cross-instance information
    isCrossInstance: data?.isCrossInstance ?? useCrossInstanceEndpoint,
    isCrossSeedFiltering,
    isAllInstancesView,
    isCrossInstanceEndpoint: useCrossInstanceEndpoint,
    // Metadata about data freshness
    isFreshData: !isCachedData || !isStaleData,
    isCachedData,
    isStaleData,
    cacheAge: cacheMetadata?.age,
    isStreaming: shouldDisablePolling,
    streamConnected: streamState.connected,
    streamError: streamState.error,
    streamMeta: streamState.lastMeta,
    streamRetrying: streamState.retrying,
    streamNextRetryAt: streamState.nextRetryAt,
    streamRetryAttempt: streamState.retryAttempt,
  }
}
