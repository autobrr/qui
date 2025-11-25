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
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedAccordionState } from "@/hooks/usePersistedAccordionState"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { api } from "@/lib/api"
import { formatBytes, getRatioColor } from "@/lib/utils"
import type { InstanceResponse, ServerState, TorrentCounts, TorrentResponse, TorrentStats } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Activity, ArrowDown, ArrowUp, ArrowUpDown, Ban, BrickWallFire, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Download, ExternalLink, Eye, EyeOff, Globe, HardDrive, Info, Minus, Plus, Rabbit, RefreshCcw, Turtle, Upload, Zap } from "lucide-react"
import { useEffect, useMemo, useState } from "react"

import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"

import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { TrackerTransferStats } from "@/types"

interface DashboardInstanceStats {
  instance: InstanceResponse
  stats: TorrentStats | null
  serverState: ServerState | null
  torrentCounts?: TorrentCounts
  altSpeedEnabled: boolean
  isLoading: boolean
  error: unknown
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
  const { preferences } = useInstancePreferences(instance.id, { enabled: instance.connected })
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


  const isConnectable = normalizedConnectionStatus === "connected"
  const isFirewalled = normalizedConnectionStatus === "firewalled"
  const ConnectionStatusIcon = isConnectable ? Globe : isFirewalled ? BrickWallFire : Ban
  const connectionStatusIconClass = hasConnectionStatus? isConnectable? "text-green-500": isFirewalled? "text-amber-500": "text-destructive": ""

  const listenPort = preferences?.listen_port
  const connectionStatusTooltip = connectionStatusDisplay
    ? `${isConnectable ? "Connectable" : connectionStatusDisplay}${listenPort ? `. Port: ${listenPort}` : ""}`
    : ""

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
                className="text-lg truncate min-w-0 max-w-[100px] sm:max-w-[130px] md:max-w-[160px] lg:max-w-[190px]"
                title={instance.name}
              >
                {instance.name}
              </CardTitle>
              <ExternalLink className="h-3.5 w-3.5 text-muted-foreground shrink-0" />
            </Link>
            <div className="flex items-center gap-1 justify-end shrink-0 basis-full sm:basis-auto sm:min-w-[4.5rem]">
              {instance.reannounceSettings?.enabled && (
                <Tooltip>
                  <TooltipTrigger asChild>
                    <RefreshCcw className="h-4 w-4 text-green-600" />
                  </TooltipTrigger>
                  <TooltipContent>
                    Automatic tracker reannounce enabled
                  </TooltipContent>
                </Tooltip>
              )}
              {instance.connected && !isFirstLoad && (
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
                      className="h-8 w-8 p-0 shrink-0"
                    >
                      {altSpeedEnabled ? (
                        <Turtle className="h-4 w-4 text-orange-600" />
                      ) : (
                        <Rabbit className="h-4 w-4 text-green-600" />
                      )}
                    </Button>
                  </TooltipTrigger>
                  <TooltipContent>
                    Alternative speed limits: {altSpeedEnabled ? "On" : "Off"}
                  </TooltipContent>
                </Tooltip>
              )}
              <InstanceSettingsButton
                instanceId={instance.id}
                instanceName={instance.name}
                showButton={showSettingsButton}
              />
            </div>
          </div>

