/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import { InstanceSettingsButton } from "@/components/instances/InstanceSettingsButton"
import { MigrationHint } from "@/components/instances/MigrationHint"
import { MagnetHandlerBanner } from "@/components/MagnetHandlerBanner"
import { PageHeader } from "@/components/layout/PageHeader"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { RemoteAccessIndicator } from "@/components/instances/RemoteAccessIndicator"
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from "@/components/ui/accordion"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle
} from "@/components/ui/drawer"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useListenPort } from "@/hooks/usePreferenceValues"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { useTitleBarSpeeds } from "@/hooks/useTitleBarSpeeds"
import { api } from "@/lib/api"
import { writeRaw } from "@/lib/client-settings"
import {
  DASHBOARD_STATS_FALLBACK_ORDER,
  DASHBOARD_STATS_FALLBACK_SORT,
  createDashboardStatsFallbackQueryKey,
  hasDashboardStatsPayload,
  resolveDashboardDataStatusKind,
  resolveDashboardSizes,
  mergeDashboardInstanceMeta,
  mergeDashboardStatsSnapshot,
  resolveDashboardStreamError,
  resolveDashboardTorrentCounts,
  shouldUseDashboardStatsFallback
} from "@/lib/dashboard-stream"
import { TRACKER_HEALTH_DISPLAY, type TrackerHealth } from "@/lib/torrent-table/row-display"
import { cn, formatBytes, formatBytesOrFallback, formatDuration, getRatioColor } from "@/lib/utils"
import type {
  CacheMetadata,
  InstanceMeta,
  InstanceResponse,
  QBittorrentAppInfo,
  ServerState,
  TorrentResponse,
  TorrentCounts,
  TorrentStats
} from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Activity, AlertCircle, AlertTriangle, Ban, BrickWallFire, ChevronDown, ChevronRight, ChevronUp, Clock, Database, Download, ExternalLink, Eye, EyeOff, Globe, HardDrive, Minus, MoreVertical, Plus, Rabbit, RefreshCcw, Turtle, Upload, Zap } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"

import { DashboardTotalSize, SizeCalculatingMark } from "@/components/dashboard/UniqueSize"
import { DashboardSettingsDialog } from "@/components/dashboard-settings-dialog"
import { createStreamKey, useSyncStreamManager, type StreamState } from "@/contexts/SyncStreamContext"
import { DEFAULT_DASHBOARD_SETTINGS, useDashboardSettings, useUpdateDashboardSettings } from "@/hooks/useDashboardSettings"
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { TorrentStreamPayload } from "@/types"

interface DashboardInstanceStats {
  instance: InstanceResponse
  stats: TorrentStats | null
  serverState: ServerState | null
  torrentCounts?: TorrentCounts
  appInfo: QBittorrentAppInfo | null
  altSpeedEnabled: boolean
  isLoading: boolean
  error: unknown
  streamConnected: boolean
  streamError: string | null
  hasLiveDashboardStatsPayload: boolean
  isDashboardStatsFallbackActive: boolean
  cacheMetadata: CacheMetadata | null | undefined
  instanceMeta: InstanceMeta | null  // Real-time instance health from SSE
}

type InstanceStreamData = {
  stats: TorrentStats | null
  serverState: ServerState | null
  torrentCounts?: TorrentCounts
  appInfo: QBittorrentAppInfo | null
  altSpeedEnabled: boolean
  isLoading: boolean
  error: unknown
  streamConnected: boolean
  streamError: string | null
  hasLiveDashboardStatsPayload: boolean
  fallbackPollInterval: number | false
  cacheMetadata: CacheMetadata | null | undefined
  instanceMeta: InstanceMeta | null  // Real-time instance health from SSE
}

const STATS_FALLBACK_POLL_INTERVAL_MS = 5000

const createDefaultInstanceStreamData = (): InstanceStreamData => ({
  stats: null,
  serverState: null,
  torrentCounts: undefined,
  appInfo: null,
  altSpeedEnabled: false,
  isLoading: true,
  error: null,
  streamConnected: false,
  streamError: null,
  hasLiveDashboardStatsPayload: false,
  fallbackPollInterval: STATS_FALLBACK_POLL_INTERVAL_MS,
  cacheMetadata: null,
  instanceMeta: null,
})

const STREAM_REFRESH_INTERVAL_MS = 2000

type InstanceUpdateResult =
  | InstanceStreamData
  | {
    data: InstanceStreamData
    immediate?: boolean
  }

function cloneInstanceDataRecord(source: Record<number, InstanceStreamData>) {
  const next: Record<number, InstanceStreamData> = {}
  for (const [key, value] of Object.entries(source)) {
    next[Number(key)] = value
  }
  return next
}

function recordsShallowEqual(
  a: Record<number, InstanceStreamData>,
  b: Record<number, InstanceStreamData>
) {
  if (a === b) {
    return true
  }

  const aKeys = Object.keys(a)
  const bKeys = Object.keys(b)
  if (aKeys.length !== bKeys.length) {
    return false
  }

  for (const key of aKeys) {
    const numericKey = Number(key)
    if (a[numericKey] !== b[numericKey]) {
      return false
    }
  }

  return true
}

function instanceStreamDataFromResponse(
  response: TorrentResponse | undefined,
  current?: InstanceStreamData
): InstanceStreamData | undefined {
  if (!response) {
    return undefined
  }

  return {
    stats: response.stats ?? current?.stats ?? null,
    serverState: response.serverState ?? current?.serverState ?? null,
    torrentCounts: resolveDashboardTorrentCounts(response.counts, current?.torrentCounts),
    appInfo: response.appInfo ?? current?.appInfo ?? null,
    altSpeedEnabled: response.serverState?.use_alt_speed_limits ?? current?.altSpeedEnabled ?? false,
    isLoading: false,
    error: null,
    streamConnected: current?.streamConnected ?? false,
    streamError: current?.streamError ?? null,
    hasLiveDashboardStatsPayload: current?.hasLiveDashboardStatsPayload ?? false,
    fallbackPollInterval: current?.fallbackPollInterval ?? STATS_FALLBACK_POLL_INTERVAL_MS,
    cacheMetadata: response.cacheMetadata ?? current?.cacheMetadata ?? null,
    instanceMeta: response.instanceMeta ?? current?.instanceMeta ?? null,
  }
}

function mergeCachedInstanceData(
  current: InstanceStreamData | undefined,
  cached: InstanceStreamData | undefined
) {
  if (!current) {
    return cached
  }
  if (!cached) {
    return current
  }

  const next: InstanceStreamData = {
    ...current,
    stats: current.stats ?? cached.stats,
    serverState: current.serverState ?? cached.serverState,
    torrentCounts: resolveDashboardTorrentCounts(current.torrentCounts, cached.torrentCounts),
    appInfo: current.appInfo ?? cached.appInfo,
    altSpeedEnabled: current.serverState?.use_alt_speed_limits ?? cached.serverState?.use_alt_speed_limits ?? current.altSpeedEnabled,
    isLoading: current.isLoading && !hasDashboardStatsPayload(cached),
    error: current.error,
    hasLiveDashboardStatsPayload: current.hasLiveDashboardStatsPayload,
    cacheMetadata: current.cacheMetadata ?? cached.cacheMetadata,
    instanceMeta: current.instanceMeta ?? cached.instanceMeta,
  }

  if (
    next.stats === current.stats &&
    next.serverState === current.serverState &&
    next.torrentCounts === current.torrentCounts &&
    next.appInfo === current.appInfo &&
    next.altSpeedEnabled === current.altSpeedEnabled &&
    next.isLoading === current.isLoading &&
    next.hasLiveDashboardStatsPayload === current.hasLiveDashboardStatsPayload &&
    next.cacheMetadata === current.cacheMetadata &&
    next.instanceMeta === current.instanceMeta
  ) {
    return current
  }

  return next
}

