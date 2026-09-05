/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import { InstanceSettingsButton } from "@/components/instances/InstanceSettingsButton"
import { MagnetHandlerBanner } from "@/components/MagnetHandlerBanner"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
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
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle
} from "@/components/ui/drawer"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
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
  mergeDashboardInstanceMeta,
  mergeDashboardStatsSnapshot,
  resolveDashboardStreamError,
  resolveDashboardTorrentCounts,
  shouldUseDashboardStatsFallback
} from "@/lib/dashboard-stream"
import { copyTextToClipboard, formatBytes, formatBytesOrFallback, formatDuration, getRatioColor } from "@/lib/utils"
import type {
  CacheMetadata,
  DashboardSettings,
  InstanceMeta,
  InstanceResponse,
  QBittorrentAppInfo,
  ServerState,
  TorrentResponse,
  TorrentCounts,
  TorrentStats,
  TrackerCustomization,
  TrackerTransferStats
} from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Activity, AlertCircle, AlertTriangle, ArrowDown, ArrowUp, ArrowUpDown, Ban, BrickWallFire, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Clock, Database, Download, ExternalLink, Eye, EyeOff, Globe, HardDrive, Info, Link2, Minus, MoreVertical, Pencil, Plus, Rabbit, RefreshCcw, Trash2, Turtle, Upload, X, Zap } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"

import { DashboardSettingsDialog } from "@/components/dashboard-settings-dialog"
import { createStreamKey, useSyncStreamManager } from "@/contexts/SyncStreamContext"
import { DEFAULT_DASHBOARD_SETTINGS, useDashboardSettings, useUpdateDashboardSettings } from "@/hooks/useDashboardSettings"
import { useCreateTrackerCustomization, useDeleteTrackerCustomization, useTrackerCustomizations, useUpdateTrackerCustomization } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
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
  cacheMetadata: CacheMetadata | null | undefined
  instanceMeta: InstanceMeta | null  // Real-time instance health from SSE
}

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

function hasDashboardInstanceData(data: InstanceStreamData | null | undefined) {
  return hasDashboardStatsPayload(data)
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
    isLoading: current.isLoading && !hasDashboardInstanceData(cached),
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
    const totalSize = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalSize || 0), 0)
    const totalRemainingSize = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalRemainingSize || 0), 0)
    const totalSeedingSize = statsData.reduce((sum, { stats }) =>
      sum + (stats?.totalSeedingSize || 0), 0)
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
function useAllInstanceStats(instances: InstanceResponse[], options: { enabled: boolean }): DashboardInstanceStats[] {
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
        }),
        enabled: fallbackEnabled,
        refetchInterval: fallbackEnabled ? 5000 : false,
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
              cacheMetadata: data.cacheMetadata ?? current.cacheMetadata,
              instanceMeta: data.instanceMeta ?? current.instanceMeta,
            }

            return {
              data: next,
              immediate: current.isLoading && !next.isLoading,
            }
          })
        })

        const unsubscribe = syncStream.subscribe(streamKey, snapshot => {
          if (cancelRef.current) {
            return
          }

          const streamUsable =
            snapshot.connected &&
            snapshot.initialized &&
            !snapshot.dataStalled &&
            !snapshot.error

          applyInstanceData(instance.id, current => {
            const next: InstanceStreamData = {
              ...current,
              streamConnected: streamUsable,
              streamError: snapshot.error ?? (streamUsable ? null : current.streamError),
              hasLiveDashboardStatsPayload: streamUsable &&
                current.hasLiveDashboardStatsPayload,
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
        })

        const initialSnapshot = syncStream.getState(streamKey)
        if (initialSnapshot) {
          const streamUsable =
            initialSnapshot.connected &&
            initialSnapshot.initialized &&
            !initialSnapshot.dataStalled &&
            !initialSnapshot.error
          applyInstanceData(instance.id, current => {
            const next: InstanceStreamData = {
              ...current,
              streamConnected: streamUsable,
              streamError: initialSnapshot.error ?? current.streamError,
              hasLiveDashboardStatsPayload: streamUsable &&
                current.hasLiveDashboardStatsPayload,
            }

            if (initialSnapshot.error) {
              next.error = initialSnapshot.error
              next.isLoading = false
            }

            if (!streamUsable) {
              next.instanceMeta = null
            }

            return next
          })
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
      streamError: resolveDashboardStreamError(state.streamError, isFallbackActive, fallbackData),
      hasLiveDashboardStatsPayload: state.hasLiveDashboardStatsPayload,
      isDashboardStatsFallbackActive: isFallbackActive,
      cacheMetadata,
      instanceMeta: state.instanceMeta,
    }
  })
}


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
  const { preferences } = useInstancePreferences(instance.id, { enabled: instance.connected })
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

  const listenPort = preferences?.listen_port
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
        <CardHeader className={`${!isFirstLoad ? "gap-1" : ""} overflow-hidden`}>
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
              {instance.hasLocalFilesystemAccess && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <HardDrive className="h-4 w-4 text-primary" />
                  </TooltipTrigger>
                  <TooltipContent>
                    {t("instanceCard.localFileAccess")}
                  </TooltipContent>
                </Tooltip>
              )}
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
                {(torrentCounts?.status?.unregistered || 0) > 0 && (
                  <Link
                    to="/instances/$instanceId"
                    params={{ instanceId: instance.id.toString() }}
                    onClick={() => {
                      writeRaw("qui-filters-global", JSON.stringify({
                        status: ["unregistered"],
                        excludeStatus: [],
                      }))
                    }}
                    className="flex items-center gap-2 text-xs w-full rounded px-1 -mx-1 hover:bg-destructive/10 transition-colors"
                  >
                    <AlertTriangle className="h-3 w-3 text-destructive flex-shrink-0" />
                    <span className="text-destructive">{t("instanceCard.unregisteredTorrents")}</span>
                    <span className="ml-auto font-medium text-destructive">{torrentCounts?.status?.unregistered}</span>
                  </Link>
                )}
                {(torrentCounts?.status?.tracker_down || 0) > 0 && (
                  <Link
                    to="/instances/$instanceId"
                    params={{ instanceId: instance.id.toString() }}
                    onClick={() => {
                      writeRaw("qui-filters-global", JSON.stringify({
                        status: ["tracker_down"],
                        excludeStatus: [],
                      }))
                    }}
                    className="flex items-center gap-2 text-xs w-full rounded px-1 -mx-1 hover:bg-yellow-500/10 transition-colors"
                  >
                    <AlertCircle className="h-3 w-3 text-yellow-500 flex-shrink-0" />
                    <span className="text-yellow-500">{t("instanceCard.trackerDown")}</span>
                    <span className="ml-auto font-medium text-yellow-500">{torrentCounts?.status?.tracker_down}</span>
                  </Link>
                )}
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
                  <span className="ml-auto font-medium truncate">{formatBytes(stats?.totalSize || 0)}</span>
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


type TrackerSortColumn = "tracker" | "uploaded" | "downloaded" | "uploadedSession" | "downloadedSession" | "ratio" | "buffer" | "count" | "size" | "performance"
type SortDirection = "asc" | "desc"

// Helper to compute ratio display values for tracker stats
function getTrackerRatioDisplay(uploaded: number, downloaded: number): { isInfinite: boolean; ratio: number; color: string } {
  const isInfinite = downloaded === 0 && uploaded > 0
  const ratio = downloaded > 0 ? uploaded / downloaded : 0
  const color = isInfinite ? "var(--chart-1)" : getRatioColor(ratio)
  return { isInfinite, ratio, color }
}

function SortIcon({ column, sortColumn, sortDirection }: { column: TrackerSortColumn; sortColumn: TrackerSortColumn; sortDirection: SortDirection }) {
  if (sortColumn !== column) {
    return <ArrowUpDown className="h-3 w-3 text-muted-foreground/50" />
  }
  return sortDirection === "asc"? <ArrowUp className="h-3 w-3" />: <ArrowDown className="h-3 w-3" />
}

// Extended tracker stats with customization support
interface ProcessedTrackerStats extends TrackerTransferStats {
  domain: string
  displayName: string
  originalDomains: string[]
  customizationId?: number
}

