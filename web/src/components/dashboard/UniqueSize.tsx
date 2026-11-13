/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { resolveDashboardSizes } from "@/lib/dashboard-stream"
import { formatBytes } from "@/lib/utils"
import type { TorrentStats } from "@/types"
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

export function DashboardTotalSize({ stats }: { stats: TorrentStats | null }) {
  const { totalSize, calculating } = resolveDashboardSizes([stats])

  return (
    <span className="ml-auto flex min-w-0 items-center gap-1.5 font-medium">
      {calculating && <SizeCalculatingMark />}
      <span className="truncate">{formatBytes(totalSize)}</span>
    </span>
  )
}