// Shared hook for computing global stats across all instances
function useGlobalStats(statsData: DashboardInstanceStats[]) {
  return useMemo(() => {
    const connected = statsData.filter(({ instance }) => instance?.connected).length
    const totalTorrents = statsData.reduce((sum, { torrentCounts }) =>
      sum + (torrentCounts?.total || 0), 0)
    const activeTorrents = statsData.reduce((sum, { torrentCounts }) =>
      sum + (torrentCounts?.status?.active || 0), 0)
    const totalDownload = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalDownloadSpeed || 0), 0)
    const totalUpload = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalUploadSpeed || 0), 0)
    const totalErrors = statsData.reduce((sum, { torrentCounts }) =>
      sum + (torrentCounts?.status?.errored || 0), 0)
    const { totalSize, seedingSize: totalSeedingSize, calculating: sizeCalculating } =
      resolveDashboardSizes(statsData.map(({ stats }) => stats))
    const totalRemainingSize = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalRemainingSize || 0), 0)
    const downloadingTorrents = statsData.reduce((sum, { stats }) =>
      sum + (stats?.downloading || 0), 0)
    const seedingTorrents = statsData.reduce((sum, { stats }) =>
      sum + (stats?.seeding || 0), 0)

    // Calculate server stats
    const alltimeDl = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.alltime_dl || 0), 0)
    const alltimeUl = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.alltime_ul || 0), 0)
    const totalPeers = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.total_peer_connections || 0), 0)

    // Calculate global ratio
    let globalRatio = 0
    if (alltimeDl > 0) {
      globalRatio = alltimeUl / alltimeDl
    }

    return {
      connected,
      total: statsData.length,
      totalTorrents,
      activeTorrents,
      totalDownload,
      totalUpload,
      totalErrors,
      totalSize,
      totalRemainingSize,
      totalSeedingSize,
      sizeCalculating,
      downloadingTorrents,
      seedingTorrents,
      alltimeDl,
      alltimeUl,
      globalRatio,
      totalPeers,
    }
  }, [statsData])
}

