/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useHasPremiumAccess } from "@/hooks/useLicense"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"
import { Check, Heart } from "lucide-react"
import { useTranslation } from "react-i18next"
import { openSupport } from "./open"
import { ReminderPill } from "./SupportReminder"
import { useSupportReminder } from "./reminder"

const WORD_QUERY = "(min-width: 1280px)"

export function SupportButton() {
  const { t } = useTranslation("common")
  const { isSupporter } = useHasPremiumAccess()
  const reminder = useSupportReminder()
  const wide = useMediaQuery(WORD_QUERY)
  if (reminder.shortcutHidden) return null
  if (reminder.due && wide) return <ReminderPill days={reminder.days} />

  const worded = isSupporter && wide
  const label = isSupporter ? t("support.supporter.title") : t("support.title")

  let mark = <Heart className="h-4 w-4 fill-current text-red-500" />
  if (worded) {
    mark = (
      <span className="flex h-7 items-center gap-1.5 rounded-full border border-red-500/30 bg-red-500/10 pr-3 pl-2.5 text-xs font-medium">
        <Heart className="h-3.5 w-3.5 fill-current text-red-500" />
        {t("support.supporter.badge")}
      </span>
    )
  } else if (isSupporter) {
    mark = (
      <span className="relative flex">
        <Heart className="h-4 w-4 fill-current text-red-500" />
        <span className="absolute -right-1.5 -bottom-1.5 flex h-3 w-3 items-center justify-center rounded-full bg-foreground ring-2 ring-background">
          <Check className="h-2 w-2 text-background" strokeWidth={4} />
        </span>
      </span>
    )
  }

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size={worded ? "sm" : "icon"}
          className={cn("hover:bg-muted transition-colors", worded && "h-9 rounded-full px-1 hover:bg-transparent")}
          aria-label={label}
          onClick={openSupport}
        >
          {mark}
        </Button>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}
