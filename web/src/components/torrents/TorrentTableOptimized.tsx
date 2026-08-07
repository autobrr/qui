/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { isClientConnectionErrorCode } from "@/contexts/SyncStreamContext"
import { useBulkActionWrappers } from "@/hooks/torrent-table/useBulkActionWrappers"
import { useColumnDnd } from "@/hooks/torrent-table/useColumnDnd"
import { useCompactViewSort } from "@/hooks/torrent-table/useCompactViewSort"
import { useCrossSeedOrchestration } from "@/hooks/torrent-table/useCrossSeedOrchestration"
import { useEffectiveServerState } from "@/hooks/torrent-table/useEffectiveServerState"
import { useFilterLifecycle } from "@/hooks/torrent-table/useFilterLifecycle"
import { useTorrentSelection } from "@/hooks/torrent-table/useTorrentSelection"
import { useTorrentSelectionDerivations } from "@/hooks/torrent-table/useTorrentSelectionDerivations"
import { useTorrentTableArrowNavigation } from "@/hooks/torrent-table/useTorrentTableArrowNavigation"
import { useTorrentTableColumns } from "@/hooks/torrent-table/useTorrentTableColumns"
import { useTorrentTableFilterExpr } from "@/hooks/torrent-table/useTorrentTableFilterExpr"
import { useTorrentTableNotifications } from "@/hooks/torrent-table/useTorrentTableNotifications"
import { useTorrentTableHotkeys } from "@/hooks/torrent-table/useTorrentTableHotkeys"
import { useTorrentTableVirtualization } from "@/hooks/torrent-table/useTorrentTableVirtualization"
import { useTrackerIconCache } from "@/hooks/torrent-table/useTrackerIconCache"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useDebounce } from "@/hooks/useDebounce"
import { useDelayedVisibility } from "@/hooks/useDelayedVisibility"
import { usePersistedColumnFilters } from "@/hooks/usePersistedColumnFilters"
import { usePersistedColumnSizing } from "@/hooks/usePersistedColumnSizing"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { usePersistedColumnVisibility } from "@/hooks/usePersistedColumnVisibility"
import { usePersistedCompactViewState } from "@/hooks/usePersistedCompactViewState"
import { TORRENT_ACTIONS, useTorrentActions } from "@/hooks/useTorrentActions"
import { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { TORRENT_STREAM_POLL_INTERVAL_SECONDS, useTorrentsList } from "@/hooks/useTorrentsList"
import { getBackendSortField } from "@/lib/torrent-table/backend-sort-field"
import { getRowBackgroundClass } from "@/lib/torrent-table/row-display"
import { resolveTrackerHealthSupport } from "@/lib/tracker-health-support"
import { formatBytes } from "@/lib/utils"
import {
  flexRender,
  getCoreRowModel,
  getFilteredRowModel,
  getSortedRowModel,
  useReactTable
} from "@tanstack/react-table"
import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react"
import { createPortal } from "react-dom"
import { useTranslation } from "react-i18next"
import { InstancePreferencesDialog } from "../instances/preferences/InstancePreferencesDialog"
import { TorrentContextMenu } from "./TorrentContextMenu"
import { type TorrentSortOptionValue } from "./torrentSortOptions"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { useIncognitoMode } from "@/lib/incognito"
import { isAllInstancesScope } from "@/lib/instances"
import { resolveFooterSpeeds } from "@/lib/scoped-speeds"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { resolveStreamFallbackStatus } from "@/lib/stream-status"
import { cn } from "@/lib/utils"
import type {
  Category,
  CrossInstanceTorrent,
  Torrent,
  TorrentCounts,
  TorrentFilters
} from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useNavigate } from "@tanstack/react-router"
import {
  ArrowUpDown,
  Ban,
  BrickWallFire,
  ChevronDown,
  ChevronUp,
  Columns3,
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
  X
} from "lucide-react"
import { AddTorrentDialog, type AddTorrentDropPayload } from "./AddTorrentDialog"
import { SelectAllHotkey } from "./SelectAllHotkey"
import { TorrentDropZone } from "./TorrentDropZone"
import { createColumns } from "./TorrentTableColumns"
import { CompactRow } from "./table/CompactRow"
import { TableColumnHeader } from "./table/TableColumnHeader"
import { TorrentTableDialogs } from "./table/TorrentTableDialogs"

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
  instance: true,
}
const DEFAULT_COLUMN_SIZING = {}
const STREAM_STATUS_TRANSITION_DELAY_MS = 800

type StreamPhase = "connecting" | "healthy" | "reconnecting" | "fallback"

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

interface ExternalIPAddressProps {
  address?: string | null
  incognitoMode: boolean
  label: string
}