// Optimized hook to get all instance stats using shared TorrentResponse cache
// eslint-disable-next-line react-refresh/only-export-components
export function useAllInstanceStats(instances: InstanceResponse[], options: { enabled: boolean }): DashboardInstanceStats[] {
  const { enabled: streamEnabled } = options
  const syncStream = useSyncStreamManager()
  const queryClient = useQueryClient()
  const streamConnectionsRef = useRef(
    new Map<
      number,
      {
        key: string
        disconnect: () => void
        unsubscribe: () => void
        cancelRef: { current: boolean }
      }
    >()
  )
  const baseStreamParams = useMemo(
    () => ({
      page: 0,
      limit: 1,
      sort: "added_on",
      order: "desc" as const,
      summary: true,
      uniqueSize: true,
    }),
    []
  )
  const [instanceData, setInstanceData] = useState<Record<number, InstanceStreamData>>({})
  const latestDataRef = useRef<Record<number, InstanceStreamData>>({})
  const flushTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const fallbackQueries = useQueries({
    queries: instances.map(instance => {
      const streamState = instanceData[instance.id] ?? latestDataRef.current[instance.id]
      const fallbackEnabled = streamEnabled && shouldUseDashboardStatsFallback(
        streamState?.streamConnected ?? false,
        streamState?.hasLiveDashboardStatsPayload ?? false
      )

      return {
        // Independent lightweight (limit:1) probe used only to keep dashboard stats
        // alive while an instance's stream is down. It does NOT share the torrent
        // list's cache entry (that key now also encodes filters/scope), so keep this
        // request minimal rather than relying on reuse.
        queryKey: createDashboardStatsFallbackQueryKey(instance.id),
        queryFn: () => api.getTorrents(instance.id, {
          page: 0,
          limit: 1,
          sort: DASHBOARD_STATS_FALLBACK_SORT,
          order: DASHBOARD_STATS_FALLBACK_ORDER,
          uniqueSize: true,
        }),
        enabled: fallbackEnabled,
        refetchInterval: fallbackEnabled ? (streamState?.fallbackPollInterval ?? STATS_FALLBACK_POLL_INTERVAL_MS) : false,
        refetchIntervalInBackground: false,
        staleTime: 2000,
        gcTime: 300000,
        placeholderData: (previousData: TorrentResponse | undefined) => previousData,
        retry: 1,
        retryDelay: 1000,
      }
    }),
  })

  const getCachedInstanceData = useCallback(
    (instanceId: number, current?: InstanceStreamData) => {
      const cachedResponse = queryClient.getQueryData<TorrentResponse>(
        createDashboardStatsFallbackQueryKey(instanceId)
      )

      return instanceStreamDataFromResponse(cachedResponse, current)
    },
    [queryClient]
  )

  const flushInstanceData = useCallback(
    (force = false) => {
      const snapshot = latestDataRef.current
      setInstanceData(prev => {
        if (!force && recordsShallowEqual(prev, snapshot)) {
          return prev
        }
        return cloneInstanceDataRecord(snapshot)
      })
    },
    []
  )

  const scheduleFlush = useCallback(() => {
    if (flushTimerRef.current) {
      return
    }

    flushTimerRef.current = setTimeout(() => {
      flushTimerRef.current = null
      flushInstanceData()
    }, STREAM_REFRESH_INTERVAL_MS)
  }, [flushInstanceData])

  const applyInstanceData = useCallback(
    (instanceId: number, buildUpdate: (current: InstanceStreamData) => InstanceUpdateResult) => {
      const currentRecord = latestDataRef.current
      let current = currentRecord[instanceId]
      if (!current) {
        current = getCachedInstanceData(instanceId) ?? createDefaultInstanceStreamData()
        currentRecord[instanceId] = current
      }

      const result = buildUpdate(current)
      const { data: next, immediate } =
        "data" in result ? result : { data: result }

      if (next === current) {
        return
      }

      currentRecord[instanceId] = next

      if (immediate) {
        flushInstanceData(true)
      } else {
        scheduleFlush()
      }
    },
    [flushInstanceData, getCachedInstanceData, scheduleFlush]
  )

  useEffect(() => {
    const nextRecord: Record<number, InstanceStreamData> = {}
    instances.forEach(instance => {
      const current = latestDataRef.current[instance.id]
      nextRecord[instance.id] =
        mergeCachedInstanceData(current, getCachedInstanceData(instance.id, current)) ??
        createDefaultInstanceStreamData()
    })
    latestDataRef.current = nextRecord
    flushInstanceData(true)
  }, [instances, flushInstanceData, getCachedInstanceData])

  useEffect(() => {
    if (typeof document === "undefined") {
      return
    }

    const flushIfVisible = () => {
      if (typeof document !== "undefined" && document.visibilityState !== "visible") {
        return
      }

      if (flushTimerRef.current) {
        if (typeof window !== "undefined") {
          window.clearTimeout(flushTimerRef.current)
        } else {
          clearTimeout(flushTimerRef.current)
        }
        flushTimerRef.current = null
      }

      flushInstanceData(true)
    }

    document.addEventListener("visibilitychange", flushIfVisible)

    if (typeof window !== "undefined") {
      window.addEventListener("focus", flushIfVisible)
    }

    return () => {
      document.removeEventListener("visibilitychange", flushIfVisible)

      if (typeof window !== "undefined") {
        window.removeEventListener("focus", flushIfVisible)
      }
    }
  }, [flushInstanceData])

  useEffect(() => {
    const activeInstanceIds = new Set<number>()

    // Query handoff: when the dashboard is hidden we tear down the heavy stream
    // connections and let the title-bar transfer-info polling take over.
    if (streamEnabled) {
      instances.forEach(instance => {
        const params = {
          ...baseStreamParams,
          instanceId: instance.id,
        }
        const streamKey = createStreamKey(params)
        activeInstanceIds.add(instance.id)

        const existing = streamConnectionsRef.current.get(instance.id)
        if (existing && existing.key === streamKey && !existing.cancelRef.current) {
          return
        }

        if (existing) {
          existing.cancelRef.current = true
          existing.disconnect()
          existing.unsubscribe()
        }

        const cancelRef = { current: false }

        const disconnect = syncStream.connect(params, (payload: TorrentStreamPayload) => {
          if (cancelRef.current || !payload) {
            return
          }

          if (payload.type === "stream-error") {
            applyInstanceData(instance.id, current => {
              return {
                data: {
                  ...current,
                  isLoading: false,
                  error: payload.error ?? current.error,
                  streamError: payload.error ?? current.streamError,
                  streamConnected: false,
                  hasLiveDashboardStatsPayload: false,
                  instanceMeta: null,
                },
                immediate: true,
              }
            })
            return
          }

          if (!payload.data) {
            return
          }

          const data = payload.data
          const hasLiveDashboardStatsPayload = hasDashboardStatsPayload(data)
          queryClient.setQueryData<TorrentResponse | undefined>(
            createDashboardStatsFallbackQueryKey(instance.id),
            current => mergeDashboardStatsSnapshot(data, current)
          )
          if (data.appInfo) {
            queryClient.setQueryData(["qbittorrent-app-info", instance.id], data.appInfo)
          }

          applyInstanceData(instance.id, current => {
            const next: InstanceStreamData = {
              stats: data.stats ?? current.stats,
              serverState: data.serverState ?? current.serverState,
              torrentCounts: resolveDashboardTorrentCounts(data.counts, current.torrentCounts),
              appInfo: data.appInfo ?? current.appInfo,
              altSpeedEnabled: data.serverState?.use_alt_speed_limits ?? current.altSpeedEnabled,
              isLoading: false,
              error: null,
              streamConnected: true,
              streamError: null,
              hasLiveDashboardStatsPayload: current.hasLiveDashboardStatsPayload || hasLiveDashboardStatsPayload,
              fallbackPollInterval: current.fallbackPollInterval,
              cacheMetadata: data.cacheMetadata ?? current.cacheMetadata,
              instanceMeta: data.instanceMeta ?? current.instanceMeta,
            }

            return {
              data: next,
              immediate: current.isLoading && !next.isLoading,
            }
          })
        })

        const applyStreamState = (snapshot: StreamState) => {
          const streamUsable = isStreamUsable(snapshot)
          applyInstanceData(instance.id, current => {
            const next: InstanceStreamData = {
              ...current,
              streamConnected: streamUsable,
              streamError: snapshot.error ?? (streamUsable ? null : current.streamError),
              hasLiveDashboardStatsPayload: streamUsable &&
                current.hasLiveDashboardStatsPayload,
              fallbackPollInterval: streamFallbackPollInterval(snapshot, STATS_FALLBACK_POLL_INTERVAL_MS),
            }

            if (snapshot.error) {
              next.error = snapshot.error
              next.isLoading = false
            } else if (streamUsable && !current.isLoading) {
              next.error = null
            }

            if (!streamUsable) {
              next.instanceMeta = null
            }

            return next
          })
        }

        const unsubscribe = syncStream.subscribe(streamKey, snapshot => {
          if (!cancelRef.current) {
            applyStreamState(snapshot)
          }
        })

        const initialSnapshot = syncStream.getState(streamKey)
        if (initialSnapshot) {
          applyStreamState(initialSnapshot)
        }

        streamConnectionsRef.current.set(instance.id, { key: streamKey, disconnect, unsubscribe, cancelRef })
      })
    }

    streamConnectionsRef.current.forEach((entry, instanceId) => {
      if (!activeInstanceIds.has(instanceId)) {
        entry.cancelRef.current = true
        entry.disconnect()
        entry.unsubscribe()
        streamConnectionsRef.current.delete(instanceId)
      }
    })
  }, [instances, syncStream, baseStreamParams, queryClient, applyInstanceData, streamEnabled])

  useEffect(() => {
    const streamConnections = streamConnectionsRef.current

    return () => {
      streamConnections.forEach(entry => {
        entry.cancelRef.current = true
        entry.disconnect()
        entry.unsubscribe()
      })
      streamConnections.clear()
      if (flushTimerRef.current) {
        clearTimeout(flushTimerRef.current)
        flushTimerRef.current = null
      }
    }
  }, [])

  return instances.map<DashboardInstanceStats>((instance, index) => {
    const cachedState = getCachedInstanceData(instance.id, instanceData[instance.id])
    const state =
      mergeCachedInstanceData(instanceData[instance.id], cachedState) ??
      createDefaultInstanceStreamData()
    const fallbackQuery = fallbackQueries[index]
    const fallbackData = fallbackQuery?.data as TorrentResponse | undefined
    const isFallbackActive = shouldUseDashboardStatsFallback(
      state.streamConnected,
      state.hasLiveDashboardStatsPayload
    )

    const stats = isFallbackActive ? (fallbackData?.stats ?? state.stats) : state.stats
    const serverState = isFallbackActive ? (fallbackData?.serverState ?? state.serverState) : state.serverState
    const torrentCounts = isFallbackActive? resolveDashboardTorrentCounts(fallbackData?.counts, state.torrentCounts): resolveDashboardTorrentCounts(state.torrentCounts, fallbackData?.counts)
    const appInfo = isFallbackActive ? (fallbackData?.appInfo ?? state.appInfo) : state.appInfo
    const cacheMetadata = isFallbackActive ? (fallbackData?.cacheMetadata ?? state.cacheMetadata) : state.cacheMetadata

    const hasHydratedData = Boolean(stats || serverState || torrentCounts)
    const isLoading = isFallbackActive? (!hasHydratedData && (state.isLoading || fallbackQuery?.isLoading || fallbackQuery?.isFetching)): state.isLoading
    const error = (() => {
      if (!isFallbackActive) {
        return state.error
      }
      if (fallbackQuery?.error) {
        return fallbackQuery.error
      }
      if (fallbackData) {
        return null
      }
      return state.error
    })()

    const mergedInstance = mergeDashboardInstanceMeta(instance, state.streamConnected, state.instanceMeta)

    return {
      instance: mergedInstance,
      stats,
      serverState,
      torrentCounts,
      appInfo,
      altSpeedEnabled: serverState?.use_alt_speed_limits ?? state.altSpeedEnabled,
      isLoading,
      error,
      streamConnected: state.streamConnected,
      // A stopped poll refreshes nothing, so its fallback data must not hide the error
      streamError: resolveDashboardStreamError(state.streamError, isFallbackActive && state.fallbackPollInterval !== false, fallbackData),
      hasLiveDashboardStatsPayload: state.hasLiveDashboardStatsPayload,
      isDashboardStatsFallbackActive: isFallbackActive,
      cacheMetadata,
      instanceMeta: state.instanceMeta,
    }
  })
}


// Issue rows of the instance card, in the order they show
const TRACKER_HEALTH_ROWS: Array<{ health: TrackerHealth; labelKey: string }> = [
  { health: "unregistered", labelKey: "instanceCard.unregisteredTorrents" },
  { health: "tracker_down", labelKey: "instanceCard.trackerDown" },
  { health: "tracker_error", labelKey: "instanceCard.trackerError" },
]

