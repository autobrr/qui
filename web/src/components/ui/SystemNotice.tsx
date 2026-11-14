/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import type { ElementType } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useNow } from "@/hooks/useNow"
import { useSystemNoticeDescription } from "@/hooks/useSystemNoticeDescription"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { cn } from "@/lib/utils"
import type { SystemNotice } from "@/types"

export function SystemNoticeDot({ fresh = false, className }: { fresh?: boolean; className?: string }) {
  return (
    <span className={cn("relative flex h-2 w-2 shrink-0", className)}>
      {fresh && (
        <span className="absolute inline-flex h-full w-full rounded-full bg-destructive opacity-70 motion-safe:animate-ping [animation-iteration-count:3]!" />
      )}
      <span className="relative inline-flex h-2 w-2 rounded-full bg-destructive ring-[3px] ring-destructive/20" />
    </span>
  )
}

export function SystemNoticeHeading({ count }: { count: number }) {
  const { t } = useTranslation("common")
  return (
    <>
      <span>{t("systemNotice.needsAttention")}</span>
      <span className="tabular-nums text-destructive">{count}</span>
    </>
  )
}

type SystemNoticeDetailProps = {
  notice: SystemNotice
  titleAs?: ElementType
  titleId?: string
  onNavigate?: () => void
}

export function SystemNoticeDetail({ notice, titleAs: Title = "p", titleId, onNavigate }: SystemNoticeDetailProps) {
  const { t } = useTranslation("common")
  const now = useNow()
  const description = useSystemNoticeDescription(notice.kind)
  const { formatClockTime } = useDateTimeFormatters()
  const openedAt = new Date(notice.openedAt)
  // Like the row, the detail counts a notice under one minute old as one minute.
  const ageFrom = new Date(Math.min(openedAt.getTime(), now - 60 * 1000))

  return (
    <div>
      <div className="flex items-start gap-3 px-4 pr-10 pt-4">
        <SystemNoticeDot className="mt-1.5" />
        <div className="min-w-0">
          <Title {...(titleId && { id: titleId })} className="text-sm font-semibold leading-snug">{description.title}</Title>
          <p className="mt-0.5 text-xs text-muted-foreground">
            {t("systemNotice.since", { time: formatClockTime(openedAt), age: formatRelativeTime(ageFrom) })}
          </p>
        </div>
      </div>
      <div className="space-y-1.5 px-4 pb-3 pl-9 pt-2.5 text-[13px] leading-relaxed">
        <p>{description.effect}</p>
        <p className="text-muted-foreground">{description.fix}</p>
      </div>
      <div className="mx-4 mb-3 ml-9 flex items-start gap-1 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1">
        <code className="min-w-0 flex-1 break-words py-0.5 font-mono text-[11px] leading-relaxed text-muted-foreground">{notice.cause}</code>
        <CopyButton value={notice.cause} label={t("systemNotice.causeLabel")} className="size-6" iconClassName="size-3" />
      </div>
      {description.action && (
        <div className="border-t px-4 py-2.5 pl-9">
          <Button asChild size="sm" className="h-7 px-2.5 text-xs">
            <Link to={description.action.to} search={description.action.search} onClick={onNavigate}>
              {description.action.label}
            </Link>
          </Button>
        </div>
      )}
    </div>
  )
}

// SystemNoticeDialog shows the detail of one notice, for the menus, where no
// popover fits beside the row. It closes when notice becomes undefined.
export function SystemNoticeDialog({ notice, onClose }: { notice?: SystemNotice; onClose: () => void }) {
  return (
    <Dialog open={notice !== undefined} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="gap-0 p-0 sm:max-w-sm" aria-describedby={undefined}>
        {notice && <SystemNoticeDetail notice={notice} titleAs={DialogTitle} onNavigate={onClose} />}
      </DialogContent>
    </Dialog>
  )
}
