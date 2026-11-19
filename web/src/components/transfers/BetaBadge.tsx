/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { cn } from "@/lib/utils"
import { useTranslation } from "react-i18next"

/** The label beside the name of Transfer and Share while the two features are new. It takes the text color of its place. */
export function BetaBadge({ className }: { className?: string }) {
  const { t } = useTranslation("common")
  return (
    <Badge
      variant="outline"
      className={cn(
        "inline-block border-current/30 px-1.5 py-[3px] text-[0.6875rem] leading-none font-medium text-current/80",
        // A line of text has more room below its capitals than above. The trim makes the two paddings equal around the letters.
        "supports-[text-box:trim-both]:py-[5px] supports-[text-box:trim-both]:[text-box:trim-both_cap_alphabetic]",
        className
      )}
    >
      {t("labels.beta")}
    </Badge>
  )
}
