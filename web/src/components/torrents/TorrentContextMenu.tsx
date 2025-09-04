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
  Pause,
  Play,
  Radio,
  Settings2,
  Sparkles,
  Tag,
  Trash2
} from "lucide-react"
import { QueueSubmenu } from "./QueueSubmenu"
import { ShareLimitSubmenu, SpeedLimitsSubmenu } from "./TorrentLimitSubmenus"
import { getLinuxIsoName } from "@/lib/incognito"
import { toast } from "sonner"
import type { Torrent } from "@/types"

interface TorrentContextMenuProps {
  children: React.ReactNode
  torrent: Torrent
  isSelected: boolean
  selectedHashes: string[]
  selectedTorrents: Torrent[]
  effectiveSelectionCount: number
  isAllSelected: boolean
  isPending: boolean
  incognitoMode: boolean
  onTorrentSelect?: (torrent: Torrent | null) => void
  onContextMenuAction: (action: "pause" | "resume" | "recheck" | "reannounce" | "increasePriority" | "decreasePriority" | "topPriority" | "bottomPriority" | "toggleAutoTMM", hashes: string[], enable?: boolean) => void
  onRecheckClick: (hashes: string[]) => void
  onReannounceClick: (hashes: string[]) => void
  onSetContextMenuData: (hashes: string[], torrents: Torrent[]) => void
  onSetShareLimit: (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number, hashes: string[]) => void
  onSetSpeedLimits: (uploadLimit: number, downloadLimit: number, hashes: string[]) => void
  onShowAddTagsDialog: () => void
  onShowTagsDialog: () => void
  onShowCategoryDialog: () => void
  onShowDeleteDialog: () => void
}

