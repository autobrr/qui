/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: MIT
 */

import React, { memo, useState, useMemo, useRef, useCallback, useEffect } from 'react'
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  type ColumnDef,
} from '@tanstack/react-table'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  DndContext,
  closestCenter,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from '@dnd-kit/core'
import {
  SortableContext,
  horizontalListSortingStrategy,
  arrayMove,
} from '@dnd-kit/sortable'
import { restrictToHorizontalAxis } from '@dnd-kit/modifiers'
import { useTorrentsList } from '@/hooks/useTorrentsList'
import { useDebounce } from '@/hooks/useDebounce'
import { usePersistedColumnVisibility } from '@/hooks/usePersistedColumnVisibility'
import { usePersistedColumnOrder } from '@/hooks/usePersistedColumnOrder'
import { usePersistedColumnSizing } from '@/hooks/usePersistedColumnSizing'
import { usePersistedColumnSorting } from '@/hooks/usePersistedColumnSorting'

// Virtual scrolling and progressive loading constants
const VIRTUAL_CONSTANTS = {
  INITIAL_LOADED_ROWS: 100,
  LOAD_MORE_BATCH_SIZE: 100,
  LOAD_MORE_THRESHOLD: 50,
  ROW_HEIGHT: 40,
  OVERSCAN_SMALL: 20,
  OVERSCAN_LARGE: 5,
  LARGE_DATASET_THRESHOLD: 10000,
  // No more manual reset constants - fully automatic virtual table
} as const

// Search and UI constants
const UI_CONSTANTS = {
  SEARCH_DEBOUNCE_MS: 200,
  REFETCH_INDICATOR_DELAY_MS: 2000,
  VIRTUALIZER_MEASURE_DELAY_MS: 0,
} as const

// Mutation timing constants
const MUTATION_CONSTANTS = {
  DELETE_REFETCH_DELAY_MS: 2000,
  DELETE_FILES_REFETCH_DELAY_MS: 5000,
  RESUME_DELAY_MS: 2000,
  DEFAULT_OPERATION_DELAY_MS: 1000,
  LOADING_FLAG_RESET_MS: 100,
} as const

// Mathematical constants
const MATH_CONSTANTS = {
  PERCENTAGE_MULTIPLIER: 100,
  MILLISECONDS_PER_SECOND: 1000,
  RATIO_PRECISION: 2,
  RATIO_INFINITY_VALUE: -1,
  PAD_LENGTH: 2,
  PAD_CHAR: '0',
} as const

// Time formatting constants
const TIME_CONSTANTS = {
  INFINITY_SECONDS: 8640000,
  SECONDS_PER_HOUR: 3600,
  SECONDS_PER_MINUTE: 60,
  HOURS_PER_DAY: 24,
} as const

// Column sizing constants
const COLUMN_CONSTANTS = {
  SELECT_COLUMN_WIDTH: 40,
  PRIORITY_COLUMN_WIDTH: 45,
  NAME_COLUMN_WIDTH: 200,
  SIZE_COLUMN_WIDTH: 85,
  PROGRESS_COLUMN_WIDTH: 120,
  STATUS_COLUMN_WIDTH: 120,
  ETA_COLUMN_WIDTH: 80,
  RATIO_COLUMN_WIDTH: 80,
  ADDED_COLUMN_WIDTH: 200,
  CATEGORY_COLUMN_WIDTH: 150,
  TAGS_COLUMN_WIDTH: 200,
  SAVE_PATH_COLUMN_WIDTH: 250,
  TRACKER_COLUMN_WIDTH: 150,
  MIN_COLUMN_PADDING: 48,
  CHAR_WIDTH_ESTIMATE: 7.5,
  SORT_INDICATOR_PADDING: 20,
  MIN_COLUMN_WIDTH: 60,
} as const

// Default values for persisted state hooks (module scope for stable references)
const DEFAULT_COLUMN_VISIBILITY = {
  downloaded: false,
  uploaded: false,
  save_path: false, // Fixed: was 'saveLocation', should match column accessorKey
  tracker: false,
  priority: false,
}
const DEFAULT_COLUMN_SIZING = {}

// Helper function to get default column order (module scope for stable reference)
function getDefaultColumnOrder(): string[] {
  const cols = createColumns(false)
  return cols.map(col => {
    if ('id' in col && col.id) return col.id
    if ('accessorKey' in col && typeof col.accessorKey === 'string') return col.accessorKey
    return null
  }).filter((v): v is string => typeof v === 'string')
}

import { useInstanceMetadata } from '@/hooks/useInstanceMetadata'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Progress } from '@/components/ui/progress'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from '@/components/ui/context-menu'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { AddTorrentDialog } from './AddTorrentDialog'
import { TorrentActions } from './TorrentActions'
import { Loader2, Play, Pause, Trash2, CheckCircle, Copy, Tag, Folder, Search, Info, Columns3, Radio, ArrowUp, ArrowDown, ChevronsUp, ChevronsDown, Eye, EyeOff, Plus, ChevronDown, ChevronUp, ListOrdered, Settings2, Sparkles } from 'lucide-react'
import { SetTagsDialog, SetCategoryDialog, RemoveTagsDialog } from './TorrentDialogs'
import { DraggableTableHeader } from './DraggableTableHeader'
import type { Torrent, TorrentCounts, Category } from '@/types'
import {
  getLinuxIsoName,
  getLinuxCategory,
  getLinuxTags,
  getLinuxSavePath,
  getLinuxTracker,
  getLinuxRatio,
  useIncognitoMode,
} from '@/lib/incognito'
import { formatBytes, formatSpeed, getRatioColor } from '@/lib/utils'
import { applyOptimisticUpdates } from '@/lib/torrent-state-utils'

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


