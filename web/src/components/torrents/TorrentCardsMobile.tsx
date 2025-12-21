/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { ScrollToTopButton } from "@/components/ui/scroll-to-top-button"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Switch } from "@/components/ui/switch"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { TORRENT_ACTIONS, useTorrentActions, type TorrentAction } from "@/hooks/useTorrentActions"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowUpDown,
  Blocks,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  Eye,
  EyeOff,
  FileEdit,
  Filter,
  Folder,
  FolderOpen,
  Gauge,
  GitBranch,
  Info,
  ListTodo,
  Loader2,
  MoreVertical,
  Pause,
  Play,
  Plus,
  Radio,
  Search,
  Settings2,
  Sprout,
  Tag,
  Trash2,
  X
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { AddTorrentDialog } from "./AddTorrentDialog"
import { DeleteTorrentDialog } from "./DeleteTorrentDialog"
import { LocationWarningDialog, RemoveTagsDialog, SetCategoryDialog, SetLocationDialog, SetTagsDialog, TmmConfirmDialog } from "./TorrentDialogs"
// import { createPortal } from 'react-dom'
// Columns dropdown removed on mobile
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedFilter } from "@/hooks/useCrossSeedFilter"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata.ts"
import { usePersistedCompactViewState, type ViewMode } from "@/hooks/usePersistedCompactViewState"
import { api } from "@/lib/api"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { getCommonCategory, getCommonSavePath, getCommonTags } from "@/lib/torrent-utils"
import { cn, formatBytes } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts, TorrentFilters } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { getDefaultSortOrder, TORRENT_SORT_OPTIONS, type TorrentSortOptionValue } from "./torrentSortOptions"

