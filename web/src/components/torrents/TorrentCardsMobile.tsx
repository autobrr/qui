/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { DisplayProfileMenuItem, DisplayProfileSheet } from "@/components/display-profiles/DisplayProfileSheet"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Progress } from "@/components/ui/progress"
import { ScrollToTopButton } from "@/components/ui/scroll-to-top-button"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useMobileScroll, useRegisterMobileScrollContainer } from "@/contexts/MobileScrollContext"
import { useActiveTaskCount } from "@/hooks/useActiveTaskCount"
import { useSwipeActions } from "@/hooks/useSwipeActions"
import { useCopyGroupRows, type CopyGroupRow } from "@/hooks/useCopyGroupRows"
import { useTorrentListController } from "@/hooks/useTorrentListController"
import { useTrackerCustomizations } from "@/hooks/useTrackerCustomizations"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { useClientSetting } from "@/lib/client-settings"
import { EMPTY_SIDEBAR_FILTERS, hasSidebarFilters } from "@/lib/filter-views"
import { buildTrackerCustomizationLookup, extractTrackerHost, getTrackerCustomizationsCacheKey, resolveTrackerDisplay, type TrackerCustomizationLookup } from "@/lib/tracker-customizations"
import { resolveTrackerHealthSupport } from "@/lib/tracker-health-support"
import { resolveTrackerIconSrc } from "@/lib/tracker-icons"
import { actionTargetInstanceIds, buildActionTarget, EMPTY_ACTION_TARGET, getSelectionIdentity as selectionIdentity, hasSelection, rememberTickedTorrents, selectAllButtonEvent, selectionReducer, type ActionTargetScope } from "@/lib/torrent-selection"
import { getToggleSelectionState, getTotalSize } from "@/lib/torrent-utils"
import { isAllInstancesScope } from "@/lib/instances"
import { useNavigate, useSearch } from "@tanstack/react-router"
import { navigateWithSearch } from "@/lib/router-search"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowRightLeft,
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
  ListTodo,
  Loader2,
  MoreVertical,
  Pause,
  Play,
  Plus,
  Radio,
  Search,
  Settings2,
  Share2,
  Sprout,
  Tag,
  Trash2,
  X
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent } from "react"
import { useTranslation } from "react-i18next"
import { AddTorrentDialog } from "./AddTorrentDialog"
import type { BulkActionPick } from "./bulk-actions/bulk-action-state"
import { useBulkActions } from "./bulk-actions/bulk-actions-context"
import { TorrentListEmptyState } from "./TorrentListEmptyState"
import { useTorrentSelectionContext, type ListFacts } from "@/contexts/TorrentSelectionContext"
import { useCrossSeedFilter } from "@/hooks/useCrossSeedFilter"
import { useInstanceCapabilities, useInstancesSupport } from "@/hooks/useInstanceCapabilities"
import { usePersistedGroupCopies } from "@/hooks/usePersistedGroupCopies"
import { usePersistedCompactViewState, type ViewMode } from "@/hooks/usePersistedCompactViewState"
import { useSubcategoriesEnabled } from "@/hooks/usePreferenceValues"
import { withSortedField } from "@/lib/copy-groups"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { unitLabel } from "@/lib/unit-format"
import { getCopyGroupAccentClass, getStatusBadgeProps } from "@/lib/torrent-table/row-display"
import { themeArea } from "@/lib/theme-areas"
import { cn, formatBytes, getRatioColor } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts, TorrentFilters } from "@/types"
import { CopyGroupMenuItems } from "./table/CopyGroupMenuItems"
import { CopyGroupPill } from "./table/CopyGroupPill"
import { getDefaultSortOrder, TORRENT_SORT_OPTIONS, type TorrentSortOptionValue } from "./torrentSortOptions"
import { transferShareOffer } from "./transfer-share-offer"

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
    <div className={cn("flex items-center justify-center", className)} role={title ? "img" : undefined} aria-label={title || undefined}>
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

