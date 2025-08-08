import { useState, useMemo, useRef, useCallback, useEffect } from 'react'
import {
  useReactTable,
  getCoreRowModel,
  flexRender,
  type ColumnDef,
  type ColumnResizeMode,
  type VisibilityState,
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
import type { Torrent } from '@/types'
import {
  getLinuxIsoName,
  getLinuxCategory,
  getLinuxTags,
  getLinuxSavePath,
  getLinuxTracker,
  getLinuxRatio,
  useIncognitoMode,
} from '@/lib/incognito'
import { formatBytes, formatSpeed } from '@/lib/utils'
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
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: any, categories?: any, tags?: string[]) => void
  filterButton?: React.ReactNode
}


function formatEta(seconds: number): string {
  if (seconds === 8640000) return '∞'
  if (seconds < 0) return ''
  
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  
  if (hours > 24) {
    const days = Math.floor(hours / 24)
    return `${days}d ${hours % 24}h`
  }
  
  if (hours > 0) {
    return `${hours}h ${minutes}m`
  }
  
  return `${minutes}m`
}

// Calculate minimum column width based on header text
function calculateMinWidth(text: string, padding: number = 48): number {
  // Approximate character width in pixels for text-sm (14px) with font-medium
  const charWidth = 7.5
  // Add padding for sort indicator
  const extraPadding = 20
  return Math.max(60, Math.ceil(text.length * charWidth) + padding + extraPadding)
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
    size: 40,
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
    size: 45,
  },
  {
    accessorKey: 'name',
    header: 'Name',
    cell: ({ row }) => {
      const displayName = incognitoMode ? getLinuxIsoName(row.original.hash) : row.original.name
      return (
        <div className="truncate text-sm" title={displayName}>
          {displayName}
        </div>
      )
    },
    size: 200,
  },
  {
    accessorKey: 'size',
    header: 'Size',
    cell: ({ row }) => <span className="text-sm truncate">{formatBytes(row.original.size)}</span>,
    size: 85,
  },
  {
    accessorKey: 'progress',
    header: 'Progress',
    cell: ({ row }) => (
      <div className="flex items-center gap-2">
        <Progress value={row.original.progress * 100} className="w-20" />
        <span className="text-xs text-muted-foreground">
          {Math.round(row.original.progress * 100)}%
        </span>
      </div>
    ),
    size: 120,
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
    size: 120,
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
    size: 80,
  },
  {
    accessorKey: 'ratio',
    header: 'Ratio',
    cell: ({ row }) => {
      const ratio = incognitoMode ? getLinuxRatio(row.original.hash) : row.original.ratio
      const displayRatio = ratio === -1 ? "∞" : ratio.toFixed(2)
      
      let colorVar = ''
      if (ratio >= 0) {
        if (ratio < 0.5) {
          colorVar = 'var(--chart-5)' // very bad - lowest/darkest
        } else if (ratio < 1.0) {
          colorVar = 'var(--chart-4)' // bad - below 1.0
        } else if (ratio < 2.0) {
          colorVar = 'var(--chart-3)' // okay - above 1.0
        } else if (ratio < 5.0) {
          colorVar = 'var(--chart-2)' // good - healthy ratio
        } else {
          colorVar = 'var(--chart-1)' // excellent - best ratio
        }
      }
      
      return (
        <span 
          className="text-sm font-medium" 
          style={{ color: colorVar }}
        >
          {displayRatio}
        </span>
      )
    },
    size: 80,
  },
  {
    accessorKey: 'added_on',
    header: 'Added',
    cell: ({ row }) => {
      const addedOn = row.original.added_on
      if (!addedOn || addedOn === 0) {
        return '-'
      }
      const date = new Date(addedOn * 1000) // Convert from Unix timestamp
      
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
          {month}/{day}/{year}, {displayHours}:{minutes.toString().padStart(2, '0')}:{seconds.toString().padStart(2, '0')} {ampm}
        </div>
      )
    },
    size: 200,
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
    size: 150,
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
    size: 200,
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
    size: 250,
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
      } catch (e) {
        // If URL parsing fails, show as is
      }
      return (
        <div className="truncate text-sm" title={tracker}>
          {displayTracker || '-'}
        </div>
      )
    },
    size: 150,
  },
]

