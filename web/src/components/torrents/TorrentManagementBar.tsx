/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TORRENT_ACTIONS } from "@/hooks/useTorrentActions"
import type { ActionTarget } from "@/lib/torrent-selection"
import {
  ArrowDown,
  ArrowUp,
  Blocks,
  CheckCircle,
  ChevronsDown,
  ChevronsUp,
  Folder,
  FolderOpen,
  Gauge,
  List,
  Pause,
  Play,
  Radio,
  Settings2,
  Share2,
  Sprout,
  Tag,
  Trash2
} from "lucide-react"
import { memo, useCallback } from "react"
import { useTranslation } from "react-i18next"
import { useBulkActions } from "./bulk-actions/bulk-actions-context"

interface TorrentManagementBarProps {
  instanceId?: number
  // The Action target that the desktop table publishes (ADR 0034).
  actionTarget: ActionTarget
}

export const TorrentManagementBar = memo(function TorrentManagementBar({
  instanceId,
  actionTarget,
}: TorrentManagementBarProps) {
  const { t } = useTranslation("torrents")
  const { count: selectionCount, previewTorrents: selectedTorrents } = actionTarget
  const isAllSelected = actionTarget.kind === "selectAll"
  const hasActionScope = typeof instanceId === "number" && instanceId >= 0
  const { pick, isPending } = useBulkActions()

  const handleQueueAction = useCallback((action: "topPriority" | "increasePriority" | "decreasePriority" | "bottomPriority") => {
    pick({ action })
  }, [pick])

  const hasSelection = selectionCount > 0 || isAllSelected
  const isDisabled = !hasActionScope || !hasSelection

  // Keep this guard after hooks so their invocation order stays stable.
  if (!hasActionScope || !hasSelection) {
    return null
  }

  return (
    <div
      className="flex items-center h-9 dark:bg-input/30 border border-input rounded-md mr-2 px-3 py-2 gap-3 shadow-xs transition-all duration-200"
      role="toolbar"
      aria-label={t("managementBar.ariaLabel", { count: selectionCount })}
    >
      <div className="flex items-center gap-3 flex-shrink-0 min-w-0">
        <span className="text-xs text-muted-foreground whitespace-nowrap min-w-[3ch] text-center">
          {selectionCount}
        </span>
      </div>

      <div className="flex items-center gap-1">
        {/* Primary Actions */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: TORRENT_ACTIONS.RESUME })}
              disabled={isPending || isDisabled}
            >
              <Play className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.resume")}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: TORRENT_ACTIONS.PAUSE })}
              disabled={isPending || isDisabled}
            >
              <Pause className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.pause")}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: TORRENT_ACTIONS.RECHECK })}
              disabled={isPending || isDisabled}
            >
              <CheckCircle className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.forceRecheck")}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: TORRENT_ACTIONS.REANNOUNCE })}
              disabled={isPending || isDisabled}
            >
              <Radio className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.reannounce")}</TooltipContent>
        </Tooltip>

        {(() => {
          const seqDlStates = selectedTorrents.map(t => t.seq_dl)
          const allSeqDlEnabled = seqDlStates.length > 0 && seqDlStates.every(state => state === true)

          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => pick({ action: TORRENT_ACTIONS.TOGGLE_SEQUENTIAL_DOWNLOAD, enable: !allSeqDlEnabled })}
                  disabled={isPending || isDisabled}
                >
                  <Blocks className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>{allSeqDlEnabled ? t("managementBar.sequentialDownload.disable") : t("managementBar.sequentialDownload.enable")}</TooltipContent>
            </Tooltip>
          )
        })()}

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: "setTags" })}
              disabled={isPending || isDisabled}
            >
              <Tag className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.setTags")}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: "setCategory" })}
              disabled={isPending || isDisabled}
            >
              <Folder className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.setCategory")}</TooltipContent>
        </Tooltip>

        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: "setLocation" })}
              disabled={isPending || isDisabled}
            >
              <FolderOpen className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.setLocation")}</TooltipContent>
        </Tooltip>

        {/* Queue Priority */}
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={isPending || isDisabled}
                >
                  <List className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.queuePriority")}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="center">
            <DropdownMenuItem
              onClick={() => handleQueueAction("topPriority")}
              disabled={isPending || isDisabled}
            >
              <ChevronsUp className="h-4 w-4 mr-2" />
              {t("managementBar.topPriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => handleQueueAction("increasePriority")}
              disabled={isPending || isDisabled}
            >
              <ArrowUp className="h-4 w-4 mr-2" />
              {t("managementBar.increasePriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => handleQueueAction("decreasePriority")}
              disabled={isPending || isDisabled}
            >
              <ArrowDown className="h-4 w-4 mr-2" />
              {t("managementBar.decreasePriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => handleQueueAction("bottomPriority")}
              disabled={isPending || isDisabled}
            >
              <ChevronsDown className="h-4 w-4 mr-2" />
              {t("managementBar.bottomPriority")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Share/Speed Limits */}
        <DropdownMenu>
          <Tooltip>
            <TooltipTrigger asChild>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={isPending || isDisabled}
                >
                  <Share2 className="h-4 w-4" />
                </Button>
              </DropdownMenuTrigger>
            </TooltipTrigger>
            <TooltipContent>{t("managementBar.limits")}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent>
            <DropdownMenuItem
              onClick={() => pick({ action: "setShareLimit" })}
              disabled={isPending || isDisabled}
            >
              <Sprout className="mr-2 h-4 w-4" />
              {t("managementBar.setShareLimit")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => pick({ action: "setSpeedLimits" })}
              disabled={isPending || isDisabled}
            >
              <Gauge className="mr-2 h-4 w-4" />
              {t("managementBar.setSpeedLimit")} {selectionCount > 1 ? `(${selectionCount})` : ""}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>

        {/* TMM Toggle */}
        {(() => {
          const tmmStates = selectedTorrents.map(t => t.auto_tmm)
          const allEnabled = tmmStates.length > 0 && tmmStates.every(state => state === true)
          const mixed = tmmStates.length > 0 && !tmmStates.every(state => state === allEnabled)

          return (
            <Tooltip>
              <TooltipTrigger asChild>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => pick({ action: TORRENT_ACTIONS.TOGGLE_AUTO_TMM, enable: !allEnabled })}
                  disabled={isPending || isDisabled}
                >
                  <Settings2 className="h-4 w-4" />
                </Button>
              </TooltipTrigger>
              <TooltipContent>
                {mixed ? t("managementBar.tmm.mixed") : allEnabled ? t("managementBar.tmm.disable") : t("managementBar.tmm.enable")}
              </TooltipContent>
            </Tooltip>
          )
        })()}

        {/* Delete Action */}
        <Tooltip>
          <TooltipTrigger asChild>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => pick({ action: TORRENT_ACTIONS.DELETE })}
              disabled={isPending || isDisabled}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="h-4 w-4" />
            </Button>
          </TooltipTrigger>
          <TooltipContent>{t("managementBar.delete")}</TooltipContent>
        </Tooltip>
      </div>
    </div>
  )
})
