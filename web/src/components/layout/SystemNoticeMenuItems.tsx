/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId } from "react"

import { DropdownMenuGroup, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator } from "@/components/ui/dropdown-menu"
import { SystemNoticeDot, SystemNoticeHeading } from "@/components/ui/SystemNotice"
import { useNow } from "@/hooks/useNow"
import { useSystemNoticeDescription } from "@/hooks/useSystemNoticeDescription"
import { formatNarrowAge } from "@/lib/dateTimeUtils"
import { isFreshSystemNotice } from "@/lib/system-notice"
import type { SystemNotice } from "@/types"

type SystemNoticeMenuItemsProps = {
  notices: SystemNotice[]
  onOpen: (kind: string, menuButton: HTMLElement | null) => void
}

// SystemNoticeMenuItems lists the open notices at the top of a menu. The menu
// closes when the user selects a row, so the caller shows the detail in a
// SystemNoticeDialog outside the menu.
export function SystemNoticeMenuItems({ notices, onOpen }: SystemNoticeMenuItemsProps) {
  const now = useNow()
  const labelId = useId()

  if (notices.length === 0) {
    return null
  }
  return (
    <>
      <DropdownMenuGroup aria-labelledby={labelId}>
        <DropdownMenuLabel id={labelId} className="flex items-center justify-between text-xs font-semibold uppercase tracking-wide text-muted-foreground">
          <SystemNoticeHeading count={notices.length} />
        </DropdownMenuLabel>
        {notices.map((notice) => (
          <SystemNoticeMenuRow key={notice.kind} notice={notice} now={now} onOpen={onOpen} />
        ))}
      </DropdownMenuGroup>
      <DropdownMenuSeparator />
    </>
  )
}

function SystemNoticeMenuRow({ notice, now, onOpen }: { notice: SystemNotice; now: number; onOpen: SystemNoticeMenuItemsProps["onOpen"] }) {
  const { short, menuFix } = useSystemNoticeDescription(notice.kind, notice.reason)

  return (
    <DropdownMenuItem className="cursor-pointer items-start" onSelect={(event) => onOpen(notice.kind, menuButtonOf(event.currentTarget))}>
      <span className="mt-1.5 flex h-2 w-4 items-center justify-center">
        <SystemNoticeDot severity={notice.severity} fresh={isFreshSystemNotice(notice.openedAt, now)} />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate font-medium">{short}</span>
        <span className="block truncate text-xs text-muted-foreground">{menuFix}</span>
      </span>
      <span className="mt-0.5 text-xs tabular-nums text-muted-foreground">
        {formatNarrowAge(now - Date.parse(notice.openedAt))}
      </span>
    </DropdownMenuItem>
  )
}

// The menu names the button that opens it in aria-labelledby.
function menuButtonOf(item: EventTarget | null): HTMLElement | null {
  const menuButtonId = item instanceof Element ? item.closest("[role=menu]")?.getAttribute("aria-labelledby") : null
  return menuButtonId ? document.getElementById(menuButtonId) : null
}