          <AlertDialog open={showSpeedLimitDialog} onOpenChange={setShowSpeedLimitDialog}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>
                  {altSpeedEnabled ? "Disable Alternative Speed Limits?" : "Enable Alternative Speed Limits?"}
                </AlertDialogTitle>
                <AlertDialogDescription>
                  {altSpeedEnabled? `This will disable alternative speed limits for ${instance.name} and return to normal speed limits.`: `This will enable alternative speed limits for ${instance.name}, which will reduce transfer speeds based on your configured limits.`
                  }
                </AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel>Cancel</AlertDialogCancel>
                <AlertDialogAction
                  onClick={() => {
                    toggleAltSpeed()
                    setShowSpeedLimitDialog(false)
                  }}
                >
                  {altSpeedEnabled ? "Disable" : "Enable"}
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
              {isFirstLoad && <p className="animate-pulse">Loading stats...</p>}
              {hasError && !isDisconnected && <p>Failed to load stats</p>}
              <InstanceErrorDisplay instance={instance} compact />
            </div>
          ) : (
            /* Show normal stats */
            <div className="space-y-2 sm:space-y-3">
              <div className="mb-3 sm:mb-6">
                <div className="flex items-center justify-center mb-1">
                  <span className="flex-1 text-center text-xs text-muted-foreground">Downloading</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">Active</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">Error</span>
                  <span className="flex-1 text-center text-xs text-muted-foreground">Total</span>
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
                  <span className="text-muted-foreground">Download</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalDownloadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <Upload className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">Upload</span>
                  <span className="ml-auto font-medium truncate">{formatSpeedWithUnit(stats?.totalUploadSpeed || 0, speedUnit)}</span>
                </div>

                <div className="flex items-center gap-2 text-xs">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span className="text-muted-foreground cursor-help inline-flex items-center gap-1">
                        Total Size
                      </span>
                    </TooltipTrigger>
                    <TooltipContent>
                      Total size of all torrents, including cross-seeds
                    </TooltipContent>
                  </Tooltip>
                  <span className="ml-auto font-medium truncate">{formatBytes(stats?.totalSize || 0)}</span>
                </div>
              </div>

              {serverState?.free_space_on_disk !== undefined && serverState.free_space_on_disk > 0 && (
                <div className="flex items-center gap-2 text-xs mt-1 sm:mt-2">
                  <HardDrive className="h-3 w-3 text-muted-foreground flex-shrink-0" />
                  <span className="text-muted-foreground">Free Space</span>
                  <span className="ml-auto font-medium truncate">{formatBytes(serverState.free_space_on_disk)}</span>
                </div>
              )}

              <Collapsible open={isAdvancedMetricsOpen} onOpenChange={setIsAdvancedMetricsOpen}>
                <CollapsibleTrigger className="flex items-center gap-2 text-xs text-muted-foreground hover:text-foreground transition-colors w-full">
                  {isAdvancedMetricsOpen ? (
                    <ChevronDown className="h-3 w-3" />
                  ) : (
                    <ChevronRight className="h-3 w-3" />
                  )}
                  <span>{`Show ${isAdvancedMetricsOpen ? "less" : "more"}`}</span>
                </CollapsibleTrigger>
                <CollapsibleContent className="space-y-2 mt-2">
                  {serverState?.total_peer_connections !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">Peer Connections</span>
                      <span className="ml-auto font-medium">{serverState.total_peer_connections || 0}</span>
                    </div>
                  )}

                  {serverState?.queued_io_jobs !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">Queued I/O Jobs</span>
                      <span className="ml-auto font-medium">{serverState.queued_io_jobs || 0}</span>
                    </div>
                  )}

                  {serverState?.total_buffers_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <HardDrive className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">Buffer Size</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_buffers_size)}</span>
                    </div>
                  )}

                  {serverState?.total_queued_size !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Activity className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">Total Queued</span>
                      <span className="ml-auto font-medium">{formatBytes(serverState.total_queued_size)}</span>
                    </div>
                  )}

                  {serverState?.average_time_queue !== undefined && (
                    <div className="flex items-center gap-2 text-xs">
                      <Zap className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">Avg Queue Time</span>
                      <span className="ml-auto font-medium">{serverState.average_time_queue}ms</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v4 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">External IPv4</span>
                      <span className={`ml-auto font-medium font-mono ${incognitoMode ? "blur-sm select-none" : ""}`} style={incognitoMode ? { filter: "blur(8px)" } : {}}>{serverState.last_external_address_v4}</span>
                    </div>
                  )}

                  {serverState?.last_external_address_v6 && (
                    <div className="flex items-center gap-2 text-xs">
                      <ExternalLink className="h-3 w-3 text-muted-foreground" />
                      <span className="text-muted-foreground">External IPv6</span>
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

function MobileGlobalStatsCard({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const [speedUnit] = useSpeedUnits()
  const globalStats = useGlobalStats(statsData)

  return (
    <Card className="sm:hidden">
      <CardHeader className="pb-3">
        <CardTitle className="text-sm font-medium">Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-3">
          {/* Instances */}
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <HardDrive className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Instances</span>
            </div>
            <div className="text-xl font-bold">{globalStats.connected}/{globalStats.total}</div>
            <p className="text-[10px] text-muted-foreground">Connected</p>
          </div>

          {/* Torrents */}
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Activity className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Torrents</span>
            </div>
            <div className="text-xl font-bold">{globalStats.totalTorrents}</div>
            <p className="text-[10px] text-muted-foreground">{globalStats.activeTorrents} active</p>
          </div>

          {/* Download */}
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Download className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Download</span>
            </div>
            <div className="text-xl font-bold">{formatSpeedWithUnit(globalStats.totalDownload, speedUnit)}</div>
            <p className="text-[10px] text-muted-foreground">{globalStats.downloadingTorrents} active</p>
          </div>

          {/* Upload */}
          <div className="space-y-1">
            <div className="flex items-center gap-1.5">
              <Upload className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">Upload</span>
            </div>
            <div className="text-xl font-bold">{formatSpeedWithUnit(globalStats.totalUpload, speedUnit)}</div>
            <p className="text-[10px] text-muted-foreground">{globalStats.seedingTorrents} active</p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}

function GlobalStatsCards({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const [speedUnit] = useSpeedUnits()
  const globalStats = useGlobalStats(statsData)

  return (
    <>
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Instances</CardTitle>
          <HardDrive className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.connected}/{globalStats.total}</div>
          <p className="text-xs text-muted-foreground">
            Connected instances
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Torrents</CardTitle>
          <Activity className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{globalStats.totalTorrents}</div>
          <p className="text-xs text-muted-foreground">
            {globalStats.activeTorrents} active - <span className="text-xs">{formatBytes(globalStats.totalSize)} total size</span>
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Download</CardTitle>
          <Download className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalDownload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {globalStats.downloadingTorrents} active - <span className="text-xs">{formatBytes(globalStats.totalRemainingSize)} remaining</span>
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
          <CardTitle className="text-sm font-medium">Total Upload</CardTitle>
          <Upload className="h-4 w-4 text-muted-foreground" />
        </CardHeader>
        <CardContent>
          <div className="text-2xl font-bold">{formatSpeedWithUnit(globalStats.totalUpload, speedUnit)}</div>
          <p className="text-xs text-muted-foreground">
            {globalStats.seedingTorrents} active - <span className="text-xs">{formatBytes(globalStats.totalSeedingSize)} seeding</span>
          </p>
        </CardContent>
      </Card>
    </>
  )
}

function GlobalAllTimeStats({ statsData }: { statsData: DashboardInstanceStats[] }) {
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
                <h3 className="text-sm font-medium text-muted-foreground">Server Statistics</h3>
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
                  <span className="text-xs text-muted-foreground">Ratio: </span>
                  <span className="font-semibold" style={{ color: ratioColor }}>
                    {globalStats.globalRatio.toFixed(2)}
                  </span>
                </div>
                {globalStats.totalPeers > 0 && (
                  <div>
                    <span className="text-xs text-muted-foreground">Peers: </span>
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
              <h3 className="text-base font-medium">Server Statistics</h3>
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
                <span className="text-muted-foreground">Ratio:</span>
                <span className="text-lg font-semibold" style={{ color: ratioColor }}>
                  {globalStats.globalRatio.toFixed(2)}
                </span>
              </div>

              {globalStats.totalPeers > 0 && (
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground">Peers:</span>
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
                <TableHead className="text-center">Instance</TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>Downloaded</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">
                  <div className="flex items-center justify-center gap-1">
                    <span>Uploaded</span>
                  </div>
                </TableHead>
                <TableHead className="text-center">Ratio</TableHead>
                <TableHead className="text-center hidden sm:table-cell">Peers</TableHead>
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

interface TrackerIconImageProps {
  tracker: string
  trackerIcons?: Record<string, string>
}

function TrackerIconImage({ tracker, trackerIcons }: TrackerIconImageProps) {
  const [hasError, setHasError] = useState(false)

  const trimmed = tracker.trim()
  const fallbackLetter = trimmed ? trimmed.charAt(0).toUpperCase() : "#"
  const src = trackerIcons?.[trimmed] ?? null

  return (
    <div className="flex h-4 w-4 items-center justify-center rounded-sm border border-border/40 bg-muted text-[10px] font-medium uppercase leading-none flex-shrink-0">
      {src && !hasError ? (
        <img
          src={src}
          alt=""
          className="h-full w-full object-contain rounded-sm"
          onError={() => setHasError(true)}
        />
      ) : (
        fallbackLetter
      )}
    </div>
  )
}

type TrackerSortColumn = "tracker" | "uploaded" | "downloaded" | "ratio" | "count" | "performance"
type SortDirection = "asc" | "desc"

function SortIcon({ column, sortColumn, sortDirection }: { column: TrackerSortColumn; sortColumn: TrackerSortColumn; sortDirection: SortDirection }) {
  if (sortColumn !== column) {
    return <ArrowUpDown className="h-3 w-3 text-muted-foreground/50" />
  }
  return sortDirection === "asc"
    ? <ArrowUp className="h-3 w-3" />
    : <ArrowDown className="h-3 w-3" />
}

function TrackerBreakdownCard({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const [accordionValue, setAccordionValue] = usePersistedAccordionState("qui-tracker-breakdown-accordion")
  const { data: trackerIcons } = useTrackerIcons()
  const [incognitoMode] = useIncognitoMode()
  const [sortColumn, setSortColumn] = useState<TrackerSortColumn>("uploaded")
  const [sortDirection, setSortDirection] = useState<SortDirection>("desc")

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
          existing.count += stats.count
        } else {
          aggregated.set(domain, { ...stats })
        }
      }
    }

    // convert to array
    return Array.from(aggregated.entries())
      .map(([domain, stats]) => ({ domain, ...stats }))
  }, [statsData])

  // sort the tracker stats based on current sort state
  const sortedTrackerStats = useMemo(() => {
    const sorted = [...trackerStats]
    const multiplier = sortDirection === "asc" ? 1 : -1

    sorted.sort((a, b) => {
      switch (sortColumn) {
        case "tracker":
          return multiplier * a.domain.localeCompare(b.domain)
        case "uploaded":
          return multiplier * (a.uploaded - b.uploaded)
        case "downloaded":
          return multiplier * (a.downloaded - b.downloaded)
        case "ratio": {
          const ratioA = a.downloaded > 0 ? a.uploaded / a.downloaded : (a.uploaded > 0 ? Infinity : 0)
          const ratioB = b.downloaded > 0 ? b.uploaded / b.downloaded : (b.uploaded > 0 ? Infinity : 0)
          return multiplier * (ratioA - ratioB)
        }
        case "count":
          return multiplier * (a.count - b.count)
        case "performance": {
          const perfA = a.count > 0 ? a.uploaded / a.count : 0
          const perfB = b.count > 0 ? b.uploaded / b.count : 0
          return multiplier * (perfA - perfB)
        }
        default:
          return 0
      }
    })

    return sorted
  }, [trackerStats, sortColumn, sortDirection])

  // pagination
  const ITEMS_PER_PAGE = 15
  const [page, setPage] = useState(0)
  const totalPages = Math.ceil(sortedTrackerStats.length / ITEMS_PER_PAGE)
  const paginatedTrackerStats = useMemo(() => {
    const start = page * ITEMS_PER_PAGE
    return sortedTrackerStats.slice(start, start + ITEMS_PER_PAGE)
  }, [sortedTrackerStats, page])

  // clamp page when data shrinks
  useEffect(() => {
    if (totalPages === 0) {
      setPage(0)
    } else if (page >= totalPages) {
      setPage(totalPages - 1)
    }
  }, [totalPages, page])

  // reset page when sort changes
  const handleSort = (column: TrackerSortColumn) => {
    setPage(0)
    if (sortColumn === column) {
      setSortDirection(prev => prev === "asc" ? "desc" : "asc")
    } else {
      setSortColumn(column)
      setSortDirection(column === "tracker" ? "asc" : "desc")
    }
  }

  // format performance as GiB/t consistently
  const formatPerformance = (uploaded: number, count: number): string => {
    if (count === 0) return "-"
    const bytesPerTorrent = uploaded / count
    const gibPerTorrent = bytesPerTorrent / (1024 * 1024 * 1024)
    return `${gibPerTorrent.toFixed(2)} GiB/t`
  }

  // don't show if no tracker data
  if (sortedTrackerStats.length === 0) {
    return null
  }

  return (
    <Accordion type="single" collapsible className="rounded-lg border bg-card" value={accordionValue} onValueChange={setAccordionValue}>
      <AccordionItem value="tracker-breakdown" className="border-0">
        <AccordionTrigger className="px-4 py-4 hover:no-underline hover:bg-muted/50 transition-colors [&>svg]:hidden group">
          {/* Mobile layout */}
          <div className="sm:hidden w-full">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-2">
                <Plus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=open]:hidden" />
                <Minus className="h-3.5 w-3.5 text-muted-foreground group-data-[state=closed]:hidden" />
                <h3 className="text-sm font-medium text-muted-foreground">Tracker Breakdown</h3>
              </div>
              <span className="text-xs text-muted-foreground">{sortedTrackerStats.length} trackers</span>
            </div>
          </div>

          {/* Desktop layout */}
          <div className="hidden sm:flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 w-full">
            <div className="flex items-center gap-2">
              <Plus className="h-4 w-4 text-muted-foreground group-data-[state=open]:hidden" />
              <Minus className="h-4 w-4 text-muted-foreground group-data-[state=closed]:hidden" />
              <h3 className="text-base font-medium">Tracker Breakdown</h3>
            </div>
            <span className="text-muted-foreground">{sortedTrackerStats.length} trackers</span>
          </div>
        </AccordionTrigger>
        <AccordionContent className="px-0 pb-0">
          {/* Mobile Sort Dropdown */}
          <div className="sm:hidden px-4 py-3 border-b">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="w-full justify-between">
                  <span className="flex items-center gap-2 text-xs">
                    Sort: {sortColumn === "tracker" ? "Tracker" :
                           sortColumn === "uploaded" ? "Uploaded" :
                           sortColumn === "downloaded" ? "Downloaded" :
                           sortColumn === "ratio" ? "Ratio" :
                           sortColumn === "count" ? "Torrents" : "Performance"}
                  </span>
                  {sortDirection === "asc" ? <ArrowUp className="h-3.5 w-3.5" /> : <ArrowDown className="h-3.5 w-3.5" />}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent className="w-full">
                <DropdownMenuItem onClick={() => handleSort("tracker")}>Tracker</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("uploaded")}>Uploaded</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("downloaded")}>Downloaded</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("ratio")}>Ratio</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("count")}>Torrents</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("performance")}>Performance</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>

          {/* Mobile Card Layout */}
          <div className="sm:hidden px-4 space-y-2 py-3">
            {paginatedTrackerStats.map(({ domain, uploaded, downloaded, count }) => {
              const isInfiniteRatio = downloaded === 0 && uploaded > 0
              const ratio = downloaded > 0 ? uploaded / downloaded : 0
              const ratioColor = isInfiniteRatio ? "var(--chart-1)" : getRatioColor(ratio)
              const displayDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain

              return (
                <Card key={domain} className="overflow-hidden">
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2 min-w-0 flex-1">
                        <TrackerIconImage tracker={displayDomain} trackerIcons={trackerIcons} />
                        <span className="font-medium truncate text-sm">{displayDomain}</span>
                      </div>
                      <Badge variant="secondary" className="ml-2 shrink-0 text-xs">
                        {count}
                      </Badge>
                    </div>
                  </CardHeader>
                  <CardContent className="pt-0">
                    <div className="grid grid-cols-2 gap-3">
                      {/* Uploaded */}
                      <div className="space-y-1">
                        <div className="flex items-center gap-1 text-xs text-muted-foreground">
                          <ChevronUp className="h-3 w-3" />
                          <span>Uploaded</span>
                        </div>
                        <div className="font-semibold text-sm">{formatBytes(uploaded)}</div>
                      </div>

                      {/* Downloaded */}
                      <div className="space-y-1">
                        <div className="flex items-center gap-1 text-xs text-muted-foreground">
                          <ChevronDown className="h-3 w-3" />
                          <span>Downloaded</span>
                        </div>
                        <div className="font-semibold text-sm">{formatBytes(downloaded)}</div>
                      </div>

                      {/* Ratio */}
                      <div className="space-y-1">
                        <div className="text-xs text-muted-foreground">Ratio</div>
                        <div className="font-semibold text-sm" style={{ color: ratioColor }}>
                          {isInfiniteRatio ? "∞" : ratio.toFixed(2)}
                        </div>
                      </div>

                      {/* Performance */}
                      <div className="space-y-1">
                        <div className="text-xs text-muted-foreground">Performance</div>
                        <div className="font-semibold text-sm">{formatPerformance(uploaded, count)}</div>
                      </div>
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>

          {/* Desktop Table */}
          <Table className="hidden sm:table">
            <TableHeader>
              <TableRow className="bg-muted/50">
                <TableHead className="w-[40%]">
                  <button
                    onClick={() => handleSort("tracker")}
                    className="flex items-center gap-1.5 hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Tracker
                    <SortIcon column="tracker" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    onClick={() => handleSort("uploaded")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Uploaded
                    <SortIcon column="uploaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    onClick={() => handleSort("downloaded")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Downloaded
                    <SortIcon column="downloaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    onClick={() => handleSort("ratio")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Ratio
                    <SortIcon column="ratio" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    onClick={() => handleSort("count")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Torrents
                    <SortIcon column="count" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right hidden lg:table-cell">
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <button
                        onClick={() => handleSort("performance")}
                        className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors"
                      >
                        Performance
                        <Info className="h-3.5 w-3.5 text-muted-foreground" />
                        <SortIcon column="performance" sortColumn={sortColumn} sortDirection={sortDirection} />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      <p className="text-xs">Upload per Torrent = Uploaded ÷ Torrent Count</p>
                    </TooltipContent>
                  </Tooltip>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {paginatedTrackerStats.map(({ domain, uploaded, downloaded, count }) => {
                const isInfiniteRatio = downloaded === 0 && uploaded > 0
                const ratio = downloaded > 0 ? uploaded / downloaded : 0
                const ratioColor = isInfiniteRatio ? "var(--chart-1)" : getRatioColor(ratio)
                const displayDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain

                return (
                  <TableRow key={domain} className="hover:bg-muted/50 transition-colors">
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <TrackerIconImage tracker={displayDomain} trackerIcons={trackerIcons} />
                        <span className="font-medium truncate">{displayDomain}</span>
                      </div>
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      {formatBytes(uploaded)}
                    </TableCell>
                    <TableCell className="text-right font-semibold">
                      {formatBytes(downloaded)}
                    </TableCell>
                    <TableCell className="text-right font-semibold" style={{ color: ratioColor }}>
                      {isInfiniteRatio ? "∞" : ratio.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right">
                      {count}
                    </TableCell>
                    <TableCell className="text-right hidden lg:table-cell font-semibold">
                      {formatPerformance(uploaded, count)}
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
                {page * ITEMS_PER_PAGE + 1}-{Math.min((page + 1) * ITEMS_PER_PAGE, sortedTrackerStats.length)} of {sortedTrackerStats.length} trackers
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPage(p => Math.max(0, p - 1))}
                  disabled={page === 0}
                >
                  <ChevronLeft className="h-4 w-4" />
                  <span className="hidden sm:inline ml-1">Previous</span>
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setPage(p => Math.min(totalPages - 1, p + 1))}
                  disabled={page >= totalPages - 1}
                >
                  <span className="hidden sm:inline mr-1">Next</span>
                  <ChevronRight className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}
        </AccordionContent>
      </AccordionItem>
    </Accordion>
  )
}

function QuickActionsDropdown({ statsData }: { statsData: DashboardInstanceStats[] }) {
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
          Quick Actions
          <ChevronDown className="h-3 w-3 ml-1" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>Add Torrent</DropdownMenuLabel>
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
              <span>Add to {instance.name}</span>
            </DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function Dashboard() {
  const { instances, isLoading } = useInstances()
  const allInstances = instances || []
  const activeInstances = allInstances.filter(instance => instance.isActive)
  const hasInstances = allInstances.length > 0
  const hasActiveInstances = activeInstances.length > 0
  const [isAdvancedMetricsOpen, setIsAdvancedMetricsOpen] = useState(false)

  // Use safe hook that always calls the same number of hooks
  const statsData = useAllInstanceStats(activeInstances)

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
        <h1 className="text-2xl sm:text-3xl font-bold">Dashboard</h1>
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 mt-2">
          <p className="text-muted-foreground">
            Overview of all your qBittorrent instances
          </p>
          {instances && instances.length > 0 && (
            <div className="flex flex-col sm:flex-row gap-2 w-full sm:w-auto">
              <QuickActionsDropdown statsData={statsData} />
              <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }} className="w-full sm:w-auto">
                <Button variant="outline" size="sm" className="w-full sm:w-auto">
                  <Plus className="h-4 w-4 mr-2" />
                  Add Instance
                </Button>
              </Link>
            </div>
          )}
        </div>
      </div>

      {/* Show banner if any instances have decryption errors */}
      <PasswordIssuesBanner instances={instances || []} />

      {hasInstances ? (
        <div className="space-y-6">
          {hasActiveInstances ? (
            <>
              {/* Stats Bar */}
              <GlobalAllTimeStats statsData={statsData} />

              {/* Tracker Breakdown */}
              <TrackerBreakdownCard statsData={statsData} />

              {/* Global Stats */}
              <div className="space-y-4">
                {/* Mobile: Single combined card */}
                <MobileGlobalStatsCard statsData={statsData} />

                {/* Tablet/Desktop: Separate cards */}
                <div className="hidden sm:grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                  <GlobalStatsCards statsData={statsData} />
                </div>
              </div>

              {/* Instance Cards */}
              <div>
                <h2 className="text-xl font-semibold mb-4">Instances</h2>
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
            </>
          ) : (
            <Card className="p-8 text-center">
              <div className="space-y-3">
                <h3 className="text-lg font-semibold">All instances are disabled</h3>
                <p className="text-muted-foreground">
                  Enable an instance from Settings → Instances to see dashboard stats.
                </p>
                <Link to="/settings" search={{ tab: "instances" as const }}>
                  <Button variant="outline" size="sm">
                    Manage Instances
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
              <h3 className="text-lg font-semibold">No instances configured</h3>
              <p className="text-muted-foreground">Get started by adding your first qBittorrent instance</p>
            </div>
            <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>
              <Button>
                <Plus className="h-4 w-4 mr-2" />
                Add Instance
              </Button>
            </Link>
          </div>
        </Card>
      )}
    </div>
  )
}
