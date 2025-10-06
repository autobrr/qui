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
import { formatBytes } from "@/lib/utils"
import { getCommonCategory, getCommonSavePath, getCommonTags, getTotalSize } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { useQuery } from "@tanstack/react-query"
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
import { useTranslation } from "react-i18next"
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
  totalSelectionSize = 0,
  filters,
  search,
  excludeHashes = [],
  onComplete,
}: TorrentManagementBarProps) {
  const { t } = useTranslation()

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
        aria-label={t("torrent_management_bar.toolbar_aria", { count: selectionCount })}
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
            <TooltipContent>{t("torrent_management_bar.actions.resume")}</TooltipContent>
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
            <TooltipContent>{t("torrent_management_bar.actions.pause")}</TooltipContent>
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
            <TooltipContent>{t("torrent_management_bar.actions.recheck")}</TooltipContent>
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
            <TooltipContent>{t("torrent_management_bar.actions.reannounce")}</TooltipContent>
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
              <TooltipContent>{t("torrent_management_bar.actions.tag_actions")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => prepareTagsAction("add", selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.tags.add", { count: selectionCount })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => prepareTagsAction("set", selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Tag className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.tags.replace", { count: selectionCount })}
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
            <TooltipContent>{t("torrent_management_bar.actions.set_category")}</TooltipContent>
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
            <TooltipContent>{t("torrent_management_bar.actions.set_location")}</TooltipContent>
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
              <TooltipContent>{t("torrent_management_bar.actions.queue_priority")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => handleQueueAction("topPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsUp className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.queue.top", { count: selectionCount })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("increasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowUp className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.queue.increase", { count: selectionCount })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("decreasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowDown className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.queue.decrease", { count: selectionCount })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("bottomPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsDown className="h-4 w-4 mr-2" />
                {t("torrent_management_bar.queue.bottom", { count: selectionCount })}
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
              <TooltipContent>{t("torrent_management_bar.actions.limits")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent>
              <DropdownMenuItem
                onClick={() => prepareShareLimitAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Sprout className="mr-2 h-4 w-4" />
                {t("torrent_management_bar.limits.set_share", { count: selectionCount })}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => prepareSpeedLimitAction(selectedHashes, selectedTorrents)}
                disabled={isPending || isDisabled}
              >
                <Gauge className="mr-2 h-4 w-4" />
                {t("torrent_management_bar.limits.set_speed", { count: selectionCount })}
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
                  {mixed ? t("torrent_management_bar.tmm.mixed") : allEnabled ? t("torrent_management_bar.tmm.disable") : t("torrent_management_bar.tmm.enable")}
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
            <TooltipContent>{t("common.buttons.delete")}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("torrent_management_bar.dialogs.delete.title", { count: totalSelectionCount || selectedHashes.length })}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("torrent_management_bar.dialogs.delete.description")}
              {deleteDialogTotalSize > 0 && (
                <span className="block mt-2 text-xs text-muted-foreground">
                  {t("torrent_management_bar.dialogs.delete.total_size")} {deleteDialogFormattedSize}
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
              {t("torrent_management_bar.dialogs.delete.delete_files_label")}
            </label>
          </div>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDeleteWrapper}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              {t("torrent_management_bar.dialogs.delete.delete_button")}
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
            <DialogTitle>{t("torrent_management_bar.dialogs.recheck.title", { count: totalSelectionCount || selectedHashes.length })}</DialogTitle>
            <DialogDescription>
              {t("torrent_management_bar.dialogs.recheck.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleRecheckWrapper} disabled={isPending}>
              {t("torrent_management_bar.dialogs.recheck.recheck_button")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reannounce Confirmation Dialog */}
      <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("torrent_management_bar.dialogs.reannounce.title", { count: totalSelectionCount || selectedHashes.length })}</DialogTitle>
            <DialogDescription>
              {t("torrent_management_bar.dialogs.reannounce.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
              {t("common.cancel")}
            </Button>
            <Button onClick={handleReannounceWrapper} disabled={isPending}>
              {t("torrent_management_bar.dialogs.reannounce.reannounce_button")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
})