function InstanceCard({
  instanceData,
  isAdvancedMetricsOpen,
  setIsAdvancedMetricsOpen,
}: {
  instanceData: DashboardInstanceStats
  isAdvancedMetricsOpen: boolean
  setIsAdvancedMetricsOpen: (open: boolean) => void
}) {
  const { t } = useTranslation("dashboard")
  const {
    instance,
    stats,
    serverState,
    torrentCounts,
    appInfo,
    altSpeedEnabled,
    isLoading,
    error,
    streamConnected,
    streamError,
    isDashboardStatsFallbackActive,
    cacheMetadata,
  } = instanceData
  const [showSpeedLimitDialog, setShowSpeedLimitDialog] = useState(false)

  // Alternative speed limits toggle - no need to track state, just provide toggle function
  const queryClient = useQueryClient()
  const { mutate: toggleAltSpeed, isPending: isToggling } = useMutation({
    mutationFn: () => api.toggleAlternativeSpeedLimits(instance.id),
    onSuccess: () => {
      // Invalidate torrent queries to refresh server state
      queryClient.invalidateQueries({
        queryKey: ["torrents-list", instance.id],
      })
    },
  })

  // Still need app info for version display - keep this separate as it's cached well
  const {
    data: qbittorrentAppInfo,
    versionInfo: qbittorrentVersionInfo,
  } = useQBittorrentAppInfo(instance.id, {
    initialData: appInfo ?? undefined,
    fetchIfMissing: !appInfo,
  })
  const listenPort = useListenPort(instance.id, { enabled: instance.connected })
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit] = useSpeedUnits()
  const appVersion = qbittorrentAppInfo?.version || qbittorrentVersionInfo?.appVersion || ""
  const webAPIVersion = qbittorrentAppInfo?.webAPIVersion || qbittorrentVersionInfo?.webAPIVersion || ""
  const libtorrentVersion = qbittorrentAppInfo?.buildInfo?.libtorrent || ""
  const launchTime = qbittorrentAppInfo?.processInfo?.launchTime
  const uptimeSeconds = typeof launchTime === "number" && launchTime > 0 ? Math.max(0, Math.floor(Date.now() / 1000) - launchTime) : null
  const displayUrl = instance.host

  // Determine card state
  const isFirstLoad = isLoading && !stats
  const isDisconnected = (stats && !instance.connected) || (!isFirstLoad && !instance.connected)
  const hasError = Boolean(error) || (!isFirstLoad && !stats)
  const hasDecryptionOrRecentErrors = instance.hasDecryptionError || (instance.recentErrors && instance.recentErrors.length > 0)

  const rawConnectionStatus = instance.connectionStatus ?? serverState?.connection_status ?? ""
  const normalizedConnectionStatus = rawConnectionStatus ? rawConnectionStatus.trim().toLowerCase() : ""
  const formattedConnectionStatus = normalizedConnectionStatus ? normalizedConnectionStatus.replace(/_/g, " ") : ""
  const connectionStatusDisplay = formattedConnectionStatus ? formattedConnectionStatus.replace(/\b\w/g, (char: string) => char.toUpperCase()) : ""
  const hasConnectionStatus = Boolean(formattedConnectionStatus)


  const isConnectable = normalizedConnectionStatus === "connected"
  const isFirewalled = normalizedConnectionStatus === "firewalled"
  const ConnectionStatusIcon = isConnectable ? Globe : isFirewalled ? BrickWallFire : Ban
  const connectionStatusIconClass = hasConnectionStatus ? isConnectable ? "text-green-500" : isFirewalled ? "text-amber-500" : "text-destructive" : ""

  const connectionStatusTooltip = connectionStatusDisplay ? `${isConnectable ? t("instanceCard.connectable") : connectionStatusDisplay}${listenPort ? t("instanceCard.portInfo", { port: listenPort }) : ""}` : ""

  const dashboardDataStatusKind = resolveDashboardDataStatusKind({
    streamError,
    fallbackActive: isDashboardStatsFallbackActive,
    cacheMetadata,
    isFirstLoad,
    streamConnected,
  })
  const dashboardDataStatus = (() => {
    switch (dashboardDataStatusKind) {
      case "error":
        return {
          Icon: AlertCircle,
          iconClassName: "text-destructive",
          tooltip: t("instanceCard.streamStatus.error"),
        }
      case "cached":
        return {
          Icon: Database,
          iconClassName: cacheMetadata?.isStale ? "text-amber-500" : "text-blue-500",
          tooltip: t("instanceCard.streamStatus.cached"),
        }
      case "fallback":
        return {
          Icon: RefreshCcw,
          iconClassName: "text-muted-foreground",
          tooltip: t("instanceCard.streamStatus.fallback"),
        }
      // "live" is the healthy path and needs no badge; the cases above all warn
      // that the numbers are not fresh, which is the only reason to spend the space
      default:
        return null
    }
  })()

  // Determine if settings button should show
  const showSettingsButton = instance.connected && !isFirstLoad && !hasDecryptionOrRecentErrors

  // Determine link destination
  const linkTo = hasDecryptionOrRecentErrors ? "/settings" : "/instances/$instanceId"
  const linkParams = hasDecryptionOrRecentErrors ? undefined : { instanceId: instance.id.toString() }
  const linkSearch = hasDecryptionOrRecentErrors ? { tab: "instances" as const } : undefined

  // Unified return statement
  return (
    <>
      <Card className="hover:shadow-lg transition-shadow">
        <CardHeader className={`${!isFirstLoad ? "gap-1" : ""} grid-cols-[minmax(0,1fr)] overflow-hidden`}>
          <div className="flex flex-wrap items-center gap-x-2 gap-y-1 w-full">
            <Link
              to={linkTo}
              params={linkParams}
              search={linkSearch}
              className="flex items-center gap-2 hover:underline overflow-hidden flex-1 min-w-0"
            >
              <CardTitle
                className="text-lg truncate min-w-0"
                title={instance.name}
              >
                {instance.name}
              </CardTitle>
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            </Link>
            <div className="flex items-center gap-1 justify-end shrink-0 sm:min-w-[4.5rem]">
              {dashboardDataStatus && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      aria-label={dashboardDataStatus.tooltip}
                      className={`inline-flex h-8 w-8 items-center justify-center rounded-md ${dashboardDataStatus.iconClassName}`}
                    >
                      <dashboardDataStatus.Icon className="h-4 w-4" aria-hidden="true" />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>
                    {dashboardDataStatus.tooltip}
                  </TooltipContent>
                </Tooltip>
              )}
              {instance.reannounceSettings?.enabled && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <RefreshCcw className="h-4 w-4 text-green-600" />
                  </TooltipTrigger>
                  <TooltipContent>
                    {t("instanceCard.reannounceTooltip")}
                  </TooltipContent>
                </Tooltip>
              )}
              {instance.connected && !isFirstLoad && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        setShowSpeedLimitDialog(true)
                      }}
                      disabled={isToggling}
                      className="size-11 sm:size-8 shrink-0"
                    >
                      {altSpeedEnabled ? (
                        <Turtle className="h-4 w-4 text-orange-600" />
                      ) : (
                        <Rabbit className="h-4 w-4 text-green-600" />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    {t("instanceCard.altSpeedLimits", { status: altSpeedEnabled ? "On" : "Off" })}
                  </TooltipContent>
                </Tooltip>
              )}
              <RemoteAccessIndicator instance={instance} variant="icon" />
              <InstanceSettingsButton
                instanceId={instance.id}
                instanceName={instance.name}
                instance={instance}
                showButton={showSettingsButton}
              />
            </div>
          </div>

          <AlertDialog open={showSpeedLimitDialog} onOpenChange={setShowSpeedLimitDialog}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {altSpeedEnabled ? t("instanceCard.altSpeedDialog.disableTitle") : t("instanceCard.altSpeedDialog.enableTitle")}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {altSpeedEnabled ? t("instanceCard.altSpeedDialog.disableDescription", { name: instance.name }) : t("instanceCard.altSpeedDialog.enableDescription", { name: instance.name })}
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>{t("instanceCard.altSpeedDialog.cancel")}</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => {
                    toggleAltSpeed()
                    setShowSpeedLimitDialog(false)
                  }}
                >
                  {altSpeedEnabled ? t("instanceCard.altSpeedDialog.disable") : t("instanceCard.altSpeedDialog.enable")}
                </AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
          {(appVersion || webAPIVersion || libtorrentVersion || formattedConnectionStatus) && (
            <CardDescription className="flex flex-wrap items-center gap-1.5 text-xs">
              {formattedConnectionStatus && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      aria-label={t("instanceCard.connectionStatus", { status: connectionStatusDisplay || formattedConnectionStatus })}
                      className={`inline-flex h-5 w-5 items-center justify-center ${connectionStatusIconClass}`}
                    >
                      <ConnectionStatusIcon className="h-4 w-4" aria-hidden="true" />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent className="max-w-[220px]">
                    <p>{connectionStatusTooltip}</p>
                  </TooltipContent>
                </Tooltip>
              )}
              {appVersion && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0.5">
                  qBit {appVersion}
                </Badge>
              )}
              {webAPIVersion && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0.5">
                  API v{webAPIVersion}
                </Badge>
              )}
              {libtorrentVersion && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0.5">
                  lt {libtorrentVersion}
                </Badge>
              )}
            </CardDescription>
          )}
          <CardDescription className="text-xs">
            <div className="flex items-center gap-1 min-w-0">
              <span
                className={`${incognitoMode ? "blur-sm select-none" : ""} truncate min-w-0`}
                style={incognitoMode ? { filter: "blur(8px)" } : {}}
                {...(!incognitoMode && { title: displayUrl })}
              >
                {displayUrl}
              </span>
              <Button
                variant="ghost"
                size="icon"
                // the icon stays small so the host line stays one line; the pseudo-element
                // carries the 44px tap target on phones, over non-interactive neighbours
                className={`${!isFirstLoad ? "h-4 w-4" : "h-5 w-5"} p-0 ${isFirstLoad ? "hover:bg-muted/50" : ""} shrink-0 relative max-sm:before:absolute max-sm:before:-inset-x-3.5 max-sm:before:-top-6 max-sm:before:-bottom-1 max-sm:before:content-['']`}
                onClick={(e) => {
                  if (isFirstLoad) {
                    e.preventDefault()
                    e.stopPropagation()
                  }
                  setIncognitoMode(!incognitoMode)
                }}
              >
                {incognitoMode ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
              </Button>
            </div>
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* Show loading or error state */}
          {(isFirstLoad || hasError || isDisconnected) ? (
            <div className="text-sm text-muted-foreground text-center">
              {isFirstLoad && <p className="animate-pulse">{t("instanceCard.loadingStats")}</p>}
              {hasError && !isDisconnected && <p>{t("instanceCard.failedToLoadStats")}</p>}
              <InstanceErrorDisplay instance={instance} compact />
            </div>
          ) : (
            /* Show normal stats */
            <div className="space-y-2 sm:space-y-3">
              <div className="mb-3 sm:mb-4">
                {/* Main stats row */}
                <div className="flex items-center justify-around text-center">
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.status?.downloading || 0}</div>
                    <div className="text-xs text-muted-foreground">{t("instanceCard.downloading")}</div>
                  </div>
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.status?.active || 0}</div>
                    <div className="text-xs text-muted-foreground">{t("instanceCard.active")}</div>
                  </div>
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.total || 0}</div>
                    <div className="text-xs text-muted-foreground">{t("instanceCard.total")}</div>
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-1 gap-1 sm:gap-2">
                {/* Issue rows - only shown when there are problems */}
                {TRACKER_HEALTH_ROWS.map(({ health, labelKey }) => {
                  const count = torrentCounts?.status?.[health] || 0
                  if (count === 0) return null
                  const { icon: Icon, iconClass, hoverClass } = TRACKER_HEALTH_DISPLAY[health]
                  return (
                    <Link
                      key={health}
                      to="/instances/$instanceId"
                      params={{ instanceId: instance.id.toString() }}
                      onClick={() => {
                        writeRaw("qui-filters-global", JSON.stringify({
                          status: [health],
                          excludeStatus: [],
                        }))
                      }}
                      className={cn("flex items-center gap-2 text-xs w-full rounded px-1 -mx-1 transition-colors", hoverClass)}
                    >
                      <Icon className={cn("h-3 w-3 flex-shrink-0", iconClass)} />
                      <span className={iconClass}>{t(labelKey)}</span>
                      <span className={cn("ml-auto font-medium", iconClass)}>{count}</span>
                    </Link>
                  )
                })}
                {(torrentCounts?.status?.errored || 0) > 0 && (
                  <Link
                    to="/instances/$instanceId"
                    params={{ instanceId: instance.id.toString() }}
                    onClick={() => {
                      writeRaw("qui-filters-global", JSON.stringify({
                        status: ["errored"],
                        excludeStatus: [],
                      }))
                    }}
                    className="flex items-center gap-2 text-xs w-full rounded px-1 -mx-1 hover:bg-destructive/10 transition-colors"
                  >
                    <AlertTriangle className="h-3 w-3 text-destructive flex-shrink-0" />
                    <span className="text-destructive">{t("instanceCard.errors")}</span>
                    <span className="ml-auto font-medium text-destructive">{torrentCounts?.status?.errored}</span>
                  </Link>
                )}

                <div className="flex items-center gap-2 text-xs">
                  <Download className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("instanceCard.download")}</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalDownloadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <Upload className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("instanceCard.upload")}</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalUploadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="text-muted-foreground cursor-help inline-flex items-center gap-1">
                        {t("instanceCard.totalSize")}
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      {t("instanceCard.totalSizeTooltip")}
                    </TooltipContent>
                  </Tooltip>
                  <DashboardTotalSize stats={stats} />
                </div>
              </div>

              {serverState?.free_space_on_disk !== undefined && (
                <div className="flex items-center gap-2 text-xs mt-1 sm:mt-2">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("instanceCard.freeSpace")}</span>
                  <span className="ml-auto font-medium truncate">{formatBytesOrFallback(serverState.free_space_on_disk, t("common:status.unknown"))}</span>
                </div>
              )}

              <Collapsible open={isAdvancedMetricsOpen} onOpenChange={setIsAdvancedMetricsOpen}>
                <CollapsibleTrigger className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors w-full">
                  {isAdvancedMetricsOpen ? (
                    <ChevronDown className="h-3 w-3" />
                  ) : (
                    <ChevronRight className="h-3 w-3" />
                  )}
                  <span>{isAdvancedMetricsOpen ? t("instanceCard.showLess") : t("instanceCard.showMore")}</span>
                </CollapsibleTrigger>
                <CollapsibleContent className="space-y-2 mt-2">
                  {uptimeSeconds !== null && (
                    <div className="flex items-center gap-2 text-xs">
                      <Clock className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.uptime")}</span>
                      <span className="ml-auto font-medium">{formatDuration(uptimeSeconds)}</span>
                    </div>
                  )}

                  {serverState?.total_peer_connections !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.peerConnections")}</span>
                      <span className="ml-auto font-medium">{serverState.total_peer_connections || 0}</span>
                    </div>
                  )}

                  {serverState?.queued_io_jobs !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.queuedIOJobs")}</span>
                      <span className="ml-auto font-medium">{serverState.queued_io_jobs || 0}</span>
                    </div>
                  )}

                  {serverState?.total_buffers_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <HardDrive className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.bufferSize")}</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_buffers_size)}</span>
                    </div>
                  )}

                  {serverState?.total_queued_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.totalQueued")}</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_queued_size)}</span>
                    </div>
                  )}

                  {serverState?.average_time_queue !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.avgQueueTime")}</span>
                      <span className="ml-auto font-medium">{serverState.average_time_queue}ms</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v4 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.externalIPv4")}</span>
                      <span className={`ml-auto font-medium font-mono ${incognitoMode ? "blur-sm select-none" : ""}`} style={incognitoMode ? { filter: "blur(8px)" } : {}}>{serverState.last_external_address_v4}</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v6 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("instanceCard.externalIPv6")}</span>
                      <span className={`ml-auto font-medium font-mono text-[10px] ${incognitoMode ? "blur-sm select-none" : ""}`} style={incognitoMode ? { filter: "blur(8px)" } : {}}>{serverState.last_external_address_v6}</span>
                    </div>
                  )}
                </CollapsibleContent>
              </Collapsible>

              <InstanceErrorDisplay instance={instance} compact />
            </div>
          )}

          {/* Version footer - always show if we have version info */}
        </CardContent>
      </Card>
    </>
  )
}

