/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useDebounce } from "@/hooks/useDebounce"
import { useKeyboardNavigation } from "@/hooks/useKeyboardNavigation"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { usePersistedColumnSizing } from "@/hooks/usePersistedColumnSizing"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { usePersistedColumnVisibility } from "@/hooks/usePersistedColumnVisibility"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { TORRENT_ACTIONS, useTorrentActions } from "@/hooks/useTorrentActions"
import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { columnFiltersToExpr } from "@/lib/column-filter-utils"
import { formatBytes } from "@/lib/utils"
import {
  DndContext,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors
} from "@dnd-kit/core"
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers"
import {
  SortableContext,
  arrayMove,
  horizontalListSortingStrategy
} from "@dnd-kit/sortable"
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable
} from "@tanstack/react-table"
import { useVirtualizer } from "@tanstack/react-virtual"
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { InstancePreferencesDialog } from "../instances/preferences/InstancePreferencesDialog"
import { TorrentContextMenu } from "./TorrentContextMenu"
import { TORRENT_SORT_OPTIONS, getDefaultSortOrder, type TorrentSortOptionValue } from "./torrentSortOptions"

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
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Logo } from "@/components/ui/Logo"
import { ScrollToTopButton } from "@/components/ui/scroll-to-top-button"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useInstancePreferences } from "@/hooks/useInstancePreferences.ts"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, getLinuxTracker, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { getCommonCategory, getCommonSavePath, getCommonTags, getTotalSize } from "@/lib/torrent-utils"
import { cn } from "@/lib/utils"
import type {
  Category,
  ServerState,
  Torrent,
  TorrentCounts,
  TorrentFilters
} from "@/types"
import { useQuery } from "@tanstack/react-query"
import { useSearch } from "@tanstack/react-router"
import {
  ArrowUpDown,
  ChevronDown,
  ChevronUp,
  Columns3,
  Folder,
  Tag,
  X
} from "lucide-react"
import { createPortal } from "react-dom"
import { AddTorrentDialog, type AddTorrentDropPayload } from "./AddTorrentDialog"
import { DeleteTorrentDialog } from "./DeleteTorrentDialog"
import { DraggableTableHeader } from "./DraggableTableHeader"
import type { SelectionInfo } from "./GlobalStatusBar"
import { SelectAllHotkey } from "./SelectAllHotkey"
import {
  AddTagsDialog,
  CreateAndAssignCategoryDialog,
  LocationWarningDialog,
  RemoveTagsDialog,
  RenameTorrentDialog,
  RenameTorrentFileDialog,
  RenameTorrentFolderDialog,
  SetCategoryDialog,
  SetLocationDialog,
  SetTagsDialog,
  ShareLimitDialog,
  SpeedLimitsDialog,
  TmmConfirmDialog
} from "./TorrentDialogs"
import { TorrentDropZone } from "./TorrentDropZone"
import { createColumns, type TableViewMode } from "./TorrentTableColumns"

const TABLE_ALLOWED_VIEW_MODES = ["normal", "dense", "compact"] as const

// Default values for persisted state hooks (module scope for stable references)
const DEFAULT_COLUMN_VISIBILITY = {
  priority: true,
  status_icon: true,
  tracker_icon: true,
  name: true,
  size: true,
  total_size: false,
  progress: true,
  state: true,
  num_seeds: true,
  num_leechs: true,
  dlspeed: true,
  upspeed: true,
  eta: true,
  ratio: true,
  popularity: true,
  category: true,
  tags: true,
  added_on: true,
  completion_on: false,
  tracker: false,
  dl_limit: false,
  up_limit: false,
  downloaded: false,
  uploaded: false,
  downloaded_session: false,
  uploaded_session: false,
  amount_left: false,
  time_active: false,
  seeding_time: false,
  save_path: false,
  completed: false,
  ratio_limit: false,
  seen_complete: false,
  last_activity: false,
  availability: false,
  infohash_v1: false,
  infohash_v2: false,
  reannounce: false,
  private: false,
  instance: false, // Hidden by default, shown when cross-seed filtering
}
const DEFAULT_COLUMN_SIZING = {}

