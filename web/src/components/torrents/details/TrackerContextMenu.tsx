/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuTrigger } from "@/components/ui/context-menu"
import { isValidTrackerUrl } from "@/lib/tracker-utils"
import type { TorrentTracker } from "@/types"
import { Edit } from "lucide-react"
import { memo, type ReactNode } from "react"

interface TrackerContextMenuProps {
  children: ReactNode
  tracker: TorrentTracker
  onEditTracker?: (tracker: TorrentTracker) => void
  supportsTrackerEditing?: boolean
}

/**
 * Wrapper component that adds a context menu with "Edit Tracker URL" option
 * to tracker elements. Only shows context menu for valid tracker URLs
 * (excludes DHT, PeX, LSD entries).
 */
export const TrackerContextMenu = memo(function TrackerContextMenu({
  children,
  tracker,
  onEditTracker,
  supportsTrackerEditing = false,
}: TrackerContextMenuProps) {
  // Only show context menu for valid URLs and when edit handler is provided
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
          Edit Tracker URL
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  )
})
