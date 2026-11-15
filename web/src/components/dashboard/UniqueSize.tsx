/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useTranslation } from "react-i18next"

export function SizeCalculatingMark() {
  const { t } = useTranslation("dashboard")

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span className="cursor-help italic text-muted-foreground">{t("uniqueSize.calculating")}</span>
      </TooltipTrigger>
      <TooltipContent className="max-w-xs">
        {t("uniqueSize.calculatingTooltip")}
      </TooltipContent>
    </Tooltip>
  )
}
