/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import { ScrollToTopButton } from "@/components/ui/scroll-to-top-button"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { Switch } from "@/components/ui/switch"
import { useDebounce } from "@/hooks/useDebounce"
import { TORRENT_ACTIONS, useTorrentActions, type TorrentAction } from "@/hooks/useTorrentActions"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { Link, useSearch } from "@tanstack/react-router"
import { useVirtualizer } from "@tanstack/react-virtual"
import {
  ArrowUpDown,
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  ChevronsUpDown,
  Clock,
  Eye,
  EyeOff,
  Filter,
  Folder,
  Gauge,
  HardDrive,
  Loader2,
  MoreVertical,
  Pause,
  Play,
  Plus,
  Radio,
  Settings2,
  Sprout,
  Tag,
  Trash2,
  X
} from "lucide-react"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { AddTorrentDialog } from "./AddTorrentDialog"
import { RemoveTagsDialog, SetCategoryDialog, SetTagsDialog } from "./TorrentDialogs"
// import { createPortal } from 'react-dom'
// Columns dropdown removed on mobile
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata.ts"
import { useInstances } from "@/hooks/useInstances"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { getStateLabel } from "@/lib/torrent-state-utils"
import { getCommonCategory, getCommonTags } from "@/lib/torrent-utils"
import { cn, formatBytes } from "@/lib/utils"
import type { Category, Torrent, TorrentCounts } from "@/types"