const ExternalIPAddress = memo(
  ({ address, incognitoMode, label }: ExternalIPAddressProps) => {
    const { t } = useTranslation("torrents")

    if (!address) return null

    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <Badge
            variant="outline"
            className="gap-1 px-1.5 py-0.5 text-[11px] leading-none text-muted-foreground"
            aria-label={`${t("statusBar.external")} ${label}`}
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

interface TorrentTableOptimizedProps {
  instanceId: number
  instanceIds?: number[]
  readOnly?: boolean
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
    useSubcategories?: boolean,
    supportsTrackerHealth?: boolean
  ) => void
  onSelectionChange?: (
    selectedHashes: string[],
    selectedTorrents: Torrent[],
    isAllSelected: boolean,
    totalSelectionCount: number,
    excludeHashes: string[],
    excludeTargets: Array<{ instanceId: number; hash: string }>,
    selectedTotalSize: number,
    selectionFilters?: TorrentFilters
  ) => void
  onResetSelection?: (handler?: () => void) => void
  onFilterChange?: (filters: TorrentFilters) => void
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
}

export const TorrentTableOptimized = memo(function TorrentTableOptimized({
  instanceId,
  instanceIds,
  readOnly = false,
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
}: TorrentTableOptimizedProps) {
  const isReadOnly = readOnly
  const isUnifiedView = isAllInstancesScope(instanceId)
  // State management
  // Move default values outside the component for stable references
  // (This should be at module scope, not inside the component)
  const [sorting, setSorting] = usePersistedColumnSorting([], instanceId)
  const [dropPayload, setDropPayload] = useState<AddTorrentDropPayload | null>(null)

  // Instance preferences dialog state
  const [preferencesOpen, setPreferencesOpen] = useState(false)

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const { t } = useTranslation("torrents")
  const [statusBarContainer, setStatusBarContainer] = useState<HTMLElement | null>(null)
  useLayoutEffect(() => {
    setStatusBarContainer(document.getElementById("qui-status-bar-container"))
  }, [])
  const { exportTorrents, isExporting: isExportingTorrent } = useTorrentExporter({ instanceId, incognitoMode })
  const [speedUnit, setSpeedUnit] = useSpeedUnits()
  const { formatTimestamp } = useDateTimeFormatters()
  const { preferences } = useInstancePreferences(instanceId, { fetchIfMissing: false, enabled: instanceId > 0 })
  const { instances } = useInstances()
  const instance = useMemo(() => instances?.find(i => i.id === instanceId), [instances, instanceId])

  // Desktop view mode state (separate from mobile view mode)
  const { viewMode: desktopViewMode, cycleViewMode } = usePersistedCompactViewState("normal", TABLE_ALLOWED_VIEW_MODES)

  const { trackerIcons, trackerCustomizationLookup } = useTrackerIconCache()

  // These should be defined at module scope, not inside the component, to ensure stable references
  // (If not already, move them to the top of the file)
  // const DEFAULT_COLUMN_VISIBILITY, DEFAULT_COLUMN_ORDER, DEFAULT_COLUMN_SIZING

  // Column visibility with persistence
  const [columnVisibility, setColumnVisibility] = usePersistedColumnVisibility(DEFAULT_COLUMN_VISIBILITY, instanceId)
  // Column order with persistence (get default order at runtime to avoid initialization order issues)
  // Latest accessor for the table's leaf column ids — reassigned after the table
  // is created below; useColumnDnd reads it lazily at drag time.
  const leafColumnIdsRef = useRef<() => string[]>(() => [])
  const getLeafColumnIds = useCallback(() => leafColumnIdsRef.current(), [])
  const { columnOrder, setColumnOrder, sensors, onDragEnd } = useColumnDnd({
    instanceId,
    defaultColumnOrder: getDefaultColumnOrder(),
    getLeafColumnIds,
  })
  // Column sizing with persistence
  const [columnSizing, setColumnSizing] = usePersistedColumnSizing(DEFAULT_COLUMN_SIZING, instanceId)
  // Column filters with persistence
  const [columnFilters, setColumnFilters] = usePersistedColumnFilters(instanceId)

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
    blockCrossSeeds,
    setBlockCrossSeeds,
    deleteCrossSeeds,
    setDeleteCrossSeeds,
    showTagsDialog,
    setShowTagsDialog,
    showCommentDialog,
    setShowCommentDialog,
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
    handleUpdateTags,
    handleSetComment,
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
    prepareCommentAction,
    prepareCategoryAction,
    prepareCreateCategoryAction,
    prepareShareLimitAction,
    prepareSpeedLimitAction,
    prepareLocationAction,
    prepareRenameTorrentAction,
    prepareRecheckAction,
    prepareReannounceAction,
    prepareTmmAction,
  } = useTorrentActions({
    instanceId,
    instanceIds,
    onActionComplete: (action) => {
      if (action === TORRENT_ACTIONS.DELETE) {
        resetSelectionState()
      }
    },
  })

  // Cross-seed warning for delete dialog
  const { crossSeedWarning, hasCrossSeedTag, shouldBlockCrossSeeds, blockCrossSeedHashes } = useCrossSeedOrchestration({
    instanceId,
    instanceName: instance?.name ?? "",
    contextTorrents,
    blockCrossSeeds,
  })

  // Fetch metadata using shared hook
  const { data: metadata, isLoading: isMetadataLoading } = useInstanceMetadata(instanceId, {
    fallbackDelayMs: 1500,
  })
  const metadataTags = metadata?.tags || []
  const metadataCategories = metadata?.categories || {}

  const navigate = useNavigate()

  const {
    globalFilter,
    setGlobalFilter,
    effectiveSearch,
    columnFiltersExpr,
    combinedFiltersExpr,
    lastUserAction,
    setLastUserAction,
  } = useTorrentTableFilterExpr({ filters, instanceId, columnFilters })

  const activeSortField = sorting.length > 0 ? getBackendSortField(sorting[0].id) : "added_on"
  const activeSortOrder: "asc" | "desc" = sorting.length > 0 ? (sorting[0].desc ? "desc" : "asc") : "desc"
  const isAllInstancesView = instanceId <= 0

  const effectiveIncludedCategories = filters?.expandedCategories ?? filters?.categories ?? []
  const effectiveExcludedCategories = filters?.expandedExcludeCategories ?? filters?.excludeCategories ?? []

  const { isHiddenDelayed, isVisible } = useDelayedVisibility(3000)
  const isVisibilitySettled = isHiddenDelayed || isVisible

  // Fetch torrents data with backend sorting
  const {
    torrents,
    totalCount,
    stats,
    counts,
    categories,
    tags,
    trackerHealthSupported,
    serverState,
    capabilities,
    useSubcategories: subcategoriesFromData,
    isLoading,
    isCachedData,
    isStaleData,
    isLoadingMore,
    hasLoadedAll,
    loadMore: backendLoadMore,
    streamConnected,
    streamMeta,
    isStreaming,
    streamError,
    streamRetrying,
    streamNextRetryAt,
    streamRetryAttempt,
    isCrossSeedFiltering,
    isCrossInstanceEndpoint,
  } = useTorrentsList(instanceId, {
    enabled: true,
    pollingEnabled: isVisibilitySettled,
    instanceIds,
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

  // When the stream drops but rows are already on screen, the data is stale but
  // usable, so the fallback banner degrades to amber instead of a red error.
  const hasTorrentRows = torrents.length > 0

  const derivedStreamPhase = useMemo<StreamPhase>(() => {
    if (streamRetrying || typeof streamNextRetryAt === "number") {
      return "reconnecting"
    }
    if (streamError) {
      return "fallback"
    }
    if (isStreaming) {
      return "healthy"
    }
    return "connecting"
  }, [isStreaming, streamError, streamNextRetryAt, streamRetrying])

  const stableStreamPhase = useDebounce(
    derivedStreamPhase,
    derivedStreamPhase === "healthy" || derivedStreamPhase === "fallback" ? 0 : STREAM_STATUS_TRANSITION_DELAY_MS
  )

  const streamStatus = useMemo(() => {
    if (isCrossSeedFiltering) {
      return {
        label: t("statusBar.streamStatus.crossInstance.label"),
        message: t("statusBar.streamStatus.crossInstance.message"),
        secondary: t("statusBar.streamStatus.crossInstance.secondary", { seconds: 10 }),
        tone: "muted" as const,
        animate: false,
      }
    }

    const serverRetrySeconds =
      typeof streamMeta?.retryInSeconds === "number" && streamMeta.retryInSeconds > 0 ? streamMeta.retryInSeconds : null
    const safeRetryAttempt =
      typeof streamRetryAttempt === "number" && streamRetryAttempt > 0 ? streamRetryAttempt : 1
    const hasClientRetryScheduled = typeof streamNextRetryAt === "number"

    // Client-side connection-state codes carry no displayable text: render the
    // localized streamStatus.* message for them. Only genuine backend payload
    // errors (dynamic server text we cannot translate) are shown verbatim.
    const backendStreamError = streamError && !isClientConnectionErrorCode(streamError) ? streamError : null

    switch (stableStreamPhase) {
      case "reconnecting":
        return {
          label: t("statusBar.streamStatus.reconnecting.label"),
          message: backendStreamError ?? t("statusBar.streamStatus.reconnecting.message"),
          secondary: hasClientRetryScheduled ? t("statusBar.streamStatus.reconnecting.retryQueued", { attempt: safeRetryAttempt }) : t("statusBar.streamStatus.reconnecting.pollingContinues"),
          tone: "warning" as const,
          animate: true,
        }
      case "fallback": {
        const plan = resolveStreamFallbackStatus({
          hasData: hasTorrentRows,
          hasLastSuccessfulSync: Boolean(streamMeta?.lastSuccessfulSync),
        })
        const retrySecondary = serverRetrySeconds && serverRetrySeconds > 0 ? t("statusBar.streamStatus.fallback.serverRetry", { seconds: serverRetrySeconds }) : t("statusBar.streamStatus.fallback.retrying")

        if (plan.variant === "degraded") {
          const staleAge = plan.showStaleAge ? formatRelativeTime(streamMeta?.lastSuccessfulSync) : null
          let degradedSecondary = retrySecondary
          if (staleAge) {
            degradedSecondary = serverRetrySeconds && serverRetrySeconds > 0 ? t("statusBar.streamStatus.fallback.degraded.dataAgeRetry", { age: staleAge, seconds: serverRetrySeconds }) : t("statusBar.streamStatus.fallback.degraded.dataAge", { age: staleAge })
          }
          return {
            label: t("statusBar.streamStatus.fallback.degraded.label"),
            message: t("statusBar.streamStatus.fallback.degraded.message"),
            secondary: degradedSecondary,
            tone: plan.tone,
            animate: false,
          }
        }

        return {
          label: t("statusBar.streamStatus.fallback.label"),
          message: backendStreamError ?? t("statusBar.streamStatus.fallback.message"),
          secondary: retrySecondary,
          tone: plan.tone,
          animate: false,
        }
      }
      case "healthy":
        return {
          label: "",
          message: null,
          secondary: null,
          tone: "success" as const,
          animate: false,
        }
      default:
        return {
          label: t("statusBar.streamStatus.connecting.label"),
          message: t("statusBar.streamStatus.connecting.message", { seconds: TORRENT_STREAM_POLL_INTERVAL_SECONDS }),
          secondary: t("statusBar.streamStatus.connecting.secondary", { seconds: TORRENT_STREAM_POLL_INTERVAL_SECONDS }),
          tone: streamConnected ? ("warning" as const) : ("muted" as const),
          animate: !streamConnected,
        }
    }
  }, [
    hasTorrentRows,
    isCrossSeedFiltering,
    stableStreamPhase,
    streamConnected,
    streamError,
    streamMeta,
    streamNextRetryAt,
    streamRetryAttempt,
    t,
  ])

  const streamToneStyles = useMemo(() => {
    switch (streamStatus.tone) {
      case "success":
        return { dotClass: "bg-emerald-500 shadow-[0_0_0_2px] shadow-emerald-500/25", textClass: "text-emerald-600 dark:text-emerald-400" }
      case "error":
        return { dotClass: "bg-destructive shadow-[0_0_0_2px] shadow-destructive/20", textClass: "text-destructive" }
      case "warning":
        return { dotClass: "bg-amber-400 shadow-[0_0_0_2px] shadow-amber-400/25", textClass: "text-amber-600 dark:text-amber-400" }
      default:
        return { dotClass: "bg-muted-foreground/60", textClass: "text-muted-foreground" }
    }
  }, [streamStatus.tone])
  const hasStreamStatusLabel = streamStatus.label.length > 0
  const hasStreamStatusDetails =
    hasStreamStatusLabel || Boolean(streamStatus.message) || Boolean(streamStatus.secondary)

  const supportsTrackerHealth = resolveTrackerHealthSupport({
    isUnifiedView: isAllInstancesView,
    capabilitySupport: capabilities?.supportsTrackerHealth,
    responseSupport: trackerHealthSupported,
  })
  const supportsSubcategories = isAllInstancesView? Boolean(subcategoriesFromData): (capabilities?.supportsSubcategories ?? false)
  const subcategoriesAlwaysEnabled = capabilities?.subcategoriesAlwaysEnabled ?? false
  const allowSubcategories = isAllInstancesView? Boolean(subcategoriesFromData): (supportsSubcategories && (subcategoriesAlwaysEnabled || (preferences?.use_subcategories ?? subcategoriesFromData ?? false)))
  const availableTags = isCrossInstanceEndpoint ? (tags ?? metadataTags) : metadataTags
  const availableCategories = isCrossInstanceEndpoint ? (categories ?? metadataCategories) : metadataCategories
  const isLoadingTags = isMetadataLoading && availableTags.length === 0
  const isLoadingCategories = isMetadataLoading && Object.keys(availableCategories).length === 0

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

  // Use torrents directly from backend (already sorted)
  const sortedTorrents = torrents

  const effectiveServerState = useEffectiveServerState({ instanceId, serverState })

  // Aggregate (all-instances) views have no single serverState; derive footer
  // transfer rates from the aggregated stats totals instead of showing 0.
  const footerSpeeds = useMemo(
    () => resolveFooterSpeeds(isAllInstancesView, stats, effectiveServerState),
    [isAllInstancesView, stats, effectiveServerState]
  )

  const {
    rowSelection,
    setRowSelection,
    isAllSelected,
    setIsAllSelected,
    excludedFromSelectAll,
    setExcludedFromSelectAll,
    shiftPressedRef,
    lastSelectedIndexRef,
    selectedRowIds,
    selectedRowIdSet,
    resetSelectionState,
    getSelectionIdentity,
    handleSelectAll,
    handleRowSelection,
    isSelectAllChecked,
    isSelectAllIndeterminate,
    handleCompactCheckboxPointerDown,
    handleCompactCheckboxChange,
  } = useTorrentSelection({
    sortedTorrents,
    isReadOnly,
    isCrossInstanceEndpoint,
    instanceId,
    onResetSelection,
    getVisibleRows: () => table.getRowModel().rows,
  })

  // Memoize columns to avoid unnecessary recalculations
  const { columns, torrentIdentityCounts } = useTorrentTableColumns({
    shiftPressedRef,
    lastSelectedIndexRef,
    handleSelectAll,
    isSelectAllChecked,
    isSelectAllIndeterminate,
    handleRowSelection,
    getSelectionIdentity,
    isAllSelected,
    excludedFromSelectAll,
    incognitoMode,
    speedUnit,
    trackerIcons,
    formatTimestamp,
    preferences,
    supportsTrackerHealth,
    isUnifiedView,
    isCrossInstanceEndpoint,
    desktopViewMode,
    trackerCustomizationLookup,
    isReadOnly,
    t,
    sortedTorrents,
  })

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
      const crossInstanceId = (row as Partial<CrossInstanceTorrent>).instanceId

      if (!baseIdentity) {
        return `row-${index}`
      }

      if (typeof crossInstanceId === "number" && crossInstanceId > 0) {
        return `${crossInstanceId}:${baseIdentity}`
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
          value: filter.value,
        })),
      }),
    },
    onSortingChange: setSorting,
    onGlobalFilterChange: setGlobalFilter,
    onRowSelectionChange: setRowSelection,
    onColumnSizingChange: setColumnSizing,
    onColumnVisibilityChange: setColumnVisibility,
    onColumnOrderChange: setColumnOrder,
    // Enable row selection
    enableRowSelection: !isReadOnly,
    // Enable column resizing
    enableColumnResizing: true,
    columnResizeMode: "onChange" as const,
    // Prevent automatic state resets during data updates
    autoResetPageIndex: false,
    autoResetExpanded: false,
  })

  // Keep the leaf-column accessor current for useColumnDnd (drag reads it lazily).
  leafColumnIdsRef.current = () => table.getAllLeafColumns().map(col => col.id)

  const {
    compactSortOptions,
    currentCompactSortLabel,
    handleCompactSortFieldChange,
    handleCompactSortOrderToggle,
  } = useCompactViewSort({
    table,
    columnVisibility,
    columnOrder,
    activeSortField,
    activeSortOrder,
    setSorting,
    setLastUserAction,
  })

  // Get selected torrent hashes - handle both regular selection and "select all" mode
  const {
    selectedHashes,
    effectiveSelectionCount,
    selectedTorrents,
    selectedTotalSize,
    selectedFormattedSize,
    deleteDialogTotalSize,
    deleteDialogFormattedSize,
    selectAllFilters,
    selectAllExcludedTargets,
    selectAllExcludeHashes,
  } = useTorrentSelectionDerivations({
    isAllSelected,
    excludedFromSelectAll,
    selectedRowIds,
    selectedRowIdSet,
    getSelectionIdentity,
    getVisibleRows: () => table.getRowModel().rows,
    sortedTorrents,
    columnFiltersExpr,
    filters,
    stats,
    totalCount,
    isCrossInstanceEndpoint,
    instanceId,
    contextTorrents,
  })
  const queryClient = useQueryClient()

  const [altSpeedOverride, setAltSpeedOverride] = useState<boolean | null>(null)
  const serverAltSpeedEnabled = effectiveServerState?.use_alt_speed_limits
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

  // Poll for async cross-seed filtering status updates


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

  const altSpeedTooltip = isAltSpeedKnown ? altSpeedEnabled ? t("statusBar.altSpeedOn") : t("statusBar.altSpeedOff") : t("statusBar.altSpeedUnknown")
  const altSpeedAriaLabel = isAltSpeedKnown ? altSpeedEnabled ? t("statusBar.disableAltSpeed") : t("statusBar.enableAltSpeed") : t("statusBar.altSpeedUnknown")

  const rawConnectionStatus = effectiveServerState?.connection_status ?? ""
  const normalizedConnectionStatus = rawConnectionStatus ? rawConnectionStatus.trim().toLowerCase() : ""
  const formattedConnectionStatus = normalizedConnectionStatus ? normalizedConnectionStatus.replace(/_/g, " ") : ""
  const connectionStatusDisplay = formattedConnectionStatus ? formattedConnectionStatus.replace(/\b\w/g, (char: string) => char.toUpperCase()) : ""
  const hasConnectionStatus = Boolean(formattedConnectionStatus)
  const isConnectable = normalizedConnectionStatus === "connected"
  const isFirewalled = normalizedConnectionStatus === "firewalled"
  const ConnectionStatusIcon = isConnectable ? Globe : isFirewalled ? BrickWallFire : hasConnectionStatus ? Ban : Globe
  const listenPort = metadata?.preferences?.listen_port
  const connectionStatusTooltip = hasConnectionStatus ? `${isConnectable ? t("statusBar.connectionConnectable") : connectionStatusDisplay}${listenPort ? `. ${t("statusBar.connectionPort", { port: listenPort })}` : ""}` : t("statusBar.connectionUnknown")
  const connectionStatusIconClass = hasConnectionStatus ? isConnectable ? "text-green-500" : isFirewalled ? "text-amber-500" : "text-destructive" : "text-muted-foreground"
  const connectionStatusAriaLabel = hasConnectionStatus ? t("statusBar.connectionAriaLabel", { status: connectionStatusDisplay || formattedConnectionStatus }) : t("statusBar.connectionAriaLabelUnknown")

  // Fire parent-facing notifications when filtered data or selection changes
  useTorrentTableNotifications({
    onFilteredDataUpdate,
    isLoading,
    instanceId,
    counts,
    categories,
    tags,
    totalCount,
    torrents,
    allowSubcategories,
    supportsTrackerHealth,
    onSelectionChange,
    selectedHashes,
    selectedTorrents,
    isAllSelected,
    effectiveSelectionCount,
    selectAllExcludeHashes,
    selectAllExcludedTargets,
    selectedTotalSize,
    selectAllFilters,
    filters,
  })

  // Callback for context menu to fetch field for matching torrents
  const fetchAllTorrentField = useCallback(async (field: "name" | "hash" | "full_path" | "magnet_uri"): Promise<string[]> => {
    const response = await api.getTorrentField(instanceId, field, {
      sort: activeSortField,
      order: activeSortOrder,
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
      excludeHashes: selectAllExcludeHashes,
      excludeTargets: isCrossInstanceEndpoint ? selectAllExcludedTargets : undefined,
      instanceIds: isCrossInstanceEndpoint ? instanceIds : undefined,
    })
    return response.values
  }, [instanceId, filters, effectiveIncludedCategories, effectiveExcludedCategories, combinedFiltersExpr, activeSortField, activeSortOrder, effectiveSearch, selectAllExcludeHashes, isCrossInstanceEndpoint, selectAllExcludedTargets, instanceIds])

  // Virtualization setup with progressive loading
  const { rows } = table.getRowModel()

  const {
    parentRef,
    virtualizer,
    virtualRows,
    safeLoadedRows,
    loadMore,
    loadedRows,
    setLoadedRows,
    setIsLoadingMoreRows,
  } = useTorrentTableVirtualization({
    rows,
    desktopViewMode,
    sortedTorrentsLength: sortedTorrents.length,
    hasLoadedAll,
    isLoadingMore,
    backendLoadMore,
  })

  // Memoize minTableWidth to avoid recalculation on every row render
  const minTableWidth = useMemo(() => {
    return table.getVisibleLeafColumns().reduce((width, col) => {
      return width + col.getSize()
    }, 0)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [table, columnVisibility])

  const { clearFiltersAtomically } = useFilterLifecycle({
    virtualizer,
    sortedTorrentsLength: sortedTorrents.length,
    onFilterChange,
    setColumnFilters,
    setSorting,
    setLoadedRows,
    isCrossSeedFiltering,
    columnFiltersLength: columnFilters.length,
    visibleRowCount: rows.length,
    loadedRows,
  })

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
    // setLoadedRows / setIsLoadingMoreRows are stable setters from the virtualization
    // hook; intentionally omitted to preserve the original effect's re-run timing.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters, effectiveSearch, instanceId, virtualizer, sortedTorrents.length, lastUserAction, resetSelectionState])

  useTorrentTableArrowNavigation({
    rows,
    virtualizer,
    safeLoadedRows,
    loadMore,
    isReadOnly,
    selectedTorrent,
    selectedRowIds,
    lastSelectedIndexRef,
    getSelectionIdentity,
    setRowSelection,
    setIsAllSelected,
    setExcludedFromSelectAll,
    onTorrentSelect,
  })

  const { isMac, selectAllWithShortcut } = useTorrentTableHotkeys({
    sortedTorrents,
    setIsAllSelected,
    setExcludedFromSelectAll,
    setRowSelection,
    lastSelectedIndexRef,
  })

  // Wrapper functions to adapt hook handlers to component needs
  const {
    normalizedSelectionFilters,
    contextClientMeta,
    runAction,
    handleExportWrapper,
    handleDeleteWrapper,
    handleSetCommentWrapper,
    handleTagsWrapper,
    handleSetCategoryWrapper,
    handleSetCategoryDirect,
    handleSetLocationWrapper,
    handleRenameTorrentWrapper,
    handleRenameFileWrapper,
    handleRenameFolderWrapper,
    handleRecheckWrapper,
    handleReannounceWrapper,
    handleTmmConfirmWrapper,
    handleSetShareLimitWrapper,
    handleSetSpeedLimitsWrapper,
    handleDropPayload,
    handleDropPayloadConsumed,
  } = useBulkActionWrappers({
    handleAction,
    handleDelete,
    handleSetComment,
    handleUpdateTags,
    handleSetCategory,
    handleSetLocation,
    handleRenameTorrent,
    handleRenameFile,
    handleRenameFolder,
    handleRecheck,
    handleReannounce,
    handleTmmConfirm,
    handleSetShareLimit,
    handleSetSpeedLimits,
    contextHashes,
    contextTorrents,
    deleteCrossSeeds,
    exportTorrents,
    isAllSelected,
    selectedHashes,
    selectedTorrents,
    effectiveSelectionCount,
    selectAllFilters,
    selectAllExcludeHashes,
    selectAllExcludedTargets,
    filters,
    effectiveSearch,
    activeSortField,
    activeSortOrder,
    crossSeedWarning,
    shouldBlockCrossSeeds,
    blockCrossSeedHashes,
    isCrossInstanceEndpoint,
    instanceIds,
    instanceId,
    setDropPayload,
    onAddTorrentModalChange,
  })

  return (
    <>
      <SelectAllHotkey
        onSelectAll={selectAllWithShortcut}
        isMac={isMac}
        enabled={!isReadOnly && sortedTorrents.length > 0}
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
                            <TooltipContent>{t("tableView.changeSortField")}</TooltipContent>
                          </Tooltip>
                          <DropdownMenuContent align="end" className="w-56 max-h-72 overflow-y-auto">
                            <DropdownMenuLabel>{t("mobileCards.sortBy")}</DropdownMenuLabel>
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
                              aria-label={`${t("sort.label")} ${activeSortOrder === "desc" ? t("sort.ascending") : t("sort.descending")}`}
                            >
                              {activeSortOrder === "desc" ? (
                                <ChevronDown className="h-4 w-4" />
                              ) : (
                                <ChevronUp className="h-4 w-4" />
                              )}
                            </Button>
                          </TooltipTrigger>
                          <TooltipContent>{t("tableView.sortDirection", { direction: activeSortOrder === "desc" ? t("tableView.ascending") : t("tableView.descending") })}</TooltipContent>
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
                              const clearingMode = isCrossSeedFiltering ? "columns-only" : "all"
                              clearFiltersAtomically(clearingMode)
                            }}
                          >
                            <X className="h-4 w-4" />
                            <span className="sr-only">{t("columnFilter.clearFilters")}</span>
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t("tableView.clearAllColumnFilters", { count: columnFilters.length })}</TooltipContent>
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
                                <span className="sr-only">{t("tableView.toggleColumns")}</span>
                              </Button>
                            </DropdownMenuTrigger>
                          </TooltipTrigger>
                          <TooltipContent>{t("tableView.toggleColumns")}</TooltipContent>
                        </Tooltip>
                        <DropdownMenuContent align="end" className="w-48">
                          <DropdownMenuLabel>{t("tableView.toggleColumns")}</DropdownMenuLabel>
                          <DropdownMenuSeparator />
                          {table
                            .getAllColumns()
                            .filter(
                              (column) =>
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

              {instanceId > 0 && (
                <AddTorrentDialog
                  instanceId={instanceId}
                  open={addTorrentModalOpen}
                  onOpenChange={onAddTorrentModalChange}
                  dropPayload={dropPayload}
                  onDropPayloadConsumed={handleDropPayloadConsumed}
                  torrents={torrents}
                />
              )}
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
            aria-label={t("tableView.tableAriaLabel")}
            aria-rowcount={totalCount}
            aria-colcount={table.getVisibleLeafColumns().length}
            onDropPayload={handleDropPayload}
          >
            {/* Loading overlay - positioned absolute to scroll container */}
            {torrents.length === 0 && showLoadingState && (
              <div className="absolute inset-0 flex items-center justify-center bg-background/80 backdrop-blur-sm z-50 animate-in fade-in duration-300">
                <div className="text-center animate-in zoom-in-95 duration-300">
                  <Logo className="h-12 w-12 animate-pulse mx-auto mb-3" />
                  <p>{t("statusBar.loadingTorrents")}</p>
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
                      {t("columnFilter.clearFilters")}
                    </Button>
                  )}
                </div>
              </div>
            )}

            <div style={{ position: "relative", minWidth: "min-content" }}>
              {/* Header - show in normal and dense table views */}
              <TableColumnHeader
                table={table}
                sensors={sensors}
                onDragEnd={onDragEnd}
                columnFilters={columnFilters}
                setColumnFilters={setColumnFilters}
                minTableWidth={minTableWidth}
                viewMode={desktopViewMode}
              />

              {/* Body */}
              <div
                onClick={(e) => {
                  // Click on empty table space clears all selection.
                  if (e.target !== e.currentTarget) {
                    return
                  }

                  if (!isAllSelected && selectedRowIds.length === 0) {
                    return
                  }

                  resetSelectionState()
                  onTorrentSelect?.(null)
                }}
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
                  const selectionIdentity = getSelectionIdentity(torrent)
                  const selectedInstanceID = (selectedTorrent as Partial<CrossInstanceTorrent> | null)?.instanceId ?? instanceId
                  const rowInstanceID = (torrent as Partial<CrossInstanceTorrent>).instanceId ?? instanceId
                  const isSelected = selectedTorrent?.hash === torrent.hash && selectedInstanceID === rowInstanceID
                  const isRowSelected = isAllSelected ? !excludedFromSelectAll.has(selectionIdentity) : row.getIsSelected()

                  // Render compact view for compact mode
                  if (desktopViewMode === "compact") {
                    return (
                      <TorrentContextMenu
                        key={row.id}
                        instanceId={instanceId}
                        readOnly={isReadOnly}
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
                        onPrepareComment={prepareCommentAction}
                        onPrepareCategory={prepareCategoryAction}
                        onPrepareCreateCategory={prepareCreateCategoryAction}
                        onPrepareShareLimit={prepareShareLimitAction}
                        onPrepareSpeedLimits={prepareSpeedLimitAction}
                        onPrepareLocation={prepareLocationAction}
                        onPrepareRenameTorrent={prepareRenameTorrentAction}
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
                        onFetchAllField={fetchAllTorrentField}
                      >
                        <CompactRow
                          torrent={torrent}
                          rowId={row.id}
                          rowIndex={virtualRow.index}
                          isSelected={isSelected}
                          isRowSelected={isRowSelected}
                          showCheckbox={table.getColumn("select")?.getIsVisible() !== false}
                          onClick={(e) => {
                            const target = e.target as HTMLElement
                            const isCheckboxElement = target.closest("[data-slot=\"checkbox\"]") || target.closest("[role=\"checkbox\"]")
                            if (isCheckboxElement) {
                              return
                            }

                            if (isReadOnly) {
                              if (isSelected) {
                                onTorrentSelect?.(null)
                              } else {
                                onTorrentSelect?.(torrent)
                              }
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
                                    handleRowSelection(getSelectionIdentity(targetRow.original), true, targetRow.id)
                                  }
                                }
                              } else {
                                handleRowSelection(selectionIdentity, true, row.id)
                                lastSelectedIndexRef.current = currentIndex
                              }
                            } else if (e.ctrlKey || e.metaKey) {
                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)
                              handleRowSelection(selectionIdentity, !isRowSelected, row.id)
                              lastSelectedIndexRef.current = currentIndex
                            } else {
                              // Plain click - open details panel
                              // Re-clicking the currently focused row toggles both details and selection off.
                              if (isSelected && isRowSelected) {
                                if (isAllSelected) {
                                  handleRowSelection(selectionIdentity, false, row.id)
                                } else {
                                  setRowSelection(prev => {
                                    if (!prev[row.id]) {
                                      return prev
                                    }

                                    const next = { ...prev }
                                    delete next[row.id]
                                    return next
                                  })

                                  if (selectedRowIds.length <= 1) {
                                    lastSelectedIndexRef.current = null
                                  }
                                }

                                onTorrentSelect?.(null)
                                return
                              }

                              // If row is not selected, select only this torrent (replace selection).
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
                            if (isReadOnly) {
                              return
                            }
                            if (!isRowSelected && selectedHashes.length <= 1) {
                              setRowSelection({ [row.id]: true })
                            }
                          }}
                          incognitoMode={incognitoMode}
                          speedUnit={speedUnit}
                          supportsTrackerHealth={supportsTrackerHealth}
                          trackerIcons={trackerIcons}
                          trackerCustomizationLookup={trackerCustomizationLookup}
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
                      readOnly={isReadOnly}
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
                      onPrepareComment={prepareCommentAction}
                      onPrepareCategory={prepareCategoryAction}
                      onPrepareCreateCategory={prepareCreateCategoryAction}
                      onPrepareShareLimit={prepareShareLimitAction}
                      onPrepareSpeedLimits={prepareSpeedLimitAction}
                      onPrepareLocation={prepareLocationAction}
                      onPrepareRenameTorrent={prepareRenameTorrentAction}
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
                      onFetchAllField={fetchAllTorrentField}
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
                            if (isReadOnly) {
                              if (isSelected) {
                                onTorrentSelect?.(null)
                              } else {
                                onTorrentSelect?.(torrent)
                              }
                              return
                            }

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
                                    handleRowSelection(getSelectionIdentity(targetRow.original), true, targetRow.id)
                                  }
                                }
                              } else {
                                // No anchor - just select this row
                                handleRowSelection(selectionIdentity, true, row.id)
                                lastSelectedIndexRef.current = currentIndex
                              }

                              // Don't update lastSelectedIndexRef on shift-click (keeps anchor stable)
                            } else if (e.ctrlKey || e.metaKey) {
                              // Ctrl/Cmd click - toggle single row EXACTLY like checkbox
                              const allRows = table.getRowModel().rows
                              const currentIndex = allRows.findIndex(r => r.id === row.id)

                              handleRowSelection(selectionIdentity, !isRowSelected, row.id)
                              lastSelectedIndexRef.current = currentIndex
                            } else {
                              // Plain click - open details panel
                              // Re-clicking the currently focused row toggles both details and selection off.
                              if (isSelected && isRowSelected) {
                                if (isAllSelected) {
                                  handleRowSelection(selectionIdentity, false, row.id)
                                } else {
                                  setRowSelection(prev => {
                                    if (!prev[row.id]) {
                                      return prev
                                    }

                                    const next = { ...prev }
                                    delete next[row.id]
                                    return next
                                  })

                                  if (selectedRowIds.length <= 1) {
                                    lastSelectedIndexRef.current = null
                                  }
                                }

                                onTorrentSelect?.(null)
                                return
                              }

                              // If row is not selected, select only this torrent (replace selection).
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
                          if (isReadOnly) {
                            return
                          }
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
                              data-torrent-column-measure={cell.column.id}
                              style={{
                                width: cell.column.getSize(),
                                flexShrink: 0,
                              }}
                              className={cn(
                                "flex items-center overflow-hidden min-w-0",
                                // Select and compact columns are centered to match header
                                (isSelectColumn || isCompactColumn) && "justify-center",
                                isCompactColumn? (desktopViewMode === "dense" ? "px-0 py-0.5" : "px-0 py-2"): (desktopViewMode === "dense" ? "px-2 py-0.5" : "px-3 py-2")
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

          {/* Status bar */}
          {(() => {
            const statusBarContent = (
              <div className="flex flex-wrap items-center justify-between gap-2 px-2 py-1.5 border-t flex-shrink-0 select-none">
                <div className="flex items-center gap-3 text-xs text-muted-foreground">
                  {/* Compact SSE status */}
                  {hasStreamStatusDetails ? (
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <div className="flex items-center gap-1.5 cursor-default text-[11px]">
                          <span
                            className={cn(
                              "h-1.5 w-1.5 rounded-full transition",
                              streamToneStyles.dotClass,
                              streamStatus.animate && "animate-pulse"
                            )}
                          />
                          {hasStreamStatusLabel && (
                            <span className={cn("opacity-80", streamToneStyles.textClass)}>{streamStatus.label}</span>
                          )}
                        </div>
                      </TooltipTrigger>
                      <TooltipContent className="max-w-xs text-xs">
                        <div className="space-y-1">
                          {hasStreamStatusLabel && <p className="font-medium">{streamStatus.label}</p>}
                          {streamStatus.message && <p>{streamStatus.message}</p>}
                          {streamStatus.secondary && <p className="text-muted-foreground">{streamStatus.secondary}</p>}
                        </div>
                      </TooltipContent>
                    </Tooltip>
                  ) : (
                    <div className="flex items-center cursor-default text-[11px]">
                      <span
                        className={cn(
                          "h-1.5 w-1.5 rounded-full transition",
                          streamToneStyles.dotClass,
                          streamStatus.animate && "animate-pulse"
                        )}
                      />
                    </div>
                  )}
                  <div>
                    {effectiveSelectionCount > 0 ? (
                      <>
                        <span>
                          {isAllSelected && excludedFromSelectAll.size === 0 ? t("statusBar.allSelected") : t("statusBar.selected", { count: effectiveSelectionCount })}
                          {selectedTotalSize > 0 && <> • {selectedFormattedSize}</>}
                        </span>
                        {/* Keyboard shortcuts helper - only show on desktop */}
                        <Tooltip>
                          <TooltipTrigger asChild>
                            <span className="hidden sm:inline-block ml-2 text-xs opacity-70 cursor-help">
                              {t("statusBar.selectionShortcuts")}
                            </span>
                          </TooltipTrigger>
                          <TooltipContent>
                            <div className="text-xs">
                              <div>{t("statusBar.shiftClick")}</div>
                              <div>{t("statusBar.ctrlClick", { modifier: isMac ? "Cmd" : "Ctrl" })}</div>
                            </div>
                          </TooltipContent>
                        </Tooltip>
                      </>
                    ) : (
                      <>
                        {/* Show special loading message when fetching without cache (cold load) */}
                        {isLoading && !isCachedData && !isStaleData && torrents.length === 0 ? (
                          <>
                            <Loader2 className="h-3 w-3 animate-spin inline mr-1"/>
                            {t("statusBar.loadingTorrents")}
                          </>
                        ) : totalCount === 0 ? (
                          emptyStateMessage
                        ) : (
                          <>
                            {hasLoadedAll ? (
                              t("statusBar.torrentCount", { count: torrents.length })
                            ) : isLoadingMore ? (
                              t("statusBar.loadingMore")
                            ) : (
                              t("statusBar.torrentsLoaded", { loaded: torrents.length, total: totalCount })
                            )}
                            {hasLoadedAll && safeLoadedRows < rows.length && ` ${t("statusBar.scrollForMore")}`}
                          </>
                        )}
                      </>
                    )}
                  </div>
                </div>

                <div className="flex flex-wrap items-center justify-end gap-2 text-xs">
                  <div className="flex items-center gap-2 pr-2 border-r last:border-r-0 last:pr-0">
                    <ChevronDown className="h-3 w-3 text-muted-foreground"/>
                    <span className="font-medium">{formatSpeedWithUnit(footerSpeeds.downloadSpeed, speedUnit)}</span>
                    <span className="font-medium">({formatBytes(footerSpeeds.downloadData)})</span>
                    <ChevronUp className="h-3 w-3 text-muted-foreground"/>
                    <span className="font-medium">{formatSpeedWithUnit(footerSpeeds.uploadSpeed, speedUnit)}</span>
                    <span className="font-medium">({formatBytes(footerSpeeds.uploadData)})</span>
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
                        {speedUnit === "bytes" ? t("statusBar.switchToBits") : t("statusBar.switchToBytes")}
                      </TooltipContent>
                    </Tooltip>
                    {/* Alternative speed limits are per-instance; the aggregate scope has
                        no single instance to toggle (and no serverState to read status from). */}
                    {!isAllInstancesView && (
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
                    )}
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
                                to: "/instances/$instanceId",
                                params: { instanceId: String(instanceId) },
                                search: { tab: "reannounce" },
                              })
                            }}
                            className="h-6 w-6 text-muted-foreground hover:text-accent-foreground"
                          >
                            <RefreshCcw className="h-4 w-4 text-green-500" />
                          </Button>
                        </TooltipTrigger>
                        <TooltipContent>{t("statusBar.reannounceEnabled")}</TooltipContent>
                      </Tooltip>
                    )}
                  </div>
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
                        {desktopViewMode === "normal" ? t("statusBar.viewModes.table") : desktopViewMode === "dense" ? t("statusBar.viewModes.dense") : t("statusBar.viewModes.stacked")}
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
                        {incognitoMode ? t("statusBar.incognitoOn") : t("statusBar.incognitoOff")}
                      </span>
                    </Button>
                  </div>
                  {effectiveServerState?.free_space_on_disk !== undefined && (
                    <div className="flex items-center gap-2 pr-2 border-r last:border-r-0 last:pr-0">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span className="flex items-center h-6 px-2 text-xs text-muted-foreground">
                            <HardDrive  aria-hidden="true" className="h-3 w-3 mr-1"/>
                            <span className="ml-auto font-medium truncate">{formatBytes(effectiveServerState.free_space_on_disk)}</span>
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>{t("statusBar.freeSpace")}</TooltipContent>
                      </Tooltip>
                    </div>
                  )}
                  <div className="flex items-center gap-2">
                    <ExternalIPAddress
                      address={effectiveServerState?.last_external_address_v4}
                      incognitoMode={incognitoMode}
                      label="IPv4"
                    />
                    <ExternalIPAddress
                      address={effectiveServerState?.last_external_address_v6}
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
                          <ConnectionStatusIcon className="h-3 w-3" aria-hidden="true"/>
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
            return statusBarContainer ? createPortal(statusBarContent, statusBarContainer) : statusBarContent
          })()}
        </div>

        <TorrentTableDialogs
          instanceId={instanceId}
          instanceIds={instanceIds}
          contextHashes={contextHashes}
          contextTorrents={contextTorrents}
          isPending={isPending}
          showDeleteDialog={showDeleteDialog}
          closeDeleteDialog={closeDeleteDialog}
          showCommentDialog={showCommentDialog}
          setShowCommentDialog={setShowCommentDialog}
          showTagsDialog={showTagsDialog}
          setShowTagsDialog={setShowTagsDialog}
          showCategoryDialog={showCategoryDialog}
          setShowCategoryDialog={setShowCategoryDialog}
          showCreateCategoryDialog={showCreateCategoryDialog}
          setShowCreateCategoryDialog={setShowCreateCategoryDialog}
          showShareLimitDialog={showShareLimitDialog}
          setShowShareLimitDialog={setShowShareLimitDialog}
          showSpeedLimitDialog={showSpeedLimitDialog}
          setShowSpeedLimitDialog={setShowSpeedLimitDialog}
          showLocationDialog={showLocationDialog}
          setShowLocationDialog={setShowLocationDialog}
          showRenameTorrentDialog={showRenameTorrentDialog}
          setShowRenameTorrentDialog={setShowRenameTorrentDialog}
          showRenameFileDialog={showRenameFileDialog}
          setShowRenameFileDialog={setShowRenameFileDialog}
          showRenameFolderDialog={showRenameFolderDialog}
          setShowRenameFolderDialog={setShowRenameFolderDialog}
          showRecheckDialog={showRecheckDialog}
          setShowRecheckDialog={setShowRecheckDialog}
          showReannounceDialog={showReannounceDialog}
          setShowReannounceDialog={setShowReannounceDialog}
          showTmmDialog={showTmmDialog}
          setShowTmmDialog={setShowTmmDialog}
          pendingTmmEnable={pendingTmmEnable}
          showLocationWarningDialog={showLocationWarningDialog}
          setShowLocationWarningDialog={setShowLocationWarningDialog}
          deleteFiles={deleteFiles}
          setDeleteFiles={setDeleteFiles}
          isDeleteFilesLocked={isDeleteFilesLocked}
          toggleDeleteFilesLock={toggleDeleteFilesLock}
          blockCrossSeeds={blockCrossSeeds}
          setBlockCrossSeeds={setBlockCrossSeeds}
          deleteCrossSeeds={deleteCrossSeeds}
          setDeleteCrossSeeds={setDeleteCrossSeeds}
          handleDeleteWrapper={handleDeleteWrapper}
          handleSetCommentWrapper={handleSetCommentWrapper}
          handleTagsWrapper={handleTagsWrapper}
          handleSetCategoryWrapper={handleSetCategoryWrapper}
          handleSetShareLimitWrapper={handleSetShareLimitWrapper}
          handleSetSpeedLimitsWrapper={handleSetSpeedLimitsWrapper}
          handleSetLocationWrapper={handleSetLocationWrapper}
          handleRenameTorrentWrapper={handleRenameTorrentWrapper}
          handleRenameFileWrapper={handleRenameFileWrapper}
          handleRenameFolderWrapper={handleRenameFolderWrapper}
          handleRecheckWrapper={handleRecheckWrapper}
          handleReannounceWrapper={handleReannounceWrapper}
          handleTmmConfirmWrapper={handleTmmConfirmWrapper}
          proceedToLocationDialog={proceedToLocationDialog}
          normalizedSelectionFilters={normalizedSelectionFilters}
          contextClientMeta={contextClientMeta}
          isAllSelected={isAllSelected}
          effectiveSelectionCount={effectiveSelectionCount}
          deleteDialogTotalSize={deleteDialogTotalSize}
          deleteDialogFormattedSize={deleteDialogFormattedSize}
          selectAllExcludeHashes={selectAllExcludeHashes}
          selectAllExcludedTargets={selectAllExcludedTargets}
          crossSeedWarning={crossSeedWarning}
          hasCrossSeedTag={hasCrossSeedTag}
          availableTags={availableTags}
          availableCategories={availableCategories}
          isLoadingTags={isLoadingTags}
          isLoadingCategories={isLoadingCategories}
          allowSubcategories={allowSubcategories}
          capabilities={capabilities}
          isCrossInstanceEndpoint={isCrossInstanceEndpoint}
          effectiveSearch={effectiveSearch}
        />

        {/* Instance Preferences Dialog */}
        {instance && instanceId > 0 && (
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
