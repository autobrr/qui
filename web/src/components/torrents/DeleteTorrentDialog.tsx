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
import type { Torrent } from "@/types"
import { AlertTriangle } from "lucide-react"
import { cn } from "@/lib/utils"
import { getLinuxIsoName, useIncognitoMode } from "@/lib/incognito"

// From this many torrents, the dialog names a few of them and the button shows the count.
const LARGE_DELETE = 10

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
  isAllSelected?: boolean
  // Loaded torrents that the delete reaches. In select all mode, the others are not loaded in the table.
  previewTorrents?: Torrent[]
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
  isAllSelected = false,
  previewTorrents = [],
}: DeleteTorrentDialogProps) {
  const { t } = useTranslation("torrents")
  const [incognitoMode] = useIncognitoMode()
  // Include cross-seeds in the displayed count when selected
  const crossSeedCount = deleteCrossSeeds ? (crossSeedWarning?.affectedTorrents.length ?? 0) : 0
  const displayCount = count + crossSeedCount
  const isLarge = isAllSelected || displayCount >= LARGE_DELETE
  const shown = previewTorrents.slice(0, 3)
  const notLoaded = Math.max(0, count - previewTorrents.length)

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent className="!max-w-2xl">
        <AlertDialogHeader>
          <AlertDialogTitle>
            {isAllSelected ? t("deleteDialog.titleSelectAll", { count }) : t("deleteDialog.title", { count: displayCount })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t("deleteDialog.description")}
            {totalSize > 0 && (
              <span className="block mt-2 text-xs text-muted-foreground">
                {t("deleteDialog.totalSize", { size: formattedSize })}
              </span>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {isLarge && (
          <div className="flex gap-2 rounded-md border border-destructive/50 bg-destructive/10 p-3 text-sm">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
            <div className="min-w-0">
              {isAllSelected && (
                <p className="font-medium text-destructive">
                  {t("deleteDialog.selectAllNotice")}
                  {notLoaded > 0 && ` ${t("deleteDialog.notLoaded", { count: notLoaded })}`}
                </p>
              )}
              <ul className={cn("space-y-0.5 text-xs text-muted-foreground", isAllSelected && "mt-2")}>
                {shown.map(torrent => <li key={torrent.hash} className="truncate">{incognitoMode ? getLinuxIsoName(torrent.hash) : torrent.name}</li>)}
                {displayCount > shown.length && <li>{t("deleteDialog.andMore", { more: displayCount - shown.length })}</li>}
              </ul>
            </div>
          </div>
        )}
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
              {t("deleteDialog.blockCrossSeeds")}
            </label>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t("deleteDialog.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={onConfirm}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {isLarge ? t("deleteDialog.deleteCount", { count: displayCount }) : t("deleteDialog.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
