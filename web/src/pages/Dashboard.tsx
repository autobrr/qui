/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceErrorDisplay } from "@/components/instances/InstanceErrorDisplay"
import { InstanceSettingsButton } from "@/components/instances/InstanceSettingsButton"
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
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedAccordionState } from "@/hooks/usePersistedAccordionState"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { api } from "@/lib/api"
import { formatBytes, getRatioColor } from "@/lib/utils"
import type { InstanceResponse, ServerState, TorrentCounts, TorrentResponse, TorrentStats } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Activity, Ban, BrickWallFire, ChevronDown, ChevronUp, Download, ExternalLink, Eye, EyeOff, Globe, HardDrive, Minus, Plus, Rabbit, Turtle, Upload, Zap } from "lucide-react"
import { useMemo, useState } from "react"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"

import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { useTranslation } from "react-i18next"

interface DashboardInstanceStats {
  instance: InstanceResponse
  stats: TorrentStats | null
  serverState: ServerState | null
  torrentCounts?: TorrentCounts
  altSpeedEnabled: boolean
  isLoading: boolean
  error: unknown
}

// Optimized hook to get all instance stats using shared TorrentResponse cache
function useAllInstanceStats(instances: InstanceResponse[]): DashboardInstanceStats[] {
  const dashboardQueries = useQueries({
    queries: instances.map(instance => ({
      // Use same query key pattern as useTorrentsList for first page with no filters
      queryKey: ["torrents-list", instance.id, 0, undefined, undefined, "added_on", "desc"],
      queryFn: () => api.getTorrents(instance.id, {
        page: 0,
        limit: 1, // Only need metadata, not actual torrents for Dashboard
        sort: "added_on",
        order: "desc" as const,
      }),
      enabled: true,
      refetchInterval: 5000, // Match TorrentTable polling
      staleTime: 2000,
      gcTime: 300000, // Match TorrentTable cache time
      placeholderData: (previousData: TorrentResponse | undefined) => previousData,
      retry: 1,
      retryDelay: 1000,
    })),
  })

  return instances.map<DashboardInstanceStats>((instance, index) => {
    const query = dashboardQueries[index]
    const data = query.data as TorrentResponse | undefined

    return {
      instance,
      // Return TorrentStats directly - no more backwards compatibility conversion
      stats: data?.stats ?? null,
      serverState: data?.serverState ?? null,
      torrentCounts: data?.counts,
      // Include alt speed status from server state to avoid separate API call
      altSpeedEnabled: data?.serverState?.use_alt_speed_limits || false,
      // Include loading/error state for individual instances
      isLoading: query.isLoading,
      error: query.error,
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
  const { t } = useTranslation()
  const { instance, stats, serverState, torrentCounts, altSpeedEnabled, isLoading, error } = instanceData
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
  } = useQBittorrentAppInfo(instance.id)
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit] = useSpeedUnits()
  const appVersion = qbittorrentAppInfo?.version || qbittorrentVersionInfo?.appVersion || ""
  const webAPIVersion = qbittorrentAppInfo?.webAPIVersion || qbittorrentVersionInfo?.webAPIVersion || ""
  const libtorrentVersion = qbittorrentAppInfo?.buildInfo?.libtorrent || ""
  const displayUrl = instance.host

  // Determine card state
  const isFirstLoad = isLoading && !stats
  const isDisconnected = (stats && !instance.connected) || (!isFirstLoad && !instance.connected)
  const hasError = Boolean(error) || (!isFirstLoad && !stats)
  const hasDecryptionOrRecentErrors = instance.hasDecryptionError || (instance.recentErrors && instance.recentErrors.length > 0)

  const rawConnectionStatus = serverState?.connection_status ?? instance.connectionStatus ?? ""
  const normalizedConnectionStatus = rawConnectionStatus ? rawConnectionStatus.trim().toLowerCase() : ""
  const formattedConnectionStatus = normalizedConnectionStatus ? normalizedConnectionStatus.replace(/_/g, " ") : ""
  const connectionStatusDisplay = formattedConnectionStatus? formattedConnectionStatus.replace(/\b\w/g, (char: string) => char.toUpperCase()): ""
  const hasConnectionStatus = Boolean(formattedConnectionStatus)

  // Determine badge variant and text
  let badgeVariant: "default" | "secondary" | "destructive" = "default"
  let badgeText = t("dashboard.instance.connected")

  if (isFirstLoad) {
    badgeVariant = "secondary"
    badgeText = t("dashboard.instance.loading")
  } else if (hasError) {
    badgeVariant = "destructive"
    badgeText = t("dashboard.instance.error")
  } else if (isDisconnected) {
    badgeVariant = "destructive"
    badgeText = t("dashboard.instance.disconnected")
  }

  const badgeClassName = "whitespace-nowrap"

  const isConnectable = normalizedConnectionStatus === "connected"
  const isFirewalled = normalizedConnectionStatus === "firewalled"
  const ConnectionStatusIcon = isConnectable ? Globe : isFirewalled ? BrickWallFire : Ban
  const connectionStatusIconClass = hasConnectionStatus? isConnectable? "text-green-500": isFirewalled? "text-amber-500": "text-destructive": ""

  const connectionStatusTooltip = connectionStatusDisplay ? (isConnectable ? t("dashboard.instance.connection_status_tooltip.connectable") : connectionStatusDisplay) : ""

  // Determine if settings button should show
  const showSettingsButton = instance.connected && !isFirstLoad && !hasDecryptionOrRecentErrors

  // Determine link destination
  const linkTo = hasDecryptionOrRecentErrors ? "/instances" : "/instances/$instanceId"
  const linkParams = hasDecryptionOrRecentErrors ? {} : { instanceId: instance.id.toString() }

  // Unified return statement
  return (
    <>
      <Card className="hover:shadow-lg transition-shadow">
        <CardHeader className={!isFirstLoad ? "gap-1" : ""}>
          <div className="flex items-center gap-2 sm:gap-3">
            <Link
              to={linkTo}
              params={linkParams}
              className="flex flex-1 items-center gap-2 hover:underline min-w-0"
            >
              <CardTitle className="text-lg truncate min-w-0 max-w-[80px] sm:max-w-[90px] md:max-w-[90px] lg:max-w-[90px] xl:max-w-[120px] 2xl:max-w-[250px]">{instance.name}</CardTitle>
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            </Link>
            <div className="flex items-center gap-2 shrink-0">
              {instance.connected && !isFirstLoad && (
                <>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={(e) => {
                          e.preventDefault()
                          e.stopPropagation()
                          setShowSpeedLimitDialog(true)
                        }}
                        disabled={isToggling}
                        className="h-8 w-8 p-0 !hover:bg-transparent"
                      >
                        {altSpeedEnabled ? (
                          <Turtle className="h-4 w-4 text-orange-600" />
                        ) : (
                          <Rabbit className="h-4 w-4 text-green-600" />
                        )}
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      {altSpeedEnabled ? t("dashboard.instance.altSpeed.enabledTooltip") : t("dashboard.instance.altSpeed.disabledTooltip")}
                    </TooltipContent>
                  </Tooltip>
                  <AlertDialog open={showSpeedLimitDialog} onOpenChange={setShowSpeedLimitDialog}>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>
                          {altSpeedEnabled ? t("dashboard.instance.altSpeed.disableTitle") : t("dashboard.instance.altSpeed.enableTitle")}
                        </AlertDialogTitle>
                        <AlertDialogDescription>
                          {altSpeedEnabled? t("dashboard.instance.altSpeed.disableDescription", { name: instance.name }): t("dashboard.instance.altSpeed.enableDescription", { name: instance.name })
                          }
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
                        <AlertDialogAction
                          onClick={() => {
                            toggleAltSpeed()
                            setShowSpeedLimitDialog(false)
                          }}
                        >
                          {altSpeedEnabled ? t("dashboard.instance.altSpeed.disableAction") : t("dashboard.instance.altSpeed.enableAction")}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                </>
              )}
              {showSettingsButton && (
                <InstanceSettingsButton
                  instanceId={instance.id}
                  instanceName={instance.name}
                />
              )}
              <Badge variant={badgeVariant} className={badgeClassName}>
                {badgeText}
              </Badge>
            </div>
          </div>
          {(appVersion || webAPIVersion || libtorrentVersion || formattedConnectionStatus) && (
            <CardDescription className="flex flex-wrap items-center gap-1.5 text-xs">
              {formattedConnectionStatus && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <span
                      aria-label={`qBittorrent connection status: ${connectionStatusDisplay || formattedConnectionStatus}`}
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
                  {t("dashboard.instance.qbit_version_badge", { version: appVersion })}
                </Badge>
              )}
              {webAPIVersion && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0.5">
                  {t("dashboard.instance.api_version_badge", { version: webAPIVersion })}
                </Badge>
              )}
              {libtorrentVersion && (
                <Badge variant="secondary" className="text-[10px] px-1.5 py-0.5">
                  {t("dashboard.instance.libtorrent_version_badge", { version: libtorrentVersion })}
                </Badge>
              )}
            </CardDescription>
          )}
          <CardDescription className="text-xs">
            <div className="flex items-center gap-1 min-w-0">
              <span className={`${incognitoMode ? "blur-sm select-none" : ""} truncate min-w-0`} style={incognitoMode ? { filter: "blur(8px)" } : {}} title={displayUrl}>
                {displayUrl}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className={`${!isFirstLoad ? "h-4 w-4" : "h-5 w-5"} p-0 ${isFirstLoad ? "hover:bg-muted/50" : ""} shrink-0`}
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
              {isFirstLoad && <p className="animate-pulse">{t("dashboard.stats.loading")}</p>}
              {hasError && !isDisconnected && <p>{t("dashboard.stats.failed")}</p>}
              <InstanceErrorDisplay instance={instance} compact />
            </div>
          ) : (
            /* Show normal stats */
            <div className="space-y-2 sm:space-y-3">
              <div className="mb-3 sm:mb-6">
                <div className="flex items-center justify-center mb-1">
                  <span className="flex-1 text-center text-xs text-muted-foreground">{t("dashboard.status.downloading")}</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">{t("dashboard.status.active")}</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">{t("dashboard.status.error")}</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">{t("dashboard.status.total")}</span>
                </div>
                <div className="flex items-center justify-center">
                  <span className="flex-1 text-center text-base sm:text-lg font-semibold">
                    {torrentCounts?.status?.downloading || 0}
                  </span>
                  <span className="flex-1 text-center text-base sm:text-lg font-semibold">{torrentCounts?.status?.active || 0}</span>
                  <span className={`flex-1 text-center text-base sm:text-lg font-semibold ${(torrentCounts?.status?.errored || 0) > 0 ? "text-destructive" : ""}`}>
                    {torrentCounts?.status?.errored || 0}
                  </span>
                  <span className="flex-1 text-center text-base sm:text-lg font-semibold">{torrentCounts?.total || 0}</span>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-1 gap-1 sm:gap-2">
                <div className="flex items-center gap-2 text-xs">
                  <Download className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("dashboard.serverStats.download")}</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalDownloadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <Upload className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("dashboard.serverStats.upload")}</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalUploadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("dashboard.serverStats.totalSize")}</span>
                  <span className="ml-auto font-medium truncate">{formatBytes(stats?.totalSize || 0)}</span>
                </div>
              </div>

              {serverState?.free_space_on_disk !== undefined && serverState.free_space_on_disk > 0 && (
                <div className="flex items-center gap-2 text-xs mt-1 sm:mt-2">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">{t("dashboard.serverStats.freeSpace")}</span>
                  <span className="ml-auto font-medium truncate">{formatBytes(serverState.free_space_on_disk)}</span>
                </div>
              )}

              <Collapsible open={isAdvancedMetricsOpen} onOpenChange={setIsAdvancedMetricsOpen}>
                <CollapsibleTrigger className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors w-full [&[data-state=open]>svg]:rotate-180">
                  <ChevronDown className="h-3 w-3 transition-transform" />
                  <span>{isAdvancedMetricsOpen ? t("dashboard.instance.showLess") : t("dashboard.instance.showMore")}</span>
                </CollapsibleTrigger>
                <CollapsibleContent className="space-y-2 mt-2">
                  {serverState?.total_peer_connections !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.peerConnections")}</span>
                      <span className="ml-auto font-medium">{serverState.total_peer_connections || 0}</span>
                    </div>
                  )}

                  {serverState?.queued_io_jobs !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.queuedIO")}</span>
                      <span className="ml-auto font-medium">{serverState.queued_io_jobs || 0}</span>
                    </div>
                  )}

                  {serverState?.total_buffers_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <HardDrive className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.bufferSize")}</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_buffers_size)}</span>
                    </div>
                  )}

                  {serverState?.total_queued_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.totalQueued")}</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_queued_size)}</span>
                    </div>
                  )}

                  {serverState?.average_time_queue !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.avgQueueTime")}</span>
                      <span className="ml-auto font-medium">{serverState.average_time_queue}ms</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v4 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.externalIPv4")}</span>
                      <span className={`ml-auto font-medium font-mono ${incognitoMode ? "blur-sm select-none" : ""}`} style={incognitoMode ? { filter: "blur(8px)" } : {}}>{serverState.last_external_address_v4}</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v6 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">{t("dashboard.serverStats.externalIPv6")}</span>
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

function GlobalStatsCards({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const { t } = useTranslation()
  const [speedUnit] = useSpeedUnits()
  const globalStats = useMemo(() => {
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
      alltimeDl,
      alltimeUl,
      globalRatio,
      totalPeers,
    }
  }, [statsData])

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                            <CardTitle className="text-sm font-medium">{t("common.titles.instances")}</CardTitle>          <HardDrive className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.connected}/{globalStats.total}</div>
          <p className="text-xs text-muted-foreground">
            {t("dashboard.globalStats.instances.description")}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("dashboard.globalStats.totalTorrents.title")}</CardTitle>
          <Activity className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.totalTorrents}</div>
          <p className="text-xs text-muted-foreground">
            {t("dashboard.globalStats.totalTorrents.description", { active: globalStats.activeTorrents, size: formatBytes(globalStats.totalSize) })}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("dashboard.globalStats.totalDownload.title")}</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalDownload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {t("dashboard.globalStats.allInstancesCombined")}
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">{t("dashboard.globalStats.totalUpload.title")}</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalUpload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {t("dashboard.globalStats.allInstancesCombined")}
          </p>
        </CardContent>
      </Card>
    </>
  )
}