function MobileGlobalStatsCard({ globalStats }: { globalStats: GlobalStats }) {
  const { t } = useTranslation("dashboard")
  const [speedUnit] = useSpeedUnits()

  return (
    <Card className="sm:hidden px-4 py-3">
      <div className="grid grid-cols-2 gap-x-4 gap-y-3">
        {[
          { Icon: HardDrive, label: t("mobileOverview.instances"), value: `${globalStats.connected}/${globalStats.total}`, caption: t("mobileOverview.connected") },
          { Icon: Activity, label: t("mobileOverview.torrents"), value: String(globalStats.totalTorrents), caption: t("mobileOverview.activeCount", { count: globalStats.activeTorrents }) },
          { Icon: Download, label: t("mobileOverview.download"), value: formatSpeedWithUnit(globalStats.totalDownload, speedUnit), caption: t("mobileOverview.activeCount", { count: globalStats.downloadingTorrents }) },
          { Icon: Upload, label: t("mobileOverview.upload"), value: formatSpeedWithUnit(globalStats.totalUpload, speedUnit), caption: t("mobileOverview.activeCount", { count: globalStats.seedingTorrents }) },
        ].map(({ Icon, label, value, caption }) => (
          <div key={label} className="min-w-0">
            <div className="flex items-center gap-1.5">
              <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
              <span className="truncate text-xs text-muted-foreground">{label}</span>
            </div>
            <div className="flex items-baseline gap-1.5">
              <span className="shrink-0 whitespace-nowrap text-lg font-bold tabular-nums">{value}</span>
              <span className="truncate text-[10px] text-muted-foreground">{caption}</span>
            </div>
          </div>
        ))}
      </div>
    </Card>
  )
}