export const TorrentContextMenu = memo(function TorrentContextMenu({
  children,
  torrent,
  isSelected,
  selectedHashes,
  selectedTorrents,
  effectiveSelectionCount,
  isAllSelected,
  isPending,
  incognitoMode,
  onTorrentSelect,
  onContextMenuAction,
  onRecheckClick,
  onReannounceClick,
  onSetContextMenuData,
  onSetShareLimit,
  onSetSpeedLimits,
  onShowAddTagsDialog,
  onShowTagsDialog,
  onShowCategoryDialog,
  onShowDeleteDialog,
}: TorrentContextMenuProps) {
  const copyToClipboard = useCallback(async (text: string, type: "name" | "hash") => {
    try {
      await navigator.clipboard.writeText(text)
      const message = type === "name" ? "Torrent name copied!" : "Torrent hash copied!"
      toast.success(message)
    } catch {
      toast.error("Failed to copy to clipboard")
    }
  }, [])

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onClick={() => onTorrentSelect?.(torrent)}>
          View Details
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            onContextMenuAction("resume", hashes)
          }}
          disabled={isPending}
        >
          <Play className="mr-2 h-4 w-4" />
          Resume {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            onContextMenuAction("pause", hashes)
          }}
          disabled={isPending}
        >
          <Pause className="mr-2 h-4 w-4" />
          Pause {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            onRecheckClick(hashes)
          }}
          disabled={isPending}
        >
          <CheckCircle className="mr-2 h-4 w-4" />
          Force Recheck {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            onReannounceClick(hashes)
          }}
          disabled={isPending}
        >
          <Radio className="mr-2 h-4 w-4" />
          Reannounce {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        {(() => {
          // Use selected torrents if this row is part of selection, or just this torrent
          const useSelection = isSelected || isAllSelected
          const hashes = useSelection ? selectedHashes : [torrent.hash]
          const hashCount = isAllSelected ? effectiveSelectionCount : hashes.length

          const handleQueueAction = (action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
            onContextMenuAction(action, hashes)
          }

          return (
            <QueueSubmenu
              type="context"
              hashCount={hashCount}
              onQueueAction={handleQueueAction}
              isPending={isPending}
            />
          )
        })()}
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            const torrents = useSelection ? selectedTorrents : [torrent]

            onSetContextMenuData(hashes, torrents)
            onShowAddTagsDialog()
          }}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          Add Tags {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            const torrents = useSelection ? selectedTorrents : [torrent]

            onSetContextMenuData(hashes, torrents)
            onShowTagsDialog()
          }}
          disabled={isPending}
        >
          <Tag className="mr-2 h-4 w-4" />
          Replace Tags {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]
            const torrents = useSelection ? selectedTorrents : [torrent]

            onSetContextMenuData(hashes, torrents)
            onShowCategoryDialog()
          }}
          disabled={isPending}
        >
          <Folder className="mr-2 h-4 w-4" />
          Set Category {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
        <ContextMenuSeparator />
        {(() => {
          // Use selected torrents if this row is part of selection, or just this torrent
          const useSelection = isSelected || isAllSelected
          const hashes = useSelection ? selectedHashes : [torrent.hash]
          const hashCount = isAllSelected ? effectiveSelectionCount : hashes.length

          // Create wrapped handlers that pass hashes directly
          const handleSetShareLimitWrapper = (ratioLimit: number, seedingTimeLimit: number, inactiveSeedingTimeLimit: number) => {
            onSetShareLimit(ratioLimit, seedingTimeLimit, inactiveSeedingTimeLimit, hashes)
          }

          const handleSetSpeedLimitsWrapper = (uploadLimit: number, downloadLimit: number) => {
            onSetSpeedLimits(uploadLimit, downloadLimit, hashes)
          }

          return (
            <>
              <ShareLimitSubmenu
                type="context"
                hashCount={hashCount}
                onConfirm={handleSetShareLimitWrapper}
                isPending={isPending}
              />
              <SpeedLimitsSubmenu
                type="context"
                hashCount={hashCount}
                onConfirm={handleSetSpeedLimitsWrapper}
                isPending={isPending}
              />
            </>
          )
        })()}
        <ContextMenuSeparator />
        {(() => {
          // Use selected torrents if this row is part of selection, or just this torrent
          const useSelection = isSelected || isAllSelected
          const hashes = useSelection ? selectedHashes : [torrent.hash]
          const torrents = useSelection ? selectedTorrents : [torrent]
          const count = isAllSelected ? effectiveSelectionCount : hashes.length

          const tmmStates = torrents.map(t => t.auto_tmm)
          const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
          const allDisabled = tmmStates.length > 0 && tmmStates.every(state => state === false)
          const mixed = tmmStates.length > 0 && !allEnabled && !allDisabled

          if (mixed) {
            return (
              <>
                <ContextMenuItem
                  onClick={() => onContextMenuAction("toggleAutoTMM", hashes, true)}
                  disabled={isPending}
                >
                  <Sparkles className="mr-2 h-4 w-4" />
                  Enable TMM {useSelection && count > 1 ? `(${count} Mixed)` : "(Mixed)"}
                </ContextMenuItem>
                <ContextMenuItem
                  onClick={() => onContextMenuAction("toggleAutoTMM", hashes, false)}
                  disabled={isPending}
                >
                  <Settings2 className="mr-2 h-4 w-4" />
                  Disable TMM {useSelection && count > 1 ? `(${count} Mixed)` : "(Mixed)"}
                </ContextMenuItem>
              </>
            )
          }

          return (
            <ContextMenuItem
              onClick={() => onContextMenuAction("toggleAutoTMM", hashes, !allEnabled)}
              disabled={isPending}
            >
              {allEnabled ? (
                <>
                  <Settings2 className="mr-2 h-4 w-4" />
                  Disable TMM {useSelection && count > 1 ? `(${count})` : ""}
                </>
              ) : (
                <>
                  <Sparkles className="mr-2 h-4 w-4" />
                  Enable TMM {useSelection && count > 1 ? `(${count})` : ""}
                </>
              )}
            </ContextMenuItem>
          )
        })()}
        <ContextMenuSeparator />
        <ContextMenuItem onClick={() => copyToClipboard(incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name, "name")}>
          <Copy className="mr-2 h-4 w-4" />
          Copy Name
        </ContextMenuItem>
        <ContextMenuItem onClick={() => copyToClipboard(torrent.hash, "hash")}>
          <Copy className="mr-2 h-4 w-4" />
          Copy Hash
        </ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem
          onClick={() => {
            // Use selected torrents if this row is part of selection, or just this torrent
            const useSelection = isSelected || isAllSelected
            const hashes = useSelection ? selectedHashes : [torrent.hash]

            onSetContextMenuData(hashes, [])
            onShowDeleteDialog()
          }}
          disabled={isPending}
          className="text-destructive"
        >
          <Trash2 className="mr-2 h-4 w-4" />
          Delete {(isSelected || isAllSelected) && effectiveSelectionCount > 1 ? `(${effectiveSelectionCount})` : ""}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})