// Helper function to get default column order (module scope for stable reference)
function getDefaultColumnOrder(): string[] {
  const cols = createColumns(false, undefined, "bytes", undefined, undefined, undefined)
  const order = cols.map(col => {
    if ("id" in col && col.id) return col.id
    if ("accessorKey" in col && typeof col.accessorKey === "string") return col.accessorKey
    return null
  }).filter((v): v is string => typeof v === "string")

  const trackerIconIndex = order.indexOf("tracker_icon")
  if (trackerIconIndex > -1 && trackerIconIndex !== 2) {
    order.splice(2, 0, order.splice(trackerIconIndex, 1)[0])
  }

  return order
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

// Compact view helper functions and components

// Returns the background class for a row based on selection state and zebra striping
function getRowBackgroundClass(isRowSelected: boolean, isSelected: boolean, rowIndex: number): string {
  if (isRowSelected || isSelected) return "bg-accent"
  if (rowIndex % 2 === 1) return "bg-muted/40"
  return ""
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

const TrackerIcon = memo(({ title, fallback, src, size = "md", className }: TrackerIconProps) => {
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
}, (prev, next) =>
  prev.title === next.title &&
  prev.fallback === next.fallback &&
  prev.src === next.src &&
  prev.size === next.size &&
  prev.className === next.className
)

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

// Compact row component for desktop
interface CompactRowProps {
  torrent: Torrent
  rowId: string
  rowIndex: number
  isSelected: boolean
  isRowSelected: boolean
  onClick: (e: React.MouseEvent) => void
  onContextMenu: () => void
  onCheckboxPointerDown: (event: React.PointerEvent<HTMLDivElement>) => void
  onCheckboxChange: (torrent: Torrent, rowId: string, checked: boolean) => void
  incognitoMode: boolean
  speedUnit: "bytes" | "bits"
  supportsTrackerHealth: boolean
  trackerIcons?: Record<string, string>
  style: React.CSSProperties
}

const CompactRow = memo(({
  torrent,
  rowId,
  rowIndex,
  isSelected,
  isRowSelected,
  onClick,
  onContextMenu,
  onCheckboxPointerDown,
  onCheckboxChange,
  incognitoMode,
  speedUnit,
  supportsTrackerHealth,
  trackerIcons,
  style,
}: CompactRowProps) => {
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

  // Compact view
  return (
    <div
      className={cn(
        "relative flex flex-col gap-1 px-3 py-2 cursor-pointer hover:bg-accent/40 overflow-hidden",
        getRowBackgroundClass(isRowSelected, isSelected, rowIndex)
      )}
      style={style}
      onClick={(e) => onClick(e)}
      onContextMenu={onContextMenu}
    >
      {/* Progress background overlay - only show when downloading */}
      {torrent.progress < 1 && (
        <div
          className="absolute inset-0 -z-10 bg-primary/10 transition-all duration-300"
          style={{
            width: `${Math.min(100, Math.max(0, torrent.progress * 100))}%`,
          }}
          aria-hidden="true"
        />
      )}
      {/* Name with progress inline */}
      <div className="flex items-center gap-2">
        <div
          className="flex items-center justify-center flex-shrink-0"
          data-slot="checkbox"
          onPointerDown={onCheckboxPointerDown}
        >
          <Checkbox
            checked={isRowSelected}
            onCheckedChange={(checked) => onCheckboxChange(torrent, rowId, checked === true)}
            aria-label="Select torrent"
            className="h-4 w-4"
          />
        </div>
        <div className="flex-1 min-w-0 overflow-hidden">
          <div className="flex items-center gap-1 whitespace-nowrap overflow-x-auto scrollbar-thin">
            <TrackerIcon
              title={trackerMeta.title}
              fallback={trackerMeta.fallback}
              src={trackerIconSrc}
              size="sm"
              className="flex-shrink-0"
            />
            <h3 className="font-medium text-sm truncate" title={displayName}>
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
          <span className="text-muted-foreground">Ratio:</span>
          <span className={cn(
            "font-medium",
            displayRatio >= 1 ? "[color:var(--chart-3)]" : "[color:var(--chart-4)]"
          )}>
            {displayRatio === -1 ? "∞" : displayRatio.toFixed(2)}
          </span>
        </div>
      </div>

      {/* Bottom row: Category/tags and percentage/speeds */}
      <div className="flex items-center justify-between gap-2 text-xs">
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
    </div>
  )
}, (prev, next) =>
  prev.torrent.hash === next.torrent.hash &&
  prev.rowId === next.rowId &&
  prev.rowIndex === next.rowIndex &&
  prev.torrent.name === next.torrent.name &&
  prev.torrent.category === next.torrent.category &&
  prev.torrent.tags === next.torrent.tags &&
  prev.torrent.tracker === next.torrent.tracker &&
  prev.torrent.tracker_health === next.torrent.tracker_health &&
  prev.torrent.state === next.torrent.state &&
  prev.torrent.progress === next.torrent.progress &&
  prev.torrent.dlspeed === next.torrent.dlspeed &&
  prev.torrent.upspeed === next.torrent.upspeed &&
  prev.torrent.downloaded === next.torrent.downloaded &&
  prev.torrent.size === next.torrent.size &&
  prev.torrent.ratio === next.torrent.ratio &&
  prev.isSelected === next.isSelected &&
  prev.isRowSelected === next.isRowSelected &&
  prev.incognitoMode === next.incognitoMode &&
  prev.speedUnit === next.speedUnit &&
  prev.supportsTrackerHealth === next.supportsTrackerHealth &&
  prev.trackerIcons === next.trackerIcons &&
  prev.style === next.style
)

interface TorrentTableOptimizedProps {
  instanceId: number
  filters?: TorrentFilters
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (
    torrents: Torrent[],
    total: number,
    counts?: TorrentCounts,
    categories?: Record<string, Category>,
    tags?: string[],
    useSubcategories?: boolean
  ) => void
  onSelectionChange?: (
    selectedHashes: string[],
    selectedTorrents: Torrent[],
    isAllSelected: boolean,
    totalSelectionCount: number,
    excludeHashes: string[],
    selectedTotalSize: number,
    selectionFilters?: TorrentFilters
  ) => void
  onResetSelection?: (handler?: () => void) => void
  onFilterChange?: (filters: TorrentFilters) => void
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
  onServerStateUpdate?: (serverState: ServerState | null, listenPort?: number | null) => void
  onSelectionInfoUpdate?: (info: SelectionInfo) => void
}

export const TorrentTableOptimized = memo(function TorrentTableOptimized({
  instanceId,
  filters,
  selectedTorrent,
  onTorrentSelect,
  addTorrentModalOpen,
  onAddTorrentModalChange,
  onFilteredDataUpdate,
  onSelectionChange,
  onResetSelection,
  onFilterChange,
  canCrossSeedSearch,
  onCrossSeedSearch,
  isCrossSeedSearching,
  onServerStateUpdate,
  onSelectionInfoUpdate,
}: TorrentTableOptimizedProps) {
  // State management
  // Move default values outside the component for stable references
  // (This should be at module scope, not inside the component)
  const [sorting, setSorting] = usePersistedColumnSorting([], instanceId)
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  const [rowSelection, setRowSelection] = useState<Record<string, boolean>>({})

  // Custom "select all" state for handling large datasets
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())
  const [dropPayload, setDropPayload] = useState<AddTorrentDropPayload | null>(null)

  // Instance preferences dialog state
  const [preferencesOpen, setPreferencesOpen] = useState(false)

  // Filter lifecycle state machine to replace fragile timing-based coordination
  type FilterLifecycleState = 'idle' | 'clearing-all' | 'clearing-columns-only' | 'cleared'
  const [filterLifecycleState, setFilterLifecycleState] = useState<FilterLifecycleState>('idle')

  const [incognitoMode] = useIncognitoMode()
  const { exportTorrents, isExporting: isExportingTorrent } = useTorrentExporter({ instanceId, incognitoMode })
  const [speedUnit] = useSpeedUnits()
  const { formatTimestamp } = useDateTimeFormatters()
  const { preferences } = useInstancePreferences(instanceId)
  const { instances } = useInstances()
  const instance = useMemo(() => instances?.find(i => i.id === instanceId), [instances, instanceId])

  // Desktop view mode state (separate from mobile view mode)
  const { viewMode: desktopViewMode } = usePersistedCompactViewState("normal", TABLE_ALLOWED_VIEW_MODES)

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

  // Detect platform for keyboard shortcuts
  const isMac = useMemo(() => {
    return typeof window !== "undefined" && /Mac|iPhone|iPad|iPod/.test(window.navigator.userAgent)
  }, [])

  // Track user-initiated actions to differentiate from automatic data updates
  const [lastUserAction, setLastUserAction] = useState<{ type: string; timestamp: number } | null>(null)
  const previousFiltersRef = useRef(filters)
  const previousInstanceIdRef = useRef(instanceId)
  const previousSearchRef = useRef("")
  const lastMetadataRef = useRef<{
    counts?: TorrentCounts
    categories?: Record<string, Category>
    tags?: string[]
    totalCount?: number
    torrentsLength?: number
    useSubcategories?: boolean
    supportsSubcategories?: boolean
  }>({})
  const serverStateRef = useRef<{ instanceId: number, state: ServerState | null }>({
    instanceId,
    state: null,
  })

  // State for range select capabilities for checkboxes
  const shiftPressedRef = useRef<boolean>(false)
  const lastSelectedIndexRef = useRef<number | null>(null)

  // Cross-seed async filtering polling

  const handleCompactCheckboxPointerDown = useCallback((event: React.PointerEvent<HTMLDivElement>) => {
    shiftPressedRef.current = event.shiftKey
  }, [])

  const resetSelectionState = useCallback(() => {
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
    setRowSelection({})
    lastSelectedIndexRef.current = null
  }, [setIsAllSelected, setExcludedFromSelectAll, setRowSelection])

  useEffect(() => {
    if (!onResetSelection) {
      return
    }

    onResetSelection(resetSelectionState)
    return () => {
      onResetSelection(undefined)
    }
  }, [onResetSelection, resetSelectionState])

  // These should be defined at module scope, not inside the component, to ensure stable references
  // (If not already, move them to the top of the file)
  // const DEFAULT_COLUMN_VISIBILITY, DEFAULT_COLUMN_ORDER, DEFAULT_COLUMN_SIZING

  // Column visibility with persistence
  const [columnVisibility, setColumnVisibility] = usePersistedColumnVisibility(DEFAULT_COLUMN_VISIBILITY, instanceId)
  // Column order with persistence (get default order at runtime to avoid initialization order issues)
  const [columnOrder, setColumnOrder] = usePersistedColumnOrder(getDefaultColumnOrder(), instanceId)
  // Column sizing with persistence
  const [columnSizing, setColumnSizing] = usePersistedColumnSizing(DEFAULT_COLUMN_SIZING, instanceId)
  // Column filters with persistence
  const [columnFilters, setColumnFilters] = usePersistedColumnFilters(instanceId)

  // Progressive loading state with async management
  const [loadedRows, setLoadedRows] = useState(100)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)

  // Delayed loading state to avoid flicker on fast loads
  const [showLoadingState, setShowLoadingState] = useState(false)

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
    showAddTagsDialog,
    setShowAddTagsDialog,
    showSetTagsDialog,
    setShowSetTagsDialog,
    showRemoveTagsDialog,
    setShowRemoveTagsDialog,
    showCategoryDialog,
    setShowCategoryDialog,
    showCreateCategoryDialog,
    setShowCreateCategoryDialog,
    showShareLimitDialog,
    setShowShareLimitDialog,
    showSpeedLimitDialog,
    setShowSpeedLimitDialog,
    showLocationDialog,
    setShowLocationDialog,
    showRenameTorrentDialog,
    setShowRenameTorrentDialog,
    showRenameFileDialog,
    setShowRenameFileDialog,
    showRenameFolderDialog,
    setShowRenameFolderDialog,
    showRecheckDialog,
    setShowRecheckDialog,
    showReannounceDialog,
    setShowReannounceDialog,
    showTmmDialog,
    setShowTmmDialog,
    pendingTmmEnable,
    showLocationWarningDialog,
    setShowLocationWarningDialog,
    contextHashes,
    contextTorrents,
    isPending,
    handleAction,
    handleDelete,
    handleAddTags,
    handleSetTags,
    handleRemoveTags,
    handleSetCategory,
    handleSetLocation,
    handleRenameTorrent,
    handleRenameFile,
    handleRenameFolder,
    handleSetShareLimit,
    handleSetSpeedLimits,
    handleRecheck,
    handleReannounce,
    handleTmmConfirm,
    proceedToLocationDialog,
    prepareDeleteAction,
    prepareTagsAction,
    prepareCategoryAction,
    prepareCreateCategoryAction,
    prepareShareLimitAction,
    prepareSpeedLimitAction,
    prepareLocationAction,
    prepareRenameTorrentAction,
    prepareRenameFileAction,
    prepareRenameFolderAction,
    prepareRecheckAction,
    prepareReannounceAction,
    prepareTmmAction,
  } = useTorrentActions({
    instanceId,
    onActionComplete: (action) => {
      if (action === TORRENT_ACTIONS.DELETE) {
        resetSelectionState()
      }
    },
  })

  // Cross-seed warning for delete dialog
  const crossSeedWarning = useCrossSeedWarning({
    instanceId,
    instanceName: instance?.name ?? "",
    torrents: contextTorrents,
  })

  // Fetch metadata using shared hook
  const { data: metadata, isLoading: isMetadataLoading } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}
  const isLoadingTags = isMetadataLoading && availableTags.length === 0
  const isLoadingCategories = isMetadataLoading && Object.keys(availableCategories).length === 0

  const shouldLoadRenameEntries = (showRenameFileDialog || showRenameFolderDialog) && Boolean(contextHashes[0])

  const {
    data: renameFileData,
    isLoading: renameEntriesLoading,
  } = useQuery({
    queryKey: ["torrent-files", instanceId, contextHashes[0]],
    queryFn: () => api.getTorrentFiles(instanceId, contextHashes[0]!, { refresh: true }),
    enabled: shouldLoadRenameEntries,
    staleTime: 0,
    gcTime: 5 * 60 * 1000,
  })

  const renameFileEntries = useMemo(() => {
    if (!Array.isArray(renameFileData)) return [] as { name: string }[]
    return renameFileData
      .filter((file) => typeof file?.name === "string")
      .map((file) => ({ name: file.name }))
  }, [renameFileData])

  const renameFolderEntries = useMemo(() => {
    if (renameFileEntries.length === 0) return [] as { name: string }[]
    const folderSet = new Set<string>()
    for (const file of renameFileEntries) {
      const parts = file.name.split("/")
      if (parts.length <= 1) continue
      let current = ""
      for (let i = 0; i < parts.length - 1; i++) {
        current = current ? `${current}/${parts[i]}` : parts[i]
        folderSet.add(current)
      }
    }
    return Array.from(folderSet)
      .sort((a, b) => a.localeCompare(b))
      .map(name => ({ name }))
  }, [renameFileEntries])

  // Debounce search to prevent excessive filtering (200ms delay for faster response)
  const debouncedSearch = useDebounce(globalFilter, 200)
  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const rawRouteSearch = typeof routeSearch?.q === "string" ? routeSearch.q : ""
  const searchFromRoute = rawRouteSearch.trim()

  // Use route search if present, otherwise fall back to local immediate/debounced search
  const effectiveSearch = (searchFromRoute || immediateSearch || debouncedSearch).trim()

  // Keep local input state in sync with route query so internal effects remain consistent
  useEffect(() => {
    if (searchFromRoute !== globalFilter) {
      setGlobalFilter(searchFromRoute)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [searchFromRoute])

  // Convert column filters to expr format for backend
  const columnFiltersExpr = useMemo(() => columnFiltersToExpr(columnFilters), [columnFilters])

  // Detect if this is cross-seed filtering (same logic as in useTorrentsList)
  const isDoingCrossSeedFiltering = useMemo(() => {
    return filters?.expr?.includes('Hash ==') && filters?.expr?.includes('||')
  }, [filters?.expr])

  // Combine column filters with any existing filter expression
  // For cross-seed filtering, we'll apply column filters client-side only
  const combinedFiltersExpr = useMemo(() => {
    const columnExpr = columnFiltersExpr
    const filterExpr = filters?.expr

    // If we're doing cross-seed filtering, don't send column filters to backend
    // They will be applied client-side by TanStack Table (along with sorting)
    if (isDoingCrossSeedFiltering) {
      return filterExpr // Only use the cross-seed expression for backend
    }

    // For regular filtering, combine column filters with existing filters
    if (columnExpr && filterExpr) {
      const combined = `(${columnExpr}) && (${filterExpr})`
      return combined
    }
    return columnExpr || filterExpr
  }, [columnFiltersExpr, filters?.expr, isDoingCrossSeedFiltering])

  // Detect user-initiated changes
  useEffect(() => {
    const filtersChanged = JSON.stringify(previousFiltersRef.current) !== JSON.stringify(filters)
    const instanceChanged = previousInstanceIdRef.current !== instanceId
    const searchChanged = previousSearchRef.current !== effectiveSearch

    if (filtersChanged || instanceChanged || searchChanged) {
      setLastUserAction({
        type: instanceChanged ? "instance" : filtersChanged ? "filter" : "search",
        timestamp: Date.now(),
      })

      // Update refs
      previousFiltersRef.current = filters
      previousInstanceIdRef.current = instanceId
      previousSearchRef.current = effectiveSearch
    }
  }, [filters, instanceId, effectiveSearch])

  // Map TanStack Table column IDs to backend field names
  const getBackendSortField = (columnId: string): string => {
    if (!columnId) {
      return "added_on"
    }

    switch (columnId) {
      case "status_icon":
        return "state"
      case "num_seeds":
        return "num_complete" // Sort by total seeds, not connected
      case "num_leechs":
        return "num_incomplete" // Sort by total peers, not connected
      default:
        return columnId
    }
  }

  const activeSortField = sorting.length > 0 ? getBackendSortField(sorting[0].id) : "added_on"
  const activeSortOrder: "asc" | "desc" = sorting.length > 0 ? (sorting[0].desc ? "desc" : "asc") : "desc"

  const effectiveIncludedCategories = filters?.expandedCategories ?? filters?.categories ?? []
  const effectiveExcludedCategories = filters?.expandedExcludeCategories ?? filters?.excludeCategories ?? []

  // Fetch torrents data with backend sorting
  const {
    torrents,
    totalCount,
    stats,
    counts,
    categories,
    tags,
    serverState,
    capabilities,
    useSubcategories: subcategoriesFromData,
    isLoading,
    isCachedData,
    isStaleData,
    isLoadingMore,
    hasLoadedAll,
    loadMore: backendLoadMore,
    isCrossSeedFiltering,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters: {
      status: filters?.status || [],
      excludeStatus: filters?.excludeStatus || [],
      categories: effectiveIncludedCategories,
      excludeCategories: effectiveExcludedCategories,
      tags: filters?.tags || [],
      excludeTags: filters?.excludeTags || [],
      trackers: filters?.trackers || [],
      excludeTrackers: filters?.excludeTrackers || [],
      expandedCategories: filters?.expandedCategories,
      expandedExcludeCategories: filters?.expandedExcludeCategories,
      expr: combinedFiltersExpr || undefined,
    },
    sort: activeSortField,
    order: activeSortOrder,
  })

  const supportsTrackerHealth = capabilities?.supportsTrackerHealth ?? true
  const supportsSubcategories = capabilities?.supportsSubcategories ?? false
  const allowSubcategories =
    supportsSubcategories && (preferences?.use_subcategories ?? subcategoriesFromData ?? false)

  // When cross-seed filtering is active, ensure instance column is visible
  useEffect(() => {
    if (isDoingCrossSeedFiltering && columnVisibility.instance === false) {
      setColumnVisibility(prev => ({ ...prev, instance: true }))
    }
  }, [isDoingCrossSeedFiltering, columnVisibility.instance, setColumnVisibility])

  // Delayed loading state to avoid flicker on fast loads
  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>

    if (isLoading && torrents.length === 0) {
      // Start a timer to show loading state after 500ms
      timeoutId = setTimeout(() => {
        setShowLoadingState(true)
      }, 500)
    } else {
      // Clear the timer and hide loading state when not loading
      setShowLoadingState(false)
    }

    return () => {
      if (timeoutId) {
        clearTimeout(timeoutId)
      }
    }
  }, [isLoading, torrents.length])

  const hasSidebarFilters = useMemo(() => {
    if (!filters) {
      return false
    }

    const {
      status = [],
      excludeStatus = [],
      categories = [],
      excludeCategories = [],
      expandedCategories = [],
      expandedExcludeCategories = [],
      tags = [],
      excludeTags = [],
      trackers = [],
      excludeTrackers = [],
      expr,
    } = filters

    return (
      status.length > 0 ||
      excludeStatus.length > 0 ||
      categories.length > 0 ||
      excludeCategories.length > 0 ||
      expandedCategories.length > 0 ||
      expandedExcludeCategories.length > 0 ||
      tags.length > 0 ||
      excludeTags.length > 0 ||
      trackers.length > 0 ||
      excludeTrackers.length > 0 ||
      Boolean(expr?.trim())
    )
  }, [filters])

  const hasSearchQuery = Boolean(effectiveSearch)
  const hasFilterControls = useMemo(() => {
    return hasSidebarFilters || columnFilters.length > 0
  }, [hasSidebarFilters, columnFilters])
  const emptyStateMessage = useMemo(() => {
    if (hasFilterControls) {
      return "No torrents match the current filters"
    }
    if (hasSearchQuery) {
      return "No torrents match the current search"
    }
    return "No torrents found"
  }, [hasFilterControls, hasSearchQuery])

  // Call the callback when filtered data updates
  useEffect(() => {
    if (!onFilteredDataUpdate || isLoading) {
      return
    }

    const nextCounts = counts ?? lastMetadataRef.current.counts
    const nextCategories = categories ?? lastMetadataRef.current.categories
    const nextTags = tags ?? lastMetadataRef.current.tags
    const prevSupportsSubcategories = lastMetadataRef.current.supportsSubcategories ?? false
    const previousUseSubcategories = lastMetadataRef.current.useSubcategories ?? false
    const nextSupportsSubcategories = supportsSubcategories
    const nextUseSubcategories = nextSupportsSubcategories ? (subcategoriesFromData ?? previousUseSubcategories) : false
    const nextTotalCount = totalCount

    const hasAnyMetadata =
      nextCounts !== undefined ||
      nextCategories !== undefined ||
      nextTags !== undefined ||
      nextUseSubcategories !== undefined
    const hasExistingTorrents = torrents.length > 0

    if (!hasAnyMetadata && !hasExistingTorrents) {
      return
    }

    const metadataChanged =
      nextCounts !== lastMetadataRef.current.counts ||
      nextCategories !== lastMetadataRef.current.categories ||
      nextTags !== lastMetadataRef.current.tags ||
      nextSupportsSubcategories !== prevSupportsSubcategories ||
      nextUseSubcategories !== previousUseSubcategories ||
      nextTotalCount !== lastMetadataRef.current.totalCount

    const torrentsLengthChanged = torrents.length !== (lastMetadataRef.current.torrentsLength ?? -1)

    if (!metadataChanged && !torrentsLengthChanged) {
      return
    }

    onFilteredDataUpdate(
      torrents,
      totalCount,
      nextCounts,
      nextCategories,
      nextTags,
      nextUseSubcategories
    )

    lastMetadataRef.current = {
      counts: nextCounts,
      categories: nextCategories,
      tags: nextTags,
      totalCount: nextTotalCount,
      torrentsLength: torrents.length,
      useSubcategories: nextUseSubcategories,
      supportsSubcategories: nextSupportsSubcategories,
    }
  }, [counts, categories, tags, totalCount, torrents, isLoading, onFilteredDataUpdate, subcategoriesFromData, supportsSubcategories])

  // Use torrents directly from backend (already sorted)
  const sortedTorrents = torrents

  // Atomic filter clearing callback
  const clearFiltersAtomically = useCallback((mode: 'all' | 'columns-only' = 'all') => {
    setFilterLifecycleState(mode === 'all' ? 'clearing-all' : 'clearing-columns-only');
  }, []);
  const effectiveServerState = useMemo(() => {
    const cached = serverStateRef.current
    const instanceChanged = cached.instanceId !== instanceId

    if (serverState != null) {
      serverStateRef.current = { instanceId, state: serverState }
      return serverState
    }

    if (serverState === null) {
      serverStateRef.current = { instanceId, state: null }
      return null
    }

    if (instanceChanged) {
      serverStateRef.current = { instanceId, state: null }
      return null
    }

    return cached.state
  }, [serverState, instanceId])

  // Notify parent of server state updates
  const listenPort = metadata?.preferences?.listen_port
  useEffect(() => {
    onServerStateUpdate?.(effectiveServerState, listenPort)
  }, [effectiveServerState, listenPort, onServerStateUpdate])

  const selectedRowIds = useMemo(() => {
    const ids: string[] = []
    for (const [rowId, isSelected] of Object.entries(rowSelection)) {
      if (isSelected) {
        ids.push(rowId)
      }
    }
    return ids
  }, [rowSelection])
  const selectedRowIdSet = useMemo(() => new Set(selectedRowIds), [selectedRowIds])

  useEffect(() => {
    if (isAllSelected) {
      if (excludedFromSelectAll.size === 0) {
        return
      }

      const visibleHashes = new Set(sortedTorrents.map(torrent => torrent.hash))
      const hasInvalidExclusion = Array.from(excludedFromSelectAll).some(hash => !visibleHashes.has(hash))

      if (hasInvalidExclusion) {
        resetSelectionState()
      }

      return
    }

    if (Object.keys(rowSelection).length === 0) {
      return
    }

    const visibleRowIds = new Set(table.getRowModel().rows.map(row => row.id))
    const hasInvalidSelection = Object.entries(rowSelection).some(([rowId, selected]) => selected && !visibleRowIds.has(rowId))

    if (hasInvalidSelection) {
      resetSelectionState()
    }
  }, [
    excludedFromSelectAll,
    isAllSelected,
    resetSelectionState,
    rowSelection,
    sortedTorrents,
  ])

  // Reset selection when table becomes empty
  useEffect(() => {
    if (sortedTorrents.length === 0 && (isAllSelected || Object.keys(rowSelection).length > 0)) {
      resetSelectionState()
    }
  }, [sortedTorrents.length, isAllSelected, rowSelection, resetSelectionState])

  // Custom selection handlers for "select all" functionality
  const handleSelectAll = useCallback(() => {
    // Gmail-style behavior: if any rows are selected, always deselect all
    const hasAnySelection = isAllSelected || selectedRowIds.length > 0

    if (hasAnySelection) {
      // Deselect all mode - regardless of checked state
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
      lastSelectedIndexRef.current = null // Reset anchor on deselect all
    } else {
      // Select all mode - only when nothing is selected
      setIsAllSelected(true)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
    }
  }, [setRowSelection, isAllSelected, selectedRowIds.length])

  const handleRowSelection = useCallback((hash: string, checked: boolean, rowId?: string) => {
    if (isAllSelected) {
      if (!checked) {
        // When deselecting a row in "select all" mode, add to exclusions
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
      // Regular selection mode - use table's built-in selection with correct row ID
      const keyToUse = rowId || hash // Use rowId if provided, fallback to hash for backward compatibility
      setRowSelection(prev => ({
        ...prev,
        [keyToUse]: checked,
      }))
    }
  }, [isAllSelected, setRowSelection])

  // Calculate these after we have selectedHashes
  const isSelectAllChecked = useMemo(() => {
    if (isAllSelected) {
      // When in "select all" mode, only show checked if no exclusions exist
      return excludedFromSelectAll.size === 0
    }
    const regularSelectionCount = selectedRowIds.length
    return regularSelectionCount === sortedTorrents.length && sortedTorrents.length > 0
  }, [isAllSelected, excludedFromSelectAll.size, selectedRowIds.length, sortedTorrents.length])

  const isSelectAllIndeterminate = useMemo(() => {
    // Show indeterminate (dash) when SOME but not ALL items are selected
    if (isAllSelected) {
      // In "select all" mode, show indeterminate if some are excluded
      return excludedFromSelectAll.size > 0
    }

    const regularSelectionCount = selectedRowIds.length

    // Indeterminate when some (but not all) are selected
    return regularSelectionCount > 0 && regularSelectionCount < sortedTorrents.length
  }, [isAllSelected, excludedFromSelectAll.size, selectedRowIds.length, sortedTorrents.length])

  // Memoize columns to avoid unnecessary recalculations
  const columns = useMemo(
    () => createColumns(incognitoMode, {
      shiftPressedRef,
      lastSelectedIndexRef,
      // Pass custom selection handlers
      customSelectAll: {
        onSelectAll: handleSelectAll,
        isAllSelected: isSelectAllChecked,
        isIndeterminate: isSelectAllIndeterminate,
      },
      onRowSelection: handleRowSelection,
      isAllSelected,
      excludedFromSelectAll,
    }, speedUnit, trackerIcons, formatTimestamp, preferences, supportsTrackerHealth, isCrossSeedFiltering, desktopViewMode as TableViewMode),
    [incognitoMode, speedUnit, trackerIcons, formatTimestamp, handleSelectAll, isSelectAllChecked, isSelectAllIndeterminate, handleRowSelection, isAllSelected, excludedFromSelectAll, preferences, supportsTrackerHealth, isCrossSeedFiltering, desktopViewMode]
  )

  const torrentIdentityCounts = useMemo(() => {
    const counts = new Map<string, number>()

    for (const torrent of sortedTorrents) {
      const baseIdentity = torrent.hash ?? torrent.infohash_v1 ?? torrent.infohash_v2
      if (!baseIdentity) continue
      counts.set(baseIdentity, (counts.get(baseIdentity) ?? 0) + 1)
    }

    return counts
  }, [sortedTorrents])

  const table = useReactTable({
    data: sortedTorrents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    // For cross-seed filtering, enable client-side sorting and filtering
    // For regular filtering, backend handles sorting and column filters
    manualSorting: !isCrossSeedFiltering,
    getSortedRowModel: isCrossSeedFiltering ? getSortedRowModel() : undefined,
    manualFiltering: !isCrossSeedFiltering,
    getFilteredRowModel: isCrossSeedFiltering ? getFilteredRowModel() : undefined,
    // Prefer stable torrent hash for row identity while keeping duplicates unique
    getRowId: (row: Torrent, index: number) => {
      const baseIdentity = row.hash ?? row.infohash_v1 ?? row.infohash_v2

      if (!baseIdentity) {
        return `row-${index}`
      }

      if ((torrentIdentityCounts.get(baseIdentity) ?? 0) > 1) {
        return `${baseIdentity}-${index}`
      }

      return baseIdentity
    },
    // State management
    state: {
      sorting,
      globalFilter,
      rowSelection,
      columnSizing,
      columnVisibility,
      columnOrder,
      // Convert our custom ColumnFilter format to TanStack Table format when doing client-side filtering
      ...(isCrossSeedFiltering && {
        columnFilters: columnFilters.map(filter => ({
          id: filter.columnId,
          value: filter.value
        }))
      }),
    },
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    onRowSelectionChange: setRowSelection,
    onColumnSizingChange: setColumnSizing,
    onColumnVisibilityChange: setColumnVisibility,
    onColumnOrderChange: setColumnOrder,
    // Enable row selection
    enableRowSelection: true,
    // Enable column resizing
    enableColumnResizing: true,
    columnResizeMode: "onChange" as const,
    // Prevent automatic state resets during data updates
    autoResetPageIndex: false,
    autoResetExpanded: false,
  })

  // Fix virtualization when column filters are cleared in cross-seed mode
  // Only run when lifecycle is idle to avoid racing with filter lifecycle handler
  useEffect(() => {
    if (filterLifecycleState === 'idle' && isCrossSeedFiltering && columnFilters.length === 0) {
      // Reset loadedRows to ensure all rows are visible when filters are cleared
      const targetRows = Math.min(100, sortedTorrents.length)
      // Use functional update to ensure idempotent, non-racing updates
      setLoadedRows(prev => Math.max(prev, targetRows))
    }
  }, [filterLifecycleState, isCrossSeedFiltering, columnFilters.length, sortedTorrents.length])

  const resolveSortColumnId = useCallback((field: string): string => {
    const columns = table.getAllLeafColumns()
    const directMatch = columns.find(column => column.id === field)
    if (directMatch) {
      return directMatch.id
    }

    const backendMatch = columns.find(column => getBackendSortField(column.id) === field)
    if (backendMatch) {
      return backendMatch.id
    }

    return field
  }, [table, columnVisibility, columnOrder])

  const compactSortOptions = useMemo(() => {
    const columns = table.getAllLeafColumns()
    const availableFields = new Set<string>()

    for (const column of columns) {
      availableFields.add(column.id)
      availableFields.add(getBackendSortField(column.id))
    }

    return TORRENT_SORT_OPTIONS.filter(option => availableFields.has(option.value))
  }, [table, columnVisibility, columnOrder])

  const currentCompactSortLabel = useMemo(() => {
    const directOption = compactSortOptions.find(option => option.value === activeSortField)
    if (directOption) {
      return directOption.label
    }

    const columns = table.getAllLeafColumns()
    const directColumn = columns.find(column => column.id === activeSortField)
    if (directColumn) {
      const meta = directColumn.columnDef.meta as { headerString?: string } | undefined
      if (meta?.headerString) {
        return meta.headerString
      }
      if (typeof directColumn.columnDef.header === "string") {
        return directColumn.columnDef.header
      }
      return directColumn.id
    }

    const backendColumn = columns.find(column => getBackendSortField(column.id) === activeSortField)
    if (backendColumn) {
      const meta = backendColumn.columnDef.meta as { headerString?: string } | undefined
      if (meta?.headerString) {
        return meta.headerString
      }
      if (typeof backendColumn.columnDef.header === "string") {
        return backendColumn.columnDef.header
      }
      return backendColumn.id
    }

    return activeSortField
  }, [compactSortOptions, activeSortField, table, columnVisibility, columnOrder])

  const handleCompactSortFieldChange = useCallback((value: TorrentSortOptionValue) => {
    if (activeSortField === value) {
      return
    }

    const columnId = resolveSortColumnId(value)
    const defaultOrder = getDefaultSortOrder(value)

    setSorting([{ id: columnId, desc: defaultOrder === "desc" }])
    setLastUserAction({
      type: "sort",
      timestamp: Date.now(),
    })
  }, [activeSortField, resolveSortColumnId, setSorting, setLastUserAction])

  const handleCompactSortOrderToggle = useCallback(() => {
    const columnId = resolveSortColumnId(activeSortField)
    const nextDesc = activeSortOrder === "asc"

    setSorting([{ id: columnId, desc: nextDesc }])
    setLastUserAction({
      type: "sort",
      timestamp: Date.now(),
    })
  }, [activeSortField, activeSortOrder, resolveSortColumnId, setSorting, setLastUserAction])

  const handleCompactCheckboxChange = useCallback((torrent: Torrent, rowId: string, checked: boolean) => {
    const nextChecked = !!checked
    const allRows = table.getRowModel().rows
    const currentIndex = allRows.findIndex(r => r.id === rowId)

    if (shiftPressedRef.current && lastSelectedIndexRef.current !== null && currentIndex !== -1) {
      const start = Math.min(lastSelectedIndexRef.current, currentIndex)
      const end = Math.max(lastSelectedIndexRef.current, currentIndex)

      for (let i = start; i <= end; i++) {
        const targetRow = allRows[i]
        if (targetRow) {
          handleRowSelection(targetRow.original.hash, nextChecked, targetRow.id)
        }
      }
    } else {
      handleRowSelection(torrent.hash, nextChecked, rowId)
    }

    if (currentIndex !== -1) {
      lastSelectedIndexRef.current = currentIndex
    }
    shiftPressedRef.current = false
  }, [handleRowSelection, table])

  // Get selected torrent hashes - handle both regular selection and "select all" mode
  const selectedHashes = useMemo((): string[] => {
    if (isAllSelected) {
      // When all are selected, return all currently loaded hashes minus exclusions
      // This is needed for actions to work properly
      return sortedTorrents
        .map(t => t.hash)
        .filter(hash => !excludedFromSelectAll.has(hash))
    } else {
      // Regular selection mode - get hashes from selected torrents directly
      const tableRows = table.getRowModel().rows
      return tableRows
        .filter(row => selectedRowIdSet.has(row.id))
        .map(row => row.original.hash)
    }
  }, [selectedRowIdSet, isAllSelected, excludedFromSelectAll, sortedTorrents, table])

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // When all selected, count is total minus exclusions
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the computed selectedHashes length
      return selectedRowIds.length
    }
  }, [isAllSelected, totalCount, excludedFromSelectAll.size, selectedRowIds.length])

  // Get selected torrents
  const selectedTorrents = useMemo((): Torrent[] => {
    if (isAllSelected) {
      // When all are selected, return all torrents minus exclusions
      return sortedTorrents.filter(t => !excludedFromSelectAll.has(t.hash))
    } else {
      // Regular selection mode
      return selectedHashes
        .map((hash: string) => sortedTorrents.find((t: Torrent) => t.hash === hash))
        .filter(Boolean) as Torrent[]
    }
  }, [selectedHashes, sortedTorrents, isAllSelected, excludedFromSelectAll])

  // Calculate total size of selected torrents
  const selectedTotalSize = useMemo(() => {
    if (isAllSelected) {
      const aggregateTotalSize = stats?.totalSize ?? 0

      if (aggregateTotalSize <= 0) {
        return 0
      }

      if (excludedFromSelectAll.size === 0) {
        return aggregateTotalSize
      }

      const excludedSize = sortedTorrents.reduce((total, torrent) => {
        if (excludedFromSelectAll.has(torrent.hash)) {
          return total + (torrent.size || 0)
        }
        return total
      }, 0)

      return Math.max(aggregateTotalSize - excludedSize, 0)
    }

    return getTotalSize(selectedTorrents)
  }, [isAllSelected, stats?.totalSize, excludedFromSelectAll, sortedTorrents, selectedTorrents])
  const selectedFormattedSize = useMemo(() => formatBytes(selectedTotalSize), [selectedTotalSize])

  // Size shown in destructive dialogs - prefer the aggregate when select-all is active
  const deleteDialogTotalSize = useMemo(() => {
    if (isAllSelected) {
      if (selectedTotalSize > 0) {
        return selectedTotalSize
      }

      if (contextTorrents.length > 0) {
        return getTotalSize(contextTorrents)
      }

      return 0
    }

    if (contextTorrents.length > 0) {
      return getTotalSize(contextTorrents)
    }

    return selectedTotalSize
  }, [isAllSelected, selectedTotalSize, contextTorrents])
  const deleteDialogFormattedSize = useMemo(() => formatBytes(deleteDialogTotalSize), [deleteDialogTotalSize])

  const selectAllFilters = useMemo(() => {
    if (!isAllSelected) {
      return undefined
    }

    const combinedExpr = columnFiltersExpr ?? filters?.expr

    if (filters) {
      return {
        ...filters,
        expr: combinedExpr ?? filters.expr ?? "",
      }
    }

    if (combinedExpr == null) {
      return undefined
    }

    return {
      status: [],
      excludeStatus: [],
      categories: [],
      excludeCategories: [],
      tags: [],
      excludeTags: [],
      trackers: [],
      excludeTrackers: [],
      expr: combinedExpr,
    }
  }, [isAllSelected, filters, columnFiltersExpr])

  // Call the callback when selection state changes
  useEffect(() => {
    if (onSelectionChange) {
      onSelectionChange(
        selectedHashes,
        selectedTorrents,
        isAllSelected,
        effectiveSelectionCount,
        Array.from(excludedFromSelectAll),
        selectedTotalSize,
        selectAllFilters ?? filters
      )
    }
  }, [onSelectionChange, selectedHashes, selectedTorrents, isAllSelected, effectiveSelectionCount, excludedFromSelectAll, selectedTotalSize, selectAllFilters, filters])

  // Virtualization setup with progressive loading
  const { rows } = table.getRowModel()
  const parentRef = useRef<HTMLDivElement>(null)

  // Load more rows as user scrolls (progressive loading + backend pagination)
  const loadMore = useCallback((): void => {
    // First, try to load more from virtual scrolling if we have more local data
    if (loadedRows < sortedTorrents.length) {
      // Prevent concurrent loads
      if (isLoadingMoreRows) {
        return
      }

      setIsLoadingMoreRows(true)

      setLoadedRows(prev => {
        const newLoadedRows = Math.min(prev + 100, sortedTorrents.length)
        return newLoadedRows
      })

      // Reset loading flag after a short delay
      setTimeout(() => setIsLoadingMoreRows(false), 100)
    } else if (!hasLoadedAll && !isLoadingMore && backendLoadMore) {
      // If we've displayed all local data but there's more on backend, load next page
      backendLoadMore()
    }
  }, [sortedTorrents.length, isLoadingMoreRows, loadedRows, hasLoadedAll, isLoadingMore, backendLoadMore])

  // Ensure loadedRows never exceeds actual data length
  const safeLoadedRows = Math.min(loadedRows, rows.length)

  // Also keep loadedRows in sync with actual data to prevent status display issues
  useEffect(() => {
    if (filterLifecycleState === 'idle' && loadedRows > rows.length && rows.length > 0) {
      setLoadedRows(rows.length)
    }
  }, [loadedRows, rows.length, filterLifecycleState])

  // Notify parent of selection info updates
  useEffect(() => {
    onSelectionInfoUpdate?.({
      effectiveSelectionCount,
      isAllSelected,
      excludedFromSelectAllSize: excludedFromSelectAll.size,
      selectedFormattedSize,
      torrentsLength: torrents.length,
      totalCount,
      hasLoadedAll,
      isLoading,
      isLoadingMore,
      isCachedData,
      isStaleData,
      emptyStateMessage,
      safeLoadedRows,
      rowsLength: rows.length,
    })
  }, [
    onSelectionInfoUpdate,
    effectiveSelectionCount,
    isAllSelected,
    excludedFromSelectAll.size,
    selectedFormattedSize,
    torrents.length,
    totalCount,
    hasLoadedAll,
    isLoading,
    isLoadingMore,
    isCachedData,
    isStaleData,
    emptyStateMessage,
    safeLoadedRows,
    rows.length,
  ])

  // Compute estimated row height based on view mode - used by virtualizer and keyboard navigation
  const estimatedRowHeight = useMemo(() => {
    switch (desktopViewMode) {
      case "compact": return 80
      case "dense": return 26
      default: return 40
    }
  }, [desktopViewMode])

  // useVirtualizer must be called at the top level, not inside useMemo
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => estimatedRowHeight,
    // Optimized overscan based on TanStack Virtual recommendations
    // Start small and adjust based on dataset size and performance
    overscan: sortedTorrents.length > 50000 ? 3 : sortedTorrents.length > 10000 ? 5 : sortedTorrents.length > 1000 ? 10 : 15,
    // Provide a key to help with item tracking - use hash with index for uniqueness
    getItemKey: useCallback((index: number) => {
      const row = rows[index]
      if (!row) return `loading-${index}`
      return row.id
    }, [rows]),
    // Optimized onChange handler following TanStack Virtual best practices
    onChange: (instance, sync) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      // Only trigger loadMore when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive loadMore calls during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling

      if (shouldCheckLoadMore && lastItem && lastItem.index >= safeLoadedRows - 50) {
        // Load more if we're near the end of virtual rows OR if we might need more data from backend
        if (safeLoadedRows < rows.length || (!hasLoadedAll && !isLoadingMore)) {
          loadMore();
        }
      }
    },
  })

  // Filter lifecycle state machine
  useLayoutEffect(() => {
    if (filterLifecycleState === 'clearing-all' || filterLifecycleState === 'clearing-columns-only') {

      // Perform clearing operations atomically
      setColumnFilters([]);
      setSorting([]);
      virtualizer.scrollToOffset(0);
      virtualizer.measure();

      // Reset loadedRows to a reasonable initial value
      const newLoadedRows = Math.min(100, sortedTorrents.length);
      setLoadedRows(newLoadedRows);

      // Only clear parent filters if clearing all (not just columns)
      if (filterLifecycleState === 'clearing-all') {
        const emptyFilters: TorrentFilters = {
          status: [],
          excludeStatus: [],
          categories: [],
          excludeCategories: [],
          tags: [],
          excludeTags: [],
          trackers: [],
          excludeTrackers: []
        };
        onFilterChange?.(emptyFilters);
      }

      // Transition to cleared state
      setFilterLifecycleState('cleared');
    } else if (filterLifecycleState === 'cleared') {
      // Reset to idle state after clearing is complete
      setFilterLifecycleState('idle');
    }
  }, [filterLifecycleState, virtualizer, onFilterChange, setLoadedRows, sortedTorrents.length]);

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, virtualizer])

  // Recalculate virtualized row sizes when view mode changes
  useEffect(() => {
    virtualizer.measure()
  }, [desktopViewMode, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  // Memoize minTableWidth to avoid recalculation on every row render
  const minTableWidth = useMemo(() => {
    return table.getVisibleLeafColumns().reduce((width, col) => {
      return width + col.getSize()
    }, 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [table, columnVisibility])

  // Reset loaded rows when data changes significantly
  useEffect(() => {
    // Always ensure loadedRows is at least 100 (or total length if less)
    const targetRows = Math.min(100, sortedTorrents.length)

    setLoadedRows(prev => {
      if (sortedTorrents.length === 0) {
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
  }, [sortedTorrents.length, virtualizer])

  // Reset when filters or search changes
  useEffect(() => {
    // Only reset loadedRows for user-initiated changes, not data updates
    const isRecentUserAction = lastUserAction && (Date.now() - lastUserAction.timestamp < 1000)

    if (isRecentUserAction) {
      const targetRows = Math.min(100, sortedTorrents.length || 0)
      setLoadedRows(targetRows)
      setIsLoadingMoreRows(false)

      // Clear selection state when data changes
      resetSelectionState() // Reset anchor on filter/search change

      // User-initiated change: scroll to top
      if (parentRef.current) {
        parentRef.current.scrollTop = 0
        setTimeout(() => {
          virtualizer.scrollToOffset(0)
          virtualizer.measure()
        }, 0)
      }
    } else {
      // Data update only: just remeasure without resetting loadedRows
      setTimeout(() => {
        virtualizer.measure()
      }, 0)
    }
  }, [filters, effectiveSearch, instanceId, virtualizer, sortedTorrents.length, lastUserAction, resetSelectionState])

  // Set up keyboard navigation (PageUp/Down, Home/End)
  useKeyboardNavigation({
    parentRef,
    virtualizer,
    safeLoadedRows,
    hasLoadedAll,
    isLoadingMore,
    loadMore,
    estimatedRowHeight,
  })

  // Apply Ctrl/Cmd+A shortcut to select all torrents
  const selectAllWithShortcut = useCallback(() => {
    if (sortedTorrents.length === 0) {
      return
    }

    setIsAllSelected(true)
    setExcludedFromSelectAll(new Set())
    setRowSelection({})
    lastSelectedIndexRef.current = null
  }, [sortedTorrents.length, setIsAllSelected, setExcludedFromSelectAll, setRowSelection])

  // Wrapper functions to adapt hook handlers to component needs
  const selectAllOptions = useMemo(() => ({
    selectAll: isAllSelected,
    filters: selectAllFilters,
    search: isAllSelected ? effectiveSearch : undefined,
    excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
  }), [isAllSelected, selectAllFilters, effectiveSearch, excludedFromSelectAll])

  const contextClientMeta = useMemo(() => ({
    clientHashes: contextHashes,
    totalSelected: isAllSelected ? effectiveSelectionCount : contextHashes.length,
  }), [contextHashes, isAllSelected, effectiveSelectionCount])

  const runAction = useCallback((action: (typeof TORRENT_ACTIONS)[keyof typeof TORRENT_ACTIONS], hashes: string[], extra?: Parameters<typeof handleAction>[2]) => {
    const clientHashes = hashes.length > 0 ? hashes : selectedHashes
    const clientCount = isAllSelected ? effectiveSelectionCount : (clientHashes.length || hashes.length || 1)
    handleAction(action, isAllSelected ? [] : hashes, {
      ...selectAllOptions,
      clientHashes,
      clientCount,
      ...extra,
    })
  }, [handleAction, isAllSelected, selectAllOptions, selectedHashes, effectiveSelectionCount])

  const handleExportWrapper = useCallback((hashes: string[], torrentsForSelection: Torrent[]) => {
    exportTorrents({
      hashes,
      torrents: torrentsForSelection,
      isAllSelected,
      totalSelected: effectiveSelectionCount,
      filters: selectAllFilters ?? filters,
      search: effectiveSearch,
      excludeHashes: Array.from(excludedFromSelectAll),
      sortField: activeSortField,
      sortOrder: activeSortOrder,
    })
  }, [
    exportTorrents,
    isAllSelected,
    effectiveSelectionCount,
    selectAllFilters,
    filters,
    effectiveSearch,
    excludedFromSelectAll,
    activeSortField,
    activeSortOrder,
  ])

  const handleDeleteWrapper = useCallback(() => {
    // Include cross-seed hashes if user opted to delete them
    const hashesToDelete = deleteCrossSeeds
      ? [...contextHashes, ...crossSeedWarning.affectedTorrents.map(t => t.hash)]
      : contextHashes

    // Update count to include cross-seeds for accurate toast message
    const deleteClientMeta = deleteCrossSeeds
      ? { clientHashes: hashesToDelete, totalSelected: hashesToDelete.length }
      : contextClientMeta

    handleDelete(
      hashesToDelete,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      deleteClientMeta
    )
  }, [handleDelete, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta, deleteCrossSeeds, crossSeedWarning.affectedTorrents])

  const handleAddTagsWrapper = useCallback((tags: string[]) => {
    handleAddTags(
      tags,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleAddTags, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleSetTagsWrapper = useCallback((tags: string[]) => {
    handleSetTags(
      tags,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleSetTags, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    handleSetCategory(
      category,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleSetCategory, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  // Direct category handler for context menu submenu
  const handleSetCategoryDirect = useCallback((category: string, hashes: string[]) => {
    const usingSelectAll = isAllSelected
    const resolvedFilters = usingSelectAll ? (selectAllFilters ?? filters) : undefined
    const resolvedSearch = usingSelectAll ? effectiveSearch : undefined
    const resolvedExclusions = usingSelectAll ? Array.from(excludedFromSelectAll) : undefined
    const clientHashes = hashes.length > 0 ? hashes : selectedHashes
    const totalSelected = usingSelectAll ? effectiveSelectionCount : (clientHashes.length || 1)

    handleSetCategory(
      category,
      usingSelectAll ? [] : hashes,
      usingSelectAll,
      resolvedFilters,
      resolvedSearch,
      resolvedExclusions,
      {
        clientHashes,
        totalSelected,
      }
    )
  }, [
    handleSetCategory,
    isAllSelected,
    selectAllFilters,
    filters,
    effectiveSearch,
    excludedFromSelectAll,
    selectedHashes,
    effectiveSelectionCount,
  ])

  const handleSetLocationWrapper = useCallback((location: string) => {
    handleSetLocation(
      location,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleSetLocation, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleRenameTorrentWrapper = useCallback(async (name: string) => {
    const hash = contextHashes[0]
    if (!hash) return
    await handleRenameTorrent(hash, name)
  }, [handleRenameTorrent, contextHashes])

  const handleRenameFileWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    const hash = contextHashes[0]
    if (!hash) return
    if (!oldPath || !newPath) return
    await handleRenameFile(hash, oldPath, newPath)
  }, [handleRenameFile, contextHashes])

  const handleRenameFolderWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    const hash = contextHashes[0]
    if (!hash) return
    if (!oldPath || !newPath) return
    await handleRenameFolder(hash, oldPath, newPath)
  }, [handleRenameFolder, contextHashes])

  const handleRemoveTagsWrapper = useCallback((tags: string[]) => {
    handleRemoveTags(
      tags,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleRemoveTags, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleRecheckWrapper = useCallback(() => {
    handleRecheck(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleRecheck, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleReannounceWrapper = useCallback(() => {
    handleReannounce(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleReannounce, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleTmmConfirmWrapper = useCallback(() => {
    handleTmmConfirm(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleTmmConfirm, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleSetShareLimitWrapper = useCallback((
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number
  ) => {
    handleSetShareLimit(
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleSetShareLimit, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleSetSpeedLimitsWrapper = useCallback((
    uploadLimit: number,
    downloadLimit: number
  ) => {
    handleSetSpeedLimits(
      uploadLimit,
      downloadLimit,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll),
      contextClientMeta
    )
  }, [handleSetSpeedLimits, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, excludedFromSelectAll, contextClientMeta])

  const handleDropPayload = useCallback((payload: AddTorrentDropPayload) => {
    setDropPayload(payload)
    onAddTorrentModalChange?.(true)
  }, [onAddTorrentModalChange])

  const handleDropPayloadConsumed = useCallback(() => {
    setDropPayload(null)
  }, [])


  // Drag and drop setup
  // Sensors must be called at the top level, not inside useMemo
  const sensors = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(TouchSensor, {
      activationConstraint: {
        delay: 250,
        tolerance: 5,
      },
    })
  )

  return (
    <>
      <SelectAllHotkey
        onSelectAll={selectAllWithShortcut}
        isMac={isMac}
        enabled={sortedTorrents.length > 0}
      />
      <div className="relative h-full flex flex-col">
        {/* Search and Actions */}
        <div className="flex flex-col gap-2 flex-shrink-0">
          {/* Search bar row */}
          <div className="flex items-center gap-1 sm:gap-2">
            {/* Action buttons - now handled by Management Bar in Header */}
            <div className="flex gap-1 sm:gap-2 flex-shrink-0">

              {/* Column controls next to search via portal, with inline fallback */}
              {(() => {
                const container = typeof document !== "undefined" ? document.getElementById("header-search-actions") : null
                const actions = (
                  <>
                    {desktopViewMode === "compact" && compactSortOptions.length > 0 && (
                      <div className="flex items-center">
                        <DropdownMenu>
                          <Tooltip disableHoverableContent={true}>
                            <TooltipTrigger
                              asChild
                              onFocus={(e) => {
                                e.preventDefault()
                              }}
                            >
                              <DropdownMenuTrigger asChild>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  className="h-8 px-2 text-xs font-medium gap-1"
                                >
                                  <ArrowUpDown className="h-3.5 w-3.5" />
                                  <span className="truncate">{currentCompactSortLabel}</span>
                                </Button>
                              </DropdownMenuTrigger>
                            </TooltipTrigger>
                            <TooltipContent>Change sort field</TooltipContent>
                          </Tooltip>
                          <DropdownMenuContent align="end" className="w-56 max-h-72 overflow-y-auto">
                            <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                            <DropdownMenuSeparator />
                            <DropdownMenuRadioGroup
                              value={activeSortField}
                              onValueChange={(value) => handleCompactSortFieldChange(value as TorrentSortOptionValue)}
                            >
                              {compactSortOptions.map(option => (
                                <DropdownMenuRadioItem key={option.value} value={option.value} className="text-sm">
                                  {option.label}
                                </DropdownMenuRadioItem>
                              ))}
                            </DropdownMenuRadioGroup>
                          </DropdownMenuContent>
                        </DropdownMenu>
                        <Tooltip disableHoverableContent={true}>
                          <TooltipTrigger
                            asChild
                            onFocus={(e) => {
                              e.preventDefault()
                            }}
                          >
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              onClick={handleCompactSortOrderToggle}
                              aria-label={`Sort ${activeSortOrder === "desc" ? "ascending" : "descending"}`}
                            >
                              {activeSortOrder === "desc" ? (
                                <ChevronDown className="h-4 w-4" />
                              ) : (
                                <ChevronUp className="h-4 w-4" />
                              )}
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>Sort {activeSortOrder === "desc" ? "ascending" : "descending"}</TooltipContent>
                        </Tooltip>
                      </div>
                    )}

                    {columnFilters.length > 0 && (
                      <Tooltip>
                        <TooltipTrigger
                          asChild
                          onFocus={(e) => {
                            // Prevent tooltip from showing on focus - only show on hover
                            e.preventDefault()
                          }}
                        >
                          <Button
                            variant="outline"
                            size="icon"
                            className="relative mr-1"
                            onClick={() => {
                              // Use atomic filter clearing to avoid race conditions
                              // Only clear column filters in cross-seed mode, clear all filters otherwise
                              const clearingMode = isCrossSeedFiltering ? 'columns-only' : 'all'
                              clearFiltersAtomically(clearingMode)
                            }}
                          >
                            <X className="h-4 w-4" />
                            <span className="sr-only">Clear all column filters</span>
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>Clear all column filters ({columnFilters.length})</TooltipContent>
                      </Tooltip>
                    )}

                    {desktopViewMode !== "compact" && (
                      <DropdownMenu>
                        <Tooltip disableHoverableContent={true}>
                          <TooltipTrigger
                            asChild
                            onFocus={(e) => {
                              // Prevent tooltip from showing on focus - only show on hover
                              e.preventDefault()
                            }}
                          >
                            <DropdownMenuTrigger asChild>
                              <Button
                                variant="outline"
                                size="icon"
                              >
                                <Columns3 className="h-4 w-4" />
                                <span className="sr-only">Toggle columns</span>
                              </Button>
                            </DropdownMenuTrigger>
                          </TooltipTrigger>
                          <TooltipContent>Toggle columns</TooltipContent>
                        </Tooltip>
                        <DropdownMenuContent align="end" className="w-48">
                          <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          {table
                            .getAllColumns()
                            .filter(
                              (column) =>
                                column.id !== "select" && // Never show select in visibility options
                                column.getCanHide()
                            )
                            .map((column) => {
                              return (
                                <DropdownMenuCheckboxItem
                                  key={column.id}
                                  className="capitalize"
                                  checked={column.getIsVisible()}
                                  onCheckedChange={(value) =>
                                    column.toggleVisibility(!!value)
                                  }
                                  onSelect={(e) => e.preventDefault()}
                                >
                                  <span className="truncate">
                                    {(column.columnDef.meta as { headerString?: string })?.headerString ||
                                      (typeof column.columnDef.header === "string" ? column.columnDef.header : column.id)}
                                  </span>
                                </DropdownMenuCheckboxItem>
                              )
                            })}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    )}
                  </>
                )

                return container ? createPortal(actions, container) : actions
              })()}

              <AddTorrentDialog
                instanceId={instanceId}
                open={addTorrentModalOpen}
                onOpenChange={onAddTorrentModalChange}
                dropPayload={dropPayload}
                onDropPayloadConsumed={handleDropPayloadConsumed}
                torrents={torrents}
              />
            </div>
          </div>
        </div>

        {/* Table container */}
        <div className="flex flex-col flex-1 min-h-0 mt-2 sm:mt-0 overflow-hidden">
          {/* Virtual scroll container with paint containment optimization for improved rendering performance */}
          <TorrentDropZone
            ref={parentRef}
            className="relative flex-1 overflow-auto scrollbar-thin select-none will-change-transform contain-paint"
            role="grid"
            aria-label="Torrents table"
            aria-rowcount={totalCount}
            aria-colcount={table.getVisibleLeafColumns().length}
            onDropPayload={handleDropPayload}
          >
            {/* Loading overlay - positioned absolute to scroll container */}
            {torrents.length === 0 && showLoadingState && (
              <div className="absolute inset-0 flex items-center justify-center bg-background/80 backdrop-blur-sm z-50 animate-in fade-in duration-300">
                <div className="text-center animate-in zoom-in-95 duration-300">
                  <Logo className="h-12 w-12 animate-pulse mx-auto mb-3" />
                  <p>Loading torrents...</p>
                </div>
              </div>
            )}
            {torrents.length === 0 && !isLoading && (
              <div
                className={cn(
                  "absolute inset-0 flex items-center justify-center z-40 animate-in fade-in duration-300",
                  !hasFilterControls && "pointer-events-none"
                )}
              >
                <div className="text-center animate-in zoom-in-95 duration-300 text-muted-foreground space-y-3">
                  <p>{emptyStateMessage}</p>
                  {hasFilterControls && (
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => clearFiltersAtomically("all")}
                    >
                      Clear filters
                    </Button>
                  )}
                </div>
              </div>
            )}

            <div style={{ position: "relative", minWidth: "min-content" }}>
              {/* Header - show in normal and dense table views */}
              {desktopViewMode !== "compact" && (
                <div className="sticky top-0 bg-background border-b" style={{ zIndex: 50 }}>
                  <DndContext
                    sensors={sensors}
                    collisionDetection={closestCenter}
                    onDragEnd={(event) => {
                      const { active, over } = event
                      if (!active || !over || active.id === over.id) {
                        return
                      }

                      setColumnOrder((currentOrder: string[]) => {
                        const allColumnIds = table.getAllLeafColumns().map((col) => col.id)

                        // Normalize current order to include all current columns exactly once
                        const sanitizedOrder = [
                          ...currentOrder.filter((id) => allColumnIds.includes(id)),
                          ...allColumnIds.filter((id) => !currentOrder.includes(id)),
                        ]

                        const oldIndex = sanitizedOrder.indexOf(active.id as string)
                        const newIndex = sanitizedOrder.indexOf(over.id as string)

                        if (oldIndex === -1 || newIndex === -1) {
                          return sanitizedOrder
                        }

                        return arrayMove(sanitizedOrder, oldIndex, newIndex)
                      })
                    }}
                    modifiers={[restrictToHorizontalAxis]}
                  >
                    {table.getHeaderGroups().map(headerGroup => {
                      const headers = headerGroup.headers
                      const headerIds = headers.map(h => h.column.id)

                      // Use memoized minTableWidth

                      return (
                        <SortableContext
                          key={headerGroup.id}
                          items={headerIds}
                          strategy={horizontalListSortingStrategy}
                        >
                          <div className="flex" style={{ minWidth: `${minTableWidth}px` }}>
                            {headers.map(header => (
                              <DraggableTableHeader
                                key={header.id}
                                header={header}
                                columnFilters={columnFilters}
                                viewMode={desktopViewMode}
                                onFilterChange={(columnId, filter) => {
                                  if (filter === null) {
                                    setColumnFilters(columnFilters.filter(f => f.columnId !== columnId))
                                  } else {
                                    const existing = columnFilters.findIndex(f => f.columnId === columnId)
                                    if (existing >= 0) {
                                      const newFilters = [...columnFilters]
                                      newFilters[existing] = filter
                                      setColumnFilters(newFilters)
                                    } else {
                                      setColumnFilters([...columnFilters, filter])
                                    }
                                  }
                                }}
                              />
                            ))}
                          </div>
                        </SortableContext>
                      )
                    })}
                  </DndContext>
                </div>
              )}

              {/* Body */}
              <div
                style={{
                  height: `${virtualizer.getTotalSize()}px`,
                  width: "100%",
                  position: "relative",
                }}
              >
                {virtualRows.map(virtualRow => {
                  const row = rows[virtualRow.index]
                  if (!row || !row.original) return null
                  const torrent = row.original
                  const isSelected = selectedTorrent?.hash === torrent.hash
                  const isRowSelected = isAllSelected ? !excludedFromSelectAll.has(torrent.hash) : row.getIsSelected()

                  // Render compact view for compact mode
                  if (desktopViewMode === "compact") {
                    return (
                      <TorrentContextMenu
                        key={row.id}
                        instanceId={instanceId}
                        torrent={torrent}
                        isSelected={isRowSelected}
                        isAllSelected={isAllSelected}
                        selectedHashes={selectedHashes}
                        selectedTorrents={selectedTorrents}
                        effectiveSelectionCount={effectiveSelectionCount}
                        onTorrentSelect={onTorrentSelect}
                        onAction={runAction}
                        onPrepareDelete={prepareDeleteAction}
                        onPrepareTags={prepareTagsAction}
                        onPrepareCategory={prepareCategoryAction}
                        onPrepareCreateCategory={prepareCreateCategoryAction}
                        onPrepareShareLimit={prepareShareLimitAction}
                        onPrepareSpeedLimits={prepareSpeedLimitAction}
                        onPrepareLocation={prepareLocationAction}
                        onPrepareRenameTorrent={prepareRenameTorrentAction}
                        onPrepareRenameFile={prepareRenameFileAction}
                        onPrepareRenameFolder={prepareRenameFolderAction}
                        onPrepareRecheck={prepareRecheckAction}
                        onPrepareReannounce={prepareReannounceAction}
                        onPrepareTmm={prepareTmmAction}
                        availableCategories={availableCategories}
                        onSetCategory={handleSetCategoryDirect}
                        isPending={isPending}
                        onExport={handleExportWrapper}
                        isExporting={isExportingTorrent}
                        capabilities={capabilities}
                        useSubcategories={allowSubcategories}
                        canCrossSeedSearch={canCrossSeedSearch}
                        onCrossSeedSearch={onCrossSeedSearch}
                        isCrossSeedSearching={isCrossSeedSearching}
                        onFilterChange={onFilterChange}
                      >
                        <CompactRow
                          torrent={torrent}
                          rowId={row.id}
                          rowIndex={virtualRow.index}
                          isSelected={isSelected}
                          isRowSelected={isRowSelected}
                          onClick={(e) => {
                            const target = e.target as HTMLElement
                            const isCheckboxElement = target.closest("[data-slot=\"checkbox\"]") || target.closest("[role=\"checkbox\"]")
                            if (isCheckboxElement) {
                              return
                            }
                            // Handle shift-click for range selection
                            if (e.shiftKey) {
                              e.preventDefault()
                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)
                              if (lastSelectedIndexRef.current !== null) {
                                const start = Math.min(lastSelectedIndexRef.current, currentIndex)
                                const end = Math.max(lastSelectedIndexRef.current, currentIndex)
                                for (let i = start; i <= end; i++) {
                                  const targetRow = allRows[i]
                                  if (targetRow) {
                                    handleRowSelection(targetRow.original.hash, true, targetRow.id)
                                  }
                                }
                              } else {
                                handleRowSelection(torrent.hash, true, row.id)
                                lastSelectedIndexRef.current = currentIndex
                              }
                            } else if (e.ctrlKey || e.metaKey) {
                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)
                              handleRowSelection(torrent.hash, !isRowSelected, row.id)
                              lastSelectedIndexRef.current = currentIndex
                            } else {
                              // Plain click - open details panel
                              // If row is already selected, keep selection intact
                              // Otherwise, select only this torrent (replace selection)
                              if (!isRowSelected) {
                                const allRows = table.getRowModel().rows
                                const currentIndex = allRows.findIndex(r => r.id === row.id)
                                setIsAllSelected(false)
                                setExcludedFromSelectAll(new Set())
                                setRowSelection({ [row.id]: true })
                                lastSelectedIndexRef.current = currentIndex
                              }
                              onTorrentSelect?.(torrent)
                            }
                          }}
                          onContextMenu={() => {
                            if (!isRowSelected && selectedHashes.length <= 1) {
                              setRowSelection({ [row.id]: true })
                            }
                          }}
                          incognitoMode={incognitoMode}
                          speedUnit={speedUnit}
                          supportsTrackerHealth={supportsTrackerHealth}
                          trackerIcons={trackerIcons}
                          onCheckboxPointerDown={handleCompactCheckboxPointerDown}
                          onCheckboxChange={handleCompactCheckboxChange}
                          style={{
                            position: "absolute",
                            top: 0,
                            left: 0,
                            width: "100%",
                            height: `${virtualRow.size}px`,
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                        />
                      </TorrentContextMenu>
                    )
                  }

                  // Use memoized minTableWidth for normal table view
                  return (
                    <TorrentContextMenu
                      key={row.id}
                      instanceId={instanceId}
                      torrent={torrent}
                      isSelected={isRowSelected}
                      isAllSelected={isAllSelected}
                      selectedHashes={selectedHashes}
                      selectedTorrents={selectedTorrents}
                      effectiveSelectionCount={effectiveSelectionCount}
                      onTorrentSelect={onTorrentSelect}
                      onAction={runAction}
                      onPrepareDelete={prepareDeleteAction}
                      onPrepareTags={prepareTagsAction}
                      onPrepareCategory={prepareCategoryAction}
                      onPrepareCreateCategory={prepareCreateCategoryAction}
                      onPrepareShareLimit={prepareShareLimitAction}
                      onPrepareSpeedLimits={prepareSpeedLimitAction}
                      onPrepareLocation={prepareLocationAction}
                      onPrepareRenameTorrent={prepareRenameTorrentAction}
                      onPrepareRenameFile={prepareRenameFileAction}
                      onPrepareRenameFolder={prepareRenameFolderAction}
                      onPrepareRecheck={prepareRecheckAction}
                      onPrepareReannounce={prepareReannounceAction}
                      onPrepareTmm={prepareTmmAction}
                      availableCategories={availableCategories}
                      onSetCategory={handleSetCategoryDirect}
                      isPending={isPending}
                      onExport={handleExportWrapper}
                      isExporting={isExportingTorrent}
                      capabilities={capabilities}
                      useSubcategories={allowSubcategories}
                      canCrossSeedSearch={canCrossSeedSearch}
                      onCrossSeedSearch={onCrossSeedSearch}
                      isCrossSeedSearching={isCrossSeedSearching}
                      onFilterChange={onFilterChange}
                    >
                      <div
                        className={`flex cursor-pointer hover:bg-accent/40 ${getRowBackgroundClass(isRowSelected, isSelected, virtualRow.index)}`}
                        style={{
                          position: "absolute",
                          top: 0,
                          left: 0,
                          minWidth: `${minTableWidth}px`,
                          height: `${virtualRow.size}px`,
                          transform: `translateY(${virtualRow.start}px)`,
                        }}
                        onClick={(e) => {
                          // Don't select when clicking checkbox or its wrapper
                          const target = e.target as HTMLElement
                          const isCheckbox = target.closest("[data-slot=\"checkbox\"]") || target.closest("[role=\"checkbox\"]") || target.closest(".p-1.-m-1")
                          if (!isCheckbox) {
                            // Handle shift-click for range selection - EXACTLY like checkbox
                            if (e.shiftKey) {
                              e.preventDefault() // Prevent text selection

                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)

                              if (lastSelectedIndexRef.current !== null) {
                                const start = Math.min(lastSelectedIndexRef.current, currentIndex)
                                const end = Math.max(lastSelectedIndexRef.current, currentIndex)

                                // Select range EXACTLY like checkbox does
                                for (let i = start; i <= end; i++) {
                                  const targetRow = allRows[i]
                                  if (targetRow) {
                                    handleRowSelection(targetRow.original.hash, true, targetRow.id)
                                  }
                                }
                              } else {
                                // No anchor - just select this row
                                handleRowSelection(torrent.hash, true, row.id)
                                lastSelectedIndexRef.current = currentIndex
                              }

                              // Don't update lastSelectedIndexRef on shift-click (keeps anchor stable)
                            } else if (e.ctrlKey || e.metaKey) {
                              // Ctrl/Cmd click - toggle single row EXACTLY like checkbox
                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)

                              handleRowSelection(torrent.hash, !isRowSelected, row.id)
                              lastSelectedIndexRef.current = currentIndex
                            } else {
                              // Plain click - open details panel
                              // If row is already selected, keep selection intact
                              // Otherwise, select only this torrent (replace selection)
                              if (!isRowSelected) {
                                const allRows = table.getRowModel().rows
                                const currentIndex = allRows.findIndex(r => r.id === row.id)
                                setIsAllSelected(false)
                                setExcludedFromSelectAll(new Set())
                                setRowSelection({ [row.id]: true })
                                lastSelectedIndexRef.current = currentIndex
                              }
                              onTorrentSelect?.(torrent)
                            }
                          }
                        }}
                        onContextMenu={() => {
                          // Only select this row if not already selected and not part of a multi-selection
                          if (!isRowSelected && selectedHashes.length <= 1) {
                            setRowSelection({ [row.id]: true })
                          }
                        }}
                      >
                        {row.getVisibleCells().map(cell => {
                          // Compact columns (tracker_icon, status_icon) use px-0 to match header
                          const isCompactColumn = cell.column.id === "tracker_icon" || cell.column.id === "status_icon"
                          const isSelectColumn = cell.column.id === "select"
                          return (
                            <div
                              key={cell.id}
                              style={{
                                width: cell.column.getSize(),
                                flexShrink: 0,
                              }}
                              className={cn(
                                "flex items-center overflow-hidden min-w-0",
                                // Select and compact columns are centered to match header
                                (isSelectColumn || isCompactColumn) && "justify-center",
                                isCompactColumn
                                  ? (desktopViewMode === "dense" ? "px-0 py-0.5" : "px-0 py-2")
                                  : (desktopViewMode === "dense" ? "px-2 py-0.5" : "px-3 py-2")
                              )}
                            >
                              {flexRender(
                                cell.column.columnDef.cell,
                                cell.getContext()
                              )}
                            </div>
                          )
                        })}
                      </div>
                    </TorrentContextMenu>
                  )
                })}
              </div>
            </div>
          </TorrentDropZone>
        </div>

        <DeleteTorrentDialog
          open={showDeleteDialog}
          onOpenChange={(open) => {
            if (!open) {
              closeDeleteDialog()
              crossSeedWarning.reset()
            }
          }}
          count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          totalSize={deleteDialogTotalSize}
          formattedSize={deleteDialogFormattedSize}
          deleteFiles={deleteFiles}
          onDeleteFilesChange={setDeleteFiles}
          isDeleteFilesLocked={isDeleteFilesLocked}
          onToggleDeleteFilesLock={toggleDeleteFilesLock}
          deleteCrossSeeds={deleteCrossSeeds}
          onDeleteCrossSeedsChange={setDeleteCrossSeeds}
          crossSeedWarning={crossSeedWarning}
          onConfirm={handleDeleteWrapper}
        />

        {/* Add Tags Dialog */}
        <AddTagsDialog
          open={showAddTagsDialog}
          onOpenChange={setShowAddTagsDialog}
          availableTags={availableTags || []}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleAddTagsWrapper}
          isPending={isPending}
          isLoadingTags={isLoadingTags}
        />

        {/* Set Tags Dialog */}
        <SetTagsDialog
          open={showSetTagsDialog}
          onOpenChange={setShowSetTagsDialog}
          availableTags={availableTags || []}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleSetTagsWrapper}
          isPending={isPending}
          initialTags={getCommonTags(contextTorrents)}
          isLoadingTags={isLoadingTags}
        />

        {/* Set Category Dialog */}
        <SetCategoryDialog
          open={showCategoryDialog}
          onOpenChange={setShowCategoryDialog}
          availableCategories={availableCategories || {}}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleSetCategoryWrapper}
          isPending={isPending}
          initialCategory={getCommonCategory(contextTorrents)}
          isLoadingCategories={isLoadingCategories}
          useSubcategories={allowSubcategories}
        />

        {/* Create and Assign Category Dialog */}
        <CreateAndAssignCategoryDialog
          open={showCreateCategoryDialog}
          onOpenChange={setShowCreateCategoryDialog}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleSetCategoryWrapper}
          isPending={isPending}
        />

        <ShareLimitDialog
          open={showShareLimitDialog}
          onOpenChange={setShowShareLimitDialog}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          torrents={contextTorrents}
          onConfirm={handleSetShareLimitWrapper}
          isPending={isPending}
        />

        <SpeedLimitsDialog
          open={showSpeedLimitDialog}
          onOpenChange={setShowSpeedLimitDialog}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          torrents={contextTorrents}
          onConfirm={handleSetSpeedLimitsWrapper}
          isPending={isPending}
        />

        {/* Set Location Dialog */}
        <SetLocationDialog
          open={showLocationDialog}
          onOpenChange={setShowLocationDialog}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleSetLocationWrapper}
          isPending={isPending}
          initialLocation={getCommonSavePath(contextTorrents)}
        />

        {/* Rename dialogs */}
        <RenameTorrentDialog
          open={showRenameTorrentDialog}
          onOpenChange={setShowRenameTorrentDialog}
          currentName={contextTorrents[0]?.name}
          onConfirm={handleRenameTorrentWrapper}
          isPending={isPending}
        />
        <RenameTorrentFileDialog
          open={showRenameFileDialog}
          onOpenChange={setShowRenameFileDialog}
          files={renameFileEntries}
          isLoading={renameEntriesLoading}
          onConfirm={handleRenameFileWrapper}
          isPending={isPending}
        />
        <RenameTorrentFolderDialog
          open={showRenameFolderDialog}
          onOpenChange={setShowRenameFolderDialog}
          folders={renameFolderEntries}
          isLoading={renameEntriesLoading}
          onConfirm={handleRenameFolderWrapper}
          isPending={isPending}
        />

        {/* Remove Tags Dialog */}
        <RemoveTagsDialog
          open={showRemoveTagsDialog}
          onOpenChange={setShowRemoveTagsDialog}
          availableTags={availableTags || []}
          hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={handleRemoveTagsWrapper}
          isPending={isPending}
          currentTags={getCommonTags(contextTorrents)}
        />

        {/* Force Recheck Confirmation Dialog */}
        <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Force Recheck {isAllSelected ? effectiveSelectionCount : contextHashes.length} torrent(s)?</DialogTitle>
              <DialogDescription>
                This will force qBittorrent to recheck all pieces of the selected torrents. This process may take some time and will temporarily pause the torrents.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
                Cancel
              </Button>
              <Button onClick={handleRecheckWrapper} disabled={isPending}>
                Force Recheck
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* Reannounce Confirmation Dialog */}
        <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Reannounce {isAllSelected ? effectiveSelectionCount : contextHashes.length} torrent(s)?</DialogTitle>
              <DialogDescription>
                This will force the selected torrents to reannounce to all their trackers. This is useful when trackers are not responding or you want to refresh your connection.
              </DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
                Cancel
              </Button>
              <Button onClick={handleReannounceWrapper} disabled={isPending}>
                Reannounce
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        {/* TMM Confirmation Dialog */}
        <TmmConfirmDialog
          open={showTmmDialog}
          onOpenChange={setShowTmmDialog}
          count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          enable={pendingTmmEnable}
          onConfirm={handleTmmConfirmWrapper}
          isPending={isPending}
        />

        {/* Location Warning Dialog */}
        <LocationWarningDialog
          open={showLocationWarningDialog}
          onOpenChange={setShowLocationWarningDialog}
          count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
          onConfirm={proceedToLocationDialog}
          isPending={isPending}
        />

        {/* Instance Preferences Dialog */}
        {instance && (
          <InstancePreferencesDialog
            open={preferencesOpen}
            onOpenChange={setPreferencesOpen}
            instanceId={instanceId}
            instanceName={instance.name}
          />
        )}

        {/* Scroll to top button*/}
        <div className="hidden lg:block">
          <ScrollToTopButton
            scrollContainerRef={parentRef}
            className="bottom-4 right-6"
          />
        </div>
      </div>
    </>
  )
});
