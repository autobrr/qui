/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ChartContainer, ChartLegend, ChartTooltip, type ChartConfig } from "@/components/ui/chart"
import { useBucketFormat } from "@/components/statistics/useBucketFormat"
import { useTapTooltip } from "@/components/statistics/useTapTooltip"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import { EVENTS, type EventDay, type EventKind } from "@/lib/statistics-events"
import { useMemo } from "react"
import { useTranslation } from "react-i18next"
import { Bar, BarChart, CartesianGrid, ReferenceLine, XAxis, YAxis } from "recharts"

const LABELS = {
  added: { labelKey: "events.added" },
  completed: { labelKey: "events.completed" },
  deleted: { labelKey: "events.deleted" },
} as const satisfies Record<EventKind, { labelKey: string }>

interface EventsChartProps {
  days: EventDay[]
}

export function EventsChart({ days }: EventsChartProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { formatStart, formatTick } = useBucketFormat()
  const formatCount = useMemo(() => new Intl.NumberFormat(locale), [locale])
  const tooltip = useTapTooltip()

  const colors = useSeriesColors()
  const config = useMemo<ChartConfig>(() => Object.fromEntries(EVENTS.map((kind, index) => [
    kind,
    { label: t(LABELS[kind].labelKey), theme: { light: colors.light[index], dark: colors.dark[index] } },
  ])), [t, colors])
  const rows = useMemo(() => days.map(day => ({ ...day, deleted: -day.deleted })), [days])
  const totals = useMemo(() => Object.fromEntries(EVENTS.map(kind => [kind, days.reduce((sum, day) => sum + day[kind], 0)])) as Record<EventKind, number>, [days])

  return (
    <ChartContainer {...tooltip.wrapper} config={config} className="aspect-auto h-[240px] w-full">
      <BarChart data={rows} stackOffset="sign" margin={{ top: 8, right: 8, bottom: 0, left: 0 }} barCategoryGap={rows.length > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} />
        <XAxis dataKey="start" tickLine={false} axisLine={false} minTickGap={48} tickFormatter={value => formatTick(Number(value))} />
        <YAxis tickLine={false} axisLine={false} width={40} allowDecimals={false} tickFormatter={value => formatCount.format(Math.abs(Number(value)))} />
        <ReferenceLine y={0} stroke="var(--border)" />
        <ChartTooltip
          {...tooltip.tooltip}
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={({ active, payload }) => {
            const row = active ? (payload?.[0]?.payload as EventDay | undefined) : undefined
            if (!row) {
              return null
            }
            return (
              <div className="min-w-40 rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
                <div className="mb-1.5 font-medium">{formatStart(row.start, "day")}</div>
                <div className="space-y-1">
                  {EVENTS.map(kind => (
                    <div key={kind} className="flex items-center gap-2">
                      <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${kind})` }} />
                      <span className="text-muted-foreground">{config[kind]?.label}</span>
                      <span className="ml-auto pl-4 font-medium tabular-nums">{formatCount.format(Math.abs(row[kind]))}</span>
                    </div>
                  ))}
                </div>
              </div>
            )
          }}
        />
        {EVENTS.map(kind => (
          <Bar
            key={kind}
            dataKey={kind}
            stackId={kind === "completed" ? "completed" : "library"}
            fill={`var(--color-${kind})`}
            radius={kind === "deleted" ? [0, 0, 2, 2] : [2, 2, 0, 0]}
            maxBarSize={64}
            isAnimationActive={false}
          />
        ))}
        <ChartLegend
          content={() => (
            <div className="flex flex-wrap gap-x-4 gap-y-1 pt-3 text-xs text-muted-foreground">
              {EVENTS.map(kind => (
                <span key={kind} className="inline-flex items-center gap-1.5">
                  <span className="size-2 shrink-0 rounded-[2px]" style={{ background: `var(--color-${kind})` }} />
                  {config[kind]?.label}
                  <span className="font-medium tabular-nums text-foreground">{formatCount.format(totals[kind])}</span>
                </span>
              ))}
            </div>
          )}
        />
      </BarChart>
    </ChartContainer>
  )
}
