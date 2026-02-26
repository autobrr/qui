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
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { CrossSeedWarning } from "./CrossSeedWarning"
import { DeleteFilesPreference } from "./DeleteFilesPreference"
import type { CrossSeedWarningResult } from "@/hooks/useCrossSeedWarning"
import { Checkbox } from "@/components/ui/checkbox"
import { useTranslation } from "react-i18next"

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
  showBlockCrossSeeds: boolean
  blockCrossSeeds: boolean
  onBlockCrossSeedsChange: (checked: boolean) => void
  crossSeedWarning?: CrossSeedWarningResult | null
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
  showBlockCrossSeeds,
  blockCrossSeeds,
  onBlockCrossSeedsChange,
  crossSeedWarning,
  onConfirm,
}: DeleteTorrentDialogProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  // Include cross-seeds in the displayed count when selected
  const crossSeedCount = deleteCrossSeeds ? (crossSeedWarning?.affectedTorrents.length ?? 0) : 0
  const displayCount = count + crossSeedCount

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!max-w-2xl">
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("deleteTorrentDialog.title", { count: displayCount })}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("deleteTorrentDialog.description")}
            {totalSize > 0 && (
              <span className="block mt-2 text-xs text-muted-foreground">
                {tr("deleteTorrentDialog.totalSize", { size: formattedSize })}
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
        {crossSeedWarning && (
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
        )}
        {showBlockCrossSeeds && (
          <div className="mt-3 flex items-center gap-2">
            <Checkbox
              id="blockCrossSeeds"
              checked={blockCrossSeeds}
              onCheckedChange={(checked) => onBlockCrossSeedsChange(checked === true)}
            />
            <label
              htmlFor="blockCrossSeeds"
              className="text-xs cursor-pointer select-none"
            >
              {tr("deleteTorrentDialog.blockCrossSeeds")}
            </label>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("deleteTorrentDialog.actions.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {tr("deleteTorrentDialog.actions.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
