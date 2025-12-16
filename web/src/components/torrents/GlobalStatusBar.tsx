/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  ArrowUpDown,
  Ban,
  BrickWallFire,
  ChevronDown,
  ChevronUp,
  EthernetPort,
  Eye,
  EyeOff,
  Globe,
  HardDrive,
  LayoutGrid,
  Loader2,
  Rabbit,
  RefreshCcw,
  Rows3,
  Table as TableIcon,
  Turtle,
} from "lucide-react"
import { memo, useCallback, useEffect, useMemo, useState } from "react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@/components/ui/tooltip"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { api } from "@/lib/api"
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { cn, formatBytes } from "@/lib/utils"
import type { Instance, ServerState } from "@/types"

const TABLE_ALLOWED_VIEW_MODES = ["normal", "dense", "compact"] as const

export interface SelectionInfo {
  effectiveSelectionCount: number
  isAllSelected: boolean
  excludedFromSelectAllSize: number
  selectedFormattedSize: string
  torrentsLength: number
  totalCount: number
  hasLoadedAll: boolean
  isLoading: boolean
  isLoadingMore: boolean
  isCachedData: boolean
  isStaleData: boolean
  emptyStateMessage: string
  safeLoadedRows: number
  rowsLength: number
}

interface ExternalIPAddressProps {
  address?: string | null
  incognitoMode: boolean
  label: string
}

const ExternalIPAddress = memo(
  ({ address, incognitoMode, label }: ExternalIPAddressProps) => {
    if (!address) return null

    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant="outline"
            className="gap-1 px-1.5 py-0.5 text-[11px] leading-none text-muted-foreground"
            aria-label={`External ${label}`}
          >
            <EthernetPort className="h-3.5 w-3.5 text-muted-foreground" />
            <span>{label}</span>
          </Badge>
        </TooltipTrigger>
        <TooltipContent>
          <p className="font-mono text-xs">
            <span {...(incognitoMode && { style: { filter: "blur(4px)" } })}>{address}</span>
          </p>
        </TooltipContent>
      </Tooltip>
    )
  },
  (prev, next) =>
    prev.address === next.address &&
    prev.incognitoMode === next.incognitoMode &&
    prev.label === next.label
)

interface GlobalStatusBarProps {
  instanceId: number
  serverState: ServerState | null
  instance?: Instance | null
  listenPort?: number | null
  selectionInfo?: SelectionInfo | null
}

