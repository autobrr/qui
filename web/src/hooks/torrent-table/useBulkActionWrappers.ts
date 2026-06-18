/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import type { AddTorrentDropPayload } from "@/components/torrents/AddTorrentDialog"
import type { useCrossSeedBlocklistActions } from "@/hooks/useCrossSeedBlocklistActions"
import { TORRENT_ACTIONS, type useTorrentActions } from "@/hooks/useTorrentActions"
import type { useTorrentExporter } from "@/hooks/useTorrentExporter"
import { buildTorrentActionTargets, type TorrentActionTarget } from "@/lib/torrent-action-targets"
import { getTorrentHashesWithTag } from "@/lib/torrent-utils"
import type { Torrent, TorrentFilters } from "@/types"
import { type Dispatch, type SetStateAction, useCallback, useMemo } from "react"

type TorrentActions = ReturnType<typeof useTorrentActions>
type HandleAction = TorrentActions["handleAction"]

export interface UseBulkActionWrappersParams {
  // From useTorrentActions (threaded in — the hook stays a single parent-level call)
  handleAction: HandleAction
  handleDelete: TorrentActions["handleDelete"]
  handleSetComment: TorrentActions["handleSetComment"]
  handleUpdateTags: TorrentActions["handleUpdateTags"]
  handleSetCategory: TorrentActions["handleSetCategory"]
  handleSetLocation: TorrentActions["handleSetLocation"]
  handleRenameTorrent: TorrentActions["handleRenameTorrent"]
  handleRenameFile: TorrentActions["handleRenameFile"]
  handleRenameFolder: TorrentActions["handleRenameFolder"]
  handleRecheck: TorrentActions["handleRecheck"]
  handleReannounce: TorrentActions["handleReannounce"]
  handleTmmConfirm: TorrentActions["handleTmmConfirm"]
  handleSetShareLimit: TorrentActions["handleSetShareLimit"]
  handleSetSpeedLimits: TorrentActions["handleSetSpeedLimits"]
  contextHashes: string[]
  contextTorrents: Torrent[]
  deleteCrossSeeds: boolean
  // From useTorrentExporter
  exportTorrents: ReturnType<typeof useTorrentExporter>["exportTorrents"]
  // Selection state + derivations
  isAllSelected: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  effectiveSelectionCount: number
  selectAllFilters: TorrentFilters | undefined
  selectAllExcludeHashes: string[] | undefined
  selectAllExcludedTargets: TorrentActionTarget[]
  // Filter / search / sort context
  filters?: TorrentFilters
  effectiveSearch: string
  activeSortField: string
  activeSortOrder: "asc" | "desc"
  // Cross-seed orchestration
  crossSeedWarning: { affectedTorrents: Torrent[] }
  shouldBlockCrossSeeds: boolean
  blockCrossSeedHashes: ReturnType<typeof useCrossSeedBlocklistActions>["blockCrossSeedHashes"]
  // Misc context
  isCrossInstanceEndpoint?: boolean
  instanceIds?: number[]
  instanceId: number
  setDropPayload: Dispatch<SetStateAction<AddTorrentDropPayload | null>>
  onAddTorrentModalChange?: (open: boolean) => void
}

/**
 * Adapts the raw useTorrentActions handlers into the bulk-action wrappers the
 * table's dialogs and context menu call. Each wrapper resolves the correct
 * select-all vs. context-selection targeting (hashes, filters, search,
 * exclusions, client metadata) before delegating.
 */
