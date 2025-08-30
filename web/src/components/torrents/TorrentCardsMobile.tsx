/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearch } from "@tanstack/react-router"
import { useVirtualizer } from "@tanstack/react-virtual"
import { useTorrentsList } from "@/hooks/useTorrentsList"
import { useDebounce } from "@/hooks/useDebounce"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { api } from "@/lib/api"
import { Progress } from "@/components/ui/progress"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet"
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
import { AddTorrentDialog } from "./AddTorrentDialog"
import {
  CheckCircle2,
  ChevronDown,
  ChevronUp,
  Clock,
  Eye,
  EyeOff,
  Filter,
  Folder,
  MoreVertical,
  Pause,
  Play,
  Plus,
  Radio,
  Tag,
  Trash2,
  X
} from "lucide-react"
import { SetCategoryDialog, SetTagsDialog } from "./TorrentDialogs"
// import { createPortal } from 'react-dom'
// Columns dropdown removed on mobile
import type { Torrent } from "@/types"
import { getLinuxCategory, getLinuxIsoName, getLinuxRatio, getLinuxTags, useIncognitoMode } from "@/lib/incognito"
import { cn, formatBytes, formatSpeed } from "@/lib/utils"
import { applyOptimisticUpdates, getStateLabel } from "@/lib/torrent-state-utils"
import { getCommonCategory, getCommonTags } from "@/lib/torrent-utils"
import { toast } from "sonner"
import { useInstances } from "@/hooks/useInstances"
import { useTorrentSelection } from "@/contexts/TorrentSelectionContext"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata.ts";

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
              <span className="font-medium">{formatSpeed(torrent.dlspeed)}</span>
            </div>
          )}

          {/* Upload speed */}
          {torrent.upspeed > 0 && (
            <div className="flex items-center gap-1">
              <ChevronUp className="h-3 w-3 [color:var(--chart-3)]"/>
              <span className="font-medium">{formatSpeed(torrent.upspeed)}</span>
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
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [deleteFiles, setDeleteFiles] = useState(false)
  const [torrentToDelete, setTorrentToDelete] = useState<Torrent | null>(null)
  const [showActionsSheet, setShowActionsSheet] = useState(false)
  const [showTagsDialog, setShowTagsDialog] = useState(false)
  const [showCategoryDialog, setShowCategoryDialog] = useState(false)
  const [actionTorrents, setActionTorrents] = useState<Torrent[]>([]);

  // Custom "select all" state for handling large datasets
  const [isAllSelected, setIsAllSelected] = useState(false)
  const [excludedFromSelectAll, setExcludedFromSelectAll] = useState<Set<string>>(new Set())

  const [incognitoMode, setIncognitoMode] = useIncognitoMode()

  const queryClient = useQueryClient()

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

  // Fetch data
  const {
    torrents,
    totalCount,
    stats,
    counts,
    categories,
    tags,

    isLoading,
    isLoadingMore,
    hasLoadedAll,
    loadMore: loadMoreTorrents,
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

  // Virtual scrolling with consistent spacing
  const virtualizer = useVirtualizer({
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

  // Reset selection when filters or search changes
  useEffect(() => {
    setSelectedHashes(new Set())
    setSelectionMode(false)
    setIsSelectionMode(false)
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())

    // Scroll to top and force virtualizer recalculation
    if (parentRef.current) {
      parentRef.current.scrollTop = 0
    }

    // Force virtualizer to recalculate after a micro-task
    setTimeout(() => {
      virtualizer.scrollToOffset(0)
      virtualizer.measure()
    }, 0)
  }, [filters, effectiveSearch, instanceId, virtualizer, setIsSelectionMode])

  // Mutations
  const mutation = useMutation({
    mutationFn: (data: {
      action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "addTags" | "removeTags" | "setTags" | "setCategory" | "toggleAutoTMM"
      hashes: string[]
      deleteFiles?: boolean
      tags?: string
      category?: string
      enable?: boolean
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
        action: data.action,
        hashes: data.hashes,
        deleteFiles: data.deleteFiles,
        tags: data.tags,
        category: data.category,
        enable: data.enable,
        selectAll: data.selectAll,
        filters: data.filters,
        search: data.search,
        excludeHashes: data.excludeHashes,
      })
    },
    onSuccess: async (_, variables) => {
      if (variables.action === "delete") {
        setSelectedHashes(new Set())
        setSelectionMode(false)
        setIsAllSelected(false)
        setExcludedFromSelectAll(new Set())

        // Optimistically remove from cache
        const cache = queryClient.getQueryCache()
        const queries = cache.findAll({
          queryKey: ["torrents-list", instanceId],
          exact: false,
        })

        queries.forEach(query => {
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

        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const refetchDelay = variables.deleteFiles ? 5000 : 2000

        setTimeout(() => {
          queryClient.refetchQueries({
            queryKey: ["torrents-list", instanceId],
            exact: false,
            type: "active",
          })
          // Also refetch the counts query
          queryClient.refetchQueries({
            queryKey: ["torrent-counts", instanceId],
            exact: false,
            type: "active",
          })
        }, refetchDelay)
      } else {
        // Handle pause/resume optimistically
        if (variables.action === "pause" || variables.action === "resume") {
          const cache = queryClient.getQueryCache()
          const queries = cache.findAll({
            queryKey: ["torrents-list", instanceId],
            exact: false,
          })

          queries.forEach(query => {
            queryClient.setQueryData(query.queryKey, (oldData: {
              torrents?: Torrent[]
              total?: number
              totalCount?: number
            }) => {
              if (!oldData?.torrents) return oldData

              const { torrents: updatedTorrents } = applyOptimisticUpdates(
                oldData.torrents,
                variables.hashes,
                variables.action as "pause" | "resume",
                filters?.status || []
              )

              return {
                ...oldData,
                torrents: updatedTorrents,
                total: updatedTorrents.length,
                totalCount: updatedTorrents.length,
              }
            })
          })
        }

        // For other operations, add delay to allow qBittorrent to process
        // Resume operations need more time for state transition
        const refetchDelay = variables.action === "resume" ? 2000 : 1000

        setTimeout(() => {
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
      }
    },
  })

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

  const handleBulkAction = useCallback((action: "pause" | "resume" | "delete" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority") => {
    const hashes = isAllSelected ? [] : Array.from(selectedHashes)
    mutation.mutate({
      action,
      hashes,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setSelectedHashes(new Set())
    setSelectionMode(false)
    setIsSelectionMode(false)
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
    setShowActionsSheet(false)
  }, [selectedHashes, mutation, setIsSelectionMode, isAllSelected, filters, effectiveSearch, excludedFromSelectAll])

  const handleDelete = async () => {
    const hashes = torrentToDelete ? [torrentToDelete.hash] : (isAllSelected ? [] : Array.from(selectedHashes))
    const deleteCount = torrentToDelete ? 1 : effectiveSelectionCount

    await mutation.mutateAsync({
      action: "delete",
      hashes,
      deleteFiles,
      selectAll: !torrentToDelete && isAllSelected,
      filters: !torrentToDelete && isAllSelected ? filters : undefined,
      search: !torrentToDelete && isAllSelected ? effectiveSearch : undefined,
      excludeHashes: !torrentToDelete && isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowDeleteDialog(false)
    setDeleteFiles(false)
    setTorrentToDelete(null)
    toast.success(`${deleteCount} torrent(s) deleted`)
  }

  const handleSetTags = async (tags: string[]) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)

    try {
      await mutation.mutateAsync({
        action: "setTags",
        hashes,
        tags: tags.join(","),
        selectAll: isAllSelected,
        filters: isAllSelected ? filters : undefined,
        search: isAllSelected ? effectiveSearch : undefined,
        excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
      })
    } catch (error) {
      if ((error as Error).message?.includes("requires qBittorrent")) {
        await mutation.mutateAsync({
          action: "addTags",
          hashes,
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
    setActionTorrents([])
    setSelectedHashes(new Set())
    setSelectionMode(false)
    setIsSelectionMode(false)
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
  }

  const handleSetCategory = async (category: string) => {
    const hashes = isAllSelected ? [] : actionTorrents.map(t => t.hash)
    await mutation.mutateAsync({
      action: "setCategory",
      hashes,
      category,
      selectAll: isAllSelected,
      filters: isAllSelected ? filters : undefined,
      search: isAllSelected ? effectiveSearch : undefined,
      excludeHashes: isAllSelected ? Array.from(excludedFromSelectAll) : undefined,
    })
    setShowCategoryDialog(false)
    setActionTorrents([])
    setSelectedHashes(new Set())
    setSelectionMode(false)
    setIsSelectionMode(false)
    setIsAllSelected(false)
    setExcludedFromSelectAll(new Set())
  }

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
            <div className="text-lg font-semibold truncate max-w-[55%]">
              {instanceName ?? ""}
            </div>
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
        <div className="flex items-center justify-center text-xs mb-3">
          <div className="flex items-center gap-1">
            <ChevronDown className="h-3 w-3"/>
            <span className="font-medium">{formatSpeed(stats.totalDownloadSpeed || 0)}</span>
            <ChevronUp className="h-3 w-3"/>
            <span className="font-medium">{formatSpeed(stats.totalUploadSpeed || 0)}</span>
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
              onClick={() => handleBulkAction("resume")}
              className="flex flex-col items-center justify-center gap-1 px-3 py-2 text-xs font-medium transition-colors min-w-0 flex-1 text-muted-foreground hover:text-foreground"
            >
              <Play className="h-5 w-5"/>
              <span className="truncate">Resume</span>
            </button>

            <button
              onClick={() => handleBulkAction("pause")}
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
                setShowTagsDialog(true)
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
              onClick={() => handleBulkAction("recheck")}
              className="justify-start"
            >
              <CheckCircle2 className="mr-2 h-4 w-4"/>
              Force Recheck
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction("reannounce")}
              className="justify-start"
            >
              <Radio className="mr-2 h-4 w-4"/>
              Reannounce
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction("topPriority")}
              className="justify-start"
            >
              <ChevronUp className="mr-2 h-4 w-4"/>
              Top Priority
            </Button>
            <Button
              variant="outline"
              onClick={() => handleBulkAction("bottomPriority")}
              className="justify-start"
            >
              <ChevronDown className="mr-2 h-4 w-4"/>
              Bottom Priority
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