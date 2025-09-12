/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDebounce } from "@/hooks/useDebounce"
import { useKeyboardNavigation } from "@/hooks/useKeyboardNavigation"
import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { usePersistedColumnSizing } from "@/hooks/usePersistedColumnSizing"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { usePersistedColumnVisibility } from "@/hooks/usePersistedColumnVisibility"
import { useTorrentActions } from "@/hooks/useTorrentActions"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import {
  DndContext,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
  type DragEndEvent
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
  useReactTable
} from "@tanstack/react-table"
import { useVirtualizer } from "@tanstack/react-virtual"
import React, { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { TorrentContextMenu } from "./TorrentContextMenu"

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
import { Button } from "@/components/ui/button"
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
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { getCommonCategory, getCommonTags } from "@/lib/torrent-utils"
import type { Category, Torrent, TorrentCounts } from "@/types"
import { useSearch } from "@tanstack/react-router"
import { ArrowUpDown, ChevronDown, ChevronUp, Columns3, Eye, EyeOff, Loader2 } from "lucide-react"
import { createPortal } from "react-dom"
import { AddTorrentDialog } from "./AddTorrentDialog"
import { DraggableTableHeader } from "./DraggableTableHeader"
import { AddTagsDialog, RemoveTagsDialog, SetCategoryDialog, SetTagsDialog } from "./TorrentDialogs"
import { createColumns } from "./TorrentTableColumns"

// Default values for persisted state hooks (module scope for stable references)
const DEFAULT_COLUMN_VISIBILITY = {
  downloaded: false,
  uploaded: false,
  save_path: false,
  tracker: false,
  priority: true,
  num_seeds: false,
  num_leechs: false,
}
const DEFAULT_COLUMN_SIZING = {}

// Helper function to get default column order (module scope for stable reference)
function getDefaultColumnOrder(): string[] {
  const cols = createColumns(false, undefined, "bytes")
  return cols.map(col => {
    if ("id" in col && col.id) return col.id
    if ("accessorKey" in col && typeof col.accessorKey === "string") return col.accessorKey
    return null
  }).filter((v): v is string => typeof v === "string")
}


interface TorrentTableOptimizedProps {
  instanceId: number
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
  selectedTorrent?: Torrent | null
  onTorrentSelect?: (torrent: Torrent | null) => void
  addTorrentModalOpen?: boolean
  onAddTorrentModalChange?: (open: boolean) => void
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: TorrentCounts, categories?: Record<string, Category>, tags?: string[]) => void
  onSelectionChange?: (selectedHashes: string[], selectedTorrents: Torrent[], isAllSelected: boolean, totalSelectionCount: number, excludeHashes: string[]) => void
  filterButton?: React.ReactNode
}