function formatEta(seconds: number): string {
  if (seconds === TIME_CONSTANTS.INFINITY_SECONDS) return '∞'
  if (seconds < 0) return ''
  
  const hours = Math.floor(seconds / TIME_CONSTANTS.SECONDS_PER_HOUR)
  const minutes = Math.floor((seconds % TIME_CONSTANTS.SECONDS_PER_HOUR) / TIME_CONSTANTS.SECONDS_PER_MINUTE)
  
  if (hours > TIME_CONSTANTS.HOURS_PER_DAY) {
    const days = Math.floor(hours / TIME_CONSTANTS.HOURS_PER_DAY)
    return `${days}d ${hours % TIME_CONSTANTS.HOURS_PER_DAY}h`
  }
  
  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }
  
  return `${minutes}m`
}

// Calculate minimum column width based on header text
function calculateMinWidth(text: string, padding: number = COLUMN_CONSTANTS.MIN_COLUMN_PADDING): number {
  // Approximate character width in pixels for text-sm (14px) with font-medium
  const charWidth = COLUMN_CONSTANTS.CHAR_WIDTH_ESTIMATE
  // Add padding for sort indicator
  const extraPadding = COLUMN_CONSTANTS.SORT_INDICATOR_PADDING
  return Math.max(COLUMN_CONSTANTS.MIN_COLUMN_WIDTH, Math.ceil(text.length * charWidth) + padding + extraPadding)
}

