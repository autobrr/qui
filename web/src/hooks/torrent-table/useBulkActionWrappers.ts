/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import type { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import type { TorrentAction, useTorrentActions } from "@/hooks/useTorrentActions"
import type { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import type { ActionTarget } from "@/lib/torrent-selection"
import { getTorrentHashesWithTag } from "@/lib/torrent-utils"
import type { Torrent } from "@/types"
import { type Dispatch, type SetStateAction, useCallback } from "react"

type TorrentActions = ReturnType<typeof useTorrentActions>

export interface UseBulkActionWrappersParams {
  actions: TorrentActions
  actionTarget: ActionTarget
  exportTorrents: ReturnType<typeof useTorrentExporter>["exportTorrents"]
  activeSortField: string
  activeSortOrder: "asc" | "desc"
  crossSeedWarning: { affectedTorrents: Torrent[] }
  shouldBlockCrossSeeds: boolean
  blockCrossSeedHashes: ReturnType<typeof useCrossSeedBlocklistActions>["blockCrossSeedHashes"]
  instanceId: number
  setDropPayload: Dispatch<SetStateAction<AddTorrentDropPayload | null>>
  onAddTorrentModalChange?: (open: boolean) => void
}

/**
 * Dialog wiring for the desktop table: each wrapper passes the value of a menu
 * item or a dialog, with the Action target of the selection, to its handler.
 */
export function useBulkActionWrappers({
  actions,
  actionTarget,
  exportTorrents,
  activeSortField,
  activeSortOrder,
  crossSeedWarning,
  shouldBlockCrossSeeds,
  blockCrossSeedHashes,
  instanceId,
  setDropPayload,
  onAddTorrentModalChange,
}: UseBulkActionWrappersParams) {
  const {
    deleteCrossSeeds,
    handleAction,
    handleDelete,
    handleSetComment,
    handleUpdateTags,
    handleSetCategory,
    handleSetLocation,
    handleRenameTorrent,
    handleRenameFile,
    handleRenameFolder,
    handleRecheck,
    handleReannounce,
    handleTmmConfirm,
    handleSetShareLimit,
    handleSetSpeedLimits,
    prepareRecheckAction,
    prepareReannounceAction,
  } = actions
  // Rename acts on one torrent.
  const renameHash = actionTarget.previewTorrents[0]?.hash

  const runAction = useCallback((action: TorrentAction, options?: { enable?: boolean }) => {
    handleAction(action, actionTarget, options)
  }, [handleAction, actionTarget])

  const handleExportWrapper = useCallback(() => {
    void exportTorrents(actionTarget, activeSortField, activeSortOrder)
  }, [exportTorrents, actionTarget, activeSortField, activeSortOrder])

  const handleDeleteWrapper = useCallback(async () => {
    const crossSeedTargets = deleteCrossSeeds ? buildTorrentActionTargets(crossSeedWarning.affectedTorrents, instanceId) : []

    if (shouldBlockCrossSeeds) {
      const taggedHashes = getTorrentHashesWithTag(actionTarget.previewTorrents, "cross-seed")
      const crossSeedHashes = deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedWarning.affectedTorrents, "cross-seed") : []
      await blockCrossSeedHashes(
        [...taggedHashes, ...crossSeedHashes],
        [...buildTorrentActionTargets(actionTarget.previewTorrents, instanceId), ...crossSeedTargets]
      )
    }

    await handleDelete(actionTarget, crossSeedTargets)
  }, [actionTarget, blockCrossSeedHashes, crossSeedWarning.affectedTorrents, deleteCrossSeeds, handleDelete, instanceId, shouldBlockCrossSeeds])

  const handleSetCommentWrapper = useCallback((comment: string) => {
    void handleSetComment(comment, actionTarget)
  }, [handleSetComment, actionTarget])

  const handleTagsWrapper = useCallback((plan: Parameters<TorrentActions["handleUpdateTags"]>[0]) => {
    void handleUpdateTags(plan, actionTarget)
  }, [handleUpdateTags, actionTarget])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    void handleSetCategory(category, actionTarget)
  }, [handleSetCategory, actionTarget])

  const handleSetLocationWrapper = useCallback((location: string) => {
    void handleSetLocation(location, actionTarget)
  }, [handleSetLocation, actionTarget])

  const handleRenameTorrentWrapper = useCallback(async (name: string) => {
    if (!renameHash) return
    await handleRenameTorrent(renameHash, name)
  }, [handleRenameTorrent, renameHash])

  const handleRenameFileWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!renameHash || !oldPath || !newPath) return
    await handleRenameFile(renameHash, oldPath, newPath)
  }, [handleRenameFile, renameHash])

  const handleRenameFolderWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    if (!renameHash || !oldPath || !newPath) return
    await handleRenameFolder(renameHash, oldPath, newPath)
  }, [handleRenameFolder, renameHash])

  const prepareRecheckWrapper = useCallback(() => prepareRecheckAction(actionTarget), [prepareRecheckAction, actionTarget])
  const prepareReannounceWrapper = useCallback(() => prepareReannounceAction(actionTarget), [prepareReannounceAction, actionTarget])
  const handleRecheckWrapper = useCallback(() => void handleRecheck(actionTarget), [handleRecheck, actionTarget])
  const handleReannounceWrapper = useCallback(() => void handleReannounce(actionTarget), [handleReannounce, actionTarget])
  const handleTmmConfirmWrapper = useCallback(() => handleTmmConfirm(actionTarget), [handleTmmConfirm, actionTarget])

  const handleSetShareLimitWrapper = useCallback((
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number,
    shareLimitAction?: string,
    shareLimitsMode?: string
  ) => {
    void handleSetShareLimit({ ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, shareLimitAction, shareLimitsMode }, actionTarget)
  }, [handleSetShareLimit, actionTarget])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    void handleSetSpeedLimits({ uploadLimit, downloadLimit }, actionTarget)
  }, [handleSetSpeedLimits, actionTarget])

  const handleDropPayload = useCallback((payload: AddTorrentDropPayload) => {
    setDropPayload(payload)
    onAddTorrentModalChange?.(true)
  }, [onAddTorrentModalChange, setDropPayload])

  const handleDropPayloadConsumed = useCallback(() => {
    setDropPayload(null)
  }, [setDropPayload])

  return {
    renameHash,
    runAction,
    handleExportWrapper,
    handleDeleteWrapper,
    handleSetCommentWrapper,
    handleTagsWrapper,
    handleSetCategoryWrapper,
    handleSetLocationWrapper,
    handleRenameTorrentWrapper,
    handleRenameFileWrapper,
    handleRenameFolderWrapper,
    prepareRecheckWrapper,
    prepareReannounceWrapper,
    handleRecheckWrapper,
    handleReannounceWrapper,
    handleTmmConfirmWrapper,
    handleSetShareLimitWrapper,
    handleSetSpeedLimitsWrapper,
    handleDropPayload,
    handleDropPayloadConsumed,
  }
}