export function TorrentTableOptimized({ instanceId, filters, selectedTorrent, onTorrentSelect, addTorrentModalOpen, onAddTorrentModalChange, onFilteredDataUpdate, filterButton }: TorrentTableOptimizedProps) {
  // State management
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
  
  // Use incognito mode hook
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  
  // Column visibility with persistence
  const defaultColumnVisibility: VisibilityState = {
    downloaded: false,
    uploaded: false,
    saveLocation: false,
    tracker: false,
    priority: false,
  }
  const [columnVisibility, setColumnVisibility] = usePersistedColumnVisibility(
    defaultColumnVisibility
  )
  
  // Column order with persistence
  const defaultColumnOrder = useMemo(() => {
    const cols = createColumns(false) // Use non-incognito columns for default order
    return cols.map(col => {
      if ('id' in col && col.id) return col.id
      if ('accessorKey' in col && typeof col.accessorKey === 'string') return col.accessorKey
      return null
    }).filter(Boolean) as string[]
  }, [])
  const [columnOrder, setColumnOrder] = usePersistedColumnOrder(
    defaultColumnOrder
  )
  
  // Column sizing with persistence
  const [columnSizing, setColumnSizing] = usePersistedColumnSizing(
    {} // Start with empty object, let columns use their default sizes
  )
  
  // Progressive loading state
  const [loadedRows, setLoadedRows] = useState(100)
  
  // Query client for invalidating queries
  const queryClient = useQueryClient()

  // Fetch metadata using shared hook
  const { data: metadata } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}

  // Debounce search to prevent excessive filtering (1 second delay)
  const debouncedSearch = useDebounce(globalFilter, 1000)

  // Use immediate search if available, otherwise use debounced search
  const effectiveSearch = immediateSearch || debouncedSearch
  
  // Check if search contains glob patterns
  const isGlobSearch = globalFilter && /[*?[\]]/.test(globalFilter)

  // Fetch torrents data
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
  })
  
  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags)
    }
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, onFilteredDataUpdate]) // Update when data changes
  
  // Show refetch indicator only if fetching takes more than 2 seconds
  // This avoids annoying flickering for fast instances
  useEffect(() => {
    let timeoutId: NodeJS.Timeout
    
    if (isFetching && !isLoading && torrents.length > 0) {
      // Only show indicator after 2 second delay
      timeoutId = setTimeout(() => {
        setShowRefetchIndicator(true)
      }, 2000)
    } else {
      setShowRefetchIndicator(false)
    }
    
    return () => clearTimeout(timeoutId)
  }, [isFetching, isLoading, torrents.length])
  
  // Handle Enter key for immediate search
  const handleSearchKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter') {
      setImmediateSearch(globalFilter)
    }
  }
  
  // Clear immediate search when input changes (to allow debounced search to take over)
  const handleSearchChange = (value: string) => {
    setGlobalFilter(value)
    if (immediateSearch) {
      setImmediateSearch('')
    }
  }

  // Sort torrents client-side
  const sortedTorrents = useMemo(() => {
    if (sorting.length === 0) return torrents
    
    const sorted = [...torrents]
    const sort = sorting[0]
    
    sorted.sort((a, b) => {
      const aValue = a[sort.id as keyof Torrent]
      const bValue = b[sort.id as keyof Torrent]
      
      if (aValue === null || aValue === undefined) return 1
      if (bValue === null || bValue === undefined) return -1
      
      if (aValue < bValue) return sort.desc ? 1 : -1
      if (aValue > bValue) return sort.desc ? -1 : 1
      return 0
    })
    
    return sorted
  }, [torrents, sorting])

  const columns = useMemo(() => createColumns(incognitoMode), [incognitoMode])
  
  const table = useReactTable({
    data: sortedTorrents,
    columns,
    getCoreRowModel: getCoreRowModel(),
    // Use torrent hash as stable row ID
    getRowId: (row) => row.hash,
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
    columnResizeMode: 'onChange' as ColumnResizeMode,
  })

  // Get selected torrent hashes
  const selectedHashes = useMemo(() => {
    return Object.keys(rowSelection)
      .filter(key => rowSelection[key as keyof typeof rowSelection])
  }, [rowSelection])
  
  // Get selected torrents
  const selectedTorrents = useMemo(() => {
    return selectedHashes
      .map(hash => sortedTorrents.find(t => t.hash === hash))
      .filter(Boolean) as Torrent[]
  }, [selectedHashes, sortedTorrents])

  // Load more rows as user scrolls (progressive loading)
  const loadMore = useCallback(() => {
    const newLoadedRows = Math.min(loadedRows + 100, sortedTorrents.length)
    setLoadedRows(newLoadedRows)
    
    // If we're near the end of loaded torrents and haven't loaded all from server
    if (newLoadedRows >= sortedTorrents.length - 50 && !hasLoadedAll && !isLoadingMore) {
      loadMoreTorrents()
    }
  }, [loadedRows, sortedTorrents.length, hasLoadedAll, isLoadingMore, loadMoreTorrents])

  // Virtualization setup with progressive loading
  const { rows } = table.getRowModel()
  const parentRef = useRef<HTMLDivElement>(null)

  // Only virtualize the loaded rows, not all rows
  const virtualizer = useVirtualizer({
    count: Math.min(loadedRows, rows.length),
    getScrollElement: () => parentRef.current,
    estimateSize: () => 40,
    overscan: 20, // Increased for smoother scrolling
    onChange: (instance) => {
      const lastItem = instance.getVirtualItems().at(-1)
      if (lastItem && lastItem.index >= loadedRows - 50) {
        loadMore()
      }
    },
  })

  const virtualRows = virtualizer.getVirtualItems()


  // Reset loaded rows when data changes
  useEffect(() => {
    if (sortedTorrents.length > 0) {
      // If we have torrents but loadedRows is 0, set initial load
      if (loadedRows === 0) {
        setLoadedRows(Math.min(100, sortedTorrents.length))
      }
      // If data reduced below loaded rows, adjust
      else if (sortedTorrents.length < loadedRows) {
        setLoadedRows(sortedTorrents.length)
      }
      // If data increased significantly, reset to show more rows
      else if (sortedTorrents.length > loadedRows && loadedRows < 100) {
        setLoadedRows(Math.min(100, sortedTorrents.length))
      }
    }
  }, [sortedTorrents.length, loadedRows])

  // Reset loaded rows when filters change
  useEffect(() => {
    setLoadedRows(Math.min(100, sortedTorrents.length))
    // Scroll to top and force virtualizer recalculation
    if (parentRef.current) {
      parentRef.current.scrollTop = 0
    }
  }, [filters])


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
    onSuccess: async (_, variables) => {
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
        
        queries.forEach(query => {
          queryClient.setQueryData(query.queryKey, (oldData: any) => {
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
        const refetchDelay = variables.deleteFiles ? 5000 : 2000
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
          queries.forEach(query => {
            queryClient.setQueryData(query.queryKey, (oldData: any) => {
              if (!oldData?.torrents) return oldData
              
              // Check if this query has a status filter in its key
              // Query key structure: ['torrents-list', instanceId, currentPage, filters, search]
              const queryKey = query.queryKey as any[]
              const filters = queryKey[3] // filters is at index 3
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
        const delay = variables.action === 'resume' ? 2000 : 1000
        
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
    } catch (error: any) {
      // If setTags fails due to version requirement, fall back to addTags
      if (error.message?.includes('requires qBittorrent')) {
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

  const handleContextMenuAction = (action: 'pause' | 'resume' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'toggleAutoTMM', hashes: string[], enable?: boolean) => {
    setContextMenuHashes(hashes)
    mutation.mutate({ action, hashes, enable })
  }

  const copyToClipboard = (text: string) => {
    navigator.clipboard.writeText(text)
  }
  
  // Get common tags from selected torrents (tags that ALL selected torrents have)
  const getCommonTags = (torrents: Torrent[]): string[] => {
    if (torrents.length === 0) return []
    
    // Get tags from first torrent
    const firstTorrentTags = torrents[0].tags
      ? torrents[0].tags.split(',').map(t => t.trim()).filter(t => t)
      : []
    
    // If only one torrent, return its tags
    if (torrents.length === 1) return firstTorrentTags
    
    // Find common tags across all torrents
    return firstTorrentTags.filter(tag => 
      torrents.every(torrent => {
        const torrentTags = torrent.tags
          ? torrent.tags.split(',').map(t => t.trim())
          : []
        return torrentTags.includes(tag)
      })
    )
  }
  
  // Get common category from selected torrents (if all have the same category)
  const getCommonCategory = (torrents: Torrent[]): string => {
    if (torrents.length === 0) return ''
    
    const firstCategory = torrents[0].category || ''
    
    // Check if all torrents have the same category
    const allSameCategory = torrents.every(t => (t.category || '') === firstCategory)
    
    return allSameCategory ? firstCategory : ''
  }
  
  // Drag and drop setup
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
  
  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event
    
    if (active && over && active.id !== over.id) {
      setColumnOrder((currentOrder) => {
        const oldIndex = currentOrder.indexOf(active.id as string)
        const newIndex = currentOrder.indexOf(over.id as string)
        return arrayMove(currentOrder, oldIndex, newIndex)
      })
    }
  }

  return (
    <div className="h-full flex flex-col">
      {/* Desktop Stats bar - shown at top on desktop */}
      <div className="hidden sm:flex flex-wrap gap-2 sm:gap-4 text-xs sm:text-sm flex-shrink-0">
        <div>Total: <strong>{stats.total}</strong></div>
        <div className="hidden lg:block">Downloading: <strong>{stats.downloading}</strong></div>
        <div className="hidden lg:block">Seeding: <strong>{stats.seeding}</strong></div>
        <div className="hidden lg:block">Paused: <strong>{stats.paused}</strong></div>
        <div className="hidden lg:block">Error: <strong className={stats.error > 0 ? "text-destructive" : ""}>{stats.error}</strong></div>
        <div className="ml-auto text-xs sm:text-sm flex items-center gap-1">
          <ChevronDown className="h-3.5 w-3.5" />
          {formatSpeed(stats.totalDownloadSpeed || 0)}
          <span className="text-muted-foreground mx-1">|</span>
          <ChevronUp className="h-3.5 w-3.5" />
          {formatSpeed(stats.totalUploadSpeed || 0)}
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
                {Object.values(columnVisibility).some(v => v === false) && (
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
            <div className="sticky top-0 bg-background border-b" style={{ zIndex: 50, position: 'sticky' }}>
              <DndContext
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={handleDragEnd}
                modifiers={[restrictToHorizontalAxis]}
              >
                {table.getHeaderGroups().map(headerGroup => {
                  const headers = headerGroup.headers
                  const headerIds = headers.map(h => h.column.id)
                  
                  // Calculate minimum table width based on visible columns
                  const minTableWidth = table.getVisibleLeafColumns().reduce((width, col) => {
                    return width + col.getSize()
                  }, 0)
                  
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
                  const torrent = row.original
                  const isSelected = selectedTorrent?.hash === torrent.hash
                  
                  // Calculate minimum table width based on visible columns
                  const minTableWidth = table.getVisibleLeafColumns().reduce((width, col) => {
                    return width + col.getSize()
                  }, 0)
                
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
                            // Select this row if not already selected
                            if (!row.getIsSelected()) {
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
                Showing {Math.min(loadedRows, totalCount)} of {totalCount} torrents
                {loadedRows < totalCount && ' (scroll to load more)'}
                {isLoadingMore && ' • Loading more...'}
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
            {virtualRows.length > 0 && (
              <div className="text-sm text-muted-foreground">
                Rendering rows {virtualRows[0].index + 1} - {virtualRows[virtualRows.length - 1].index + 1}
              </div>
            )}
            
            {/* Incognito mode toggle - barely visible */}
            <button
              onClick={() => setIncognitoMode(!incognitoMode)}
              className="p-1 rounded-sm transition-all hover:bg-muted/50"
              style={{ opacity: incognitoMode ? 0.5 : 0.2 }}
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
        initialTags={getCommonTags(contextMenuTorrents)}
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
        currentTags={getCommonTags(contextMenuTorrents)}
      />
    </div>
  )
}