type GlobalStats = ReturnType<typeof useGlobalStats>

function GlobalStatsCards({ globalStats }: { globalStats: GlobalStats }) {
  const { t } = useTranslation("dashboard")
  const [speedUnit] = useSpeedUnits()

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("globalStats.instances")}</CardTitle>
          <HardDrive className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.connected}/{globalStats.total}</div>
          <p className="text-xs text-muted-foreground">
            {t("globalStats.connectedInstances")}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("globalStats.totalTorrents")}</CardTitle>
          <Activity className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.totalTorrents}</div>
          <p className="text-xs text-muted-foreground">
            {t("globalStats.activeTotal", { active: globalStats.activeTorrents, size: formatBytes(globalStats.totalSize) })}
            {globalStats.sizeCalculating && <> <SizeCalculatingMark /></>}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("globalStats.totalDownload")}</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalDownload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {t("globalStats.downloadingActive", { count: globalStats.downloadingTorrents, size: formatBytes(globalStats.totalRemainingSize) })}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("globalStats.totalUpload")}</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalUpload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {t("globalStats.seedingActive", { count: globalStats.seedingTorrents, size: formatBytes(globalStats.totalSeedingSize) })}
            {globalStats.sizeCalculating && <> <SizeCalculatingMark /></>}
          </p>
        </CardContent>
      </Card>
    </>
  )
}

interface GlobalAllTimeStatsProps {
  statsData: DashboardInstanceStats[]
  isCollapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
}

type DrawerMetric = { label: string; value: string; color?: string }

// shared by the tracker and server-stats detail drawers
function MetricGrid({ metrics, className }: { metrics: DrawerMetric[]; className?: string }) {
  return (
    <div className={`grid grid-cols-2 gap-x-4 gap-y-3 px-4 ${className}`}>
      {metrics.map(({ label, value, color }) => (
        <div key={label} className="flex items-baseline justify-between gap-2 border-b pb-1.5">
          <span className="truncate text-xs text-muted-foreground">{label}</span>
          <span className="shrink-0 text-sm font-semibold tabular-nums" style={color ? { color } : undefined}>{value}</span>
        </div>
      ))}
    </div>
  )
}

const alltimeRatio = (serverState: ServerState | null) =>
  serverState?.alltime_dl ? (serverState.alltime_ul || 0) / serverState.alltime_dl : 0

