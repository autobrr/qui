/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"

interface CachedDataBadgeProps {
  updatedAt: number
  className?: string
}

export function CachedDataBadge({ updatedAt, className }: CachedDataBadgeProps) {
  const { t } = useTranslation("torrents")
  // updatedAt is epoch milliseconds; pass a Date so formatRelativeTime does not
  // treat the number as seconds. Reuses the same formatter the list badge uses
  // for streamMeta.lastSuccessfulSync so the two badges read identically.
  const relativeAge = formatRelativeTime(new Date(updatedAt))

  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 text-xs font-medium text-amber-600 dark:text-amber-400",
        className
      )}
    >
      <span className="h-2 w-2 shrink-0 rounded-full bg-amber-400 shadow-[0_0_0_2px] shadow-amber-400/25" />
      <span>{t("detailCache.showingCached", { age: relativeAge })}</span>
    </span>
  )
}
