/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChevronRight } from "lucide-react"
import { useId, useState } from "react"

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import { SystemNoticeDetail, SystemNoticeDot, SystemNoticeHeading } from "@/components/ui/SystemNotice"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useNow } from "@/hooks/useNow"
import { useSystemNoticeDescription } from "@/hooks/useSystemNoticeDescription"
import { useSystemNotices } from "@/hooks/useSystemNotices"
import { formatNarrowAge } from "@/lib/dateTimeUtils"
import { isFreshSystemNotice } from "@/lib/system-notice"
import { cn, collapseRowClassName } from "@/lib/utils"
import type { SystemNotice } from "@/types"

const fadeClassName = "transition-opacity duration-200 motion-reduce:transition-none"

export function SystemNoticeList({ iconOnly = false }: { iconOnly?: boolean }) {
  const notices = useSystemNotices()
  const now = useNow()
  const headingId = useId()

  if (notices.length === 0) {
    return null
  }
  return (
    <section aria-labelledby={headingId}>
      <div className={collapseRowClassName(iconOnly)}>
        <div className="min-h-0 overflow-hidden">
          <p
            id={headingId}
            className="flex items-center justify-between whitespace-nowrap px-3 pb-1.5 text-xs font-semibold uppercase tracking-wider text-sidebar-foreground/70"
          >
            <SystemNoticeHeading count={notices.length} />
          </p>
        </div>
      </div>
      <div className="space-y-0.5 short:space-y-px">
        {notices.map((notice) => (
          <SystemNoticeRow key={notice.kind} notice={notice} now={now} iconOnly={iconOnly} />
        ))}
      </div>
    </section>
  )
}

function SystemNoticeRow({ notice, now, iconOnly }: { notice: SystemNotice; now: number; iconOnly: boolean }) {
  const { short } = useSystemNoticeDescription(notice.kind, notice.reason)
  const [open, setOpen] = useState(false)
  const titleId = useId()

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <Tooltip delayDuration={150} disabled={!iconOnly || open}>
        <PopoverTrigger asChild>
          <TooltipTrigger
            openOnTap={false}
            className={cn(
              "group flex w-full items-center gap-3 rounded-md px-3 py-1.5 text-left text-sm font-medium short:py-1 short:text-13/5 text-sidebar-foreground outline-none transition-all duration-200 ease-out hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-2 focus-visible:ring-sidebar-ring data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground",
              iconOnly && "overflow-hidden whitespace-nowrap"
            )}
          >
            <span className="flex h-4 w-4 flex-shrink-0 items-center justify-center">
              <SystemNoticeDot severity={notice.severity} fresh={isFreshSystemNotice(notice.openedAt, now)} />
            </span>
            <span className={cn("flex min-w-0 flex-1 items-center gap-3", fadeClassName, iconOnly && "min-w-45 shrink-0 opacity-0")}>
              <span className="min-w-0 flex-1 truncate">{short}</span>
              <span className="text-xs font-normal tabular-nums text-sidebar-foreground/50 group-hover:hidden group-data-[state=open]:hidden">
                {formatNarrowAge(now - Date.parse(notice.openedAt))}
              </span>
              <ChevronRight className="hidden h-3.5 w-3.5 text-sidebar-foreground/60 group-hover:block group-data-[state=open]:block" />
            </span>
          </TooltipTrigger>
        </PopoverTrigger>
        <TooltipContent side="right" className="max-w-64 text-xs">{short}</TooltipContent>
      </Tooltip>
      <PopoverContent side="right" align="end" sideOffset={16} className="w-80 p-0" aria-labelledby={titleId}>
        <SystemNoticeDetail notice={notice} titleId={titleId} onNavigate={() => setOpen(false)} />
      </PopoverContent>
    </Popover>
  )
}
