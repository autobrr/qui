/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import type { ElementType } from "react"
import { useTranslation } from "react-i18next"

import { ShrinkFailedActions } from "@/components/database/ShrinkFailedActions"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { SystemNoticeDismissButton } from "@/components/ui/SystemNoticeDismissButton"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useNow } from "@/hooks/useNow"
import { useSystemNoticeDescription } from "@/hooks/useSystemNoticeDescription"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { systemNoticeDotColor } from "@/lib/system-notice"
import { cn } from "@/lib/utils"
import type { SystemNotice } from "@/types"

type SystemNoticeDotProps = { severity: SystemNotice["severity"]; fresh?: boolean; className?: string }

export function SystemNoticeDot({ severity, fresh = false, className }: SystemNoticeDotProps) {
  const color = systemNoticeDotColor[severity] ?? systemNoticeDotColor.error
  return (
    <span className={cn("relative flex h-2 w-2 shrink-0", className)}>
      {fresh && (
        <span className={cn("absolute inline-flex h-full w-full rounded-full opacity-70 motion-safe:animate-ping [animation-iteration-count:3]!", color.fill)} />
      )}
      <span className={cn("relative inline-flex h-2 w-2 rounded-full ring-[3px]", color.fill, color.ring)} />
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
  const description = useSystemNoticeDescription(notice.kind, notice.reason)
  const hasDismissButton = notice.kind === "low_disk"
  const { formatClockTime } = useDateTimeFormatters()
  const openedAt = new Date(notice.openedAt)
  // Like the row, the detail counts a notice under one minute old as one minute.
  const ageFrom = new Date(Math.min(openedAt.getTime(), now - 60 * 1000))

  return (
    <div>
      <div className="flex items-start gap-3 px-4 pr-10 pt-4">
        <SystemNoticeDot severity={notice.severity} className="mt-1.5" />
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
      {(description.action || hasDismissButton) && (
        <div className="flex flex-wrap items-center gap-2 border-t px-4 py-2.5 pl-9">
          {description.action && (
            <Button asChild size="sm" className="h-7 px-2.5 text-xs">
              <Link to={description.action.to} search={description.action.search} onClick={onNavigate}>
                {description.action.label}
              </Link>
            </Button>
          )}
          {hasDismissButton && <SystemNoticeDismissButton kind={notice.kind} />}
        </div>
      )}
      {notice.kind === "shrink_failed" && (
        <div className="border-t px-4 py-2.5 pl-9">
          <ShrinkFailedActions notice={notice} onNavigate={onNavigate} />
        </div>
      )}
    </div>
  )
}

type SystemNoticeDialogProps = {
  notice?: SystemNotice
  returnFocusTo?: HTMLElement | null
  onClose: () => void
}

// SystemNoticeDialog shows the detail of one notice, for the menus, where no
// popover fits beside the row. It closes when notice becomes undefined, and
// then gives focus to returnFocusTo, because the dialog has no trigger of its own.
export function SystemNoticeDialog({ notice, returnFocusTo, onClose }: SystemNoticeDialogProps) {
  return (
    <Dialog open={notice !== undefined} onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        className="max-h-[calc(100dvh-2rem)] gap-0 overflow-y-auto p-0 sm:max-w-sm"
        aria-describedby={undefined}
        onCloseAutoFocus={(event) => {
          if (returnFocusTo?.isConnected) {
            event.preventDefault()
            returnFocusTo.focus()
          }
        }}
      >
        {notice && <SystemNoticeDetail notice={notice} titleAs={DialogTitle} onNavigate={onClose} />}
      </DialogContent>
    </Dialog>
  )
}