function GlobalAllTimeStats({ statsData, isCollapsed, onCollapsedChange }: GlobalAllTimeStatsProps) {
  const { t } = useTranslation("dashboard")
  // Accordion value is "server-stats" when expanded, "" when collapsed
  const accordionValue = isCollapsed ? "" : "server-stats"
  const setAccordionValue = (value: string) => onCollapsedChange(value === "")
  // mobile detail drawer, keyed by id so the numbers stay live while it is open
  const [detailsInstanceId, setDetailsInstanceId] = useState<number | null>(null)

  const globalStats = useMemo(() => {
    // Calculate server stats
    const alltimeDl = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.alltime_dl || 0), 0)
    const alltimeUl = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.alltime_ul || 0), 0)
    const totalPeers = statsData.reduce((sum, { serverState }) =>
      sum + (serverState?.total_peer_connections || 0), 0)

    // Calculate global ratio
    let globalRatio = 0
    if (alltimeDl > 0) {
      globalRatio = alltimeUl / alltimeDl
    }

    return {
      alltimeDl,
      alltimeUl,
      globalRatio,
      totalPeers,
    }
  }, [statsData])

  // Apply color grading to ratio
  const ratioColor = getRatioColor(globalStats.globalRatio)

  const reportingInstances = statsData.filter(({ serverState }) => serverState?.alltime_dl || serverState?.alltime_ul)
  const detailsInstance = reportingInstances.find(({ instance }) => instance.id === detailsInstanceId)

  // Don't show if no data
  if (globalStats.alltimeDl === 0 && globalStats.alltimeUl === 0) {
    return null
  }

  return (
    <Accordion type="single" collapsible className="rounded-lg border bg-card" value={accordionValue} onValueChange={setAccordionValue}>
      <AccordionItem value="server-stats" className="border-0">
        <AccordionTrigger className="px-4 py-4 hover:no-underline hover:bg-muted/50 transition-colors [&>svg]:hidden group">
          {/* Mobile layout */}
          <div className="sm:hidden w-full">
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <Plus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=open]:hidden" />
                <Minus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=closed]:hidden" />
                <h3 className="text-sm font-medium text-muted-foreground">{t("serverStats.title")}</h3>
              </div>
            </div>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-4">
                <div className="flex items-center gap-1.5">
                  <ChevronDown className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-sm font-semibold">{formatBytes(globalStats.alltimeDl)}</span>
                </div>
                <div className="flex items-center gap-1.5">
                  <ChevronUp className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-sm font-semibold">{formatBytes(globalStats.alltimeUl)}</span>
                </div>
              </div>
              <div className="flex items-center gap-4 text-sm">
                <div>
                  <span className="text-xs text-muted-foreground">{t("serverStats.ratio")} </span>
                  <span className="font-semibold" style={{ color: ratioColor }}>
                    {globalStats.globalRatio.toFixed(2)}
                  </span>
                </div>
                {/* peers omitted: a fourth value wraps the summary onto a second line */}
              </div>
            </div>
          </div>

          {/* Desktop layout */}
          <div className="hidden sm:flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 w-full">
            <div className="flex items-center gap-2">
              <Plus className="h-4 w-4 text-muted-foreground group-data-[state=open]:hidden" />
              <Minus className="h-4 w-4 text-muted-foreground group-data-[state=closed]:hidden" />
              <h3 className="text-base font-medium">{t("serverStats.title")}</h3>
            </div>
            <div className="flex flex-wrap items-center gap-6 text-sm">
              <div className="flex items-center gap-2">
                <ChevronDown className="h-4 w-4 text-muted-foreground" />
                <span className="text-lg font-semibold">{formatBytes(globalStats.alltimeDl)}</span>
              </div>

              <div className="flex items-center gap-2">
                <ChevronUp className="h-4 w-4 text-muted-foreground" />
                <span className="text-lg font-semibold">{formatBytes(globalStats.alltimeUl)}</span>
              </div>

              <div className="flex items-center gap-2">
                <span className="text-muted-foreground">{t("serverStats.ratio")}</span>
                <span className="text-lg font-semibold" style={{ color: ratioColor }}>
                  {globalStats.globalRatio.toFixed(2)}
                </span>
              </div>

              {globalStats.totalPeers > 0 && (
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">{t("serverStats.peers")}</span>
                  <span className="text-lg font-semibold tabular-nums inline-block min-w-[3rem] text-right">
                    {globalStats.totalPeers}
                  </span>
                </div>
              )}
            </div>
          </div>
        </AccordionTrigger>
        <AccordionContent className="px-0 pb-0">
          {/* Mobile row list: the desktop table is six columns wide and only two of them
              fit on a phone, so it scrolls sideways with nothing to say that it does */}
          <div className="sm:hidden divide-y">
            {reportingInstances.map(({ instance, serverState }) => {
              const instanceRatio = alltimeRatio(serverState)

              return (
                <button
                  key={instance.id}
                  type="button"
                  onClick={() => setDetailsInstanceId(instance.id)}
                  className="flex w-full items-center gap-2 px-4 py-2 text-left"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate text-sm font-medium">{instance.name}</div>
                    <div className="flex items-center gap-2 text-xs text-muted-foreground tabular-nums">
                      <span className="flex shrink-0 items-center gap-0.5">
                        <ChevronDown className="h-3 w-3" />{formatBytes(serverState?.alltime_dl || 0)}
                      </span>
                      <span className="flex shrink-0 items-center gap-0.5">
                        <ChevronUp className="h-3 w-3" />{formatBytes(serverState?.alltime_ul || 0)}
                      </span>
                      <span className="shrink-0" style={{ color: getRatioColor(instanceRatio) }}>
                        {instanceRatio.toFixed(2)}
                      </span>
                    </div>
                  </div>
                  <MoreVertical className="h-4 w-4 shrink-0 text-muted-foreground" />
                </button>
              )
            })}
          </div>

          <Table className="hidden sm:table">
            <TableHeader>
              <TableRow className="bg-muted/50">
                <TableHead className="text-center">{t("serverStats.tableHeaders.instance")}</TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("serverStats.tableHeaders.downloaded")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("serverStats.tableHeaders.downloadedSession")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("serverStats.tableHeaders.uploaded")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("serverStats.tableHeaders.uploadedSession")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">{t("serverStats.tableHeaders.ratio")}</TableHead>
                <TableHead className="text-center">{t("serverStats.tableHeaders.peers")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {reportingInstances.map(({ instance, serverState }) => {
                const instanceRatio = alltimeRatio(serverState)
                const instanceRatioColor = getRatioColor(instanceRatio)

                return (
                  <TableRow key={instance.id}>
                    <TableCell className="text-center font-medium">{instance.name}</TableCell>
                    <TableCell className="text-center font-semibold">
                      {formatBytes(serverState?.alltime_dl || 0)}
                    </TableCell>
                    <TableCell className="text-center font-semibold">
                      {formatBytes(serverState?.dl_info_data || 0)}
                    </TableCell>
                    <TableCell className="text-center font-semibold">
                      {formatBytes(serverState?.alltime_ul || 0)}
                    </TableCell>
                    <TableCell className="text-center font-semibold">
                      {formatBytes(serverState?.up_info_data || 0)}
                    </TableCell>
                    <TableCell className="text-center font-semibold" style={{ color: instanceRatioColor }}>
                      {instanceRatio.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-center font-semibold">
                      {serverState?.total_peer_connections ?? "-"}
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        </AccordionContent>
      </AccordionItem>

      {/* Mobile detail drawer: the columns the row has no room for */}
      <Drawer open={Boolean(detailsInstance)} onOpenChange={(open) => !open && setDetailsInstanceId(null)}>
        <DrawerContent>
          {detailsInstance && (() => {
            const { instance, serverState } = detailsInstance
            const instanceRatio = alltimeRatio(serverState)
            const metrics: DrawerMetric[] = [
              { label: t("serverStats.tableHeaders.downloaded"), value: formatBytes(serverState?.alltime_dl || 0) },
              { label: t("serverStats.tableHeaders.downloadedSession"), value: formatBytes(serverState?.dl_info_data || 0) },
              { label: t("serverStats.tableHeaders.uploaded"), value: formatBytes(serverState?.alltime_ul || 0) },
              { label: t("serverStats.tableHeaders.uploadedSession"), value: formatBytes(serverState?.up_info_data || 0) },
              { label: t("serverStats.tableHeaders.ratio"), value: instanceRatio.toFixed(2), color: getRatioColor(instanceRatio) },
              { label: t("serverStats.tableHeaders.peers"), value: serverState?.total_peer_connections !== undefined ? String(serverState.total_peer_connections || 0) : "-" },
            ]

            return (
              <>
                <DrawerHeader className="text-left">
                  <DrawerTitle className="truncate">{instance.name}</DrawerTitle>
                  <DrawerDescription className="sr-only">{t("serverStats.title")}</DrawerDescription>
                </DrawerHeader>
                <MetricGrid metrics={metrics} className="pb-6" />
              </>
            )
          })()}
        </DrawerContent>
      </Drawer>
    </Accordion>
  )
}

function QuickActionsDropdown({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const { t } = useTranslation("dashboard")
  const connectedInstances = statsData
    .filter(({ instance }) => instance?.connected)
    .map(({ instance }) => instance)

  if (connectedInstances.length === 0) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="h-11 w-full sm:h-8 sm:w-auto">
          <Plus className="h-4 w-4 mr-2" />
          {t("quickActions.addTorrent")}
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>{t("quickActions.addTorrent")}</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {connectedInstances.map(instance => (
          <Link
            key={instance.id}
            to="/instances/$instanceId"
            params={{ instanceId: instance.id.toString() }}
            search={{ modal: "add-torrent" }}
          >
            <DropdownMenuItem className="cursor-pointer active:bg-accent focus:bg-accent">
              <Plus className="h-4 w-4 mr-2" />
              <span>{t("quickActions.addTo", { name: instance.name })}</span>
            </DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Dashboard() {
  const { t } = useTranslation("dashboard")
  const { instances, isLoading } = useInstances()
  const allInstances = useMemo(
    () => instances ?? [],
    [instances]
  )
  const activeInstances = useMemo(
    () => allInstances.filter(instance => instance.isActive),
    [allInstances]
  )
  const hasInstances = allInstances.length > 0
  const hasActiveInstances = activeInstances.length > 0
  const [isAdvancedMetricsOpen, setIsAdvancedMetricsOpen] = useState(false)
  const { isHiddenDelayed } = useDelayedVisibility(3000)
  const [titleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)

  // Dashboard settings
  const { data: dashboardSettings } = useDashboardSettings()
  const updateSettings = useUpdateDashboardSettings()
  const settings = dashboardSettings || DEFAULT_DASHBOARD_SETTINGS

  // Use safe hook that always calls the same number of hooks
  // Query handoff: when the dashboard is visible we run the full SSE-backed
  // stats stream; after the delayed hide, we stop the heavy stream and only
  // poll transfer info for title bar speeds.
  const statsData = useAllInstanceStats(activeInstances, { enabled: !isHiddenDelayed })
  const globalStats = useGlobalStats(statsData)
  const transferInfoQueries = useQueries({
    queries: activeInstances.map(instance => ({
      queryKey: ["transfer-info", instance.id],
      queryFn: () => api.getTransferInfo(instance.id),
      enabled: titleBarSpeedsEnabled && isHiddenDelayed && hasActiveInstances,
      refetchInterval: 3000,
      refetchIntervalInBackground: true,
      staleTime: 0,
    })),
  })
  const backgroundSpeedsState = transferInfoQueries.reduce(
    (state, query) => {
      const info = query.data
      if (!info) {
        return state
      }
      return {
        hasData: true,
        dl: state.dl + (info.dl_info_speed ?? 0),
        up: state.up + (info.up_info_speed ?? 0),
      }
    },
    { dl: 0, up: 0, hasData: false }
  )
  const backgroundSpeeds = backgroundSpeedsState.hasData ? { dl: backgroundSpeedsState.dl, up: backgroundSpeedsState.up } : undefined
  useTitleBarSpeeds({
    mode: "dashboard",
    enabled: titleBarSpeedsEnabled && hasActiveInstances,
    foregroundSpeeds: hasActiveInstances ? {
      dl: globalStats.totalDownload ?? 0,
      up: globalStats.totalUpload ?? 0,
    } : undefined,
    backgroundSpeeds: isHiddenDelayed && hasActiveInstances ? backgroundSpeeds : undefined,
  })


  // Handler for section collapsed state changes
  const handleSectionCollapsedChange = (sectionId: string, collapsed: boolean) => {
    updateSettings.mutate({
      sectionCollapsed: { ...settings.sectionCollapsed, [sectionId]: collapsed },
    })
  }

  // Check if a section is visible
  const isSectionVisible = (sectionId: string) => {
    return settings.sectionVisibility[sectionId] !== false
  }

  // Get ordered section IDs that are visible
  const visibleSections = settings.sectionOrder.filter(id => isSectionVisible(id))

  if (isLoading) {
    return (
      <div className="p-4 lg:p-6 pb-16">
        <div className="animate-pulse space-y-4">
          <div className="h-8 bg-muted rounded w-48"></div>
          <div className="h-4 bg-muted rounded w-64"></div>
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-24 bg-muted rounded"></div>
            ))}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="p-4 lg:p-6 pb-16">
      <div className="mb-6">
        <PageHeader
          title={t("title")}
          description={t("description")}
          actions={instances && instances.length > 0 && (
            <>
              {/* on phones the two rare actions ride the title row, which is otherwise dead
                  space, so only the primary action costs a row of its own below */}
              <div className="flex items-center gap-1 sm:hidden">
                <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>
                  <Button variant="outline" size="icon" className="size-11" aria-label={t("addInstance")}>
                    <HardDrive className="h-4 w-4" />
                  </Button>
                </Link>
                <DashboardSettingsDialog iconOnly />
              </div>
              <QuickActionsDropdown statsData={statsData} />
              <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }} className="hidden sm:block">
                <Button variant="outline" size="sm">
                  <HardDrive className="h-4 w-4 mr-2" />
                  {t("addInstance")}
                </Button>
              </Link>
              <div className="hidden sm:block">
                <DashboardSettingsDialog />
              </div>
            </>
          )}
        />
      </div>

      {/* Show banner if any instances have decryption errors */}
      <PasswordIssuesBanner instances={instances || []} />

      {/* Show banner to register as magnet handler (Firefox support) */}
      <MagnetHandlerBanner />

      {hasInstances ? (
        <div className="space-y-6">
          {hasActiveInstances ? (
            <>
              {visibleSections.map((sectionId) => {
                switch (sectionId) {
                  case "server-stats":
                    return (
                      <GlobalAllTimeStats
                        key={sectionId}
                        statsData={statsData}
                        isCollapsed={settings.sectionCollapsed["server-stats"] ?? false}
                        onCollapsedChange={(collapsed) => handleSectionCollapsedChange("server-stats", collapsed)}
                      />
                    )
                  case "global-stats":
                    return (
                      <div key={sectionId} className="space-y-4">
                        {/* Mobile: Single combined card */}
                        <MobileGlobalStatsCard globalStats={globalStats} />
                        {/* Tablet/Desktop: Separate cards */}
                        <div className="hidden sm:grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                          <GlobalStatsCards globalStats={globalStats} />
                        </div>
                      </div>
                    )
                  case "instances":
                    return (
                      <div key={sectionId}>
                        {/* Responsive layout so each instance mounts once */}
                        <div className="flex flex-col gap-4 sm:grid sm:grid-cols-1 md:grid-cols-2 lg:grid-cols-3 2xl:grid-cols-4">
                          {statsData.map(instanceData => (
                            <InstanceCard
                              key={instanceData.instance.id}
                              instanceData={instanceData}
                              isAdvancedMetricsOpen={isAdvancedMetricsOpen}
                              setIsAdvancedMetricsOpen={setIsAdvancedMetricsOpen}
                            />
                          ))}
                        </div>
                      </div>
                    )
                  default:
                    return null
                }
              })}
            </>
          ) : (
            <Card className="p-8 text-center">
              <div className="space-y-3">
                <h3 className="text-lg font-semibold">{t("emptyState.allDisabled")}</h3>
                <p className="text-muted-foreground">
                  {t("emptyState.enableInstance")}
                </p>
                <Link to="/settings" search={{ tab: "instances" as const }}>
                  <Button variant="outline" size="sm">
                    {t("emptyState.manageInstances")}
                  </Button>
                </Link>
              </div>
            </Card>
          )}
        </div>
      ) : (
        <Card className="p-8 sm:p-12 text-center">
          <div className="space-y-4">
            <HardDrive className="h-12 w-12 mx-auto text-muted-foreground" />
            <div>
              <h3 className="text-lg font-semibold">{t("emptyState.noInstances")}</h3>
              <p className="text-muted-foreground">{t("emptyState.getStarted")}</p>
            </div>
            <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>
              <Button>
                <HardDrive className="h-4 w-4 mr-2" />
                {t("addInstance")}
              </Button>
            </Link>
          </div>
          <MigrationHint className="mt-10 sm:mt-12" />
        </Card>
      )}
    </div>
  )
}
