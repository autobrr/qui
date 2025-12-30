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
import { Checkbox } from "@/components/ui/checkbox"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useInstances } from "@/hooks/useInstances"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { api } from "@/lib/api"
import { copyTextToClipboard, formatBytes, getRatioColor } from "@/lib/utils"
import type { InstanceResponse, ServerState, TorrentCounts, TorrentResponse, TorrentStats } from "@/types"
import { useMutation, useQueries, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { Activity, AlertCircle, AlertTriangle, ArrowDown, ArrowUp, ArrowUpDown, Ban, BrickWallFire, ChevronDown, ChevronLeft, ChevronRight, ChevronUp, Download, ExternalLink, Eye, EyeOff, Globe, HardDrive, Info, Link2, Minus, Pencil, Plus, Rabbit, RefreshCcw, Trash2, Turtle, Upload, X, Zap } from "lucide-react"
import { useEffect, useMemo, useState } from "react"
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
import { DEFAULT_DASHBOARD_SETTINGS, useDashboardSettings, useUpdateDashboardSettings } from "@/hooks/useDashboardSettings"
import { useCreateTrackerCustomization, useDeleteTrackerCustomization, useTrackerCustomizations, useUpdateTrackerCustomization } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { DashboardSettings, TrackerCustomization, TrackerTransferStats } from "@/types"

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
              <div className="mb-3 sm:mb-4">
                {/* Main stats row */}
                <div className="flex items-center justify-around text-center">
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.status?.downloading || 0}</div>
                    <div className="text-xs text-muted-foreground">Downloading</div>
                  </div>
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.status?.active || 0}</div>
                    <div className="text-xs text-muted-foreground">Active</div>
                  </div>
                  <div>
                    <div className="text-base sm:text-lg font-semibold">{torrentCounts?.total || 0}</div>
                    <div className="text-xs text-muted-foreground">Total</div>
                  </div>
                </div>
              </div>

              <div className="grid grid-cols-1 sm:grid-cols-1 gap-1 sm:gap-2">
                {/* Issue rows - only shown when there are problems */}
                {(torrentCounts?.status?.unregistered || 0) > 0 && (
                  <div className="flex items-center gap-2 text-xs">
                    <AlertTriangle className="h-3 w-3 text-destructive flex-shrink-0" />
                    <span className="text-destructive">Unregistered torrents</span>
                    <span className="ml-auto font-medium text-destructive">{torrentCounts?.status?.unregistered}</span>
                  </div>
                )}
                {(torrentCounts?.status?.tracker_down || 0) > 0 && (
                  <div className="flex items-center gap-2 text-xs">
                    <AlertCircle className="h-3 w-3 text-yellow-500 flex-shrink-0" />
                    <span className="text-yellow-500">Tracker Down</span>
                    <span className="ml-auto font-medium text-yellow-500">{torrentCounts?.status?.tracker_down}</span>
                  </div>
                )}
                {(torrentCounts?.status?.errored || 0) > 0 && (
                  <div className="flex items-center gap-2 text-xs">
                    <AlertTriangle className="h-3 w-3 text-destructive flex-shrink-0" />
                    <span className="text-destructive">Errors</span>
                    <span className="ml-auto font-medium text-destructive">{torrentCounts?.status?.errored}</span>
                  </div>
                )}

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

              {serverState?.free_space_on_disk !== undefined && (
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

interface GlobalAllTimeStatsProps {
  statsData: DashboardInstanceStats[]
  isCollapsed: boolean
  onCollapsedChange: (collapsed: boolean) => void
}

function GlobalAllTimeStats({ statsData, isCollapsed, onCollapsedChange }: GlobalAllTimeStatsProps) {
  // Accordion value is "server-stats" when expanded, "" when collapsed
  const accordionValue = isCollapsed ? "" : "server-stats"
  const setAccordionValue = (value: string) => onCollapsedChange(value === "")

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


type TrackerSortColumn = "tracker" | "uploaded" | "downloaded" | "ratio" | "buffer" | "count" | "size" | "performance"
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
  return sortDirection === "asc"
    ? <ArrowUp className="h-3 w-3" />
    : <ArrowDown className="h-3 w-3" />
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
            existing.totalSize += stats.totalSize
            existing.count += stats.count
          }
        }
      }
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

  const clearSelection = () => {
    setSelectedDomains(new Set())
  }

  // Save customization (create or update)
  const handleSaveCustomization = () => {
    if (!customizeDisplayName.trim()) return

    const domains = editingCustomization
      ? editingCustomization.domains
      : Array.from(selectedDomains)

    if (domains.length === 0) return

    // Get included domains from state (secondary domains that contribute to stats)
    const included = editingCustomization
      ? editingCustomization.includedInStats
      : Array.from(includedInStats)

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
      includedInStats: fullCustomization?.includedInStats ?? []
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
      const newIncluded = include
        ? [...editingCustomization.includedInStats.filter(d => d.toLowerCase() !== domainLower), domain]
        : editingCustomization.includedInStats.filter(d => d.toLowerCase() !== domainLower)
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
      toast.error("No customizations to export")
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
      toast.success("Copied to clipboard")
    } catch (error) {
      console.error("[Export] Failed to copy to clipboard:", error)
      toast.error("Failed to copy to clipboard")
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
        return { valid: false, entries: [], error: "Invalid format: expected trackerCustomizations array" }
      }

      // Validate each entry
      for (const entry of entries) {
        if (!entry.displayName || typeof entry.displayName !== "string") {
          return { valid: false, entries: [], error: "Invalid entry: missing displayName" }
        }
        if (!Array.isArray(entry.domains) || entry.domains.length === 0) {
          return { valid: false, entries: [], error: "Invalid entry: domains must be a non-empty array" }
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
          isIdentical
        }
      })

      return { valid: true, entries: entriesWithConflicts, error: null }
    } catch {
      return { valid: false, entries: [], error: "Invalid JSON" }
    }
  }, [importJson, customizations])

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
      toast.error(`Failed to import: ${failed.join(", ")}`)
    } else if (imported > 0 && skipped > 0) {
      toast.success(`Imported ${imported}, skipped ${skipped}`)
    } else if (imported > 0) {
      toast.success(`Imported ${imported} customization${imported !== 1 ? "s" : ""}`)
    } else {
      toast.info("No customizations imported")
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
                <TooltipContent>Import customizations</TooltipContent>
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
                <TooltipContent>Export customizations</TooltipContent>
              </Tooltip>
              <span className="text-muted-foreground ml-1">{sortedTrackerStats.length} trackers</span>
            </div>
          </div>
        </AccordionTrigger>
        <AccordionContent className="px-0 pb-0">
          {/* Mobile Sort Dropdown and Import/Export */}
          <div className="sm:hidden px-4 py-3 border-b flex items-center gap-2">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="flex-1 justify-between">
                  <span className="flex items-center gap-2 text-xs">
                    Sort: {sortColumn === "tracker" ? "Tracker" :
                           sortColumn === "uploaded" ? "Uploaded" :
                           sortColumn === "downloaded" ? "Downloaded" :
                           sortColumn === "ratio" ? "Ratio" :
                           sortColumn === "count" ? "Torrents" :
                           sortColumn === "size" ? "Size" : "Seeded"}
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
                <DropdownMenuItem onClick={() => handleSort("size")}>Size</DropdownMenuItem>
                <DropdownMenuItem onClick={() => handleSort("performance")}>Seeded</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button variant="ghost" size="sm" onClick={openImportDialog} className="h-8 px-2">
              <Download className="h-4 w-4" />
            </Button>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleExport}
              disabled={!customizations || customizations.length === 0}
              className="h-8 px-2"
            >
              <Upload className="h-4 w-4" />
            </Button>
          </div>


          {/* Mobile Card Layout */}
          <div className="sm:hidden px-4 space-y-2 py-3">
            {paginatedTrackerStats.map((tracker) => {
              const { domain, displayName, originalDomains, uploaded, downloaded, totalSize, count, customizationId } = tracker
              const { isInfinite, ratio, color: ratioColor } = getTrackerRatioDisplay(uploaded, downloaded)
              const displayValue = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
              const iconDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
              const isSelected = selectedDomains.has(domain)
              const isMerged = originalDomains.length > 1
              const hasCustomization = Boolean(customizationId)

              return (
                <Card key={displayName} className={`overflow-hidden ${isSelected ? "ring-2 ring-primary" : ""}`}>
                  <CardHeader className="pb-3">
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2 min-w-0 flex-1">
                        {!hasCustomization && (
                          <Checkbox
                            checked={isSelected}
                            onCheckedChange={() => toggleSelection(domain)}
                            className="shrink-0"
                          />
                        )}
                        <TrackerIconImage tracker={iconDomain} trackerIcons={trackerIcons} />
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="font-medium truncate text-sm cursor-default">
                              {displayValue}
                            </span>
                          </TooltipTrigger>
                          {(isMerged || (hasCustomization && displayName !== domain)) && (
                            <TooltipContent>
                              <p className="text-xs">
                                {isMerged ? `Merged from: ${originalDomains.join(", ")}` : `Original: ${domain}`}
                              </p>
                            </TooltipContent>
                          )}
                        </Tooltip>
                        {isMerged && <Link2 className="h-3 w-3 text-muted-foreground shrink-0" />}
                      </div>
                      <div className="flex items-center gap-1">
                        {hasCustomization && customizationId ? (
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
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            className="h-6 w-6 p-0"
                            onClick={(e) => { e.stopPropagation(); openRenameDialog(domain) }}
                          >
                            {selectedDomains.size > 0 ? (
                              <Link2 className="h-3 w-3 text-primary" />
                            ) : (
                              <Pencil className="h-3 w-3 text-muted-foreground" />
                            )}
                          </Button>
                        )}
                        <Badge variant="secondary" className="shrink-0 text-xs">
                          {count}
                        </Badge>
                      </div>
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
                          {isInfinite ? "∞" : ratio.toFixed(2)}
                        </div>
                      </div>

                      {/* Size */}
                      <div className="space-y-1">
                        <div className="text-xs text-muted-foreground">Size</div>
                        <div className="font-semibold text-sm">{formatBytes(totalSize)}</div>
                      </div>

                      {/* Seeded */}
                      <div className="space-y-1">
                        <div className="text-xs text-muted-foreground">Seeded</div>
                        <div className="font-semibold text-sm">{formatEfficiency(uploaded, totalSize)}</div>
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
                <TableHead className="w-8 pl-4" />
                <TableHead className="w-[35%]">
                  <button
                    type="button"
                    onClick={() => handleSort("tracker")}
                    className="flex items-center gap-1.5 hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Tracker
                    <SortIcon column="tracker" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    type="button"
                    onClick={() => handleSort("uploaded")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Uploaded
                    <SortIcon column="uploaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    type="button"
                    onClick={() => handleSort("downloaded")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Downloaded
                    <SortIcon column="downloaded" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    type="button"
                    onClick={() => handleSort("ratio")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Ratio
                    <SortIcon column="ratio" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right hidden lg:table-cell">
                  <button
                    type="button"
                    onClick={() => handleSort("buffer")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Buffer
                    <SortIcon column="buffer" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right">
                  <button
                    type="button"
                    onClick={() => handleSort("count")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Torrents
                    <SortIcon column="count" sortColumn={sortColumn} sortDirection={sortDirection} />
                  </button>
                </TableHead>
                <TableHead className="text-right hidden lg:table-cell">
                  <button
                    type="button"
                    onClick={() => handleSort("size")}
                    className="flex items-center gap-1.5 ml-auto hover:text-foreground transition-colors rounded px-1 py-0.5 -mx-1 -my-0.5"
                  >
                    Size
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
                        Seeded
                        <Info className="h-3.5 w-3.5 text-muted-foreground" />
                        <SortIcon column="performance" sortColumn={sortColumn} sortDirection={sortDirection} />
                      </button>
                    </TooltipTrigger>
                    <TooltipContent side="top">
                      <p className="text-xs">Uploaded ÷ Content Size — how many times you&apos;ve seeded your content</p>
                    </TooltipContent>
                  </Tooltip>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {paginatedTrackerStats.map((tracker, index) => {
                const { domain, displayName, originalDomains, uploaded, downloaded, totalSize, count, customizationId } = tracker
                const { isInfinite, ratio, color: ratioColor } = getTrackerRatioDisplay(uploaded, downloaded)
                const displayValue = incognitoMode ? getLinuxTrackerDomain(displayName) : displayName
                const iconDomain = incognitoMode ? getLinuxTrackerDomain(domain) : domain
                const isSelected = selectedDomains.has(domain)
                const isMerged = originalDomains.length > 1
                const hasCustomization = Boolean(customizationId)
                const buffer = uploaded - downloaded
                const uploadPercent = totalUploaded > 0 ? (uploaded / totalUploaded) * 100 : 0

                return (
                  <TableRow
                    key={displayName}
                    className={`group ${isSelected ? "bg-primary/5" : index % 2 === 1 ? "bg-muted/30" : ""} hover:bg-muted/50`}
                  >
                    <TableCell className="w-8 pl-4">
                      {!hasCustomization && (
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
                                {isMerged ? `Merged from: ${originalDomains.join(", ")}` : `Original: ${domain}`}
                              </p>
                            </TooltipContent>
                          )}
                        </Tooltip>
                        {isMerged && <Link2 className="h-3 w-3 text-muted-foreground shrink-0" />}
                        <div className="flex items-center gap-0.5 ml-auto opacity-0 group-hover:opacity-100 shrink-0">
                          {hasCustomization && customizationId ? (
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
                          ) : (
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  className="h-6 w-6 p-0"
                                  onClick={(e) => { e.stopPropagation(); openRenameDialog(domain) }}
                                >
                                  {selectedDomains.size > 0 ? (
                                    <Link2 className="h-3 w-3 text-primary" />
                                  ) : (
                                    <Pencil className="h-3 w-3 text-muted-foreground" />
                                  )}
                                </Button>
                              </TooltipTrigger>
                              <TooltipContent>
                                {selectedDomains.size > 0 ? "Add to merge" : "Rename"}
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
                      {formatBytes(downloaded)}
                    </TableCell>
                    <TableCell className="text-right font-semibold" style={{ color: ratioColor }}>
                      {isInfinite ? "∞" : ratio.toFixed(2)}
                    </TableCell>
                    <TableCell className="text-right hidden lg:table-cell font-semibold">
                      <span
                        className={buffer < 0 ? "text-destructive" : ""}
                        style={buffer >= 0 ? { color: "oklch(0.7040 0.1910 142)" } : undefined}
                      >
                        {buffer >= 0 ? "+" : "-"}{formatBytes(Math.abs(buffer))}
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
                {page * itemsPerPage + 1}-{Math.min((page + 1) * itemsPerPage, sortedTrackerStats.length)} of {sortedTrackerStats.length} trackers
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

      {/* Customize Dialog (Rename/Merge/Edit) */}
      <Dialog open={showCustomizeDialog} onOpenChange={(open) => !open && closeCustomizeDialog()}>
        <DialogContent className="max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle>
              {editingCustomization
                ? "Edit Tracker Name"
                : selectedDomains.size === 1
                  ? "Rename Tracker"
                  : "Merge Trackers"}
            </DialogTitle>
            <DialogDescription>
              {editingCustomization
                ? "Update the display name for this tracker."
                : selectedDomains.size === 1
                  ? "Give this tracker a custom display name."
                  : "Combine these trackers into a single entry with a custom name."}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4 min-h-0 flex-1 flex flex-col">
            <div className="space-y-2">
              <Label htmlFor="customize-name">Display Name</Label>
              <Input
                id="customize-name"
                value={customizeDisplayName}
                onChange={(e) => setCustomizeDisplayName(e.target.value)}
                placeholder="e.g., TorrentLeech"
              />
            </div>
            <div className="space-y-2 min-h-0 flex-1 flex flex-col overflow-hidden">
              <Label>{editingCustomization ? "Domain(s)" : "Selected Tracker(s)"}</Label>
              {((editingCustomization && editingCustomization.domains.length > 1) || (!editingCustomization && selectedDomains.size > 1)) && (
                <p className="text-xs text-muted-foreground">
                  Uncheck duplicate tracker URLs to avoid counting the same torrents twice.
                </p>
              )}
              <ScrollArea className="h-[300px]">
                  <div className="text-sm text-muted-foreground space-y-1.5 pr-4">
                    {(editingCustomization ? editingCustomization.domains : Array.from(selectedDomains)).map((domain, index, arr) => {
                    const hasMultiple = arr.length > 1
                    const isPrimary = index === 0
                    // Get inclusion state from appropriate source
                    // Primary is always included; secondary domains only if in includedInStats
                    const currentIncluded = editingCustomization
                      ? editingCustomization.includedInStats
                      : Array.from(includedInStats)
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
                            <Badge variant="secondary" className="text-[10px]">Primary</Badge>
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
          <DialogFooter>
            <Button variant="outline" onClick={closeCustomizeDialog}>
              Cancel
            </Button>
            <Button
              onClick={handleSaveCustomization}
              disabled={!customizeDisplayName.trim() || createCustomization.isPending || updateCustomization.isPending}
            >
              {(createCustomization.isPending || updateCustomization.isPending)
                ? "Saving..."
                : editingCustomization
                  ? "Save"
                  : selectedDomains.size === 1
                    ? "Rename"
                    : "Merge"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Import Dialog */}
      <Dialog open={showImportDialog} onOpenChange={setShowImportDialog}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Import Tracker Customizations</DialogTitle>
            <DialogDescription>
              Paste JSON to import tracker customizations (renames and merges).
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="import-json">JSON Data</Label>
              <Textarea
                id="import-json"
                value={importJson}
                onChange={(e) => setImportJson(e.target.value)}
                placeholder={`{\n  "trackerCustomizations": [\n    { "displayName": "Name", "domains": ["domain.com"] }\n  ]\n}`}
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
                            {newEntries.length > 0 && <span>{newEntries.length} new</span>}
                            {newEntries.length > 0 && (conflicts.length > 0 || identicalEntries.length > 0) && <span>, </span>}
                            {conflicts.length > 0 && <span className="text-yellow-600">{conflicts.length} conflict{conflicts.length !== 1 ? "s" : ""}</span>}
                            {conflicts.length > 0 && identicalEntries.length > 0 && <span>, </span>}
                            {identicalEntries.length > 0 && <span className="text-muted-foreground">{identicalEntries.length} unchanged</span>}
                          </div>
                          {conflicts.length > 0 && (
                            <>
                              <Label>Resolve conflicts</Label>
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
                                          Conflicts with: {entry.conflict?.displayName}
                                        </div>
                                      </div>
                                      <div className="flex items-center gap-1 shrink-0">
                                        <Button
                                          variant={importConflicts.get(entry.index) === "skip" ? "secondary" : "ghost"}
                                          size="sm"
                                          className="h-6 px-2 text-xs"
                                          onClick={() => setImportConflicts(new Map(importConflicts).set(entry.index, "skip"))}
                                        >
                                          Skip
                                        </Button>
                                        <Button
                                          variant={importConflicts.get(entry.index) === "overwrite" ? "secondary" : "ghost"}
                                          size="sm"
                                          className="h-6 px-2 text-xs"
                                          onClick={() => setImportConflicts(new Map(importConflicts).set(entry.index, "overwrite"))}
                                        >
                                          Overwrite
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
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowImportDialog(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleImport}
              disabled={!parseImportJson.valid || !allConflictsResolved || createCustomization.isPending || updateCustomization.isPending}
            >
              {(createCustomization.isPending || updateCustomization.isPending) ? "Importing..." : "Import"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
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

  // Dashboard settings
  const { data: dashboardSettings } = useDashboardSettings()
  const updateSettings = useUpdateDashboardSettings()
  const settings = dashboardSettings || DEFAULT_DASHBOARD_SETTINGS

  // Use safe hook that always calls the same number of hooks
  const statsData = useAllInstanceStats(activeInstances)

  // Handler for TrackerBreakdownCard to update settings
  const handleTrackerSettingsChange = (input: { trackerBreakdownSortColumn?: string; trackerBreakdownSortDirection?: string; trackerBreakdownItemsPerPage?: number }) => {
    updateSettings.mutate(input)
  }

  // Handler for section collapsed state changes
  const handleSectionCollapsedChange = (sectionId: string, collapsed: boolean) => {
    updateSettings.mutate({
      sectionCollapsed: { ...settings.sectionCollapsed, [sectionId]: collapsed }
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
              <DashboardSettingsDialog />
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
                        <MobileGlobalStatsCard statsData={statsData} />
                        {/* Tablet/Desktop: Separate cards */}
                        <div className="hidden sm:grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
                          <GlobalStatsCards statsData={statsData} />
                        </div>
                      </div>
                    )
                  case "instances":
                    return (
                      <div key={sectionId}>
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
                    )
                  default:
                    return null
                }
              })}
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
