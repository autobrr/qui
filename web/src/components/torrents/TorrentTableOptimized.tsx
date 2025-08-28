/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import React, { memo, useState, useMemo, useRef, useCallback, useEffect } from "react"
import {
  useReactTable,
  getCoreRowModel,
  flexRender
} from "@tanstack/react-table"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  DndContext,
  closestCenter,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  type DragEndEvent
} from "@dnd-kit/core"
import {
  SortableContext,
  horizontalListSortingStrategy,
  arrayMove
} from "@dnd-kit/sortable"
import { restrictToHorizontalAxis } from "@dnd-kit/modifiers"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { useDebounce } from "@/hooks/useDebounce"
import { usePersistedColumnVisibility } from "@/hooks/usePersistedColumnVisibility"
import { usePersistedColumnOrder } from "@/hooks/usePersistedColumnOrder"
import { usePersistedColumnSizing } from "@/hooks/usePersistedColumnSizing"
import { usePersistedColumnSorting } from "@/hooks/usePersistedColumnSorting"
import { usePersistedDeleteFiles } from "@/hooks/usePersistedDeleteFiles"

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

import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
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
import { AddTorrentDialog } from "./AddTorrentDialog"
import { TorrentActions } from "./TorrentActions"
import { Loader2, Play, Pause, Trash2, CheckCircle, Copy, Tag, Folder, Columns3, Radio, Eye, EyeOff, ChevronDown, ChevronUp, Settings2, Sparkles } from "lucide-react"
import { createPortal } from "react-dom"
import { SetTagsDialog, SetCategoryDialog, RemoveTagsDialog } from "./TorrentDialogs"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"
import { QueueSubmenu } from "./QueueSubmenu"
import { DraggableTableHeader } from "./DraggableTableHeader"
import type { Torrent, TorrentCounts, Category } from "@/types"
import {
  getLinuxIsoName,
  useIncognitoMode
} from "@/lib/incognito"
import { formatSpeed } from "@/lib/utils"
import { applyOptimisticUpdates } from "@/lib/torrent-state-utils"
import { useSearch } from "@tanstack/react-router"
import { createColumns } from "./TorrentTableColumns"

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
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showRemoveTagsDialog, setShowRemoveTagsDialog] = useState(false)
  const [showRefetchIndicator, setShowRefetchIndicator] = useState(false)

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

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
    isLoadingMore,
    hasLoadedAll,
    loadMore: loadMoreTorrents,
    isCachedData,
    isStaleData,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters,
    sort: sorting.length > 0 ? getBackendSortField(sorting[0].id) : "added_on",
    order: sorting.length > 0 ? (sorting[0].desc ? "desc" : "asc") : "desc",
  })
  
  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags)
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

  // Memoize columns to avoid unnecessary recalculations
  const columns = useMemo(
    () => createColumns(incognitoMode, { shiftPressedRef, lastSelectedIndexRef }),
    [incognitoMode]
  )

  const table = useReactTable({
    data: sortedTorrents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    // Use torrent hash as stable row ID
    getRowId: (row: Torrent) => row.hash,
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
  })

  // Get selected torrent hashes
  const selectedHashes = useMemo((): string[] => {
    return Object.keys(rowSelection)
      .filter((key: string) => (rowSelection as Record<string, boolean>)[key])
  }, [rowSelection])
  
  // Get selected torrents
  const selectedTorrents = useMemo((): Torrent[] => {
    return selectedHashes
      .map((hash: string) => sortedTorrents.find((t: Torrent) => t.hash === hash))
      .filter(Boolean) as Torrent[]
  }, [selectedHashes, sortedTorrents])

  // Virtualization setup with progressive loading
  const { rows } = table.getRowModel()
  const parentRef = useRef<HTMLDivElement>(null)
  
  // Load more rows as user scrolls (progressive loading)
  const loadMore = useCallback((): void => {
    // Prevent concurrent loads
    if (isLoadingMoreRows) return
    
    setIsLoadingMoreRows(true)
    
    // Use functional update to avoid stale closure
    setLoadedRows(prev => {
      const newLoadedRows = Math.min(prev + 100, sortedTorrents.length)
      
      // If we're near the end of loaded torrents and haven't loaded all from server
      if (newLoadedRows >= sortedTorrents.length - 50 && !hasLoadedAll && !isLoadingMore) {
        loadMoreTorrents()
      }
      
      return newLoadedRows
    })
    
    // Reset loading flag after a short delay
    setTimeout(() => setIsLoadingMoreRows(false), 100)
  }, [sortedTorrents.length, hasLoadedAll, isLoadingMore, loadMoreTorrents, isLoadingMoreRows])

  // Ensure loadedRows never exceeds actual data length
  const safeLoadedRows = Math.min(loadedRows, rows.length)
  
  // useVirtualizer must be called at the top level, not inside useMemo
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 40,
    // Reduce overscan for large datasets to minimize DOM nodes
    overscan: sortedTorrents.length > 10000 ? 5 : 20,
    // Provide a key to help with item tracking
    getItemKey: useCallback((index: number) => rows[index]?.id || `row-${index}`, [rows]),
    // Use a debounced onChange to prevent excessive rendering
    onChange: (instance) => {
      const vRows = instance.getVirtualItems();
      
      // Check if we need to load more first (no need to wait for debounce)
      const lastItem = vRows.at(-1);
      if (lastItem && lastItem.index >= safeLoadedRows - 50 && safeLoadedRows < rows.length) {
        loadMore();
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
      } else if (sortedTorrents.length < prev) {
        // Data reduced, cap at new length
        return sortedTorrents.length
      } else if (prev < targetRows) {
        // Not enough rows loaded, load at least 100
        return targetRows
      }
      return prev
    })
    
    // Force virtualizer to recalculate
    virtualizer.measure()
  }, [sortedTorrents.length, virtualizer])

  // Reset when filters or search changes
  useEffect(() => {
    const targetRows = Math.min(100, sortedTorrents.length || 0)
    setLoadedRows(targetRows)
    setIsLoadingMoreRows(false)
    
    // Scroll to top and force virtualizer recalculation
    if (parentRef.current) {
      parentRef.current.scrollTop = 0
    }
    
    // Force virtualizer to recalculate after a micro-task
    setTimeout(() => {
      virtualizer.scrollToOffset(0)
      virtualizer.measure()
    }, 0)
  }, [filters, effectiveSearch, instanceId, virtualizer, sortedTorrents.length])


  // Mutation for bulk actions
  const mutation = useMutation({
    mutationFn: (data: {
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "addTags" | "removeTags" | "setTags" | "setCategory" | "toggleAutoTMM" | "setShareLimit" | "setUploadLimit" | "setDownloadLimit"
      deleteFiles?: boolean
      hashes: string[]
      tags?: string
      category?: string
      enable?: boolean
      ratioLimit?: number
      seedingTimeLimit?: number
      inactiveSeedingTimeLimit?: number
      uploadLimit?: number
      downloadLimit?: number
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
      })
    },
    onSuccess: async (_: unknown, variables: {
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
    }) => {
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
        // For pause/resume, optimistically update the cache immediately
        if (variables.action === "pause" || variables.action === "resume") {
          // Get all cached queries for this instance
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ["torrents-list", instanceId],
            exact: false,
          })
          
          // Optimistically update torrent states in all cached queries
          queries.forEach((query) => {
            queryClient.setQueryData(query.queryKey, (oldData: {
              torrents?: Torrent[]
              total?: number
              totalCount?: number
            }) => {
              if (!oldData?.torrents) return oldData
              
              // Check if this query has a status filter in its key
              // Query key structure: ['torrents-list', instanceId, currentPage, filters, search]
              const queryKey = query.queryKey as unknown[]
              const filters = queryKey[3] as { status?: string[] } | undefined // filters is at index 3
              const statusFilters = filters?.status || []
              
              // Apply optimistic updates using our utility function
              const { torrents: updatedTorrents } = applyOptimisticUpdates(
                oldData.torrents,
                variables.hashes,
                variables.action as "pause" | "resume", // Type narrowed by if condition above
                statusFilters
              )
              
              return {
                ...oldData,
                torrents: updatedTorrents,
                total: updatedTorrents.length,
                totalCount: updatedTorrents.length,
              }
            })
          })
          
          // Note: torrent-counts are handled server-side now, no need for optimistic updates
        }
        
        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const delay = variables.action === "resume" ? 2000 : 1000
        
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
        }, delay)
        setContextMenuHashes([])
      }
    },
  })

  const handleDelete = async () => {
    await mutation.mutateAsync({ 
      action: "delete", 
      deleteFiles,
      hashes: contextMenuHashes, 
    })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
    setContextMenuHashes([])
  }
  
  const handleSetTags = async (tags: string[]) => {
    // Use setTags action (with fallback to addTags for older versions)
    // The backend will handle the version check
    try {
      await mutation.mutateAsync({ 
        hashes: contextMenuHashes,
        action: "setTags", 
        tags: tags.join(","), 
      })
    } catch (error) {
      // If setTags fails due to version requirement, fall back to addTags
      if ((error as Error).message?.includes("requires qBittorrent")) {
        await mutation.mutateAsync({ 
          hashes: contextMenuHashes,
          action: "addTags", 
          tags: tags.join(","), 
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
      hashes: contextMenuHashes,
    })
    setShowCategoryDialog(false)
    setContextMenuHashes([])
  }
  
  const handleRemoveTags = async (tags: string[]) => {
    await mutation.mutateAsync({ 
      action: "removeTags",
      tags: tags.join(","),
      hashes: contextMenuHashes,
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
              const actions = selectedHashes.length > 0 ? (
                <TorrentActions 
                  instanceId={instanceId} 
                  selectedHashes={selectedHashes}
                  selectedTorrents={selectedTorrents}
                  onComplete={() => setRowSelection({})}
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
                    <ContextMenu key={torrent.hash}>
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
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("resume", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Play className="mr-2 h-4 w-4" />
                          Resume {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("pause", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Pause className="mr-2 h-4 w-4" />
                          Pause {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("recheck", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <CheckCircle className="mr-2 h-4 w-4" />
                          Force Recheck {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction("reannounce", hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Radio className="mr-2 h-4 w-4" />
                          Reannounce {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        {(() => {
                          const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                          const hashCount = hashes.length
                          
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
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            const torrents = row.getIsSelected() ? selectedTorrents : [torrent]
                            setContextMenuHashes(hashes)
                            setContextMenuTorrents(torrents)
                            setShowTagsDialog(true)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Tag className="mr-2 h-4 w-4" />
                          Manage Tags {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            const torrents = row.getIsSelected() ? selectedTorrents : [torrent]
                            setContextMenuHashes(hashes)
                            setContextMenuTorrents(torrents)
                            setShowCategoryDialog(true)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Folder className="mr-2 h-4 w-4" />
                          Set Category {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        {(() => {
                          const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                          const hashCount = hashes.length
                          
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
                          const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                          const torrents = row.getIsSelected() ? selectedTorrents : [torrent]
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
                                  Enable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length} Mixed)` : "(Mixed)"}
                                </ContextMenuItem>
                                <ContextMenuItem
                                  onClick={() => handleContextMenuAction("toggleAutoTMM", hashes, false)}
                                  disabled={mutation.isPending}
                                >
                                  <Settings2 className="mr-2 h-4 w-4" />
                                  Disable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length} Mixed)` : "(Mixed)"}
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
                                  Disable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
                                </>
                              ) : (
                                <>
                                  <Sparkles className="mr-2 h-4 w-4" />
                                  Enable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
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
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            setContextMenuHashes(hashes)
                            setShowDeleteDialog(true)
                          }}
                          disabled={mutation.isPending}
                          className="text-destructive"
                        >
                          <Trash2 className="mr-2 h-4 w-4" />
                          Delete {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ""}
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
                {totalCount} torrent{totalCount !== 1 ? "s" : ""}
                {safeLoadedRows < rows.length && ` • ${safeLoadedRows} loaded in viewport`}
                {safeLoadedRows < rows.length && " (scroll for more)"}
                {isLoadingMore && " • Loading more from server..."}
              </>
            )}
            {selectedHashes.length > 0 && (
              <>
                <span className="ml-2">
                  ({selectedHashes.length} selected)
                </span>
                <button
                  onClick={() => setRowSelection({})}
                  className="ml-2 text-xs text-primary hover:text-foreground transition-colors underline-offset-4 hover:underline"
                >
                  Clear selection
                </button>
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
            <AlertDialogTitle>Delete {contextMenuHashes.length} torrent(s)?</AlertDialogTitle>
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

      {/* Set Tags Dialog */}
      <SetTagsDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        hashCount={contextMenuHashes.length}
        onConfirm={handleSetTags}
        isPending={mutation.isPending}
        initialTags={getCommonTagsSync(contextMenuTorrents)}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories || {}}
        hashCount={contextMenuHashes.length}
        onConfirm={handleSetCategory}
        isPending={mutation.isPending}
        initialCategory={getCommonCategory(contextMenuTorrents)}
      />

      {/* Remove Tags Dialog */}
      <RemoveTagsDialog
        open={showRemoveTagsDialog}
        onOpenChange={setShowRemoveTagsDialog}
        availableTags={availableTags || []}
        hashCount={contextMenuHashes.length}
        onConfirm={handleRemoveTags}
        isPending={mutation.isPending}
        currentTags={getCommonTagsSync(contextMenuTorrents)}
      />
    </div>
  )
});