/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
import { Progress } from "@/components/ui/progress"
import { ScrollToTopButton } from "@/components/ui/scroll-to-top-button"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useMobileScroll, useRegisterMobileScrollContainer } from "@/contexts/MobileScrollContext"
import { useActiveTaskCount } from "@/hooks/useActiveTaskCount"
import { useDebounce } from "@/hooks/useDebounce"
import { TORRENT_ACTIONS, useTorrentActions, type TorrentAction } from "@/hooks/useTorrentActions"
import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { useTorrentListController, useTorrentListCrossSeed } from "@/hooks/useTorrentListController"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { useClientSetting } from "@/lib/client-settings"
import { EMPTY_SIDEBAR_FILTERS, hasSidebarFilters } from "@/lib/filter-views"
import { buildTrackerCustomizationLookup, extractTrackerHost, getTrackerCustomizationsCacheKey, resolveTrackerDisplay, type TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { resolveTrackerHealthSupport } from "@/lib/tracker-health-support"
import { resolveTrackerIconSrc } from "@/lib/tracker-icons"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import { actionTargetRequest, buildActionTarget, getSelectionIdentity as selectionIdentity, hasSelection, rememberTickedTorrents, selectAllButtonEvent, type ActionTargetScope } from "@/lib/torrent-selection"
import { getCommonCategory, getCommonSavePath, getToggleSelectionState, getTorrentHashesWithTag, getTotalSize } from "@/lib/torrent-utils"
import { isAllInstancesScope } from "@/lib/instances"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { navigateWithSearch } from "@/lib/router-search"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowUpDown,
  Blocks,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  Download,
  Eye,
  EyeOff,
  FastForward,
  FileEdit,
  FileUp,
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
import { useTranslation } from "react-i18next"
import { AddTorrentDialog } from "./AddTorrentDialog"
import { DeleteTorrentDialog } from "./DeleteTorrentDialog"
import { TorrentListEmptyState } from "./TorrentListEmptyState"
import {
  LocationWarningDialog,
  SetCategoryDialog,
  SetLocationDialog,
  ShareLimitDialog,
  SpeedLimitsDialog,
  TagEditorDialog,
  TmmConfirmDialog
} from "./TorrentDialogs"
// import { createPortal } from 'react-dom'
// Columns dropdown removed on mobile
import { useTorrentSelectionContext } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedFilter } from "@/hooks/useCrossSeedFilter"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata.ts"
import { usePersistedCompactViewState, type ViewMode } from "@/hooks/usePersistedCompactViewState"
import { useSubcategoriesEnabled } from "@/hooks/usePreferenceValues"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { unitLabel } from "@/lib/unit-format"
import { getStatusBadgeProps } from "@/lib/torrent-table/row-display"
import { cn, formatBytes, getRatioColor } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts, TorrentFilters } from "@/types"
import { getDefaultSortOrder, TORRENT_SORT_OPTIONS, type TorrentSortOptionValue } from "./torrentSortOptions"

interface TorrentCardsMobileProps {
  instanceId: number
  instanceIds?: number[]
  filters: TorrentFilters
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: TorrentCounts, categories?: Record<string, Category>, tags?: string[], useSubcategories?: boolean, supportsTrackerHealth?: boolean) => void
  onFilterChange?: (filters: TorrentFilters) => void
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
  onManualCrossSeed?: (torrent: Torrent) => void
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