// Mobile-friendly Share Limits Dialog
function MobileShareLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => void
  isPending: boolean
}) {
  const [ratioEnabled, setRatioEnabled] = useState(false)
  const [ratioLimit, setRatioLimit] = useState(1.5)
  const [seedingTimeEnabled, setSeedingTimeEnabled] = useState(false)
  const [seedingTimeLimit, setSeedingTimeLimit] = useState(1440)
  const [inactiveSeedingTimeEnabled, setInactiveSeedingTimeEnabled] = useState(false)
  const [inactiveSeedingTimeLimit, setInactiveSeedingTimeLimit] = useState(10080)

  const handleSubmit = () => {
    onConfirm(
      ratioEnabled ? ratioLimit : -1,
      seedingTimeEnabled ? seedingTimeLimit : -1,
      inactiveSeedingTimeEnabled ? inactiveSeedingTimeLimit : -1
    )
    // Reset form
    setRatioEnabled(false)
    setRatioLimit(1.5)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(1440)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(10080)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set Share Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Configure seeding limits. Use -1 or disable to remove limits.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="ratioEnabled"
                checked={ratioEnabled}
                onCheckedChange={setRatioEnabled}
              />
              <Label htmlFor="ratioEnabled">Set ratio limit</Label>
            </div>
            {ratioEnabled && (
              <Input
                type="number"
                min="0"
                step="0.1"
                value={ratioLimit}
                onChange={(e) => setRatioLimit(parseFloat(e.target.value) || 0)}
                placeholder="1.5"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="seedingTimeEnabled"
                checked={seedingTimeEnabled}
                onCheckedChange={setSeedingTimeEnabled}
              />
              <Label htmlFor="seedingTimeEnabled">Set seeding time limit (minutes)</Label>
            </div>
            {seedingTimeEnabled && (
              <Input
                type="number"
                min="0"
                value={seedingTimeLimit}
                onChange={(e) => setSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="1440"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="inactiveSeedingTimeEnabled"
                checked={inactiveSeedingTimeEnabled}
                onCheckedChange={setInactiveSeedingTimeEnabled}
              />
              <Label htmlFor="inactiveSeedingTimeEnabled">Set inactive seeding limit (minutes)</Label>
            </div>
            {inactiveSeedingTimeEnabled && (
              <Input
                type="number"
                min="0"
                value={inactiveSeedingTimeLimit}
                onChange={(e) => setInactiveSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="10080"
              />
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isPending}>
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Mobile-friendly Speed Limits Dialog
function MobileSpeedLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending: boolean
}) {
  const [uploadEnabled, setUploadEnabled] = useState(false)
  const [uploadLimit, setUploadLimit] = useState(1024)
  const [downloadEnabled, setDownloadEnabled] = useState(false)
  const [downloadLimit, setDownloadLimit] = useState(1024)

  const handleSubmit = () => {
    onConfirm(
      uploadEnabled ? uploadLimit : -1,
      downloadEnabled ? downloadLimit : -1
    )
    // Reset form
    setUploadEnabled(false)
    setUploadLimit(1024)
    setDownloadEnabled(false)
    setDownloadLimit(1024)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set Speed Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Set upload and download speed limits in KB/s. Use -1 or disable to remove limits.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="uploadEnabled"
                checked={uploadEnabled}
                onCheckedChange={setUploadEnabled}
              />
              <Label htmlFor="uploadEnabled">Set upload limit (KB/s)</Label>
            </div>
            {uploadEnabled && (
              <Input
                type="number"
                min="0"
                value={uploadLimit}
                onChange={(e) => setUploadLimit(parseInt(e.target.value) || 0)}
                placeholder="1024"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="downloadEnabled"
                checked={downloadEnabled}
                onCheckedChange={setDownloadEnabled}
              />
              <Label htmlFor="downloadEnabled">Set download limit (KB/s)</Label>
            </div>
            {downloadEnabled && (
              <Input
                type="number"
                min="0"
                value={downloadLimit}
                onChange={(e) => setDownloadLimit(parseInt(e.target.value) || 0)}
                placeholder="1024"
              />
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isPending}>
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

interface TorrentCardsMobileProps {
  instanceId: number
  filters?: TorrentFilters
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: TorrentCounts, categories?: Record<string, Category>, tags?: string[], useSubcategories?: boolean) => void
  onFilterChange?: (filters: TorrentFilters) => void
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
}

function formatEta(seconds: number): string {
  if (seconds === 8640000) return "∞"
  if (seconds < 0) return ""

  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)

  if (hours > 24) {
    const days = Math.floor(hours / 24)
    return `${days}d`
  }

  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }

  return `${minutes}m`
}

function getStatusBadgeVariant(state: string): "default" | "secondary" | "destructive" | "outline" {
  switch (state) {
    case "downloading":
      return "default"
    case "stalledDL":
      return "secondary"
    case "uploading":
      return "default"
    case "stalledUP":
      return "secondary"
    case "pausedDL":
    case "pausedUP":
      return "secondary"
    case "error":
    case "missingFiles":
      return "destructive"
    default:
      return "outline"
  }
}

function getStatusBadgeProps(torrent: Torrent, supportsTrackerHealth: boolean): {
  variant: "default" | "secondary" | "destructive" | "outline"
  label: string
  className: string
} {
  const baseVariant = getStatusBadgeVariant(torrent.state)
  let variant = baseVariant
  let label = getStateLabel(torrent.state)
  let className = ""

  if (supportsTrackerHealth) {
    const trackerHealth = torrent.tracker_health ?? null
    if (trackerHealth === "tracker_down") {
      label = "Tracker Down"
      variant = "outline"
      className = "text-yellow-500 border-yellow-500/40 bg-yellow-500/10"
    } else if (trackerHealth === "unregistered") {
      label = "Unregistered"
      variant = "outline"
      className = "text-destructive border-destructive/40 bg-destructive/10"
    }
  }

  return { variant, label, className }
}

function shallowEqualTrackerIcons(
  prev?: Record<string, string>,
  next?: Record<string, string>
): boolean {
  if (prev === next) {
    return true
  }

  if (!prev || !next) {
    return false
  }

  const prevKeys = Object.keys(prev)
  const nextKeys = Object.keys(next)

  if (prevKeys.length !== nextKeys.length) {
    return false
  }

  for (const key of prevKeys) {
    if (prev[key] !== next[key]) {
      return false
    }
  }

  return true
}

interface MobileSortState {
  field: TorrentSortOptionValue
  order: "asc" | "desc"
}

const DEFAULT_MOBILE_SORT_STATE: MobileSortState = {
  field: "added_on",
  order: getDefaultSortOrder("added_on"),
}

const MOBILE_SORT_STORAGE_KEY = "qui:torrent-mobile-sort"

function isValidSortField(value: unknown): value is TorrentSortOptionValue {
  return TORRENT_SORT_OPTIONS.some(option => option.value === value)
}

const trackerIconSizeClasses = {
  xs: "h-3 w-3 text-[8px]",
  sm: "h-[14px] w-[14px] text-[9px]",
  md: "h-4 w-4 text-[10px]",
} as const

type TrackerIconSize = keyof typeof trackerIconSizeClasses

interface TrackerIconProps {
  title: string
  fallback: string
  src: string | null
  size?: TrackerIconSize
  className?: string
}

const TrackerIcon = ({ title, fallback, src, size = "md", className }: TrackerIconProps) => {
  const [hasError, setHasError] = useState(false)

  useEffect(() => {
    setHasError(false)
  }, [src])

  return (
    <div className={cn("flex items-center justify-center", className)} title={title}>
      <div
        className={cn(
          "flex items-center justify-center rounded-sm border border-border/40 bg-muted font-medium uppercase leading-none select-none",
          trackerIconSizeClasses[size]
        )}
      >
        {src && !hasError ? (
          <img
            src={src}
            alt=""
            className="h-full w-full rounded-[2px] object-cover"
            loading="lazy"
            draggable={false}
            onError={() => setHasError(true)}
          />
        ) : (
          <span aria-hidden="true">{fallback}</span>
        )}
      </div>
    </div>
  )
}

const getTrackerDisplayMeta = (tracker?: string) => {
  if (!tracker) {
    return {
      host: "",
      fallback: "#",
      title: "",
    }
  }

  const trimmed = tracker.trim()
  const fallbackLetter = trimmed ? trimmed.charAt(0).toUpperCase() : "#"

  let host = trimmed
  try {
    if (trimmed.includes("://")) {
      const url = new URL(trimmed)
      host = url.hostname
    }
  } catch {
    // Keep host as trimmed value if URL parsing fails
  }

  return {
    host,
    fallback: fallbackLetter,
    title: host,
  }
}

// Swipeable card component with gesture support
function SwipeableCard({
  torrent,
  isSelected,
  onSelect,
  onClick,
  onLongPress,
  incognitoMode,
  selectionMode,
  speedUnit,
  viewMode,
  supportsTrackerHealth,
  trackerIcons,
}: {
  torrent: Torrent
  isSelected: boolean
  onSelect: (selected: boolean) => void
  onClick: () => void
  onLongPress: (torrent: Torrent) => void
  incognitoMode: boolean
  selectionMode: boolean
  speedUnit: SpeedUnit
  viewMode: ViewMode
  supportsTrackerHealth: boolean
  trackerIcons?: Record<string, string>
}) {

  // Use number for timeoutId in browser
  const [longPressTimer, setLongPressTimer] = useState<number | null>(null)
  const [touchStart, setTouchStart] = useState<{ x: number; y: number } | null>(null)
  const [hasMoved, setHasMoved] = useState(false)

  const handleTouchStart = (e: React.TouchEvent) => {
    if (selectionMode) return // Don't trigger long press in selection mode

    const touch = e.touches[0]
    setTouchStart({ x: touch.clientX, y: touch.clientY })
    setHasMoved(false)

    const timer = window.setTimeout(() => {
      if (!hasMoved) {
        // Vibrate if available
        if ("vibrate" in navigator) {
          navigator.vibrate(50)
        }
        onLongPress(torrent)
      }
    }, 600) // Increased to 600ms to be less sensitive
    setLongPressTimer(timer)
  }

  const handleTouchMove = (e: React.TouchEvent) => {
    if (!touchStart || hasMoved) return

    const touch = e.touches[0]
    const deltaX = Math.abs(touch.clientX - touchStart.x)
    const deltaY = Math.abs(touch.clientY - touchStart.y)

    // If moved more than 10px in any direction, cancel long press
    if (deltaX > 10 || deltaY > 10) {
      setHasMoved(true)
      if (longPressTimer !== null) {
        clearTimeout(longPressTimer)
        setLongPressTimer(null)
      }
    }
  }

  const handleTouchEnd = () => {
    if (longPressTimer !== null) {
      clearTimeout(longPressTimer)
      setLongPressTimer(null)
    }
    setTouchStart(null)
    setHasMoved(false)
  }

  const displayName = incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name
  const displayCategory = incognitoMode ? getLinuxCategory(torrent.hash) : torrent.category
  const displayTags = incognitoMode ? getLinuxTags(torrent.hash) : torrent.tags
  const displayRatio = incognitoMode ? getLinuxRatio(torrent.hash) : torrent.ratio
  const { variant: statusBadgeVariant, label: statusBadgeLabel, className: statusBadgeClass } = useMemo(
    () => getStatusBadgeProps(torrent, supportsTrackerHealth),
    [torrent, supportsTrackerHealth]
  )
  const trackerValue = incognitoMode ? getLinuxTracker(torrent.hash) : torrent.tracker
  const trackerMeta = useMemo(() => getTrackerDisplayMeta(trackerValue), [trackerValue])
  const trackerIconSrc = trackerMeta.host ? trackerIcons?.[trackerMeta.host] ?? null : null

  return (
    <div
      className={cn(
        "bg-card rounded-lg border cursor-pointer transition-all relative overflow-hidden select-none",
        viewMode === "ultra-compact" ? "px-3 py-1" : viewMode === "compact" ? "p-2" : "p-4",
        isSelected && "bg-accent/50",
        !selectionMode && "active:scale-[0.98]"
      )}
      onTouchStart={!selectionMode ? handleTouchStart : undefined}
      onTouchMove={!selectionMode ? handleTouchMove : undefined}
      onTouchEnd={!selectionMode ? handleTouchEnd : undefined}
      onTouchCancel={!selectionMode ? handleTouchEnd : undefined}
      onClick={() => {
        if (selectionMode) {
          onSelect(!isSelected)
        } else {
          onClick()
        }
      }}
    >
      {/* Inner selection ring */}
      {isSelected && (
        <div className="absolute inset-0 rounded-lg ring-2 ring-primary ring-inset pointer-events-none" />
      )}
      {/* Selection checkbox - visible in normal view selection mode */}
      {selectionMode && viewMode === "normal" && (
        <div className="absolute top-2 right-2 z-10">
          <Checkbox
            checked={isSelected}
            onCheckedChange={onSelect}
            className="h-5 w-5"
            onClick={(e) => e.stopPropagation()}
          />
        </div>
      )}

      {viewMode === "ultra-compact" ? (
        /* Ultra Compact Layout - Single Line */
        <div className="flex items-center gap-2">
          <div className="flex-1 min-w-0 overflow-hidden">
            <div className="w-full overflow-x-auto scrollbar-thin">
              <div className="flex items-center gap-1 whitespace-nowrap">
                <TrackerIcon
                  title={trackerMeta.title}
                  fallback={trackerMeta.fallback}
                  src={trackerIconSrc}
                  size="xs"
                  className="flex-shrink-0"
                />
                <h3 className={cn(
                  "font-medium text-xs inline-block",
                  selectionMode && "pr-8"
                )} title={displayName}>
                  {displayName}
                </h3>
              </div>
            </div>
          </div>

          {/* Speeds if applicable */}
          {(torrent.dlspeed > 0 || torrent.upspeed > 0) && (
            <div className="flex items-center gap-1 text-[10px] flex-shrink-0">
              {torrent.dlspeed > 0 && (
                <span className="text-chart-2 font-medium">
                  ↓{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}
                </span>
              )}
              {torrent.upspeed > 0 && (
                <span className="text-chart-3 font-medium">
                  ↑{formatSpeedWithUnit(torrent.upspeed, speedUnit)}
                </span>
              )}
            </div>
          )}

          {/* State badge - smaller */}
          <Badge variant={statusBadgeVariant} className={cn("text-[10px] px-1 py-0 h-4 flex-shrink-0", statusBadgeClass)}>
            {statusBadgeLabel}
          </Badge>

          {/* Percentage if not 100% */}
          {torrent.progress * 100 !== 100 && (
            <span className="text-[10px] text-muted-foreground flex-shrink-0">
              {torrent.progress >= 0.99 && torrent.progress < 1 ? (
                (Math.floor(torrent.progress * 1000) / 10).toFixed(1)
              ) : (
                Math.round(torrent.progress * 100)
              )}%
            </span>
          )}
        </div>
      ) : viewMode === "compact" ? (
        /* Compact Layout */
        <>
          {/* Name with progress inline */}
          <div className="flex items-center gap-2 mb-1">
            <div className="flex-1 min-w-0 overflow-hidden">
              <div className="w-full overflow-x-auto scrollbar-thin">
                <div className="flex items-center gap-1 whitespace-nowrap">
                  <TrackerIcon
                    title={trackerMeta.title}
                    fallback={trackerMeta.fallback}
                    src={trackerIconSrc}
                    size="sm"
                    className="flex-shrink-0"
                  />
                  <h3 className={cn(
                    "font-medium text-sm inline-block",
                    selectionMode && "pr-8"
                  )} title={displayName}>
                    {displayName}
                  </h3>
                </div>
              </div>
            </div>
            <Badge variant={statusBadgeVariant} className={cn("text-xs flex-shrink-0", statusBadgeClass)}>
              {statusBadgeLabel}
            </Badge>
          </div>

          {/* Downloaded/Size and Ratio */}
          <div className="flex items-center justify-between text-xs">
            <span className="text-muted-foreground">
              {formatBytes(torrent.downloaded)} / {formatBytes(torrent.size)}
            </span>
            <div className="flex items-center gap-1">
              <span className="text-muted-foreground">Ratio:</span>
              <span className={cn(
                "font-medium",
                displayRatio >= 1 ? "[color:var(--chart-3)]" : "[color:var(--chart-4)]"
              )}>
                {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
              </span>
            </div>
          </div>
        </>
      ) : (
        /* Full Layout */
        <>
          {/* Torrent name */}
          <div className="mb-3">
            <h3 className={cn(
              "font-medium text-sm line-clamp-2 break-all h-10",
              selectionMode && "pr-8"
            )}>
              {displayName}
            </h3>
            <div className="mt-1 flex items-center gap-1 text-xs text-muted-foreground truncate h-4">
            {trackerMeta.title && (
              <>
                <TrackerIcon
                  title={trackerMeta.title}
                  fallback={trackerMeta.fallback}
                  src={trackerIconSrc}
                  size="xs"
                />
                <span className="truncate" title={trackerMeta.title}>
                  {trackerMeta.title}
                </span>
              </>
            )}
            </div>
          </div>

          {/* Progress bar */}
          <div className="mb-3">
            <div className="flex items-center justify-between mb-1">
              <span className="text-xs text-muted-foreground">
                {formatBytes(torrent.downloaded)} / {formatBytes(torrent.size)}
              </span>
              <div className="flex items-center gap-2">
                {/* ETA */}
                {torrent.eta > 0 && torrent.eta !== 8640000 && (
                  <div className="flex items-center gap-1">
                    <Clock className="h-3 w-3 text-muted-foreground" />
                    <span className="text-xs text-muted-foreground">{formatEta(torrent.eta)}</span>
                  </div>
                )}
                <span className="text-xs font-medium">
                  {torrent.progress >= 0.99 && torrent.progress < 1 ? (
                    (Math.floor(torrent.progress * 1000) / 10).toFixed(1)
                  ) : (
                    Math.round(torrent.progress * 100)
                  )}%
                </span>
              </div>
            </div>
            <Progress value={torrent.progress * 100} className="h-2" />
          </div>

          {/* Speed, Ratio and State row */}
          <div className="flex items-center justify-between text-xs mb-2">
            <div className="flex items-center gap-3">
              {/* Ratio on the left */}
              <div className="flex items-center gap-1">
                <span className="text-muted-foreground">Ratio:</span>
                <span className={cn(
                  "font-medium",
                  displayRatio >= 1 ? "[color:var(--chart-3)]" : "[color:var(--chart-4)]"
                )}>
                  {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
                </span>
              </div>

              {/* Download speed */}
              {torrent.dlspeed > 0 && (
                <div className="flex items-center gap-1">
                  <ChevronDown className="h-3 w-3 [color:var(--chart-2)]" />
                  <span className="font-medium">{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}</span>
                </div>
              )}

              {/* Upload speed */}
              {torrent.upspeed > 0 && (
                <div className="flex items-center gap-1">
                  <ChevronUp className="h-3 w-3 [color:var(--chart-3)]" />
                  <span className="font-medium">{formatSpeedWithUnit(torrent.upspeed, speedUnit)}</span>
                </div>
              )}
            </div>

            {/* State badge on the right */}
            <Badge variant={statusBadgeVariant} className={cn("text-xs", statusBadgeClass)}>
              {statusBadgeLabel}
            </Badge>
          </div>
        </>
      )}

      {/* Bottom row: Category/Tags and Status/Speeds - only for compact and full views */}
      {viewMode === "compact" ? (
        /* Compact version: Category/tags on left, percentage/speeds on right */
        <div className="flex items-center justify-between gap-2 text-xs mt-1">
          {/* Left side: Category and Tags */}
          <div className="flex items-center gap-2 text-muted-foreground min-w-0 overflow-hidden">
            {displayCategory && (
              <span className="flex items-center gap-1 flex-shrink-0">
                <Folder className="h-3 w-3" />
                {displayCategory}
              </span>
            )}
            {displayTags && (
              <div className="flex items-center gap-1 min-w-0 overflow-hidden">
                <Tag className="h-3 w-3 flex-shrink-0" />
                <span className="truncate">
                  {Array.isArray(displayTags) ? displayTags.join(", ") : displayTags}
                </span>
              </div>
            )}
          </div>

          {/* Right side: Percentage and Speeds */}
          <div className="flex items-center gap-2 flex-shrink-0">
            <span className="text-muted-foreground">
              {torrent.progress >= 0.99 && torrent.progress < 1 ? (
                (Math.floor(torrent.progress * 1000) / 10).toFixed(1)
              ) : (
                Math.round(torrent.progress * 100)
              )}%
            </span>
            {/* Speeds */}
            {(torrent.dlspeed > 0 || torrent.upspeed > 0) && (
              <div className="flex items-center gap-1">
                {torrent.dlspeed > 0 && (
                  <span className="text-chart-2 font-medium">
                    ↓{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}
                  </span>
                )}
                {torrent.upspeed > 0 && (
                  <span className="text-chart-3 font-medium">
                    ↑{formatSpeedWithUnit(torrent.upspeed, speedUnit)}
                  </span>
                )}
              </div>
            )}
          </div>
        </div>
      ) : viewMode === "normal" ? (
        /* Full version: Original layout */
        <div className="flex items-center justify-between gap-2 min-h-[20px]">
          {/* Category */}
          {displayCategory && (
            <div className="flex items-center gap-1 flex-shrink-0">
              <Folder className="h-3 w-3 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">{displayCategory}</span>
            </div>
          )}

          {/* Tags - aligned to the right */}
          {displayTags && (
            <div className="flex items-center gap-1 flex-wrap justify-end ml-auto overflow-hidden max-h-4">
              <Tag className="h-3 w-3 text-muted-foreground flex-shrink-0" />
              {(Array.isArray(displayTags) ? displayTags : displayTags.split(",")).map((tag, i) => (
                <Badge key={i} variant="secondary" className="text-[10px] px-1.5 py-0 h-4">
                  {tag.trim()}
                </Badge>
              ))}
            </div>
          )}
        </div>
      ) : null /* Ultra-compact has no bottom row */}
    </div>
  )
}

export function TorrentCardsMobile({
  instanceId,
  filters,
  onTorrentSelect,
  addTorrentModalOpen,
  onAddTorrentModalChange,
  onFilteredDataUpdate,
  onFilterChange,
  canCrossSeedSearch,
  onCrossSeedSearch,
  isCrossSeedSearching,
}: TorrentCardsMobileProps) {
  // State
  const [sortState, setSortState] = useState<MobileSortState>(() => {
    if (typeof window === "undefined") {
      return DEFAULT_MOBILE_SORT_STATE
    }

    try {
      const stored = window.localStorage.getItem(`${MOBILE_SORT_STORAGE_KEY}:${instanceId}`)
      if (stored) {
        const parsed = JSON.parse(stored) as Partial<MobileSortState>
        const field = isValidSortField(parsed?.field) ? parsed?.field : DEFAULT_MOBILE_SORT_STATE.field
        const defaultOrder = getDefaultSortOrder(field)
        const order = parsed?.order === "asc" || parsed?.order === "desc" ? parsed.order : defaultOrder
        return { field, order }
      }
    } catch {
      // Ignore malformed localStorage entries
    }

    return DEFAULT_MOBILE_SORT_STATE
  })
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  const [selectedHashes, setSelectedHashes] = useState<Set<string>>(new Set())
  const [selectionMode, setSelectionMode] = useState(false)
  const { setIsSelectionMode } = useTorrentSelection()

  const parentRef = useRef<HTMLDivElement>(null)
  const [torrentToDelete, setTorrentToDelete] = useState<Torrent | null>(null)
  const [showActionsSheet, setShowActionsSheet] = useState(false)
  const [actionTorrents, setActionTorrents] = useState<Torrent[]>([]);
  const [showShareLimitDialog, setShowShareLimitDialog] = useState(false)
  const [showSpeedLimitDialog, setShowSpeedLimitDialog] = useState(false)
  const [showSearchModal, setShowSearchModal] = useState(false)
  const sortField = sortState.field
  const sortOrder = sortState.order

  // Map sort field to backend field name (e.g., num_seeds -> num_complete)
  const backendSortField = sortField === "num_seeds" ? "num_complete" : sortField === "num_leechs" ? "num_incomplete" : sortField

  const currentSortOption = useMemo(() => {
    return TORRENT_SORT_OPTIONS.find(option => option.value === sortField) ?? TORRENT_SORT_OPTIONS[0]
  }, [sortField])

  const handleSortFieldChange = useCallback((value: TorrentSortOptionValue) => {
    setSortState(prev => {
      if (prev.field === value) {
        return prev
      }
      return {
        field: value,
        order: getDefaultSortOrder(value),
      }
    })
  }, [])

  const toggleSortOrder = useCallback(() => {
    setSortState(prev => ({
      field: prev.field,
      order: prev.order === "desc" ? "asc" : "desc",
    }))
  }, [])

  // Custom "select all" state for handling large datasets
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit, setSpeedUnit] = useSpeedUnits()
  // Mobile cards don't support "dense" mode (which is table-row based on desktop).
  // Mobile uses card layouts: normal (full cards), compact, and ultra-compact.
  // This restriction syncs with FilterSidebar's mobile mode to keep view states consistent.
  const { viewMode } = usePersistedCompactViewState("compact", ["normal", "compact", "ultra-compact"])
  const trackerIconsQuery = useTrackerIcons()
  const trackerIconsRef = useRef<Record<string, string> | undefined>(undefined)
  const trackerIcons = useMemo(() => {
    const latest = trackerIconsQuery.data
    if (!latest) {
      return trackerIconsRef.current
    }

    const previous = trackerIconsRef.current
    if (previous && shallowEqualTrackerIcons(previous, latest)) {
      return previous
    }

    trackerIconsRef.current = latest
    return latest
  }, [trackerIconsQuery.data])

  // Track user-initiated actions to differentiate from automatic data updates
  const [lastUserAction, setLastUserAction] = useState<{ type: string; timestamp: number } | null>(null)
  const previousFiltersRef = useRef(filters)
  const previousInstanceIdRef = useRef(instanceId)
  const previousSearchRef = useRef("")
  const previousSortRef = useRef(sortState)

  const effectiveFilters = useMemo(() => {
    if (!filters) {
      return undefined
    }

    return {
      ...filters,
      categories: filters.expandedCategories ?? filters.categories ?? [],
      excludeCategories: filters.expandedExcludeCategories ?? filters.excludeCategories ?? [],
    }
  }, [filters])

  // Progressive loading state with async management
  const [loadedRows, setLoadedRows] = useState(100)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)

  // Use the shared torrent actions hook
  const {
    showDeleteDialog,
    closeDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    isDeleteFilesLocked,
    toggleDeleteFilesLock,
    deleteCrossSeeds,
    setDeleteCrossSeeds,
    showSetTagsDialog,
    setShowSetTagsDialog,
    showRemoveTagsDialog,
    setShowRemoveTagsDialog,
    showCategoryDialog,
    setShowCategoryDialog,
    showLocationDialog,
    setShowLocationDialog,
    showTmmDialog,
    setShowTmmDialog,
    pendingTmmEnable,
    showLocationWarningDialog,
    setShowLocationWarningDialog,
    isPending,
    handleAction,
    handleDelete,
    handleSetTags,
    handleRemoveTags,
    handleSetCategory,
    handleSetLocation,
    handleSetShareLimit,
    handleSetSpeedLimits,
    prepareDeleteAction,
    prepareLocationAction,
    prepareTmmAction,
    handleTmmConfirm,
    proceedToLocationDialog,
  } = useTorrentActions({
    instanceId,
    onActionComplete: (action) => {
      if (action === TORRENT_ACTIONS.DELETE) {
        setSelectedHashes(new Set())
        setSelectionMode(false)
        setIsSelectionMode(false)
        setIsAllSelected(false)
        setExcludedFromSelectAll(new Set())
      }
    },
  })

  // IMPORTANT REMINDER: mobile view currently lacks column filter expressions,
  // so select-all actions only forward the sidebar filters. If column filters
  // are ever added to this view, ensure the combined filters (including expr)
  // are passed into these bulk action payloads similar to the desktop table.

  // Get instance info for cross-seed warning
  const { instances } = useInstances()
  const instance = useMemo(() => instances?.find(i => i.id === instanceId), [instances, instanceId])

  const { data: metadata } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}
  const preferences = metadata?.preferences

  const debouncedSearch = useDebounce(globalFilter, 1000)
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string }
  const searchFromRoute = routeSearch?.q || ""

  const effectiveSearch = searchFromRoute || immediateSearch || debouncedSearch
  const navigate = useNavigate()

  // Query active task count for badge (lightweight endpoint)
  const { data: activeTaskCount = 0 } = useQuery({
    queryKey: ["active-task-count", instanceId],
    queryFn: () => api.getActiveTaskCount(instanceId),
    refetchInterval: 30000, // Poll every 30 seconds (lightweight check)
    refetchIntervalInBackground: true,
  })

  useEffect(() => {
    if (typeof window === "undefined") {
      setSortState(DEFAULT_MOBILE_SORT_STATE)
      return
    }

    const storageKey = `${MOBILE_SORT_STORAGE_KEY}:${instanceId}`
    setSortState(prev => {
      try {
        const stored = window.localStorage.getItem(storageKey)
        if (!stored) {
          return DEFAULT_MOBILE_SORT_STATE
        }

        const parsed = JSON.parse(stored) as Partial<MobileSortState>
        const field = isValidSortField(parsed?.field) ? parsed?.field : DEFAULT_MOBILE_SORT_STATE.field
        const defaultOrder = getDefaultSortOrder(field)
        const order = parsed?.order === "asc" || parsed?.order === "desc" ? parsed.order : defaultOrder

        if (prev.field === field && prev.order === order) {
          return prev
        }

        return { field, order }
      } catch {
        return DEFAULT_MOBILE_SORT_STATE
      }
    })
  }, [instanceId])

  useEffect(() => {
    if (typeof window === "undefined") {
      return
    }

    try {
      window.localStorage.setItem(
        `${MOBILE_SORT_STORAGE_KEY}:${instanceId}`,
        JSON.stringify(sortState)
      )
    } catch {
      // Ignore storage quota errors
    }
  }, [sortState, instanceId])

  // Columns controls removed on mobile

  useEffect(() => {
    if (searchFromRoute !== globalFilter) {
      setGlobalFilter(searchFromRoute)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchFromRoute])

  // Detect user-initiated changes
  useEffect(() => {
    const filtersChanged = JSON.stringify(previousFiltersRef.current) !== JSON.stringify(filters)
    const instanceChanged = previousInstanceIdRef.current !== instanceId
    const searchChanged = previousSearchRef.current !== effectiveSearch
    const sortChanged =
      previousSortRef.current.field !== sortState.field ||
      previousSortRef.current.order !== sortState.order

    if (filtersChanged || instanceChanged || searchChanged || sortChanged) {
      const actionType = instanceChanged ? "instance" : sortChanged ? "sort" : filtersChanged ? "filter" : "search"

      setLastUserAction({
        type: actionType,
        timestamp: Date.now(),
      })
    }

    // Update refs
    previousFiltersRef.current = filters
    previousInstanceIdRef.current = instanceId
    previousSearchRef.current = effectiveSearch
    previousSortRef.current = sortState
  }, [filters, instanceId, effectiveSearch, sortState])

  // Fetch data
  const {
    torrents,
    totalCount,
    counts,
    categories,
    tags,
    stats,
    useSubcategories: subcategoriesFromData,

    isLoading,
    isLoadingMore,
    hasLoadedAll,
    loadMore: backendLoadMore,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters: effectiveFilters,
    sort: backendSortField,
    order: sortOrder,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId)
  const supportsTrackerHealth = capabilities?.supportsTrackerHealth ?? true
  const supportsTorrentCreation = capabilities?.supportsTorrentCreation ?? true
  const supportsSubcategories = capabilities?.supportsSubcategories ?? false
  // subcategoriesFromData reflects backend/server state; allowSubcategories
  // additionally respects user preferences for UI surfaces like dialogs.
  const allowSubcategories =
    supportsSubcategories && (preferences?.use_subcategories ?? subcategoriesFromData ?? false)

  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags, subcategoriesFromData)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, subcategoriesFromData, onFilteredDataUpdate]) // Update when data changes

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // When all selected, count is total minus exclusions
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the selectedHashes size
      return selectedHashes.size
    }
  }, [isAllSelected, totalCount, excludedFromSelectAll.size, selectedHashes.size])

  const selectedTotalSize = useMemo(() => {
    if (isAllSelected) {
      const aggregateTotalSize = stats?.totalSize ?? 0

      if (aggregateTotalSize <= 0) {
        return 0
      }

      if (excludedFromSelectAll.size === 0) {
        return aggregateTotalSize
      }

      const excludedSize = torrents.reduce((total, torrent) => {
        if (excludedFromSelectAll.has(torrent.hash)) {
          return total + (torrent.size || 0)
        }
        return total
      }, 0)

      return Math.max(aggregateTotalSize - excludedSize, 0)
    }

    let total = 0
    torrents.forEach(torrent => {
      if (selectedHashes.has(torrent.hash)) {
        total += torrent.size || 0
      }
    })

    return total
  }, [isAllSelected, stats?.totalSize, excludedFromSelectAll, torrents, selectedHashes])

  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  // Torrents to check for cross-seeds (either single torrent or selected torrents)
  const deleteTorrents = useMemo(() => {
    if (torrentToDelete) {
      return [torrentToDelete]
    }
    return torrents.filter(t => selectedHashes.has(t.hash))
  }, [torrentToDelete, torrents, selectedHashes])

  // Cross-seed warning for delete dialog
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    instanceName: instance?.name ?? "",
    torrents: deleteTorrents,
  })

  // Load more rows as user scrolls (progressive loading + backend pagination)
  const loadMore = useCallback((): void => {
    // First, try to load more from virtual scrolling if we have more local data
    if (loadedRows < torrents.length) {
      // Prevent concurrent loads
      if (isLoadingMoreRows) {
        return
      }

      setIsLoadingMoreRows(true)

      setLoadedRows(prev => {
        const newLoadedRows = Math.min(prev + 100, torrents.length)
        return newLoadedRows
      })

      // Reset loading flag after a short delay
      setTimeout(() => setIsLoadingMoreRows(false), 100)
    } else if (!hasLoadedAll && !isLoadingMore && backendLoadMore) {
      // If we've displayed all local data but there's more on backend, load next page
      backendLoadMore()
    }
  }, [torrents.length, isLoadingMoreRows, loadedRows, hasLoadedAll, isLoadingMore, backendLoadMore])

  // Ensure loadedRows never exceeds actual data length
  const safeLoadedRows = Math.min(loadedRows, torrents.length)

  // Also keep loadedRows in sync with actual data to prevent status display issues
  useEffect(() => {
    if (loadedRows > torrents.length && torrents.length > 0) {
      setLoadedRows(torrents.length)
    }
  }, [loadedRows, torrents.length])

  // Virtual scrolling with consistent spacing
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => viewMode === "ultra-compact" ? 32 : viewMode === "compact" ? 86 : 204,
    overscan: 5,
    // Provide a key to help with item tracking - use hash with index for uniqueness
    getItemKey: useCallback((index: number) => {
      const torrent = torrents[index]
      return torrent?.hash ? `${torrent.hash}-${index}` : `loading-${index}`
    }, [torrents]),
    // Optimized onChange handler following TanStack Virtual best practices
    onChange: (instance, sync) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      // Only trigger loadMore when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive loadMore calls during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling

      if (shouldCheckLoadMore && lastItem && lastItem.index >= safeLoadedRows - 20) {
        // Load more if we're near the end of virtual rows OR if we might need more data from backend
        if (safeLoadedRows < torrents.length || (!hasLoadedAll && !isLoadingMore)) {
          loadMore();
        }
      }
    },
  })

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, virtualizer])

  const virtualItems = virtualizer.getVirtualItems()

  // Exit selection mode when no items selected
  useEffect(() => {
    if (selectionMode && effectiveSelectionCount === 0) {
      setSelectionMode(false)
      setIsSelectionMode(false)
    }
  }, [effectiveSelectionCount, selectionMode, setIsSelectionMode])

  // Sync selection mode with context
  useEffect(() => {
    setIsSelectionMode(selectionMode && effectiveSelectionCount > 0)
  }, [selectionMode, effectiveSelectionCount, setIsSelectionMode])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      setIsSelectionMode(false)
    }
  }, [setIsSelectionMode])

  // Reset loaded rows when data changes significantly
  useEffect(() => {
    // Always ensure loadedRows is at least 100 (or total length if less)
    const targetRows = Math.min(100, torrents.length)

    setLoadedRows(prev => {
      if (torrents.length === 0) {
        // No data, reset to 0
        return 0
      } else if (prev === 0) {
        // Initial load
        return targetRows
      } else if (prev < targetRows) {
        // Not enough rows loaded, load at least 100
        return targetRows
      }
      // Don't reset loadedRows backward due to temporary server data fluctuations
      // Progressive loading should be independent of server data variations
      return prev
    })

    // Force virtualizer to recalculate
    virtualizer.measure()
  }, [torrents.length, virtualizer])

  // Reset when filters or search changes
  useEffect(() => {
    // Only reset loadedRows for user-initiated changes, not data updates
    const isRecentUserAction = lastUserAction && (Date.now() - lastUserAction.timestamp < 1000)

    if (isRecentUserAction) {
      const targetRows = Math.min(100, torrents.length || 0)
      setLoadedRows(targetRows)
      setIsLoadingMoreRows(false)

      // Clear selection state when data changes
      setSelectedHashes(new Set())
      setSelectionMode(false)
      setIsSelectionMode(false)
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())

      // User-initiated change: scroll to top
      if (parentRef.current) {
        parentRef.current.scrollTop = 0
        setTimeout(() => {
          virtualizer.scrollToOffset(0)
          virtualizer.measure()
          // Additional force after a short delay to ensure all items are remeasured
          setTimeout(() => virtualizer.measure(), 100)
        }, 0)
      }
    } else {
      // Data update: aggressive remeasurement for dynamic content
      setTimeout(() => {
        virtualizer.measure()
        // Second pass to catch any missed items
        setTimeout(() => virtualizer.measure(), 50)
      }, 0)
    }
  }, [filters, effectiveSearch, instanceId, virtualizer, setIsSelectionMode, torrents.length, lastUserAction])

  // Recalculate virtualizer when view mode changes
  useEffect(() => {
    // Force complete remeasurement when view mode changes
    if (virtualizer) {
      setTimeout(() => {
        virtualizer.measure()
        // Multiple passes to ensure all items are properly measured
        setTimeout(() => virtualizer.measure(), 50)
        setTimeout(() => virtualizer.measure(), 150)
      }, 0)
    }
  }, [viewMode, virtualizer])

  // Additional effect to handle torrent content changes that affect height
  useEffect(() => {
    // Remeasure when the actual torrent data changes (not just count)
    if (virtualizer && torrents.length > 0) {
      setTimeout(() => {
        virtualizer.measure()
      }, 0)
    }
  }, [torrents, virtualizer])



  // Handlers
  const handleLongPress = useCallback((torrent: Torrent) => {
    setSelectionMode(true)
    setSelectedHashes(new Set([torrent.hash]))
  }, [])

  const handleSelect = useCallback((hash: string, selected: boolean) => {
    if (isAllSelected) {
      if (!selected) {
        // When deselecting in "select all" mode, add to exclusions
        setExcludedFromSelectAll(prev => new Set(prev).add(hash))
      } else {
        // When selecting a row that was excluded, remove from exclusions
        setExcludedFromSelectAll(prev => {
          const newSet = new Set(prev)
          newSet.delete(hash)
          return newSet
        })
      }
    } else {
      // Regular selection mode
      setSelectedHashes(prev => {
        const next = new Set(prev)
        if (selected) {
          next.add(hash)
        } else {
          next.delete(hash)
        }
        return next
      })
    }
  }, [isAllSelected])

  const handleSelectAll = useCallback(() => {
    const currentlySelectedCount = isAllSelected ? effectiveSelectionCount : selectedHashes.size
    const loadedTorrentsCount = torrents.length

    if (currentlySelectedCount === totalCount || (currentlySelectedCount === loadedTorrentsCount && currentlySelectedCount < totalCount)) {
      // Deselect all
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setSelectedHashes(new Set())
    } else if (loadedTorrentsCount >= totalCount) {
      // All torrents are loaded, use regular selection
      setSelectedHashes(new Set(torrents.map(t => t.hash)))
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
    } else {
      // Not all torrents are loaded, use "select all" mode
      setIsAllSelected(true)
      setExcludedFromSelectAll(new Set())
      setSelectedHashes(new Set())
    }
  }, [isAllSelected, effectiveSelectionCount, selectedHashes.size, torrents, totalCount])

  const triggerSelectionAction = useCallback((action: TorrentAction, extra?: Parameters<typeof handleAction>[2]) => {
    const hashes = isAllSelected ? [] : Array.from(selectedHashes)
    const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : Array.from(selectedHashes)
    const clientCount = isAllSelected ? effectiveSelectionCount : visibleHashes.length || 1

    handleAction(action, hashes, {
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      clientHashes: visibleHashes,
      clientCount,
      ...extra,
    })
  }, [handleAction, isAllSelected, selectedHashes, torrents, excludedFromSelectAll, effectiveSelectionCount, filters, effectiveSearch])

  const handleBulkAction = useCallback((action: TorrentAction, extra?: Parameters<typeof handleAction>[2]) => {
    triggerSelectionAction(action, extra)
    setShowActionsSheet(false)
  }, [triggerSelectionAction])

  const handleDeleteWrapper = useCallback(async () => {
    let hashes: string[]
    if (torrentToDelete) {
      hashes = [torrentToDelete.hash]
    } else if (isAllSelected) {
      hashes = []
    } else {
      hashes = Array.from(selectedHashes)
    }

    // Include cross-seed hashes if user opted to delete them
    const crossSeedHashes = deleteCrossSeeds
      ? crossSeedWarning.affectedTorrents.map(t => t.hash)
      : []
    const hashesToDelete = [...hashes, ...crossSeedHashes]

    let visibleHashes: string[]
    if (torrentToDelete) {
      visibleHashes = [torrentToDelete.hash]
    } else if (isAllSelected) {
      visibleHashes = torrents
        .filter(t => !excludedFromSelectAll.has(t.hash))
        .map(t => t.hash)
    } else {
      visibleHashes = Array.from(selectedHashes)
    }

    // Include cross-seeds in visible hashes for optimistic updates
    const visibleHashesToDelete = [...visibleHashes, ...crossSeedHashes]

    let totalSelected: number
    if (torrentToDelete) {
      totalSelected = 1
    } else if (isAllSelected) {
      totalSelected = effectiveSelectionCount
    } else {
      totalSelected = visibleHashes.length
    }

    // Add cross-seed count
    const totalToDelete = totalSelected + crossSeedHashes.length

    await handleDelete(
      hashesToDelete,
      !torrentToDelete && isAllSelected,
      !torrentToDelete && isAllSelected ? filters : undefined,
      !torrentToDelete && isAllSelected ? effectiveSearch : undefined,
      !torrentToDelete && isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      {
        clientHashes: visibleHashesToDelete,
        totalSelected: totalToDelete,
      }
    )
    setTorrentToDelete(null)
  }, [torrentToDelete, isAllSelected, selectedHashes, handleDelete, filters, effectiveSearch, excludedFromSelectAll, torrents, effectiveSelectionCount, deleteCrossSeeds, crossSeedWarning.affectedTorrents])

  const handleSetTagsWrapper = useCallback(async (tags: string[]) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : actionTorrents.map(t => t.hash)
    const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length
    await handleSetTags(
      tags,
      hashes,
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      {
        clientHashes: visibleHashes,
        totalSelected,
      }
    )
    setActionTorrents([])
  }, [isAllSelected, actionTorrents, handleSetTags, filters, effectiveSearch, excludedFromSelectAll, torrents, effectiveSelectionCount])

  const handleSetCategoryWrapper = useCallback(async (category: string) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : actionTorrents.map(t => t.hash)
    const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length
    await handleSetCategory(
      category,
      hashes,
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      {
        clientHashes: visibleHashes,
        totalSelected,
      }
    )
    setActionTorrents([])
  }, [isAllSelected, actionTorrents, handleSetCategory, filters, effectiveSearch, excludedFromSelectAll, torrents, effectiveSelectionCount])

  const handleSetLocationWrapper = useCallback(async (location: string) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : actionTorrents.map(t => t.hash)
    const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length
    await handleSetLocation(
      location,
      hashes,
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      {
        clientHashes: visibleHashes,
        totalSelected,
      }
    )
    setActionTorrents([])
  }, [isAllSelected, actionTorrents, handleSetLocation, filters, effectiveSearch, excludedFromSelectAll, torrents, effectiveSelectionCount])

  const handleTmmConfirmWrapper = useCallback(() => {
    const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : Array.from(selectedHashes)
    const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length || 1
    handleTmmConfirm(
      isAllSelected ? [] : Array.from(selectedHashes),
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      {
        clientHashes: visibleHashes,
        totalSelected,
      }
    )
  }, [isAllSelected, selectedHashes, handleTmmConfirm, filters, effectiveSearch, excludedFromSelectAll, torrents, effectiveSelectionCount])

  const getSelectedTorrents = useMemo(() => {
    if (isAllSelected) {
      // When all are selected, return all torrents minus exclusions
      return torrents.filter(t => !excludedFromSelectAll.has(t.hash))
    } else {
      // Regular selection mode
      return torrents.filter(t => selectedHashes.has(t.hash))
    }
  }, [torrents, selectedHashes, isAllSelected, excludedFromSelectAll])

  const { isFilteringCrossSeeds, filterCrossSeeds } = useCrossSeedFilter({
    instanceId,
    onFilterChange,
  })

  const singleSelectedTorrent = getSelectedTorrents[0] ?? null

  const handleClearSearch = useCallback(() => {
    setGlobalFilter("")

    if (routeSearch && Object.prototype.hasOwnProperty.call(routeSearch, "q")) {
      const next = { ...(routeSearch || {}) }
      delete next.q
      navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
    }
  }, [navigate, routeSearch])

  const handleClearSearchAndClose = useCallback(() => {
    handleClearSearch()
    setShowSearchModal(false)
  }, [handleClearSearch])

  return (
    <div className="relative flex h-full min-h-0 flex-col overflow-hidden">
      {/* Header with stats */}
      <div className="sticky top-0 z-40 bg-background">
        {/* Stats bar */}
        <div className="flex items-center justify-between text-xs mb-3">
          <div className="text-muted-foreground">
            {torrents.length === 0 && isLoading ? (
              "Loading torrents..."
            ) : totalCount === 0 ? (
              "No torrents found"
            ) : (
              <>
                {hasLoadedAll ? (
                  `${torrents.length} torrent${torrents.length !== 1 ? "s" : ""}`
                ) : isLoadingMore ? (
                  "Loading more torrents..."
                ) : (
                  `${safeLoadedRows} of ${totalCount} torrents loaded`
                )}
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  className="h-7 px-2 text-xs font-medium text-muted-foreground hover:text-foreground md:hidden"
                >
                  Sort: {currentSortOption.label}
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48 max-h-100 overflow-y-auto">
                <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                <DropdownMenuRadioGroup
                  value={sortField}
                  onValueChange={(value) => handleSortFieldChange(value as TorrentSortOptionValue)}
                >
                  {TORRENT_SORT_OPTIONS.map(option => (
                    <DropdownMenuRadioItem key={option.value} value={option.value} className="text-xs">
                      {option.label}
                    </DropdownMenuRadioItem>
                  ))}
                </DropdownMenuRadioGroup>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button
              variant="ghost"
              size="sm"
              onClick={toggleSortOrder}
              className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground md:hidden"
              aria-label={`Sort ${sortOrder === "desc" ? "descending" : "ascending"}`}
              title={`Sort ${sortOrder === "desc" ? "descending" : "ascending"}`}
            >
              {sortOrder === "desc" ? (
                <ChevronDown className="h-3.5 w-3.5" />
              ) : (
                <ChevronUp className="h-3.5 w-3.5" />
              )}
            </Button>
            <button
              onClick={() => setSpeedUnit(speedUnit === "bytes" ? "bits" : "bytes")}
              className="flex items-center gap-1 pl-1.5 py-0.5 rounded-sm transition-all hover:bg-muted/50"
            >
              <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">
                {speedUnit === "bytes" ? "MiB/s" : "Mbps"}
              </span>
            </button>
          </div>
        </div>

        {effectiveSearch && (
          <div className="mb-3 flex items-center justify-between gap-2 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-xs text-muted-foreground">
            <div className="flex min-w-0 items-center gap-2">
              <Search className="h-3.5 w-3.5 text-primary" aria-hidden="true" />
              <span className="truncate text-sm text-foreground" title={effectiveSearch}>
                {effectiveSearch}
              </span>
            </div>
            <Button
              variant="ghost"
              size="sm"
              onClick={handleClearSearch}
              className="h-7 px-2 text-xs font-medium text-primary hover:text-primary"
              aria-label="Clear search filter"
            >
              Clear
              <X className="ml-1 h-3 w-3" aria-hidden="true" />
            </Button>
          </div>
        )}

        {/* Selection mode header */}
        {selectionMode && (
          <div className="bg-primary text-primary-foreground px-4 py-2 mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  setSelectedHashes(new Set())
                  setSelectionMode(false)
                  setIsSelectionMode(false)
                  setIsAllSelected(false)
                  setExcludedFromSelectAll(new Set())
                }}
                className="p-1"
              >
                <X className="h-4 w-4" />
              </button>
              <span className="text-sm font-medium flex items-center gap-2">
                {isAllSelected ? `All ${effectiveSelectionCount}` : effectiveSelectionCount} selected
                {selectedTotalSize > 0 && (
                  <span className="text-xs text-primary-foreground/80">
                    • {selectedFormattedSize}
                  </span>
                )}
              </span>
            </div>
            <button
              onClick={handleSelectAll}
              className="text-sm font-medium"
            >
              {effectiveSelectionCount === totalCount ? "Deselect All" : "Select All"}
            </button>
          </div>
        )}
      </div>

      {/* Torrent cards with virtual scrolling */}
      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto overscroll-contain"
        style={{ paddingBottom: "calc(8rem + env(safe-area-inset-bottom))" }}
      >
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: "100%",
            position: "relative",
          }}
        >
          {virtualItems.map(virtualItem => {
            const torrent = torrents[virtualItem.index]
            const isSelected = isAllSelected ? !excludedFromSelectAll.has(torrent.hash) : selectedHashes.has(torrent.hash)

            return (
              <div
                key={virtualItem.key}
                data-index={virtualItem.index}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  height: `${virtualItem.size}px`,
                  transform: `translateY(${virtualItem.start}px)`,
                  paddingBottom: viewMode === "ultra-compact" ? "4px" : "8px",
                }}
              >
                <SwipeableCard
                  torrent={torrent}
                  isSelected={isSelected}
                  onSelect={(selected) => handleSelect(torrent.hash, selected)}
                  onClick={() => onTorrentSelect?.(torrent)}
                  onLongPress={handleLongPress}
                  incognitoMode={incognitoMode}
                  selectionMode={selectionMode}
                  speedUnit={speedUnit}
                  viewMode={viewMode}
                  supportsTrackerHealth={supportsTrackerHealth}
                  trackerIcons={trackerIcons}
                />
              </div>
            )
          })}
        </div>

        {/* Progressive loading implemented - shows loading indicator when needed */}
        {safeLoadedRows < torrents.length && !isLoadingMore && (
          <div className="p-4 text-center">
            <Button
              variant="ghost"
              onClick={loadMore}
              disabled={isLoadingMoreRows}
              className="text-muted-foreground"
            >
              {isLoadingMoreRows ? "Loading..." : "Load More"}
            </Button>
          </div>
        )}

        {isLoadingMore && (
          <div className="p-4 text-center text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin mx-auto mb-2" />
            <p className="text-sm">Loading more torrents...</p>
          </div>
        )}
      </div>

      {/* Fixed bottom action bar - visible in selection mode */}
      {selectionMode && effectiveSelectionCount > 0 && (
        <div
          className={cn(
            "fixed bottom-0 left-0 right-0 z-40 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50",
            "transition-transform duration-200 ease-in-out",
            selectionMode && effectiveSelectionCount > 0 ? "translate-y-0" : "translate-y-full"
          )}
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          <div className="flex items-center justify-around h-16">
            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RESUME)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Play className="h-5 w-5" />
              <span className="truncate">Resume</span>
            </button>

            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.PAUSE)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Pause className="h-5 w-5" />
              <span className="truncate">Pause</span>
            </button>

            <button
              onClick={() => {
                setActionTorrents(getSelectedTorrents)
                setShowCategoryDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Folder className="h-5 w-5" />
              <span className="truncate">Category</span>
            </button>

            <button
              onClick={() => {
                setActionTorrents(getSelectedTorrents)
                setShowSetTagsDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Tag className="h-5 w-5" />
              <span className="truncate">Tags</span>
            </button>

            <button
              onClick={() => setShowActionsSheet(true)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <MoreVertical className="h-5 w-5" />
              <span className="truncate">More</span>
            </button>
          </div>
        </div>
      )}

      {/* More actions sheet */}
      <Sheet open={showActionsSheet} onOpenChange={setShowActionsSheet}>
        <SheetContent side="bottom" className="h-auto pb-8">
          <SheetHeader>
            <SheetTitle>Actions
              for {isAllSelected ? `all ${effectiveSelectionCount}` : effectiveSelectionCount} torrent(s)</SheetTitle>
          </SheetHeader>
          <div className="grid gap-2 py-4 px-4">
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RECHECK)}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4" />
              Force Recheck
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.REANNOUNCE)}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4" />
              Reannounce
            </Button>
            {(() => {
              const seqDlStates = getSelectedTorrents?.map(t => t.seq_dl) ?? []
              const allSeqDlEnabled = seqDlStates.length > 0 && seqDlStates.every(state => state === true)
              return (
                <Button
                  variant="outline"
                  onClick={() => handleBulkAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, { enable: !allSeqDlEnabled })}
                  className="justify-start"
                >
                  <Blocks className="mr-2 h-4 w-4" />
                  {allSeqDlEnabled ? "Disable" : "Enable"} Sequential Download
                </Button>
              )
            })()}
            {onFilterChange && (
              <Button
                variant="outline"
                onClick={() => {
                  filterCrossSeeds(getSelectedTorrents)
                  setShowActionsSheet(false)
                }}
                disabled={isFilteringCrossSeeds || getSelectedTorrents.length !== 1}
                className="justify-start"
              >
                <GitBranch className="mr-2 h-4 w-4" />
                Filter Cross-Seeds
              </Button>
            )}
            {canCrossSeedSearch && onCrossSeedSearch && (
              <Button
                variant="outline"
                onClick={() => {
                  if (!singleSelectedTorrent) {
                    return
                  }
                  onCrossSeedSearch(singleSelectedTorrent)
                  setShowActionsSheet(false)
                }}
                disabled={!singleSelectedTorrent || isCrossSeedSearching}
                className="justify-start"
              >
                <Search className="mr-2 h-4 w-4" />
                Search Cross-Seeds
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.INCREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              Increase Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.DECREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              Decrease Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.TOP_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              Top Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.BOTTOM_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              Bottom Priority
            </Button>
            {(() => {
              // Check TMM state across selected torrents
              const tmmStates = getSelectedTorrents?.map(t => t.auto_tmm) ?? []
              const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
              const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
              const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

              if (mixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => {
                        const hashes = isAllSelected ? [] : Array.from(selectedHashes)
                        prepareTmmAction(hashes, effectiveSelectionCount, true)
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4" />
                      Enable TMM (Mixed)
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => {
                        const hashes = isAllSelected ? [] : Array.from(selectedHashes)
                        prepareTmmAction(hashes, effectiveSelectionCount, false)
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4" />
                      Disable TMM (Mixed)
                    </Button>
                  </>
                )
              }

              return (
                <Button
                  variant="outline"
                  onClick={() => {
                    const hashes = isAllSelected ? [] : Array.from(selectedHashes)
                    prepareTmmAction(hashes, effectiveSelectionCount, !allEnabled)
                    setShowActionsSheet(false)
                  }}
                  className="justify-start"
                >
                  {allEnabled ? (
                    <>
                      <Settings2 className="mr-2 h-4 w-4" />
                      Disable TMM
                    </>
                  ) : (
                    <>
                      <Settings2 className="mr-2 h-4 w-4" />
                      Enable TMM
                    </>
                  )}
                </Button>
              )
            })()}
            <Button
              variant="outline"
              onClick={() => {
                setShowShareLimitDialog(true)
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <Sprout className="mr-2 h-4 w-4" />
              Set Share Limits
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setShowSpeedLimitDialog(true)
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <Gauge className="mr-2 h-4 w-4" />
              Set Speed Limits
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setActionTorrents(getSelectedTorrents)
                prepareLocationAction(
                  isAllSelected ? [] : Array.from(selectedHashes),
                  getSelectedTorrents
                )
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <FolderOpen className="mr-2 h-4 w-4" />
              Set Location
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                prepareDeleteAction(
                  isAllSelected ? [] : Array.from(selectedHashes),
                  getSelectedTorrents
                )
                setShowActionsSheet(false)
              }}
              className="justify-start !bg-destructive !text-destructive-foreground"
            >
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      {/* Delete confirmation dialog */}
      <DeleteTorrentDialog
        open={showDeleteDialog}
        onOpenChange={(open) => {
          if (!open) {
            closeDeleteDialog()
            crossSeedWarning.reset()
            setTorrentToDelete(null)
          }
        }}
        count={torrentToDelete ? 1 : effectiveSelectionCount}
        totalSize={selectedTotalSize}
        formattedSize={selectedFormattedSize}
        deleteFiles={deleteFiles}
        onDeleteFilesChange={setDeleteFiles}
        isDeleteFilesLocked={isDeleteFilesLocked}
        onToggleDeleteFilesLock={toggleDeleteFilesLock}
        deleteCrossSeeds={deleteCrossSeeds}
        onDeleteCrossSeedsChange={setDeleteCrossSeeds}
        crossSeedWarning={crossSeedWarning}
        onConfirm={handleDeleteWrapper}
      />

      {/* Tags dialog */}
      <SetTagsDialog
        open={showSetTagsDialog}
        onOpenChange={setShowSetTagsDialog}
        availableTags={availableTags || []}
        hashCount={actionTorrents.length}
        onConfirm={handleSetTagsWrapper}
        isPending={isPending}
        initialTags={getCommonTags(actionTorrents)}
      />

      {/* Category dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={actionTorrents.length}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(actionTorrents)}
        useSubcategories={allowSubcategories}
      />

      {/* Remove Tags dialog */}
      <RemoveTagsDialog
        open={showRemoveTagsDialog}
        onOpenChange={setShowRemoveTagsDialog}
        availableTags={availableTags || []}
        hashCount={actionTorrents.length}
        onConfirm={async (tags) => {
          const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
          const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : actionTorrents.map(t => t.hash)
          const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length
          await handleRemoveTags(
            tags,
            hashes,
            isAllSelected,
            isAllSelected ? filters : undefined,
            isAllSelected ? effectiveSearch : undefined,
            isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
            {
              clientHashes: visibleHashes,
              totalSelected,
            }
          )
          setActionTorrents([])
        }}
        isPending={isPending}
      />

      {/* Share Limits Dialog */}
      <MobileShareLimitsDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={effectiveSelectionCount}
        onConfirm={async (ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit) => {
          const hashes = isAllSelected ? [] : Array.from(selectedHashes)
          const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : Array.from(selectedHashes)
          const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length || 1
          await handleSetShareLimit(
            ratioLimit,
            seedingTimeLimit,
            inactiveSeedingTimeLimit,
            hashes,
            isAllSelected,
            isAllSelected ? filters : undefined,
            isAllSelected ? effectiveSearch : undefined,
            isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
            {
              clientHashes: visibleHashes,
              totalSelected,
            }
          )
          setShowShareLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Speed Limits Dialog */}
      <MobileSpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={effectiveSelectionCount}
        onConfirm={async (uploadLimit, downloadLimit) => {
          const hashes = isAllSelected ? [] : Array.from(selectedHashes)
          const visibleHashes = isAllSelected ? torrents.filter(t => !excludedFromSelectAll.has(t.hash)).map(t => t.hash) : Array.from(selectedHashes)
          const totalSelected = isAllSelected ? effectiveSelectionCount : visibleHashes.length || 1
          await handleSetSpeedLimits(
            uploadLimit,
            downloadLimit,
            hashes,
            isAllSelected,
            isAllSelected ? filters : undefined,
            isAllSelected ? effectiveSearch : undefined,
            isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
            {
              clientHashes: visibleHashes,
              totalSelected,
            }
          )
          setShowSpeedLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={effectiveSelectionCount}
        initialLocation={getCommonSavePath(getSelectedTorrents)}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
      />

      {/* TMM Confirmation Dialog */}
      <TmmConfirmDialog
        open={showTmmDialog}
        onOpenChange={setShowTmmDialog}
        count={effectiveSelectionCount}
        enable={pendingTmmEnable}
        onConfirm={handleTmmConfirmWrapper}
        isPending={isPending}
      />

      {/* Location Warning Dialog */}
      <LocationWarningDialog
        open={showLocationWarningDialog}
        onOpenChange={setShowLocationWarningDialog}
        count={effectiveSelectionCount}
        onConfirm={proceedToLocationDialog}
        isPending={isPending}
      />

      {/* Search Modal */}
      <Dialog open={showSearchModal} onOpenChange={setShowSearchModal}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Search Torrents</DialogTitle>
            <DialogDescription>
              Search by name, category, or tags. Supports glob patterns like *.mkv or *1080p*.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
              <Input
                placeholder="Search torrents..."
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    setShowSearchModal(false)
                  } else if (e.key === "Escape") {
                    handleClearSearch()
                    setShowSearchModal(false)
                  }
                }}
                className={`w-full pl-9 ${globalFilter ? "ring-1 ring-primary/50" : ""
                  } ${globalFilter && /[*?[\]]/.test(globalFilter) ? "ring-1 ring-primary" : ""}`}
                autoFocus
              />
              {globalFilter && (
                <button
                  type="button"
                  className="absolute right-2 top-1/2 -translate-y-1/2 p-1 hover:bg-muted rounded-sm transition-colors"
                  onClick={handleClearSearch}
                  aria-label="Clear search"
                >
                  <X className="h-3.5 w-3.5 text-muted-foreground" />
                </button>
              )}
            </div>

            <div className="space-y-2 text-xs text-muted-foreground bg-muted/50 rounded-lg p-3">
              <div className="flex items-start gap-2">
                <Info className="h-4 w-4 flex-shrink-0 mt-0.5" />
                <div className="space-y-1">
                  <p className="font-semibold">Search Features:</p>
                  <ul className="space-y-1 ml-2">
                    <li>• <strong>Glob patterns:</strong> *.mkv, *1080p*, S??E??</li>
                    <li>• <strong>Fuzzy matching:</strong> "breaking bad" finds "Breaking.Bad"</li>
                    <li>• Searches name, category, and tags</li>
                    <li>• Auto-searches after typing</li>
                  </ul>
                </div>
              </div>
            </div>
          </div>
          <DialogFooter className="sm:justify-between">
            <Button variant="outline" onClick={handleClearSearchAndClose}>
              Clear
            </Button>
            <Button onClick={() => setShowSearchModal(false)}>
              Done
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add torrent dialog */}
      <AddTorrentDialog
        instanceId={instanceId}
        open={addTorrentModalOpen}
        onOpenChange={onAddTorrentModalChange}
      />

      {/* Fixed bottom navbar - only visible when not in selection mode */}
      {!selectionMode && (
        <div
          className={cn(
            "fixed left-0 right-0 z-50 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50"
          )}
          style={{ bottom: "calc(4rem + env(safe-area-inset-bottom))" }}
        >
          <div className="flex items-center justify-around h-14 px-2">
            <button
              onClick={() => setShowSearchModal(true)}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              <Search className="h-5 w-5" />
              <span className="truncate text-[10px]">Search</span>
            </button>

            <button
              onClick={() => setIncognitoMode(!incognitoMode)}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              {incognitoMode ? <EyeOff className="h-5 w-5" /> : <Eye className="h-5 w-5" />}
              <span className="truncate text-[10px]">Incognito</span>
            </button>

            <button
              onClick={() => window.dispatchEvent(new Event("qui-open-mobile-filters"))}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              <Filter className="h-5 w-5" />
              <span className="truncate text-[10px]">Filters</span>
            </button>

            <button
              onClick={() => onAddTorrentModalChange?.(true)}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              <Plus className="h-5 w-5" />
              <span className="truncate text-[10px]">Add</span>
            </button>

            {supportsTorrentCreation && (
              <button
                onClick={() => {
                  const next = { ...(routeSearch || {}), modal: "create-torrent" }
                  navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                }}
                className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
              >
                <FileEdit className="h-5 w-5" />
                <span className="truncate text-[10px]">Create</span>
              </button>
            )}

            {supportsTorrentCreation && (
              <button
                onClick={() => {
                  const next = { ...(routeSearch || {}), modal: "tasks" }
                  navigate({ search: next as any, replace: true }) // eslint-disable-line @typescript-eslint/no-explicit-any
                }}
                className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95 relative"
              >
                <ListTodo className="h-5 w-5" />
                {activeTaskCount > 0 && (
                  <Badge variant="default" className="absolute top-0 right-1 h-4 min-w-4 flex items-center justify-center p-0 text-[9px]">
                    {activeTaskCount}
                  </Badge>
                )}
                <span className="truncate text-[10px]">Tasks</span>
              </button>
            )}
          </div>
        </div>
      )}

      {/* Scroll to top button - only on mobile */}
      <div className="sm:hidden">
        <ScrollToTopButton
          scrollContainerRef={parentRef}
          className="right-4 z-[60] bottom-[calc(8rem+env(safe-area-inset-bottom))]"
        />
      </div>
    </div>
  )
}