function SwipeableCard({
  torrent: cardTorrent,
  copyGroup,
  sortedField,
  onToggleCopyGroup,
  isSelected,
  onSelect,
  onClick,
  onLongPress,
  swipeOpen,
  onSwipeOpenChange,
  onSwipeAction,
  incognitoMode,
  selectionMode,
  speedUnit,
  viewMode,
  supportsTrackerHealth,
  trackerIcons,
  trackerCustomizationLookup,
}: {
  torrent: Torrent
  copyGroup: CopyGroupRow<Torrent>
  sortedField: string
  onToggleCopyGroup: (id: string) => void
  isSelected: boolean
  onSelect: (selected: boolean) => void
  onClick: () => void
  onLongPress: (torrent: Torrent) => void
  swipeOpen: boolean
  onSwipeOpenChange: (open: boolean) => void
  onSwipeAction: (pick: BulkActionPick) => void
  incognitoMode: boolean
  selectionMode: boolean
  speedUnit: SpeedUnit
  viewMode: ViewMode
  supportsTrackerHealth: boolean
  trackerIcons?: Record<string, string>
  trackerCustomizationLookup?: TrackerCustomizationLookup
}) {
  const { t } = useTranslation("torrents")
  const torrent = useMemo(
    () => withSortedField(cardTorrent, copyGroup.sortedTorrent, sortedField),
    [cardTorrent, copyGroup.sortedTorrent, sortedField]
  )

  const [longPressTimer, setLongPressTimer] = useState<number | null>(null)
  const [touchStart, setTouchStart] = useState<{ x: number; y: number } | null>(null)
  const [hasMoved, setHasMoved] = useState(false)
  const { trackRef, actionsRef, actionsMounted, trackProps } = useSwipeActions({ open: swipeOpen, onOpenChange: onSwipeOpenChange, disabled: selectionMode })

  const handleTouchStart = (e: React.TouchEvent) => {
    if (selectionMode) return

    const touch = e.touches[0]
    setTouchStart({ x: touch.clientX, y: touch.clientY })
    setHasMoved(false)

    const timer = window.setTimeout(() => {
      if (!hasMoved) {
        if ("vibrate" in navigator) {
          navigator.vibrate(50)
        }
        onLongPress(cardTorrent)
      }
    }, 600)
    setLongPressTimer(timer)
  }

  const handleTouchMove = (e: React.TouchEvent) => {
    if (!touchStart || hasMoved) return

    const touch = e.touches[0]
    const deltaX = Math.abs(touch.clientX - touchStart.x)
    const deltaY = Math.abs(touch.clientY - touchStart.y)

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
  const trackerDisplayInfo = useMemo(() => {
    if (!trackerCustomizationLookup || trackerCustomizationLookup.size === 0) {
      return { displayName: trackerMeta.host, primaryDomain: trackerMeta.host, isCustomized: false }
    }
    return resolveTrackerDisplay(trackerMeta.host, trackerCustomizationLookup)
  }, [trackerMeta.host, trackerCustomizationLookup])
  const iconDomain = trackerDisplayInfo.primaryDomain || trackerMeta.host
  const trackerIconSrc = resolveTrackerIconSrc(trackerIcons, iconDomain, trackerMeta.host)
  const trackerDisplayName = trackerDisplayInfo.displayName || trackerMeta.title
  const groupPill = copyGroup.groupId !== undefined && (
    <CopyGroupPill
      id={copyGroup.groupId}
      open={copyGroup.open}
      copies={copyGroup.copies}
      hiddenProblem={copyGroup.hiddenProblem}
      hiddenTicks={copyGroup.hiddenTicks}
      name={displayName}
      size={viewMode === "normal" ? "normal" : "dense"}
      onToggle={onToggleCopyGroup}
    />
  )
  const helperBadge = torrent.qui_helper && (
    <Tooltip>
      <TooltipTrigger asChild openOnTap="only">
        <Badge variant="outline" className="text-[10px] px-1 py-0 h-4 flex-shrink-0 ml-1">
          {torrent.qui_helper === "share" ? t("tableColumns.helperShare") : t("tableColumns.helperTransfer")}
        </Badge>
      </TooltipTrigger>
      <TooltipContent>{t("tableColumns.helperHint")}</TooltipContent>
    </Tooltip>
  )
  const isComplete = torrent.progress === 1
  const hasSpeeds = torrent.dlspeed > 0 || torrent.upspeed > 0
  const trackerTitle = trackerDisplayInfo.isCustomized ? `${trackerDisplayName} (${trackerMeta.host})` : trackerDisplayName

  const card = (
    <div
      {...themeArea("torrent-card")}
      className={cn(
        "bg-card rounded-lg border cursor-pointer transition-all relative overflow-hidden select-none",
        viewMode === "ultra-compact" ? "px-3 py-1" : viewMode === "compact" ? "p-2" : "p-4",
        (copyGroup.open || copyGroup.nestedCopy) && getCopyGroupAccentClass(copyGroup.open),
        isSelected && "bg-accent/50",
        !selectionMode && "[&:not(:has(button:active))]:active:scale-[0.98]",
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
      {isSelected && (
        <div className="absolute inset-0 rounded-lg ring-2 ring-primary ring-inset pointer-events-none" />
      )}
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
        <div className="flex items-center gap-2">
          <div className="flex-1 min-w-0 overflow-hidden">
            <div className="w-full overflow-x-auto scrollbar-thin touch-pan-y">
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
                )}>
                  {displayName}
                </h3>
              </div>
            </div>
          </div>

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

          {groupPill}

          {helperBadge}

          <Badge variant={statusBadgeVariant} className={cn("text-[10px] px-1 py-0 h-4 flex-shrink-0", statusBadgeClass)}>
            {statusBadgeLabel}
          </Badge>

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
        <>
          <div className="flex h-5 items-center mb-1.5">
            <div className="flex-1 min-w-0 overflow-hidden">
              <div className="w-full overflow-x-auto scrollbar-thin touch-pan-y">
                <h3 className={cn(
                  "font-medium text-sm inline-block whitespace-nowrap",
                  selectionMode && "pr-8"
                )}>
                  {displayName}
                </h3>
              </div>
            </div>
            {groupPill}
            {helperBadge}
          </div>

          <div className="flex h-[22px] items-center gap-2 text-xs">
            <div className="flex items-center gap-2 text-muted-foreground min-w-0">
              {trackerDisplayName && (
                <span className="flex items-center gap-1 min-w-0 shrink-[9999]">
                  <TrackerIcon
                    title={trackerTitle}
                    fallback={trackerMeta.fallback}
                    src={trackerIconSrc}
                    size="xs"
                    className="flex-shrink-0"
                  />
                  <span className="truncate">{trackerDisplayName}</span>
                </span>
              )}
              <span className="min-w-0 truncate">
                {isComplete ? formatBytes(torrent.size) : `${formatBytes(torrent.downloaded)} / ${formatBytes(torrent.size)}`}
              </span>
              <span className="flex items-center gap-1 flex-shrink-0">
                <span>{t("mobileCards.ratio")}</span>
                <span
                  className="font-medium"
                  style={{ color: getRatioColor(displayRatio) }}
                >
                  {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
                </span>
              </span>
            </div>
            <Badge variant={statusBadgeVariant} className={cn("text-xs flex-shrink-0 ml-auto", statusBadgeClass)}>
              {statusBadgeLabel}
            </Badge>
          </div>
        </>
      ) : (
        <>
          <div className={cn("mb-3 flex items-start", selectionMode && "pr-8")}>
            <h3 className="flex-1 min-w-0 font-medium text-sm line-clamp-2 break-all h-10">
              {displayName}
            </h3>
            {groupPill}
            {helperBadge}
          </div>

          <div className="mb-3">
            <div className="flex items-center justify-between mb-1">
              <span className="text-xs text-muted-foreground">
                {formatBytes(torrent.downloaded)} / {formatBytes(torrent.size)}
              </span>
              <div className="flex items-center gap-2">
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

          <div className="flex items-center justify-between text-xs mb-2">
            <div className="flex items-center gap-3">
              <div className="flex items-center gap-1">
                <span className="text-muted-foreground">{t("mobileCards.ratio")}</span>
                <span
                  className="font-medium"
                  style={{ color: getRatioColor(displayRatio) }}
                >
                  {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
                </span>
              </div>

              {torrent.dlspeed > 0 && (
                <div className="flex items-center gap-1">
                  <ChevronDown className="h-3 w-3 [color:var(--chart-2)]" />
                  <span className="font-medium">{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}</span>
                </div>
              )}

              {torrent.upspeed > 0 && (
                <div className="flex items-center gap-1">
                  <ChevronUp className="h-3 w-3 [color:var(--chart-3)]" />
                  <span className="font-medium">{formatSpeedWithUnit(torrent.upspeed, speedUnit)}</span>
                </div>
              )}
            </div>

            <Badge variant={statusBadgeVariant} className={cn("text-xs", statusBadgeClass)}>
              {statusBadgeLabel}
            </Badge>
          </div>
        </>
      )}

      {viewMode === "compact" ? (
        <div className="flex h-4 items-center justify-between gap-2 text-xs mt-1">
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

          {isComplete && !hasSpeeds ? (
            <span className="text-muted-foreground flex-shrink-0">
              {t("mobileCards.added", { time: formatRelativeTime(torrent.added_on) })}
            </span>
          ) : (
            <div className="flex items-center gap-2 flex-shrink-0">
              {!isComplete && (
                <span className="text-muted-foreground">
                  {torrent.progress >= 0.99 && torrent.progress < 1 ? (
                    (Math.floor(torrent.progress * 1000) / 10).toFixed(1)
                  ) : (
                    Math.round(torrent.progress * 100)
                  )}%
                </span>
              )}
              {hasSpeeds && (
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
          )}
        </div>
      ) : viewMode === "normal" ? (
        <div className="flex items-center justify-between gap-2 min-h-[20px]">
          <div className="flex items-center gap-3 flex-shrink-0">
            {trackerDisplayName && (
              <div className="flex items-center gap-1">
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
      ) : null }
    </div>
  )

  return (
    <div ref={trackRef} className="relative touch-pan-y" data-swipe-open={swipeOpen || undefined} {...trackProps}>
      {card}
      {actionsMounted && !selectionMode && (
        <div ref={actionsRef} inert={!swipeOpen} className="absolute inset-y-0 left-full flex gap-1.5 pl-1.5">
          <SwipeActions
            stopped={/^(paused|stopped)(DL|UP)$/.test(cardTorrent.state)}
            dense={viewMode === "ultra-compact"}
            onAction={onSwipeAction}
          />
        </div>
      )}
    </div>
  )
}

function SwipeActions({ stopped, dense, onAction }: {
  stopped: boolean
  dense: boolean
  onAction: (pick: BulkActionPick) => void
}) {
  const { t } = useTranslation("torrents")
  const toggleLabel = stopped ? t("managementBar.resume") : t("managementBar.pause")
  const ToggleIcon = stopped ? Play : Pause
  const actionClass = cn(
    "flex flex-col items-center justify-center gap-1 rounded-lg text-[11px] font-medium active:opacity-80",
    dense ? "w-12" : "w-16"
  )
  const iconClass = dense ? "h-4 w-4" : "h-5 w-5"

  return (
    <>
      <button
        type="button"
        aria-label={toggleLabel}
        onClick={() => onAction({ action: stopped ? "resume" : "pause" })}
        className={cn(actionClass, "bg-secondary text-secondary-foreground")}
      >
        <ToggleIcon className={iconClass} />
        {!dense && <span className="max-w-full truncate px-1">{toggleLabel}</span>}
      </button>
      <button
        type="button"
        aria-label={t("managementBar.delete")}
        onClick={() => onAction({ action: "delete" })}
        className={cn(actionClass, "bg-destructive text-destructive-foreground")}
      >
        <Trash2 className={iconClass} />
        {!dense && <span className="max-w-full truncate px-1">{t("managementBar.delete")}</span>}
      </button>
    </>
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
  const [sortState, setSortState] = useClientSetting<MobileSortState>(
    `${MOBILE_SORT_STORAGE_KEY}:${instanceId}`,
    { defaultValue: DEFAULT_MOBILE_SORT_STATE, parse: parseMobileSortState }
  )
  const { selection, dispatch: dispatchSelection, publishSummary } = useTorrentSelectionContext()
  const { isAllSelected, selected: selectedHashes, excluded: excludedFromSelectAll } = selection
  const selectionMode = hasSelection(selection)

  const parentRef = useRef<HTMLDivElement>(null)
  const { isFooterVisible } = useMobileScroll()
  useRegisterMobileScrollContainer(parentRef)

  const [showActionsSheet, setShowActionsSheet] = useState(false)
  const [showProfileSheet, setShowProfileSheet] = useState(false)
  const [swipedKey, setSwipedKey] = useState<string | null>(null)
  const setCardSwiped = useCallback((key: string, open: boolean) => {
    setSwipedKey(current => open ? key : current === key ? null : current)
  }, [])
  const swallowListClick = useRef(false)
  const sortButtonRef = useRef<HTMLButtonElement>(null)
  const sortField = sortState.field
  const sortOrder = sortState.order

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

  const trackerCustomizationsQuery = useTrackerCustomizations()
  const trackerCustomizationsRef = useRef<{ key: string; lookup: TrackerCustomizationLookup } | undefined>(undefined)
  const trackerCustomizationLookup = useMemo(() => {
    const latest = trackerCustomizationsQuery.data
    if (!latest) {
      return trackerCustomizationsRef.current?.lookup ?? new Map()
    }

    const newKey = getTrackerCustomizationsCacheKey(latest)

    const previous = trackerCustomizationsRef.current
    if (previous && previous.key === newKey) {
      return previous.lookup
    }

    const newLookup = buildTrackerCustomizationLookup(latest)
    trackerCustomizationsRef.current = { key: newKey, lookup: newLookup }
    return newLookup
  }, [trackerCustomizationsQuery.data])

  const { pick, isExporting } = useBulkActions()

  const routeSearch = useSearch({ strict: false }) as { q?: string; modal?: string }
  const effectiveSearch = routeSearch?.q || ""
  const navigate = useNavigate()
  const activeTaskCount = useActiveTaskCount(instanceId, !isAllInstancesView)

  const [groupCopiesSetting, toggleGroupCopies] = usePersistedGroupCopies()

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
    visibleRows,
    groupExpansion,
    toggleGroup,
    copyGroupsReady,
  } = useTorrentListController({
    instanceId,
    instanceIds,
    search: effectiveSearch,
    filters,
    sort: backendSortField,
    order: sortOrder,
    groupCopies: groupCopiesSetting,
  })

  const { data: capabilities } = useInstanceCapabilities(instanceId, { enabled: instanceId > 0 })
  const supportsTrackerHealth = resolveTrackerHealthSupport({
    isUnifiedView: isAllInstancesView,
    capabilitySupport: capabilities?.supportsTrackerHealth,
    responseSupport: trackerHealthSupported,
  })
  const supportsTorrentCreation = isAllInstancesView ? false : (capabilities?.supportsTorrentCreation ?? true)
  const instanceSubcategoriesEnabled = useSubcategoriesEnabled(instanceId, capabilities, subcategoriesFromData)
  const allowSubcategories = isAllInstancesView? Boolean(subcategoriesFromData): instanceSubcategoriesEnabled

  const getSelectionIdentity = useCallback(
    (torrent: Torrent): string => selectionIdentity(torrent, isCrossInstanceEndpoint, instanceId),
    [isCrossInstanceEndpoint, instanceId]
  )

  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags, allowSubcategories, supportsTrackerHealth)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, allowSubcategories, onFilteredDataUpdate, supportsTrackerHealth])

  const { describeRow: describeCopyGroupRow } = useCopyGroupRows({ rows: torrents, expansion: groupExpansion, selection, getSelectionIdentity })

  const actionSelection = useMemo(
    () => selectionMode || swipedKey === null ? selection : selectionReducer(selection, { type: "selectOnly", key: swipedKey }),
    [selectionMode, swipedKey, selection]
  )
  const tickedTorrentsRef = useRef(new Map<string, Torrent>())
  const actionTarget = useMemo(() => {
    if (!hasSelection(actionSelection)) {
      tickedTorrentsRef.current.clear()
      return EMPTY_ACTION_TARGET
    }
    const ticked = rememberTickedTorrents(tickedTorrentsRef.current, actionSelection.selected, torrents, getSelectionIdentity)
    const scope: ActionTargetScope = { instanceId, instanceIds, filters, columnFilters: null, search: effectiveSearch, isCrossInstance: isCrossInstanceEndpoint }
    return buildActionTarget(actionSelection, scope, { loaded: torrents, ticked, totalCount })
  }, [actionSelection, torrents, getSelectionIdentity, instanceId, instanceIds, filters, effectiveSearch, isCrossInstanceEndpoint, totalCount])
  const { count: effectiveSelectionCount, previewTorrents: selectedTorrents } = actionTarget
  const actionInstanceIds = useMemo(() => actionTargetInstanceIds(actionTarget, instanceId), [actionTarget, instanceId])
  const supportsTorrentExport = useInstancesSupport(actionInstanceIds, "supportsTorrentExport")
  const offer = transferShareOffer(selectedTorrents, effectiveSelectionCount, actionInstanceIds)

  const selectionBarVisible = selectionMode && effectiveSelectionCount > 0

  const totalSize = isAllSelected ? stats?.totalSize ?? 0 : 0
  const excludedSize = useMemo(() => {
    if (!isAllSelected || excludedFromSelectAll.size === 0) {
      return 0
    }
    return torrents.reduce((total, torrent) => excludedFromSelectAll.has(getSelectionIdentity(torrent)) ? total + (torrent.size || 0) : total, 0)
  }, [isAllSelected, excludedFromSelectAll, torrents, getSelectionIdentity])
  const selectedTotalSize = isAllSelected ? Math.max(totalSize - excludedSize, 0) : getTotalSize(selectedTorrents)
  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  const listFacts = useMemo<ListFacts>(() => ({
    isCrossInstance: Boolean(isCrossInstanceEndpoint),
    categories,
    tags,
    allowSubcategories,
    capabilities,
    sortField: backendSortField,
    sortOrder,
  }), [isCrossInstanceEndpoint, categories, tags, allowSubcategories, capabilities, backendSortField, sortOrder])

  useEffect(() => {
    publishSummary({ actionTarget, totalSize, excludedSize, instanceId, instanceIds, listFacts })
  }, [publishSummary, actionTarget, totalSize, excludedSize, instanceId, instanceIds, listFacts])
  useEffect(() => () => publishSummary(null), [publishSummary])

  const virtualizer = useVirtualizer({
    count: loadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => viewMode === "ultra-compact" ? 32 : viewMode === "compact" ? 90 : 180,
    overscan: 5,
    getItemKey: useCallback((index: number) => {
      const torrent = visibleRows[index]
      return torrent ? getSelectionIdentity(torrent) : `loading-${index}`
    }, [visibleRows, getSelectionIdentity]),
    onChange: (instance, sync) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      const shouldCheckLoadMore = !sync || !instance.isScrolling
      const scrolledPastRows = (instance.scrollOffset ?? 0) > instance.getTotalSize()

      if (shouldCheckLoadMore && !scrolledPastRows && lastItem && lastItem.index >= loadedRows - 20 && hasMoreRows) {
        reportNearEnd();
      }
    },
  })

  useEffect(() => {
    virtualizer.measure()
  }, [loadedRows, virtualizer])

  const virtualItems = virtualizer.getVirtualItems()

  useEffect(() => {
    if (parentRef.current) {
      parentRef.current.scrollTop = 0
    }
    virtualizer.scrollToOffset(0)
  }, [listKey, virtualizer])

  useEffect(() => {
    if (virtualizer) {
      setTimeout(() => {
        virtualizer.measure()
        setTimeout(() => virtualizer.measure(), 50)
        setTimeout(() => virtualizer.measure(), 150)
      }, 0)
    }
  }, [viewMode, virtualizer])

  useEffect(() => {
    if (virtualizer && torrents.length > 0) {
      setTimeout(() => {
        virtualizer.measure()
      }, 0)
    }
  }, [torrents, virtualizer])



  const handleLongPress = useCallback((torrent: Torrent) => {
    setSwipedKey(null)
    dispatchSelection({ type: "selectOnly", key: getSelectionIdentity(torrent) })
  }, [dispatchSelection, getSelectionIdentity])

  const pickSwipeAction = useCallback((bulkPick: BulkActionPick) => {
    pick(bulkPick)
    if (bulkPick.action !== "delete") {
      setSwipedKey(null)
    }
  }, [pick])

  const closeSwipedCard = (e: PointerEvent<HTMLElement>) => {
    swallowListClick.current = false
    if (swipedKey === null || (e.target as Element).closest("[data-swipe-open]")) return
    swallowListClick.current = parentRef.current?.contains(e.target as Node) ?? false
    setSwipedKey(null)
  }

  const handleSelect = useCallback((key: string, selected: boolean) => {
    dispatchSelection({ type: "tick", keys: [key], checked: selected })
  }, [dispatchSelection])

  const handleSelectAll = useCallback(() => {
    dispatchSelection(selectAllButtonEvent(selection, totalCount))
  }, [dispatchSelection, selection, totalCount])

  const pickBulkAction = useCallback((bulkPick: BulkActionPick) => {
    pick(bulkPick)
    setShowActionsSheet(false)
  }, [pick])

  const { isFilteringCrossSeeds, filterCrossSeeds } = useCrossSeedFilter({
    instanceId,
    onFilterChange,
  })

  const singleSelectedTorrent = selectedTorrents[0] ?? null

  const stateUnknownForSelection = isAllSelected && selectedTorrents.length < effectiveSelectionCount

  const handleClearSearch = useCallback(() => {
    if (routeSearch && Object.prototype.hasOwnProperty.call(routeSearch, "q")) {
      const next = { ...(routeSearch || {}) }
      delete next.q
      navigateWithSearch({ navigate, search: next, replace: true })
    }
  }, [navigate, routeSearch])

  return (
    <div className="relative flex h-full min-h-0 flex-col overflow-hidden" onPointerDownCapture={closeSwipedCard}>
      <div className="sticky top-0 z-40 bg-background">
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
              ) : copyGroupsReady === false ? (
                <span className="flex items-center gap-1">
                  <Loader2 className="h-3 w-3 animate-spin" />
                  {t("copyGroups.grouping")}
                </span>
              ) : (
                <>
                  {hasLoadedAll ? (
                    t("statusBar.torrentCount", { count: torrents.length })
                  ) : isLoadingMore ? (
                    t("statusBar.loadingMore")
                  ) : (
                    t("statusBar.torrentsLoaded", { loaded: torrents.length, total: totalCount })
                  )}
                </>
              )}
            </div>
            <div className="flex items-center gap-1">
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    ref={sortButtonRef}
                    variant="ghost"
                    size="sm"
                    className="h-7 px-2 text-xs font-medium text-muted-foreground hover:text-foreground md:hidden"
                  >
                    {t("mobileCards.sort")}: {t(currentSortOption.labelKey)}
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-56 max-h-100 overflow-y-auto">
                  <DisplayProfileMenuItem onSelect={() => setShowProfileSheet(true)} />
                  <CopyGroupMenuItems groupCopies={groupCopiesSetting} onToggleGroupCopies={toggleGroupCopies} />
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
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={toggleSortOrder}
                    className="h-7 w-7 p-0 text-muted-foreground hover:text-foreground md:hidden"
                    aria-label={`${t("sort.label")} ${sortOrder === "desc" ? t("sort.descending") : t("sort.ascending")}`}
                  >
                    {sortOrder === "desc" ? (
                      <ChevronDown className="h-3.5 w-3.5" />
                    ) : (
                      <ChevronUp className="h-3.5 w-3.5" />
                    )}
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{`${t("sort.label")} ${sortOrder === "desc" ? t("sort.descending") : t("sort.ascending")}`}</TooltipContent>
              </Tooltip>
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
              <TruncatedText className="text-sm text-foreground">
                {effectiveSearch}
              </TruncatedText>
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

      <div
        ref={parentRef}
        className="flex-1 overflow-y-auto overscroll-contain"
        onClickCapture={(e) => {
          if (swallowListClick.current) {
            swallowListClick.current = false
            e.stopPropagation()
          }
        }}
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
            const torrent = visibleRows[virtualItem.index]
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
                  overflow: "clip",
                }}
              >
                <SwipeableCard
                  torrent={torrent}
                  copyGroup={describeCopyGroupRow(torrent)}
                  sortedField={backendSortField}
                  onToggleCopyGroup={toggleGroup}
                  isSelected={isSelected}
                  onSelect={(selected) => handleSelect(selectionIdentity, selected)}
                  onClick={() => onTorrentSelect?.(torrent)}
                  onLongPress={handleLongPress}
                  swipeOpen={!selectionMode && swipedKey === selectionIdentity}
                  onSwipeOpenChange={(open) => setCardSwiped(selectionIdentity, open)}
                  onSwipeAction={pickSwipeAction}
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

      {selectionBarVisible && (
        <div
          className="fixed bottom-0 left-0 right-0 z-40 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50"
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          <div className="flex items-center justify-around h-16">
            <button
              onClick={() => pickBulkAction({ action: "resume" })}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Play className="h-5 w-5" />
              <span className="truncate">{t("managementBar.resume")}</span>
            </button>

            <button
              onClick={() => pickBulkAction({ action: "pause" })}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Pause className="h-5 w-5" />
              <span className="truncate">{t("managementBar.pause")}</span>
            </button>

            <button
              onClick={() => pickBulkAction({ action: "setCategory" })}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Folder className="h-5 w-5" />
              <span className="truncate">{t("managementBar.setCategory")}</span>
            </button>

            <button
              onClick={() => pickBulkAction({ action: "setTags" })}
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
                      onClick={() => pickBulkAction({ action: "forceStart", enable: true })}
                      className="justify-start"
                    >
                      <FastForward className="mr-2 h-4 w-4" />
                      {t("contextMenu.forceStart")} {t("contextMenu.mixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => pickBulkAction({ action: "forceStart", enable: false })}
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
                  onClick={() => pickBulkAction({ action: "forceStart", enable: !allForceStarted })}
                  className="justify-start"
                >
                  <FastForward className="mr-2 h-4 w-4" />
                  {allForceStarted ? t("contextMenu.disableForceStart") : t("contextMenu.forceStart")}
                </Button>
              )
            })()}
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "recheck" })}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4" />
              {t("managementBar.forceRecheck")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "reannounce" })}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4" />
              {t("managementBar.reannounce")}
            </Button>
            {offer.transfer && (
              <Button
                variant="outline"
                onClick={() => pickBulkAction({ action: "transfer" })}
                className="justify-start"
              >
                <ArrowRightLeft className="mr-2 h-4 w-4" />
                {t("contextMenu.transferToInstance")}
              </Button>
            )}
            {offer.share && (
              <Button
                variant="outline"
                onClick={() => pickBulkAction({ action: "share" })}
                className="justify-start"
              >
                <Share2 className="mr-2 h-4 w-4" />
                {t("contextMenu.shareWithSomeone")}
              </Button>
            )}
            {(() => {
              const { allEnabled: allSeqDlEnabled, mixed: seqDlMixed } = getToggleSelectionState(selectedTorrents.map(t => t.seq_dl), stateUnknownForSelection)

              if (seqDlMixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => pickBulkAction({ action: "toggleSequentialDownload", enable: true })}
                      className="justify-start"
                    >
                      <Blocks className="mr-2 h-4 w-4" />
                      {t("managementBar.sequentialDownload.enable")} {t("contextMenu.mixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => pickBulkAction({ action: "toggleSequentialDownload", enable: false })}
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
                  onClick={() => pickBulkAction({ action: "toggleSequentialDownload", enable: !allSeqDlEnabled })}
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
              onClick={() => pickBulkAction({ action: "increasePriority" })}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              {t("managementBar.increasePriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "decreasePriority" })}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              {t("managementBar.decreasePriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "topPriority" })}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              {t("managementBar.topPriority")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "bottomPriority" })}
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
                      onClick={() => pickBulkAction({ action: "toggleAutoTMM", enable: true })}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4" />
                      {t("mobileCards.enableTmmMixed")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => pickBulkAction({ action: "toggleAutoTMM", enable: false })}
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
                  onClick={() => pickBulkAction({ action: "toggleAutoTMM", enable: !allEnabled })}
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
              onClick={() => pickBulkAction({ action: "setShareLimit" })}
              className="justify-start"
            >
              <Sprout className="mr-2 h-4 w-4" />
              {t("contextMenu.setShareLimits")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "setSpeedLimits" })}
              className="justify-start"
            >
              <Gauge className="mr-2 h-4 w-4" />
              {t("contextMenu.setSpeedLimits")}
            </Button>
            <Button
              variant="outline"
              onClick={() => pickBulkAction({ action: "setLocation" })}
              className="justify-start"
            >
              <FolderOpen className="mr-2 h-4 w-4" />
              {t("managementBar.setLocation")}
            </Button>
            {supportsTorrentExport && (
              <Button
                variant="outline"
                onClick={() => pickBulkAction({ action: "export" })}
                disabled={isExporting}
                className="justify-start"
              >
                <Download className="mr-2 h-4 w-4" />
                {effectiveSelectionCount > 1 ? t("contextMenu.exportTorrents", { count: effectiveSelectionCount }) : t("contextMenu.exportTorrent")}
              </Button>
            )}
            <Button
              variant="destructive"
              onClick={() => pickBulkAction({ action: "delete" })}
              className="justify-start !bg-destructive !text-destructive-foreground"
            >
              <Trash2 className="mr-2 h-4 w-4" />
              {t("managementBar.delete")}
            </Button>
          </div>
        </SheetContent>
      </Sheet>

      <DisplayProfileSheet open={showProfileSheet} onOpenChange={setShowProfileSheet} returnFocusRef={sortButtonRef} />

      {instanceId > 0 && (
        <AddTorrentDialog
          instanceId={instanceId}
          open={addTorrentModalOpen}
          onOpenChange={onAddTorrentModalChange}
        />
      )}

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

      <ScrollToTopButton
        scrollContainerRef={parentRef}
        extraBottomBarsRem={selectionBarVisible ? 4 : !selectionMode && isFooterVisible ? 3.5 : 0}
      />
    </div>
  )
}
