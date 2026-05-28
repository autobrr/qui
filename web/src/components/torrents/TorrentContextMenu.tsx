/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import { useCrossSeedFilter } from "@/hooks/useCrossSeedFilter"
import type { TorrentAction } from "@/hooks/useTorrentActions"
import { TORRENT_ACTIONS } from "@/hooks/useTorrentActions"
import { api } from "@/lib/api"
import { getLinuxIsoName, getLinuxSavePath, useIncognitoMode } from "@/lib/incognito"
import { buildTorrentActionTargets } from "@/lib/torrent-action-targets"
import { getTorrentDisplayHash } from "@/lib/torrent-utils"
import { copyTextToClipboard } from "@/lib/utils"
import type { Category, ExternalProgram, InstanceCapabilities, Torrent, TorrentFilters } from "@/types"
import { useMutation, useQueries, useQuery } from "@tanstack/react-query"
import {
  Blocks,
  CheckCircle,
  Copy,
  Download,
  FastForward,
  FolderOpen,
  Gauge,
  GitBranch,
  MessageSquare,
  Pause,
  Play,
  Radio,
  Search,
  Settings2,
  Sparkles,
  Sprout,
  Tag,
  Terminal,
  Trash2
} from "lucide-react"
import { memo, useCallback, useMemo } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { CategorySubmenu } from "./CategorySubmenu"
import { QueueSubmenu } from "./QueueSubmenu"
import { RenameSubmenu } from "./RenameSubmenu"

interface TorrentContextMenuProps {
  children: React.ReactNode
  instanceId: number
  readOnly?: boolean
  torrent: Torrent
  isSelected: boolean
  isAllSelected?: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  effectiveSelectionCount: number
  onTorrentSelect?: (torrent: Torrent | null, initialTab?: string) => void
  onAction: (action: TorrentAction, hashes: string[], options?: { enable?: boolean; targets?: Array<{ instanceId: number; hash: string }> }) => void
  onPrepareDelete: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareTags: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareComment?: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareCreateCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareShareLimit: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareSpeedLimits: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRecheck: (hashes: string[], count?: number) => void
  onPrepareReannounce: (hashes: string[], count?: number) => void
  onPrepareLocation: (hashes: string[], torrents?: Torrent[], count?: number) => void
  onPrepareTmm?: (hashes: string[], count: number, enable: boolean) => void
  onPrepareRenameTorrent: (hashes: string[], torrents?: Torrent[]) => void
  availableCategories?: Record<string, Category>
  onSetCategory?: (category: string, hashes: string[], targets?: Array<{ instanceId: number; hash: string }>) => void
  isPending?: boolean
  onExport?: (hashes: string[], torrents: Torrent[]) => Promise<void> | void
  isExporting?: boolean
  capabilities?: InstanceCapabilities
  useSubcategories?: boolean
  canCrossSeedSearch?: boolean
  onCrossSeedSearch?: (torrent: Torrent) => void
  isCrossSeedSearching?: boolean
  onFilterChange?: (filters: TorrentFilters) => void
  onFetchAllField?: (field: "name" | "hash" | "full_path" | "magnet_uri") => Promise<string[]>
}