const createColumns = (incognitoMode: boolean): ColumnDef<Torrent>[] => [
  {
    id: 'select',
    header: ({ table }) => (
      <div className="flex items-center justify-center p-1 -m-1">
        <Checkbox
          checked={table.getIsAllPageRowsSelected()}
          onCheckedChange={(checked) => table.toggleAllPageRowsSelected(!!checked)}
          aria-label="Select all"
          className="hover:border-ring cursor-pointer transition-colors"
        />
      </div>
    ),
    cell: ({ row }) => (
      <div className="flex items-center justify-center p-1 -m-1">
        <Checkbox
          checked={row.getIsSelected()}
          onCheckedChange={(checked) => row.toggleSelected(!!checked)}
          aria-label="Select row"
          className="hover:border-ring cursor-pointer transition-colors"
        />
      </div>
    ),
    size: COLUMN_CONSTANTS.SELECT_COLUMN_WIDTH,
    enableResizing: false,
  },
  {
    accessorKey: 'priority',
    header: () => (
      <Tooltip>
        <TooltipTrigger asChild>
          <div className="flex items-center justify-center">
            <ListOrdered className="h-4 w-4" />
          </div>
        </TooltipTrigger>
        <TooltipContent>Priority</TooltipContent>
      </Tooltip>
    ),
    meta: {
      headerString: 'Priority' // For the column visibility dropdown
    },
    cell: ({ row }) => {
      const priority = row.original.priority
      // Priority 0 means torrent is not queued/managed
      if (priority === 0) return <span className="text-sm text-muted-foreground text-center block">-</span>
      // In qBittorrent, 1 is highest priority, higher numbers are lower priority
      return <span className="text-sm font-medium text-center block">{priority}</span>
    },
    size: COLUMN_CONSTANTS.PRIORITY_COLUMN_WIDTH,
  },
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => {
      const displayName = incognitoMode ? getLinuxIsoName(row.original.hash) : row.original.name
      const safeName = displayName || row.original.name || 'Unknown'
      return (
        <div className="truncate text-sm" title={safeName}>
          {safeName}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.NAME_COLUMN_WIDTH,
  },
  {
    accessorKey: 'size',
    header: 'Size',
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.size)}</span>,
    size: COLUMN_CONSTANTS.SIZE_COLUMN_WIDTH,
  },
  {
    accessorKey: 'progress',
    header: 'Progress',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Progress value={row.original.progress * MATH_CONSTANTS.PERCENTAGE_MULTIPLIER} className="w-20" />
        <span className="text-xs text-muted-foreground">
          {Math.round(row.original.progress * MATH_CONSTANTS.PERCENTAGE_MULTIPLIER)}%
        </span>
      </div>
    ),
    size: COLUMN_CONSTANTS.PROGRESS_COLUMN_WIDTH,
  },
  {
    accessorKey: 'state',
    header: 'Status',
    cell: ({ row }) => {
      const state = row.original.state
      const variant = 
        state === 'downloading' ? 'default' :
        state === 'stalledDL' ? 'secondary' :
        state === 'uploading' ? 'default' :
        state === 'stalledUP' ? 'secondary' :
        state === 'pausedDL' || state === 'pausedUP' ? 'secondary' :
        state === 'error' || state === 'missingFiles' ? 'destructive' :
        'outline'
      
      return <Badge variant={variant} className="text-xs">{state}</Badge>
    },
    size: COLUMN_CONSTANTS.STATUS_COLUMN_WIDTH,
  },
  {
    accessorKey: 'dlspeed',
    header: 'Down Speed',
    cell: ({ row }) => <span className="text-sm truncate">{formatSpeed(row.original.dlspeed)}</span>,
    size: calculateMinWidth('Down Speed'),
  },
  {
    accessorKey: 'upspeed',
    header: 'Up Speed',
    cell: ({ row }) => <span className="text-sm truncate">{formatSpeed(row.original.upspeed)}</span>,
    size: calculateMinWidth('Up Speed'),
  },
  {
    accessorKey: 'eta',
    header: 'ETA',
    cell: ({ row }) => <span className="text-sm truncate">{formatEta(row.original.eta)}</span>,
    size: COLUMN_CONSTANTS.ETA_COLUMN_WIDTH,
  },
  {
    accessorKey: 'ratio',
    header: 'Ratio',
    cell: ({ row }) => {
      const ratio = incognitoMode ? getLinuxRatio(row.original.hash) : row.original.ratio
      const displayRatio = ratio === MATH_CONSTANTS.RATIO_INFINITY_VALUE ? "∞" : ratio.toFixed(MATH_CONSTANTS.RATIO_PRECISION)
      const colorVar = getRatioColor(ratio)
      
      return (
        <span 
          className="text-sm font-medium" 
          style={{ color: colorVar }}
        >
          {displayRatio}
        </span>
      )
    },
    size: COLUMN_CONSTANTS.RATIO_COLUMN_WIDTH,
  },
  {
    accessorKey: 'added_on',
    header: 'Added',
    cell: ({ row }) => {
      const addedOn = row.original.added_on
      if (!addedOn || addedOn === 0) {
        return '-'
      }
      const date = new Date(addedOn * MATH_CONSTANTS.MILLISECONDS_PER_SECOND) // Convert from Unix timestamp
      
      // Format: M/D/YYYY, h:mm:ss AM/PM
      const month = date.getMonth() + 1 // getMonth() returns 0-11
      const day = date.getDate()
      const year = date.getFullYear()
      const hours = date.getHours()
      const minutes = date.getMinutes()
      const seconds = date.getSeconds()
      const ampm = hours >= 12 ? 'PM' : 'AM'
      const displayHours = hours % 12 || 12 // Convert to 12-hour format
      
      return (
        <div className="whitespace-nowrap text-sm">
          {month}/{day}/{year}, {displayHours}:{minutes.toString().padStart(MATH_CONSTANTS.PAD_LENGTH, MATH_CONSTANTS.PAD_CHAR)}:{seconds.toString().padStart(MATH_CONSTANTS.PAD_LENGTH, MATH_CONSTANTS.PAD_CHAR)} {ampm}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.ADDED_COLUMN_WIDTH,
  },
  {
    accessorKey: 'category',
    header: 'Category',
    cell: ({ row }) => {
      const displayCategory = incognitoMode ? getLinuxCategory(row.original.hash) : row.original.category
      return (
        <div className="truncate text-sm" title={displayCategory || '-'}>
          {displayCategory || '-'}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.CATEGORY_COLUMN_WIDTH,
  },
  {
    accessorKey: 'tags',
    header: 'Tags',
    cell: ({ row }) => {
      const tags = incognitoMode ? getLinuxTags(row.original.hash) : row.original.tags
      const displayTags = Array.isArray(tags) ? tags.join(', ') : tags || '-'
      return (
        <div className="truncate text-sm" title={displayTags}>
          {displayTags}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.TAGS_COLUMN_WIDTH,
  },
  {
    accessorKey: 'downloaded',
    header: 'Downloaded',
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.downloaded)}</span>,
    size: calculateMinWidth('Downloaded'),
  },
  {
    accessorKey: 'uploaded',
    header: 'Uploaded',
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.uploaded)}</span>,
    size: calculateMinWidth('Uploaded'),
  },
  {
    accessorKey: 'save_path',
    header: 'Save Path',
    cell: ({ row }) => {
      const displayPath = incognitoMode ? getLinuxSavePath(row.original.hash) : row.original.save_path
      return (
        <div className="truncate text-sm" title={displayPath}>
          {displayPath}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.SAVE_PATH_COLUMN_WIDTH,
  },
  {
    accessorKey: 'tracker',
    header: 'Tracker',
    cell: ({ row }) => {
      const tracker = incognitoMode ? getLinuxTracker(row.original.hash) : row.original.tracker
      // Extract domain from tracker URL
      let displayTracker = tracker
      try {
        if (tracker && tracker.includes('://')) {
          const url = new URL(tracker)
          displayTracker = url.hostname
        }
      } catch {
        // If URL parsing fails, show as is
      }
      return (
        <div className="truncate text-sm" title={tracker}>
          {displayTracker || '-'}
        </div>
      )
    },
    size: COLUMN_CONSTANTS.TRACKER_COLUMN_WIDTH,
  },
]

export const TorrentTableOptimized = memo(function TorrentTableOptimized({ instanceId, filters, selectedTorrent, onTorrentSelect, addTorrentModalOpen, onAddTorrentModalChange, onFilteredDataUpdate, filterButton }: TorrentTableOptimizedProps) {
  // State management
  // Move default values outside the component for stable references
  // (This should be at module scope, not inside the component)
  const [sorting, setSorting] = usePersistedColumnSorting([])
  const [globalFilter, setGlobalFilter] = useState('')
  const [immediateSearch, setImmediateSearch] = useState('')
  const [rowSelection, setRowSelection] = useState({})
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [contextMenuHashes, setContextMenuHashes] = useState<string[]>([])
  const [contextMenuTorrents, setContextMenuTorrents] = useState<Torrent[]>([])
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [showRemoveTagsDialog, setShowRemoveTagsDialog] = useState(false)
  const [showRefetchIndicator, setShowRefetchIndicator] = useState(false)

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

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
  const [loadedRows, setLoadedRows] = useState<number>(VIRTUAL_CONSTANTS.INITIAL_LOADED_ROWS)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)
  
  // Query client for invalidating queries
  const queryClient = useQueryClient()

  // Fetch metadata using shared hook
  const { data: metadata } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}

  // Debounce search to prevent excessive filtering
  const debouncedSearch = useDebounce(globalFilter, UI_CONSTANTS.SEARCH_DEBOUNCE_MS)

  // Use immediate search if available, otherwise use debounced search
  const effectiveSearch = immediateSearch || debouncedSearch

  // Check if search contains glob patterns
  const isGlobSearch = !!globalFilter && /[*?[\]]/.test(globalFilter)

  // Map TanStack Table column IDs to backend field names
  const getBackendSortField = (columnId: string): string => {
    const mapping: Record<string, string> = {
      'priority': 'priority',
      'name': 'name', 
      'size': 'size',
      'progress': 'progress',
      'state': 'state',
      'dlspeed': 'dlspeed',
      'upspeed': 'upspeed',
      'eta': 'eta',
      'ratio': 'ratio',
      'added_on': 'added_on',
      'category': 'category',
      'tags': 'tags',
      'downloaded': 'downloaded',
      'uploaded': 'uploaded',
      'save_path': 'save_path',
      'tracker': 'tracker'
    }
    return mapping[columnId] || 'added_on'
  }

  // Fetch torrents data with backend sorting
  const { 
    torrents, 
    totalCount, 
    stats, 
    counts,
    categories,
    tags,
    serverState, 
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
    sort: sorting.length > 0 ? getBackendSortField(sorting[0].id) : 'added_on',
    order: sorting.length > 0 ? (sorting[0].desc ? 'desc' : 'asc') : 'desc',
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
      }, UI_CONSTANTS.REFETCH_INDICATOR_DELAY_MS)
    } else {
      setShowRefetchIndicator(false)
    }
    
    return () => clearTimeout(timeoutId)
  }, [isFetching, isLoading, torrents.length])
  
  // Handle Enter key for immediate search
  // Memoize handlers to avoid unnecessary re-renders
  const handleSearchKeyDown = useCallback((e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      setImmediateSearch(globalFilter)
    }
  }, [globalFilter]) 

  const handleSearchChange = useCallback((value: string) => {
    setGlobalFilter(value)
    if (immediateSearch) {
      setImmediateSearch('')
    }
  }, [immediateSearch]) 

  // Use torrents directly from backend (already sorted)
  const sortedTorrents = torrents

  // Memoize columns to avoid unnecessary recalculations
  const columns = useMemo(() => createColumns(incognitoMode), [incognitoMode])
  
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
    columnResizeMode: 'onChange' as const,
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
      const newLoadedRows = Math.min(prev + VIRTUAL_CONSTANTS.LOAD_MORE_BATCH_SIZE, sortedTorrents.length)
      
      // If we're near the end of loaded torrents and haven't loaded all from server
      if (newLoadedRows >= sortedTorrents.length - VIRTUAL_CONSTANTS.LOAD_MORE_THRESHOLD && !hasLoadedAll && !isLoadingMore) {
        loadMoreTorrents()
      }
      
      return newLoadedRows
    })
    
    // Reset loading flag after a short delay
    setTimeout(() => setIsLoadingMoreRows(false), MUTATION_CONSTANTS.LOADING_FLAG_RESET_MS)
  }, [sortedTorrents.length, hasLoadedAll, isLoadingMore, loadMoreTorrents, isLoadingMoreRows])

  // Ensure loadedRows never exceeds actual data length
  const safeLoadedRows = Math.min(loadedRows, rows.length)
  
  // useVirtualizer must be called at the top level, not inside useMemo
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
    getScrollElement: () => parentRef.current,
    estimateSize: () => VIRTUAL_CONSTANTS.ROW_HEIGHT,
    // Reduce overscan for large datasets to minimize DOM nodes
    overscan: sortedTorrents.length > VIRTUAL_CONSTANTS.LARGE_DATASET_THRESHOLD ? VIRTUAL_CONSTANTS.OVERSCAN_LARGE : VIRTUAL_CONSTANTS.OVERSCAN_SMALL,
    // Provide a key to help with item tracking
    getItemKey: useCallback((index: number) => rows[index]?.id || `row-${index}`, [rows]),
    // Use a debounced onChange to prevent excessive rendering
    onChange: (instance: any) => {
      const vRows = instance.getVirtualItems();
      
      // Only load more as user scrolls - no manual shrinking
      const lastItem = vRows.at(-1);
      if (lastItem && lastItem.index >= safeLoadedRows - VIRTUAL_CONSTANTS.LOAD_MORE_THRESHOLD && safeLoadedRows < rows.length) {
        loadMore();
      }
      
      // Virtual table should be fully automatic - no manual position jumping
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
  }, [table, columnSizing, columnVisibility, columnOrder])

  // Derive hidden columns state from table API for accuracy
  const hasHiddenColumns = useMemo(() => {
    return table.getAllLeafColumns().filter(c => c.getCanHide()).some(c => !c.getIsVisible())
  }, [table, columnVisibility])

  // Reset loaded rows when data changes significantly
  useEffect(() => {
    // Always ensure loadedRows is at least INITIAL_LOADED_ROWS (or total length if less)
    const targetRows = Math.min(VIRTUAL_CONSTANTS.INITIAL_LOADED_ROWS, sortedTorrents.length)
    
    setLoadedRows(prev => {
      if (sortedTorrents.length === 0) {
        // No data, reset to 0
        return 0
      } else if (prev === 0) {
        // Initial load
        return targetRows
      } else if (sortedTorrents.length < prev) {
        // Data reduced significantly, reset to reasonable size
        return Math.min(targetRows, sortedTorrents.length)
      } else if (prev < targetRows) {
        // Not enough rows loaded, load at least INITIAL_LOADED_ROWS
        return targetRows
      } else if (prev > sortedTorrents.length * 0.5 && targetRows <= VIRTUAL_CONSTANTS.INITIAL_LOADED_ROWS) {
        // If we had loaded more than half the total items and now have few items, reset
        // This handles cases where filters dramatically reduce the dataset
        return targetRows
      }
      return prev
    })
    
    // Force virtualizer to recalculate
    virtualizer.measure()
  }, [sortedTorrents.length, virtualizer])

  // Reset when filters or search changes
  useEffect(() => {
    const targetRows = Math.min(VIRTUAL_CONSTANTS.INITIAL_LOADED_ROWS, sortedTorrents.length || 0)
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
  }, [filters, effectiveSearch, instanceId, virtualizer])


  // Mutation for bulk actions
  const mutation = useMutation({
    mutationFn: (data: {
      action: 'pause' | 'resume' | 'delete' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'addTags' | 'removeTags' | 'setTags' | 'setCategory' | 'toggleAutoTMM'
      deleteFiles?: boolean
      hashes: string[]
      tags?: string
      category?: string
      enable?: boolean
    }) => {
      return api.bulkAction(instanceId, {
        hashes: data.hashes,
        action: data.action,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
      })
    },
    onSuccess: async (_: unknown, variables: {
      action: 'pause' | 'resume' | 'delete' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'addTags' | 'removeTags' | 'setTags' | 'setCategory' | 'toggleAutoTMM'
      hashes: string[]
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
    }) => {
      // For delete operations, optimistically remove from UI immediately
      if (variables.action === 'delete') {
        // Clear selection and context menu immediately
        setRowSelection({})
        setContextMenuHashes([])
        
        // Optimistically remove torrents from ALL cached queries for this instance
        // This includes all pages, filters, and search variations
        const cache = queryClient.getQueryCache()
        const queries = cache.findAll({
          queryKey: ['torrents-list', instanceId],
          exact: false
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
              totalCount: Math.max(0, (oldData.totalCount || oldData.total || 0) - variables.hashes.length)
            }
          })
        })
        
        // Refetch later to sync with actual server state (don't invalidate!)
        // Longer delay when deleting files from disk
        const refetchDelay = variables.deleteFiles ? MUTATION_CONSTANTS.DELETE_FILES_REFETCH_DELAY_MS : MUTATION_CONSTANTS.DELETE_REFETCH_DELAY_MS
        setTimeout(() => {
          // Use refetch instead of invalidate to keep showing data
          queryClient.refetchQueries({ 
            queryKey: ['torrents-list', instanceId],
            exact: false,
            type: 'active' // Only refetch if component is mounted
          })
          // Also refetch the counts query
          queryClient.refetchQueries({ 
            queryKey: ['torrent-counts', instanceId],
            exact: false,
            type: 'active'
          })
        }, refetchDelay)
      } else {
        // For pause/resume, optimistically update the cache immediately
        if (variables.action === 'pause' || variables.action === 'resume') {
          // Get all cached queries for this instance
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ['torrents-list', instanceId],
            exact: false
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
                variables.action as 'pause' | 'resume', // Type narrowed by if condition above
                statusFilters
              )
              
              return {
                ...oldData,
                torrents: updatedTorrents,
                total: updatedTorrents.length,
                totalCount: updatedTorrents.length
              }
            })
          })
          
          // Note: torrent-counts are handled server-side now, no need for optimistic updates
        }
        
        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const delay = variables.action === 'resume' ? MUTATION_CONSTANTS.RESUME_DELAY_MS : MUTATION_CONSTANTS.DEFAULT_OPERATION_DELAY_MS
        
        setTimeout(() => {
          // Use refetch instead of invalidate to avoid loading state
          queryClient.refetchQueries({ 
            queryKey: ['torrents-list', instanceId],
            exact: false,
            type: 'active'
          })
          queryClient.refetchQueries({ 
            queryKey: ['torrent-counts', instanceId],
            exact: false,
            type: 'active'
          })
        }, delay)
        setContextMenuHashes([])
      }
    },
  })

  const handleDelete = async () => {
    await mutation.mutateAsync({ 
      action: 'delete', 
      deleteFiles,
      hashes: contextMenuHashes 
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
        action: 'setTags', 
        tags: tags.join(',') 
      })
    } catch (error) {
      // If setTags fails due to version requirement, fall back to addTags
      if ((error as Error).message?.includes('requires qBittorrent')) {
        await mutation.mutateAsync({ 
          hashes: contextMenuHashes,
          action: 'addTags', 
          tags: tags.join(',') 
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
      action: 'setCategory',
      category,
      hashes: contextMenuHashes,
    })
    setShowCategoryDialog(false)
    setContextMenuHashes([])
  }
  
  const handleRemoveTags = async (tags: string[]) => {
    await mutation.mutateAsync({ 
      action: 'removeTags',
      tags: tags.join(','),
      hashes: contextMenuHashes,
    })
    setShowRemoveTagsDialog(false)
    setContextMenuHashes([])
  }

  const handleContextMenuAction = useCallback((action: 'pause' | 'resume' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'toggleAutoTMM', hashes: string[], enable?: boolean) => {
    setContextMenuHashes(hashes)
    mutation.mutate({ action, hashes, enable })
  }, [mutation]) 

  const copyToClipboard = useCallback((text: string) => {
    navigator.clipboard.writeText(text)
  }, []) 
  
  // Synchronous version for immediate use (backwards compatibility)
  const getCommonTagsSync = (torrents: Torrent[]): string[] => {
    if (torrents.length === 0) return []
    
    // Fast path for single torrent
    if (torrents.length === 1) {
      const tags = torrents[0].tags;
      return tags ? tags.split(',').map(t => t.trim()).filter(Boolean) : [];
    }
    
    // Initialize with first torrent's tags
    const firstTorrent = torrents[0];
    if (!firstTorrent.tags) return [];
    
    // Use a Set for O(1) lookups
    const firstTorrentTagsSet = new Set(
      firstTorrent.tags.split(',').map(t => t.trim()).filter(Boolean)
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
        torrent.tags.split(',').map(t => t.trim()).filter(Boolean)
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
    if (torrents.length === 0) return '';
    if (torrents.length === 1) return torrents[0].category || '';
    
    const firstCategory = torrents[0].category || '';
    
    // Use direct loop instead of every() for early return optimization
    for (let i = 1; i < torrents.length; i++) {
      if ((torrents[i].category || '') !== firstCategory) {
        return ''; // Different category found, no need to check the rest
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
      {/* Desktop Stats bar - shown at top on desktop */}
      <div className="hidden sm:flex items-center justify-between gap-3 text-xs sm:text-sm flex-shrink-0">
        {/* Torrent counts - more compact */}
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1.5">
            <span className="text-muted-foreground">Total:</span>
            <strong>{stats.total}</strong>
          </div>
          
          {/* Show key stats with colors */}
          <div className="flex items-center gap-3 text-xs">
            {stats.downloading > 0 && (
              <div className="flex items-center gap-1">
                <div className="w-2 h-2 rounded-full bg-blue-500" />
                <span>{stats.downloading}</span>
              </div>
            )}
            {stats.seeding > 0 && (
              <div className="flex items-center gap-1">
                <div className="w-2 h-2 rounded-full bg-emerald-500" />
                <span>{stats.seeding}</span>
              </div>
            )}
            {stats.paused > 0 && (
              <div className="flex items-center gap-1">
                <div className="w-2 h-2 rounded-full bg-gray-500" />
                <span>{stats.paused}</span>
              </div>
            )}
            {stats.error > 0 && (
              <div className="flex items-center gap-1">
                <div className="w-2 h-2 rounded-full bg-red-500" />
                <span className="text-destructive">{stats.error}</span>
              </div>
            )}
          </div>
        </div>
        
        {/* User statistics - more compact */}
        {serverState && (
          <div className="flex items-center gap-3">
            {/* All-time stats */}
            {serverState.alltime_dl !== undefined && (
              <div className="flex items-center gap-2 text-xs">
                <span className="text-muted-foreground">All-time stats:</span>
                <div className="flex items-center gap-1">
                  <ChevronDown className="h-3 w-3 text-muted-foreground" />
                  <span className="font-medium">{formatBytes(serverState.alltime_dl || 0)}</span>
                </div>
                <span className="text-muted-foreground">|</span>
                <div className="flex items-center gap-1">
                  <ChevronUp className="h-3 w-3 text-muted-foreground" />
                  <span className="font-medium">{formatBytes(serverState.alltime_ul || 0)}</span>
                </div>
              </div>
            )}
            
            {/* Ratio with color coding */}
            {(serverState.global_ratio !== undefined || (serverState.alltime_dl !== undefined && serverState.alltime_ul !== undefined)) && (() => {
              let ratioValue = 0
              let displayRatio = '0.00'
              
              if (serverState.global_ratio && serverState.global_ratio !== '') {
                ratioValue = parseFloat(serverState.global_ratio)
                displayRatio = serverState.global_ratio
              } else if (serverState.alltime_dl && serverState.alltime_dl > 0 && serverState.alltime_ul !== undefined) {
                ratioValue = (serverState.alltime_ul || 0) / serverState.alltime_dl
                displayRatio = ratioValue.toFixed(2)
              }
              
              const colorVar = getRatioColor(ratioValue)
              
              return (
                <div className="flex items-center gap-1.5 text-xs">
                  <span className="text-muted-foreground">Ratio:</span>
                  <strong style={{ color: colorVar }}>{displayRatio}</strong>
                </div>
              )
            })()}
            
            {/* Peers */}
            {serverState.total_peer_connections !== undefined && (
              <div className="flex items-center gap-1.5 text-xs">
                <span className="text-muted-foreground">Peers:</span>
                <span className="font-medium">{serverState.total_peer_connections || 0}</span>
              </div>
            )}
          </div>
        )}
        
        {/* Current speeds - right aligned */}
        <div className="flex items-center gap-2 text-xs ml-auto">
          <div className="flex items-center gap-1">
            <ChevronDown className="h-3 w-3 text-muted-foreground" />
            <span className="font-medium">{formatSpeed(stats.totalDownloadSpeed || 0)}</span>
          </div>
          <span className="text-muted-foreground">|</span>
          <div className="flex items-center gap-1">
            <ChevronUp className="h-3 w-3 text-muted-foreground" />
            <span className="font-medium">{formatSpeed(stats.totalUploadSpeed || 0)}</span>
          </div>
        </div>
      </div>

      {/* Search and Actions */}
      <div className="flex flex-col gap-2 flex-shrink-0 sm:mt-3">
        {/* Search bar row */}
        <div className="flex items-center gap-1 sm:gap-2">
          {/* Filter button - only on desktop */}
          {filterButton && (
            <div className="hidden xl:block">
              {filterButton}
            </div>
          )}
          <div className="relative flex-1">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
            <Input
              placeholder={isGlobSearch ? "Glob pattern..." : "Search torrents..."}
              value={globalFilter ?? ''}
              onChange={event => handleSearchChange(event.target.value)}
              onKeyDown={handleSearchKeyDown}
              className={`w-full pl-9 pr-9 sm:pr-20 transition-all ${
                effectiveSearch ? 'ring-1 ring-primary/50' : ''
              } ${
                isGlobSearch ? 'ring-1 ring-primary' : ''
              }`}
            />
            <div className="absolute right-2 top-1/2 -translate-y-1/2 flex items-center gap-1">
              {isGlobSearch && (
                <Badge variant="secondary" className="hidden sm:inline-flex text-[10px] px-1.5 py-0 h-5">
                  GLOB
                </Badge>
              )}
              {isLoading && (
                <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />
              )}
              <Tooltip>
                <TooltipTrigger asChild>
                  <button 
                    type="button"
                    className="p-1 hover:bg-muted rounded-sm transition-colors hidden sm:block"
                    onClick={(e) => e.preventDefault()}
                  >
                    <Info className="h-3.5 w-3.5 text-muted-foreground" />
                  </button>
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  <div className="space-y-2 text-xs">
                    <p className="font-semibold">Smart Search Features:</p>
                    <ul className="space-y-1 ml-2">
                      <li>• <strong>Glob patterns:</strong> *.mkv, *1080p*, S??E??</li>
                      <li>• <strong>Fuzzy matching:</strong> "breaking bad" finds "Breaking.Bad"</li>
                      <li>• Handles dots, underscores, and brackets</li>
                      <li>• Searches name, category, and tags</li>
                      <li>• Press Enter for instant search</li>
                      <li>• Auto-searches after 1 second pause</li>
                    </ul>
                  </div>
                </TooltipContent>
              </Tooltip>
            </div>
          </div>
          
          {/* Action buttons */}
          <div className="flex gap-1 sm:gap-2 flex-shrink-0">
            {selectedHashes.length > 0 && (
              <TorrentActions 
                instanceId={instanceId} 
                selectedHashes={selectedHashes}
                selectedTorrents={selectedTorrents}
                onComplete={() => setRowSelection({})}
              />
            )}
            
            {/* Add Torrent button */}
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="outline"
                  size="icon"
                  onClick={() => onAddTorrentModalChange?.(true)}
                  className="sm:hidden"
                >
                  <Plus className="h-4 w-4" />
                  <span className="sr-only">Add Torrent</span>
                </Button>
              </TooltipTrigger>
              <TooltipContent>Add Torrent</TooltipContent>
            </Tooltip>
            <Button
              variant="outline"
              onClick={() => onAddTorrentModalChange?.(true)}
              className="hidden sm:inline-flex"
            >
              <Plus className="h-4 w-4 sm:mr-2" />
              <span className="hidden sm:inline">Add Torrent</span>
            </Button>
            
            {/* Column visibility dropdown */}
            <DropdownMenu>
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
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel>Toggle columns</DropdownMenuLabel>
              <DropdownMenuSeparator />
              {table
                .getAllColumns()
                .filter(
                  (column) =>
                    column.id !== 'select' && // Never show select in visibility options
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
                        {(column.columnDef.meta as any)?.headerString || 
                         (typeof column.columnDef.header === 'string' ? column.columnDef.header : column.id)}
                      </span>
                    </DropdownMenuCheckboxItem>
                  )
                })}
            </DropdownMenuContent>
          </DropdownMenu>
          
          <AddTorrentDialog 
            instanceId={instanceId} 
            open={addTorrentModalOpen}
            onOpenChange={onAddTorrentModalChange}
          />
          </div>
        </div>
      </div>

      {/* Table container */}
      <div className="rounded-md border flex flex-col flex-1 min-h-0 mt-2 sm:mt-3 overflow-hidden">
        <div className="relative flex-1 overflow-auto scrollbar-thin" ref={parentRef}>
          <div style={{ position: 'relative', minWidth: 'min-content' }}>
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
                  width: '100%',
                  position: 'relative',
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
                          className={`flex border-b cursor-pointer hover:bg-muted/50 ${row.getIsSelected() ? 'bg-muted/50' : ''} ${isSelected ? 'bg-accent' : ''}`}
                          style={{
                            position: 'absolute',
                            top: 0,
                            left: 0,
                            minWidth: `${minTableWidth}px`,
                            height: `${virtualRow.size}px`,
                            transform: `translateY(${virtualRow.start}px)`,
                          }}
                          onClick={(e) => {
                            // Don't select when clicking checkbox or its wrapper
                            const target = e.target as HTMLElement
                            const isCheckbox = target.closest('[data-slot="checkbox"]') || target.closest('[role="checkbox"]') || target.closest('.p-1.-m-1')
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
                            handleContextMenuAction('resume', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Play className="mr-2 h-4 w-4" />
                          Resume {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('pause', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Pause className="mr-2 h-4 w-4" />
                          Pause {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('recheck', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <CheckCircle className="mr-2 h-4 w-4" />
                          Force Recheck {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('reannounce', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <Radio className="mr-2 h-4 w-4" />
                          Reannounce {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('increasePriority', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <ArrowUp className="mr-2 h-4 w-4" />
                          Increase Priority {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('decreasePriority', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <ArrowDown className="mr-2 h-4 w-4" />
                          Decrease Priority {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('topPriority', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <ChevronsUp className="mr-2 h-4 w-4" />
                          Top Priority {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
                        <ContextMenuItem 
                          onClick={() => {
                            const hashes = row.getIsSelected() ? selectedHashes : [torrent.hash]
                            handleContextMenuAction('bottomPriority', hashes)
                          }}
                          disabled={mutation.isPending}
                        >
                          <ChevronsDown className="mr-2 h-4 w-4" />
                          Bottom Priority {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
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
                          Set Tags {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
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
                          Set Category {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                        </ContextMenuItem>
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
                                  onClick={() => handleContextMenuAction('toggleAutoTMM', hashes, true)}
                                  disabled={mutation.isPending}
                                >
                                  <Sparkles className="mr-2 h-4 w-4" />
                                  Enable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length} Mixed)` : '(Mixed)'}
                                </ContextMenuItem>
                                <ContextMenuItem
                                  onClick={() => handleContextMenuAction('toggleAutoTMM', hashes, false)}
                                  disabled={mutation.isPending}
                                >
                                  <Settings2 className="mr-2 h-4 w-4" />
                                  Disable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length} Mixed)` : '(Mixed)'}
                                </ContextMenuItem>
                              </>
                            )
                          }
                          
                          return (
                            <ContextMenuItem
                              onClick={() => handleContextMenuAction('toggleAutoTMM', hashes, !allEnabled)}
                              disabled={mutation.isPending}
                            >
                              {allEnabled ? (
                                <>
                                  <Settings2 className="mr-2 h-4 w-4" />
                                  Disable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                                </>
                              ) : (
                                <>
                                  <Sparkles className="mr-2 h-4 w-4" />
                                  Enable TMM {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
                                </>
                              )}
                            </ContextMenuItem>
                          )
                        })()}
                        <ContextMenuSeparator />
                        <ContextMenuItem onClick={() => copyToClipboard(incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name)}>
                          <Copy className="mr-2 h-4 w-4" />
                          Copy Name
                        </ContextMenuItem>
                        <ContextMenuItem onClick={() => copyToClipboard(torrent.hash)}>
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
                          Delete {row.getIsSelected() && selectedHashes.length > 1 ? `(${selectedHashes.length})` : ''}
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
              'No torrents found'
            ) : (
              <>
                {totalCount} torrent{totalCount !== 1 ? 's' : ''}
                {safeLoadedRows < totalCount && ` • ${safeLoadedRows} loaded in viewport`}
                {safeLoadedRows < totalCount && ' (scroll for more)'}
                {isLoadingMore && ' • Loading more from server...'}
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