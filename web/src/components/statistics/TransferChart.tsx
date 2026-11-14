/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartLegend, ChartTooltip, type ChartConfig } from "@/components/ui/chart"
import { byteTicks, formatGapLength, labelledGaps, SERIES_COLORS, type BarWidth, type ChartUnit, type Metric, type StatisticsBar } from "@/lib/statistics"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
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
  const { formatStart, formatTick } = useBucketFormat(unit)

  const config = useMemo<ChartConfig>(() => {
    const entries: ChartConfig = {}
    groups.forEach((group, index) => {
      entries[`s${index}`] = { label: displayName(group), theme: { light: SERIES_COLORS.light[index], dark: SERIES_COLORS.dark[index] } }
    })
    entries[OTHER] = { label: t("chart.other"), color: "var(--muted-foreground)" }
    return entries
  }, [groups, displayName, t])

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

  const gapLabels = useMemo(() => labelledGaps(rows.flatMap((row, index) => row.gapSeconds > 0 ? [index] : []), rows.length), [rows])
  const ticks = useMemo(() => byteTicks(Math.max(0, ...rows.map(row => row.total))), [rows])
  const otherGroups = useMemo(() => new Set(bars.flatMap(bar => [...bar.bytes].filter(([group, bytes]) => bytes[metric] > 0 && !groups.includes(group)).map(([group]) => group))).size, [bars, groups, metric])
  const hasOther = otherGroups > 0
  const keys = [...groups.map((_, index) => `s${index}`), ...(hasOther ? [OTHER] : [])]

  return (
    <ChartContainer config={config} className="aspect-auto h-[300px] w-full">
      <BarChart data={rows} margin={{ top: 16, right: 24, bottom: 0, left: 0 }} barCategoryGap={rows.length > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="start" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={value => formatTick(Number(value))} />
        <YAxis tickLine={false} axisLine={false} width={72} ticks={ticks} domain={[0, ticks[ticks.length - 1]]} tickFormatter={value => formatBytes(Number(value))} />
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
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={({ active, payload }) => {
            const row = active ? (payload?.[0]?.payload as ChartRow | undefined) : undefined
            if (!row) {
              return null
            }
            return (
              <div className="min-w-48 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
                <div className="mb-1.5 font-medium">
                  {formatStart(row.start, row.width)}
                  {row.width === "day" && unit === "hour" && <span className="ml-1 font-normal text-muted-foreground">{t("chart.foldedDay")}</span>}
                </div>
                <div className="space-y-1">
                  {keys.map(key => (
                    <div key={key} className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})` }} />
                      <span className="truncate text-muted-foreground">{config[key]?.label}</span>
                      <span className="ml-auto pl-4 font-medium tabular-nums">{formatBytes(row[key] as number)}</span>
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
            maxBarSize={24}
            isAnimationActive={false}
          />
        ))}
        <ChartLegend
          content={() => (
            <div className="flex flex-wrap gap-x-4 gap-y-1 pt-3 text-xs text-muted-foreground">
              {keys.map(key => (
                <span key={key} className="inline-flex items-center gap-1.5">
                  <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})`, opacity: key === OTHER ? 0.45 : 1 }} />
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