export const TorrentContextMenu = memo(function TorrentContextMenu({
  children,
  instanceId: _instanceId,
  readOnly = false,
  torrent,
  isSelected,
  isAllSelected = false,
  selectedHashes,
  selectedTorrents,
  effectiveSelectionCount,
  onTorrentSelect,
  onAction,
  onPrepareDelete,
  onPrepareTags,
  onPrepareComment,
  onPrepareShareLimit,
  onPrepareSpeedLimits,
  onPrepareRecheck,
  onPrepareReannounce,
  onPrepareLocation,
  onPrepareRenameTorrent,
  onPrepareTmm,
  availableCategories = {},
  onSetCategory,
  isPending = false,
  onExport,
  isExporting = false,
  capabilities,
  useSubcategories = false,
  canCrossSeedSearch = false,
  onCrossSeedSearch,
  isCrossSeedSearching = false,
  onFilterChange,
  onFetchAllField,
}: TorrentContextMenuProps) {
  const { t } = useTranslation("torrents")
  const [incognitoMode] = useIncognitoMode()

  // Determine if we should use selection or just this torrent
  const useSelection = isSelected || isAllSelected

  // Memoize hashes and torrents to avoid re-creating arrays on every render
  const hashes = useMemo(() =>
    useSelection ? selectedHashes : [torrent.hash],
  [useSelection, selectedHashes, torrent.hash]
  )

  const torrents = useMemo(() =>
    useSelection ? selectedTorrents : [torrent],
  [useSelection, selectedTorrents, torrent]
  )
  const actionTargets = useMemo(() => buildTorrentActionTargets(torrents, _instanceId), [torrents, _instanceId])

  const targetInstanceIds = useMemo(() => {
    const ids = new Set<number>()
    for (const target of actionTargets) {
      if (target.instanceId > 0) {
        ids.add(target.instanceId)
      }
    }
    return Array.from(ids).sort((a, b) => a - b)
  }, [actionTargets])

  const shouldResolveSetCommentSupport = capabilities === undefined && _instanceId <= 0 && targetInstanceIds.length > 0
  const setCommentCapabilityQueries = useQueries({
    queries: targetInstanceIds.map(id => ({
      queryKey: ["instance-capabilities", id],
      queryFn: () => api.getInstanceCapabilities(id),
      staleTime: 60_000,
      enabled: shouldResolveSetCommentSupport,
    })),
  })

  const count = isAllSelected ? effectiveSelectionCount : hashes.length

  // State for cross-seed search
  const { isFilteringCrossSeeds, filterCrossSeeds } = useCrossSeedFilter({
    instanceId: _instanceId,
    onFilterChange,
  })

  const handleFilterCrossSeeds = useCallback(() => {
    filterCrossSeeds(torrents)
  }, [filterCrossSeeds, torrents])

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash" | "full path" | "magnet link", itemCount: number) => {
    try {
      await copyTextToClipboard(text)
      const pluralTypes: Record<"name" | "hash" | "full path" | "magnet link", string> = {
        name: "names",
        hash: "hashes",
        "full path": "full paths",
        "magnet link": "magnet links",
      }
      const label = itemCount > 1 ? pluralTypes[type] : type
      toast.success(t("contextMenu.toast.torrentCopied", { label }))
    } catch {
      toast.error(t("contextMenu.toast.failedToCopy"))
    }
  }, [t])

  const handleCopyNames = useCallback(async () => {
    // Select all fetch from backend
    if (isAllSelected && onFetchAllField && torrents.length < effectiveSelectionCount) {
      try {
        if (incognitoMode) {
          // In incognito mode, fetch hashes and transform client-side
          const hashes = await onFetchAllField("hash")
          const values = hashes.map(h => getLinuxIsoName(h)).filter(Boolean)
          if (values.length === 0) { toast.error(t("contextMenu.toast.nameNotAvailable")); return }
          void copyToClipboard(values.join("\n"), "name", values.length)
        } else {
          const values = await onFetchAllField("name")
          if (values.length === 0) { toast.error(t("contextMenu.toast.nameNotAvailable")); return }
          void copyToClipboard(values.join("\n"), "name", values.length)
        }
      } catch (error) {
        console.error("Failed to fetch torrent names:", error)
        toast.error(t("contextMenu.toast.failedToFetchNames"))
      }
      return
    }

    const values = torrents
      .map(t => incognitoMode ? getLinuxIsoName(t.hash) : t.name)
      .map(value => (value ?? "").trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("contextMenu.toast.nameNotAvailable"))
      return
    }

    void copyToClipboard(values.join("\n"), "name", values.length)
  }, [copyToClipboard, incognitoMode, torrents, isAllSelected, effectiveSelectionCount, onFetchAllField, t])

  const handleCopyHashes = useCallback(async () => {
    if (isAllSelected && onFetchAllField && torrents.length < effectiveSelectionCount) {
      try {
        const values = await onFetchAllField("hash")
        if (values.length === 0) { toast.error(t("contextMenu.toast.hashNotAvailable")); return }
        void copyToClipboard(values.join("\n"), "hash", values.length)
      } catch (error) {
        console.error("Failed to fetch torrent hashes:", error)
        toast.error(t("contextMenu.toast.failedToFetchHashes"))
      }
      return
    }

    const values = torrents
      .map(t => getTorrentDisplayHash(t) || t.hash || "")
      .map(value => value.trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("contextMenu.toast.hashNotAvailable"))
      return
    }
    void copyToClipboard(values.join("\n"), "hash", values.length)
  }, [copyToClipboard, torrents, isAllSelected, effectiveSelectionCount, onFetchAllField, t])

  const handleCopyFullPaths = useCallback(async () => {
    if (isAllSelected && onFetchAllField && torrents.length < effectiveSelectionCount) {
      try {
        if (incognitoMode) {
          // In incognito mode, fetch hashes and construct fake paths
          const hashes = await onFetchAllField("hash")
          const values = hashes
            .map(h => `${getLinuxSavePath(h)}/${getLinuxIsoName(h)}`)
            .filter(Boolean)
          if (values.length === 0) { toast.error(t("contextMenu.toast.fullPathNotAvailable")); return }
          void copyToClipboard(values.join("\n"), "full path", values.length)
        } else {
          const values = await onFetchAllField("full_path")
          if (values.length === 0) { toast.error(t("contextMenu.toast.fullPathNotAvailable")); return }
          void copyToClipboard(values.join("\n"), "full path", values.length)
        }
      } catch (error) {
        console.error("Failed to fetch torrent paths:", error)
        toast.error(t("contextMenu.toast.failedToFetchPaths"))
      }
      return
    }

    const values = torrents
      .map(t => {
        const name = incognitoMode ? getLinuxIsoName(t.hash) : t.name
        const savePath = incognitoMode ? getLinuxSavePath(t.hash) : t.save_path
        if (!name || !savePath) {
          return ""
        }
        return `${savePath}/${name}`
      })
      .map(value => value.trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("contextMenu.toast.fullPathNotAvailable"))
      return
    }

    void copyToClipboard(values.join("\n"), "full path", values.length)
  }, [copyToClipboard, incognitoMode, torrents, isAllSelected, effectiveSelectionCount, onFetchAllField, t])

  const handleCopyMagnetLinks = useCallback(async () => {
    if (isAllSelected && onFetchAllField && torrents.length < effectiveSelectionCount) {
      try {
        const values = await onFetchAllField("magnet_uri")
        if (values.length === 0) { toast.error(t("contextMenu.toast.magnetNotAvailable")); return }
        void copyToClipboard(values.join("\n"), "magnet link", values.length)
      } catch (error) {
        console.error("Failed to fetch torrent magnet links:", error)
        toast.error(t("contextMenu.toast.failedToFetchMagnets"))
      }
      return
    }

    const values = torrents
      .map(t => (t.magnet_uri ?? "").trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("contextMenu.toast.magnetNotAvailable"))
      return
    }

    void copyToClipboard(values.join("\n"), "magnet link", values.length)
  }, [copyToClipboard, torrents, isAllSelected, effectiveSelectionCount, onFetchAllField, t])

  const handleExport = useCallback(() => {
    if (!onExport) {
      return
    }
    void onExport(hashes, torrents)
  }, [hashes, onExport, torrents])

  const forceStartStates = torrents.map(t => t.force_start)
  const allForceStarted = forceStartStates.length > 0 && forceStartStates.every(state => state === true)
  const allForceDisabled = forceStartStates.length > 0 && forceStartStates.every(state => state === false)
  const forceStartMixed = forceStartStates.length > 0 && !allForceStarted && !allForceDisabled

  // TMM state calculation
  const tmmStates = torrents.map(t => t.auto_tmm)
  const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
  const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
  const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

  // Sequential download state calculation
  const seqDlStates = torrents.map(t => t.seq_dl)
  const allSeqDlEnabled = seqDlStates.length > 0 && seqDlStates.every(state => state === true)
  const allSeqDlDisabled = seqDlStates.length > 0 && seqDlStates.every(state => state === false)
  const seqDlMixed = seqDlStates.length > 0 && !allSeqDlEnabled && !allSeqDlDisabled

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    onAction(action as TorrentAction, hashes, { targets: actionTargets })
  }, [onAction, hashes, actionTargets])

  const handleForceStartToggle = useCallback((enable: boolean) => {
    onAction(TORRENT_ACTIONS.FORCE_START, hashes, { enable, targets: actionTargets })
  }, [onAction, hashes, actionTargets])

  const handleSeqDlToggle = useCallback((enable: boolean) => {
    onAction(TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, hashes, { enable, targets: actionTargets })
  }, [onAction, hashes, actionTargets])

  const handleSetCategory = useCallback((category: string) => {
    if (onSetCategory) {
      onSetCategory(category, hashes, actionTargets)
    }
  }, [onSetCategory, hashes, actionTargets])

  const handleTmmToggle = useCallback((enable: boolean) => {
    if (onPrepareTmm) {
      onPrepareTmm(hashes, count, enable)
    } else {
      onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable, targets: actionTargets })
    }
  }, [onPrepareTmm, onAction, hashes, count, actionTargets])

  const handleLocationClick = useCallback(() => {
    onPrepareLocation(hashes, torrents, count)
  }, [onPrepareLocation, hashes, torrents, count])

  const supportsTorrentExport = capabilities?.supportsTorrentExport ?? true
  const supportsSetComment = capabilities?.supportsSetComment ?? (
    shouldResolveSetCommentSupport? setCommentCapabilityQueries.some(query => query.data?.supportsSetComment === true): false
  )
  const supportsInstanceScopedActions = _instanceId > 0

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent
        alignOffset={8}
        collisionPadding={10}
        className="ml-2"
      >
        {readOnly ? (
          <>
            <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
              {t("contextMenu.viewDetails")}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem onClick={handleCopyNames}>
              {t("contextMenu.copyName")}
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyHashes}>
              {t("contextMenu.copyHash")}
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyFullPaths}>
              {t("contextMenu.copyFullPath")}
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyMagnetLinks}>
              {t("contextMenu.copyMagnetLink")}
            </ContextMenuItem>
          </>
        ) : (
          <>
            <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
              {t("contextMenu.viewDetails")}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.RESUME, hashes, { targets: actionTargets })}
              disabled={isPending}
            >
              <Play className="mr-2 h-4 w-4" />
              {t("contextMenu.resume")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            {forceStartMixed ? (
              <>
                <ContextMenuItem
                  onClick={() => handleForceStartToggle(true)}
                  disabled={isPending}
                >
                  <FastForward className="mr-2 h-4 w-4" />
                  {t("contextMenu.forceStart")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
                <ContextMenuItem
                  onClick={() => handleForceStartToggle(false)}
                  disabled={isPending}
                >
                  <FastForward className="mr-2 h-4 w-4" />
                  {t("contextMenu.disableForceStart")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
              </>
            ) : (
              <ContextMenuItem
                onClick={() => handleForceStartToggle(!allForceStarted)}
                disabled={isPending}
              >
                <FastForward className="mr-2 h-4 w-4" />
                {allForceStarted ? `${t("contextMenu.disableForceStart")} ${count > 1 ? `(${count})` : ""}` : `${t("contextMenu.forceStart")} ${count > 1 ? `(${count})` : ""}`}
              </ContextMenuItem>
            )}
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.PAUSE, hashes, { targets: actionTargets })}
              disabled={isPending}
            >
              <Pause className="mr-2 h-4 w-4" />
              {t("contextMenu.pause")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onPrepareRecheck(hashes, count)}
              disabled={isPending}
            >
              <CheckCircle className="mr-2 h-4 w-4" />
              {t("contextMenu.forceRecheck")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onPrepareReannounce(hashes, count)}
              disabled={isPending}
            >
              <Radio className="mr-2 h-4 w-4" />
              {t("contextMenu.reannounce")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            {seqDlMixed ? (
              <>
                <ContextMenuItem
                  onClick={() => handleSeqDlToggle(true)}
                  disabled={isPending}
                >
                  <Blocks className="mr-2 h-4 w-4" />
                  {t("contextMenu.enableSequentialDownload")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
                <ContextMenuItem
                  onClick={() => handleSeqDlToggle(false)}
                  disabled={isPending}
                >
                  <Blocks className="mr-2 h-4 w-4" />
                  {t("contextMenu.disableSequentialDownload")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
              </>
            ) : (
              <ContextMenuItem
                onClick={() => handleSeqDlToggle(!allSeqDlEnabled)}
                disabled={isPending}
              >
                <Blocks className="mr-2 h-4 w-4" />
                {allSeqDlEnabled ? `${t("contextMenu.disableSequentialDownload")} ${count > 1 ? `(${count})` : ""}` : `${t("contextMenu.enableSequentialDownload")} ${count > 1 ? `(${count})` : ""}`}
              </ContextMenuItem>
            )}
            <ContextMenuSeparator />
            <QueueSubmenu
              type="context"
              hashCount={count}
              onQueueAction={handleQueueAction}
              isPending={isPending}
            />
            <ContextMenuSeparator />
            {canCrossSeedSearch && (
              <ContextMenuItem
                onClick={() => onCrossSeedSearch?.(torrent)}
                disabled={isPending || isCrossSeedSearching}
              >
                <Search className="mr-2 h-4 w-4" />
                {t("contextMenu.searchCrossSeeds")}
              </ContextMenuItem>
            )}
            {onFilterChange && supportsInstanceScopedActions && (
              <ContextMenuItem
                onClick={handleFilterCrossSeeds}
                disabled={isPending || isFilteringCrossSeeds || count > 1}
                title={count > 1 ? t("crossseed:hooks.filter.singleSelectionOnly") : undefined}
              >
                <GitBranch className="mr-2 h-4 w-4" />
                {count > 1 ? (
                  <span className="text-muted-foreground">{t("contextMenu.filterCrossSeedsSingleOnly")}</span>
                ) : (
                  <>{t("contextMenu.filterCrossSeeds")}</>
                )}
                {isFilteringCrossSeeds && <span className="ml-1 text-xs text-muted-foreground">...</span>}
              </ContextMenuItem>
            )}
            {(canCrossSeedSearch || (onFilterChange && supportsInstanceScopedActions)) && <ContextMenuSeparator />}
            <ContextMenuItem
              onClick={() => onPrepareTags(hashes, torrents)}
              disabled={isPending}
            >
              <Tag className="mr-2 h-4 w-4" />
              {t("contextMenu.setTags")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            <CategorySubmenu
              type="context"
              hashCount={count}
              availableCategories={availableCategories}
              onSetCategory={handleSetCategory}
              isPending={isPending}
              currentCategory={torrent.category}
              useSubcategories={useSubcategories}
            />
            <ContextMenuItem
              onClick={handleLocationClick}
              disabled={isPending}
            >
              <FolderOpen className="mr-2 h-4 w-4" />
              {t("contextMenu.setLocation")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            {supportsInstanceScopedActions && (
              <RenameSubmenu
                type="context"
                hashCount={count}
                onRenameTorrent={() => onPrepareRenameTorrent(hashes, torrents)}
                onRenameFile={() => onTorrentSelect?.(torrent, "content")}
                onRenameFolder={() => onTorrentSelect?.(torrent, "content")}
                isPending={isPending}
                capabilities={capabilities}
              />
            )}
            {supportsSetComment && onPrepareComment && (
              <ContextMenuItem
                onClick={() => onPrepareComment(hashes, torrents)}
                disabled={isPending}
              >
                <MessageSquare className="mr-2 h-4 w-4" />
                {t("contextMenu.setComment")} {count > 1 ? `(${count})` : ""}
              </ContextMenuItem>
            )}
            <ContextMenuSeparator />
            <ContextMenuItem
              onClick={() => onPrepareShareLimit(hashes, torrents)}
              disabled={isPending}
            >
              <Sprout className="mr-2 h-4 w-4" />
              {t("contextMenu.setShareLimits")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onPrepareSpeedLimits(hashes, torrents)}
              disabled={isPending}
            >
              <Gauge className="mr-2 h-4 w-4" />
              {t("contextMenu.setSpeedLimits")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
            <ContextMenuSeparator />
            {mixed ? (
              <>
                <ContextMenuItem
                  onClick={() => handleTmmToggle(true)}
                  disabled={isPending}
                >
                  <Sparkles className="mr-2 h-4 w-4" />
                  {t("contextMenu.enableTmm")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
                <ContextMenuItem
                  onClick={() => handleTmmToggle(false)}
                  disabled={isPending}
                >
                  <Settings2 className="mr-2 h-4 w-4" />
                  {t("contextMenu.disableTmm")} {count > 1 ? `(${count} ${t("contextMenu.mixed")})` : t("contextMenu.mixed")}
                </ContextMenuItem>
              </>
            ) : (
              <ContextMenuItem
                onClick={() => handleTmmToggle(!allEnabled)}
                disabled={isPending}
              >
                {allEnabled ? (
                  <>
                    <Settings2 className="mr-2 h-4 w-4" />
                    {t("contextMenu.disableTmm")} {count > 1 ? `(${count})` : ""}
                  </>
                ) : (
                  <>
                    <Sparkles className="mr-2 h-4 w-4" />
                    {t("contextMenu.enableTmm")} {count > 1 ? `(${count})` : ""}
                  </>
                )}
              </ContextMenuItem>
            )}
            <ContextMenuSeparator />
            {supportsInstanceScopedActions && <ExternalProgramsSubmenu instanceId={_instanceId} hashes={hashes} />}
            {supportsTorrentExport && (
              <ContextMenuItem
                onClick={handleExport}
                disabled={isExporting}
              >
                <Download className="mr-2 h-4 w-4" />
                {count > 1 ? t("contextMenu.exportTorrents", { count }) : t("contextMenu.exportTorrent")}
              </ContextMenuItem>
            )}
            <ContextMenuSub>
              <ContextMenuSubTrigger>
                <Copy className="mr-4 h-4 w-4" />
                {t("contextMenu.copy")}
              </ContextMenuSubTrigger>
              <ContextMenuSubContent>
                <ContextMenuItem onClick={handleCopyNames}>
                  {t("contextMenu.copyName")}
                </ContextMenuItem>
                <ContextMenuItem onClick={handleCopyHashes}>
                  {t("contextMenu.copyHash")}
                </ContextMenuItem>
                <ContextMenuItem onClick={handleCopyFullPaths}>
                  {t("contextMenu.copyFullPath")}
                </ContextMenuItem>
                <ContextMenuItem onClick={handleCopyMagnetLinks}>
                  {t("contextMenu.copyMagnetLink")}
                </ContextMenuItem>
              </ContextMenuSubContent>
            </ContextMenuSub>
            <ContextMenuSeparator />
            <ContextMenuItem
              onClick={() => onPrepareDelete(hashes, torrents)}
              disabled={isPending}
              className="text-destructive"
            >
              <Trash2 className="mr-2 h-4 w-4" />
              {t("contextMenu.delete")} {count > 1 ? `(${count})` : ""}
            </ContextMenuItem>
          </>
        )}
      </ContextMenuContent>
    </ContextMenu>
  )
})

interface ExternalProgramsSubmenuProps {
  instanceId: number
  hashes: string[]
}

function ExternalProgramsSubmenu({ instanceId, hashes }: ExternalProgramsSubmenuProps) {
  const { t } = useTranslation("torrents")
  const { data: programs, isLoading } = useQuery({
    queryKey: ["externalPrograms", "enabled"],
    queryFn: () => api.listExternalPrograms(),
    select: (data) => data.filter(p => p.enabled),
    staleTime: 60 * 1000, // 1 minute
  })

  // Types derived from API for strong typing
  type ExecResp = Awaited<ReturnType<typeof api.executeExternalProgram>>
  type ExecVars = { program: ExternalProgram; instanceId: number; hashes: string[] }

  const executeMutation = useMutation<ExecResp, Error, ExecVars>({
    mutationFn: async ({ program, instanceId, hashes }) =>
      api.executeExternalProgram({
        program_id: program.id,
        instance_id: instanceId,
        hashes,
      }),
    onSuccess: (response) => {
      const successCount = response.results.filter(r => r.success).length
      const failureCount = response.results.length - successCount

      if (failureCount === 0) {
        toast.success(t("contextMenu.toast.externalProgramSuccess", { count: successCount }))
      } else if (successCount === 0) {
        toast.error(t("contextMenu.toast.externalProgramAllFailed", { count: failureCount }))
      } else {
        toast.warning(t("contextMenu.toast.externalProgramPartial", { success: successCount, failure: failureCount }))
      }

      // Log detailed errors in development only to avoid leaking PII/paths in production
      if (import.meta.env.DEV) {
        response.results.forEach(r => {
          if (!r.success && r.error) console.error(`External program failed for ${r.hash}:`, r.error)
        })
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : String(error)
      toast.error(t("contextMenu.toast.externalProgramError", { message }))
    },
  })

  const handleExecute = useCallback((program: ExternalProgram) => {
    executeMutation.mutate({ program, instanceId, hashes })
  }, [executeMutation, instanceId, hashes])

  if (isLoading) {
    return (
      <ContextMenuItem disabled>
        {t("contextMenu.loadingPrograms")}
      </ContextMenuItem>
    )
  }

  // programs is already filtered to enabled by select
  if (!programs || programs.length === 0) {
    return null // Don't show the submenu if no programs are enabled
  }

  return (
    <ContextMenuSub>
      <ContextMenuSubTrigger>
        <Terminal className="mr-4 h-4 w-4" />
        {t("contextMenu.externalPrograms")}
      </ContextMenuSubTrigger>
      <ContextMenuSubContent>
        {programs.map(program => (
          <ContextMenuItem
            key={program.id}
            onClick={() => handleExecute(program)}
            disabled={executeMutation.isPending}
          >
            {program.name}
          </ContextMenuItem>
        ))}
      </ContextMenuSubContent>
    </ContextMenuSub>
  )
}
