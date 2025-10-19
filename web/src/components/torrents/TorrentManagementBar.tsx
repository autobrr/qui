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
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger
} from "@/components/ui/tooltip"
import { TORRENT_ACTIONS, useTorrentActions } from "@/hooks/useTorrentActions"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { formatBytes } from "@/lib/utils"
import { getCommonCategory, getCommonSavePath, getCommonTags, getTotalSize } from "@/lib/torrent-utils"
import type { Torrent, TorrentFilters } from "@/types"
import {
  ArrowDown,
  ArrowUp,
  CheckCircle,
  ChevronsDown,
  ChevronsUp,
  Folder,
  FolderOpen,
  Gauge,
  List,
  Pause,
  Play,
  Radio,
  Settings2,
  Share2,
  Sprout,
  Tag,
  Trash2
} from "lucide-react"
import { memo, useCallback, useMemo, type ChangeEvent } from "react"
import {
  AddTagsDialog,
  SetCategoryDialog,
  SetLocationDialog,
  SetTagsDialog,
  ShareLimitDialog,
  SpeedLimitsDialog
} from "./TorrentDialogs"

interface TorrentManagementBarProps {
  instanceId?: number
  selectedHashes?: string[]
  selectedTorrents?: Torrent[]
  isAllSelected?: boolean
  totalSelectionCount?: number
  totalSelectionSize?: number
  filters?: TorrentFilters
  search?: string
  excludeHashes?: string[]
  onComplete?: () => void
}

