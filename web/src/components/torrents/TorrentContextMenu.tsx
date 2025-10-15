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
import type { InstanceCapabilities, Torrent } from "@/types"
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
  availableCategories?: Record<string, unknown>
  onSetCategory?: (category: string, hashes: string[]) => void
  isPending?: boolean
  onExport?: (hashes: string[], torrents: Torrent[]) => Promise<void> | void
  isExporting?: boolean
  capabilities?: InstanceCapabilities
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
}: TorrentContextMenuProps) {
  const [incognitoMode] = useIncognitoMode()

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash" | "full path") => {
    try {
      await copyTextToClipboard(text)
      toast.success(`Torrent ${type} copied to clipboard`)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])

  const displayHash = useMemo(() => getTorrentDisplayHash(torrent), [torrent])

  const copyHash = useCallback(() => {
    const value = displayHash || torrent.hash
    if (!value) {
      toast.error("Hash not available")
      return
    }
    void copyToClipboard(value, "hash")
  }, [copyToClipboard, displayHash, torrent.hash])

  const copyFullPath = useCallback(() => {
    const name = incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name
    const savePath = incognitoMode ? getLinuxSavePath(torrent.hash) : torrent.save_path
    const fullPath = `${savePath}/${name}`
    void copyToClipboard(fullPath, "full path")
  }, [copyToClipboard, incognitoMode, torrent.hash, torrent.name, torrent.save_path])

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

  const handleExport = useCallback(() => {
    if (!onExport) {
      return
    }
    void onExport(hashes, torrents)
  }, [hashes, onExport, torrents])

  const count = isAllSelected ? effectiveSelectionCount : hashes.length

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
          View Details
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.RESUME, hashes)}
          disabled={isPending}
        >
          <Play className="mr-2 h-4 w-4" />
          Resume {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onAction(TORRENT_ACTIONS.PAUSE, hashes)}
          disabled={isPending}
        >
          <Pause className="mr-2 h-4 w-4" />
          Pause {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareRecheck(hashes, count)}
          disabled={isPending}
        >
          <CheckCircle className="mr-2 h-4 w-4" />
          Force Recheck {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareReannounce(hashes, count)}
          disabled={isPending}
        >
          <Radio className="mr-2 h-4 w-4" />
          Reannounce {count > 1 ? `(${count})` : ""}
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
          Add Tags {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareTags("set", hashes, torrents)}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          Replace Tags {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <CategorySubmenu
          type="context"
          hashCount={count}
          availableCategories={availableCategories}
          onSetCategory={handleSetCategory}
          isPending={isPending}
          currentCategory={torrent.category}
        />
        <ContextMenuItem
          onClick={() => onPrepareLocation(hashes, torrents)}
          disabled={isPending}
        >
          <FolderOpen className="mr-2 h-4 w-4" />
          Set Location {count > 1 ? `(${count})` : ""}
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
          Set Share Limits {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareSpeedLimits(hashes, torrents)}
          disabled={isPending}
        >
          <Gauge className="mr-2 h-4 w-4" />
          Set Speed Limits {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        {mixed ? (
          <>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: true })}
              disabled={isPending}
            >
              <Sparkles className="mr-2 h-4 w-4" />
              Enable TMM {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
            </ContextMenuItem>
            <ContextMenuItem
              onClick={() => onAction(TORRENT_ACTIONS.TOGGLE_AUTO_TMM, hashes, { enable: false })}
              disabled={isPending}
            >
              <Settings2 className="mr-2 h-4 w-4" />
              Disable TMM {count > 1 ? `(${count} Mixed)` : "(Mixed)"}
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
                Disable TMM {count > 1 ? `(${count})` : ""}
              </>
            ) : (
              <>
                <Sparkles className="mr-2 h-4 w-4" />
                Enable TMM {count > 1 ? `(${count})` : ""}
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
            {count > 1 ? `Export Torrents (${count})` : "Export Torrent"}
          </ContextMenuItem>
        )}
        <ContextMenuSub>
          <ContextMenuSubTrigger>
            <Copy className="mr-4 h-4 w-4" />
            Copy...
          </ContextMenuSubTrigger>
          <ContextMenuSubContent>
            <ContextMenuItem
              onClick={() => copyToClipboard(incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name, "name")}
            >
              Copy Name
            </ContextMenuItem>
            <ContextMenuItem onClick={copyHash}>
              Copy Hash
            </ContextMenuItem>
            <ContextMenuItem onClick={copyFullPath}>
              Copy Full Path
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
          Delete {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})
