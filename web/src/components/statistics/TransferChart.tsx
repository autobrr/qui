/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartLegend, ChartTooltip, type ChartConfig } from "@/components/ui/chart"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { byteTicks, foldOther, formatGapLength, labelledGaps, type BarWidth, type ChartUnit, type Metric, type StatisticsBar } from "@/lib/statistics"
import { DayBands } from "@/components/statistics/DayBands"
import { useHourAxis } from "@/components/statistics/useHourAxis"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import { useTapTooltip } from "@/components/statistics/useTapTooltip"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import { useTrackerGroupIcon } from "@/hooks/useTrackerGroupName"
import { formatBytes } from "@/lib/utils"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { Bar, BarChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from "recharts"

const OTHER = "other"

interface ChartRow {
  start: number
  width: BarWidth
  gapSeconds: number
  total: number
  [series: string]: number | string
}

interface TransferChartProps {
  bars: StatisticsBar[]
  groups: string[]
  metric: Metric
  unit: ChartUnit
  displayName: (group: string) => string
}

/** Stacked bars of the metric, one series for each of the top Tracker groups, and Other for the rest. */
export function TransferChart({ bars, groups, metric, unit, displayName }: TransferChartProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatStart, formatTick, formatHour } = useBucketFormat()
  const colors = useSeriesColors()
  const tooltip = useTapTooltip()
  const narrow = useIsPhone()
  const groupIcon = useTrackerGroupIcon()

  const config = useMemo<ChartConfig>(() => {
    const entries: ChartConfig = {}
    groups.forEach((group, index) => {
      entries[`s${index}`] = { label: displayName(group), theme: { light: colors.light[index], dark: colors.dark[index] } }
    })
    entries[OTHER] = { label: t("chart.other"), color: "var(--muted-foreground)" }
    return entries
  }, [groups, displayName, t, colors])

  const rows = useMemo(() => bars.map(bar => {
    const row: ChartRow = { start: bar.start, width: bar.width, gapSeconds: bar.gapSeconds, total: 0, [OTHER]: 0 }
    groups.forEach((_, index) => {
      row[`s${index}`] = 0
    })
    for (const [group, bytes] of bar.bytes) {
      const index = groups.indexOf(group)
      const key = index >= 0 ? `s${index}` : OTHER
      row[key] = (row[key] as number) + bytes[metric]
      row.total += bytes[metric]
    }
    return row
  }), [bars, groups, metric])

  const widths = useMemo(() => new Map(rows.map(row => [row.start, row.width])), [rows])
  const gapLabels = useMemo(() => labelledGaps(rows.flatMap((row, index) => row.gapSeconds > 0 ? [index] : []), rows.length), [rows])
  const hourAxis = useHourAxis(rows, unit)
  const ticks = useMemo(() => byteTicks(Math.max(0, ...rows.map(row => row.total))), [rows])
  const otherGroups = useMemo(() => {
    const totals = new Map<string, number>()
    for (const bar of bars) {
      for (const [group, bytes] of bar.bytes) {
        totals.set(group, (totals.get(group) ?? 0) + bytes[metric])
      }
    }
    return foldOther(totals, groups)?.groups ?? 0
  }, [bars, groups, metric])
  const hasOther = otherGroups > 0
  const keys = [...groups.map((_, index) => `s${index}`), ...(hasOther ? [OTHER] : [])]
  const icons = useMemo(() => new Map(groups.map((group, index) => [`s${index}`, groupIcon(group)])), [groups, groupIcon])
  const iconOf = (key: string, keepWidth: boolean) => {
    const icon = icons.get(key)
    return icon ? <TrackerIconImage {...icon} /> : keepWidth && <span className="size-4 shrink-0" />
  }

  return (
    <ChartContainer {...tooltip.wrapper} config={config} className="aspect-auto h-[260px] w-full sm:h-[300px] sideways:h-[220px]">
      <BarChart data={rows} margin={{ top: 16, right: narrow ? 4 : 24, bottom: 0, left: 0 }} barCategoryGap={rows.length > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} />
        {hourAxis ? (
          <XAxis dataKey="start" tickLine={false} axisLine={false} ticks={hourAxis.ticks} interval={0} tickFormatter={value => formatHour(Number(value))} />
        ) : (
          <XAxis dataKey="start" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={value => formatTick(Number(value), widths.get(Number(value)))} />
        )}
        {hourAxis && <DayBands axis={hourAxis} />}
        <YAxis tickLine={false} axisLine={false} width={narrow ? "auto" : 72} ticks={ticks} domain={[0, ticks[ticks.length - 1]]} tickFormatter={value => formatBytes(Number(value))} />
        {rows.map((row, index) => row.gapSeconds > 0 && (
          <ReferenceLine
            key={row.start}
            x={row.start}
            stroke="#eab308"
            strokeDasharray="2 3"
            label={gapLabels.has(index) ? { value: t("chart.gapLabel", { duration: formatGapLength(row.gapSeconds, locale) }), position: "top", fill: "var(--muted-foreground)", fontSize: 10 } : undefined}
          />
        ))}
        <ChartTooltip
          {...tooltip.tooltip}
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={({ active, payload }) => {
            const row = active ? (payload?.[0]?.payload as ChartRow | undefined) : undefined
            if (!row) {
              return null
            }
            return (
              <div className="min-w-48 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md max-sm:max-w-[calc(100vw-4rem)]">
                <div className="mb-1.5 font-medium">
                  {formatStart(row.start, row.width)}
                  {row.width === "day" && unit === "hour" && <span className="ml-1 font-normal text-muted-foreground">{t("chart.foldedDay")}</span>}
                </div>
                <div className="space-y-1">
                  {keys.map(key => (
                    <div key={key} className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})` }} />
                      {iconOf(key, true)}
                      <span className="min-w-0 truncate text-muted-foreground">{config[key]?.label}</span>
                      <span className="ml-auto shrink-0 whitespace-nowrap pl-4 font-medium tabular-nums">{formatBytes(row[key] as number)}</span>
                    </div>
                  ))}
                  <div className="flex items-center gap-2 border-t pt-1">
                    <span className="text-muted-foreground">{t("chart.total")}</span>
                    <span className="ml-auto pl-4 font-medium tabular-nums">{formatBytes(row.total)}</span>
                  </div>
                  {row.gapSeconds > 0 && (
                    <div className="pt-1 text-yellow-600 dark:text-yellow-500">{t("chart.gap", { duration: formatGapLength(row.gapSeconds, locale) })}</div>
                  )}
                </div>
              </div>
            )
          }}
        />
        {keys.map((key, index) => (
          <Bar
            key={key}
            dataKey={key}
            stackId="transfer"
            fill={`var(--color-${key})`}
            fillOpacity={key === OTHER ? 0.45 : 1}
            radius={index === keys.length - 1 ? [2, 2, 0, 0] : 0}
            maxBarSize={64}
            isAnimationActive={false}
          />
        ))}
        <ChartLegend
          content={() => (
            <div className="flex flex-wrap gap-x-4 gap-y-1 pt-3 text-xs text-muted-foreground">
              {keys.map(key => (
                <span key={key} className="inline-flex items-center gap-1.5">
                  <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})`, opacity: key === OTHER ? 0.45 : 1 }} />
                  {iconOf(key, false)}
                  {key === OTHER ? t("chart.otherCount", { groups: otherGroups }) : config[key]?.label}
                </span>
              ))}
            </div>
          )}
        />
      </BarChart>
    </ChartContainer>
  )
}
