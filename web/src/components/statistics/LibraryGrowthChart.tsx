/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartLegend, ChartTooltip, type ChartConfig } from "@/components/ui/chart"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import type { LibraryPoint } from "@/lib/statistics-library"
import { byteTicks } from "@/lib/statistics"
import { formatBytes } from "@/lib/utils"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts"

const SERIES = [
  { key: "totalSize", color: 0 },
  { key: "uniqueSize", color: 2 },
] as const

function isolated(points: LibraryPoint[], key: "totalSize" | "uniqueSize", index: number): boolean {
  return points[index]?.[key] !== null && (points[index - 1]?.[key] ?? null) === null && (points[index + 1]?.[key] ?? null) === null
}

/** The total size and the Unique size for each day. A day with no value shows as a gap in its line. */
export function LibraryGrowthChart({ points }: { points: LibraryPoint[] }) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatStart, formatTick } = useBucketFormat("day")
  const colors = useSeriesColors()

  const config = useMemo<ChartConfig>(() => {
    const labels = { totalSize: t("growth.totalSize"), uniqueSize: t("growth.uniqueSize") }
    return Object.fromEntries(SERIES.map(({ key, color }) => [key, { label: labels[key], theme: { light: colors.light[color], dark: colors.dark[color] } }]))
  }, [t, colors])
  const ticks = useMemo(() => byteTicks(Math.max(0, ...points.map(point => point.totalSize ?? 0))), [points])

  return (
    <ChartContainer config={config} className="aspect-auto h-[240px] w-full">
      <AreaChart data={points} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="start" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={value => formatTick(Number(value))} />
        <YAxis tickLine={false} axisLine={false} width={72} ticks={ticks} domain={[0, ticks[ticks.length - 1]]} tickFormatter={value => formatBytes(Number(value))} />
        <ChartTooltip
          cursor={{ stroke: "var(--border)" }}
          filterNull={false}
          isAnimationActive={false}
          content={({ active, payload }) => {
            const point = active ? (payload?.[0]?.payload as LibraryPoint | undefined) : undefined
            if (!point) {
              return null
            }
            const size = (bytes: number | null) => bytes === null ? t("growth.noValue") : formatBytes(bytes)
            return (
              <div className="min-w-48 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
                <div className="mb-1.5 font-medium">{formatStart(point.start, "day")}</div>
                <div className="space-y-1">
                  {SERIES.map(({ key }) => (
                    <div key={key} className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})` }} />
                      <span className="text-muted-foreground">{config[key]?.label}</span>
                      <span className="ml-auto pl-4 font-medium tabular-nums">{size(point[key])}</span>
                    </div>
                  ))}
                  <div className="flex items-center gap-2 border-t pt-1">
                    <span className="text-muted-foreground">{t("growth.torrents")}</span>
                    <span className="ml-auto pl-4 font-medium tabular-nums">{point.torrents === null ? t("growth.noValue") : point.torrents.toLocaleString(locale)}</span>
                  </div>
                </div>
              </div>
            )
          }}
        />
        {SERIES.map(({ key }) => (
          <Area
            key={key}
            dataKey={key}
            stroke={`var(--color-${key})`}
            strokeWidth={2}
            fill={`var(--color-${key})`}
            fillOpacity={0.1}
            dot={({ index, cx, cy }: { index: number; cx?: number; cy?: number }) => (
              // A day between two gaps has no line, so it gets a dot.
              <circle key={index} cx={cx} cy={cy} r={isolated(points, key, index) && cy !== undefined ? 3 : 0} fill={`var(--color-${key})`} />
            )}
            isAnimationActive={false}
          />
        ))}
        <ChartLegend
          content={() => (
            <div className="flex flex-wrap gap-x-4 gap-y-1 pt-3 text-xs text-muted-foreground">
              {SERIES.map(({ key }) => (
                <span key={key} className="inline-flex items-center gap-1.5">
                  <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${key})` }} />
                  {config[key]?.label}
                </span>
              ))}
            </div>
          )}
        />
      </AreaChart>
    </ChartContainer>
  )
}
