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
import type { useCrossSeedOrchestration } from "@/hooks/torrent-table/useCrossSeedOrchestration"
import type { useTorrentsList } from "@/hooks/useTorrentsList"
import { api } from "@/lib/api"
import { getCommonCategory, getCommonSavePath } from "@/lib/torrent-utils"
import type { Category, CrossInstanceTorrent, Torrent } from "@/types"
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
  // Context + identity
  instanceId: number
  instanceIds?: number[]
  contextHashes: string[]
  contextTorrents: Torrent[]
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

  // Submit wrappers + meta
  handleDeleteWrapper: Wrappers["handleDeleteWrapper"]
  handleSetCommentWrapper: Wrappers["handleSetCommentWrapper"]
  handleTagsWrapper: Wrappers["handleTagsWrapper"]
  handleSetCategoryWrapper: Wrappers["handleSetCategoryWrapper"]
  handleSetShareLimitWrapper: Wrappers["handleSetShareLimitWrapper"]
  handleSetSpeedLimitsWrapper: Wrappers["handleSetSpeedLimitsWrapper"]
  handleSetLocationWrapper: Wrappers["handleSetLocationWrapper"]
  handleRenameTorrentWrapper: Wrappers["handleRenameTorrentWrapper"]
  handleRenameFileWrapper: Wrappers["handleRenameFileWrapper"]
  handleRenameFolderWrapper: Wrappers["handleRenameFolderWrapper"]
  handleRecheckWrapper: Wrappers["handleRecheckWrapper"]
  handleReannounceWrapper: Wrappers["handleReannounceWrapper"]
  handleTmmConfirmWrapper: Wrappers["handleTmmConfirmWrapper"]
  proceedToLocationDialog: () => void
  normalizedSelectionFilters: Wrappers["normalizedSelectionFilters"]
  contextClientMeta: Wrappers["contextClientMeta"]

  // Selection derivations
  isAllSelected: boolean
  effectiveSelectionCount: number
  deleteDialogTotalSize: number
  deleteDialogFormattedSize: string
  selectAllExcludeHashes?: string[]
  selectAllExcludedTargets: Array<{ instanceId: number; hash: string }>

  // Cross-seed
  crossSeedWarning: ReturnType<typeof useCrossSeedOrchestration>["crossSeedWarning"]
  hasCrossSeedTag: boolean

  // Metadata / capabilities
  availableTags: string[]
  availableCategories: Record<string, Category>
  isLoadingTags: boolean
  isLoadingCategories: boolean
  allowSubcategories: boolean
  capabilities: ReturnType<typeof useTorrentsList>["capabilities"]
  isCrossInstanceEndpoint?: boolean
  effectiveSearch: string
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
  instanceIds,
  contextHashes,
  contextTorrents,
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
  handleRenameTorrentWrapper,
  handleRenameFileWrapper,
  handleRenameFolderWrapper,
  handleRecheckWrapper,
  handleReannounceWrapper,
  handleTmmConfirmWrapper,
  proceedToLocationDialog,
  normalizedSelectionFilters,
  contextClientMeta,
  isAllSelected,
  effectiveSelectionCount,
  deleteDialogTotalSize,
  deleteDialogFormattedSize,
  selectAllExcludeHashes,
  selectAllExcludedTargets,
  crossSeedWarning,
  hasCrossSeedTag,
  availableTags,
  availableCategories,
  isLoadingTags,
  isLoadingCategories,
  allowSubcategories,
  capabilities,
  isCrossInstanceEndpoint,
  effectiveSearch,
}: TorrentTableDialogsProps) {
  const { t } = useTranslation("torrents")

  const shouldLoadRenameEntries = (showRenameFileDialog || showRenameFolderDialog) && Boolean(contextHashes[0])

  const {
    data: renameFileData,
    isLoading: renameEntriesLoading,
  } = useQuery({
    queryKey: ["torrent-files", instanceId, contextHashes[0]],
    queryFn: () => api.getTorrentFiles(instanceId, contextHashes[0]!, { refresh: true }),
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
        count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
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
      />

      <SetCommentDialog
        open={showCommentDialog}
        onOpenChange={setShowCommentDialog}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        instanceId={
          contextTorrents.length === 1? ((contextTorrents[0] as CrossInstanceTorrent).instanceId ?? instanceId): instanceId
        }
        torrentHash={contextHashes.length === 1 ? contextHashes[0] : undefined}
        onConfirm={handleSetCommentWrapper}
        isPending={isPending}
      />

      <TagEditorDialog
        open={showTagsDialog}
        onOpenChange={setShowTagsDialog}
        availableTags={availableTags || []}
        selectedTorrents={contextTorrents}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        selectionRequest={{
          instanceId,
          instanceIds: isCrossInstanceEndpoint ? instanceIds : undefined,
          hashes: !isAllSelected ? contextHashes : undefined,
          targets: !isAllSelected && (contextClientMeta.actionTargets?.length ?? 0) === contextHashes.length ? contextClientMeta.actionTargets : undefined,
          selectAll: isAllSelected,
          filters: isAllSelected ? normalizedSelectionFilters : undefined,
          search: isAllSelected ? effectiveSearch : undefined,
          excludeHashes: isAllSelected ? selectAllExcludeHashes : undefined,
          excludeTargets: isAllSelected && isCrossInstanceEndpoint ? selectAllExcludedTargets : undefined,
        }}
        onConfirm={handleTagsWrapper}
        isPending={isPending}
        isLoadingTags={isLoadingTags}
      />

      {/* Set Category Dialog */}
      <SetCategoryDialog
        open={showCategoryDialog}
        onOpenChange={setShowCategoryDialog}
        availableCategories={availableCategories || {}}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
        initialCategory={getCommonCategory(contextTorrents)}
        isLoadingCategories={isLoadingCategories}
        useSubcategories={allowSubcategories}
      />

      {/* Create and Assign Category Dialog */}
      <CreateAndAssignCategoryDialog
        open={showCreateCategoryDialog}
        onOpenChange={setShowCreateCategoryDialog}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        onConfirm={handleSetCategoryWrapper}
        isPending={isPending}
      />

      <ShareLimitDialog
        open={showShareLimitDialog}
        onOpenChange={setShowShareLimitDialog}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        torrents={contextTorrents}
        onConfirm={handleSetShareLimitWrapper}
        isPending={isPending}
        supportsShareLimitsAction={capabilities?.supportsShareLimitsAction}
        supportsShareLimitsMode={capabilities?.supportsShareLimitsMode}
      />

      <SpeedLimitsDialog
        open={showSpeedLimitDialog}
        onOpenChange={setShowSpeedLimitDialog}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        torrents={contextTorrents}
        onConfirm={handleSetSpeedLimitsWrapper}
        isPending={isPending}
      />

      {/* Set Location Dialog */}
      <SetLocationDialog
        open={showLocationDialog}
        onOpenChange={setShowLocationDialog}
        hashCount={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        onConfirm={handleSetLocationWrapper}
        isPending={isPending}
        initialLocation={getCommonSavePath(contextTorrents)}
        instanceId={instanceId}
        capabilities={capabilities}
      />

      {/* Rename dialogs */}
      <RenameTorrentDialog
        open={showRenameTorrentDialog}
        onOpenChange={setShowRenameTorrentDialog}
        currentName={contextTorrents[0]?.name}
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
            <DialogTitle>{t("recheckDialog.title", { count: isAllSelected ? effectiveSelectionCount : contextHashes.length })}</DialogTitle>
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
            <DialogTitle>{t("reannounceDialog.title", { count: isAllSelected ? effectiveSelectionCount : contextHashes.length })}</DialogTitle>
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
        count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        enable={pendingTmmEnable}
        onConfirm={handleTmmConfirmWrapper}
        isPending={isPending}
      />

      {/* Location Warning Dialog */}
      <LocationWarningDialog
        open={showLocationWarningDialog}
        onOpenChange={setShowLocationWarningDialog}
        count={isAllSelected ? effectiveSelectionCount : contextHashes.length}
        onConfirm={proceedToLocationDialog}
        isPending={isPending}
      />
    </>
  )
}
