/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { CrossSeedWarning } from "./CrossSeedWarning"
import { DeleteFilesPreference } from "./DeleteFilesPreference"
import type { CrossSeedWarningResult } from "@/hooks/useCrossSeedWarning"

interface DeleteTorrentDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  totalSize: number
  formattedSize: string
  deleteFiles: boolean
  onDeleteFilesChange: (checked: boolean) => void
  isDeleteFilesLocked: boolean
  onToggleDeleteFilesLock: () => void
  deleteCrossSeeds: boolean
  onDeleteCrossSeedsChange: (checked: boolean) => void
  crossSeedWarning: CrossSeedWarningResult
  onConfirm: () => void
}

export function DeleteTorrentDialog({
  open,
  onOpenChange,
  count,
  totalSize,
  formattedSize,
  deleteFiles,
  onDeleteFilesChange,
  isDeleteFilesLocked,
  onToggleDeleteFilesLock,
  deleteCrossSeeds,
  onDeleteCrossSeedsChange,
  crossSeedWarning,
  onConfirm,
}: DeleteTorrentDialogProps) {
  // Include cross-seeds in the displayed count when selected
  const displayCount = deleteCrossSeeds
    ? count + crossSeedWarning.affectedTorrents.length
    : count

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!max-w-2xl">
        <AlertDialogHeader>
          <AlertDialogTitle>Delete {displayCount} torrent(s)?</AlertDialogTitle>
          <AlertDialogDescription>
            This action cannot be undone. The torrents will be removed from qBittorrent.
            {totalSize > 0 && (
              <span className="block mt-2 text-xs text-muted-foreground">
                Total size: {formattedSize}
              </span>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <DeleteFilesPreference
          id="deleteFiles"
          checked={deleteFiles}
          onCheckedChange={onDeleteFilesChange}
          isLocked={isDeleteFilesLocked}
          onToggleLock={onToggleDeleteFilesLock}
        />
        <CrossSeedWarning
          affectedTorrents={crossSeedWarning.affectedTorrents}
          searchState={crossSeedWarning.searchState}
          hasWarning={crossSeedWarning.hasWarning}
          deleteFiles={deleteFiles}
          deleteCrossSeeds={deleteCrossSeeds}
          onDeleteCrossSeedsChange={onDeleteCrossSeedsChange}
          onSearch={crossSeedWarning.search}
          totalToCheck={crossSeedWarning.totalToCheck}
          checkedCount={crossSeedWarning.checkedCount}
        />
        <AlertDialogFooter>
          <AlertDialogCancel>Cancel</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            Delete
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
