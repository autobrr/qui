/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DashboardOverview } from "@/components/dashboard/DashboardOverview"
import { MigrationHint } from "@/components/instances/MigrationHint"
import { PasswordIssuesBanner } from "@/components/instances/PasswordIssuesBanner"
import { Page } from "@/components/layout/Page"
import { MagnetHandlerBanner } from "@/components/MagnetHandlerBanner"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { createStreamKey, useSyncStreamManager, type StreamState } from "@/contexts/SyncStreamContext"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedTitleBarSpeeds } from "@/hooks/usePersistedTitleBarSpeeds"
import { usePageTitle } from "@/hooks/usePageTitle"
import { useTitleBarSpeeds } from "@/hooks/useTitleBarSpeeds"
import { api } from "@/lib/api"
import type { DashboardInstanceStats } from "@/lib/dashboard-overview"
import {
  DASHBOARD_STATS_FALLBACK_ORDER,
  DASHBOARD_STATS_FALLBACK_SORT,
  createDashboardStatsFallbackQueryKey,
  hasDashboardStatsPayload,
  mergeDashboardInstanceMeta,
  mergeDashboardStatsSnapshot,
  resolveDashboardStreamError,
  resolveDashboardTorrentCounts,
  shouldUseDashboardStatsFallback
} from "@/lib/dashboard-stream"
import { useIncognitoMode } from "@/lib/incognito"
import { isStreamUsable, streamFallbackPollInterval } from "@/lib/sync-stream-state"
import { themeArea } from "@/lib/theme-areas"
import type {
  CacheMetadata,
  InstanceMeta,
  InstanceResponse,
  QBittorrentAppInfo,
  ServerState,
  TorrentCounts,
  TorrentResponse,
  TorrentStats,
  TorrentStreamPayload
} from "@/types"
import { useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { ChevronDown, Eye, EyeOff, HardDrive, Plus } from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

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
  instanceMeta: InstanceMeta | null
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
const DISK_REFRESH_INTERVAL_MS = 60_000

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

  const diskQueries = useQueries({
    queries: instances.map(instance => ({
      queryKey: ["instance-disk", instance.id, instance.filesystemMode],
      queryFn: () => api.getInstanceDisk(instance.id),
      enabled: streamEnabled && instance.filesystemMode !== "none",
      staleTime: DISK_REFRESH_INTERVAL_MS,
      refetchInterval: DISK_REFRESH_INTERVAL_MS,
    })),
  })

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
      streamError: resolveDashboardStreamError(state.streamError, isFallbackActive && state.fallbackPollInterval !== false, fallbackData),
      hasLiveDashboardStatsPayload: state.hasLiveDashboardStatsPayload,
      isDashboardStatsFallbackActive: isFallbackActive,
      cacheMetadata,
      instanceMeta: state.instanceMeta,
      disk: diskQueries[index]?.data ?? null,
    }
  })
}


function QuickActionsDropdown({ statsData, compact = false }: { statsData: DashboardInstanceStats[]; compact?: boolean }) {
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
        {compact ? (
          <Button variant="outline" className="gap-0.5 px-3" aria-label={t("quickActions.addTorrent")}>
            <Plus className="h-4 w-4" />
            <ChevronDown className="h-3 w-3 opacity-60" />
          </Button>
        ) : (
          <Button variant="outline">
            <Plus className="h-4 w-4 mr-2" />
            {t("quickActions.addTorrent")}
            <ChevronDown className="h-3 w-3 ml-1" />
          </Button>
        )}
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
  const { isHiddenDelayed } = useDelayedVisibility(3000)
  const phone = useIsPhone()
  const [titleBarSpeedsEnabled] = usePersistedTitleBarSpeeds(false)
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

  const statsData = useAllInstanceStats(activeInstances, { enabled: !isHiddenDelayed })
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
  const titleBarSpeeds = useTitleBarSpeeds({
    enabled: titleBarSpeedsEnabled && hasActiveInstances,
    foregroundSpeeds: hasActiveInstances ? {
      dl: statsData.reduce((sum, { stats }) => sum + (stats?.totalDownloadSpeed ?? 0), 0),
      up: statsData.reduce((sum, { stats }) => sum + (stats?.totalUploadSpeed ?? 0), 0),
    } : undefined,
    backgroundSpeeds: isHiddenDelayed && hasActiveInstances ? backgroundSpeeds : undefined,
  })
  usePageTitle({ speeds: titleBarSpeeds })

  const incognitoToggle = (
    <Button
      variant="outline"
      size="icon"
      aria-label={incognitoMode ? t("incognito.turnOff") : t("incognito.turnOn")}
      aria-pressed={incognitoMode}
      onClick={() => setIncognitoMode(!incognitoMode)}
    >
      {incognitoMode ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
    </Button>
  )

  if (isLoading) {
    return (
      <Page title={t("title")} description={t("description")}>
        <div className="h-56 animate-pulse rounded bg-muted" />
      </Page>
    )
  }

  return (
    <Page
      title={t("title")}
      description={t("description")}
      controls={hasInstances && (
        <>
          <QuickActionsDropdown statsData={statsData} compact={phone} />
          <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>
            {phone ? (
              <Button variant="outline" size="icon" aria-label={t("addInstance")}>
                <HardDrive className="h-4 w-4" />
              </Button>
            ) : (
              <Button variant="outline">
                <HardDrive className="h-4 w-4 mr-2" />
                {t("addInstance")}
              </Button>
            )}
          </Link>
          {incognitoToggle}
        </>
      )}
    >
      <PasswordIssuesBanner instances={instances || []} />

      <MagnetHandlerBanner />

      {hasInstances ? (
        hasActiveInstances ? (
          <DashboardOverview statsData={statsData} />
        ) : (
          <Card {...themeArea("dashboard-card")} className="p-8 text-center">
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
        )
      ) : (
        <Card {...themeArea("dashboard-card")} className="p-8 sm:p-12 text-center">
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
    </Page>
  )
}
