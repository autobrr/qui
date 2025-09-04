/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useDebounce } from "@/hooks/useDebounce"
import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { usePersistedColumnSizing } from "@/hooks/usePersistedColumnSizing"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { usePersistedColumnVisibility } from "@/hooks/usePersistedColumnVisibility"
import { usePersistedDeleteFiles } from "@/hooks/usePersistedDeleteFiles"
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
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
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
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { api } from "@/lib/api"
import {
  getLinuxIsoName,
  useIncognitoMode
} from "@/lib/incognito"
import { formatSpeed } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { useSearch } from "@tanstack/react-router"
import { CheckCircle, ChevronDown, ChevronUp, Columns3, Copy, Eye, EyeOff, Folder, Loader2, Pause, Play, Radio, Settings2, Sparkles, Tag, Trash2 } from "lucide-react"
import { createPortal } from "react-dom"
import { toast } from "sonner"
import { AddTorrentDialog } from "./AddTorrentDialog"
import { DraggableTableHeader } from "./DraggableTableHeader"
import { QueueSubmenu } from "./QueueSubmenu"
import { TorrentActions } from "./TorrentActions"
import { AddTagsDialog, RemoveTagsDialog, SetCategoryDialog, SetTagsDialog } from "./TorrentDialogs"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"
import { createColumns } from "./TorrentTableColumns"

// Default values for persisted state hooks (module scope for stable references)
const DEFAULT_COLUMN_VISIBILITY = {
  downloaded: false,
  uploaded: false,
  save_path: false, // Fixed: was 'saveLocation', should match column accessorKey
  tracker: false,
  priority: true,
}
const DEFAULT_COLUMN_SIZING = {}

// Helper function to get default column order (module scope for stable reference)
function getDefaultColumnOrder(): string[] {
  const cols = createColumns(false)
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
  filterButton?: React.ReactNode
}

