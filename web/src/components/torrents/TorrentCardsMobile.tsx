import { useState, useRef, useCallback, useEffect, useMemo } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTorrentsList } from '@/hooks/useTorrentsList'
import { useDebounce } from '@/hooks/useDebounce'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { api } from '@/lib/api'
import { Progress } from '@/components/ui/progress'
import { Badge } from '@/components/ui/badge'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
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
import { AddTorrentDialog } from './AddTorrentDialog'
import { 
  Play, 
  Pause, 
  Trash2, 
  Plus, 
  Search, 
  X,
  Clock,
  CheckCircle2,
  MoreVertical,
  Tag,
  Folder,
  Radio,
  ChevronDown,
  ChevronUp,
  Eye,
  EyeOff,
} from 'lucide-react'
import { SetTagsDialog, SetCategoryDialog } from './TorrentDialogs'
import type { Torrent } from '@/types'
import {
  getLinuxIsoName,
  getLinuxCategory,
  getLinuxTags,
  getLinuxRatio,
  useIncognitoMode,
} from '@/lib/incognito'
import { formatBytes, formatSpeed, cn } from '@/lib/utils'
import { applyOptimisticUpdates } from '@/lib/torrent-state-utils'
import { getCommonTags, getCommonCategory } from '@/lib/torrent-utils'
import { motion, AnimatePresence } from 'framer-motion'
import { toast } from 'sonner'

interface TorrentCardsMobileProps {
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
}