interface TrackerBreakdownCardProps {
  statsData: DashboardInstanceStats[]
  settings: DashboardSettings
  onSettingsChange: (input: { trackerBreakdownSortColumn?: string; trackerBreakdownSortDirection?: string; trackerBreakdownItemsPerPage?: number }) => void
  isCollapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
}

function TrackerBreakdownCard({ statsData, settings, onSettingsChange, isCollapsed, onCollapsedChange }: TrackerBreakdownCardProps) {
  const { t } = useTranslation("dashboard")
  // Accordion value is "tracker-breakdown" when expanded, "" when collapsed
  const accordionValue = isCollapsed ? "" : "tracker-breakdown"
  const setAccordionValue = (value: string) => onCollapsedChange(value === "")
  const { data: trackerIcons } = useTrackerIcons()
  const [incognitoMode] = useIncognitoMode()

  // Use settings directly - React Query handles optimistic updates
  const sortColumn = (settings.trackerBreakdownSortColumn as TrackerSortColumn) || "uploaded"
  const sortDirection = (settings.trackerBreakdownSortDirection as SortDirection) || "desc"

  // Tracker customizations
  const { data: customizations } = useTrackerCustomizations()
  const createCustomization = useCreateTrackerCustomization()
  const updateCustomization = useUpdateTrackerCustomization()
  const deleteCustomization = useDeleteTrackerCustomization()

  // Selection state for merging/renaming
  const [selectedDomains, setSelectedDomains] = useState<Set<string>>(new Set())
  const [selectedGroupId, setSelectedGroupId] = useState<number | null>(null)
  const [showCustomizeDialog, setShowCustomizeDialog] = useState(false)
  const [customizeDisplayName, setCustomizeDisplayName] = useState("")
  const [editingCustomization, setEditingCustomization] = useState<{ id: number; domains: string[]; includedInStats: string[] } | null>(null)
  const [includedInStats, setIncludedInStats] = useState<Set<string>>(new Set())

  // Import/Export state
  const [showImportDialog, setShowImportDialog] = useState(false)
  const [importJson, setImportJson] = useState("")
  const [importConflicts, setImportConflicts] = useState<Map<number, "skip" | "overwrite">>(new Map())

  const trackerStats = useMemo(() => {
    // aggregate tracker transfer stats across all instances
    const aggregated = new Map<string, TrackerTransferStats>()

    for (const { torrentCounts } of statsData) {
      if (!torrentCounts?.trackerTransfers) continue
      for (const [domain, stats] of Object.entries(torrentCounts.trackerTransfers)) {
        const existing = aggregated.get(domain)
        if (existing) {
          existing.uploaded += stats.uploaded
          existing.downloaded += stats.downloaded
          existing.uploadedSession += stats.uploadedSession
          existing.downloadedSession += stats.downloadedSession
          existing.totalSize += stats.totalSize
          existing.count += stats.count
        } else {
          aggregated.set(domain, { ...stats })
        }
      }
    }

    // Build domain -> customization mapping
    const domainToCustomization = new Map<string, TrackerCustomization>()
    for (const custom of customizations ?? []) {
      for (const domain of custom.domains) {
        domainToCustomization.set(domain.toLowerCase(), custom)
      }
    }

    // Apply customizations: sum non-excluded domains, use display names
    // Two-pass approach to handle domains appearing in any order in the aggregated map
    const processed = new Map<string, ProcessedTrackerStats>()

    // Pass 1: Create entries for primary domains and standalone domains
    for (const [domain, stats] of aggregated) {
      const customization = domainToCustomization.get(domain.toLowerCase())

      if (customization) {
        const isPrimary = customization.domains[0]?.toLowerCase() === domain.toLowerCase()
        if (isPrimary) {
          processed.set(customization.displayName, {
            ...stats,
            domain,
            displayName: customization.displayName,
            originalDomains: customization.domains,
            customizationId: customization.id,
          })
        }
      } else {
        // No customization - use domain as-is
        processed.set(domain, {
          ...stats,
          domain,
          displayName: domain,
          originalDomains: [domain],
        })
      }
    }

    // Pass 2: Add stats from explicitly included secondary domains
    for (const [domain, stats] of aggregated) {
      const customization = domainToCustomization.get(domain.toLowerCase())

      if (customization) {
        const isPrimary = customization.domains[0]?.toLowerCase() === domain.toLowerCase()
        const isIncluded = customization.includedInStats?.some(
          d => d.toLowerCase() === domain.toLowerCase()
        )

        // Secondary domains only contribute if explicitly in includedInStats
        if (!isPrimary && isIncluded) {
          const existing = processed.get(customization.displayName)
          if (existing) {
            existing.uploaded += stats.uploaded
            existing.downloaded += stats.downloaded
            existing.uploadedSession += stats.uploadedSession
            existing.downloadedSession += stats.downloadedSession
            existing.totalSize += stats.totalSize
            existing.count += stats.count
            continue
          }

          processed.set(customization.displayName, {
            ...stats,
            domain: customization.domains[0] ?? domain,
            displayName: customization.displayName,
            originalDomains: customization.domains,
            customizationId: customization.id,
          })
        }
      }
    }

    // Pass 3: Ensure merged groups remain visible even if the primary domain has no torrents.
    // If no primary/included domain produced a group entry, fall back to whichever domain in the group
    // currently has stats (pick the one with the highest torrent count to avoid double-counting).
    const fallbackByDisplayName = new Map<string, { customization: TrackerCustomization; stats: TrackerTransferStats; domain: string }>()
    for (const [domain, stats] of aggregated) {
      const customization = domainToCustomization.get(domain.toLowerCase())
      if (!customization) continue
      if (processed.has(customization.displayName)) continue

      const existing = fallbackByDisplayName.get(customization.displayName)
      if (
        !existing ||
        stats.count > existing.stats.count ||
        (stats.count === existing.stats.count && stats.uploaded > existing.stats.uploaded)
      ) {
        fallbackByDisplayName.set(customization.displayName, { customization, stats, domain })
      }
    }

    for (const { customization, stats, domain } of fallbackByDisplayName.values()) {
      processed.set(customization.displayName, {
        ...stats,
        domain: customization.domains[0] ?? domain,
        displayName: customization.displayName,
        originalDomains: customization.domains,
        customizationId: customization.id,
      })
    }

    return Array.from(processed.values())
  }, [statsData, customizations])

  // sort the tracker stats based on current sort state
  const sortedTrackerStats = useMemo(() => {
    const sorted = [...trackerStats]
    const multiplier = sortDirection === "asc" ? 1 : -1

    sorted.sort((a, b) => {
      switch (sortColumn) {
        case "tracker":
          return multiplier * a.displayName.localeCompare(b.displayName)
        case "uploaded":
          return multiplier * (a.uploaded - b.uploaded)
        case "downloaded":
          return multiplier * (a.downloaded - b.downloaded)
        case "uploadedSession":
          return multiplier * (a.uploadedSession - b.uploadedSession)
        case "downloadedSession":
          return multiplier * (a.downloadedSession - b.downloadedSession)
        case "ratio": {
          const ratioA = a.downloaded > 0 ? a.uploaded / a.downloaded : (a.uploaded > 0 ? Infinity : 0)
          const ratioB = b.downloaded > 0 ? b.uploaded / b.downloaded : (b.uploaded > 0 ? Infinity : 0)
          return multiplier * (ratioA - ratioB)
        }
        case "buffer":
          return multiplier * ((a.uploaded - a.downloaded) - (b.uploaded - b.downloaded))
        case "count":
          return multiplier * (a.count - b.count)
        case "size":
          return multiplier * (a.totalSize - b.totalSize)
        case "performance": {
          // efficiency = uploaded / totalSize (how many times content has been seeded)
          const perfA = a.totalSize > 0 ? a.uploaded / a.totalSize : 0
          const perfB = b.totalSize > 0 ? b.uploaded / b.totalSize : 0
          return multiplier * (perfA - perfB)
        }
        default:
          return 0
      }
    })

    return sorted
  }, [trackerStats, sortColumn, sortDirection])

  // Calculate total uploaded for percentage display
  const totalUploaded = useMemo(() => {
    return trackerStats.reduce((sum, t) => sum + t.uploaded, 0)
  }, [trackerStats])

  // Calculate total session uploaded for percentage display
  const totalUploadedSession = useMemo(() => {
    return trackerStats.reduce((sum, t) => sum + t.uploadedSession, 0)
  }, [trackerStats])

  // Selection handlers
  const toggleSelection = (domain: string) => {
    setSelectedDomains(prev => {
      const next = new Set(prev)
      if (next.has(domain)) {
        next.delete(domain)
      } else {
        next.add(domain)
      }
      return next
    })
  }

  const toggleGroupSelection = (customizationId: number) => {
    setSelectedGroupId(prev => prev === customizationId ? null : customizationId)
  }

  const clearSelection = () => {
    setSelectedDomains(new Set())
    setSelectedGroupId(null)
  }

  // Merge into a group
  const handleMergeIntoGroup = (targetGroupId: number, domain?: string) => {
    const group = customizations?.find(c => c.id === targetGroupId)
    if (!group) return

    const domainsSet = new Set(selectedDomains)
    if (domain) domainsSet.add(domain) // no-op if already present
    const domainsToMerge = Array.from(domainsSet)

    if (domainsToMerge.length === 0) return

    // Merge into selected group
    const mergedDomains = [...new Set([...group.domains, ...domainsToMerge])]
    updateCustomization.mutate({
      id: targetGroupId,
      data: {
        displayName: group.displayName,
        domains: mergedDomains,
        includedInStats: group.includedInStats ?? [],
      },
    }, {
      onSuccess: () => {
        clearSelection()
      },
    })
  }

  // Save customization (create or update)
  const handleSaveCustomization = () => {
    if (!customizeDisplayName.trim()) return

    const domains = editingCustomization? editingCustomization.domains: Array.from(selectedDomains)

    if (domains.length === 0) return

    // Get included domains from state (secondary domains that contribute to stats)
    const included = editingCustomization? editingCustomization.includedInStats: Array.from(includedInStats)

    if (editingCustomization) {
      // Update existing
      updateCustomization.mutate(
        {
          id: editingCustomization.id,
          data: {
            displayName: customizeDisplayName.trim(),
            domains,
            includedInStats: included,
          },
        },
        {
          onSuccess: () => {
            closeCustomizeDialog()
          },
        }
      )
    } else {
      // Check if displayName already exists, merge if so
      const existing = customizations?.find(
        c => c.displayName.toLowerCase() === customizeDisplayName.trim().toLowerCase()
      )

      if (existing) {
        // Merge into existing - combine inclusions
        const mergedIncluded = [...(existing.includedInStats ?? []), ...included]
        updateCustomization.mutate(
          {
            id: existing.id,
            data: {
              displayName: existing.displayName,
              domains: [...existing.domains, ...domains],
              includedInStats: mergedIncluded,
            },
          },
          {
            onSuccess: () => {
              closeCustomizeDialog()
              clearSelection()
            },
          }
        )
      } else {
        // Create new
        createCustomization.mutate(
          {
            displayName: customizeDisplayName.trim(),
            domains,
            includedInStats: included,
          },
          {
            onSuccess: () => {
              closeCustomizeDialog()
              clearSelection()
            },
          }
        )
      }
    }
  }

  // Delete customization handler
  const handleDeleteCustomization = (customizationId: number) => {
    deleteCustomization.mutate(customizationId)
  }

  // Reorder domains so ones with icons come first
  const reorderDomainsForIcons = (domains: string[]): string[] => {
    if (!trackerIcons || domains.length <= 1) return domains
    const withIcon: string[] = []
    const withoutIcon: string[] = []
    for (const d of domains) {
      if (trackerIcons[d.toLowerCase()] || trackerIcons[d]) {
        withIcon.push(d)
      } else {
        withoutIcon.push(d)
      }
    }
    return [...withIcon, ...withoutIcon]
  }

  // Open customize dialog for editing existing customization
  const openEditDialog = (customizationId: number, currentName: string, domains: string[]) => {
    // Look up the full customization to get includedInStats
    const fullCustomization = customizations?.find(c => c.id === customizationId)
    setEditingCustomization({
      id: customizationId,
      domains,
      includedInStats: fullCustomization?.includedInStats ?? [],
    })
    setCustomizeDisplayName(currentName)
    setShowCustomizeDialog(true)
  }

  // Open rename/merge dialog for a domain
  // If other domains are already selected, this acts as "add to merge"
  const openRenameDialog = (domain: string) => {
    setEditingCustomization(null)
    // Use functional update to ensure we have the latest selection state
    setSelectedDomains(prev => {
      // If we have 2+ selected, keep selection (add domain if not already in)
      if (prev.size >= 2) {
        const newSelection = new Set(prev)
        newSelection.add(domain) // no-op if already present
        return new Set(reorderDomainsForIcons(Array.from(newSelection)))
      }
      // If 1 selected and clicking a different domain, merge them
      if (prev.size === 1 && !prev.has(domain)) {
        const newSelection = new Set(prev)
        newSelection.add(domain)
        return new Set(reorderDomainsForIcons(Array.from(newSelection)))
      }
      // Single domain rename (0 selected, or clicking the only selected one)
      return new Set([domain])
    })
    setCustomizeDisplayName("")
    setShowCustomizeDialog(true)
  }

  // Close dialog and reset state
  const closeCustomizeDialog = () => {
    setShowCustomizeDialog(false)
    setCustomizeDisplayName("")
    setEditingCustomization(null)
    setIncludedInStats(new Set())
  }

  // Remove a domain from the customize dialog
  const handleRemoveDomainFromDialog = (domainToRemove: string) => {
    if (editingCustomization) {
      const newDomains = editingCustomization.domains.filter(d => d !== domainToRemove)
      const newIncluded = editingCustomization.includedInStats.filter(
        d => d.toLowerCase() !== domainToRemove.toLowerCase()
      )
      if (newDomains.length > 0) {
        setEditingCustomization({ ...editingCustomization, domains: newDomains, includedInStats: newIncluded })
      }
    } else {
      const newSelected = new Set(selectedDomains)
      newSelected.delete(domainToRemove)
      // Also remove from includedInStats for new customizations
      const newIncluded = new Set(includedInStats)
      newIncluded.delete(domainToRemove)
      setIncludedInStats(newIncluded)
      if (newSelected.size > 0) {
        setSelectedDomains(newSelected)
      }
    }
  }

  // Toggle stats inclusion for a domain in the dialog
  // include=true means "add to includedInStats", include=false means "remove from includedInStats"
  const handleToggleStatsInclusion = (domain: string, include: boolean) => {
    if (editingCustomization) {
      const domainLower = domain.toLowerCase()
      const newIncluded = include? [...editingCustomization.includedInStats.filter(d => d.toLowerCase() !== domainLower), domain]: editingCustomization.includedInStats.filter(d => d.toLowerCase() !== domainLower)
      setEditingCustomization({ ...editingCustomization, includedInStats: newIncluded })
    } else {
      const newIncluded = new Set(includedInStats)
      if (include) {
        newIncluded.add(domain)
      } else {
        newIncluded.delete(domain)
      }
      setIncludedInStats(newIncluded)
    }
  }

  // Export customizations to clipboard
  const handleExport = async () => {
    if (!customizations || customizations.length === 0) {
      toast.error(t("trackerBreakdown.toasts.noCustomizationsToExport"))
      return
    }

    const exportData = {
      comment: "qui tracker customizations for Dashboard",
      trackerCustomizations: customizations.map(c => {
        const entry: { displayName: string; domains: string[]; includedInStats?: string[] } = {
          displayName: c.displayName,
          domains: c.domains,
        }
        // Only include includedInStats if non-empty
        if (c.includedInStats && c.includedInStats.length > 0) {
          entry.includedInStats = c.includedInStats
        }
        return entry
      }),
    }

    const jsonString = JSON.stringify(exportData, null, 2)
    const exportText = "```json\n" + jsonString + "\n```"

    try {
      await copyTextToClipboard(exportText)
      toast.success(t("trackerBreakdown.toasts.copiedToClipboard"))
    } catch (error) {
      console.error("[Export] Failed to copy to clipboard:", error)
      toast.error(t("trackerBreakdown.toasts.failedToCopy"))
    }
  }

  // Open import dialog
  const openImportDialog = () => {
    setImportJson("")
    setImportConflicts(new Map())
    setShowImportDialog(true)
  }

  // Parse and validate import JSON
  const parseImportJson = useMemo(() => {
    if (!importJson.trim()) {
      return { valid: false, entries: [], error: null }
    }

    try {
      // Strip markdown codeblocks if present (```json ... ```)
      let jsonText = importJson.trim()
      if (jsonText.startsWith("```")) {
        jsonText = jsonText.replace(/^```(?:json)?\s*\n?/, "").replace(/\n?```\s*$/, "")
      }

      const parsed = JSON.parse(jsonText)
      const entries = parsed.trackerCustomizations

      if (!Array.isArray(entries)) {
        return { valid: false, entries: [], error: t("trackerBreakdown.importDialog.invalidFormat") }
      }

      // Validate each entry
      for (const entry of entries) {
        if (!entry.displayName || typeof entry.displayName !== "string") {
          return { valid: false, entries: [], error: t("trackerBreakdown.importDialog.missingDisplayName") }
        }
        if (!Array.isArray(entry.domains) || entry.domains.length === 0) {
          return { valid: false, entries: [], error: t("trackerBreakdown.importDialog.invalidDomains") }
        }
      }

      // Check for conflicts with existing customizations
      const existingDomains = new Map<string, { id: number; displayName: string; domains: string[] }>()
      for (const c of customizations ?? []) {
        for (const d of c.domains) {
          existingDomains.set(d.toLowerCase(), { id: c.id, displayName: c.displayName, domains: c.domains })
        }
      }

      const entriesWithConflicts = entries.map((entry: { displayName: string; domains: string[]; includedInStats?: string[] }, index: number) => {
        const conflictingDomain = entry.domains.find((d: string) => existingDomains.has(d.toLowerCase()))
        const existingCustomization = conflictingDomain ? existingDomains.get(conflictingDomain.toLowerCase()) : null

        // Check if identical (same name and same domains) - skip these entirely
        let isIdentical = false
        if (existingCustomization) {
          const sameDisplayName = existingCustomization.displayName === entry.displayName
          const existingDomainsSet = new Set(existingCustomization.domains.map(d => d.toLowerCase()))
          const entryDomainsSet = new Set(entry.domains.map(d => d.toLowerCase()))
          const sameDomains = existingDomainsSet.size === entryDomainsSet.size &&
            [...entryDomainsSet].every(d => existingDomainsSet.has(d))

          isIdentical = sameDisplayName && sameDomains
        }

        return {
          ...entry,
          includedInStats: entry.includedInStats ?? [],
          index,
          conflict: existingCustomization,
          isIdentical,
        }
      })

      return { valid: true, entries: entriesWithConflicts, error: null }
    } catch {
      return { valid: false, entries: [], error: t("trackerBreakdown.importDialog.invalidJson") }
    }
  }, [importJson, customizations, t])

  // Handle import
  const handleImport = async () => {
    if (!parseImportJson.valid) return

    let imported = 0
    let skipped = 0
    const failed: string[] = []

    for (const entry of parseImportJson.entries) {
      // Skip identical entries (already exist with same name and domains)
      if (entry.isIdentical) {
        skipped++
        continue
      }

      const action = entry.conflict ? importConflicts.get(entry.index) : undefined

      if (entry.conflict && action === "skip") {
        skipped++
        continue
      }

      try {
        if (entry.conflict && action === "overwrite") {
          // Update existing customization
          await updateCustomization.mutateAsync({
            id: entry.conflict.id,
            data: {
              displayName: entry.displayName,
              domains: entry.domains,
              includedInStats: entry.includedInStats,
            },
          })
          imported++
        } else if (!entry.conflict) {
          // Create new customization
          await createCustomization.mutateAsync({
            displayName: entry.displayName,
            domains: entry.domains,
            includedInStats: entry.includedInStats,
          })
          imported++
        } else {
          // Conflict not resolved - skip
          skipped++
        }
      } catch (error) {
        console.error(`[Import] Failed to import "${entry.displayName}":`, error)
        failed.push(entry.displayName)
      }
    }

    setShowImportDialog(false)
    setImportJson("")
    setImportConflicts(new Map())

    if (failed.length > 0) {
      toast.error(t("trackerBreakdown.toasts.failedToImport", { names: failed.join(", ") }))
    } else if (imported > 0 && skipped > 0) {
      toast.success(t("trackerBreakdown.toasts.importedAndSkipped", { imported, skipped }))
    } else if (imported > 0) {
      toast.success(t(imported !== 1 ? "trackerBreakdown.toasts.importedCount_plural" : "trackerBreakdown.toasts.importedCount", { count: imported }))
    } else {
      toast.info(t("trackerBreakdown.toasts.noCustomizationsImported"))
    }
  }

  // Check if all conflicts are resolved (identical entries don't need resolution)
  const allConflictsResolved = useMemo(() => {
    if (!parseImportJson.valid) return false
    const conflictEntries = parseImportJson.entries.filter((e: { conflict: unknown; isIdentical?: boolean }) => e.conflict && !e.isIdentical)
    return conflictEntries.every((e: { index: number }) => importConflicts.has(e.index))
  }, [parseImportJson, importConflicts])

  // pagination
  const itemsPerPage = settings.trackerBreakdownItemsPerPage || 15
  const [page, setPage] = useState(0)
  const totalPages = Math.ceil(sortedTrackerStats.length / itemsPerPage)
  const paginatedTrackerStats = useMemo(() => {
    const start = page * itemsPerPage
    return sortedTrackerStats.slice(start, start + itemsPerPage)
  }, [sortedTrackerStats, page, itemsPerPage])

  // clamp page when data shrinks
  useEffect(() => {
    if (totalPages === 0) {
      setPage(0)
    } else if (page >= totalPages) {
      setPage(totalPages - 1)
    }
  }, [totalPages, page])

  // reset page when sort changes and persist to settings
  const handleSort = (column: TrackerSortColumn) => {
    setPage(0)
    if (sortColumn === column) {
      const newDirection = sortDirection === "asc" ? "desc" : "asc"
      onSettingsChange({ trackerBreakdownSortDirection: newDirection })
    } else {
      const newDirection = column === "tracker" ? "asc" : "desc"
      onSettingsChange({
        trackerBreakdownSortColumn: column,
        trackerBreakdownSortDirection: newDirection,
      })
    }
  }

  // format efficiency as multiplier (uploaded / totalSize)
  const formatEfficiency = (uploaded: number, totalSize: number): string => {
    if (totalSize === 0) return "-"
    const efficiency = uploaded / totalSize
    return `${efficiency.toFixed(2)}x`
  }

  const formatBuffer = (uploaded: number, downloaded: number): string => {
    const buffer = uploaded - downloaded
    return `${buffer >= 0 ? "+" : "-"}${formatBytes(Math.abs(buffer))}`
  }

  // mobile detail drawer, keyed by domain so the numbers stay live while it is open
  const [detailsDomain, setDetailsDomain] = useState<string | null>(null)
  const detailsTracker = sortedTrackerStats.find(tracker => tracker.domain === detailsDomain)

  // the mobile row always shows uploaded, downloaded and ratio; the sorted metric replaces
  // the trailing count when it is none of those, so the order never rests on a hidden number
  const sortedMetric = (tracker: ProcessedTrackerStats): string | null => {
    switch (sortColumn) {
      case "uploadedSession":
        return formatBytes(tracker.uploadedSession)
      case "downloadedSession":
        return formatBytes(tracker.downloadedSession)
      case "buffer":
        return formatBuffer(tracker.uploaded, tracker.downloaded)
      case "size":
        return formatBytes(tracker.totalSize)
      case "performance":
        return formatEfficiency(tracker.uploaded, tracker.totalSize)
      default:
        return null
    }
  }

  // don't show if no tracker data
  if (sortedTrackerStats.length === 0) {
    return null
  }

  return (
    <>
      <Accordion type="single" collapsible className="rounded-lg border bg-card" value={accordionValue} onValueChange={setAccordionValue}>
        <AccordionItem value="tracker-breakdown" className="border-0">
          <AccordionTrigger className="px-4 py-4 hover:no-underline hover:bg-muted/50 transition-colors [&>svg]:hidden group">
            {/* Mobile layout */}
            <div className="sm:hidden w-full">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Plus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=open]:hidden" />
                  <Minus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=closed]:hidden" />
                  <h3 className="text-sm font-medium text-muted-foreground">{t("trackerBreakdown.title")}</h3>
                </div>
                <span className="text-xs text-muted-foreground">{t("trackerBreakdown.trackersCount", { count: sortedTrackerStats.length })}</span>
              </div>
            </div>

            {/* Desktop layout */}
            <div className="hidden sm:flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 w-full">
              <div className="flex items-center gap-2">
                <Plus className="h-4 w-4 text-muted-foreground group-data-[state=open]:hidden" />
                <Minus className="h-4 w-4 text-muted-foreground group-data-[state=closed]:hidden" />
                <h3 className="text-base font-medium">{t("trackerBreakdown.title")}</h3>
              </div>
              <div className="flex items-center gap-1">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      role="button"
                      tabIndex={0}
                      onClick={(e) => { e.stopPropagation(); openImportDialog() }}
                      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.stopPropagation(); openImportDialog() } }}
                      className="inline-flex items-center justify-center h-7 w-7 rounded-md hover:bg-accent hover:text-accent-foreground cursor-pointer"
                    >
                      <Download className="h-3.5 w-3.5" />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{t("trackerBreakdown.importTooltip")}</TooltipContent>
                </Tooltip>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      role="button"
                      tabIndex={0}
                      onClick={(e) => { e.stopPropagation(); handleExport() }}
                      onKeyDown={(e) => { if (e.key === "Enter" || e.key === " ") { e.stopPropagation(); handleExport() } }}
                      className={`inline-flex items-center justify-center h-7 w-7 rounded-md hover:bg-accent hover:text-accent-foreground cursor-pointer ${!customizations || customizations.length === 0 ? "opacity-50 pointer-events-none" : ""}`}
                      aria-disabled={!customizations || customizations.length === 0}
                    >
                      <Upload className="h-3.5 w-3.5" />
                    </span>
                  </TooltipTrigger>
                  <TooltipContent>{t("trackerBreakdown.exportTooltip")}</TooltipContent>
                </Tooltip>
                <span className="text-muted-foreground ml-1">{t("trackerBreakdown.trackersCount", { count: sortedTrackerStats.length })}</span>
              </div>
            </div>
          </AccordionTrigger>
          <AccordionContent className="px-0 pb-0">
            {/* Mobile Sort Dropdown and Import/Export */}
            <div className="sm:hidden px-4 py-2 border-b flex items-center gap-2">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button variant="outline" className="h-11 flex-1 justify-between">
                    <span className="flex items-center gap-2 text-xs">
                      {t("trackerBreakdown.sort", { column: t(`trackerBreakdown.sortOptions.${sortColumn === "count" ? "torrents" : sortColumn === "performance" ? "seeded" : sortColumn}`) })}
                    </span>
                    {sortDirection === "asc" ? <ArrowUp className="h-3.5 w-3.5" /> : <ArrowDown className="h-3.5 w-3.5" />}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent className="w-full">
                  <DropdownMenuItem onClick={() => handleSort("tracker")}>{t("trackerBreakdown.sortOptions.tracker")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("uploaded")}>{t("trackerBreakdown.sortOptions.uploaded")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("downloaded")}>{t("trackerBreakdown.sortOptions.downloaded")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("uploadedSession")}>{t("trackerBreakdown.sortOptions.uploadedSession")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("downloadedSession")}>{t("trackerBreakdown.sortOptions.downloadedSession")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("ratio")}>{t("trackerBreakdown.sortOptions.ratio")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("count")}>{t("trackerBreakdown.sortOptions.torrents")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("size")}>{t("trackerBreakdown.sortOptions.size")}</DropdownMenuItem>
                  <DropdownMenuItem onClick={() => handleSort("performance")}>{t("trackerBreakdown.sortOptions.seeded")}</DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button variant="ghost" size="icon" className="size-11" onClick={openImportDialog} aria-label={t("trackerBreakdown.importTooltip")}>
                <Download className="h-4 w-4" />
              </Button>
              <Button
                variant="ghost"
                size="icon"
                className="size-11"
                onClick={handleExport}
                disabled={!customizations || customizations.length === 0}
                aria-label={t("trackerBreakdown.exportTooltip")}
              >
                <Upload className="h-4 w-4" />
              </Button>
            </div>


            {/* Mobile row list */}
            <div className="sm:hidden divide-y">
              {paginatedTrackerStats.map((tracker) => {
                const { domain, displayName, originalDomains, uploaded, downloaded, count, customizationId } = tracker
                const { isInfinite, ratio, color: ratioColor } = getTrackerRatioDisplay(uploaded, downloaded)
                const displayValue = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
                const iconDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
                const isSelected = selectedDomains.has(domain)
                const isGroupSelected = selectedGroupId === customizationId
                const isMerged = originalDomains.length > 1
                const hasCustomization = Boolean(customizationId)
                const showCheckbox = !hasCustomization || selectedGroupId === null || isGroupSelected
                const extraMetric = sortedMetric(tracker)

                return (
                  <div
                    key={displayName}
                    className={`flex items-center ${isSelected || isGroupSelected ? "bg-primary/5" : ""}`}
                  >
                    {/* reserves the width when the checkbox is hidden, so rows stay aligned */}
                    <div className="flex h-11 w-11 shrink-0 items-center justify-center">
                      {showCheckbox && (
                        <Checkbox
                          checked={hasCustomization ? isGroupSelected : isSelected}
                          onCheckedChange={() => hasCustomization ? toggleGroupSelection(customizationId!) : toggleSelection(domain)}
                          // the box is 16px; the pseudo-element grows the tap target to fill the 44px cell
                          className="relative before:absolute before:-inset-3.5 before:content-['']"
                        />
                      )}
                    </div>
                    <button
                      type="button"
                      onClick={() => setDetailsDomain(domain)}
                      className="flex min-w-0 flex-1 items-center gap-2 py-2 pr-3 text-left"
                    >
                      <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-1">
                          <span className="truncate text-sm font-medium">{displayValue}</span>
                          {isMerged && <Link2 className="h-3 w-3 shrink-0 text-muted-foreground" />}
                        </div>
                        <div className="flex items-center gap-2 text-xs text-muted-foreground tabular-nums">
                          <span className="flex shrink-0 items-center gap-0.5">
                            <ChevronUp className="h-3 w-3" />{formatBytes(uploaded)}
                          </span>
                          <span className="flex shrink-0 items-center gap-0.5">
                            <ChevronDown className="h-3 w-3" />{formatBytes(downloaded)}
                          </span>
                          <span className="shrink-0" style={{ color: ratioColor }}>
                            {isInfinite ? "∞" : ratio.toFixed(2)}
                          </span>
                        </div>
                      </div>
                      {/* torrent count, or the sorted metric when it is not on the line above */}
                      <span className="shrink-0 text-xs text-muted-foreground tabular-nums">
                        {extraMetric ?? count}
                      </span>
                      <MoreVertical className="h-4 w-4 shrink-0 text-muted-foreground" />
                    </button>
                  </div>
                )
              })}
            </div>

            {/* Desktop Table */}
            <Table className="hidden sm:table">
              <TableHeader>
                <TableRow className="bg-muted/50">
                  <TableHead className="w-8 pl-4" />
                  <TableHead className="w-[35%]">
                    <button
                      type="button"
                      onClick={() => handleSort("tracker")}
                      className="flex items-center gap-1.5 hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.tracker")}
                      <SortIcon column="tracker" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("uploaded")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.uploaded")}
                      <SortIcon column="uploaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("uploadedSession")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.uploadedSession")}
                      <SortIcon column="uploadedSession" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("downloaded")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.downloaded")}
                      <SortIcon column="downloaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("downloadedSession")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.downloadedSession")}
                      <SortIcon column="downloadedSession" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("ratio")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.ratio")}
                      <SortIcon column="ratio" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right hidden lg:table-cell">
                    <button
                      type="button"
                      onClick={() => handleSort("buffer")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.buffer")}
                      <SortIcon column="buffer" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right">
                    <button
                      type="button"
                      onClick={() => handleSort("count")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.torrents")}
                      <SortIcon column="count" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right hidden lg:table-cell">
                    <button
                      type="button"
                      onClick={() => handleSort("size")}
                      className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                    >
                      {t("trackerBreakdown.tableHeaders.size")}
                      <SortIcon column="size" sortColumn={sortColumn} sortDirection={sortDirection} />
                    </button>
                  </TableHead>
                  <TableHead className="text-right hidden lg:table-cell pr-4">
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          onClick={() => handleSort("performance")}
                          className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors"
                        >
                          {t("trackerBreakdown.tableHeaders.seeded")}
                          <Info className="h-3.5 w-3.5 text-muted-foreground" />
                          <SortIcon column="performance" sortColumn={sortColumn} sortDirection={sortDirection} />
                        </button>
                      </TooltipTrigger>
                      <TooltipContent side="top">
                        <p className="text-xs">{t("trackerBreakdown.seededTooltip")}</p>
                      </TooltipContent>
                    </Tooltip>
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {paginatedTrackerStats.map((tracker, index) => {
                  const { domain, displayName, originalDomains, uploaded, downloaded, uploadedSession, downloadedSession, totalSize, count, customizationId } = tracker
                  const { isInfinite, ratio, color: ratioColor } = getTrackerRatioDisplay(uploaded, downloaded)
                  const displayValue = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
                  const iconDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
                  const isSelected = selectedDomains.has(domain)
                  const isGroupSelected = selectedGroupId === customizationId
                  const isMerged = originalDomains.length > 1
                  const hasCustomization = Boolean(customizationId)
                  const buffer = uploaded - downloaded
                  const uploadPercent = totalUploaded > 0 ? (uploaded / totalUploaded) * 100 : 0
                  const uploadSessionPercent = totalUploadedSession > 0 ? (uploadedSession / totalUploadedSession) * 100 : 0

                  return (
                    <TableRow
                      key={displayName}
                      className={`group ${isSelected || isGroupSelected ? "bg-primary/5" : index % 2 === 1 ? "bg-muted/30" : ""} hover:bg-muted/50`}
                    >
                      <TableCell className="w-8 pl-4">
                        {hasCustomization ? (
                        // Show group checkbox if no group selected or the group selected
                          (selectedGroupId === null || isGroupSelected) && (
                            <Checkbox
                              checked={isGroupSelected}
                              onCheckedChange={() => toggleGroupSelection(customizationId!)}
                              className="opacity-0 group-hover:opacity-100 data-[state=checked]:opacity-100"
                            />
                          )
                        ) : (
                          <Checkbox
                            checked={isSelected}
                            onCheckedChange={() => toggleSelection(domain)}
                            className="opacity-0 group-hover:opacity-100 data-[state=checked]:opacity-100"
                          />
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <span className="font-medium truncate cursor-default">
                                {displayValue}
                              </span>
                            </TooltipTrigger>
                            {(isMerged || (hasCustomization && displayName !== domain)) && (
                              <TooltipContent>
                                <p className="text-xs">
                                  {isMerged ? t("trackerBreakdown.mergedFrom", { domains: originalDomains.join(", ") }) : t("trackerBreakdown.original", { domain })}
                                </p>
                              </TooltipContent>
                            )}
                          </Tooltip>
                          {isMerged && <Link2 className="h-3 w-3 text-muted-foreground shrink-0" />}
                          <div className="flex items-center gap-0.5 ml-auto opacity-0 group-hover:opacity-100 shrink-0">
                            {hasCustomization && customizationId ? (
                            // Show group merge if domains selected and if no other group is selected
                              selectedDomains.size > 0 && !(selectedGroupId !== null && selectedGroupId !== customizationId) ? (
                                <Tooltip>
                                  <TooltipTrigger asChild>
                                    <Button
                                      variant="ghost"
                                      size="sm"
                                      className="h-6 w-6 p-0"
                                      onClick={(e) => { e.stopPropagation(); handleMergeIntoGroup(customizationId) }}
                                    >
                                      <Link2 className="h-3 w-3 text-primary" />
                                    </Button>
                                  </TooltipTrigger>
                                  <TooltipContent>{t("trackerBreakdown.mergeTooltip")}</TooltipContent>
                                </Tooltip>
                              ) : (
                                <>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 p-0"
                                    onClick={(e) => { e.stopPropagation(); openEditDialog(customizationId, displayName, originalDomains) }}
                                  >
                                    <Pencil className="h-3 w-3 text-muted-foreground" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 p-0"
                                    onClick={(e) => { e.stopPropagation(); handleDeleteCustomization(customizationId) }}
                                  >
                                    <Trash2 className="h-3 w-3 text-muted-foreground" />
                                  </Button>
                                </>
                              )
                            ) : (
                              <Tooltip>
                                <TooltipTrigger asChild>
                                  <Button
                                    variant="ghost"
                                    size="sm"
                                    className="h-6 w-6 p-0"
                                    onClick={(e) => {
                                      e.stopPropagation()
                                      if (selectedGroupId) {
                                        handleMergeIntoGroup(selectedGroupId, domain)
                                      } else {
                                        openRenameDialog(domain)
                                      }
                                    }}
                                  >
                                    {selectedGroupId || selectedDomains.size > 0 ? (
                                      <Link2 className="h-3 w-3 text-primary" />
                                    ) : (
                                      <Pencil className="h-3 w-3 text-muted-foreground" />
                                    )}
                                  </Button>
                                </TooltipTrigger>
                                <TooltipContent>
                                  {selectedGroupId ? t("trackerBreakdown.mergeIntoGroup") : selectedDomains.size > 0 ? t("trackerBreakdown.addToMerge") : t("trackerBreakdown.rename")}
                                </TooltipContent>
                              </Tooltip>
                            )}
                          </div>
                        </div>
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        {formatBytes(uploaded)} <span className="text-[10px] text-muted-foreground font-normal">({uploadPercent.toFixed(1)}%)</span>
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        {formatBytes(uploadedSession)} <span className="text-[10px] text-muted-foreground font-normal">({uploadSessionPercent.toFixed(1)}%)</span>
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        {formatBytes(downloaded)}
                      </TableCell>
                      <TableCell className="text-right font-semibold">
                        {formatBytes(downloadedSession)}
                      </TableCell>
                      <TableCell className="text-right font-semibold" style={{ color: ratioColor }}>
                        {isInfinite ? "∞" : ratio.toFixed(2)}
                      </TableCell>
                      <TableCell className="text-right hidden lg:table-cell font-semibold">
                        <span
                          className={buffer < 0 ? "text-destructive" : ""}
                          style={buffer >= 0 ? { color: "oklch(0.7040 0.1910 142)" } : undefined}
                        >
                          {formatBuffer(uploaded, downloaded)}
                        </span>
                      </TableCell>
                      <TableCell className="text-right">
                        {count}
                      </TableCell>
                      <TableCell className="text-right hidden lg:table-cell font-semibold">
                        {formatBytes(totalSize)}
                      </TableCell>
                      <TableCell className="text-right hidden lg:table-cell font-semibold pr-4">
                        {formatEfficiency(uploaded, totalSize)}
                      </TableCell>
                    </TableRow>
                  )
                })}
              </TableBody>
            </Table>
            {/* Pagination controls */}
            {totalPages > 1 && (
              <div className="flex items-center justify-between px-4 py-3 border-t">
                <span className="text-sm text-muted-foreground">
                  {t("trackerBreakdown.pagination.range", { start: page * itemsPerPage + 1, end: Math.min((page + 1) * itemsPerPage, sortedTrackerStats.length), total: sortedTrackerStats.length })}
                </span>
                <div className="flex items-center gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => Math.max(0, p - 1))}
                    disabled={page === 0}
                  >
                    <ChevronLeft className="h-4 w-4" />
                    <span className="hidden sm:inline ml-1">{t("trackerBreakdown.pagination.previous")}</span>
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
                    disabled={page >= totalPages - 1}
                  >
                    <span className="hidden sm:inline mr-1">{t("trackerBreakdown.pagination.next")}</span>
                    <ChevronRight className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            )}
          </AccordionContent>
        </AccordionItem>
      </Accordion>

      {/* Mobile detail drawer: full metrics and the row actions that no longer fit inline */}
      <Drawer open={Boolean(detailsTracker)} onOpenChange={(open) => !open && setDetailsDomain(null)}>
        <DrawerContent>
          {detailsTracker && (() => {
            const { domain, displayName, originalDomains, uploaded, downloaded, uploadedSession, downloadedSession, totalSize, count, customizationId } = detailsTracker
            const { isInfinite, ratio, color: ratioColor } = getTrackerRatioDisplay(uploaded, downloaded)
            const displayValue = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
            const isMerged = originalDomains.length > 1
            const maskedDomains = incognitoMode ? originalDomains.map(getLinuxTrackerDomain) : originalDomains
            // only worth a line when it says something the title does not
            const subtitle = isMerged ? t("trackerBreakdown.mergedFrom", { domains: maskedDomains.join(", ") }) : displayName !== domain ? t("trackerBreakdown.original", { domain: maskedDomains[0] }) : ""
            const hasCustomization = Boolean(customizationId)
            const canMergeIntoGroup = hasCustomization && selectedDomains.size > 0 && !(selectedGroupId !== null && selectedGroupId !== customizationId)
            const closeAnd = (action: () => void) => () => { setDetailsDomain(null); action() }
            const metrics: DrawerMetric[] = [
              { label: t("trackerBreakdown.tableHeaders.uploaded"), value: formatBytes(uploaded) },
              { label: t("trackerBreakdown.tableHeaders.uploadedSession"), value: formatBytes(uploadedSession) },
              { label: t("trackerBreakdown.tableHeaders.downloaded"), value: formatBytes(downloaded) },
              { label: t("trackerBreakdown.tableHeaders.downloadedSession"), value: formatBytes(downloadedSession) },
              { label: t("trackerBreakdown.tableHeaders.ratio"), value: isInfinite ? "∞" : ratio.toFixed(2), color: ratioColor },
              { label: t("trackerBreakdown.tableHeaders.buffer"), value: formatBuffer(uploaded, downloaded) },
              { label: t("trackerBreakdown.tableHeaders.torrents"), value: String(count) },
              { label: t("trackerBreakdown.tableHeaders.size"), value: formatBytes(totalSize) },
              { label: t("trackerBreakdown.tableHeaders.seeded"), value: formatEfficiency(uploaded, totalSize) },
            ]

            return (
              <>
                <DrawerHeader className="text-left">
                  <DrawerTitle className="truncate">{displayValue}</DrawerTitle>
                  <DrawerDescription className="truncate">{subtitle}</DrawerDescription>
                </DrawerHeader>
                <MetricGrid metrics={metrics} className="pb-2" />
                <DrawerFooter>
                  {canMergeIntoGroup ? (
                    <Button className="h-11" onClick={closeAnd(() => handleMergeIntoGroup(customizationId!))}>
                      <Link2 className="h-4 w-4" />
                      {t("trackerBreakdown.mergeTooltip")}
                    </Button>
                  ) : hasCustomization && customizationId ? (
                    <>
                      <Button variant="outline" className="h-11" onClick={closeAnd(() => openEditDialog(customizationId, displayName, originalDomains))}>
                        <Pencil className="h-4 w-4" />
                        {t("trackerBreakdown.edit")}
                      </Button>
                      <Button variant="outline" className="h-11 text-destructive" onClick={closeAnd(() => handleDeleteCustomization(customizationId))}>
                        <Trash2 className="h-4 w-4" />
                        {t("trackerBreakdown.delete")}
                      </Button>
                    </>
                  ) : (
                    <Button
                      variant="outline"
                      className="h-11"
                      onClick={closeAnd(() => selectedGroupId ? handleMergeIntoGroup(selectedGroupId, domain) : openRenameDialog(domain))}
                    >
                      {selectedGroupId ? <Link2 className="h-4 w-4" /> : <Pencil className="h-4 w-4" />}
                      {selectedGroupId ? t("trackerBreakdown.mergeIntoGroup") : selectedDomains.size > 0 ? t("trackerBreakdown.addToMerge") : t("trackerBreakdown.rename")}
                    </Button>
                  )}
                </DrawerFooter>
              </>
            )
          })()}
        </DrawerContent>
      </Drawer>

      {/* Customize Dialog (Rename/Merge/Edit) */}
      <Dialog open={showCustomizeDialog} onOpenChange={(open) => !open && closeCustomizeDialog()}>
        <DialogContent className="max-h-[90dvh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>
              {editingCustomization? t("trackerBreakdown.customizeDialog.editTitle"): selectedDomains.size === 1? t("trackerBreakdown.customizeDialog.renameTitle"): t("trackerBreakdown.customizeDialog.mergeTitle")}
            </DialogTitle>
            <DialogDescription>
              {editingCustomization? t("trackerBreakdown.customizeDialog.editDescription"): selectedDomains.size === 1? t("trackerBreakdown.customizeDialog.renameDescription"): t("trackerBreakdown.customizeDialog.mergeDescription")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4 min-h-0 flex-1 flex flex-col">
            <div className="space-y-2">
              <Label htmlFor="customize-name">{t("trackerBreakdown.customizeDialog.displayName")}</Label>
              <Input
                id="customize-name"
                value={customizeDisplayName}
                onChange={(e) => setCustomizeDisplayName(e.target.value)}
                placeholder={t("trackerBreakdown.customizeDialog.displayNamePlaceholder")}
              />
            </div>
            <div className="space-y-2 min-h-0 flex-1 flex flex-col overflow-hidden">
              <Label>{editingCustomization ? t("trackerBreakdown.customizeDialog.domains") : t("trackerBreakdown.customizeDialog.selectedTrackers")}</Label>
              {((editingCustomization && editingCustomization.domains.length > 1) || (!editingCustomization && selectedDomains.size > 1)) && (
                <p className="text-xs text-muted-foreground">
                  {t("trackerBreakdown.customizeDialog.uncheckDuplicate")}
                </p>
              )}
              <ScrollArea className="h-[300px]">
                <div className="text-sm text-muted-foreground space-y-1.5 pr-4">
                  {(editingCustomization ? editingCustomization.domains : Array.from(selectedDomains)).map((domain, index, arr) => {
                    const hasMultiple = arr.length > 1
                    const isPrimary = index === 0
                    // Get inclusion state from appropriate source
                    // Primary is always included; secondary domains only if in includedInStats
                    const currentIncluded = editingCustomization? editingCustomization.includedInStats: Array.from(includedInStats)
                    const isInList = currentIncluded.some(d => d.toLowerCase() === domain.toLowerCase())
                    const isIncluded = isPrimary || isInList

                    return (
                      <div key={domain} className={`grid items-center gap-2 ${hasMultiple ? "grid-cols-[auto_1fr_auto_auto]" : "grid-cols-[1fr]"}`}>
                        {hasMultiple && (
                          <Checkbox
                            checked={isIncluded}
                            disabled={isPrimary}
                            onCheckedChange={(checked) => handleToggleStatsInclusion(domain, !!checked)}
                            className="h-4 w-4"
                          />
                        )}
                        <span className={`truncate${isPrimary ? " font-medium" : ""}`} title={domain}>{domain}</span>
                        {hasMultiple && (
                          isPrimary ? (
                            <Badge variant="secondary" className="text-[10px]">{t("trackerBreakdown.customizeDialog.primary")}</Badge>
                          ) : <span />
                        )}
                        {hasMultiple && (
                          <button
                            type="button"
                            onClick={() => handleRemoveDomainFromDialog(domain)}
                            className="text-muted-foreground hover:text-destructive cursor-pointer"
                          >
                            <X className="h-3.5 w-3.5" />
                          </button>
                        )}
                      </div>
                    )
                  })}
                </div>
              </ScrollArea>
            </div>
          </div>
          <DialogFooter className="flex-shrink-0">
            <Button variant="outline" onClick={closeCustomizeDialog}>
              {t("trackerBreakdown.customizeDialog.cancel")}
            </Button>
            <Button
              onClick={handleSaveCustomization}
              disabled={!customizeDisplayName.trim() || createCustomization.isPending || updateCustomization.isPending}
            >
              {(createCustomization.isPending || updateCustomization.isPending)? t("trackerBreakdown.customizeDialog.saving"): editingCustomization? t("trackerBreakdown.customizeDialog.save"): selectedDomains.size === 1? t("trackerBreakdown.customizeDialog.renameAction"): t("trackerBreakdown.customizeDialog.mergeAction")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Import Dialog */}
      <Dialog open={showImportDialog} onOpenChange={setShowImportDialog}>
        <DialogContent className="sm:max-w-lg max-h-[90dvh] flex flex-col">
          <DialogHeader className="flex-shrink-0">
            <DialogTitle>{t("trackerBreakdown.importDialog.title")}</DialogTitle>
            <DialogDescription>
              {t("trackerBreakdown.importDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="flex-1 overflow-y-auto min-h-0 space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="import-json">{t("trackerBreakdown.importDialog.jsonData")}</Label>
              <Textarea
                id="import-json"
                value={importJson}
                onChange={(e) => setImportJson(e.target.value)}
                placeholder={"{\n  \"trackerCustomizations\": [\n    { \"displayName\": \"Name\", \"domains\": [\"domain.com\"] }\n  ]\n}"}
                className="font-mono text-xs h-32"
              />
            </div>

            {/* Validation feedback */}
            {importJson.trim() && (
              <div className="space-y-2">
                {parseImportJson.error ? (
                  <div className="flex items-center gap-2 text-destructive text-sm">
                    <AlertTriangle className="h-4 w-4" />
                    {parseImportJson.error}
                  </div>
                ) : parseImportJson.valid && (
                  <>
                    {(() => {
                      const conflicts = parseImportJson.entries.filter((e: { conflict?: unknown; isIdentical?: boolean }) => e.conflict && !e.isIdentical)
                      const newEntries = parseImportJson.entries.filter((e: { conflict?: unknown; isIdentical?: boolean }) => !e.conflict && !e.isIdentical)
                      const identicalEntries = parseImportJson.entries.filter((e: { isIdentical?: boolean }) => e.isIdentical)
                      return (
                        <>
                          <div className="text-sm text-muted-foreground">
                            {newEntries.length > 0 && <span>{t("trackerBreakdown.importDialog.newCount", { count: newEntries.length })}</span>}
                            {newEntries.length > 0 && (conflicts.length > 0 || identicalEntries.length > 0) && <span>, </span>}
                            {conflicts.length > 0 && <span className="text-yellow-600">{t(conflicts.length !== 1 ? "trackerBreakdown.importDialog.conflictCount_plural" : "trackerBreakdown.importDialog.conflictCount", { count: conflicts.length })}</span>}
                            {conflicts.length > 0 && identicalEntries.length > 0 && <span>, </span>}
                            {identicalEntries.length > 0 && <span className="text-muted-foreground">{t("trackerBreakdown.importDialog.unchangedCount", { count: identicalEntries.length })}</span>}
                          </div>
                          {conflicts.length > 0 && (
                            <>
                              <Label>{t("trackerBreakdown.importDialog.resolveConflicts")}</Label>
                              <div className="border rounded-md max-h-48 overflow-y-auto">
                                {conflicts.map((entry: { displayName: string; domains: string[]; index: number; conflict?: { id: number; displayName: string; domains: string[] } | null }) => (
                                  <div
                                    key={entry.index}
                                    className="px-3 py-2 text-sm border-b last:border-b-0 bg-yellow-500/10"
                                  >
                                    <div className="flex items-center justify-between gap-2">
                                      <div className="min-w-0 flex-1">
                                        <div className="font-medium truncate">{entry.displayName}</div>
                                        <div className="text-xs text-muted-foreground truncate">
                                          {entry.domains.join(", ")}
                                        </div>
                                        <div className="text-xs text-yellow-600 mt-1">
                                          {t("trackerBreakdown.importDialog.conflictsWith", { name: entry.conflict?.displayName })}
                                        </div>
                                      </div>
                                      <div className="flex items-center gap-1 shrink-0">
                                        <Button
                                          variant={importConflicts.get(entry.index) === "skip" ? "secondary" : "ghost"}
                                          size="sm"
                                          className="h-6 px-2 text-xs"
                                          onClick={() => setImportConflicts(new Map(importConflicts).set(entry.index, "skip"))}
                                        >
                                          {t("trackerBreakdown.importDialog.skip")}
                                        </Button>
                                        <Button
                                          variant={importConflicts.get(entry.index) === "overwrite" ? "secondary" : "ghost"}
                                          size="sm"
                                          className="h-6 px-2 text-xs"
                                          onClick={() => setImportConflicts(new Map(importConflicts).set(entry.index, "overwrite"))}
                                        >
                                          {t("trackerBreakdown.importDialog.overwrite")}
                                        </Button>
                                      </div>
                                    </div>
                                  </div>
                                ))}
                              </div>
                            </>
                          )}
                        </>
                      )
                    })()}
                  </>
                )}
              </div>
            )}
          </div>
          <DialogFooter className="flex-shrink-0">
            <Button variant="outline" onClick={() => setShowImportDialog(false)}>
              {t("trackerBreakdown.importDialog.cancel")}
            </Button>
            <Button
              onClick={handleImport}
              disabled={!parseImportJson.valid || !allConflictsResolved || createCustomization.isPending || updateCustomization.isPending}
            >
              {(createCustomization.isPending || updateCustomization.isPending) ? t("trackerBreakdown.importDialog.importing") : t("trackerBreakdown.importDialog.import")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
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

  // Handler for TrackerBreakdownCard to update settings
  const handleTrackerSettingsChange = (input: { trackerBreakdownSortColumn?: string; trackerBreakdownSortDirection?: string; trackerBreakdownItemsPerPage?: number }) => {
    updateSettings.mutate(input)
  }

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
      <div className="container mx-auto p-4 sm:p-6">
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
    <div className="container mx-auto p-4 sm:p-6">
      {/* Header with Actions */}
      <div className="mb-6">
        <div className="flex items-center justify-between gap-2">
          <h1 className="text-2xl sm:text-3xl font-bold">{t("title")}</h1>
          {/* on phones the two rare actions ride the title row, which is otherwise dead
              space, so only the primary action costs a row of its own below */}
          {instances && instances.length > 0 && (
            <div className="flex items-center gap-1 sm:hidden">
              <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>
                <Button variant="outline" size="icon" className="size-11" aria-label={t("addInstance")}>
                  <HardDrive className="h-4 w-4" />
                </Button>
              </Link>
              <DashboardSettingsDialog iconOnly />
            </div>
          )}
        </div>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mt-2">
          <p className="text-muted-foreground">
            {t("description")}
          </p>
          {instances && instances.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 w-full sm:w-auto">
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
            </div>
          )}
        </div>
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
                  case "tracker-breakdown":
                    return (
                      <TrackerBreakdownCard
                        key={sectionId}
                        statsData={statsData}
                        settings={settings}
                        onSettingsChange={handleTrackerSettingsChange}
                        isCollapsed={settings.sectionCollapsed["tracker-breakdown"] ?? false}
                        onCollapsedChange={(collapsed) => handleSectionCollapsedChange("tracker-breakdown", collapsed)}
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
        </Card>
      )}
    </div>
  )
}
