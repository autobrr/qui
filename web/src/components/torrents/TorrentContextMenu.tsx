/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { memo, useCallback } from "react"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger
} from "@/components/ui/context-menu"
import {
  CheckCircle,
  Copy,
  Folder,
  FolderOpen,
  Pause,
  Play,
  Radio,
  Settings2,
  Sparkles,
  Tag,
  Trash2
} from "lucide-react"
import { toast } from "sonner"
import type { Torrent } from "@/types"
import type { TorrentAction } from "@/hooks/useTorrentActions"
import { TORRENT_ACTIONS } from "@/hooks/useTorrentActions"
import { QueueSubmenu } from "./QueueSubmenu"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"
import { useMemo } from "react"

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
  onPrepareRecheck: (hashes: string[], count?: number) => void
  onPrepareReannounce: (hashes: string[], count?: number) => void
  onPrepareLocation: (hashes: string[], torrents?: Torrent[]) => void
  onSetShareLimit: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number, hashes: string[]) => void
  onSetSpeedLimits: (uploadLimit: number, downloadLimit: number, hashes: string[]) => void
  isPending?: boolean
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
  onPrepareCategory,
  onPrepareRecheck,
  onPrepareReannounce,
  onPrepareLocation,
  onSetShareLimit,
  onSetSpeedLimits,
  isPending = false,
}: TorrentContextMenuProps) {
  const [incognitoMode] = useIncognitoMode()

  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash") => {
    try {
      await navigator.clipboard.writeText(text)
      const message = type === "name" ? "Torrent name copied!" : "Torrent hash copied!"
      toast.success(message)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])

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

  // TMM state calculation
  const tmmStates = torrents.map(t => t.auto_tmm)
  const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
  const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
  const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    onAction(action as TorrentAction, hashes)
  }, [onAction, hashes])

  const handleSetShareLimitWrapper = useCallback((ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
    onSetShareLimit(ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, hashes)
  }, [onSetShareLimit, hashes])

  const handleSetSpeedLimitsWrapper = useCallback((uploadLimit: number, downloadLimit: number) => {
    onSetSpeedLimits(uploadLimit, downloadLimit, hashes)
  }, [onSetSpeedLimits, hashes])

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
        <ContextMenuItem
          onClick={() => onPrepareCategory(hashes, torrents)}
          disabled={isPending}
        >
          <Folder className="mr-2 h-4 w-4" />
          Set Category {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => onPrepareLocation(hashes, torrents)}
          disabled={isPending}
        >
          <FolderOpen className="mr-2 h-4 w-4" />
          Set Location {count > 1 ? `(${count})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ShareLimitSubmenu
          type="context"
          hashCount={count}
          onConfirm={handleSetShareLimitWrapper}
          isPending={isPending}
        />
        <SpeedLimitsSubmenu
          type="context"
          hashCount={count}
          onConfirm={handleSetSpeedLimitsWrapper}
          isPending={isPending}
        />
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
        <ContextMenuItem
          onClick={() => copyToClipboard(incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name, "name")}
        >
          <Copy className="mr-2 h-4 w-4" />
          Copy Name
        </ContextMenuItem>
        <ContextMenuItem onClick={() => copyToClipboard(torrent.hash, "hash")}>
          <Copy className="mr-2 h-4 w-4" />
          Copy Hash
        </ContextMenuItem>
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