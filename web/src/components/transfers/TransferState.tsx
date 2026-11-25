/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Progress } from "@/components/ui/progress"
import { formatNarrowAge, formatRelativeTime } from "@/lib/dateTimeUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import type { TransferRow, TransferState } from "@/lib/transfers-api"
import { cn } from "@/lib/utils"
import { Ban, CircleCheck, CircleX, Clock, Loader2, type LucideIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { percentOf, timeLeftMs, waitsForRetry } from "./transfer-display"

const stateIcons: Record<TransferState, { icon: LucideIcon; className: string }> = {
  waiting: { icon: Clock, className: "text-muted-foreground" },
  copying: { icon: Loader2, className: "motion-safe:animate-spin" },
  checking: { icon: Loader2, className: "motion-safe:animate-spin" },
  finishing: { icon: Loader2, className: "motion-safe:animate-spin" },
  done: { icon: CircleCheck, className: "text-emerald-500" },
  failed: { icon: CircleX, className: "text-destructive" },
  cancelled: { icon: Ban, className: "text-muted-foreground" },
}

export function StateLabel({ state, className }: { state: TransferState; className?: string }) {
  const { t } = useTranslation("transfers")
  const { icon: Icon, className: iconClass } = stateIcons[state] ?? stateIcons.waiting
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-foreground", className)}>
      <Icon aria-hidden="true" className={cn("size-3.5 shrink-0", iconClass)} />
      {t([`states.${state}`, "states.fallback"])}
    </span>
  )
}

export function StateDetail({ row }: { row: TransferRow }) {
  const { t, i18n } = useTranslation("transfers")
  const [speedUnit] = useSpeedUnits()
  if (row.progress) {
    const percent = percentOf(row.progress)
    const left = timeLeftMs(row.progress)
    const facts = [
      new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language, { style: "percent" }).format(percent / 100),
      ...(row.progress.speed > 0 ? [formatSpeedWithUnit(row.progress.speed, speedUnit)] : []),
      ...(left === null ? [] : [t("row.timeLeft", { time: formatNarrowAge(left) })]),
    ]
    return (
      <div className="mt-1.5 w-full max-w-48 space-y-1 @lg:w-48">
        <Progress value={percent} aria-label={t("columns.progress")} className="h-1.5" />
        <div className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">{facts.join(" · ")}</div>
      </div>
    )
  }
  if (waitsForRetry(row)) {
    return <div className="mt-1 text-xs text-muted-foreground">{t("row.retryAt", { time: formatRelativeTime(row.retryAt) })}</div>
  }
  return null
}

export function StartedBy({ row }: { row: TransferRow }) {
  const { t } = useTranslation("transfers")
  if (row.startedBy === "user") return t("row.byUser")
  return row.ruleName ? t("row.byRule", { name: row.ruleName }) : t("row.byDeletedRule")
}
