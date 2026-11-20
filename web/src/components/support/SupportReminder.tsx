/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { Heart, X } from "lucide-react"
import { useTranslation } from "react-i18next"
import { openSupport } from "./open"
import { notNow, useSupportReminder } from "./reminder"

/** The yearly reminder in the header, from 1280 px: the heart grows into a pill. */
export function ReminderPill({ days }: { days: number }) {
  const { t } = useTranslation("common")

  return (
    <div className="flex h-9 items-center rounded-full border border-red-500/25 bg-red-500/10 pr-1 pl-3 text-sm">
      <Tooltip>
        <TooltipTrigger asChild>
          <button type="button" className="flex items-center gap-2 whitespace-nowrap" onClick={openSupport}>
            <Heart className="h-4 w-4 shrink-0 fill-current text-red-500" />
            <span className="font-medium">{t("support.reminder.title", { count: days })}</span>
            <span className="hidden text-muted-foreground min-[1366px]:inline">{t("support.title")}</span>
          </button>
        </TooltipTrigger>
        <TooltipContent>{t("support.reminder.tooltip")}</TooltipContent>
      </Tooltip>
      <button
        type="button"
        aria-label={t("support.reminder.notNow")}
        className="ml-1 flex h-7 w-7 items-center justify-center rounded-full text-muted-foreground hover:bg-red-500/15 hover:text-foreground"
        onClick={() => notNow(t)}
      >
        <X className="h-3.5 w-3.5" />
      </button>
    </div>
  )
}

/** The yearly reminder under 1280 px and on a phone: a row above the page. The header has no room for the pill there. */
export function ReminderBar() {
  const { t } = useTranslation("common")
  const { due, days } = useSupportReminder()
  if (!due) return null

  return (
    <div className="flex items-center gap-1 border-b bg-muted/40 pl-3 xl:hidden">
      <button type="button" className="flex min-h-12 min-w-0 flex-1 items-center gap-2.5 text-left" onClick={openSupport}>
        <Heart className="h-4 w-4 shrink-0 fill-current text-red-500" />
        <span className="min-w-0">
          <span className="block truncate text-sm font-medium">{t("support.reminder.title", { count: days })}</span>
          <span className="block truncate text-xs text-muted-foreground">{t("support.reminder.subtitle")}</span>
        </span>
      </button>
      <button
        type="button"
        aria-label={t("support.reminder.notNow")}
        className="flex h-12 w-11 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground"
        onClick={() => notNow(t)}
      >
        <X className="h-4 w-4" />
      </button>
    </div>
  )
}