function parseMobileSortState(raw: string): MobileSortState {
  const parsed = JSON.parse(raw) as Partial<MobileSortState>
  const field = isValidSortField(parsed?.field) ? parsed.field : DEFAULT_MOBILE_SORT_STATE.field
  const defaultOrder = getDefaultSortOrder(field)
  const order = parsed?.order === "asc" || parsed?.order === "desc" ? parsed.order : defaultOrder
  return { field, order }
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
  const host = extractTrackerHost(tracker)
  if (!host) {
    return {
      host: "",
      fallback: "#",
      title: "",
    }
  }

  const fallbackLetter = host.charAt(0).toUpperCase()

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
  trackerCustomizationLookup,
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
  trackerCustomizationLookup?: TrackerCustomizationLookup
}) {
  const { t } = useTranslation("torrents")

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
    () => getStatusBadgeProps(torrent, supportsTrackerHealth, t),
    [torrent, supportsTrackerHealth, t]
  )
  const trackerValue = incognitoMode ? getLinuxTracker(torrent.hash) : torrent.tracker
  const trackerMeta = useMemo(() => getTrackerDisplayMeta(trackerValue), [trackerValue])
  // Resolve custom display name from customizations
  const trackerDisplayInfo = useMemo(() => {
    if (!trackerCustomizationLookup || trackerCustomizationLookup.size === 0) {
      return { displayName: trackerMeta.host, primaryDomain: trackerMeta.host, isCustomized: false }
    }
    return resolveTrackerDisplay(trackerMeta.host, trackerCustomizationLookup)
  }, [trackerMeta.host, trackerCustomizationLookup])
  // Use primary domain for icon lookup (so merged trackers share icons)
  const iconDomain = trackerDisplayInfo.primaryDomain || trackerMeta.host
  const trackerIconSrc = resolveTrackerIconSrc(trackerIcons, iconDomain, trackerMeta.host)
  // Display name is either custom name or hostname
  const trackerDisplayName = trackerDisplayInfo.displayName || trackerMeta.title

  return (
    <div
      className={cn(
        "bg-card rounded-lg border cursor-pointer transition-all relative overflow-hidden select-none",
        viewMode === "ultra-compact" ? "px-3 py-1" : viewMode === "compact" ? "p-2" : "p-4",
        isSelected && "bg-accent/50",
        !selectionMode && "active:scale-[0.98]",
        selectionMode && viewMode !== "normal" && "pr-10"
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
      {/* Selection checkbox - visible in selection mode */}
      {selectionMode && (
        <div
          className={cn(
            "absolute right-2 z-10",
            viewMode === "normal" ? "top-2" : "top-1/2 -translate-y-1/2"
          )}
        >
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
                <h3 className={cn(
                  "font-medium text-sm inline-block whitespace-nowrap",
                  selectionMode && "pr-8"
                )} title={displayName}>
                  {displayName}
                </h3>
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
              <span className="text-muted-foreground">{t("mobileCards.ratio")}</span>
              <span
                className="font-medium"
                style={{ color: getRatioColor(displayRatio) }}
              >
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
                <span className="text-muted-foreground">{t("mobileCards.ratio")}</span>
                <span
                  className="font-medium"
                  style={{ color: getRatioColor(displayRatio) }}
                >
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

      {/* Bottom row: Tracker/Category/Tags and Status/Speeds - only for compact and full views */}
      {viewMode === "compact" ? (
        /* Compact version: Tracker/Category/tags on left, percentage/speeds on right */
        <div className="flex items-center justify-between gap-2 text-xs mt-1">
          {/* Left side: Tracker, Category and Tags */}
          <div className="flex items-center gap-2 text-muted-foreground min-w-0 overflow-hidden">
            {trackerDisplayName && (
              <span className="flex items-center gap-1 flex-shrink-0" title={trackerDisplayInfo.isCustomized ? `${trackerDisplayName} (${trackerMeta.host})` : trackerDisplayName}>
                <TrackerIcon
                  title={trackerDisplayInfo.isCustomized ? `${trackerDisplayName} (${trackerMeta.host})` : trackerDisplayName}
                  fallback={trackerMeta.fallback}
                  src={trackerIconSrc}
                  size="xs"
                />
                {trackerDisplayName}
              </span>
            )}
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
        /* Full version: Original layout with tracker, category, tags */
        <div className="flex items-center justify-between gap-2 min-h-[20px]">
          {/* Left side: Tracker and Category */}
          <div className="flex items-center gap-3 flex-shrink-0">
            {trackerDisplayName && (
              <div className="flex items-center gap-1" title={trackerDisplayInfo.isCustomized ? `${trackerDisplayName} (${trackerMeta.host})` : trackerDisplayName}>
                <TrackerIcon
                  title={trackerDisplayInfo.isCustomized ? `${trackerDisplayName} (${trackerMeta.host})` : trackerDisplayName}
                  fallback={trackerMeta.fallback}
                  src={trackerIconSrc}
                  size="xs"
                />
                <span className="text-xs text-muted-foreground">{trackerDisplayName}</span>
              </div>
            )}
            {displayCategory && (
              <div className="flex items-center gap-1">
                <Folder className="h-3 w-3 text-muted-foreground" />
                <span className="text-xs text-muted-foreground">{displayCategory}</span>
              </div>
            )}
          </div>

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
  instanceIds,
  filters,
  onTorrentSelect,
  addTorrentModalOpen,
  onAddTorrentModalChange,
  onFilteredDataUpdate,
  onFilterChange,
  canCrossSeedSearch,
  onCrossSeedSearch,
  isCrossSeedSearching,
  onManualCrossSeed,
}: TorrentCardsMobileProps) {
  const { t } = useTranslation("torrents")
  const isAllInstancesView = isAllInstancesScope(instanceId)
  // State
  const [sortState, setSortState] = useClientSetting<MobileSortState>(
    `${MOBILE_SORT_STORAGE_KEY}:${instanceId}`,
    { defaultValue: DEFAULT_MOBILE_SORT_STATE, parse: parseMobileSortState }
  )
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  // Selection keys: hash for single-instance, `${instanceId}:${hash}` for unified scope.
  const { selection, dispatch: dispatchSelection } = useTorrentSelectionContext()
  const { isAllSelected, selected: selectedHashes, excluded: excludedFromSelectAll } = selection
  // Selection mode means that something is selected, Select all mode included.
  const selectionMode = hasSelection(selection)

  const parentRef = useRef<HTMLDivElement>(null)
  const { isFooterVisible } = useMobileScroll()
  useRegisterMobileScrollContainer(parentRef)

  const [showActionsSheet, setShowActionsSheet] = useState(false)
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
  }, [setSortState])

  const toggleSortOrder = useCallback(() => {
    setSortState(prev => ({
      field: prev.field,
      order: prev.order === "desc" ? "asc" : "desc",
    }))
  }, [setSortState])

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit, setSpeedUnit] = useSpeedUnits()
  const { viewMode } = usePersistedCompactViewState("mobile")
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

  // Tracker customizations for custom display names and merged domains
  const trackerCustomizationsQuery = useTrackerCustomizations()
  const trackerCustomizationsRef = useRef<{ key: string; lookup: TrackerCustomizationLookup } | undefined>(undefined)
  const trackerCustomizationLookup = useMemo(() => {
    const latest = trackerCustomizationsQuery.data
    if (!latest) {
      return trackerCustomizationsRef.current?.lookup ?? new Map()
    }

    // Build a cache key from ids + updatedAt to detect any changes
    const newKey = getTrackerCustomizationsCacheKey(latest)

    // Check if the lookup has changed using the cache key
    const previous = trackerCustomizationsRef.current
    if (previous && previous.key === newKey) {
      return previous.lookup
    }

    // Build a new lookup map from the customizations
    const newLookup = buildTrackerCustomizationLookup(latest)
    trackerCustomizationsRef.current = { key: newKey, lookup: newLookup }
    return newLookup
  }, [trackerCustomizationsQuery.data])

  // Use the shared torrent actions hook
  const {
    showDeleteDialog,
    closeDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    isDeleteFilesLocked,
    toggleDeleteFilesLock,
    blockCrossSeeds,
    setBlockCrossSeeds,
    deleteCrossSeeds,
    setDeleteCrossSeeds,
    showTagsDialog,
    setShowTagsDialog,
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
    handleUpdateTags,
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
    instanceIds,
    onActionComplete: (action) => {
      if (action === TORRENT_ACTIONS.DELETE) {
        dispatchSelection({ type: "clear" })
      }
    },
  })

  const { data: metadata } = useInstanceMetadata(instanceId, { fallbackDelayMs: 1500 })
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}

  const debouncedSearch = useDebounce(globalFilter, 1000)
  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string }
  const searchFromRoute = routeSearch?.q || ""

  const effectiveSearch = searchFromRoute || immediateSearch || debouncedSearch
  const navigate = useNavigate()
  const activeTaskCount = useActiveTaskCount(instanceId, !isAllInstancesView)

  // Columns controls removed on mobile

  useEffect(() => {
    if (searchFromRoute !== globalFilter) {
      setGlobalFilter(searchFromRoute)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchFromRoute])

  // The search modal filters without the route `q`, so the page scope does not
  // see it. A search change clears the selection here instead.
  const selectionSearchRef = useRef(effectiveSearch)
  useEffect(() => {
    if (selectionSearchRef.current !== effectiveSearch) {
      selectionSearchRef.current = effectiveSearch
      dispatchSelection({ type: "clear" })
    }
  }, [effectiveSearch, dispatchSelection])

  // Fetch data
  const {
    torrents,
    totalCount,
    counts,
    categories,
    tags,
    stats,
    useSubcategories: subcategoriesFromData,
    trackerHealthSupported,

    isLoading,
    isLoadingMore,
    hasLoadedAll,
    loadedRows,
    hasMoreRows,
    reportNearEnd,
    listKey,
    isCrossInstanceEndpoint,
  } = useTorrentListController({
    instanceId,
    instanceIds,
    search: effectiveSearch,
    filters,
    sort: backendSortField,
    order: sortOrder,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: instanceId > 0 })
  const { exportTorrents, isExporting } = useTorrentExporter({ instanceId, incognitoMode })
  const supportsTrackerHealth = resolveTrackerHealthSupport({
    isUnifiedView: isAllInstancesView,
    capabilitySupport: capabilities?.supportsTrackerHealth,
    responseSupport: trackerHealthSupported,
  })
  const supportsTorrentCreation = isAllInstancesView ? false : (capabilities?.supportsTorrentCreation ?? true)
  const instanceSubcategoriesEnabled = useSubcategoriesEnabled(instanceId, capabilities, subcategoriesFromData)
  // subcategoriesFromData reflects backend/server state; allowSubcategories
  // additionally respects user preferences for UI surfaces like dialogs.
  const allowSubcategories = isAllInstancesView? Boolean(subcategoriesFromData): instanceSubcategoriesEnabled

  // The cross-seed filter also spans instances, so the keys follow the endpoint, not the route.
  const getSelectionIdentity = useCallback(
    (torrent: Torrent): string => selectionIdentity(torrent, isCrossInstanceEndpoint, instanceId),
    [isCrossInstanceEndpoint, instanceId]
  )

  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags, allowSubcategories, supportsTrackerHealth)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, allowSubcategories, onFilteredDataUpdate, supportsTrackerHealth]) // Update when data changes

  const tickedTorrentsRef = useRef(new Map<string, Torrent>())
  // Every bulk action on the cards reads this target (ADR 0034). The cards have no Column filters.
  const actionTarget = useMemo(() => {
    const ticked = rememberTickedTorrents(tickedTorrentsRef.current, selectedHashes, torrents, getSelectionIdentity)
    const scope: ActionTargetScope = { instanceId, instanceIds, filters, columnFilters: null, search: effectiveSearch, isCrossInstance: isCrossInstanceEndpoint }
    return buildActionTarget(selection, scope, { loaded: torrents, ticked, totalCount })
  }, [selection, selectedHashes, torrents, getSelectionIdentity, instanceId, instanceIds, filters, effectiveSearch, isCrossInstanceEndpoint, totalCount])
  const { count: effectiveSelectionCount, previewTorrents: selectedTorrents } = actionTarget

  const selectionBarVisible = selectionMode && effectiveSelectionCount > 0

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
        if (excludedFromSelectAll.has(getSelectionIdentity(torrent))) {
          return total + (torrent.size || 0)
        }
        return total
      }, 0)

      return Math.max(aggregateTotalSize - excludedSize, 0)
    }

    return getTotalSize(selectedTorrents)
  }, [isAllSelected, stats?.totalSize, excludedFromSelectAll, torrents, selectedTorrents, getSelectionIdentity])

  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  const { crossSeedWarning, hasCrossSeedTag, shouldBlockCrossSeeds, blockCrossSeedHashes } = useTorrentListCrossSeed({
    instanceId,
    actionTarget,
    blockCrossSeeds,
  })

  // Virtual scrolling with consistent spacing
  const virtualizer = useVirtualizer({
    count: loadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => viewMode === "ultra-compact" ? 32 : viewMode === "compact" ? 86 : 180,
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

      // Only report the end when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive reports during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling
      // A scroll position past the last row belongs to the previous list: the
      // window shrank before the Layout scrolled back to the top.
      const scrolledPastRows = (instance.scrollOffset ?? 0) > instance.getTotalSize()

      if (shouldCheckLoadMore && !scrolledPastRows && lastItem && lastItem.index >= loadedRows - 20 && hasMoreRows) {
        reportNearEnd();
      }
    },
  })

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [loadedRows, virtualizer])

  const virtualItems = virtualizer.getVirtualItems()

  // The controller starts the list again at the first rows; scroll to them.
  useEffect(() => {
    if (parentRef.current) {
      parentRef.current.scrollTop = 0
    }
    virtualizer.scrollToOffset(0)
  }, [listKey, virtualizer])

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
    dispatchSelection({ type: "selectOnly", key: getSelectionIdentity(torrent) })
  }, [dispatchSelection, getSelectionIdentity])

  const handleSelect = useCallback((key: string, selected: boolean) => {
    dispatchSelection({ type: "tick", keys: [key], checked: selected })
  }, [dispatchSelection])

  const handleSelectAll = useCallback(() => {
    dispatchSelection(selectAllButtonEvent(selection, totalCount))
  }, [dispatchSelection, selection, totalCount])

  const handleBulkAction = useCallback((action: TorrentAction, options?: { enable?: boolean }) => {
    handleAction(action, actionTarget, options)
    setShowActionsSheet(false)
  }, [handleAction, actionTarget])

  const handleDeleteWrapper = useCallback(async () => {
    const crossSeedTargets = deleteCrossSeeds ? buildTorrentActionTargets(crossSeedWarning.affectedTorrents, instanceId) : []

    if (shouldBlockCrossSeeds) {
      const taggedHashes = getTorrentHashesWithTag(selectedTorrents, "cross-seed")
      const crossSeedTagHashesToBlock = deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedWarning.affectedTorrents, "cross-seed") : []
      await blockCrossSeedHashes([...taggedHashes, ...crossSeedTagHashesToBlock], [...buildTorrentActionTargets(selectedTorrents, instanceId), ...crossSeedTargets])
    }

    await handleDelete(actionTarget, crossSeedTargets)
  }, [actionTarget, blockCrossSeedHashes, crossSeedWarning.affectedTorrents, deleteCrossSeeds, handleDelete, instanceId, selectedTorrents, shouldBlockCrossSeeds])

  const handleTagsWrapper = useCallback(async (plan: Parameters<typeof handleUpdateTags>[0]) => {
    await handleUpdateTags(plan, actionTarget)
  }, [handleUpdateTags, actionTarget])

  const handleSetCategoryWrapper = useCallback(async (category: string) => {
    await handleSetCategory(category, actionTarget)
  }, [handleSetCategory, actionTarget])

  const handleSetLocationWrapper = useCallback(async (location: string) => {
    await handleSetLocation(location, actionTarget)
  }, [handleSetLocation, actionTarget])

  const handleExport = () => {
    void exportTorrents(actionTarget, backendSortField, sortOrder)
    setShowActionsSheet(false)
  }

  const { isFilteringCrossSeeds, filterCrossSeeds } = useCrossSeedFilter({
    instanceId,
    onFilterChange,
  })

  const singleSelectedTorrent = selectedTorrents[0] ?? null

  // select-all: unloaded rows have unknown state, so offer both directions.
  const stateUnknownForSelection = isAllSelected && selectedTorrents.length < effectiveSelectionCount

  const handleClearSearch = useCallback(() => {
    setGlobalFilter("")

    if (routeSearch && Object.prototype.hasOwnProperty.call(routeSearch, "q")) {
      const next = { ...(routeSearch || {}) }
      delete next.q
      navigateWithSearch({ navigate, search: next, replace: true })
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
        {/* Stats bar - swapped for the selection header in selection mode so the list doesn't shift */}
        {selectionMode ? (
          <div className="bg-primary text-primary-foreground px-4 mb-3 flex min-h-7 items-center justify-between">
            <div className="flex items-center gap-2">
              <button
                onClick={() => dispatchSelection({ type: "clear" })}
                className="p-1"
              >
                <X className="h-4 w-4" />
              </button>
              <span className="text-sm font-medium flex items-center gap-2">
                {isAllSelected? t("mobileCards.selection.allSelectedCount", { count: effectiveSelectionCount }): t("mobileCards.selection.selectedCount", { count: effectiveSelectionCount })}
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
              {effectiveSelectionCount === totalCount ? t("detailsPanel.deselectAll") : t("detailsPanel.selectAll")}
            </button>
          </div>
        ) : (
          <div className="flex items-center justify-between text-xs mb-3">
            <div className="text-muted-foreground">
              {torrents.length === 0 && isLoading ? (
                t("statusBar.loadingTorrents")
              ) : totalCount === 0 ? (
                t("mobileCards.noTorrentsFound")
              ) : (
                <>
                  {hasLoadedAll ? (
                    t("statusBar.torrentCount", { count: torrents.length })
                  ) : isLoadingMore ? (
                    t("statusBar.loadingMore")
                  ) : (
                    t("statusBar.torrentsLoaded", { loaded: loadedRows, total: totalCount })
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
                    {t("mobileCards.sort")}: {t(currentSortOption.labelKey)}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-48 max-h-100 overflow-y-auto">
                  <DropdownMenuLabel>{t("mobileCards.sortBy")}</DropdownMenuLabel>
                  <DropdownMenuRadioGroup
                    value={sortField}
                    onValueChange={(value) => handleSortFieldChange(value as TorrentSortOptionValue)}
                  >
                    {TORRENT_SORT_OPTIONS.map(option => (
                      <DropdownMenuRadioItem key={option.value} value={option.value} className="text-xs">
                        {t(option.labelKey)}
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
                aria-label={`${t("sort.label")} ${sortOrder === "desc" ? t("sort.descending") : t("sort.ascending")}`}
                title={`${t("sort.label")} ${sortOrder === "desc" ? t("sort.descending") : t("sort.ascending")}`}
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
                  {speedUnit === "bytes" ? unitLabel("MiB", true) : unitLabel("Mb", true)}
                </span>
              </button>
            </div>
          </div>
        )}

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
              aria-label={t("mobileCards.clearSearchFilter")}
            >
              {t("mobileCards.clear")}
              <X className="ml-1 h-3 w-3" aria-hidden="true" />
            </Button>
          </div>
        )}
      </div>

      {/* Torrent cards with virtual scrolling */}
      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto overscroll-contain"
        style={{
          paddingBottom: selectionBarVisible? "calc(4rem + env(safe-area-inset-bottom))": isFooterVisible? "calc(8rem + env(safe-area-inset-bottom))": "env(safe-area-inset-bottom)",
        }}
      >
        {torrents.length === 0 && !isLoading && (
          <div className="flex h-full items-center justify-center px-4">
            <TorrentListEmptyState
              hasFilters={hasSidebarFilters(filters)}
              hasSearch={Boolean(effectiveSearch)}
              onClearFilters={onFilterChange ? () => onFilterChange(EMPTY_SIDEBAR_FILTERS) : undefined}
            />
          </div>
        )}
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: "100%",
            position: "relative",
          }}
        >
          {virtualItems.map(virtualItem => {
            const torrent = torrents[virtualItem.index]
            const selectionIdentity = getSelectionIdentity(torrent)
            const isSelected = isAllSelected ? !excludedFromSelectAll.has(selectionIdentity) : selectedHashes.has(selectionIdentity)

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
                  onSelect={(selected) => handleSelect(selectionIdentity, selected)}
                  onClick={() => onTorrentSelect?.(torrent)}
                  onLongPress={handleLongPress}
                  incognitoMode={incognitoMode}
                  selectionMode={selectionMode}
                  speedUnit={speedUnit}
                  viewMode={viewMode}
                  supportsTrackerHealth={supportsTrackerHealth}
                  trackerIcons={trackerIcons}
                  trackerCustomizationLookup={trackerCustomizationLookup}
                />
              </div>
            )
          })}
        </div>

        {hasMoreRows && !isLoadingMore && (
          <div className="p-4 text-center">
            <Button
              variant="ghost"
              onClick={reportNearEnd}
              className="text-muted-foreground"
            >
              {t("mobileCards.loadMore")}
            </Button>
          </div>
        )}

        {isLoadingMore && (
          <div className="p-4 text-center text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin mx-auto mb-2" />
            <p className="text-sm">{t("statusBar.loadingMore")}</p>
          </div>
        )}
      </div>

      {/* Fixed bottom action bar - visible in selection mode */}
      {selectionBarVisible && (
        <div
          className="fixed bottom-0 left-0 right-0 z-40 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50"
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          <div className="flex items-center justify-around h-16">
            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RESUME)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Play className="h-5 w-5" />
              <span className="truncate">{t("managementBar.resume")}</span>
            </button>

            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.PAUSE)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Pause className="h-5 w-5" />
              <span className="truncate">{t("managementBar.pause")}</span>
            </button>

            <button
              onClick={() => {
                setShowCategoryDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Folder className="h-5 w-5" />
              <span className="truncate">{t("managementBar.setCategory")}</span>
            </button>

            <button
              onClick={() => {
                setShowTagsDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Tag className="h-5 w-5" />
              <span className="truncate">{t("managementBar.setTags")}</span>
            </button>

            <button
              onClick={() => setShowActionsSheet(true)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <MoreVertical className="h-5 w-5" />
              <span className="truncate">{t("mobileCards.more")}</span>
            </button>
          </div>
        </div>
      )}

      {/* More actions sheet */}
      <Sheet open={showActionsSheet} onOpenChange={setShowActionsSheet}>
        <SheetContent side="bottom" className="max-h-[85dvh] pb-8">
          <SheetHeader>
            <SheetTitle>
              {isAllSelected? t("mobileCards.actionsForAll", { count: effectiveSelectionCount }): t("mobileCards.actionsForCount", { count: effectiveSelectionCount })}
            </SheetTitle>
          </SheetHeader>
          <div className="grid gap-2 py-4 px-4 min-h-0 overflow-y-auto">
            {(() => {
              const { allEnabled: allForceStarted, mixed: forceStartMixed } = getToggleSelectionState(selectedTorrents.map(t => t.force_start), stateUnknownForSelection)

              if (forceStartMixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => handleBulkAction(TORRENT_ACTIONS.FORCE_START, { enable: true })}
                      className="justify-start"
                    >
                      <FastForward className="mr-2 h-4 w-4" />
                      {t("contextMenu.forceStart")} {t("contextMenu.mixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => handleBulkAction(TORRENT_ACTIONS.FORCE_START, { enable: false })}
                      className="justify-start"
                    >
                      <FastForward className="mr-2 h-4 w-4" />
                      {t("contextMenu.disableForceStart")} {t("contextMenu.mixed")}
                    </Button>
                  </>
                )
              }

              return (
                <Button
                  variant="outline"
                  onClick={() => handleBulkAction(TORRENT_ACTIONS.FORCE_START, { enable: !allForceStarted })}
                  className="justify-start"
                >
                  <FastForward className="mr-2 h-4 w-4" />
                  {allForceStarted ? t("contextMenu.disableForceStart") : t("contextMenu.forceStart")}
                </Button>
              )
            })()}
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RECHECK)}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4" />
              {t("managementBar.forceRecheck")}
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.REANNOUNCE)}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4" />
              {t("managementBar.reannounce")}
            </Button>
            {(() => {
              const { allEnabled: allSeqDlEnabled, mixed: seqDlMixed } = getToggleSelectionState(selectedTorrents.map(t => t.seq_dl), stateUnknownForSelection)

              if (seqDlMixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => handleBulkAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, { enable: true })}
                      className="justify-start"
                    >
                      <Blocks className="mr-2 h-4 w-4" />
                      {t("managementBar.sequentialDownload.enable")} {t("contextMenu.mixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => handleBulkAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, { enable: false })}
                      className="justify-start"
                    >
                      <Blocks className="mr-2 h-4 w-4" />
                      {t("managementBar.sequentialDownload.disable")} {t("contextMenu.mixed")}
                    </Button>
                  </>
                )
              }

              return (
                <Button
                  variant="outline"
                  onClick={() => handleBulkAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, { enable: !allSeqDlEnabled })}
                  className="justify-start"
                >
                  <Blocks className="mr-2 h-4 w-4" />
                  {allSeqDlEnabled ? t("managementBar.sequentialDownload.disable") : t("managementBar.sequentialDownload.enable")}
                </Button>
              )
            })()}
            {onFilterChange && !isAllInstancesView && (
              <Button
                variant="outline"
                onClick={() => {
                  filterCrossSeeds(selectedTorrents)
                  setShowActionsSheet(false)
                }}
                disabled={isFilteringCrossSeeds || selectedTorrents.length !== 1}
                className="justify-start"
              >
                <GitBranch className="mr-2 h-4 w-4" />
                {t("contextMenu.filterCrossSeeds")}
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
                disabled={effectiveSelectionCount !== 1 || !singleSelectedTorrent || isCrossSeedSearching}
                className="justify-start"
              >
                <Search className="mr-2 h-4 w-4" />
                {t("contextMenu.searchCrossSeeds")}
              </Button>
            )}
            {onManualCrossSeed && (
              <Button
                variant="outline"
                onClick={() => {
                  if (!singleSelectedTorrent) {
                    return
                  }
                  onManualCrossSeed(singleSelectedTorrent)
                  setShowActionsSheet(false)
                }}
                disabled={effectiveSelectionCount !== 1 || !singleSelectedTorrent || singleSelectedTorrent.progress < 1}
                className="justify-start"
              >
                <FileUp className="mr-2 h-4 w-4" />
                {t("contextMenu.manualCrossSeed")}
              </Button>
            )}
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.INCREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              {t("managementBar.increasePriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.DECREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              {t("managementBar.decreasePriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.TOP_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              {t("managementBar.topPriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.BOTTOM_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              {t("managementBar.bottomPriority")}
            </Button>
            {(() => {
              const { allEnabled, mixed } = getToggleSelectionState(selectedTorrents.map(t => t.auto_tmm), stateUnknownForSelection)

              if (mixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => {
                        prepareTmmAction(true)
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4" />
                      {t("mobileCards.enableTmmMixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => {
                        prepareTmmAction(false)
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4" />
                      {t("mobileCards.disableTmmMixed")}
                    </Button>
                  </>
                )
              }

              return (
                <Button
                  variant="outline"
                  onClick={() => {
                    prepareTmmAction(!allEnabled)
                    setShowActionsSheet(false)
                  }}
                  className="justify-start"
                >
                  {allEnabled ? (
                    <>
                      <Settings2 className="mr-2 h-4 w-4" />
                      {t("managementBar.tmm.disable")}
                    </>
                  ) : (
                    <>
                      <Settings2 className="mr-2 h-4 w-4" />
                      {t("managementBar.tmm.enable")}
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
              {t("contextMenu.setShareLimits")}
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
              {t("contextMenu.setSpeedLimits")}
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                prepareLocationAction()
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <FolderOpen className="mr-2 h-4 w-4" />
              {t("managementBar.setLocation")}
            </Button>
            {(capabilities?.supportsTorrentExport ?? true) && (
              <Button
                variant="outline"
                onClick={handleExport}
                disabled={isExporting}
                className="justify-start"
              >
                <Download className="mr-2 h-4 w-4" />
                {effectiveSelectionCount > 1 ? t("contextMenu.exportTorrents", { count: effectiveSelectionCount }) : t("contextMenu.exportTorrent")}
              </Button>
            )}
            <Button
              variant="destructive"
              onClick={() => {
                prepareDeleteAction()
                setShowActionsSheet(false)
              }}
              className="justify-start !bg-destructive !text-destructive-foreground"
            >
              <Trash2 className="mr-2 h-4 w-4" />
              {t("managementBar.delete")}
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
          }
        }}
        count={effectiveSelectionCount}
        totalSize={selectedTotalSize}
        formattedSize={selectedFormattedSize}
        deleteFiles={deleteFiles}
        onDeleteFilesChange={setDeleteFiles}
        isDeleteFilesLocked={isDeleteFilesLocked}
        onToggleDeleteFilesLock={toggleDeleteFilesLock}
        deleteCrossSeeds={deleteCrossSeeds}
        onDeleteCrossSeedsChange={setDeleteCrossSeeds}
        showBlockCrossSeeds={hasCrossSeedTag}
        blockCrossSeeds={blockCrossSeeds}
        onBlockCrossSeedsChange={setBlockCrossSeeds}
        crossSeedWarning={crossSeedWarning}
        onConfirm={handleDeleteWrapper}
        isAllSelected={isAllSelected}
        previewTorrents={selectedTorrents}
      />

      {/* Tags dialog */}
      <TagEditorDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        selectedTorrents={selectedTorrents}
        hashCount={effectiveSelectionCount}
        selectionRequest={{ instanceId, ...actionTargetRequest(actionTarget) }}
        onConfirm={handleTagsWrapper}
        isPending={isPending}
      />

      {/* Category dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={effectiveSelectionCount}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(selectedTorrents)}
        useSubcategories={allowSubcategories}
      />

      {/* Share Limits Dialog */}
      <ShareLimitDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={effectiveSelectionCount}
        torrents={selectedTorrents}
        supportsShareLimitsAction={capabilities?.supportsShareLimitsAction}
        supportsShareLimitsMode={capabilities?.supportsShareLimitsMode}
        onConfirm={async (ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode) => {
          await handleSetShareLimit({ ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode }, actionTarget)
          setShowShareLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Speed Limits Dialog */}
      <SpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={effectiveSelectionCount}
        torrents={selectedTorrents}
        onConfirm={async (uploadLimit, downloadLimit) => {
          await handleSetSpeedLimits({ uploadLimit, downloadLimit }, actionTarget)
          setShowSpeedLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={effectiveSelectionCount}
        initialLocation={getCommonSavePath(selectedTorrents)}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
        instanceId={instanceId}
        capabilities={capabilities}
      />

      {/* TMM Confirmation Dialog */}
      <TmmConfirmDialog
        open={showTmmDialog}
        onOpenChange={setShowTmmDialog}
        count={effectiveSelectionCount}
        enable={pendingTmmEnable}
        onConfirm={() => handleTmmConfirm(actionTarget)}
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
            <DialogTitle>{t("mobileCards.searchModal.title")}</DialogTitle>
            <DialogDescription>
              {t("mobileCards.searchModal.description")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
              <Input
                placeholder={t("mobileCards.searchModal.placeholder")}
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
                  aria-label={t("common:header.clearSearch")}
                >
                  <X className="h-3.5 w-3.5 text-muted-foreground" />
                </button>
              )}
            </div>

            <div className="space-y-2 text-xs text-muted-foreground bg-muted/50 rounded-lg p-3">
              <div className="flex items-start gap-2">
                <Info className="h-4 w-4 flex-shrink-0 mt-0.5" />
                <div className="space-y-1">
                  <p className="font-semibold">{t("common:header.smartSearchTitle")}</p>
                  <ul className="space-y-1 ml-2">
                    <li>• <strong>{t("common:header.smartSearchGlob")}</strong> {t("mobileCards.searchModal.globExamples")}</li>
                    <li>• <strong>{t("common:header.smartSearchFuzzy")}</strong> {t("common:header.smartSearchFuzzyExample")}</li>
                    <li>• {t("common:header.smartSearchFields")}</li>
                    <li>• {t("mobileCards.searchModal.autoSearches")}</li>
                  </ul>
                </div>
              </div>
            </div>
          </div>
          <DialogFooter className="sm:justify-between">
            <Button variant="outline" onClick={handleClearSearchAndClose}>
              {t("mobileCards.clear")}
            </Button>
            <Button onClick={() => setShowSearchModal(false)}>
              {t("mobileCards.done")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Add torrent dialog */}
      {instanceId > 0 && (
        <AddTorrentDialog
          instanceId={instanceId}
          open={addTorrentModalOpen}
          onOpenChange={onAddTorrentModalChange}
        />
      )}

      {/* Fixed bottom navbar - only visible when not in selection mode */}
      {!selectionMode && (
        <div
          className={cn(
            "fixed left-0 right-0 z-50 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50",
            "transition-transform duration-300",
            !isFooterVisible && "translate-y-[calc(100%+4rem+env(safe-area-inset-bottom))]"
          )}
          style={{ bottom: "calc(4rem + env(safe-area-inset-bottom))" }}
        >
          <div className="flex items-center justify-around h-14 px-2">
            <button
              onClick={() => setShowSearchModal(true)}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              <Search className="h-5 w-5" />
              <span className="truncate text-[10px]">{t("common:actions.search")}</span>
            </button>

            <button
              onClick={() => setIncognitoMode(!incognitoMode)}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              {incognitoMode ? <EyeOff className="h-5 w-5" /> : <Eye className="h-5 w-5" />}
              <span className="truncate text-[10px]">{t("mobileCards.incognito")}</span>
            </button>

            <button
              onClick={() => window.dispatchEvent(new Event("qui-open-mobile-filters"))}
              className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
            >
              <Filter className="h-5 w-5" />
              <span className="truncate text-[10px]">{t("filterSidebar.title")}</span>
            </button>

            {!isAllInstancesView && (
              <button
                onClick={() => onAddTorrentModalChange?.(true)}
                className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
              >
                <Plus className="h-5 w-5" />
                <span className="truncate text-[10px]">{t("mobileCards.add")}</span>
              </button>
            )}

            {supportsTorrentCreation && (
              <button
                onClick={() => {
                  const next = { ...(routeSearch || {}), modal: "create-torrent" }
                  navigateWithSearch({ navigate, search: next, replace: true })
                }}
                className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95"
              >
                <FileEdit className="h-5 w-5" />
                <span className="truncate text-[10px]">{t("common:actions.create")}</span>
              </button>
            )}

            {supportsTorrentCreation && (
              <button
                onClick={() => {
                  const next = { ...(routeSearch || {}), modal: "tasks" }
                  navigateWithSearch({ navigate, search: next, replace: true })
                }}
                className="flex flex-col items-center justify-center gap-0.5 px-2 py-1.5 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground active:scale-95 relative"
              >
                <ListTodo className="h-5 w-5" />
                {activeTaskCount > 0 && (
                  <Badge variant="default" className="absolute top-0 right-1 h-4 min-w-4 flex items-center justify-center p-0 text-[9px]">
                    {activeTaskCount}
                  </Badge>
                )}
                <span className="truncate text-[10px]">{t("mobileCards.tasks")}</span>
              </button>
            )}
          </div>
        </div>
      )}

      {/* Scroll to top button - only on mobile */}
      <div className="sm:hidden">
        <ScrollToTopButton
          scrollContainerRef={parentRef}
          className={cn(
            "right-8 z-[60]",
            selectionBarVisible? "bottom-[calc(5rem+env(safe-area-inset-bottom))]": isFooterVisible? "bottom-[calc(8.5rem+env(safe-area-inset-bottom))]": "bottom-[calc(1.5rem+env(safe-area-inset-bottom))]"
          )}
        />
      </div>
    </div>
  )
}
