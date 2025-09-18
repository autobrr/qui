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
import { api } from "@/lib/api"
import { getCommonCategory, getCommonSavePath, getCommonTags } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { ArrowDown, ArrowUp, ChevronsDown, ChevronsUp, Folder, FolderOpen, List, LoaderCircle, Pause, Play, Radio, Settings2, Share2, Tag, Trash2 } from "lucide-react"
import type { ChangeEvent } from "react"
import { memo, useCallback } from "react"
import { AddTagsDialog, SetCategoryDialog, SetLocationDialog, SetTagsDialog } from "./TorrentDialogs"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"

interface TorrentManagementBarProps {
  instanceId?: number
  selectedHashes?: string[]
  selectedTorrents?: Torrent[]
  isAllSelected?: boolean
  totalSelectionCount?: number
  filters?: {
    status: string[]
    categories: string[]
    tags: string[]
    trackers: string[]
  }
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
  filters,
  search,
  excludeHashes = [],
  onComplete,
}: TorrentManagementBarProps) {
  // Fetch available tags
  const { data: availableTags = [] } = useQuery({
    queryKey: ["tags", instanceId],
    queryFn: () => instanceId ? api.getTags(instanceId) : Promise.resolve([]),
    enabled: !!instanceId,
    staleTime: 60000,
  })

  // Fetch available categories
  const { data: availableCategories = {} } = useQuery({
    queryKey: ["categories", instanceId],
    queryFn: () => instanceId ? api.getCategories(instanceId) : Promise.resolve({}),
    enabled: !!instanceId,
    staleTime: 60000,
  })

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
    prepareLocationAction,
    prepareRecheckAction,
    prepareReannounceAction,
  } = useTorrentActions({
    instanceId: instanceId || 0,
    onActionComplete: onComplete,
  })

  // Wrapper functions to adapt hook handlers to component needs
  const handleDeleteWrapper = useCallback(() => {
    handleDelete(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleDelete, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleAddTagsWrapper = useCallback((tags: string[]) => {
    handleAddTags(
      tags,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleAddTags, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleSetTagsWrapper = useCallback((tags: string[]) => {
    handleSetTags(
      tags,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleSetTags, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    handleSetCategory(
      category,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleSetCategory, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleSetLocationWrapper = useCallback((location: string) => {
    handleSetLocation(
      location,
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleSetLocation, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleRecheckWrapper = useCallback(() => {
    handleRecheck(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleRecheck, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleReannounceWrapper = useCallback(() => {
    handleReannounce(
      selectedHashes,
      isAllSelected,
      filters,
      search,
      excludeHashes
    )
  }, [handleReannounce, selectedHashes, isAllSelected, filters, search, excludeHashes])

  const handleRecheckClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      prepareRecheckAction(selectedHashes, count)
    } else {
      handleAction(TORRENT_ACTIONS.RECHECK, selectedHashes)
    }
  }, [totalSelectionCount, selectedHashes, prepareRecheckAction, handleAction])

  const handleReannounceClick = useCallback(() => {
    const count = totalSelectionCount || selectedHashes.length
    if (count > 1) {
      prepareReannounceAction(selectedHashes, count)
    } else {
      handleAction(TORRENT_ACTIONS.REANNOUNCE, selectedHashes)
    }
  }, [totalSelectionCount, selectedHashes, prepareReannounceAction, handleAction])

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    const actionMap = {
      topPriority: TORRENT_ACTIONS.TOP_PRIORITY,
      increasePriority: TORRENT_ACTIONS.INCREASE_PRIORITY,
      decreasePriority: TORRENT_ACTIONS.DECREASE_PRIORITY,
      bottomPriority: TORRENT_ACTIONS.BOTTOM_PRIORITY,
    }
    handleAction(actionMap[action], selectedHashes)
  }, [handleAction, selectedHashes])

  const handleSetShareLimitWrapper = useCallback((ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
    handleSetShareLimit(ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, selectedHashes)
  }, [handleSetShareLimit, selectedHashes])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    handleSetSpeedLimits(uploadLimit, downloadLimit, selectedHashes)
  }, [handleSetSpeedLimits, selectedHashes])

  const selectionCount = totalSelectionCount || selectedHashes.length
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
                onClick={() => handleAction(TORRENT_ACTIONS.RESUME, selectedHashes)}
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
                onClick={() => handleAction(TORRENT_ACTIONS.PAUSE, selectedHashes)}
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
                <LoaderCircle className="h-4 w-4" />
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
            <DropdownMenuContent align="center" className="w-72">
              <ShareLimitSubmenu
                type="dropdown"
                hashCount={selectionCount}
                onConfirm={handleSetShareLimitWrapper}
                isPending={isPending}
              />
              <SpeedLimitsSubmenu
                type="dropdown"
                hashCount={selectionCount}
                onConfirm={handleSetSpeedLimitsWrapper}
                isPending={isPending}
              />
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
                    onClick={() => handleAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, selectedHashes, { enable: !allEnabled })}
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