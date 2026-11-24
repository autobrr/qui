/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { TransferChart } from "@/components/statistics/TransferChart"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import { ChartStyle, type ChartConfig } from "@/components/ui/chart"
import { TruncatedText } from "@/components/ui/truncated-text"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
import { calendarDays, daysSinceMonday, formatGapLength, monthBars, yearSummary, type CalendarDay, type StatisticsBar } from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"

const CHART_ID = "statistics-year"

// The share of the series color for each step of upload above zero.
const HEAT = [25, 50, 75, 100].map(percent => `color-mix(in oklab, var(--color-heat) ${percent}%, transparent)`)

interface YearViewProps {
  // The day bars of the period, by Tracker group.
  bars: StatisticsBar[]
  // The Tracker groups with a series color, in series order.
  groups: string[]
  to: Date
  since?: Date
  displayName: (group: string) => string
}

/** The upload of a long period: a calendar of the last 53 weeks, the best day and month, the weekend share, and the months by Tracker group. */
export function YearView({ bars, groups, to, since, displayName }: YearViewProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const days = useMemo(() => calendarDays(bars, to, since), [bars, to, since])
  const months = useMemo(() => monthBars(bars), [bars])
  const summary = useMemo(() => yearSummary(bars), [bars])
  const colors = useSeriesColors()
  const config = useMemo<ChartConfig>(() => ({ heat: { theme: { light: colors.light[0], dark: colors.dark[0] } } }), [colors])
  const { formatStart } = useBucketFormat()
  const formatPercent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 })

  return (
    <div className="@container/year" data-chart={CHART_ID}>
      <ChartStyle id={CHART_ID} config={config} />
      {/* A wide card puts the three numbers next to the calendar. A wider card puts them below it, and the months next to both. */}
      <div className="space-y-6 sm:space-y-8 @[96rem]/year:grid @[96rem]/year:grid-cols-[minmax(0,auto)_minmax(36rem,1fr)] @[96rem]/year:items-center @[96rem]/year:gap-x-10 @[96rem]/year:space-y-0">
        <div className="grid grid-cols-1 gap-6 sm:gap-8 @[68rem]/year:grid-cols-[minmax(0,auto)_minmax(14rem,1fr)] @[68rem]/year:items-center @[96rem]/year:grid-cols-1">
          <Calendar days={days} />
          <dl className="grid grid-cols-2 gap-4 sm:grid-cols-3 sm:gap-6 @[68rem]/year:grid-cols-1 @[68rem]/year:gap-4 @[96rem]/year:grid-cols-3 @[96rem]/year:gap-6">
            <YearFact
              label={t("yearView.bestDay")}
              value={summary.bestDay && formatBytes(summary.bestDay.uploaded)}
              detail={summary.bestDay && formatStart(summary.bestDay.start, "day")}
            />
            <YearFact
              label={t("yearView.bestMonth")}
              value={summary.bestMonth && formatStart(summary.bestMonth.start, "month")}
              detail={summary.bestMonth && formatBytes(summary.bestMonth.uploaded)}
            />
            <YearFact
              className="col-span-2 sm:col-span-1"
              label={t("yearView.weekends")}
              value={summary.weekendShare === undefined ? undefined : formatPercent.format(summary.weekendShare)}
              detail={t("yearView.weekendsDetail")}
            />
          </dl>
        </div>
        <div>
          <div className="mb-2 text-xs font-medium">{t("yearView.months")}</div>
          <TransferChart bars={months} groups={groups} metric="uploaded" unit="day" displayName={displayName} />
        </div>
      </div>
    </div>
  )
}