export const TorrentTableOptimized = memo(function TorrentTableOptimized({ instanceId, filters, selectedTorrent, onTorrentSelect, addTorrentModalOpen, onAddTorrentModalChange, onFilteredDataUpdate, filterButton }: TorrentTableOptimizedProps) {
  // State management
  // Move default values outside the component for stable references
  // (This should be at module scope, not inside the component)
  const [sorting, setSorting] = usePersistedColumnSorting([])
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  const [rowSelection, setRowSelection] = useState({})
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = usePersistedDeleteFiles()
  const [contextMenuHashes, setContextMenuHashes] = useState<string[]>([])
  const [contextMenuTorrents, setContextMenuTorrents] = useState<Torrent[]>([])
  const [showAddTagsDialog, setShowAddTagsDialog] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showRemoveTagsDialog, setShowRemoveTagsDialog] = useState(false)
  const [showRecheckDialog, setShowRecheckDialog] = useState(false)
  const [showReannounceDialog, setShowReannounceDialog] = useState(false)
  const [showRefetchIndicator, setShowRefetchIndicator] = useState(false)

  // Custom "select all" state for handling large datasets
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

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

  // Query client for invalidating queries
  const queryClient = useQueryClient()

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
    const mapping: Record<string, string> = {
      "priority": "priority",
      "name": "name",
      "size": "size",
      "progress": "progress",
      "state": "state",
      "dlspeed": "dlspeed",
      "upspeed": "upspeed",
      "eta": "eta",
      "ratio": "ratio",
      "added_on": "added_on",
      "category": "category",
      "tags": "tags",
      "downloaded": "downloaded",
      "uploaded": "uploaded",
      "save_path": "save_path",
      "tracker": "tracker",
    }
    return mapping[columnId] || "added_on"
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
    }),
    [incognitoMode, handleSelectAll, isSelectAllChecked, isSelectAllIndeterminate, handleRowSelection, isAllSelected, excludedFromSelectAll]
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


  // Mutation for bulk actions
  const mutation = useMutation({
    mutationFn: (data: {
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "addTags" | "removeTags" | "setTags" | "setCategory" | "toggleAutoTMM" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit"
      hashes: string[]
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
      ratioLimit?: number
      seedingTimeLimit?: number
      inactiveSeedingTimeLimit?: number
      uploadLimit?: number
      downloadLimit?: number
      selectAll?: boolean
      filters?: {
        status: string[]
        categories: string[]
        tags: string[]
        trackers: string[]
      }
      search?: string
      excludeHashes?: string[]
    }) => {
      return api.bulkAction(instanceId, {
        hashes: data.hashes,
        action: data.action,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
        ratioLimit: data.ratioLimit,
        seedingTimeLimit: data.seedingTimeLimit,
        inactiveSeedingTimeLimit: data.inactiveSeedingTimeLimit,
        uploadLimit: data.uploadLimit,
        downloadLimit: data.downloadLimit,
        selectAll: data.selectAll,
        filters: data.filters,
        search: data.search,
        excludeHashes: data.excludeHashes,
      })
    },
    onSuccess: async (_, variables) => {
      // For delete operations, optimistically remove from UI immediately
      if (variables.action === "delete") {
        // Clear selection and context menu immediately
        setRowSelection({})
        setContextMenuHashes([])

        // Optimistically remove torrents from ALL cached queries for this instance
        // This includes all pages, filters, and search variations
        const cache = queryClient.getQueryCache()
        const queries = cache.findAll({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })

        queries.forEach((query) => {
          queryClient.setQueryData(query.queryKey, (oldData: {
            torrents?: Torrent[]
            total?: number
            totalCount?: number
          }) => {
            if (!oldData) return oldData
            return {
              ...oldData,
              torrents: oldData.torrents?.filter((t: Torrent) =>
                !variables.hashes.includes(t.hash)
              ) || [],
              total: Math.max(0, (oldData.total || 0) - variables.hashes.length),
              totalCount: Math.max(0, (oldData.totalCount || oldData.total || 0) - variables.hashes.length),
            }
          })
        })

        // Refetch later to sync with actual server state (don't invalidate!)
        // Longer delay when deleting files from disk
        const refetchDelay = variables.deleteFiles ? 5000 : 2000

        setTimeout(() => {
          // Use refetch instead of invalidate to keep showing data
          queryClient.refetchQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
            type: "active", // Only refetch if component is mounted
          })
          // Also refetch the counts query
          queryClient.refetchQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
            type: "active",
          })
        }, refetchDelay)
      } else {
        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const refetchDelay = variables.action === "resume" ? 2000 : 1000

        setTimeout(() => {
          // Use refetch instead of invalidate to avoid loading state
          queryClient.refetchQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
            type: "active",
          })
          queryClient.refetchQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
            type: "active",
          })
        }, refetchDelay)
        setContextMenuHashes([])
      }
    },
  })

  const handleDelete = async () => {
    await mutation.mutateAsync({
      action: "delete",
      deleteFiles,
      hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
    setContextMenuHashes([])
  }

  const handleAddTags = async (tags: string[]) => {
    await mutation.mutateAsync({
      hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
      action: "addTags",
      tags: tags.join(","),
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowAddTagsDialog(false)
    setContextMenuHashes([])
  }

  const handleSetTags = async (tags: string[]) => {
    // Use setTags action (with fallback to addTags for older versions)
    // The backend will handle the version check
    try {
      await mutation.mutateAsync({
        hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
        action: "setTags",
        tags: tags.join(","),
        selectAll: isAllSelected,
        filters: isAllSelected ? filters : undefined,
        search: isAllSelected ? effectiveSearch : undefined,
        excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      })
    } catch (error) {
      // If setTags fails due to version requirement, fall back to addTags
      if ((error as Error).message?.includes("requires qBittorrent")) {
        await mutation.mutateAsync({
          hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
          action: "addTags",
          tags: tags.join(","),
          selectAll: isAllSelected,
          filters: isAllSelected ? filters : undefined,
          search: isAllSelected ? effectiveSearch : undefined,
          excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
        })
      } else {
        throw error
      }
    }

    setShowTagsDialog(false)
    setContextMenuHashes([])
  }

  const handleSetCategory = async (category: string) => {
    await mutation.mutateAsync({
      action: "setCategory",
      category,
      hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowCategoryDialog(false)
    setContextMenuHashes([])
  }

  const handleRemoveTags = async (tags: string[]) => {
    await mutation.mutateAsync({
      action: "removeTags",
      tags: tags.join(","),
      hashes: isAllSelected ? [] : contextMenuHashes,  // Empty hashes when selectAll is true
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowRemoveTagsDialog(false)
    setContextMenuHashes([])
  }

  const handleSetShareLimit = async (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number, hashes?: string[]) => {
    const targetHashes = hashes || contextMenuHashes
    await mutation.mutateAsync({
      action: "setShareLimit",
      hashes: targetHashes,
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
    })
    setContextMenuHashes([])
  }

  const handleSetSpeedLimits = async (uploadLimit: number, downloadLimit: number, hashes?: string[]) => {
    const targetHashes = hashes || contextMenuHashes
    // Set upload and download limits separately since they are different actions
    const promises = []
    if (uploadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setUploadLimit", hashes: targetHashes, uploadLimit }))
    }
    if (downloadLimit >= 0) {
      promises.push(mutation.mutateAsync({ action: "setDownloadLimit", hashes: targetHashes, downloadLimit }))
    }

    if (promises.length > 0) {
      await Promise.all(promises)
    }

    setContextMenuHashes([])
  }

  const handleContextMenuAction = useCallback((action: "pause" | "resume" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "toggleAutoTMM", hashes: string[], enable?: boolean) => {
    setContextMenuHashes(hashes)
    mutation.mutate({ action, hashes, enable })
  }, [mutation])

  const handleRecheck = useCallback(async () => {
    await mutation.mutateAsync({
      action: "recheck",
      hashes: isAllSelected ? [] : contextMenuHashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowRecheckDialog(false)
    setContextMenuHashes([])
  }, [mutation, isAllSelected, contextMenuHashes, filters, effectiveSearch, excludedFromSelectAll])

  const handleReannounce = useCallback(async () => {
    await mutation.mutateAsync({
      action: "reannounce",
      hashes: isAllSelected ? [] : contextMenuHashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowReannounceDialog(false)
    setContextMenuHashes([])
  }, [mutation, isAllSelected, contextMenuHashes, filters, effectiveSearch, excludedFromSelectAll])

  const handleRecheckClick = useCallback((hashes: string[]) => {
    const count = isAllSelected ? effectiveSelectionCount : hashes.length
    if (count > 1) {
      setContextMenuHashes(hashes)
      setShowRecheckDialog(true)
    } else {
      handleContextMenuAction("recheck", hashes)
    }
  }, [isAllSelected, effectiveSelectionCount, handleContextMenuAction])

  const handleReannounceClick = useCallback((hashes: string[]) => {
    const count = isAllSelected ? effectiveSelectionCount : hashes.length
    if (count > 1) {
      setContextMenuHashes(hashes)
      setShowReannounceDialog(true)
    } else {
      handleContextMenuAction("reannounce", hashes)
    }
  }, [isAllSelected, effectiveSelectionCount, handleContextMenuAction])

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash") => {
    try {
      await navigator.clipboard.writeText(text)
      const message = type === "name" ? "Torrent name copied!" : "Torrent hash copied!"
      toast.success(message)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])

  // Synchronous version for immediate use (backwards compatibility)
  const getCommonTagsSync = (torrents: Torrent[]): string[] => {
    if (torrents.length === 0) return []

    // Fast path for single torrent
    if (torrents.length === 1) {
      const tags = torrents[0].tags;
      return tags ? tags.split(",").map(t => t.trim()).filter(Boolean) : [];
    }

    // Initialize with first torrent's tags
    const firstTorrent = torrents[0];
    if (!firstTorrent.tags) return [];

    // Use a Set for O(1) lookups
    const firstTorrentTagsSet = new Set(
      firstTorrent.tags.split(",").map(t => t.trim()).filter(Boolean)
    );

    // If first torrent has no tags, no common tags exist
    if (firstTorrentTagsSet.size === 0) return [];

    // Convert to array once for iteration
    const firstTorrentTags = Array.from(firstTorrentTagsSet);

    // Use Object as a counter map for better performance with large datasets
    const tagCounts: Record<string, number> = {};
    for (const tag of firstTorrentTags) {
      tagCounts[tag] = 1; // First torrent has this tag
    }

    // Count occurrences of each tag across all torrents
    for (let i = 1; i < torrents.length; i++) {
      const torrent = torrents[i];
      if (!torrent.tags) continue;

      // Create a Set of this torrent's tags for O(1) lookups
      const currentTags = new Set(
        torrent.tags.split(",").map(t => t.trim()).filter(Boolean)
      );

      // Only increment count for tags that this torrent has
      for (const tag in tagCounts) {
        if (currentTags.has(tag)) {
          tagCounts[tag]++;
        }
      }
    }

    // Return tags that appear in all torrents
    return Object.keys(tagCounts).filter(tag => tagCounts[tag] === torrents.length);
  }

  // Optimized version of getCommonCategory with early returns
  const getCommonCategory = (torrents: Torrent[]): string => {
    // Early returns for common cases
    if (torrents.length === 0) return "";
    if (torrents.length === 1) return torrents[0].category || "";

    const firstCategory = torrents[0].category || "";

    // Use direct loop instead of every() for early return optimization
    for (let i = 1; i < torrents.length; i++) {
      if ((torrents[i].category || "") !== firstCategory) {
        return ""; // Different category found, no need to check the rest
      }
    }

    return firstCategory;
  }

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
          {/* Action buttons */}
          <div className="flex gap-1 sm:gap-2 flex-shrink-0">
            {(() => {
              const actions = effectiveSelectionCount > 0 ? (
                <TorrentActions
                  instanceId={instanceId}
                  selectedHashes={selectedHashes}
                  selectedTorrents={selectedTorrents}
                  onComplete={() => {
                    setRowSelection({})
                    setIsAllSelected(false)
                    setExcludedFromSelectAll(new Set())
                  }}
                  isAllSelected={isAllSelected}
                  totalSelectionCount={effectiveSelectionCount}
                  filters={filters}
                  search={effectiveSearch}
                  excludeHashes={Array.from(excludedFromSelectAll)}
                />
              ) : null
              const headerLeft = typeof document !== "undefined" ? document.getElementById("header-left-of-filter") : null
              return (
                <>
                  {/* Mobile/tablet inline (hidden on xl and up) */}
                  <div className="xl:hidden">
                    {actions}
                  </div>
                  {/* Desktop portal: render directly left of the filter button in header */}
                  {headerLeft && actions ? createPortal(actions, headerLeft) : null}
                </>
              )
            })()}

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
        <div className="relative flex-1 overflow-auto scrollbar-thin" ref={parentRef}>
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
            {torrents.length === 0 && isLoading ? (
              // Show skeleton loader for initial load
              <div className="p-8 text-center text-muted-foreground">
                <Loader2 className="h-8 w-8 animate-spin mx-auto mb-2" />
                <p>Loading torrents...</p>
              </div>
            ) : torrents.length === 0 ? (
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
                    <ContextMenu key={`${torrent.hash}-${virtualRow.index}`}>
                      <ContextMenuTrigger asChild>
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
                      </ContextMenuTrigger>
                      <ContextMenuContent>
                        <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
                          View Details
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("resume", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Play className="mr-2 h-4 w-4" />
                          Resume {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("pause", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Pause className="mr-2 h-4 w-4" />
                          Pause {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            handleRecheckClick(hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <CheckCircle className="mr-2 h-4 w-4" />
                          Force Recheck {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            handleReannounceClick(hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Radio className="mr-2 h-4 w-4" />
                          Reannounce {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        {(() => {
                          // Use selected torrents if this row is part of selection, or just this torrent
                          const useSelection = row.getIsSelected() || isAllSelected
                          const hashes = useSelection ? selectedHashes : [torrent.hash]
                          const hashCount = isAllSelected ? effectiveSelectionCount : hashes.length

                          const handleQueueAction = (action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
                            handleContextMenuAction(action, hashes)
                          }

                          return (
                            <QueueSubmenu
                              type="context"
                              hashCount={hashCount}
                              onQueueAction={handleQueueAction}
                              isPending={mutation.isPending}
                            />
                          )
                        })()}
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            const torrents = useSelection ? selectedTorrents : [torrent]

                            setContextMenuHashes(hashes)
                            setContextMenuTorrents(torrents)
                            setShowAddTagsDialog(true)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Tag className="mr-2 h-4 w-4" />
                          Add Tags {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            const torrents = useSelection ? selectedTorrents : [torrent]

                            setContextMenuHashes(hashes)
                            setContextMenuTorrents(torrents)
                            setShowTagsDialog(true)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Tag className="mr-2 h-4 w-4" />
                          Replace Tags {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]
                            const torrents = useSelection ? selectedTorrents : [torrent]

                            setContextMenuHashes(hashes)
                            setContextMenuTorrents(torrents)
                            setShowCategoryDialog(true)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Folder className="mr-2 h-4 w-4" />
                          Set Category {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        {(() => {
                          // Use selected torrents if this row is part of selection, or just this torrent
                          const useSelection = row.getIsSelected() || isAllSelected
                          const hashes = useSelection ? selectedHashes : [torrent.hash]
                          const hashCount = isAllSelected ? effectiveSelectionCount : hashes.length

                          // Create wrapped handlers that pass hashes directly
                          const handleSetShareLimitWrapper = (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
                            handleSetShareLimit(ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, hashes)
                          }

                          const handleSetSpeedLimitsWrapper = (uploadLimit: number, downloadLimit: number) => {
                            handleSetSpeedLimits(uploadLimit, downloadLimit, hashes)
                          }

                          return (
                            <>
                              <ShareLimitSubmenu
                                type="context"
                                hashCount={hashCount}
                                onConfirm={handleSetShareLimitWrapper}
                                isPending={mutation.isPending}
                              />
                              <SpeedLimitsSubmenu
                                type="context"
                                hashCount={hashCount}
                                onConfirm={handleSetSpeedLimitsWrapper}
                                isPending={mutation.isPending}
                              />
                            </>
                          )
                        })()}
                        <ContextMenuSeparator />
                        {(() => {
                          // Use selected torrents if this row is part of selection, or just this torrent
                          const useSelection = row.getIsSelected() || isAllSelected
                          const hashes = useSelection ? selectedHashes : [torrent.hash]
                          const torrents = useSelection ? selectedTorrents : [torrent]
                          const count = isAllSelected ? effectiveSelectionCount : hashes.length

                          const tmmStates = torrents.map(t => t.auto_tmm)
                          const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
                          const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
                          const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

                          if (mixed) {
                            return (
                              <>
                                <ContextMenuItem
                                  onClick={() => handleContextMenuAction("toggleAutoTMM", hashes, true)}
                                  disabled={mutation.isPending}
                                >
                                  <Sparkles className="mr-2 h-4 w-4" />
                                  Enable TMM {useSelection && count > 1 ? `(${count} Mixed)` : "(Mixed)"}
                                </ContextMenuItem>
                                <ContextMenuItem
                                  onClick={() => handleContextMenuAction("toggleAutoTMM", hashes, false)}
                                  disabled={mutation.isPending}
                                >
                                  <Settings2 className="mr-2 h-4 w-4" />
                                  Disable TMM {useSelection && count > 1 ? `(${count} Mixed)` : "(Mixed)"}
                                </ContextMenuItem>
                              </>
                            )
                          }

                          return (
                            <ContextMenuItem
                              onClick={() => handleContextMenuAction("toggleAutoTMM", hashes, !allEnabled)}
                              disabled={mutation.isPending}
                            >
                              {allEnabled ? (
                                <>
                                  <Settings2 className="mr-2 h-4 w-4" />
                                  Disable TMM {useSelection && count > 1 ? `(${count})` : ""}
                                </>
                              ) : (
                                <>
                                  <Sparkles className="mr-2 h-4 w-4" />
                                  Enable TMM {useSelection && count > 1 ? `(${count})` : ""}
                                </>
                              )}
                            </ContextMenuItem>
                          )
                        })()}
                        <ContextMenuSeparator />
                        <ContextMenuItem onClick={() => copyToClipboard(incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name, "name")}>
                          <Copy className="mr-2 h-4 w-4" />
                          Copy Name
                        </ContextMenuItem>
                        <ContextMenuItem onClick={() => copyToClipboard(torrent.hash, "hash")}>
                          <Copy className="mr-2 h-4 w-4" />
                          Copy Hash
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem
                          onClick={() => {
                            // Use selected torrents if this row is part of selection, or just this torrent
                            const useSelection = row.getIsSelected() || isAllSelected
                            const hashes = useSelection ? selectedHashes : [torrent.hash]

                            setContextMenuHashes(hashes)
                            setShowDeleteDialog(true)
                          }}
                          disabled={mutation.isPending}
                          className="text-destructive"
                        >
                          <Trash2 className="mr-2 h-4 w-4" />
                          Delete {(row.getIsSelected() || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
                        </ContextMenuItem>
                      </ContextMenuContent>
                    </ContextMenu>
                  )
                })}
              </div>
            )}
          </div>
        </div>

        {/* Status bar */}
        <div className="flex items-center justify-between p-2 border-t flex-shrink-0">
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
              <span className="font-medium">{formatSpeed(stats.totalDownloadSpeed || 0)}</span>
              <ChevronUp className="h-3 w-3 text-muted-foreground" />
              <span className="font-medium">{formatSpeed(stats.totalUploadSpeed || 0)}</span>
            </div>
          </div>



          <div className="flex items-center gap-4">
            {/* Incognito mode toggle - barely visible */}
            <button
              onClick={() => setIncognitoMode(!incognitoMode)}
              className="p-1 rounded-sm transition-all hover:bg-muted/50"
              title={incognitoMode ? "Exit incognito mode" : "Enable incognito mode"}
            >
              {incognitoMode ? (
                <EyeOff className="h-3.5 w-3.5 text-muted-foreground" />
              ) : (
                <Eye className="h-3.5 w-3.5 text-muted-foreground" />
              )}
            </button>
          </div>
        </div>
      </div>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {isAllSelected ? effectiveSelectionCount : contextMenuHashes.length} torrent(s)?</AlertDialogTitle>
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
              onClick={handleDelete}
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
        hashCount={isAllSelected ? effectiveSelectionCount : contextMenuHashes.length}
        onConfirm={handleAddTags}
        isPending={mutation.isPending}
      />

      {/* Set Tags Dialog */}
      <SetTagsDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        hashCount={isAllSelected ? effectiveSelectionCount : contextMenuHashes.length}
        onConfirm={handleSetTags}
        isPending={mutation.isPending}
        initialTags={getCommonTagsSync(contextMenuTorrents)}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories || {}}
        hashCount={isAllSelected ? effectiveSelectionCount : contextMenuHashes.length}
        onConfirm={handleSetCategory}
        isPending={mutation.isPending}
        initialCategory={getCommonCategory(contextMenuTorrents)}
      />

      {/* Remove Tags Dialog */}
      <RemoveTagsDialog
        open={showRemoveTagsDialog}
        onOpenChange={setShowRemoveTagsDialog}
        availableTags={availableTags || []}
        hashCount={isAllSelected ? effectiveSelectionCount : contextMenuHashes.length}
        onConfirm={handleRemoveTags}
        isPending={mutation.isPending}
        currentTags={getCommonTagsSync(contextMenuTorrents)}
      />

      {/* Force Recheck Confirmation Dialog */}
      <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Force Recheck {isAllSelected ? effectiveSelectionCount : contextMenuHashes.length} torrent(s)?</DialogTitle>
            <DialogDescription>
              This will force qBittorrent to recheck all pieces of the selected torrents. This process may take some time and will temporarily pause the torrents.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleRecheck} disabled={mutation.isPending}>
              Force Recheck
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reannounce Confirmation Dialog */}
      <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reannounce {isAllSelected ? effectiveSelectionCount : contextMenuHashes.length} torrent(s)?</DialogTitle>
            <DialogDescription>
              This will force the selected torrents to reannounce to all their trackers. This is useful when trackers are not responding or you want to refresh your connection.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
              Cancel
            </Button>
            <Button onClick={handleReannounce} disabled={mutation.isPending}>
              Reannounce
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
});