function GlobalAllTimeStats({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const { t } = useTranslation()
  const [accordionValue, setAccordionValue] = usePersistedAccordionState("qui-global-stats-accordion")

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
                <h3 className="text-sm font-medium text-muted-foreground">{t("dashboard.serverStats.title")}</h3>
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
                  <span className="text-xs text-muted-foreground">{t("dashboard.serverStats.ratio")} </span>
                  <span className="font-semibold" style={{ color: ratioColor }}>
                    {globalStats.globalRatio.toFixed(2)}
                  </span>
                </div>
                {globalStats.totalPeers > 0 && (
                  <div>
                    <span className="text-xs text-muted-foreground">{t("dashboard.serverStats.peers")} </span>
                    <span className="font-semibold">{globalStats.totalPeers}</span>
                  </div>
                )}
              </div>
            </div>
          </div>

          {/* Desktop layout */}
          <div className="hidden sm:flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 w-full">
            <div className="flex items-center gap-2">
              <Plus className="h-4 w-4 text-muted-foreground group-data-[state=open]:hidden" />
              <Minus className="h-4 w-4 text-muted-foreground group-data-[state=closed]:hidden" />
              <h3 className="text-base font-medium">{t("dashboard.serverStats.title")}</h3>
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
                <span className="text-muted-foreground">{t("dashboard.serverStats.ratio")}</span>
                <span className="text-lg font-semibold" style={{ color: ratioColor }}>
                  {globalStats.globalRatio.toFixed(2)}
                </span>
              </div>

              {globalStats.totalPeers > 0 && (
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">{t("dashboard.serverStats.peers")}</span>
                  <span className="text-lg font-semibold">{globalStats.totalPeers}</span>
                </div>
              )}
            </div>
          </div>
        </AccordionTrigger>
        <AccordionContent className="px-0 pb-0">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/50">
                <TableHead className="text-center">{t("dashboard.instanceTable.instance")}</TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("dashboard.instanceTable.downloaded")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>{t("dashboard.instanceTable.uploaded")}</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">{t("dashboard.instanceTable.ratio")}</TableHead>
                <TableHead className="text-center hidden sm:table-cell">{t("dashboard.instanceTable.peers")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {statsData
                .filter(({ serverState }) => serverState?.alltime_dl || serverState?.alltime_ul)
                .map(({ instance, serverState }) => {
                  const instanceRatio = serverState?.alltime_dl ? (serverState.alltime_ul || 0) / serverState.alltime_dl : 0
                  const instanceRatioColor = getRatioColor(instanceRatio)

                  return (
                    <TableRow key={instance.id}>
                      <TableCell className="text-center font-medium">{instance.name}</TableCell>
                      <TableCell className="text-center font-semibold">
                        {formatBytes(serverState?.alltime_dl || 0)}
                      </TableCell>
                      <TableCell className="text-center font-semibold">
                        {formatBytes(serverState?.alltime_ul || 0)}
                      </TableCell>
                      <TableCell className="text-center font-semibold" style={{ color: instanceRatioColor }}>
                        {instanceRatio.toFixed(2)}
                      </TableCell>
                      <TableCell className="text-center font-semibold hidden sm:table-cell">
                        {serverState?.total_peer_connections !== undefined ? (serverState.total_peer_connections || 0) : "-"}
                      </TableCell>
                    </TableRow>
                  )
                })}
            </TableBody>
          </Table>
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}

function QuickActionsDropdown({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const { t } = useTranslation()
  const connectedInstances = statsData
    .filter(({ instance }) => instance?.connected)
    .map(({ instance }) => instance)

  if (connectedInstances.length === 0) {
    return null
  }

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="outline" size="sm" className="w-full sm:w-auto">
          <Zap className="h-4 w-4 mr-2" />
          {t("dashboard.quickActions.title")}
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>{t("dashboard.quickActions.addTorrent")}</DropdownMenuLabel>
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
              <span>{t("dashboard.quickActions.addTorrentTo", { name: instance.name })}</span>
            </DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Dashboard() {
  const { t } = useTranslation()
  const { instances, isLoading } = useInstances()
  const allInstances = instances || []
  const [isAdvancedMetricsOpen, setIsAdvancedMetricsOpen] = useState(false)

  // Use safe hook that always calls the same number of hooks
  const statsData = useAllInstanceStats(allInstances)

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
        <h1 className="text-2xl sm:text-3xl font-bold">{t("dashboard.title")}</h1>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mt-2">
          <p className="text-muted-foreground">
            {t("dashboard.description")}
          </p>
          {instances && instances.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 w-full sm:w-auto">
              <QuickActionsDropdown statsData={statsData} />
              <Link to="/instances" search={{ modal: "add-instance" }} className="w-full sm:w-auto">
                <Button variant="outline" size="sm" className="w-full sm:w-auto">
                  <Plus className="h-4 w-4 mr-2" />
              {t("instances.add")}
                </Button>
              </Link>
            </div>
          )}
        </div>
      </div>

      {/* Show banner if any instances have decryption errors */}
      <PasswordIssuesBanner instances={instances || []} />

      {instances && instances.length > 0 ? (
        <div className="space-y-6">
          {/* Stats Bar */}
          <GlobalAllTimeStats statsData={statsData} />

          {/* Global Stats */}
          <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
            <GlobalStatsCards statsData={statsData} />
          </div>


          {/* Instance Cards */}
          {allInstances.length > 0 && (
            <div>
              <h2 className="text-xl font-semibold mb-4">{t("common.titles.instances")}</h2>
              {/* Responsive layout so each instance mounts once */}
              <div className="flex flex-col gap-4 sm:grid sm:grid-cols-1 md:grid-cols-2 lg:grid-cols-3">
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
          )}
        </div>
      ) : (
        <Card className="p-8 sm:p-12 text-center">
          <div className="space-y-4">
            <HardDrive className="h-12 w-12 mx-auto text-muted-foreground" />
            <div>
                              <h3 className="text-lg font-semibold">{t("common.messages.noInstancesConfigured")}</h3>              <p className="text-muted-foreground">{t("dashboard.instances.getStarted")}</p>
            </div>
            <Link to="/instances" search={{ modal: "add-instance" }}>
              <Button>
                <Plus className="h-4 w-4 mr-2" />
                {t("instances.add")}
              </Button>
            </Link>
          </div>
        </Card>
      )}
    </div>
  )
}