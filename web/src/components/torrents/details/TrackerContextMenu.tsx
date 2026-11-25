/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { isValidTrackerUrl } from "@/lib/tracker-utils"
import type { TorrentTracker } from "@/types"
import { Edit } from "lucide-react"
import { memo, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

interface TrackerContextMenuProps {
  children: ReactNode
  tracker: TorrentTracker
  onEditTracker?: (tracker: TorrentTracker) => void
  supportsTrackerEditing?: boolean
}

export const TrackerContextMenu = memo(function TrackerContextMenu({
  children,
  tracker,
  onEditTracker,
  supportsTrackerEditing = false,
}: TrackerContextMenuProps) {
  const { t } = useTranslation("torrents")
  if (!onEditTracker || !isValidTrackerUrl(tracker.url)) {
    return <>{children}</>
  }

  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        {children}
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem
          disabled={!supportsTrackerEditing}
          onClick={() => onEditTracker(tracker)}
        >
          <Edit className="mr-2 h-4 w-4" />
          {t("trackerContextMenu.editTrackerUrl")}
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})