export const TorrentManagementBar = memo(function TorrentManagementBar({
  instanceId,
  selectedHashes = [],
  selectedTorrents = [],
  isAllSelected = false,
  totalSelectionCount = 0,
  totalSelectionSize = 0,
  filters,
  search,
  excludeHashes = [],
  onComplete,
}: TorrentManagementBarProps) {
  // Use shared metadata hook to leverage cache from table and filter sidebar
  const { data: metadata, isLoading: isMetadataLoading } = useInstanceMetadata(instanceId || 0)
  const availableTags = metadata?.tags || []
  const availableCategories = metadata?.categories || {}
  
  const isLoadingTagsData = isMetadataLoading && availableTags.length === 0
  const isLoadingCategoriesData = isMetadataLoading && Object.keys(availableCategories).length === 0

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
    showCategoryDialog,
    setShowCategoryDialog,
    showShareLimitDialog,
    setShowShareLimitDialog,
    showSpeedLimitDialog,
    setShowSpeedLimitDialog,
    showLocationDialog,
    setShowLocationDialog,
    showRecheckDialog,
    setShowRecheckDialog,
    showReannounceDialog,
    setShowReannounceDialog,
    isPending,
    handleAction,
    handleDelete,
    handleAddTags,
    handleSetTags,
    handleSetCategory,
    handleSetLocation,
    handleSetShareLimit,
    handleSetSpeedLimits,
    handleRecheck,
    handleReannounce,
    prepareDeleteAction,
    prepareTagsAction,
    prepareCategoryAction,
    prepareShareLimitAction,
    prepareSpeedLimitAction,
    prepareLocationAction,
    prepareRecheckAction,
    prepareReannounceAction,
  } = useTorrentActions({
    instanceId: instanceId || 0,
    onActionComplete: onComplete,
  })

  const selectionCount = totalSelectionCount || selectedHashes.length

  // Wrapper functions to adapt hook handlers to component needs
  const actionHashes = useMemo(() => (isAllSelected ? [] : selectedHashes), [isAllSelected, selectedHashes])
  const actionOptions = useMemo(() => ({
    selectAll: isAllSelected,
    filters: isAllSelected ? filters : undefined,
    search: isAllSelected ? search : undefined,
    excludeHashes: isAllSelected ? excludeHashes : undefined,
    clientHashes: selectedHashes,
    clientCount: selectionCount,
  }), [isAllSelected, filters, search, excludeHashes, selectedHashes, selectionCount])

  const clientMeta = useMemo(() => ({
    clientHashes: selectedHashes,
    totalSelected: selectionCount,
  }), [selectedHashes, selectionCount])

  const deleteDialogTotalSize = useMemo(() => {
    if (totalSelectionSize > 0) {
      return totalSelectionSize
    }

    if (selectedTorrents.length > 0) {
      return getTotalSize(selectedTorrents)
    }

    return 0
  }, [totalSelectionSize, selectedTorrents])
  const deleteDialogFormattedSize = useMemo(() => formatBytes(deleteDialogTotalSize), [deleteDialogTotalSize])

  const triggerAction = useCallback((action: (typeof TORRENT_ACTIONS)[keyof typeof TORRENT_ACTIONS], extra?: Parameters<typeof handleAction>[2]) => {
    handleAction(action, actionHashes, {
      ...actionOptions,
      ...extra,
    })
  }, [handleAction, actionHashes, actionOptions])

  const handleDeleteWrapper = useCallback(() => {
    handleDelete(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleDelete, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleAddTagsWrapper = useCallback((tags: string[]) => {
    handleAddTags(
      tags,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleAddTags, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleSetTagsWrapper = useCallback((tags: string[]) => {
    handleSetTags(
      tags,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleSetTags, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    handleSetCategory(
      category,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleSetCategory, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleSetLocationWrapper = useCallback((location: string) => {
    handleSetLocation(
      location,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleSetLocation, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleRecheckWrapper = useCallback(() => {
    handleRecheck(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleRecheck, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleReannounceWrapper = useCallback(() => {
    handleReannounce(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleReannounce, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleRecheckClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      prepareRecheckAction(selectedHashes, count)
    } else {
      triggerAction(TORRENT_ACTIONS.RECHECK)
    }
  }, [totalSelectionCount, selectedHashes, prepareRecheckAction, triggerAction])

  const handleReannounceClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      prepareReannounceAction(selectedHashes, count)
    } else {
      triggerAction(TORRENT_ACTIONS.REANNOUNCE)
    }
  }, [totalSelectionCount, selectedHashes, prepareReannounceAction, triggerAction])

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    const actionMap = {
      topPriority: TORRENT_ACTIONS.TOP_PRIORITY,
      increasePriority: TORRENT_ACTIONS.INCREASE_PRIORITY,
      decreasePriority: TORRENT_ACTIONS.DECREASE_PRIORITY,
      bottomPriority: TORRENT_ACTIONS.BOTTOM_PRIORITY,
    }
    triggerAction(actionMap[action])
  }, [triggerAction])

  const handleSetShareLimitWrapper = useCallback((ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
    handleSetShareLimit(
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleSetShareLimit, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    handleSetSpeedLimits(
      uploadLimit,
      downloadLimit,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes,
      clientMeta
    )
  }, [handleSetSpeedLimits, selectedHashes, isAllSelected, filters, search, excludeHashes, clientMeta])

  const hasSelection = selectionCount > 0 || isAllSelected
  const isDisabled = !instanceId || !hasSelection


  return (
    <>
      <div
        className="flex items-center h-9 dark:bg-input/30 border border-input rounded-md mr-2 px-3 py-2 gap-3 shadow-xs transition-all duration-200"
        role="toolbar"
        aria-label={`${selectionCount} torrent${selectionCount !== 1 ? "s" : ""} selected - Bulk actions available`}
      >
        <div className="flex items-center gap-3 flex-shrink-0 min-w-0">
          <span className="text-xs text-muted-foreground whitespace-nowrap min-w-[3ch] text-center">
            {selectionCount}
          </span>
        </div>

        <div className="flex items-center gap-1">
          {/* Primary Actions */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => triggerAction(TORRENT_ACTIONS.RESUME)}
                disabled={isPending || isDisabled}
              >
                <Play className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Resume</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => triggerAction(TORRENT_ACTIONS.PAUSE)}
                disabled={isPending || isDisabled}
              >
                <Pause className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Pause</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleRecheckClick}
                disabled={isPending || isDisabled}
              >
                <CheckCircle className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Force Recheck</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={handleReannounceClick}
                disabled={isPending || isDisabled}
              >
                <Radio className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Reannounce</TooltipContent>
          </Tooltip>

          {/* Tag Actions */}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={isPending || isDisabled}
                  >
                    <Tag className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Tag Actions</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => prepareTagsAction("add", selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                Add Tags {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => prepareTagsAction("set", selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                Replace Tags {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareCategoryAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Folder className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Set Category</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareLocationAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <FolderOpen className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Set Location</TooltipContent>
          </Tooltip>

          {/* Queue Priority */}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={isPending || isDisabled}
                  >
                    <List className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Queue Priority</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => handleQueueAction("topPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsUp className="h-4 w-4 mr-2" />
                Top Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("increasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowUp className="h-4 w-4 mr-2" />
                Increase Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("decreasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowDown className="h-4 w-4 mr-2" />
                Decrease Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("bottomPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsDown className="h-4 w-4 mr-2" />
                Bottom Priority {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {/* Share/Speed Limits */}
          <DropdownMenu>
            <Tooltip>
              <TooltipTrigger asChild>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={isPending || isDisabled}
                  >
                    <Share2 className="h-4 w-4" />
                  </Button>
                </DropdownMenuTrigger>
              </TooltipTrigger>
              <TooltipContent>Limits</TooltipContent>
            </Tooltip>
            <DropdownMenuContent>
              <DropdownMenuItem
                onClick={() => prepareShareLimitAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Sprout className="mr-2 h-4 w-4" />
                Set Share Limit {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => prepareSpeedLimitAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Gauge className="mr-2 h-4 w-4" />
                Set Speed Limit {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {/* TMM Toggle */}
          {(() => {
            const tmmStates = selectedTorrents?.map(t => t.auto_tmm) ?? []
            const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
            const mixed = tmmStates.length > 0 && !tmmStates.every(state => state === allEnabled)

            return (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => triggerAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, { enable: !allEnabled })}
                    disabled={isPending || isDisabled}
                  >
                    <Settings2 className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {mixed ? "TMM (Mixed)" : allEnabled ? "Disable TMM" : "Enable TMM"}
                </TooltipContent>
              </Tooltip>
            )
          })()}

          {/* Delete Action */}
          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareDeleteAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>Delete</TooltipContent>
          </Tooltip>
        </div>
      </div>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete {totalSelectionCount || selectedHashes.length} torrent(s)?</AlertDialogTitle>
            <AlertDialogDescription>
              This action cannot be undone. The torrents will be removed from qBittorrent.
              {deleteDialogTotalSize > 0 && (
                <span className="block mt-2 text-xs text-muted-foreground">
                  Total size: {deleteDialogFormattedSize}
                </span>
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex items-center space-x-2 py-4">
            <input
              type="checkbox"
              id="deleteFiles"
              checked={deleteFiles}
              onChange={(e: ChangeEvent<HTMLInputElement>) => setDeleteFiles(e.target.checked)}
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
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleAddTagsWrapper}
        isPending={isPending}
        isLoadingTags={isLoadingTagsData}
      />

      {/* Set Tags Dialog */}
      <SetTagsDialog
        open={showSetTagsDialog}
        onOpenChange={setShowSetTagsDialog}
        availableTags={availableTags || []}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleSetTagsWrapper}
        isPending={isPending}
        initialTags={getCommonTags(selectedTorrents)}
        isLoadingTags={isLoadingTagsData}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(selectedTorrents)}
        isLoadingCategories={isLoadingCategoriesData}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={totalSelectionCount || selectedHashes.length}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
        initialLocation={getCommonSavePath(selectedTorrents)}
      />

      <ShareLimitDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={totalSelectionCount || selectedHashes.length}
        torrents={selectedTorrents}
        onConfirm={handleSetShareLimitWrapper}
        isPending={isPending}
      />

      <SpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={totalSelectionCount || selectedHashes.length}
        torrents={selectedTorrents}
        onConfirm={handleSetSpeedLimitsWrapper}
        isPending={isPending}
      />

      {/* Force Recheck Confirmation Dialog */}
      <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Force Recheck {totalSelectionCount || selectedHashes.length} torrent(s)?</DialogTitle>
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
            <DialogTitle>Reannounce {totalSelectionCount || selectedHashes.length} torrent(s)?</DialogTitle>
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
    </>
  )
})
