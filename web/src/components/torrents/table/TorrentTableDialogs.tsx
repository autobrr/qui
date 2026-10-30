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
import type { useBulkActionWrappers } from "@/hooks/torrent-table/useBulkActionWrappers"
import type { useTorrentListCrossSeed } from "@/hooks/useTorrentListController"
import type { useTorrentsList } from "@/hooks/useTorrentsList"
import { api } from "@/lib/api"
import { actionTargetRequest, type ActionTarget } from "@/lib/torrent-selection"
import { getCommonCategory, getCommonSavePath } from "@/lib/torrent-utils"
import type { Category, CrossInstanceTorrent } from "@/types"
import { useQuery } from "@tanstack/react-query"
import { type Dispatch, type SetStateAction, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { DeleteTorrentDialog } from "../DeleteTorrentDialog"
import {
  CreateAndAssignCategoryDialog,
  LocationWarningDialog,
  RenameTorrentDialog,
  RenameTorrentFileDialog,
  RenameTorrentFolderDialog,
  SetCategoryDialog,
  SetLocationDialog,
  TagEditorDialog,
  SetCommentDialog,
  ShareLimitDialog,
  SpeedLimitsDialog,
  TmmConfirmDialog
} from "../TorrentDialogs"

type Wrappers = ReturnType<typeof useBulkActionWrappers>

export interface TorrentTableDialogsProps {
  instanceId: number
  // Every dialog acts on the Action target of the selection (ADR 0034).
  actionTarget: ActionTarget
  isPending: boolean

  // Dialog open/close state
  showDeleteDialog: boolean
  closeDeleteDialog: () => void
  showCommentDialog: boolean
  setShowCommentDialog: Dispatch<SetStateAction<boolean>>
  showTagsDialog: boolean
  setShowTagsDialog: Dispatch<SetStateAction<boolean>>
  showCategoryDialog: boolean
  setShowCategoryDialog: Dispatch<SetStateAction<boolean>>
  showCreateCategoryDialog: boolean
  setShowCreateCategoryDialog: Dispatch<SetStateAction<boolean>>
  showShareLimitDialog: boolean
  setShowShareLimitDialog: Dispatch<SetStateAction<boolean>>
  showSpeedLimitDialog: boolean
  setShowSpeedLimitDialog: Dispatch<SetStateAction<boolean>>
  showLocationDialog: boolean
  setShowLocationDialog: Dispatch<SetStateAction<boolean>>
  showRenameTorrentDialog: boolean
  setShowRenameTorrentDialog: Dispatch<SetStateAction<boolean>>
  showRenameFileDialog: boolean
  setShowRenameFileDialog: Dispatch<SetStateAction<boolean>>
  showRenameFolderDialog: boolean
  setShowRenameFolderDialog: Dispatch<SetStateAction<boolean>>
  showRecheckDialog: boolean
  setShowRecheckDialog: Dispatch<SetStateAction<boolean>>
  showReannounceDialog: boolean
  setShowReannounceDialog: Dispatch<SetStateAction<boolean>>
  showTmmDialog: boolean
  setShowTmmDialog: Dispatch<SetStateAction<boolean>>
  pendingTmmEnable: boolean
  showLocationWarningDialog: boolean
  setShowLocationWarningDialog: Dispatch<SetStateAction<boolean>>

  // Delete-dialog options
  deleteFiles: boolean
  setDeleteFiles: Dispatch<SetStateAction<boolean>>
  isDeleteFilesLocked: boolean
  toggleDeleteFilesLock: () => void
  blockCrossSeeds: boolean
  setBlockCrossSeeds: Dispatch<SetStateAction<boolean>>
  deleteCrossSeeds: boolean
  setDeleteCrossSeeds: Dispatch<SetStateAction<boolean>>

  // Submit wrappers
  handleDeleteWrapper: Wrappers["handleDeleteWrapper"]
  handleSetCommentWrapper: Wrappers["handleSetCommentWrapper"]
  handleTagsWrapper: Wrappers["handleTagsWrapper"]
  handleSetCategoryWrapper: Wrappers["handleSetCategoryWrapper"]
  handleSetShareLimitWrapper: Wrappers["handleSetShareLimitWrapper"]
  handleSetSpeedLimitsWrapper: Wrappers["handleSetSpeedLimitsWrapper"]
  handleSetLocationWrapper: Wrappers["handleSetLocationWrapper"]
  renameHash: Wrappers["renameHash"]
  handleRenameTorrentWrapper: Wrappers["handleRenameTorrentWrapper"]
  handleRenameFileWrapper: Wrappers["handleRenameFileWrapper"]
  handleRenameFolderWrapper: Wrappers["handleRenameFolderWrapper"]
  handleRecheckWrapper: Wrappers["handleRecheckWrapper"]
  handleReannounceWrapper: Wrappers["handleReannounceWrapper"]
  handleTmmConfirmWrapper: Wrappers["handleTmmConfirmWrapper"]
  proceedToLocationDialog: () => void

  deleteDialogTotalSize: number
  deleteDialogFormattedSize: string

  // Cross-seed
  crossSeedWarning: ReturnType<typeof useTorrentListCrossSeed>["crossSeedWarning"]
  hasCrossSeedTag: boolean

  // Metadata / capabilities
  availableTags: string[]
  availableCategories: Record<string, Category>
  isLoadingTags: boolean
  isLoadingCategories: boolean
  allowSubcategories: boolean
  capabilities: ReturnType<typeof useTorrentsList>["capabilities"]
}

/**
 * Renders the torrent-table action dialogs (delete, tags, category, locations,
 * renames, share/speed limits, recheck/reannounce/tmm confirmations, …). All
 * open/close state and submit handlers are threaded in from the orchestrator's
 * hooks; this component owns only the rename-entry query that exclusively feeds
 * the rename dialogs.
 */
export function TorrentTableDialogs({
  instanceId,
  actionTarget,
  isPending,
  showDeleteDialog,
  closeDeleteDialog,
  showCommentDialog,
  setShowCommentDialog,
  showTagsDialog,
  setShowTagsDialog,
  showCategoryDialog,
  setShowCategoryDialog,
  showCreateCategoryDialog,
  setShowCreateCategoryDialog,
  showShareLimitDialog,
  setShowShareLimitDialog,
  showSpeedLimitDialog,
  setShowSpeedLimitDialog,
  showLocationDialog,
  setShowLocationDialog,
  showRenameTorrentDialog,
  setShowRenameTorrentDialog,
  showRenameFileDialog,
  setShowRenameFileDialog,
  showRenameFolderDialog,
  setShowRenameFolderDialog,
  showRecheckDialog,
  setShowRecheckDialog,
  showReannounceDialog,
  setShowReannounceDialog,
  showTmmDialog,
  setShowTmmDialog,
  pendingTmmEnable,
  showLocationWarningDialog,
  setShowLocationWarningDialog,
  deleteFiles,
  setDeleteFiles,
  isDeleteFilesLocked,
  toggleDeleteFilesLock,
  blockCrossSeeds,
  setBlockCrossSeeds,
  deleteCrossSeeds,
  setDeleteCrossSeeds,
  handleDeleteWrapper,
  handleSetCommentWrapper,
  handleTagsWrapper,
  handleSetCategoryWrapper,
  handleSetShareLimitWrapper,
  handleSetSpeedLimitsWrapper,
  handleSetLocationWrapper,
  renameHash,
  handleRenameTorrentWrapper,
  handleRenameFileWrapper,
  handleRenameFolderWrapper,
  handleRecheckWrapper,
  handleReannounceWrapper,
  handleTmmConfirmWrapper,
  proceedToLocationDialog,
  deleteDialogTotalSize,
  deleteDialogFormattedSize,
  crossSeedWarning,
  hasCrossSeedTag,
  availableTags,
  availableCategories,
  isLoadingTags,
  isLoadingCategories,
  allowSubcategories,
  capabilities,
}: TorrentTableDialogsProps) {
  const { t } = useTranslation("torrents")
  const { count, previewTorrents } = actionTarget
  const isAllSelected = actionTarget.kind === "selectAll"

  const shouldLoadRenameEntries = (showRenameFileDialog || showRenameFolderDialog) && Boolean(renameHash)

  const {
    data: renameFileData,
    isLoading: renameEntriesLoading,
  } = useQuery({
    queryKey: ["torrent-files", instanceId, renameHash],
    queryFn: () => api.getTorrentFiles(instanceId, renameHash!, { refresh: true }),
    enabled: shouldLoadRenameEntries,
    staleTime: 0,
    gcTime: 5 * 60 * 1000,
  })

  const renameFileEntries = useMemo(() => {
    if (!Array.isArray(renameFileData)) return [] as { name: string }[]
    return renameFileData
      .filter((file) => typeof file?.name === "string")
      .map((file) => ({ name: file.name }))
  }, [renameFileData])

  const renameFolderEntries = useMemo(() => {
    if (renameFileEntries.length === 0) return [] as { name: string }[]
    const folderSet = new Set<string>()
    for (const file of renameFileEntries) {
      const parts = file.name.split("/")
      if (parts.length <= 1) continue
      let current = ""
      for (let i = 0; i < parts.length - 1; i++) {
        current = current ? `${current}/${parts[i]}` : parts[i]
        folderSet.add(current)
      }
    }
    return Array.from(folderSet)
      .sort((a, b) => a.localeCompare(b))
      .map(name => ({ name }))
  }, [renameFileEntries])

  return (
    <>
      <DeleteTorrentDialog
        open={showDeleteDialog}
        onOpenChange={(open) => {
          if (!open) {
            closeDeleteDialog()
            crossSeedWarning.reset()
          }
        }}
        count={count}
        totalSize={deleteDialogTotalSize}
        formattedSize={deleteDialogFormattedSize}
        deleteFiles={deleteFiles}
        onDeleteFilesChange={setDeleteFiles}
        isDeleteFilesLocked={isDeleteFilesLocked}
        onToggleDeleteFilesLock={toggleDeleteFilesLock}
        showBlockCrossSeeds={hasCrossSeedTag}
        blockCrossSeeds={blockCrossSeeds}
        onBlockCrossSeedsChange={setBlockCrossSeeds}
        deleteCrossSeeds={deleteCrossSeeds}
        onDeleteCrossSeedsChange={setDeleteCrossSeeds}
        crossSeedWarning={crossSeedWarning}
        onConfirm={handleDeleteWrapper}
        isAllSelected={isAllSelected}
        previewTorrents={previewTorrents}
      />

      <SetCommentDialog
        open={showCommentDialog}
        onOpenChange={setShowCommentDialog}
        hashCount={count}
        instanceId={
          previewTorrents.length === 1? ((previewTorrents[0] as CrossInstanceTorrent).instanceId ?? instanceId): instanceId
        }
        torrentHash={previewTorrents.length === 1 ? previewTorrents[0].hash : undefined}
        onConfirm={handleSetCommentWrapper}
        isPending={isPending}
      />

      <TagEditorDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        selectedTorrents={previewTorrents}
        hashCount={count}
        selectionRequest={{ instanceId, ...actionTargetRequest(actionTarget) }}
        onConfirm={handleTagsWrapper}
        isPending={isPending}
        isLoadingTags={isLoadingTags}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories || {}}
        hashCount={count}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(previewTorrents)}
        isLoadingCategories={isLoadingCategories}
        useSubcategories={allowSubcategories}
      />

      {/* Create and Assign Category Dialog */}
      <CreateAndAssignCategoryDialog
        open={showCreateCategoryDialog}
        onOpenChange={setShowCreateCategoryDialog}
        hashCount={count}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
      />

      <ShareLimitDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={count}
        torrents={previewTorrents}
        onConfirm={handleSetShareLimitWrapper}
        isPending={isPending}
        supportsShareLimitsAction={capabilities?.supportsShareLimitsAction}
        supportsShareLimitsMode={capabilities?.supportsShareLimitsMode}
      />

      <SpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={count}
        torrents={previewTorrents}
        onConfirm={handleSetSpeedLimitsWrapper}
        isPending={isPending}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={count}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
        initialLocation={getCommonSavePath(previewTorrents)}
        instanceId={instanceId}
        capabilities={capabilities}
      />

      {/* Rename dialogs */}
      <RenameTorrentDialog
        open={showRenameTorrentDialog}
        onOpenChange={setShowRenameTorrentDialog}
        currentName={previewTorrents[0]?.name}
        onConfirm={handleRenameTorrentWrapper}
        isPending={isPending}
      />
      <RenameTorrentFileDialog
        open={showRenameFileDialog}
        onOpenChange={setShowRenameFileDialog}
        files={renameFileEntries}
        isLoading={renameEntriesLoading}
        onConfirm={handleRenameFileWrapper}
        isPending={isPending}
      />
      <RenameTorrentFolderDialog
        open={showRenameFolderDialog}
        onOpenChange={setShowRenameFolderDialog}
        folders={renameFolderEntries}
        isLoading={renameEntriesLoading}
        onConfirm={handleRenameFolderWrapper}
        isPending={isPending}
      />


      {/* Force Recheck Confirmation Dialog */}
      <Dialog open={showRecheckDialog} onOpenChange={setShowRecheckDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("recheckDialog.title", { count })}</DialogTitle>
            <DialogDescription>
              {t("recheckDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRecheckDialog(false)}>
              {t("recheckDialog.cancel")}
            </Button>
            <Button onClick={handleRecheckWrapper} disabled={isPending}>
              {t("recheckDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Reannounce Confirmation Dialog */}
      <Dialog open={showReannounceDialog} onOpenChange={setShowReannounceDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("reannounceDialog.title", { count })}</DialogTitle>
            <DialogDescription>
              {t("reannounceDialog.description")}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowReannounceDialog(false)}>
              {t("reannounceDialog.cancel")}
            </Button>
            <Button onClick={handleReannounceWrapper} disabled={isPending}>
              {t("reannounceDialog.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* TMM Confirmation Dialog */}
      <TmmConfirmDialog
        open={showTmmDialog}
        onOpenChange={setShowTmmDialog}
        count={count}
        enable={pendingTmmEnable}
        onConfirm={handleTmmConfirmWrapper}
        isPending={isPending}
      />

      {/* Location Warning Dialog */}
      <LocationWarningDialog
        open={showLocationWarningDialog}
        onOpenChange={setShowLocationWarningDialog}
        count={count}
        onConfirm={proceedToLocationDialog}
        isPending={isPending}
      />
    </>
  )
}