// Mobile-friendly Share Limits Dialog
function MobileShareLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => void
  isPending: boolean
}) {
  const [ratioEnabled, setRatioEnabled] = useState(false)
  const [ratioLimit, setRatioLimit] = useState(1.5)
  const [seedingTimeEnabled, setSeedingTimeEnabled] = useState(false)
  const [seedingTimeLimit, setSeedingTimeLimit] = useState(1440)
  const [inactiveSeedingTimeEnabled, setInactiveSeedingTimeEnabled] = useState(false)
  const [inactiveSeedingTimeLimit, setInactiveSeedingTimeLimit] = useState(10080)

  const handleSubmit = () => {
    onConfirm(
      ratioEnabled ? ratioLimit : -1,
      seedingTimeEnabled ? seedingTimeLimit : -1,
      inactiveSeedingTimeEnabled ? inactiveSeedingTimeLimit : -1
    )
    // Reset form
    setRatioEnabled(false)
    setRatioLimit(1.5)
    setSeedingTimeEnabled(false)
    setSeedingTimeLimit(1440)
    setInactiveSeedingTimeEnabled(false)
    setInactiveSeedingTimeLimit(10080)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set Share Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Configure seeding limits. Use -1 or disable to remove limits.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="ratioEnabled"
                checked={ratioEnabled}
                onCheckedChange={setRatioEnabled}
              />
              <Label htmlFor="ratioEnabled">Set ratio limit</Label>
            </div>
            {ratioEnabled && (
              <Input
                type="number"
                min="0"
                step="0.1"
                value={ratioLimit}
                onChange={(e) => setRatioLimit(parseFloat(e.target.value) || 0)}
                placeholder="1.5"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="seedingTimeEnabled"
                checked={seedingTimeEnabled}
                onCheckedChange={setSeedingTimeEnabled}
              />
              <Label htmlFor="seedingTimeEnabled">Set seeding time limit (minutes)</Label>
            </div>
            {seedingTimeEnabled && (
              <Input
                type="number"
                min="0"
                value={seedingTimeLimit}
                onChange={(e) => setSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="1440"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="inactiveSeedingTimeEnabled"
                checked={inactiveSeedingTimeEnabled}
                onCheckedChange={setInactiveSeedingTimeEnabled}
              />
              <Label htmlFor="inactiveSeedingTimeEnabled">Set inactive seeding limit (minutes)</Label>
            </div>
            {inactiveSeedingTimeEnabled && (
              <Input
                type="number"
                min="0"
                value={inactiveSeedingTimeLimit}
                onChange={(e) => setInactiveSeedingTimeLimit(parseInt(e.target.value) || 0)}
                placeholder="10080"
              />
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isPending}>
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Mobile-friendly Speed Limits Dialog
function MobileSpeedLimitsDialog({
  open,
  onOpenChange,
  hashCount,
  onConfirm,
  isPending,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  hashCount: number
  onConfirm: (uploadLimit: number, downloadLimit: number) => void
  isPending: boolean
}) {
  const [uploadEnabled, setUploadEnabled] = useState(false)
  const [uploadLimit, setUploadLimit] = useState(1024)
  const [downloadEnabled, setDownloadEnabled] = useState(false)
  const [downloadLimit, setDownloadLimit] = useState(1024)

  const handleSubmit = () => {
    onConfirm(
      uploadEnabled ? uploadLimit : -1,
      downloadEnabled ? downloadLimit : -1
    )
    // Reset form
    setUploadEnabled(false)
    setUploadLimit(1024)
    setDownloadEnabled(false)
    setDownloadLimit(1024)
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Set Speed Limits for {hashCount} torrent(s)</DialogTitle>
          <DialogDescription>
            Set upload and download speed limits in KB/s. Use -1 or disable to remove limits.
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-4">
          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="uploadEnabled"
                checked={uploadEnabled}
                onCheckedChange={setUploadEnabled}
              />
              <Label htmlFor="uploadEnabled">Set upload limit (KB/s)</Label>
            </div>
            {uploadEnabled && (
              <Input
                type="number"
                min="0"
                value={uploadLimit}
                onChange={(e) => setUploadLimit(parseInt(e.target.value) || 0)}
                placeholder="1024"
              />
            )}
          </div>

          <div className="space-y-2">
            <div className="flex items-center space-x-2">
              <Switch
                id="downloadEnabled"
                checked={downloadEnabled}
                onCheckedChange={setDownloadEnabled}
              />
              <Label htmlFor="downloadEnabled">Set download limit (KB/s)</Label>
            </div>
            {downloadEnabled && (
              <Input
                type="number"
                min="0"
                value={downloadLimit}
                onChange={(e) => setDownloadLimit(parseInt(e.target.value) || 0)}
                placeholder="1024"
              />
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button onClick={handleSubmit} disabled={isPending}>
            {isPending ? "Setting..." : "Apply Limits"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

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
  onFilteredDataUpdate?: (torrents: Torrent[], total: number, counts?: TorrentCounts, categories?: Record<string, Category>, tags?: string[]) => void
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
}: {
  torrent: Torrent
  isSelected: boolean
  onSelect: (selected: boolean) => void
  onClick: () => void
  onLongPress: (torrent: Torrent) => void
  incognitoMode: boolean
  selectionMode: boolean
  speedUnit: SpeedUnit
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
        <div className="absolute inset-0 rounded-lg ring-2 ring-primary ring-inset pointer-events-none"/>
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
                <Clock className="h-3 w-3 text-muted-foreground"/>
                <span className="text-xs text-muted-foreground">{formatEta(torrent.eta)}</span>
              </div>
            )}
            <span className="text-xs font-medium">
              {Math.round(torrent.progress * 100)}%
            </span>
          </div>
        </div>
        <Progress value={torrent.progress * 100} className="h-2"/>
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
              <ChevronDown className="h-3 w-3 [color:var(--chart-2)]"/>
              <span className="font-medium">{formatSpeedWithUnit(torrent.dlspeed, speedUnit)}</span>
            </div>
          )}

          {/* Upload speed */}
          {torrent.upspeed > 0 && (
            <div className="flex items-center gap-1">
              <ChevronUp className="h-3 w-3 [color:var(--chart-3)]"/>
              <span className="font-medium">{formatSpeedWithUnit(torrent.upspeed, speedUnit)}</span>
            </div>
          )}
        </div>

        {/* State badge on the right */}
        <Badge variant={getStatusBadgeVariant(torrent.state)} className="text-xs">
          {getStateLabel(torrent.state)}
        </Badge>
      </div>

      {/* Bottom row: Category and Tags */}
      <div className="flex items-center justify-between gap-2 min-h-[20px]">
        {/* Category */}
        {displayCategory && (
          <div className="flex items-center gap-1 flex-shrink-0">
            <Folder className="h-3 w-3 text-muted-foreground"/>
            <span className="text-xs text-muted-foreground">{displayCategory}</span>
          </div>
        )}

        {/* Tags - aligned to the right */}
        {displayTags && (
          <div className="flex items-center gap-1 flex-wrap justify-end ml-auto">
            <Tag className="h-3 w-3 text-muted-foreground flex-shrink-0"/>
            {(Array.isArray(displayTags) ? displayTags : displayTags.split(",")).map((tag, i) => (
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
  onFilteredDataUpdate,
}: TorrentCardsMobileProps) {
  // State
  const [globalFilter, setGlobalFilter] = useState("")
  const [immediateSearch] = useState("")
  const [selectedHashes, setSelectedHashes] = useState<Set<string>>(new Set())
  const [selectionMode, setSelectionMode] = useState(false)
  const { setIsSelectionMode } = useTorrentSelection()

  const parentRef = useRef<HTMLDivElement>(null)
  const [torrentToDelete, setTorrentToDelete] = useState<Torrent | null>(null)
  const [showActionsSheet, setShowActionsSheet] = useState(false)
  const [actionTorrents, setActionTorrents] = useState<Torrent[]>([]);
  const [showShareLimitDialog, setShowShareLimitDialog] = useState(false)
  const [showSpeedLimitDialog, setShowSpeedLimitDialog] = useState(false)

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

  // Progressive loading state with async management
  const [loadedRows, setLoadedRows] = useState(100)
  const [isLoadingMoreRows, setIsLoadingMoreRows] = useState(false)

  // Use the shared torrent actions hook
  const {
    showDeleteDialog,
    setShowDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    showSetTagsDialog,
    setShowSetTagsDialog,
    showRemoveTagsDialog,
    setShowRemoveTagsDialog,
    showCategoryDialog,
    setShowCategoryDialog,
    isPending,
    handleAction,
    handleDelete,
    handleSetTags,
    handleRemoveTags,
    handleSetCategory,
    handleSetShareLimit,
    handleSetSpeedLimits,
  } = useTorrentActions({
    instanceId,
    onActionComplete: () => {
      setSelectedHashes(new Set())
      setSelectionMode(false)
      setIsSelectionMode(false)
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
    },
  })

  const { data: metadata } = useInstanceMetadata(instanceId)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}

  const debouncedSearch = useDebounce(globalFilter, 1000)
  const routeSearch = useSearch({ strict: false }) as { q?: string }
  const searchFromRoute = routeSearch?.q || ""

  const effectiveSearch = searchFromRoute || immediateSearch || debouncedSearch

  const { instances } = useInstances()
  const instanceName = useMemo(() => {
    return instances?.find(i => i.id === instanceId)?.name ?? null
  }, [instances, instanceId])

  // Columns controls removed on mobile

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

  // Fetch data
  const {
    torrents,
    totalCount,
    counts,
    categories,
    tags,

    isLoading,
    isLoadingMore,
    hasLoadedAll,
    loadMore: backendLoadMore,
  } = useTorrentsList(instanceId, {
    search: effectiveSearch,
    filters,
  })

  // Call the callback when filtered data updates
  useEffect(() => {
    if (onFilteredDataUpdate && torrents && totalCount !== undefined && !isLoading) {
      onFilteredDataUpdate(torrents, totalCount, counts, categories, tags)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [totalCount, isLoading, torrents.length, counts, categories, tags, onFilteredDataUpdate]) // Update when data changes

  // Calculate the effective selection count for display
  const effectiveSelectionCount = useMemo(() => {
    if (isAllSelected) {
      // When all selected, count is total minus exclusions
      return Math.max(0, totalCount - excludedFromSelectAll.size)
    } else {
      // Regular selection mode - use the selectedHashes size
      return selectedHashes.size
    }
  }, [isAllSelected, totalCount, excludedFromSelectAll.size, selectedHashes.size])

  // Load more rows as user scrolls (progressive loading + backend pagination)
  const loadMore = useCallback((): void => {
    // First, try to load more from virtual scrolling if we have more local data
    if (loadedRows < torrents.length) {
      // Prevent concurrent loads
      if (isLoadingMoreRows) {
        return
      }

      setIsLoadingMoreRows(true)

      setLoadedRows(prev => {
        const newLoadedRows = Math.min(prev + 100, torrents.length)
        return newLoadedRows
      })

      // Reset loading flag after a short delay
      setTimeout(() => setIsLoadingMoreRows(false), 100)
    } else if (!hasLoadedAll && !isLoadingMore && backendLoadMore) {
      // If we've displayed all local data but there's more on backend, load next page
      backendLoadMore()
    }
  }, [torrents.length, isLoadingMoreRows, loadedRows, hasLoadedAll, isLoadingMore, backendLoadMore])

  // Ensure loadedRows never exceeds actual data length
  const safeLoadedRows = Math.min(loadedRows, torrents.length)

  // Also keep loadedRows in sync with actual data to prevent status display issues
  useEffect(() => {
    if (loadedRows > torrents.length && torrents.length > 0) {
      setLoadedRows(torrents.length)
    }
  }, [loadedRows, torrents.length])

  // Virtual scrolling with consistent spacing
  const virtualizer = useVirtualizer({
    count: safeLoadedRows,
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
    // Provide a key to help with item tracking - use hash with index for uniqueness
    getItemKey: useCallback((index: number) => {
      const torrent = torrents[index]
      return torrent?.hash ? `${torrent.hash}-${index}` : `loading-${index}`
    }, [torrents]),
    // Optimized onChange handler following TanStack Virtual best practices
    onChange: (instance, sync) => {
      const vRows = instance.getVirtualItems();
      const lastItem = vRows.at(-1);

      // Only trigger loadMore when scrolling has paused (sync === false) or we're not actively scrolling
      // This prevents excessive loadMore calls during rapid scrolling
      const shouldCheckLoadMore = !sync || !instance.isScrolling

      if (shouldCheckLoadMore && lastItem && lastItem.index >= safeLoadedRows - 20) {
        // Load more if we're near the end of virtual rows OR if we might need more data from backend
        if (safeLoadedRows < torrents.length || (!hasLoadedAll && !isLoadingMore)) {
          loadMore();
        }
      }
    },
  })

  // Force virtualizer to recalculate when count changes
  useEffect(() => {
    virtualizer.measure()
  }, [safeLoadedRows, virtualizer])

  const virtualItems = virtualizer.getVirtualItems()

  // Exit selection mode when no items selected
  useEffect(() => {
    if (selectionMode && effectiveSelectionCount === 0) {
      setSelectionMode(false)
      setIsSelectionMode(false)
    }
  }, [effectiveSelectionCount, selectionMode, setIsSelectionMode])

  // Sync selection mode with context
  useEffect(() => {
    setIsSelectionMode(selectionMode && effectiveSelectionCount > 0)
  }, [selectionMode, effectiveSelectionCount, setIsSelectionMode])

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      setIsSelectionMode(false)
    }
  }, [setIsSelectionMode])

  // Reset loaded rows when data changes significantly
  useEffect(() => {
    // Always ensure loadedRows is at least 100 (or total length if less)
    const targetRows = Math.min(100, torrents.length)

    setLoadedRows(prev => {
      if (torrents.length === 0) {
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
  }, [torrents.length, virtualizer])

  // Reset when filters or search changes
  useEffect(() => {
    // Only reset loadedRows for user-initiated changes, not data updates
    const isRecentUserAction = lastUserAction && (Date.now() - lastUserAction.timestamp < 1000)

    if (isRecentUserAction) {
      const targetRows = Math.min(100, torrents.length || 0)
      setLoadedRows(targetRows)
      setIsLoadingMoreRows(false)

      // Clear selection state when data changes
      setSelectedHashes(new Set())
      setSelectionMode(false)
      setIsSelectionMode(false)
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())

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
  }, [filters, effectiveSearch, instanceId, virtualizer, setIsSelectionMode, torrents.length, lastUserAction])


  // Handlers
  const handleLongPress = useCallback((torrent: Torrent) => {
    setSelectionMode(true)
    setSelectedHashes(new Set([torrent.hash]))
  }, [])

  const handleSelect = useCallback((hash: string, selected: boolean) => {
    if (isAllSelected) {
      if (!selected) {
        // When deselecting in "select all" mode, add to exclusions
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
      // Regular selection mode
      setSelectedHashes(prev => {
        const next = new Set(prev)
        if (selected) {
          next.add(hash)
        } else {
          next.delete(hash)
        }
        return next
      })
    }
  }, [isAllSelected])

  const handleSelectAll = useCallback(() => {
    const currentlySelectedCount = isAllSelected ? effectiveSelectionCount : selectedHashes.size
    const loadedTorrentsCount = torrents.length

    if (currentlySelectedCount === totalCount || (currentlySelectedCount === loadedTorrentsCount && currentlySelectedCount < totalCount)) {
      // Deselect all
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
      setSelectedHashes(new Set())
    } else if (loadedTorrentsCount >= totalCount) {
      // All torrents are loaded, use regular selection
      setSelectedHashes(new Set(torrents.map(t => t.hash)))
      setIsAllSelected(false)
      setExcludedFromSelectAll(new Set())
    } else {
      // Not all torrents are loaded, use "select all" mode
      setIsAllSelected(true)
      setExcludedFromSelectAll(new Set())
      setSelectedHashes(new Set())
    }
  }, [isAllSelected, effectiveSelectionCount, selectedHashes.size, torrents, totalCount])

  const handleBulkAction = useCallback((action: TorrentAction) => {
    const hashes = isAllSelected ? [] : Array.from(selectedHashes)
    handleAction(action, hashes, {
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowActionsSheet(false)
  }, [selectedHashes, handleAction, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleDeleteWrapper = useCallback(async () => {
    const hashes = torrentToDelete ? [torrentToDelete.hash] : (isAllSelected ? [] : Array.from(selectedHashes))

    await handleDelete(
      hashes,
      !torrentToDelete && isAllSelected,
      !torrentToDelete && isAllSelected ? filters : undefined,
      !torrentToDelete && isAllSelected ? effectiveSearch : undefined,
      !torrentToDelete && isAllSelected ? Array.from(excludedFromSelectAll) : undefined
    )
    setTorrentToDelete(null)
  }, [torrentToDelete, isAllSelected, selectedHashes, handleDelete, filters, effectiveSearch, excludedFromSelectAll])

  const handleSetTagsWrapper = useCallback(async (tags: string[]) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    await handleSetTags(
      tags,
      hashes,
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined
    )
    setActionTorrents([])
  }, [isAllSelected, actionTorrents, handleSetTags, filters, effectiveSearch, excludedFromSelectAll])

  const handleSetCategoryWrapper = useCallback(async (category: string) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    await handleSetCategory(
      category,
      hashes,
      isAllSelected,
      isAllSelected ? filters : undefined,
      isAllSelected ? effectiveSearch : undefined,
      isAllSelected ? Array.from(excludedFromSelectAll) : undefined
    )
    setActionTorrents([])
  }, [isAllSelected, actionTorrents, handleSetCategory, filters, effectiveSearch, excludedFromSelectAll])

  const getSelectedTorrents = useMemo(() => {
    if (isAllSelected) {
      // When all are selected, return all torrents minus exclusions
      return torrents.filter(t => !excludedFromSelectAll.has(t.hash))
    } else {
      // Regular selection mode
      return torrents.filter(t => selectedHashes.has(t.hash))
    }
  }, [torrents, selectedHashes, isAllSelected, excludedFromSelectAll])

  return (
    <div className="h-full flex flex-col relative">
      {/* Header with stats */}
      <div className="sticky top-0 z-40 bg-background">
        <div className="pb-3">
          <div className="flex items-center gap-2">
            {instanceName && instances && instances.length > 1 ? (
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <button
                    className="flex items-center text-lg font-semibold max-w-[55%] hover:opacity-80 transition-opacity rounded-sm px-1 -mx-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    aria-label={`Current instance: ${instanceName}. Tap to switch instances.`}
                    aria-haspopup="menu"
                  >
                    <span className="truncate">{instanceName}</span>
                    <ChevronsUpDown className="h-3 w-3 text-muted-foreground ml-1 mt-0.5 opacity-60 flex-shrink-0" />
                  </button>
                </DropdownMenuTrigger>
                <DropdownMenuContent className="w-64" side="bottom" align="start">
                  <DropdownMenuLabel className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">
                    Switch Instance
                  </DropdownMenuLabel>
                  <DropdownMenuSeparator />
                  <div className="max-h-64 overflow-y-auto">
                    {instances.map((instance) => (
                      <DropdownMenuItem key={instance.id} asChild>
                        <Link
                          to="/instances/$instanceId"
                          params={{ instanceId: instance.id.toString() }}
                          className={cn(
                            "flex items-center gap-2 cursor-pointer",
                            instance.id === instanceId && "font-medium"
                          )}
                        >
                          <HardDrive className="h-4 w-4 flex-shrink-0" />
                          <span className="flex-1 truncate">{instance.name}</span>
                          <span
                            className={cn(
                              "h-2 w-2 rounded-full flex-shrink-0",
                              instance.connected ? "bg-green-500" : "bg-red-500"
                            )}
                            aria-label={instance.connected ? "Connected" : "Disconnected"}
                          />
                        </Link>
                      </DropdownMenuItem>
                    ))}
                  </div>
                </DropdownMenuContent>
              </DropdownMenu>
            ) : (
              <div className="text-lg font-semibold truncate max-w-[55%]">
                {instanceName ?? ""}
              </div>
            )}
            <div className="flex-1"/>
            <Button
              size="icon"
              variant="ghost"
              onClick={() => setIncognitoMode(!incognitoMode)}
              title={incognitoMode ? "Disable incognito mode" : "Enable incognito mode"}
            >
              {incognitoMode ? <EyeOff className="h-4 w-4"/> : <Eye className="h-4 w-4"/>}
            </Button>
            {/* Columns control hidden on mobile */}
            {/* Filters button (opens mobile filters sheet) */}
            <Button
              variant="outline"
              size="icon"
              onClick={() => window.dispatchEvent(new Event("qui-open-mobile-filters"))}
              title="Filters"
            >
              <Filter className="h-4 w-4"/>
            </Button>

            <Button
              size="icon"
              variant="outline"
              onClick={() => onAddTorrentModalChange?.(true)}
            >
              <Plus className="h-4 w-4"/>
            </Button>
          </div>
        </div>

        {/* Stats bar */}
        <div className="flex items-center justify-between text-xs mb-3">
          <div className="text-muted-foreground">
            {torrents.length === 0 && isLoading ? (
              "Loading torrents..."
            ) : totalCount === 0 ? (
              "No torrents found"
            ) : (
              <>
                {hasLoadedAll ? (
                  `${torrents.length} torrent${torrents.length !== 1 ? "s" : ""}`
                ) : isLoadingMore ? (
                  "Loading more torrents..."
                ) : (
                  `${safeLoadedRows} of ${totalCount} torrents loaded`
                )}
              </>
            )}
          </div>
          <div className="flex items-center gap-1">
            <button
              onClick={() => setSpeedUnit(speedUnit === "bytes" ? "bits" : "bytes")}
              className="flex items-center gap-1 pl-1.5 py-0.5 rounded-sm transition-all hover:bg-muted/50"
            >
              <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" />
              <span className="text-xs text-muted-foreground">
                {speedUnit === "bytes" ? "MiB/s" : "Mbps"}
              </span>
            </button>
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
                  setIsSelectionMode(false)
                  setIsAllSelected(false)
                  setExcludedFromSelectAll(new Set())
                }}
                className="p-1"
              >
                <X className="h-4 w-4"/>
              </button>
              <span className="text-sm font-medium">
                {isAllSelected ? `All ${effectiveSelectionCount}` : effectiveSelectionCount} selected
              </span>
            </div>
            <button
              onClick={handleSelectAll}
              className="text-sm font-medium"
            >
              {effectiveSelectionCount === totalCount ? "Deselect All" : "Select All"}
            </button>
          </div>
        )}
      </div>

      {/* Torrent cards with virtual scrolling */}
      <div ref={parentRef} className="flex-1 overflow-auto"
        style={{ paddingBottom: "calc(5rem + env(safe-area-inset-bottom))" }}>
        <div
          style={{
            height: `${virtualizer.getTotalSize()}px`,
            width: "100%",
            position: "relative",
          }}
        >
          {virtualItems.map(virtualItem => {
            const torrent = torrents[virtualItem.index]
            const isSelected = isAllSelected ? !excludedFromSelectAll.has(torrent.hash) : selectedHashes.has(torrent.hash)

            return (
              <div
                key={virtualItem.key}
                ref={virtualizer.measureElement}
                data-index={virtualItem.index}
                style={{
                  position: "absolute",
                  top: 0,
                  left: 0,
                  width: "100%",
                  transform: `translateY(${virtualItem.start}px)`,
                  paddingBottom: "12px",
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
                  speedUnit={speedUnit}
                />
              </div>
            )
          })}
        </div>

        {/* Progressive loading implemented - shows loading indicator when needed */}
        {safeLoadedRows < torrents.length && !isLoadingMore && (
          <div className="p-4 text-center">
            <Button
              variant="ghost"
              onClick={loadMore}
              disabled={isLoadingMoreRows}
              className="text-muted-foreground"
            >
              {isLoadingMoreRows ? "Loading..." : "Load More"}
            </Button>
          </div>
        )}

        {isLoadingMore && (
          <div className="p-4 text-center text-muted-foreground">
            <Loader2 className="h-4 w-4 animate-spin mx-auto mb-2" />
            <p className="text-sm">Loading more torrents...</p>
          </div>
        )}
      </div>

      {/* Fixed bottom action bar - visible in selection mode */}
      {selectionMode && effectiveSelectionCount > 0 && (
        <div
          className={cn(
            "fixed bottom-0 left-0 right-0 z-40 lg:hidden bg-background/80 backdrop-blur-md border-t border-border/50",
            "transition-transform duration-200 ease-in-out",
            selectionMode && effectiveSelectionCount > 0 ? "translate-y-0" : "translate-y-full"
          )}
          style={{ paddingBottom: "env(safe-area-inset-bottom)" }}
        >
          <div className="flex items-center justify-around h-16">
            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RESUME)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Play className="h-5 w-5"/>
              <span className="truncate">Resume</span>
            </button>

            <button
              onClick={() => handleBulkAction(TORRENT_ACTIONS.PAUSE)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Pause className="h-5 w-5"/>
              <span className="truncate">Pause</span>
            </button>

            <button
              onClick={() => {
                setActionTorrents(getSelectedTorrents)
                setShowCategoryDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Folder className="h-5 w-5"/>
              <span className="truncate">Category</span>
            </button>

            <button
              onClick={() => {
                setActionTorrents(getSelectedTorrents)
                setShowSetTagsDialog(true)
              }}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Tag className="h-5 w-5"/>
              <span className="truncate">Tags</span>
            </button>

            <button
              onClick={() => setShowActionsSheet(true)}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <MoreVertical className="h-5 w-5"/>
              <span className="truncate">More</span>
            </button>
          </div>
        </div>
      )}

      {/* More actions sheet */}
      <Sheet open={showActionsSheet} onOpenChange={setShowActionsSheet}>
        <SheetContent side="bottom" className="h-auto pb-8">
          <SheetHeader>
            <SheetTitle>Actions
              for {isAllSelected ? `all ${effectiveSelectionCount}` : effectiveSelectionCount} torrent(s)</SheetTitle>
          </SheetHeader>
          <div className="grid gap-2 py-4 px-4">
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.RECHECK)}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4"/>
              Force Recheck
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.REANNOUNCE)}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4"/>
              Reannounce
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.INCREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4"/>
              Increase Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.DECREASE_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4"/>
              Decrease Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.TOP_PRIORITY)}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4"/>
              Top Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction(TORRENT_ACTIONS.BOTTOM_PRIORITY)}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4"/>
              Bottom Priority
            </Button>
            {(() => {
              // Check TMM state across selected torrents
              const tmmStates = getSelectedTorrents?.map(t => t.auto_tmm) ?? []
              const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
              const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
              const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

              if (mixed) {
                return (
                  <>
                    <Button
                      variant="outline"
                      onClick={() => {
                        handleAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, isAllSelected ? [] : Array.from(selectedHashes), { enable: true })
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4"/>
                      Enable TMM (Mixed)
                    </Button>
                    <Button
                      variant="outline"
                      onClick={() => {
                        handleAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, isAllSelected ? [] : Array.from(selectedHashes), { enable: false })
                        setShowActionsSheet(false)
                      }}
                      className="justify-start"
                    >
                      <Settings2 className="mr-2 h-4 w-4"/>
                      Disable TMM (Mixed)
                    </Button>
                  </>
                )
              }

              return (
                <Button
                  variant="outline"
                  onClick={() => {
                    handleAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, isAllSelected ? [] : Array.from(selectedHashes), { enable: !allEnabled })
                    setShowActionsSheet(false)
                  }}
                  className="justify-start"
                >
                  {allEnabled ? (
                    <>
                      <Settings2 className="mr-2 h-4 w-4"/>
                      Disable TMM
                    </>
                  ) : (
                    <>
                      <Settings2 className="mr-2 h-4 w-4"/>
                      Enable TMM
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
              <Sprout className="mr-2 h-4 w-4"/>
              Set Share Limits
            </Button>
            <Button
              variant="outline"
              onClick={() => {
                setShowSpeedLimitDialog(true)
                setShowActionsSheet(false)
              }}
              className="justify-start"
            >
              <Gauge className="mr-2 h-4 w-4"/>
              Set Speed Limits
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                setShowDeleteDialog(true)
                setShowActionsSheet(false)
              }}
              className="justify-start !bg-destructive !text-destructive-foreground"
            >
              <Trash2 className="mr-2 h-4 w-4"/>
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
              Delete {torrentToDelete ? "1" : (isAllSelected ? `all ${effectiveSelectionCount}` : effectiveSelectionCount)} torrent(s)?
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
              onClick={handleDeleteWrapper}
              className="bg-destructive text-destructive-foreground"
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Tags dialog */}
      <SetTagsDialog
        open={showSetTagsDialog}
        onOpenChange={setShowSetTagsDialog}
        availableTags={availableTags || []}
        hashCount={actionTorrents.length}
        onConfirm={handleSetTagsWrapper}
        isPending={isPending}
        initialTags={getCommonTags(actionTorrents)}
      />

      {/* Category dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={actionTorrents.length}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(actionTorrents)}
      />

      {/* Remove Tags dialog */}
      <RemoveTagsDialog
        open={showRemoveTagsDialog}
        onOpenChange={setShowRemoveTagsDialog}
        availableTags={availableTags || []}
        hashCount={actionTorrents.length}
        onConfirm={async (tags) => {
          const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
          await handleRemoveTags(
            tags,
            hashes,
            isAllSelected,
            isAllSelected ? filters : undefined,
            isAllSelected ? effectiveSearch : undefined,
            isAllSelected ? Array.from(excludedFromSelectAll) : undefined
          )
          setActionTorrents([])
        }}
        isPending={isPending}
      />

      {/* Share Limits Dialog */}
      <MobileShareLimitsDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={effectiveSelectionCount}
        onConfirm={async (ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit) => {
          const hashes = isAllSelected ? [] : Array.from(selectedHashes)
          await handleSetShareLimit(
            ratioLimit,
            seedingTimeLimit,
            inactiveSeedingTimeLimit,
            hashes
          )
          setShowShareLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Speed Limits Dialog */}
      <MobileSpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={effectiveSelectionCount}
        onConfirm={async (uploadLimit, downloadLimit) => {
          const hashes = isAllSelected ? [] : Array.from(selectedHashes)
          await handleSetSpeedLimits(uploadLimit, downloadLimit, hashes)
          setShowSpeedLimitDialog(false)
        }}
        isPending={isPending}
      />

      {/* Add torrent dialog */}
      <AddTorrentDialog
        instanceId={instanceId}
        open={addTorrentModalOpen}
        onOpenChange={onAddTorrentModalChange}
      />

      {/* Scroll to top button - only on mobile */}
      <div className="lg:hidden">
        <ScrollToTopButton
          scrollContainerRef={parentRef}
          className="bottom-24 right-4"
        />
      </div>
    </div>
  )
}