function formatEta(seconds: number): string {
  if (seconds === 8640000) return '∞'
  if (seconds < 0) return ''
  
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
  switch(state) {
    case 'downloading':
      return 'default'
    case 'stalledDL':
      return 'secondary'
    case 'uploading':
      return 'default'
    case 'stalledUP':
      return 'secondary'
    case 'pausedDL':
    case 'pausedUP':
      return 'secondary'
    case 'error':
    case 'missingFiles':
      return 'destructive'
    default:
      return 'outline'
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
}: {
  torrent: Torrent
  isSelected: boolean
  onSelect: (selected: boolean) => void
  onClick: () => void
  onLongPress: (torrent: Torrent) => void
  incognitoMode: boolean
  selectionMode: boolean
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
        if ('vibrate' in navigator) {
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
  
  return (
    <div
      className={cn(
        "bg-card rounded-lg border p-4 cursor-pointer transition-all relative overflow-hidden select-none",
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
        {/* Selection checkbox - visible in selection mode */}
        {selectionMode && (
          <div className="absolute top-2 right-2 z-10">
            <Checkbox
              checked={isSelected}
              onCheckedChange={onSelect}
              className="h-5 w-5"
              onClick={(e) => e.stopPropagation()}
            />
          </div>
        )}
        
        {/* Torrent name */}
        <div className="mb-3">
          <h3 className={cn(
            "font-medium text-sm line-clamp-2 break-all",
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
                {Math.round(torrent.progress * 100)}%
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
                <span className="font-medium">{formatSpeed(torrent.dlspeed)}</span>
              </div>
            )}
            
            {/* Upload speed */}
            {torrent.upspeed > 0 && (
              <div className="flex items-center gap-1">
                <ChevronUp className="h-3 w-3 [color:var(--chart-3)]" />
                <span className="font-medium">{formatSpeed(torrent.upspeed)}</span>
              </div>
            )}
          </div>
          
          {/* State badge on the right */}
          <Badge variant={getStatusBadgeVariant(torrent.state)} className="text-xs">
            {torrent.state}
          </Badge>
        </div>
        
        {/* Bottom row: Category and Tags */}
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
            <div className="flex items-center gap-1 flex-wrap justify-end ml-auto">
              <Tag className="h-3 w-3 text-muted-foreground flex-shrink-0" />
              {(Array.isArray(displayTags) ? displayTags : displayTags.split(',')).map((tag, i) => (
                <Badge key={i} variant="secondary" className="text-[10px] px-1.5 py-0 h-4">
                  {tag.trim()}
                </Badge>
              ))}
            </div>
          )}
        </div>
    </div>
  )
}

export function TorrentCardsMobile({ 
  instanceId, 
  filters, 
  onTorrentSelect,
  addTorrentModalOpen,
  onAddTorrentModalChange,
  onFilteredDataUpdate 
}: TorrentCardsMobileProps) {
  // State
  const [globalFilter, setGlobalFilter] = useState('')
  const [immediateSearch, setImmediateSearch] = useState('')
  const [selectedHashes, setSelectedHashes] = useState<Set<string>>(new Set())
  const [selectionMode, setSelectionMode] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [torrentToDelete, setTorrentToDelete] = useState<Torrent | null>(null)
  const [showActionsSheet, setShowActionsSheet] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [actionTorrents, setActionTorrents] = useState<Torrent[]>([]);
  
  const [incognitoMode, setIncognitoMode] = useIncognitoMode()
  const queryClient = useQueryClient()
  const debouncedSearch = useDebounce(globalFilter, 1000)
  const effectiveSearch = immediateSearch || debouncedSearch
  
  // Fetch data
  const { 
    torrents,
    totalCount, 
    stats,
    counts,
    categories,
    tags,
    isLoadingMore,
    hasLoadedAll,
    loadMore: loadMoreTorrents,
    isFreshData,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters,
  })
  
  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && isFreshData && torrents && totalCount !== undefined) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags)
    }
  }, [totalCount, isFreshData, torrents.length, counts, categories, tags, onFilteredDataUpdate]) // Update when data changes
  
  // Use tags and categories from the main data fetch
  const availableTags = tags || []
  const availableCategories = categories || {}
  
  // Virtual scrolling with consistent spacing
  const parentRef = useRef<HTMLDivElement>(null)
  const rowVirtualizer = useVirtualizer({
    count: torrents.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 180, // Default estimate for card height
    measureElement: (element) => {
      // Measure actual element height
      if (element) {
        return element.getBoundingClientRect().height
      }
      return 180
    },
    overscan: 5,
    onChange: (instance) => {
      const lastItem = instance.getVirtualItems().at(-1)
      if (lastItem && lastItem.index >= torrents.length - 5 && !hasLoadedAll && !isLoadingMore) {
        loadMoreTorrents()
      }
    },
  })
  
  const virtualizer = rowVirtualizer
  
  const virtualItems = virtualizer.getVirtualItems()
  
  // Exit selection mode when no items selected
  useEffect(() => {
    if (selectionMode && selectedHashes.size === 0) {
      setSelectionMode(false)
    }
  }, [selectedHashes.size, selectionMode])
  
  // Mutations
  const mutation = useMutation({
    mutationFn: (data: {
      action: 'pause' | 'resume' | 'delete' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority' | 'addTags' | 'removeTags' | 'setTags' | 'setCategory' | 'toggleAutoTMM'
      hashes: string[]
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
    }) => {
      return api.bulkAction(instanceId, data)
    },
    onSuccess: async (_, variables) => {
      if (variables.action === 'delete') {
        setSelectedHashes(new Set())
        setSelectionMode(false)
        
        // Optimistically remove from cache
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
            }
          })
        })
        
        setTimeout(() => {
          queryClient.invalidateQueries({ 
            queryKey: ['torrents-list', instanceId],
            exact: false 
          })
        }, variables.deleteFiles ? 5000 : 2000)
      } else {
        // Handle pause/resume optimistically
        if (variables.action === 'pause' || variables.action === 'resume') {
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ['torrents-list', instanceId],
            exact: false
          })
          
          queries.forEach(query => {
            queryClient.setQueryData(query.queryKey, (oldData: any) => {
              if (!oldData?.torrents) return oldData
              
              const { torrents: updatedTorrents } = applyOptimisticUpdates(
                oldData.torrents,
                variables.hashes,
                variables.action as 'pause' | 'resume',
                filters?.status || []
              )
              
              return {
                ...oldData,
                torrents: updatedTorrents,
              }
            })
          })
        }
        
        setTimeout(() => {
          queryClient.invalidateQueries({ 
            queryKey: ['torrents-list', instanceId],
            exact: false 
          })
        }, variables.action === 'resume' ? 2000 : 1000)
      }
    },
  })
  
  // Handlers
  const handleLongPress = useCallback((torrent: Torrent) => {
    setSelectionMode(true)
    setSelectedHashes(new Set([torrent.hash]))
  }, [])
  
  const handleSelect = useCallback((hash: string, selected: boolean) => {
    setSelectedHashes(prev => {
      const next = new Set(prev)
      if (selected) {
        next.add(hash)
      } else {
        next.delete(hash)
      }
      return next
    })
  }, [])
  
  const handleSelectAll = useCallback(() => {
    if (selectedHashes.size === torrents.length) {
      setSelectedHashes(new Set())
    } else {
      setSelectedHashes(new Set(torrents.map(t => t.hash)))
    }
  }, [selectedHashes.size, torrents])
  
  const handleBulkAction = useCallback((action: 'pause' | 'resume' | 'delete' | 'recheck' | 'reannounce' | 'increasePriority' | 'decreasePriority' | 'topPriority' | 'bottomPriority') => {
    const hashes = Array.from(selectedHashes)
    mutation.mutate({ action, hashes })
    setSelectedHashes(new Set())
    setSelectionMode(false)
    setShowActionsSheet(false)
  }, [selectedHashes, mutation])
  
  const handleDelete = async () => {
    const hashes = torrentToDelete ? [torrentToDelete.hash] : Array.from(selectedHashes)
    await mutation.mutateAsync({ 
      action: 'delete', 
      deleteFiles,
      hashes 
    })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
    setTorrentToDelete(null)
    toast.success(`${hashes.length} torrent(s) deleted`)
  }
  
  const handleSetTags = async (tags: string[]) => {
    const hashes = actionTorrents.map(t => t.hash)
    await mutation.mutateAsync({ 
      action: 'setTags',
      tags: tags.join(','),
      hashes 
    })
    setShowTagsDialog(false)
    setActionTorrents([])
    setSelectedHashes(new Set())
    setSelectionMode(false)
  }
  
  const handleSetCategory = async (category: string) => {
    const hashes = actionTorrents.map(t => t.hash)
    await mutation.mutateAsync({ 
      action: 'setCategory',
      category,
      hashes 
    })
    setShowCategoryDialog(false)
    setActionTorrents([])
    setSelectedHashes(new Set())
    setSelectionMode(false)
  }
  
  const getSelectedTorrents = useMemo(() => {
    return torrents.filter(t => selectedHashes.has(t.hash))
  }, [torrents, selectedHashes])
  
  return (
    <div className="h-full flex flex-col relative">
      {/* Header with stats */}
      <div className="sticky top-0 z-40 bg-background">
        <div className="pb-3">
          {/* Stats bar */}
          <div className="flex items-center justify-between text-xs mb-3">
            <div className="flex items-center gap-2">
              <span>Total: <strong>{stats.total}</strong></span>
              <span className="text-muted-foreground">|</span>
              <div className="flex items-center gap-0.5 [color:var(--chart-3)]">
                <ChevronUp className="h-3 w-3" />
                <span>{stats.seeding}</span>
              </div>
              <div className="flex items-center gap-0.5 [color:var(--chart-2)]">
                <ChevronDown className="h-3 w-3" />
                <span>{stats.downloading}</span>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <ChevronDown className="h-3 w-3" />
              <span className="font-medium">{formatSpeed(stats.totalDownloadSpeed || 0)}</span>
              <span className="text-muted-foreground">|</span>
              <ChevronUp className="h-3 w-3" />
              <span className="font-medium">{formatSpeed(stats.totalUploadSpeed || 0)}</span>
            </div>
          </div>
          
          {/* Search bar */}
          <div className="flex items-center gap-2">
            <div className="flex-1 relative">
              <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground pointer-events-none" />
              <Input
                type="text"
                placeholder="Search torrents..."
                value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') {
                    setImmediateSearch(globalFilter)
                  }
                }}
                className="pl-9 pr-3 h-9"
              />
            </div>
            
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setIncognitoMode(!incognitoMode)}
              title={incognitoMode ? "Disable incognito mode" : "Enable incognito mode"}
            >
              {incognitoMode ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            </Button>
            
            <Button
              size="icon"
              variant="outline"
              onClick={() => onAddTorrentModalChange?.(true)}
            >
              <Plus className="h-4 w-4" />
            </Button>
          </div>
        </div>
        
        {/* Selection mode header */}
        {selectionMode && (
          <div className="bg-primary text-primary-foreground px-4 py-2 mb-3 flex items-center justify-between">
            <div className="flex items-center gap-2">
              <button
                onClick={() => {
                  setSelectedHashes(new Set())
                  setSelectionMode(false)
                }}
                className="p-1"
              >
                <X className="h-4 w-4" />
              </button>
              <span className="text-sm font-medium">
                {selectedHashes.size} selected
              </span>
            </div>
            <button
              onClick={handleSelectAll}
              className="text-sm font-medium"
            >
              {selectedHashes.size === torrents.length ? 'Deselect All' : 'Select All'}
            </button>
          </div>
        )}
      </div>
      
      {/* Torrent cards with virtual scrolling */}
      <div ref={parentRef} className="flex-1 overflow-auto" style={{ paddingBottom: 'calc(5rem + env(safe-area-inset-bottom))' }}>
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: '100%',
            position: 'relative',
          }}
        >
          {virtualItems.map(virtualItem => {
            const torrent = torrents[virtualItem.index]
            const isSelected = selectedHashes.has(torrent.hash)
            
            return (
              <div
                key={virtualItem.key}
                ref={virtualizer.measureElement}
                data-index={virtualItem.index}
                style={{
                  position: 'absolute',
                  top: 0,
                  left: 0,
                  width: '100%',
                  transform: `translateY(${virtualItem.start}px)`,
                  paddingBottom: '12px',
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
                />
              </div>
            )
          })}
        </div>
        
        {/* Loading indicator */}
        {isLoadingMore && (
          <div className="flex items-center justify-center py-4">
            <div className="text-sm text-muted-foreground">Loading more...</div>
          </div>
        )}
      </div>
      
      {/* Fixed bottom action bar - visible in selection mode */}
      <AnimatePresence>
        {selectionMode && selectedHashes.size > 0 && (
          <motion.div
            initial={{ y: 100 }}
            animate={{ y: 0 }}
            exit={{ y: 100 }}
            className="fixed bottom-0 left-0 right-0 bg-background border-t z-50"
            style={{ 
              padding: '0.75rem',
              paddingBottom: 'calc(0.75rem + env(safe-area-inset-bottom))'
            }}
          >
            <div className="flex items-center justify-around">
              <button
                onClick={() => handleBulkAction('resume')}
                className="flex flex-col items-center gap-1 p-2"
              >
                <Play className="h-5 w-5" />
                <span className="text-xs">Resume</span>
              </button>
              
              <button
                onClick={() => handleBulkAction('pause')}
                className="flex flex-col items-center gap-1 p-2"
              >
                <Pause className="h-5 w-5" />
                <span className="text-xs">Pause</span>
              </button>
              
              <button
                onClick={() => {
                  setActionTorrents(getSelectedTorrents)
                  setShowCategoryDialog(true)
                }}
                className="flex flex-col items-center gap-1 p-2"
              >
                <Folder className="h-5 w-5" />
                <span className="text-xs">Category</span>
              </button>
              
              <button
                onClick={() => {
                  setActionTorrents(getSelectedTorrents)
                  setShowTagsDialog(true)
                }}
                className="flex flex-col items-center gap-1 p-2"
              >
                <Tag className="h-5 w-5" />
                <span className="text-xs">Tags</span>
              </button>
              
              <button
                onClick={() => setShowActionsSheet(true)}
                className="flex flex-col items-center gap-1 p-2"
              >
                <MoreVertical className="h-5 w-5" />
                <span className="text-xs">More</span>
              </button>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
      
      {/* More actions sheet */}
      <Sheet open={showActionsSheet} onOpenChange={setShowActionsSheet}>
        <SheetContent side="bottom" className="h-auto">
          <SheetHeader>
            <SheetTitle>Actions for {selectedHashes.size} torrent(s)</SheetTitle>
          </SheetHeader>
          <div className="grid gap-2 py-4 px-4">
            <Button
              variant="outline"
              onClick={() => handleBulkAction('recheck')}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4" />
              Force Recheck
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction('reannounce')}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4" />
              Reannounce
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction('topPriority')}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4" />
              Top Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction('bottomPriority')}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4" />
              Bottom Priority
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                setShowDeleteDialog(true)
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <Trash2 className="mr-2 h-4 w-4" />
              Delete
            </Button>
          </div>
        </SheetContent>
      </Sheet>
      
      {/* Delete confirmation dialog */}
      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              Delete {torrentToDelete ? '1' : selectedHashes.size} torrent(s)?
            </AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center space-x-2 py-4">
            <Checkbox
              id="deleteFiles"
              checked={deleteFiles}
              onCheckedChange={(checked) => setDeleteFiles(checked as boolean)}
            />
            <label htmlFor="deleteFiles" className="text-sm font-medium">
              Also delete files from disk
            </label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      
      {/* Tags dialog */}
      <SetTagsDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags}
        hashCount={actionTorrents.length}
        onConfirm={handleSetTags}
        isPending={mutation.isPending}
        initialTags={getCommonTags(actionTorrents)}
      />
      
      {/* Category dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={actionTorrents.length}
        onConfirm={handleSetCategory}
        isPending={mutation.isPending}
        initialCategory={getCommonCategory(actionTorrents)}
      />
      
      {/* Add torrent dialog */}
      <AddTorrentDialog 
        instanceId={instanceId} 
        open={addTorrentModalOpen}
        onOpenChange={onAddTorrentModalChange}
      />
    </div>
  )
}