export const GlobalStatusBar = memo(function GlobalStatusBar({
  instanceId,
  serverState,
  instance,
  listenPort,
  selectionInfo,
}: GlobalStatusBarProps) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit, setSpeedUnit] = useSpeedUnits()
  const { viewMode: desktopViewMode, cycleViewMode } = usePersistedCompactViewState("normal", TABLE_ALLOWED_VIEW_MODES)

  // Detect platform for keyboard shortcuts
  const isMac = useMemo(() => {
    return typeof window !== "undefined" && /Mac|iPhone|iPad|iPod/.test(window.navigator.userAgent)
  }, [])

  // Alt speed toggle state
  const [altSpeedOverride, setAltSpeedOverride] = useState<boolean | null>(null)
  const serverAltSpeedEnabled = serverState?.use_alt_speed_limits
  const hasAltSpeedStatus = typeof serverAltSpeedEnabled === "boolean"
  const isAltSpeedKnown = altSpeedOverride !== null || hasAltSpeedStatus
  const altSpeedEnabled = altSpeedOverride ?? serverAltSpeedEnabled ?? false
  const AltSpeedIcon = altSpeedEnabled ? Turtle : Rabbit
  const altSpeedIconClass = isAltSpeedKnown ? altSpeedEnabled ? "text-destructive" : "text-green-500" : "text-muted-foreground"

  useEffect(() => {
    setAltSpeedOverride(null)
  }, [instanceId])

  const { mutateAsync: toggleAltSpeedLimits, isPending: isTogglingAltSpeed } = useMutation({
    mutationFn: () => api.toggleAlternativeSpeedLimits(instanceId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId] })
      queryClient.invalidateQueries({ queryKey: ["alternative-speed-limits", instanceId] })
    },
  })

  useEffect(() => {
    if (altSpeedOverride === null) {
      return
    }

    if (serverAltSpeedEnabled === altSpeedOverride) {
      setAltSpeedOverride(null)
    }
  }, [serverAltSpeedEnabled, altSpeedOverride])

  const handleToggleAltSpeedLimits = useCallback(async () => {
    if (isTogglingAltSpeed) {
      return
    }

    const current = altSpeedOverride ?? serverAltSpeedEnabled ?? false
    const next = !current

    setAltSpeedOverride(next)

    try {
      await toggleAltSpeedLimits()
    } catch {
      setAltSpeedOverride(current)
    }
  }, [altSpeedOverride, serverAltSpeedEnabled, toggleAltSpeedLimits, isTogglingAltSpeed])

  const altSpeedTooltip = isAltSpeedKnown ? altSpeedEnabled ? "Alternative speed limits: On" : "Alternative speed limits: Off" : "Alternative speed limits status unknown"
  const altSpeedAriaLabel = isAltSpeedKnown ? altSpeedEnabled ? "Disable alternative speed limits" : "Enable alternative speed limits" : "Alternative speed limits status unknown"

  // Connection status
  const rawConnectionStatus = serverState?.connection_status ?? ""
  const normalizedConnectionStatus = rawConnectionStatus ? rawConnectionStatus.trim().toLowerCase() : ""
  const formattedConnectionStatus = normalizedConnectionStatus ? normalizedConnectionStatus.replace(/_/g, " ") : ""
  const connectionStatusDisplay = formattedConnectionStatus ? formattedConnectionStatus.replace(/\b\w/g, (char: string) => char.toUpperCase()) : ""
  const hasConnectionStatus = Boolean(formattedConnectionStatus)
  const isConnectable = normalizedConnectionStatus === "connected"
  const isFirewalled = normalizedConnectionStatus === "firewalled"
  const ConnectionStatusIcon = isConnectable ? Globe : isFirewalled ? BrickWallFire : hasConnectionStatus ? Ban : Globe
  const connectionStatusTooltip = hasConnectionStatus
    ? `${isConnectable ? "Connectable" : connectionStatusDisplay}${listenPort ? `. Port: ${listenPort}` : ""}`
    : "Connection status unknown"
  const connectionStatusIconClass = hasConnectionStatus ? isConnectable ? "text-green-500" : isFirewalled ? "text-amber-500" : "text-destructive" : "text-muted-foreground"
  const connectionStatusAriaLabel = hasConnectionStatus ? `qBittorrent connection status: ${connectionStatusDisplay || formattedConnectionStatus}` : "qBittorrent connection status unknown"

  return (
    <div className="flex flex-wrap items-center justify-between gap-2 px-2 py-1.5 border-t flex-shrink-0 select-none text-xs">
      {/* Left: Selection/Count Info */}
      <div className="text-muted-foreground min-w-[200px]">
        {selectionInfo ? (
          selectionInfo.effectiveSelectionCount > 0 ? (
            <>
              <span>
                {selectionInfo.isAllSelected && selectionInfo.excludedFromSelectAllSize === 0 ? "All" : selectionInfo.effectiveSelectionCount} selected
                {selectionInfo.selectedFormattedSize && <> • {selectionInfo.selectedFormattedSize}</>}
              </span>
              {/* Keyboard shortcuts helper - only show on desktop */}
              <Tooltip>
                <TooltipTrigger asChild>
                  <span className="hidden sm:inline-block ml-2 text-xs opacity-70 cursor-help">
                    Selection shortcuts
                  </span>
                </TooltipTrigger>
                <TooltipContent>
                  <div className="text-xs">
                    <div>Shift+click for range</div>
                    <div>{isMac ? "Cmd" : "Ctrl"}+click for multiple</div>
                  </div>
                </TooltipContent>
              </Tooltip>
            </>
          ) : (
            <>
              {/* Show special loading message when fetching without cache (cold load) */}
              {selectionInfo.isLoading && !selectionInfo.isCachedData && !selectionInfo.isStaleData && selectionInfo.torrentsLength === 0 ? (
                <>
                  <Loader2 className="h-3 w-3 animate-spin inline mr-1" />
                  Loading torrents...
                </>
              ) : selectionInfo.totalCount === 0 ? (
                selectionInfo.emptyStateMessage
              ) : (
                <>
                  {selectionInfo.hasLoadedAll ? (
                    `${selectionInfo.torrentsLength} torrent${selectionInfo.torrentsLength !== 1 ? "s" : ""}`
                  ) : selectionInfo.isLoadingMore ? (
                    "Loading more torrents..."
                  ) : (
                    `${selectionInfo.torrentsLength} of ${selectionInfo.totalCount} torrents loaded`
                  )}
                  {selectionInfo.hasLoadedAll && selectionInfo.safeLoadedRows < selectionInfo.rowsLength && " (scroll for more)"}
                </>
              )}
            </>
          )
        ) : (
          <span className="opacity-50">Loading...</span>
        )}
      </div>

      {/* Right: Speed, controls, network info */}
      <div className="flex flex-wrap items-center gap-2">
        {/* Speed & Controls */}
        <div className="flex items-center gap-2 pr-2 border-r last:border-r-0 last:pr-0">
          <ChevronDown className="h-3 w-3 text-muted-foreground" />
          <span className="font-medium">{formatSpeedWithUnit(serverState?.dl_info_speed ?? 0, speedUnit)}</span>
          <ChevronUp className="h-3 w-3 text-muted-foreground" />
          <span className="font-medium">{formatSpeedWithUnit(serverState?.up_info_speed ?? 0, speedUnit)}</span>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => setSpeedUnit(speedUnit === "bytes" ? "bits" : "bytes")}
                className="h-6 px-2 text-xs text-muted-foreground hover:text-accent-foreground"
              >
                <ArrowUpDown className="h-3 w-3" />
                <span>{speedUnit === "bytes" ? "MiB/s" : "Mbps"}</span>
              </Button>
            </TooltipTrigger>
            <TooltipContent>
              {speedUnit === "bytes" ? "Switch to bits per second (bps)" : "Switch to bytes per second (B/s)"}
            </TooltipContent>
          </Tooltip>
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => void handleToggleAltSpeedLimits()}
                disabled={isTogglingAltSpeed}
                aria-pressed={isAltSpeedKnown ? altSpeedEnabled : undefined}
                aria-label={altSpeedAriaLabel}
                className={cn(
                  "h-6 w-6 text-muted-foreground hover:text-accent-foreground",
                  "disabled:opacity-60 disabled:cursor-not-allowed"
                )}
              >
                {isTogglingAltSpeed ? (
                  <Loader2 className="h-3 w-3 animate-spin" />
                ) : (
                  <AltSpeedIcon className={cn("h-3 w-3", altSpeedIconClass)} />
                )}
              </Button>
            </TooltipTrigger>
            <TooltipContent>{altSpeedTooltip}</TooltipContent>
          </Tooltip>
          {instance?.reannounceSettings?.enabled && (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    void navigate({
                      to: "/services",
                      search: { instanceId: String(instanceId) },
                    })
                  }}
                  className="h-6 w-6 text-muted-foreground hover:text-accent-foreground"
                >
                  <RefreshCcw className="h-4 w-4 text-green-500" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>Automatic tracker reannounce enabled - Click to configure</TooltipContent>
            </Tooltip>
          )}
        </div>

        {/* View Controls */}
        <div className="flex items-center gap-2 pr-2 border-r last:border-r-0 last:pr-0">
          <Button
            variant="ghost"
            size="sm"
            onClick={cycleViewMode}
            className={cn(
              "h-6 px-2 text-xs hover:text-accent-foreground",
              "text-muted-foreground"
            )}
          >
            {desktopViewMode === "normal" ? (
              <TableIcon className="h-3 w-3" />
            ) : desktopViewMode === "dense" ? (
              <Rows3 className="h-3 w-3" />
            ) : (
              <LayoutGrid className="h-3 w-3" />
            )}
            <span className="hidden sm:inline">
              {desktopViewMode === "normal" ? "Table" : desktopViewMode === "dense" ? "Dense" : "Stacked"}
            </span>
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setIncognitoMode(!incognitoMode)}
            className={cn(
              "h-6 px-2 text-xs hover:text-accent-foreground",
              incognitoMode ? "text-foreground" : "text-muted-foreground"
            )}
          >
            {incognitoMode ? (
              <EyeOff className="h-3 w-3" />
            ) : (
              <Eye className="h-3 w-3" />
            )}
            <span className="hidden sm:inline">
              {incognitoMode ? "Incognito on" : "Incognito off"}
            </span>
          </Button>
        </div>

        {/* Free Space */}
        {serverState?.free_space_on_disk !== undefined && (
          <div className="flex items-center gap-2 pr-2 border-r last:border-r-0 last:pr-0">
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="flex items-center h-6 px-2 text-xs text-muted-foreground">
                  <HardDrive aria-hidden="true" className="h-3 w-3 mr-1" />
                  <span className="ml-auto font-medium truncate">{formatBytes(serverState.free_space_on_disk)}</span>
                </span>
              </TooltipTrigger>
              <TooltipContent>Free Space</TooltipContent>
            </Tooltip>
          </div>
        )}

        {/* Network Status */}
        <div className="flex items-center gap-2">
          <ExternalIPAddress
            address={serverState?.last_external_address_v4}
            incognitoMode={incognitoMode}
            label="IPv4"
          />
          <ExternalIPAddress
            address={serverState?.last_external_address_v6}
            incognitoMode={incognitoMode}
            label="IPv6"
          />
          <Tooltip>
            <TooltipTrigger asChild>
              <span
                tabIndex={0}
                aria-label={connectionStatusAriaLabel}
                className={cn(
                  "inline-flex h-6 w-6 items-center justify-center rounded-md border border-transparent",
                  "text-muted-foreground",
                  connectionStatusIconClass
                )}
              >
                <ConnectionStatusIcon className="h-3 w-3" aria-hidden="true" />
              </span>
            </TooltipTrigger>
            <TooltipContent className="max-w-[220px]">
              <p>{connectionStatusTooltip}</p>
            </TooltipContent>
          </Tooltip>
        </div>
      </div>
    </div>
  )
})