function Calendar({ days }: { days: CalendarDay[] }) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { preferences } = usePersistedDateTimePreferences()
  const scroller = useRef<HTMLDivElement>(null)
  // The start of the day that the pointer is on, or that the user tapped last.
  const [shown, setShown] = useState<number>()

  // On a narrow screen the calendar scrolls sideways, so it opens at the latest weeks.
  useEffect(() => {
    if (scroller.current) {
      scroller.current.scrollLeft = scroller.current.scrollWidth
    }
  }, [days])

  const thresholds = useMemo(() => {
    const values = days.flatMap(day => day.uploaded ? [day.uploaded] : []).sort((a, b) => a - b)
    return [0.25, 0.5, 0.75].map(quantile => values[Math.floor(values.length * quantile)] ?? 0)
  }, [days])

  // One column for each week from Monday on. The first column starts on the weekday of the first day.
  const weeks = useMemo(() => {
    const columns: (CalendarDay | undefined)[][] = []
    const lead = daysSinceMonday(new Date(days[0].start))
    const slots = [...Array<undefined>(lead).fill(undefined), ...days]
    for (let index = 0; index < slots.length; index += 7) {
      columns.push(slots.slice(index, index + 7))
    }
    return columns
  }, [days])

  const labels = useMemo(() => {
    return new Map(days.map(day => {
      const date = formatCalendarDay(new Date(day.start), preferences, "short")
      const text = day.uploaded === undefined ? t("yearView.dayNotRecorded", { date }) : t("yearView.day", { date, uploaded: formatBytes(day.uploaded) })
      return [day.start, day.gapSeconds > 0 ? t("yearView.dayGap", { day: text, gap: t("chart.gap", { duration: formatGapLength(day.gapSeconds, locale) }) }) : text]
    }))
  }, [days, locale, preferences, t])

  return (
    <div>
      <div className="mb-2 flex items-center justify-between gap-4 text-xs text-muted-foreground">
        <span aria-live="polite">{(shown !== undefined && labels.get(shown)) || t("yearView.calendar")}</span>
        <span className="hidden items-center gap-1 sm:flex" aria-hidden>
          {t("yearView.less")}
          <span className="size-3 rounded-[3px] bg-muted" />
          {HEAT.map(background => <span key={background} className="size-3 rounded-[3px]" style={{ background }} />)}
          {t("yearView.more")}
        </span>
      </div>
      <div ref={scroller} className="overflow-x-auto pb-1">
        {/* The cells grow with the card. The padding at the end holds the hit area of the last column, so that a calendar that fits does not scroll. */}
        <ol className="grid w-full min-w-min auto-cols-[minmax(0.75rem,1.125rem)] grid-flow-col gap-[3px] pe-0.5" aria-label={t("yearView.calendar")} onMouseLeave={() => setShown(undefined)}>
          {weeks.map((week, index) => {
            const first = week.find(day => day && new Date(day.start).getDate() === 1)
            return (
              <li key={index} className="flex min-w-0 flex-col gap-[3px]">
                {/* The name of a month is wider than its column. In the last columns it ends at the column, so that it stays in the calendar. */}
                <span className={cn("flex h-4 text-[10px] leading-4 whitespace-nowrap text-muted-foreground", index >= weeks.length - 2 && "justify-end")} aria-hidden>
                  {first && new Date(first.start).toLocaleDateString(locale, { month: "short" })}
                </span>
                <ol className="contents">
                  {week.map((day, slot) => day ? (
                    <li
                      key={day.start}
                      aria-label={labels.get(day.start)}
                      data-gap={day.gapSeconds > 0 || undefined}
                      onMouseEnter={() => setShown(day.start)}
                      onClick={() => setShown(day.start)}
                      className={cn(
                        // The hit area covers the gaps around the cell, so that each tap in the calendar lands on a day.
                        "relative aspect-square w-full rounded-[3px] after:absolute after:-inset-[1.5px]",
                        day.uploaded === undefined ? "border border-dashed border-border" : !day.uploaded && "bg-muted",
                        day.gapSeconds > 0 && "ring-1 ring-yellow-500",
                        shown === day.start && "outline outline-1 outline-foreground"
                      )}
                      style={day.uploaded ? { background: HEAT[thresholds.filter(threshold => day.uploaded! > threshold).length] } : undefined}
                    />
                  ) : <li key={`empty-${slot}`} className="aspect-square w-full" aria-hidden />)}
                </ol>
              </li>
            )
          })}
        </ol>
      </div>
    </div>
  )
}

function YearFact({ label, value, detail, className }: { label: string; value?: string; detail?: string; className?: string }) {
  return (
    <div className={cn("min-w-0 border-l-2 pl-3", className)} style={{ borderLeftColor: "var(--color-heat)" }}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-lg font-semibold tabular-nums sm:text-xl"><TruncatedText wrap className="block sm:truncate">{value ?? "–"}</TruncatedText></dd>
      {/* In the one column next to the calendar, a long detail goes to a second line. */}
      {detail && <dd className="text-sm text-muted-foreground"><TruncatedText className="block @[68rem]/year:whitespace-normal">{detail}</TruncatedText></dd>}
    </div>
  )
}
