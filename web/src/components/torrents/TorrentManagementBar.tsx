/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

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
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useCrossSeedWarning } from "@/hooks/useCrossSeedWarning"
import { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import { useSubcategoriesEnabled } from "@/hooks/usePreferenceValues"
import { TORRENT_ACTIONS, useTorrentActions } from "@/hooks/useTorrentActions"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import { actionTargetRequest, type ActionTarget } from "@/lib/torrent-selection"
import { anyTorrentHasTag, getCommonCategory, getCommonSavePath, getTorrentHashesWithTag, getTotalSize, parseTorrentTags } from "@/lib/torrent-utils"
import { formatBytes } from "@/lib/utils"
import type { Category } from "@/types"
import {
  ArrowDown,
  ArrowUp,
  Blocks,
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
import { memo, useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { DeleteTorrentDialog } from "./DeleteTorrentDialog"
import {
  LocationWarningDialog,
  SetCategoryDialog,
  SetLocationDialog,
  TagEditorDialog,
  ShareLimitDialog,
  SpeedLimitsDialog,
  TmmConfirmDialog
} from "./TorrentDialogs"

interface TorrentManagementBarProps {
  instanceId?: number
  instanceIds?: number[]
  // The Action target that the desktop table publishes (ADR 0034).
  actionTarget: ActionTarget
  totalSelectionSize?: number
  onComplete?: () => void
}

export const TorrentManagementBar = memo(function TorrentManagementBar({
  instanceId,
  instanceIds,
  actionTarget,
  totalSelectionSize = 0,
  onComplete,
}: TorrentManagementBarProps) {
  const { t } = useTranslation("torrents")
  const { count: selectionCount, previewTorrents: selectedTorrents } = actionTarget
  const isAllSelected = actionTarget.kind === "selectAll"
  const hasActionScope = typeof instanceId === "number" && instanceId >= 0
  const actionInstanceId = hasActionScope ? instanceId : -1
  const metadataInstanceId = actionInstanceId > 0 ? actionInstanceId : 0
  const supportsCrossSeedDeleteTools = actionInstanceId >= 0
  const supportsCrossSeedBlocklist = actionInstanceId >= 0

  // Use shared metadata hook to leverage cache from table and filter sidebar
  const { data: metadata, isLoading: isMetadataLoading } = useInstanceMetadata(metadataInstanceId, {
    fallbackDelayMs: 1500,
  })
  const fallbackTags = useMemo(() => {
    const tags = new Set<string>()
    for (const torrent of selectedTorrents) {
      for (const tag of parseTorrentTags(torrent.tags)) {
        tags.add(tag)
      }
    }
    return Array.from(tags).sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }))
  }, [selectedTorrents])
  const fallbackCategories = useMemo(() => {
    const categories: Record<string, Category> = {}
    for (const torrent of selectedTorrents) {
      const name = torrent.category?.trim()
      if (!name) {
        continue
      }
      const existing = categories[name]
      if (!existing) {
        categories[name] = { name, savePath: torrent.save_path ?? "" }
        continue
      }
      if (!existing.savePath && torrent.save_path) {
        categories[name] = { ...existing, savePath: torrent.save_path }
      }
    }
    return categories
  }, [selectedTorrents])
  const availableTags = metadata?.tags?.length ? metadata.tags : fallbackTags
  const availableCategories = Object.keys(metadata?.categories ?? {}).length > 0 ? (metadata?.categories ?? {}) : fallbackCategories

  const isLoadingTagsData = metadataInstanceId > 0 && isMetadataLoading && availableTags.length === 0
  const isLoadingCategoriesData = metadataInstanceId > 0 && isMetadataLoading && Object.keys(availableCategories).length === 0

  // Get capabilities to check subcategory support
  const { data: capabilities } = useInstanceCapabilities(metadataInstanceId, { enabled: metadataInstanceId > 0 })
  const allowSubcategories = useSubcategoriesEnabled(metadataInstanceId, capabilities)

  // Use the shared torrent actions hook
  const {
    showDeleteDialog,
    closeDeleteDialog,
    deleteFiles,
    setDeleteFiles,
    isDeleteFilesLocked,
    toggleDeleteFilesLock,
    blockCrossSeeds,
    setBlockCrossSeeds,
    deleteCrossSeeds,
    setDeleteCrossSeeds,
    showTagsDialog,
    setShowTagsDialog,
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
    showTmmDialog,
    setShowTmmDialog,
    pendingTmmEnable,
    showLocationWarningDialog,
    setShowLocationWarningDialog,
    isPending,
    handleAction,
    handleDelete,
    handleUpdateTags,
    handleSetCategory,
    handleSetLocation,
    handleSetShareLimit,
    handleSetSpeedLimits,
    handleRecheck,
    handleReannounce,
    handleTmmConfirm,
    proceedToLocationDialog,
    prepareDeleteAction,
    prepareTagsAction,
    prepareCategoryAction,
    prepareShareLimitAction,
    prepareSpeedLimitAction,
    prepareLocationAction,
    prepareRecheckAction,
    prepareReannounceAction,
    prepareTmmAction,
  } = useTorrentActions({
    instanceId: actionInstanceId,
    instanceIds,
    onActionComplete: (action) => {
      if (action === TORRENT_ACTIONS.DELETE) {
        onComplete?.()
      }
    },
  })

  // Cross-seed warning for delete dialog
  const crossSeedWarning = useCrossSeedWarning({
    instanceId: actionInstanceId,
    torrents: selectedTorrents,
    selectAll: isAllSelected ? actionTarget.selectAll : undefined,
  })
  const crossSeedAffectedTorrents = useMemo(
    () => (supportsCrossSeedDeleteTools ? crossSeedWarning.affectedTorrents : []),
    [supportsCrossSeedDeleteTools, crossSeedWarning.affectedTorrents]
  )

  const hasCrossSeedTag = useMemo(
    () => supportsCrossSeedBlocklist
      && (anyTorrentHasTag(selectedTorrents, "cross-seed") || anyTorrentHasTag(crossSeedAffectedTorrents, "cross-seed")),
    [supportsCrossSeedBlocklist, selectedTorrents, crossSeedAffectedTorrents]
  )
  const shouldBlockCrossSeeds = hasCrossSeedTag && blockCrossSeeds
  const { blockCrossSeedHashes } = useCrossSeedBlocklistActions(actionInstanceId)

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

  const triggerAction = useCallback((action: (typeof TORRENT_ACTIONS)[keyof typeof TORRENT_ACTIONS], options?: { enable?: boolean }) => {
    handleAction(action, actionTarget, options)
  }, [handleAction, actionTarget])

  const handleDeleteWrapper = useCallback(async () => {
    const crossSeedTargets = deleteCrossSeeds ? buildTorrentActionTargets(crossSeedAffectedTorrents, actionInstanceId) : []
    if (shouldBlockCrossSeeds) {
      const taggedHashes = getTorrentHashesWithTag(selectedTorrents, "cross-seed")
      const crossSeedHashes = deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedAffectedTorrents, "cross-seed") : []
      await blockCrossSeedHashes([...taggedHashes, ...crossSeedHashes], [...buildTorrentActionTargets(selectedTorrents, actionInstanceId), ...crossSeedTargets])
    }

    await handleDelete(actionTarget, crossSeedTargets)
  }, [
    actionInstanceId,
    actionTarget,
    blockCrossSeedHashes,
    crossSeedAffectedTorrents,
    deleteCrossSeeds,
    handleDelete,
    selectedTorrents,
    shouldBlockCrossSeeds,
  ])

  const handleTagsWrapper = useCallback((plan: Parameters<typeof handleUpdateTags>[0]) => {
    void handleUpdateTags(plan, actionTarget)
  }, [handleUpdateTags, actionTarget])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    void handleSetCategory(category, actionTarget)
  }, [handleSetCategory, actionTarget])

  const handleSetLocationWrapper = useCallback((location: string) => {
    void handleSetLocation(location, actionTarget)
  }, [handleSetLocation, actionTarget])

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    const actionMap = {
      topPriority: TORRENT_ACTIONS.TOP_PRIORITY,
      increasePriority: TORRENT_ACTIONS.INCREASE_PRIORITY,
      decreasePriority: TORRENT_ACTIONS.DECREASE_PRIORITY,
      bottomPriority: TORRENT_ACTIONS.BOTTOM_PRIORITY,
    }
    triggerAction(actionMap[action])
  }, [triggerAction])

  const handleSetShareLimitWrapper = useCallback((ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number, shareLimitAction?: string, shareLimitsMode?: string) => {
    void handleSetShareLimit({ ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode }, actionTarget)
  }, [handleSetShareLimit, actionTarget])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    void handleSetSpeedLimits({ uploadLimit, downloadLimit }, actionTarget)
  }, [handleSetSpeedLimits, actionTarget])

  const hasSelection = selectionCount > 0 || isAllSelected
  const isDisabled = !hasActionScope || !hasSelection

  // Keep this guard after hooks so their invocation order stays stable.
  if (!hasActionScope || !hasSelection) {
    return null
  }

  return (
    <>
      <div
        className="flex items-center h-9 dark:bg-input/30 border border-input rounded-md mr-2 px-3 py-2 gap-3 shadow-xs transition-all duration-200"
        role="toolbar"
        aria-label={t("managementBar.ariaLabel", { count: selectionCount })}
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
            <TooltipContent>{t("managementBar.resume")}</TooltipContent>
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
            <TooltipContent>{t("managementBar.pause")}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareRecheckAction(actionTarget)}
                disabled={isPending || isDisabled}
              >
                <CheckCircle className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.forceRecheck")}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareReannounceAction(actionTarget)}
                disabled={isPending || isDisabled}
              >
                <Radio className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.reannounce")}</TooltipContent>
          </Tooltip>

          {(() => {
            const seqDlStates = selectedTorrents.map(t => t.seq_dl)
            const allSeqDlEnabled = seqDlStates.length > 0 && seqDlStates.every(state => state === true)

            return (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => triggerAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, { enable: !allSeqDlEnabled })}
                    disabled={isPending || isDisabled}
                  >
                    <Blocks className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{allSeqDlEnabled ? t("managementBar.sequentialDownload.disable") : t("managementBar.sequentialDownload.enable")}</TooltipContent>
              </Tooltip>
            )
          })()}

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareTagsAction()}
                disabled={isPending || isDisabled}
              >
                <Tag className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.setTags")}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareCategoryAction()}
                disabled={isPending || isDisabled}
              >
                <Folder className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.setCategory")}</TooltipContent>
          </Tooltip>

          <Tooltip>
            <TooltipTrigger asChild>
              <Button
                variant="ghost"
                size="sm"
                onClick={() => prepareLocationAction()}
                disabled={isPending || isDisabled}
              >
                <FolderOpen className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.setLocation")}</TooltipContent>
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
              <TooltipContent>{t("managementBar.queuePriority")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="center">
              <DropdownMenuItem
                onClick={() => handleQueueAction("topPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsUp className="h-4 w-4 mr-2" />
                {t("managementBar.topPriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("increasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowUp className="h-4 w-4 mr-2" />
                {t("managementBar.increasePriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("decreasePriority")}
                disabled={isPending || isDisabled}
              >
                <ArrowDown className="h-4 w-4 mr-2" />
                {t("managementBar.decreasePriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => handleQueueAction("bottomPriority")}
                disabled={isPending || isDisabled}
              >
                <ChevronsDown className="h-4 w-4 mr-2" />
                {t("managementBar.bottomPriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
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
              <TooltipContent>{t("managementBar.limits")}</TooltipContent>
            </Tooltip>
            <DropdownMenuContent>
              <DropdownMenuItem
                onClick={() => prepareShareLimitAction()}
                disabled={isPending || isDisabled}
              >
                <Sprout className="mr-2 h-4 w-4" />
                {t("managementBar.setShareLimit")} {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
              <DropdownMenuItem
                onClick={() => prepareSpeedLimitAction()}
                disabled={isPending || isDisabled}
              >
                <Gauge className="mr-2 h-4 w-4" />
                {t("managementBar.setSpeedLimit")} {selectionCount > 1 ? `(${selectionCount})` : ""}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>

          {/* TMM Toggle */}
          {(() => {
            const tmmStates = selectedTorrents.map(t => t.auto_tmm)
            const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
            const mixed = tmmStates.length > 0 && !tmmStates.every(state => state === allEnabled)

            return (
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => prepareTmmAction(!allEnabled)}
                    disabled={isPending || isDisabled}
                  >
                    <Settings2 className="h-4 w-4" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>
                  {mixed ? t("managementBar.tmm.mixed") : allEnabled ? t("managementBar.tmm.disable") : t("managementBar.tmm.enable")}
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
                onClick={() => prepareDeleteAction()}
                disabled={isPending || isDisabled}
                className="text-destructive hover:text-destructive"
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.delete")}</TooltipContent>
          </Tooltip>
        </div>
      </div>

      <DeleteTorrentDialog
        open={showDeleteDialog}
        onOpenChange={(open) => {
          if (!open) {
            closeDeleteDialog()
            crossSeedWarning.reset()
          }
        }}
        count={selectionCount}
        totalSize={deleteDialogTotalSize}
        formattedSize={deleteDialogFormattedSize}
        deleteFiles={deleteFiles}
        onDeleteFilesChange={setDeleteFiles}
        isDeleteFilesLocked={isDeleteFilesLocked}
        onToggleDeleteFilesLock={toggleDeleteFilesLock}
        deleteCrossSeeds={deleteCrossSeeds}
        onDeleteCrossSeedsChange={setDeleteCrossSeeds}
        showBlockCrossSeeds={hasCrossSeedTag}
        blockCrossSeeds={blockCrossSeeds}
        onBlockCrossSeedsChange={setBlockCrossSeeds}
        crossSeedWarning={supportsCrossSeedDeleteTools ? crossSeedWarning : null}
        onConfirm={handleDeleteWrapper}
        isAllSelected={isAllSelected}
        previewTorrents={selectedTorrents}
      />

      <TagEditorDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        selectedTorrents={selectedTorrents}
        hashCount={selectionCount}
        selectionRequest={{ instanceId: actionInstanceId, ...actionTargetRequest(actionTarget) }}
        onConfirm={handleTagsWrapper}
        isPending={isPending}
        isLoadingTags={isLoadingTagsData}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories}
        hashCount={selectionCount}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(selectedTorrents)}
        isLoadingCategories={isLoadingCategoriesData}
        useSubcategories={allowSubcategories}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={selectionCount}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
        initialLocation={getCommonSavePath(selectedTorrents)}
        instanceId={metadataInstanceId}
        capabilities={capabilities}
      />

      <ShareLimitDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={selectionCount}
        torrents={selectedTorrents}
        onConfirm={handleSetShareLimitWrapper}
        isPending={isPending}
        supportsShareLimitsAction={capabilities?.supportsShareLimitsAction}
        supportsShareLimitsMode={capabilities?.supportsShareLimitsMode}
      />

      <SpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={selectionCount}
        torrents={selectedTorrents}
        onConfirm={handleSetSpeedLimitsWrapper}
        isPending={isPending}
      />

      {/* Force Recheck Confirmation Dialog */}
      <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("recheckDialog.title", { count: selectionCount })}</DialogTitle>
            <DialogDescription>
              {t("recheckDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
              {t("recheckDialog.cancel")}
            </Button>
            <Button onClick={() => void handleRecheck(actionTarget)} disabled={isPending}>
              {t("recheckDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reannounce Confirmation Dialog */}
      <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reannounceDialog.title", { count: selectionCount })}</DialogTitle>
            <DialogDescription>
              {t("reannounceDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
              {t("reannounceDialog.cancel")}
            </Button>
            <Button onClick={() => void handleReannounce(actionTarget)} disabled={isPending}>
              {t("reannounceDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* TMM Confirmation Dialog */}
      <TmmConfirmDialog
        open={showTmmDialog}
        onOpenChange={setShowTmmDialog}
        count={selectionCount}
        enable={pendingTmmEnable}
        onConfirm={() => handleTmmConfirm(actionTarget)}
        isPending={isPending}
      />

      {/* Location Warning Dialog */}
      <LocationWarningDialog
        open={showLocationWarningDialog}
        onOpenChange={setShowLocationWarningDialog}
        count={selectionCount}
        onConfirm={proceedToLocationDialog}
        isPending={isPending}
      />
    </>
  )
})