export const TorrentTableOptimized = memo(function TorrentTableOptimized({ instanceId, filters, selectedTorrent, onTorrentSelect, addTorrentModalOpen, onAddTorrentModalChange, onFilteredDataUpdate, onSelectionChange, filterButton }: TorrentTableOptimizedProps) {
  // State management
  // Move default values outside the component for stable references
  // (This should be at module scope, not inside the component)
  const [sorting, setSorting] = usePersistedColumnSorting([])
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  const [rowSelection, setRowSelection] = useState({})
  const [showRefetchIndicator, setShowRefetchIndicator] = useState(false)

  // Custom "select all" state for handling large datasets
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const [speedUnit, setSpeedUnit] = useSpeedUnits()

  // Track user-initiated actions to differentiate from automatic data updates
  const [lastUserAction, setLastUserAction] = useState<{ type: string; timestamp: number } | null>(null)
  const previousFiltersRef = useRef(filters)
  const previousInstanceIdRef = useRef(instanceId)
  const previousSearchRef = useRef("")

  // State for range select capabilities for checkboxes
  const shiftPressedRef = useRef<boolean>(false)
  const lastSelectedIndexRef = useRef<number | null>(null)

  // These should be defined at module scope, not inside the component, to ensure stable references
  // (If not already, move them to the top of the file)
  // const DEFAULT_COLUMN_VISIBILITY, DEFAULT_COLUMN_ORDER, DEFAULT_COLUMN_SIZING

  // Column visibility with persistence
  const [columnVisibility, setColumnVisibility] = usePersistedColumnVisibility(DEFAULT_COLUMN_VISIBILITY)
  // Column order with persistence (get default order at runtime to avoid initialization order issues)
  const [columnOrder, setColumnOrder] = usePersistedColumnOrder(getDefaultColumnOrder())
  // Column sizing with persistence
  const [columnSizing, setColumnSizing] = usePersistedColumnSizing(DEFAULT_COLUMN_SIZING)

  // Progressive loading state with async management
  const [loadedRows, setLoadedRows] = useState(100)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)

  // Delayed loading state to avoid flicker on fast loads
  const [showLoadingState, setShowLoadingState] = useState(false)

  // Use the shared torrent actions hook
  const {
    showDeleteDialog,
    setShowDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    showAddTagsDialog,
    setShowAddTagsDialog,
    showSetTagsDialog,
    setShowSetTagsDialog,
    showRemoveTagsDialog,
    setShowRemoveTagsDialog,
    showCategoryDialog,
    setShowCategoryDialog,
    showRecheckDialog,
    setShowRecheckDialog,
    showReannounceDialog,
    setShowReannounceDialog,
    contextHashes,
    contextTorrents,
    isPending,
    handleAction,
    handleDelete,
    handleAddTags,
    handleSetTags,
    handleRemoveTags,
    handleSetCategory,
    handleSetShareLimit,
    handleSetSpeedLimits,
    handleRecheck,
    handleReannounce,
    prepareDeleteAction,
    prepareTagsAction,
    prepareCategoryAction,
    prepareRecheckAction,
    prepareReannounceAction,
  } = useTorrentActions({
    instanceId,
    onActionComplete: () => {
      setRowSelection({})
    },
  })

  // Fetch metadata using shared hook
  const { data: metadata } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}

  // Debounce search to prevent excessive filtering (200ms delay for faster response)
  const debouncedSearch = useDebounce(globalFilter, 200)
  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const searchFromRoute = routeSearch?.q || ""

  // Use route search if present, otherwise fall back to local immediate/debounced search
  const effectiveSearch = searchFromRoute || immediateSearch || debouncedSearch

  // Keep local input state in sync with route query so internal effects remain consistent
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
    return columnId || "added_on"
  }

  // Fetch torrents data with backend sorting
  const {
    torrents,
    totalCount,
    stats,
    counts,
    categories,
    tags,

    isLoading,
    isFetching,
    isCachedData,
    isStaleData,
    isLoadingMore,
    hasLoadedAll,
    loadMore: backendLoadMore,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters,
    sort: sorting.length > 0 ? getBackendSortField(sorting[0].id) : "added_on",
    order: sorting.length > 0 ? (sorting[0].desc ? "desc" : "asc") : "desc",
  })

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

  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      // Only skip callback if ALL metadata is undefined (indicates incomplete initial load during instance switch)
      // If any metadata exists, or if torrents list is non-empty, proceed with callback
      const hasAnyMetadata = counts !== undefined || categories !== undefined || tags !== undefined
      const hasExistingTorrents = torrents.length > 0

      if (hasAnyMetadata || hasExistingTorrents) {
        onFilteredDataUpdate(torrents, totalCount, counts, categories, tags)
      }
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, onFilteredDataUpdate]) // Use torrents.length to avoid unnecessary calls when content updates


  // Show refetch indicator only if fetching takes more than 2 seconds
  useEffect(() => {
    let timeoutId: ReturnType<typeof setTimeout>

    if (isFetching && !isLoading && torrents.length > 0) {
      timeoutId = setTimeout(() => {
        setShowRefetchIndicator(true)
      }, 2000)
    } else {
      setShowRefetchIndicator(false)
    }

    return () => clearTimeout(timeoutId)
  }, [isFetching, isLoading, torrents.length])

  // Use torrents directly from backend (already sorted)
  const sortedTorrents = torrents

  // Custom selection handlers for "select all" functionality
  const handleSelectAll = useCallback(() => {
    // Gmail-style behavior: if any rows are selected, always deselect all
    const hasAnySelection = isAllSelected || Object.values(rowSelection).some(selected => selected)

    if (hasAnySelection) {
      // Deselect all mode - regardless of checked state
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
    } else {
      // Select all mode - only when nothing is selected
      setIsAllSelected(true)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})
    }
  }, [setRowSelection, isAllSelected, rowSelection])

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
    const regularSelectionCount = Object.keys(rowSelection)
      .filter((key: string) => (rowSelection as Record<string, boolean>)[key]).length
    return regularSelectionCount === sortedTorrents.length && sortedTorrents.length > 0
  }, [isAllSelected, excludedFromSelectAll.size, rowSelection, sortedTorrents.length])

  const isSelectAllIndeterminate = useMemo(() => {
    // Show indeterminate (dash) when SOME but not ALL items are selected
    if (isAllSelected) {
      // In "select all" mode, show indeterminate if some are excluded
      return excludedFromSelectAll.size > 0
    }

    const regularSelectionCount = Object.keys(rowSelection)
      .filter((key: string) => (rowSelection as Record<string, boolean>)[key]).length

    // Indeterminate when some (but not all) are selected
    return regularSelectionCount > 0 && regularSelectionCount < sortedTorrents.length
  }, [isAllSelected, excludedFromSelectAll.size, rowSelection, sortedTorrents.length])

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
    }, speedUnit),
    [incognitoMode, speedUnit, handleSelectAll, isSelectAllChecked, isSelectAllIndeterminate, handleRowSelection, isAllSelected, excludedFromSelectAll]
  )

  const table = useReactTable({
    data: sortedTorrents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    // Use torrent hash with index as unique row ID to handle duplicates
    getRowId: (row: Torrent, index: number) => `${row.hash}-${index}`,
    // State management
    state: {
      sorting,
      globalFilter,
      rowSelection,
      columnSizing,
      columnVisibility,
      columnOrder,
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
        .filter(row => (rowSelection as Record<string, boolean>)[row.id])
        .map(row => row.original.hash)
    }
  }, [rowSelection, isAllSelected, excludedFromSelectAll, sortedTorrents, table])

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // When all selected, count is total minus exclusions
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the computed selectedHashes length
      return Object.keys(rowSelection)
        .filter((key: string) => (rowSelection as Record<string, boolean>)[key]).length
    }
  }, [isAllSelected, totalCount, excludedFromSelectAll.size, rowSelection])

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

  // Call the callback when selection state changes
  useEffect(() => {
    if (onSelectionChange) {
      onSelectionChange(
        selectedHashes,
        selectedTorrents,
        isAllSelected,
        effectiveSelectionCount,
        Array.from(excludedFromSelectAll)
      )
    }
  }, [onSelectionChange, selectedHashes, selectedTorrents, isAllSelected, effectiveSelectionCount, excludedFromSelectAll])

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
    if (loadedRows > rows.length && rows.length > 0) {
      setLoadedRows(rows.length)
    }
  }, [loadedRows, rows.length])

  // useVirtualizer must be called at the top level, not inside useMemo
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 40,
    // Optimized overscan based on TanStack Virtual recommendations
    // Start small and adjust based on dataset size and performance
    overscan: sortedTorrents.length > 50000 ? 3 : sortedTorrents.length > 10000 ? 5 : sortedTorrents.length > 1000 ? 10 : 15,
    // Provide a key to help with item tracking - use hash with index for uniqueness
    getItemKey: useCallback((index: number) => {
      const row = rows[index]
      return row?.original?.hash ? `${row.original.hash}-${index}` : `loading-${index}`
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

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, virtualizer])

  const virtualRows = virtualizer.getVirtualItems()

  // Memoize minTableWidth to avoid recalculation on every row render
  const minTableWidth = useMemo(() => {
    return table.getVisibleLeafColumns().reduce((width, col) => {
      return width + col.getSize()
    }, 0)
  }, [table])

  // Derive hidden columns state from table API for accuracy
  const hasHiddenColumns = useMemo(() => {
    return table.getAllLeafColumns().filter(c => c.getCanHide()).some(c => !c.getIsVisible())
  }, [table])

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
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setRowSelection({})

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
  }, [filters, effectiveSearch, instanceId, virtualizer, sortedTorrents.length, setRowSelection, lastUserAction])

  // Clear selection handler for keyboard navigation
  const clearSelection = useCallback(() => {
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
    setRowSelection({})
  }, [setRowSelection])

  // Set up keyboard navigation with selection clearing
  useKeyboardNavigation({
    parentRef,
    virtualizer,
    safeLoadedRows,
    hasLoadedAll,
    isLoadingMore,
    loadMore,
    estimatedRowHeight: 40,
    onClearSelection: clearSelection,
    hasSelection: isAllSelected || Object.values(rowSelection).some(selected => selected),
  })



  // Wrapper functions to adapt hook handlers to component needs
  const handleDeleteWrapper = useCallback(() => {
    handleDelete(
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleDelete, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleAddTagsWrapper = useCallback((tags: string[]) => {
    handleAddTags(
      tags,
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleAddTags, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleSetTagsWrapper = useCallback((tags: string[]) => {
    handleSetTags(
      tags,
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleSetTags, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    handleSetCategory(
      category,
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleSetCategory, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleRemoveTagsWrapper = useCallback((tags: string[]) => {
    handleRemoveTags(
      tags,
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleRemoveTags, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleRecheckWrapper = useCallback(() => {
    handleRecheck(
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleRecheck, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleReannounceWrapper = useCallback(() => {
    handleReannounce(
      contextHashes,
      isAllSelected,
      filters,
      effectiveSearch,
      Array.from(excludedFromSelectAll)
    )
  }, [handleReannounce, contextHashes, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])


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

  const handleDragEnd = useCallback((event: DragEndEvent) => {
    const { active, over } = event
    if (active && over && active.id !== over.id) {
      setColumnOrder((currentOrder: string[]) => {
        const oldIndex = currentOrder.indexOf(active.id as string)
        const newIndex = currentOrder.indexOf(over.id as string)
        return arrayMove(currentOrder, oldIndex, newIndex)
      })
    }
  }, [setColumnOrder])

  return (
    <div className="h-full flex flex-col">
      {/* Search and Actions */}
      <div className="flex flex-col gap-2 flex-shrink-0">
        {/* Search bar row */}
        <div className="flex items-center gap-1 sm:gap-2">
          {/* Filter button - only on desktop */}
          {filterButton && (
            <div className="hidden xl:block">
              {filterButton}
            </div>
          )}
          {/* Action buttons - now handled by Management Bar in Header */}
          <div className="flex gap-1 sm:gap-2 flex-shrink-0">

            {/* Column visibility dropdown moved next to search via portal, with inline fallback */}
            {(() => {
              const container = typeof document !== "undefined" ? document.getElementById("header-search-actions") : null
              const dropdown = (
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
                          className="relative"
                        >
                          <Columns3 className="h-4 w-4" />
                          {hasHiddenColumns && (
                            <span className="absolute -top-1 -right-1 h-2 w-2 bg-primary rounded-full" />
                          )}
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
              )
              return container ? createPortal(dropdown, container) : dropdown
            })()}

            <AddTorrentDialog
              instanceId={instanceId}
              open={addTorrentModalOpen}
              onOpenChange={onAddTorrentModalChange}
            />
          </div>
        </div>
      </div>

      {/* Table container */}
      <div className="flex flex-col flex-1 min-h-0 mt-2 sm:mt-0 overflow-hidden">
        <div
          className="relative flex-1 overflow-auto scrollbar-thin select-none"
          ref={parentRef}
          role="grid"
          aria-label="Torrents table"
          aria-rowcount={totalCount}
          aria-colcount={table.getVisibleLeafColumns().length}
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

          <div style={{ position: "relative", minWidth: "min-content" }}>
            {/* Header */}
            <div className="sticky top-0 bg-background border-b" style={{ zIndex: 50 }}>
              <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
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
                          />
                        ))}
                      </div>
                    </SortableContext>
                  )
                })}
              </DndContext>
            </div>

            {/* Body */}
            {torrents.length === 0 && !isLoading ? (
              // Show empty state
              <div className="p-8 text-center text-muted-foreground">
                <p>No torrents found</p>
              </div>
            ) : (
              // Show virtual table
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

                  // Use memoized minTableWidth
                  return (
                    <TorrentContextMenu
                      key={`${torrent.hash}-${virtualRow.index}`}
                      torrent={torrent}
                      isSelected={row.getIsSelected()}
                      isAllSelected={isAllSelected}
                      selectedHashes={selectedHashes}
                      selectedTorrents={selectedTorrents}
                      effectiveSelectionCount={effectiveSelectionCount}
                      onTorrentSelect={onTorrentSelect}
                      onAction={handleAction}
                      onPrepareDelete={prepareDeleteAction}
                      onPrepareTags={prepareTagsAction}
                      onPrepareCategory={prepareCategoryAction}
                      onPrepareRecheck={prepareRecheckAction}
                      onPrepareReannounce={prepareReannounceAction}
                      onSetShareLimit={handleSetShareLimit}
                      onSetSpeedLimits={handleSetSpeedLimits}
                      isPending={isPending}
                    >
                      <div
                        className={`flex border-b cursor-pointer hover:bg-muted/50 ${row.getIsSelected() ? "bg-muted/50" : ""} ${isSelected ? "bg-accent" : ""}`}
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
                            onTorrentSelect?.(torrent)
                          }
                        }}
                        onContextMenu={() => {
                          // Only select this row if not already selected and not part of a multi-selection
                          if (!row.getIsSelected() && selectedHashes.length <= 1) {
                            setRowSelection({ [row.id]: true })
                          }
                        }}
                      >
                        {row.getVisibleCells().map(cell => (
                          <div
                            key={cell.id}
                            style={{
                              width: cell.column.getSize(),
                              flexShrink: 0,
                            }}
                            className="px-3 py-2 flex items-center overflow-hidden min-w-0"
                          >
                            {flexRender(
                              cell.column.columnDef.cell,
                              cell.getContext()
                            )}
                          </div>
                        ))}
                      </div>
                    </TorrentContextMenu>
                  )
                })}
              </div>
            )}
          </div>
        </div>

        {/* Status bar */}
        <div className="flex items-center justify-between p-2 border-t flex-shrink-0 select-none">
          <div className="text-sm text-muted-foreground">
            {/* Show special loading message when fetching without cache (cold load) */}
            {isLoading && !isCachedData && !isStaleData && torrents.length === 0 ? (
              <>
                <Loader2 className="h-3 w-3 animate-spin inline mr-1" />
                Loading torrents from instance... (no cache available)
              </>
            ) : totalCount === 0 ? (
              "No torrents found"
            ) : (
              <>
                {hasLoadedAll ? (
                  `${torrents.length} torrent${torrents.length !== 1 ? "s" : ""}`
                ) : isLoadingMore ? (
                  "Loading more torrents..."
                ) : (
                  `${torrents.length} of ${totalCount} torrents loaded • Scroll to load more`
                )}
                {hasLoadedAll && safeLoadedRows < rows.length && " (scroll for more)"}
              </>
            )}
            {effectiveSelectionCount > 0 && (
              <>
                <span className="ml-2">
                  ({isAllSelected && excludedFromSelectAll.size === 0 ? `All ${effectiveSelectionCount}` : effectiveSelectionCount} selected)
                </span>
              </>
            )}
            {showRefetchIndicator && (
              <span className="ml-2">
                <Loader2 className="h-3 w-3 animate-spin inline mr-1" />
                Updating...
              </span>
            )}
          </div>


          <div className="flex items-center gap-2 text-xs">
            <div className="flex items-center gap-1">
              <ChevronDown className="h-3 w-3 text-muted-foreground" />
              <span className="font-medium">{formatSpeedWithUnit(stats.totalDownloadSpeed || 0, speedUnit)}</span>
              <ChevronUp className="h-3 w-3 text-muted-foreground" />
              <span className="font-medium">{formatSpeedWithUnit(stats.totalUploadSpeed || 0, speedUnit)}</span>
            </div>
          </div>



          <div className="flex items-center gap-4">
            {/* Speed units toggle */}
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setSpeedUnit(speedUnit === "bytes" ? "bits" : "bytes")}
                  className="flex items-center gap-1 pl-1.5 py-0.5 rounded-sm transition-all hover:bg-muted/50"
                >
                  <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-xs text-muted-foreground">
                    {speedUnit === "bytes" ? "MiB/s" : "Mbps"}
                  </span>
                </button>
              </TooltipTrigger>
              <TooltipContent>
                {speedUnit === "bytes" ? "Switch to bits per second (bps)" : "Switch to bytes per second (B/s)"}
              </TooltipContent>
            </Tooltip>
            {/* Incognito mode toggle */}
            <Tooltip>
              <TooltipTrigger asChild>
                <button
                  onClick={() => setIncognitoMode(!incognitoMode)}
                  className="p-1 rounded-sm transition-all hover:bg-muted/50"
                >
                  {incognitoMode ? (
                    <EyeOff className="h-3.5 w-3.5 text-muted-foreground" />
                  ) : (
                    <Eye className="h-3.5 w-3.5 text-muted-foreground" />
                  )}
                </button>
              </TooltipTrigger>
              <TooltipContent>
                {incognitoMode ? "Exit incognito mode" : "Enable incognito mode"}
              </TooltipContent>
            </Tooltip>
          </div>
        </div>
      </div>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {isAllSelected ? effectiveSelectionCount : contextHashes.length} torrent(s)?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. The torrents will be removed from qBittorrent.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center space-x-2 py-4">
            <input
              type="checkbox"
              id="deleteFiles"
              checked={deleteFiles}
              onChange={(e) => setDeleteFiles(e.target.checked)}
              className="rounded border-input"
            />
            <label htmlFor="deleteFiles" className="text-sm font-medium">
              Also delete files from disk
            </label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteWrapper}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Add Tags Dialog */}
      <AddTagsDialog
        open={showAddTagsDialog}
        onOpenChange={setShowAddTagsDialog}
        availableTags={availableTags || []}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        onConfirm={handleAddTagsWrapper}
        isPending={isPending}
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

      {/* Scroll to top button*/}
      <div className="hidden lg:block">
        <ScrollToTopButton
          scrollContainerRef={parentRef}
          className="bottom-20 right-6"
        />
      </div>
    </div>
  )
});