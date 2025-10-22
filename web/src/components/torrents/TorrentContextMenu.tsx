/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
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
import type { TorrentAction } from "@/hooks/useTorrentActions"
import { TORRENT_ACTIONS } from "@/hooks/useTorrentActions"
import { getLinuxIsoName, getLinuxSavePath, useIncognitoMode } from "@/lib/incognito"
import { getTorrentDisplayHash } from "@/lib/torrent-utils"
import { copyTextToClipboard } from "@/lib/utils"
import type { Category, InstanceCapabilities, Torrent } from "@/types"
import {
  CheckCircle,
  Copy,
  Download,
  FolderOpen,
  Gauge,
  Pause,
  Play,
  Radio,
  Settings2,
  Sparkles,
  Sprout,
  Tag,
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
  torrent: Torrent
  isSelected: boolean
  isAllSelected?: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  effectiveSelectionCount: number
  onTorrentSelect?: (torrent: Torrent | null) => void
  onAction: (action: TorrentAction, hashes: string[], options?: { enable?: boolean }) => void
  onPrepareDelete: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareTags: (action: "add" | "set" | "remove", hashes: string[], torrents?: Torrent[]) => void
  onPrepareCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareCreateCategory: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareShareLimit: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareSpeedLimits: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRecheck: (hashes: string[], count?: number) => void
  onPrepareReannounce: (hashes: string[], count?: number) => void
  onPrepareLocation: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameTorrent: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameFile: (hashes: string[], torrents?: Torrent[]) => void
  onPrepareRenameFolder: (hashes: string[], torrents?: Torrent[]) => void
  availableCategories?: Record<string, Category>
  onSetCategory?: (category: string, hashes: string[]) => void
  isPending?: boolean
  onExport?: (hashes: string[], torrents: Torrent[]) => Promise<void> | void
  isExporting?: boolean
  capabilities?: InstanceCapabilities
  useSubcategories?: boolean
}

