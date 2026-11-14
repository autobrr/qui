/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartTooltip, type ChartConfig } from "@/components/ui/chart"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import { barBytes, byteTicks, formatGapLength, type BarWidth, type ChartUnit, type StatisticsBar } from "@/lib/statistics"
import { formatBytes } from "@/lib/utils"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { Bar, BarChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from "recharts"

interface ChartRow {
  start: number
  width: BarWidth
  gapSeconds: number
  uploaded: number
  // Negative, so that the bar hangs below the zero line.
  downloaded: number
}

interface GroupChartProps {
  bars: StatisticsBar[]
  unit: ChartUnit
  // The theme colors of the upload series.
  color: { light: string; dark: string } | undefined
}

/** The upload of one group above the zero line and its download below it. */
export function GroupChart({ bars, unit, color }: GroupChartProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatStart, formatTick } = useBucketFormat(unit)

  const config = useMemo<ChartConfig>(() => ({
    uploaded: color ? { label: t("chart.upload"), theme: color } : { label: t("chart.upload"), color: "var(--foreground)" },
    downloaded: { label: t("chart.download"), color: "var(--muted-foreground)" },
  }), [color, t])

  const rows = useMemo(() => bars.map((bar): ChartRow => {
    const { uploaded, downloaded } = barBytes(bar)
    return { start: bar.start, width: bar.width, gapSeconds: bar.gapSeconds, uploaded, downloaded: -downloaded }
  }), [bars])

  const widths = useMemo(() => new Map(rows.map(row => [row.start, row.width])), [rows])

  const ticks = useMemo(() => {
    const up = byteTicks(Math.max(0, ...rows.map(row => row.uploaded)), 2)
    const down = byteTicks(Math.max(0, ...rows.map(row => -row.downloaded)), 2)
    return [...down.slice(1).map(value => -value).reverse(), ...up]
  }, [rows])

  return (
    <ChartContainer config={config} className="aspect-auto h-[200px] w-full">
      <BarChart data={rows} stackOffset="sign" margin={{ top: 8, right: 8, bottom: 0, left: 0 }} barCategoryGap={rows.length > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="start" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={value => formatTick(Number(value), widths.get(Number(value)))} />
        <YAxis tickLine={false} axisLine={false} width={72} ticks={ticks} domain={[ticks[0], ticks[ticks.length - 1]]} tickFormatter={value => formatBytes(Math.abs(Number(value)))} />
        <ReferenceLine y={0} stroke="var(--border)" />
        {rows.map(row => row.gapSeconds > 0 && (
          <ReferenceLine key={row.start} x={row.start} stroke="#eab308" strokeDasharray="2 3" />
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
              <div className="min-w-44 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
                <div className="mb-1.5 font-medium">
                  {formatStart(row.start, row.width)}
                  {row.width === "day" && unit === "hour" && <span className="ml-1 font-normal text-muted-foreground">{t("chart.foldedDay")}</span>}
                </div>
                <div className="space-y-1">
                  {(["uploaded", "downloaded"] as const).map(key => (
                    <div key={key} className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})` }} />
                      <span className="text-muted-foreground">{config[key]?.label}</span>
                      <span className="ml-auto pl-4 font-medium tabular-nums">{formatBytes(Math.abs(row[key]))}</span>
                    </div>
                  ))}
                  {row.gapSeconds > 0 && (
                    <div className="pt-1 text-yellow-600 dark:text-yellow-500">{t("chart.gap", { duration: formatGapLength(row.gapSeconds, locale) })}</div>
                  )}
                </div>
              </div>
            )
          }}
        />
        <Bar dataKey="uploaded" stackId="group" fill="var(--color-uploaded)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
        <Bar dataKey="downloaded" stackId="group" fill="var(--color-downloaded)" fillOpacity={0.5} radius={[0, 0, 2, 2]} maxBarSize={24} isAnimationActive={false} />
      </BarChart>
    </ChartContainer>
  )
}