export function useBulkActionWrappers({
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
  contextHashes,
  contextTorrents,
  deleteCrossSeeds,
  exportTorrents,
  isAllSelected,
  selectedHashes,
  selectedTorrents,
  effectiveSelectionCount,
  selectAllFilters,
  selectAllExcludeHashes,
  selectAllExcludedTargets,
  filters,
  effectiveSearch,
  activeSortField,
  activeSortOrder,
  crossSeedWarning,
  shouldBlockCrossSeeds,
  blockCrossSeedHashes,
  isCrossInstanceEndpoint,
  instanceIds,
  instanceId,
  setDropPayload,
  onAddTorrentModalChange,
}: UseBulkActionWrappersParams) {
  const selectAllOptions = useMemo(() => ({
    instanceIds: isCrossInstanceEndpoint ? instanceIds : undefined,
    selectAll: isAllSelected,
    filters: selectAllFilters,
    search: isAllSelected ? effectiveSearch : undefined,
    excludeHashes: isAllSelected ? selectAllExcludeHashes : undefined,
    excludeTargets: isAllSelected && isCrossInstanceEndpoint ? selectAllExcludedTargets : undefined,
  }), [isAllSelected, selectAllFilters, effectiveSearch, selectAllExcludeHashes, isCrossInstanceEndpoint, selectAllExcludedTargets, instanceIds])
  const normalizedSelectionFilters = useMemo(() => {
    const sourceFilters = selectAllFilters ?? filters
    if (!sourceFilters) {
      return undefined
    }

    return {
      ...sourceFilters,
      categories: sourceFilters.expandedCategories ?? sourceFilters.categories ?? [],
      excludeCategories: sourceFilters.expandedExcludeCategories ?? sourceFilters.excludeCategories ?? [],
    }
  }, [selectAllFilters, filters])

  const contextClientMeta = useMemo(() => ({
    clientHashes: contextHashes,
    totalSelected: isAllSelected ? effectiveSelectionCount : contextHashes.length,
    actionTargets: buildTorrentActionTargets(contextTorrents, instanceId),
    excludeTargets: isAllSelected && isCrossInstanceEndpoint ? selectAllExcludedTargets : undefined,
  }), [contextHashes, isAllSelected, effectiveSelectionCount, contextTorrents, instanceId, isCrossInstanceEndpoint, selectAllExcludedTargets])

  const runAction = useCallback((action: (typeof TORRENT_ACTIONS)[keyof typeof TORRENT_ACTIONS], hashes: string[], extra?: Parameters<HandleAction>[2]) => {
    const clientHashes = hashes.length > 0 ? hashes : selectedHashes
    const clientCount = isAllSelected ? effectiveSelectionCount : (clientHashes.length || hashes.length || 1)
    const defaultTargets = buildTorrentActionTargets(selectedTorrents, instanceId)
    const actionTargets = isAllSelected ? undefined : (extra?.targets ?? defaultTargets)
    const extraOptions = extra ?? {}
    handleAction(action, isAllSelected ? [] : hashes, {
      ...selectAllOptions,
      ...extraOptions,
      clientHashes,
      clientCount,
      targets: actionTargets,
    })
  }, [handleAction, isAllSelected, selectAllOptions, selectedHashes, effectiveSelectionCount, selectedTorrents, instanceId])

  const handleExportWrapper = useCallback((hashes: string[], torrentsForSelection: Torrent[]) => {
    exportTorrents({
      hashes,
      torrents: torrentsForSelection,
      isAllSelected,
      totalSelected: effectiveSelectionCount,
      filters: selectAllFilters ?? filters,
      search: effectiveSearch,
      excludeHashes: selectAllExcludeHashes,
      excludeTargets: isAllSelected && isCrossInstanceEndpoint ? selectAllExcludedTargets : undefined,
      instanceIds: isCrossInstanceEndpoint ? instanceIds : undefined,
      sortField: activeSortField,
      sortOrder: activeSortOrder,
    })
  }, [
    exportTorrents,
    isAllSelected,
    effectiveSelectionCount,
    selectAllFilters,
    filters,
    effectiveSearch,
    selectAllExcludeHashes,
    selectAllExcludedTargets,
    isCrossInstanceEndpoint,
    instanceIds,
    activeSortField,
    activeSortOrder,
  ])

  const handleDeleteWrapper = useCallback(async () => {
    const crossSeedHashes = deleteCrossSeeds ? getTorrentHashesWithTag(crossSeedWarning.affectedTorrents, "cross-seed") : []

    if (shouldBlockCrossSeeds) {
      const taggedHashes = getTorrentHashesWithTag(contextTorrents, "cross-seed")
      const blocklistTargets = [
        ...(contextClientMeta.actionTargets ?? []),
        ...buildTorrentActionTargets(crossSeedWarning.affectedTorrents, instanceId),
      ]
      await blockCrossSeedHashes([...taggedHashes, ...crossSeedHashes], blocklistTargets)
    }

    // Include cross-seed hashes if user opted to delete them
    const hashesToDelete = deleteCrossSeeds? [...contextHashes, ...crossSeedWarning.affectedTorrents.map(t => t.hash)]: contextHashes

    // Update count to include cross-seeds for accurate toast message
    const deleteClientMeta = deleteCrossSeeds? { clientHashes: hashesToDelete, totalSelected: hashesToDelete.length }: contextClientMeta

    await handleDelete(
      hashesToDelete,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      deleteClientMeta
    )
  }, [
    blockCrossSeedHashes,
    contextClientMeta,
    contextHashes,
    contextTorrents,
    crossSeedWarning.affectedTorrents,
    deleteCrossSeeds,
    effectiveSearch,
    selectAllExcludeHashes,
    filters,
    handleDelete,
    instanceId,
    isAllSelected,
    selectAllFilters,
    shouldBlockCrossSeeds,
  ])

  const handleSetCommentWrapper = useCallback((comment: string) => {
    handleSetComment(
      comment,
      contextHashes,
      isAllSelected,
      normalizedSelectionFilters ?? selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleSetComment, contextHashes, isAllSelected, normalizedSelectionFilters, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleTagsWrapper = useCallback((plan: Parameters<TorrentActions["handleUpdateTags"]>[0]) => {
    handleUpdateTags(
      plan,
      contextHashes,
      isAllSelected,
      normalizedSelectionFilters ?? selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleUpdateTags, contextHashes, isAllSelected, normalizedSelectionFilters, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleSetCategoryWrapper = useCallback((category: string) => {
    handleSetCategory(
      category,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleSetCategory, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  // Direct category handler for context menu submenu
  const handleSetCategoryDirect = useCallback((category: string, hashes: string[], targets?: Array<{ instanceId: number; hash: string }>) => {
    const usingSelectAll = isAllSelected
    const resolvedFilters = usingSelectAll ? (selectAllFilters ?? filters) : undefined
    const resolvedSearch = usingSelectAll ? effectiveSearch : undefined
    const resolvedExclusions = usingSelectAll ? selectAllExcludeHashes : undefined
    const clientHashes = hashes.length > 0 ? hashes : selectedHashes
    const totalSelected = usingSelectAll ? effectiveSelectionCount : (clientHashes.length || 1)

    handleSetCategory(
      category,
      usingSelectAll ? [] : hashes,
      usingSelectAll,
      resolvedFilters,
      resolvedSearch,
      resolvedExclusions,
      {
        clientHashes,
        totalSelected,
        actionTargets: usingSelectAll ? undefined : targets,
        excludeTargets: usingSelectAll ? selectAllExcludedTargets : undefined,
      }
    )
  }, [
    handleSetCategory,
    isAllSelected,
    selectAllFilters,
    filters,
    effectiveSearch,
    selectAllExcludeHashes,
    selectAllExcludedTargets,
    selectedHashes,
    effectiveSelectionCount,
  ])

  const handleSetLocationWrapper = useCallback((location: string) => {
    handleSetLocation(
      location,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleSetLocation, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleRenameTorrentWrapper = useCallback(async (name: string) => {
    const hash = contextHashes[0]
    if (!hash) return
    await handleRenameTorrent(hash, name)
  }, [handleRenameTorrent, contextHashes])

  const handleRenameFileWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    const hash = contextHashes[0]
    if (!hash) return
    if (!oldPath || !newPath) return
    await handleRenameFile(hash, oldPath, newPath)
  }, [handleRenameFile, contextHashes])

  const handleRenameFolderWrapper = useCallback(async ({ oldPath, newPath }: { oldPath: string; newPath: string }) => {
    const hash = contextHashes[0]
    if (!hash) return
    if (!oldPath || !newPath) return
    await handleRenameFolder(hash, oldPath, newPath)
  }, [handleRenameFolder, contextHashes])

  const handleRecheckWrapper = useCallback(() => {
    handleRecheck(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleRecheck, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleReannounceWrapper = useCallback(() => {
    handleReannounce(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleReannounce, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleTmmConfirmWrapper = useCallback(() => {
    handleTmmConfirm(
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleTmmConfirm, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleSetShareLimitWrapper = useCallback((
    ratioLimit: number,
    seedingTimeLimit: number,
    inactiveSeedingTimeLimit: number,
    shareLimitAction?: string,
    shareLimitsMode?: string
  ) => {
    handleSetShareLimit(
      ratioLimit,
      seedingTimeLimit,
      inactiveSeedingTimeLimit,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta,
      shareLimitAction,
      shareLimitsMode
    )
  }, [handleSetShareLimit, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleSetSpeedLimitsWrapper = useCallback((
    uploadLimit: number,
    downloadLimit: number
  ) => {
    handleSetSpeedLimits(
      uploadLimit,
      downloadLimit,
      contextHashes,
      isAllSelected,
      selectAllFilters ?? filters,
      effectiveSearch,
      selectAllExcludeHashes,
      contextClientMeta
    )
  }, [handleSetSpeedLimits, contextHashes, isAllSelected, selectAllFilters, filters, effectiveSearch, selectAllExcludeHashes, contextClientMeta])

  const handleDropPayload = useCallback((payload: AddTorrentDropPayload) => {
    setDropPayload(payload)
    onAddTorrentModalChange?.(true)
  }, [onAddTorrentModalChange, setDropPayload])

  const handleDropPayloadConsumed = useCallback(() => {
    setDropPayload(null)
  }, [setDropPayload])

  return {
    normalizedSelectionFilters,
    contextClientMeta,
    runAction,
    handleExportWrapper,
    handleDeleteWrapper,
    handleSetCommentWrapper,
    handleTagsWrapper,
    handleSetCategoryWrapper,
    handleSetCategoryDirect,
    handleSetLocationWrapper,
    handleRenameTorrentWrapper,
    handleRenameFileWrapper,
    handleRenameFolderWrapper,
    handleRecheckWrapper,
    handleReannounceWrapper,
    handleTmmConfirmWrapper,
    handleSetShareLimitWrapper,
    handleSetSpeedLimitsWrapper,
    handleDropPayload,
    handleDropPayloadConsumed,
  }
}