export const TorrentContextMenu = memo(function TorrentContextMenu({
  children,
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
  onPrepareShareLimit,
  onPrepareSpeedLimits,
  onPrepareRecheck,
  onPrepareReannounce,
  onPrepareLocation,
  onPrepareRenameTorrent,
  onPrepareRenameFile,
  onPrepareRenameFolder,
  availableCategories = {},
  onSetCategory,
  isPending = false,
  onExport,
  isExporting = false,
  capabilities,
  useSubcategories = false,
}: TorrentContextMenuProps) {
  const { t } = useTranslation()
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

  const count = isAllSelected ? effectiveSelectionCount : hashes.length

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash" | "full path", itemCount: number) => {
    try {
      await copyTextToClipboard(text)
      const pluralTypes: Record<"name" | "hash" | "full path", string> = {
        name: "names",
        hash: "hashes",
        "full path": "full paths",
      }
      const label = itemCount > 1 ? pluralTypes[type] : type
      toast.success(t("torrent_context_menu.toasts.copied_success_multiple", { label }))
    } catch {
      toast.error(t("torrent_context_menu.toasts.copy_failed"))
    }
  }, [])

  const handleCopyNames = useCallback(() => {
    const values = torrents
      .map(t => incognitoMode ? getLinuxIsoName(t.hash) : t.name)
      .map(value => (value ?? "").trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("torrent_context_menu.toasts.name_not_available"))
      return
    }

    void copyToClipboard(values.join("\n"), "name", values.length)
  }, [copyToClipboard, incognitoMode, torrents])

  const handleCopyHashes = useCallback(() => {
    const values = torrents
      .map(t => getTorrentDisplayHash(t) || t.hash || "")
      .map(value => value.trim())
      .filter(Boolean)

    if (values.length === 0) {
      toast.error(t("torrent_context_menu.toasts.hash_not_available"))
      return
    }
    void copyToClipboard(values.join("\n"), "hash", values.length)
  }, [copyToClipboard, torrents])

  const handleCopyFullPaths = useCallback(() => {
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
      toast.error(t("torrent_context_menu.toasts.full_path_not_available"))
      return
    }

    void copyToClipboard(values.join("\n"), "full path", values.length)
  }, [copyToClipboard, incognitoMode, torrents])

  const handleExport = useCallback(() => {
    if (!onExport) {
      return
    }
    void onExport(hashes, torrents)
  }, [hashes, onExport, torrents])

  // TMM state calculation
  const tmmStates = torrents.map(t => t.auto_tmm)
  const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
  const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
  const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    onAction(action as TorrentAction, hashes)
  }, [onAction, hashes])

  const handleSetCategory = useCallback((category: string) => {
    if (onSetCategory) {
      onSetCategory(category, hashes)
    }
  }, [onSetCategory, hashes])

  const supportsTorrentExport = capabilities?.supportsTorrentExport ?? true

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
        <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
          {t("torrent_context_menu.view_details")}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.RESUME, hashes)}
          disabled={isPending}
        >
          <Play className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.resume", { count })}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.PAUSE, hashes)}
          disabled={isPending}
        >
          <Pause className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.pause", { count })}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareRecheck(hashes, count)}
          disabled={isPending}
        >
          <CheckCircle className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.recheck", { count })}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareReannounce(hashes, count)}
          disabled={isPending}
        >
          <Radio className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.reannounce", { count })}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <QueueSubmenu
          type="context"
          hashCount={count}
          onQueueAction={handleQueueAction}
          isPending={isPending}
        />
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onPrepareTags("add", hashes, torrents)}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.add_tags", { count })}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareTags("set", hashes, torrents)}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.replace_tags", { count })}
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
          onClick={() => onPrepareLocation(hashes, torrents)}
          disabled={isPending}
        >
          <FolderOpen className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.set_location", { count })}
        </ContextMenuItem>
        <RenameSubmenu
          type="context"
          hashCount={count}
          onRenameTorrent={() => onPrepareRenameTorrent(hashes, torrents)}
          onRenameFile={() => onPrepareRenameFile(hashes, torrents)}
          onRenameFolder={() => onPrepareRenameFolder(hashes, torrents)}
          isPending={isPending}
          capabilities={capabilities}
        />
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onPrepareShareLimit(hashes, torrents)}
          disabled={isPending}
        >
          <Sprout className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.set_share_limits", { count })}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareSpeedLimits(hashes, torrents)}
          disabled={isPending}
        >
          <Gauge className="mr-2 h-4 w-4" />
          {t("torrent_context_menu.set_speed_limits", { count })}
        </ContextMenuItem>
        <ContextMenuSeparator />
        {mixed ? (
          <>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: true })}
              disabled={isPending}
            >
              <Sparkles className="mr-2 h-4 w-4" />
              {t("torrent_context_menu.enable_tmm_mixed", { count })}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: false })}
              disabled={isPending}
            >
              <Settings2 className="mr-2 h-4 w-4" />
              {t("torrent_context_menu.disable_tmm_mixed", { count })}
            </ContextMenuItem>
          </>
        ) : (
          <ContextMenuItem
            onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: !allEnabled })}
            disabled={isPending}
          >
            {allEnabled ? (
              <>
                <Settings2 className="mr-2 h-4 w-4" />
                {t("torrent_context_menu.disable_tmm", { count })}
              </>
            ) : (
              <>
                <Sparkles className="mr-2 h-4 w-4" />
                {t("torrent_context_menu.enable_tmm", { count })}
              </>
            )}
          </ContextMenuItem>
        )}
        <ContextMenuSeparator />
        {supportsTorrentExport && (
          <ContextMenuItem
            onClick={handleExport}
            disabled={isExporting}
          >
            <Download className="mr-2 h-4 w-4" />
            {count > 1 ? t("torrent_context_menu.export_count", { count }) : t("torrent_context_menu.export")}
          </ContextMenuItem>
        )}
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Copy className="mr-4 h-4 w-4" />
            {t("torrent_context_menu.copy_options.title")}
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            <ContextMenuItem onClick={handleCopyNames}>
              {t("torrent_context_menu.copy_options.name")}
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyHashes}>
              {t("torrent_context_menu.copy_options.hash")}
            </ContextMenuItem>
            <ContextMenuItem onClick={handleCopyFullPaths}>
              {t("torrent_context_menu.copy_options.full_path")}
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
          {count > 1 ? t("common.buttons.delete_count", { count }) : t("common.buttons.delete")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})
