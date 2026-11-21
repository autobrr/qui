/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardAction, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import { useTrackerGroupIcon, useTrackerGroupName } from "@/hooks/useTrackerGroupName"
import type { TrackerGroupShares } from "@/lib/dashboard-overview"
import { themeArea } from "@/lib/theme-areas"
import { cn, formatBytes } from "@/lib/utils"
import { Link } from "@tanstack/react-router"
import { ChevronRight } from "lucide-react"
import type { CSSProperties } from "react"
import { useTranslation } from "react-i18next"

const OTHER_COLOR = "color-mix(in oklab, var(--muted-foreground) 45%, transparent)"

// The series color of the theme: the inline style holds both sets, and the class picks one.
const SERIES_CLASS = "[--series:var(--series-light)] dark:[--series:var(--series-dark)]"

const TITLE_ID = "dashboard-top-tracker-groups"

/** The largest Tracker groups of the last 24 hours, in the series colors of the Statistics page. The rows add up to the upload. */
export function TopTrackerGroups({ shares, className }: { shares: TrackerGroupShares; className?: string }) {
  const { t, i18n } = useTranslation("dashboard")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const colors = useSeriesColors()
  const displayName = useTrackerGroupName()
  const groupIcon = useTrackerGroupIcon()
  const percent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 })
  const rows = [
    ...shares.top.map((share, index) => ({
      key: share.group,
      name: displayName(share.group),
      icon: groupIcon(share.group),
      uploaded: share.uploaded,
      style: { "--series-light": colors.light[index], "--series-dark": colors.dark[index], background: "var(--series)" } as CSSProperties,
    })),
    ...(shares.other ? [{ key: "", name: t("statistics:chart.otherCount", { groups: shares.other.groups }), icon: undefined, uploaded: shares.other.uploaded, style: { background: OTHER_COLOR } }] : []),
  ]

  return (
    <Card {...themeArea("dashboard-card")} role="region" aria-labelledby={TITLE_ID} className={cn("gap-3 py-4", className)}>
      <CardHeader className="px-4 lg:px-6">
        <CardTitle id={TITLE_ID}>{t("trackers.title")}</CardTitle>
        <CardAction>
          <Link to="/statistics" search={{ period: "24h" }} className="-my-2 flex h-8 items-center text-xs text-muted-foreground hover:text-foreground">
            {t("trackers.statistics")}<ChevronRight className="size-3.5" />
          </Link>
        </CardAction>
      </CardHeader>
      <CardContent className="px-4 lg:px-6">
        {shares.uploaded > 0 ? (
          <>
            <div className="flex h-2 gap-px overflow-hidden rounded-full" aria-hidden="true">
              {rows.map(row => <span key={row.key} className={SERIES_CLASS} style={{ ...row.style, width: `${(row.uploaded / shares.uploaded) * 100}%` }} />)}
            </div>
            <ul className="mt-2 grid gap-x-10 sm:grid-cols-2 2xl:grid-cols-3">
              {rows.map(row => (
                <li key={row.key} data-testid="tracker-row" className="flex h-9 min-w-0 items-center gap-2 border-b border-border/60 text-sm tabular-nums">
                  <span className={`size-2.5 shrink-0 rounded-[3px] ${SERIES_CLASS}`} style={row.style} />
                  {row.icon ? <TrackerIconImage {...row.icon} /> : <span className="size-4 shrink-0" />}
                  <span className="min-w-0 flex-1 truncate">{row.name}</span>
                  <span>{formatBytes(row.uploaded)}</span>
                  <span className="w-10 text-right text-muted-foreground">{percent.format(row.uploaded / shares.uploaded)}</span>
                </li>
              ))}
            </ul>
          </>
        ) : (
          <p className="text-sm text-muted-foreground">{t("trackers.empty")}</p>
        )}
      </CardContent>
    </Card>
  )
}
