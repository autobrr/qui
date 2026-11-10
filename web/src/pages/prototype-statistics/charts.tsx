/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: chart primitives shared by the Statistics page variants (issue #3991).

import { cn, formatBytes } from "@/lib/utils"
import type { ReactNode } from "react"
import { Area, AreaChart, Bar, BarChart, CartesianGrid, Line, LineChart, ReferenceArea, ReferenceLine, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts"
import type { Bucket, GroupRow, LibraryPoint, Stats } from "./mock-data"
import { SERIES_COUNT, sumOf } from "./mock-data"

export type Palette = "categorical" | "theme"

const CATEGORICAL =
  "[--series-1:#2a78d6] [--series-2:#eb6834] [--series-3:#1baf7a] [--series-4:#eda100] [--series-5:#e87ba4] " +
  "dark:[--series-1:#3987e5] dark:[--series-2:#d95926] dark:[--series-3:#199e70] dark:[--series-4:#c98500] dark:[--series-5:#d55181]"
const THEME =
  "[--series-1:var(--chart-1)] [--series-2:var(--chart-2)] [--series-3:var(--chart-3)] [--series-4:var(--chart-4)] [--series-5:var(--chart-5)]"

export function paletteClass(palette: Palette): string {
  return cn("[--series-other:var(--muted-foreground)]", palette === "theme" ? THEME : CATEGORICAL)
}

export function seriesColor(index: number): string {
  return index >= 0 && index < SERIES_COUNT ? `var(--series-${index + 1})` : "var(--series-other)"
}

export function formatBucket(t: number, unit: Stats["unit"], span: number): string {
  const d = new Date(t)
  if (unit === "hour") {
    const time = d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
    return span > 24 ? `${d.toLocaleDateString(undefined, { weekday: "short" })} ${time}` : time
  }
  return d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
}

function axisTick(t: number, unit: Stats["unit"], span: number): string {
  const d = new Date(t)
  if (unit === "hour") {
    return span > 24 ? d.toLocaleDateString(undefined, { weekday: "short", day: "numeric" }) : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
  }
  return span > 120 ? d.toLocaleDateString(undefined, { month: "short" }) : d.toLocaleDateString(undefined, { day: "numeric", month: "short" })
}

interface TipProps {
  active?: boolean
  payload?: ReadonlyArray<{ payload?: unknown }>
}

function TipShell({ title, children }: { title: string; children: ReactNode }) {
  return (
    <div className="rounded-md border bg-popover px-3 py-2 text-xs text-popover-foreground shadow-md">
      <div className="mb-1.5 font-medium">{title}</div>
      <div className="space-y-1">{children}</div>
    </div>
  )
}

function TipRow({ color, label, value }: { color?: string; label: string; value: string }) {
  return (
    <div className="flex items-center gap-2">
      {color && <span className="size-2 shrink-0 rounded-[2px]" style={{ background: color }} />}
      <span className="text-muted-foreground">{label}</span>
      <span className="ml-auto pl-4 font-medium tabular-nums">{value}</span>
    </div>
  )
}

interface StackRow {
  t: number
  other: number
  total: number
  gapHours?: number
  recorded: boolean
  [series: `s${number}`]: number
}

function toStackRows(buckets: Bucket[], metric: "up" | "down"): StackRow[] {
  return buckets.map(b => {
    const values = b[metric]
    const row: StackRow = { t: b.t, other: sumOf(values.slice(SERIES_COUNT)), total: sumOf(values), gapHours: b.gapHours, recorded: b.recorded }
    for (let i = 0; i < SERIES_COUNT; i++) row[`s${i}`] = values[i]
    return row
  })
}

const AXIS_TICK = { fontSize: 11, fill: "var(--muted-foreground)" }

function byteStep(max: number, count = 4): number {
  if (max <= 0) return 1
  const unit = 1024 ** Math.floor(Math.log(max) / Math.log(1024))
  const raw = max / unit / count
  const pow = 10 ** Math.floor(Math.log10(raw))
  return ([1, 2, 2.5, 5, 10].map(m => m * pow).find(m => m >= raw) ?? raw) * unit
}

/** Round axis ticks in the byte unit of the largest value, from -below to above. */
function byteTicks(above: number, below = 0): number[] {
  const step = byteStep(Math.max(above, below))
  const ticks: number[] = []
  for (let v = -Math.ceil(below / step) * step; v < above + step; v += step) ticks.push(v)
  return ticks
}

interface StackedBarsProps {
  stats: Stats
  metric: "up" | "down"
  height?: number
  /** Show the series of one ranked group only, in its own color. */
  only?: number
  hideAxis?: boolean
}

export function StackedBars({ stats, metric, height = 300, only, hideAxis = false }: StackedBarsProps) {
  const rows = toStackRows(stats.buckets, metric)
  const span = rows.length
  const unrecorded = rows.filter(r => !r.recorded)
  const gaps = rows.filter(r => r.gapHours)
  const keys = only === undefined ? [...Array.from({ length: SERIES_COUNT }, (_, i) => `s${i}`), "other"] : [`s${only}`]
  const ticks = byteTicks(Math.max(...rows.map(r => (only === undefined ? r.total : r[`s${only}`]))))

  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} margin={{ top: 16, right: 4, bottom: 0, left: 0 }} barCategoryGap={span > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" hide={hideAxis} tickLine={false} axisLine={{ stroke: "var(--border)" }} tick={AXIS_TICK} minTickGap={48} tickFormatter={t => axisTick(Number(t), stats.unit, span)} />
        <YAxis hide={hideAxis} tickLine={false} axisLine={false} tick={AXIS_TICK} width={72} ticks={ticks} domain={[0, ticks[ticks.length - 1]]} tickFormatter={v => formatBytes(Number(v))} />
        {unrecorded.length > 1 && (
          <ReferenceArea x1={unrecorded[0].t} x2={unrecorded[unrecorded.length - 1].t} fill="var(--muted)" fillOpacity={0.55} label={{ value: "Not recorded", fill: "var(--muted-foreground)", fontSize: 11 }} />
        )}
        {gaps.map(g => (
          <ReferenceLine key={g.t} x={g.t} stroke="var(--color-yellow-500, #eab308)" strokeDasharray="2 3" label={hideAxis ? undefined : { value: `gap ${g.gapHours}h`, position: "top", fill: "var(--muted-foreground)", fontSize: 10 }} />
        ))}
        <Tooltip
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as StackRow | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={formatBucket(row.t, stats.unit, span)}>
                {only === undefined ? (
                  <>
                    {stats.groups.slice(0, SERIES_COUNT).map((g, i) => (
                      <TipRow key={g.key} color={seriesColor(i)} label={g.name} value={formatBytes(row[`s${i}`])} />
                    ))}
                    <TipRow color={seriesColor(-1)} label="Other" value={formatBytes(row.other)} />
                    <div className="mt-1 border-t pt-1">
                      <TipRow label="Total" value={formatBytes(row.total)} />
                    </div>
                  </>
                ) : (
                  <TipRow color={seriesColor(only)} label={stats.groups[only].name} value={formatBytes(row[`s${only}`])} />
                )}
                {row.gapHours && <div className="pt-1 text-yellow-600 dark:text-yellow-500">Includes a recording gap of {row.gapHours} h</div>}
              </TipShell>
            )
          }}
        />
        {keys.map((key, i) => (
          <Bar
            key={key}
            dataKey={key}
            stackId="a"
            fill={key === "other" ? seriesColor(-1) : seriesColor(only ?? i)}
            fillOpacity={key === "other" ? 0.45 : 1}
            radius={i === keys.length - 1 ? [2, 2, 0, 0] : 0}
            maxBarSize={24}
            isAnimationActive={false}
          />
        ))}
      </BarChart>
    </ResponsiveContainer>
  )
}

export function SeriesLegend({ groups, className }: { groups: GroupRow[]; className?: string }) {
  return (
    <div className={cn("flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground", className)}>
      {groups.slice(0, SERIES_COUNT).map((g, i) => (
        <span key={g.key} className="inline-flex items-center gap-1.5">
          <span className="size-2 rounded-[2px]" style={{ background: seriesColor(i) }} />
          {g.name}
        </span>
      ))}
      {groups.length > SERIES_COUNT && (
        <span className="inline-flex items-center gap-1.5">
          <span className="size-2 rounded-[2px] opacity-45" style={{ background: seriesColor(-1) }} />
          Other ({groups.length - SERIES_COUNT})
        </span>
      )}
    </div>
  )
}

/** Upload above the baseline, download below it. One unit, one axis. */
export function UpDownBars({ stats, height = 140 }: { stats: Stats; height?: number }) {
  const span = stats.buckets.length
  const rows = stats.buckets.map(b => ({ t: b.t, up: sumOf(b.up), down: -sumOf(b.down), gapHours: b.gapHours }))
  const ticks = byteTicks(Math.max(...rows.map(r => r.up)), Math.max(...rows.map(r => -r.down)))
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} stackOffset="sign" margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap={span > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" tickLine={false} axisLine={false} tick={AXIS_TICK} minTickGap={48} tickFormatter={t => axisTick(Number(t), stats.unit, span)} />
        <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={72} ticks={ticks} domain={[ticks[0], ticks[ticks.length - 1]]} tickFormatter={v => formatBytes(Math.abs(Number(v)))} />
        <ReferenceLine y={0} stroke="var(--border)" />
        <Tooltip
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as (typeof rows)[number] | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={formatBucket(row.t, stats.unit, span)}>
                <TipRow color="var(--series-1)" label="Uploaded" value={formatBytes(row.up)} />
                <TipRow color="var(--series-2)" label="Downloaded" value={formatBytes(-row.down)} />
                {row.gapHours && <div className="pt-1 text-yellow-600 dark:text-yellow-500">Includes a recording gap of {row.gapHours} h</div>}
              </TipShell>
            )
          }}
        />
        <Bar dataKey="up" stackId="a" fill="var(--series-1)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
        <Bar dataKey="down" stackId="a" fill="var(--series-2)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  )
}

/** One group: upload above the baseline in its own color, download below it in gray. */
export function GroupBars({ stats, points, color, height = 200 }: { stats: Stats; points: { t: number; up: number; down: number; gapHours?: number }[]; color: string; height?: number }) {
  const span = points.length
  const rows = points.map(p => ({ ...p, down: -p.down }))
  const ticks = byteTicks(Math.max(...rows.map(r => r.up)), Math.max(...rows.map(r => -r.down)))
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} stackOffset="sign" margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap={span > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" tickLine={false} axisLine={false} tick={AXIS_TICK} minTickGap={48} tickFormatter={t => axisTick(Number(t), stats.unit, span)} />
        <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={72} ticks={ticks} domain={[ticks[0], ticks[ticks.length - 1]]} tickFormatter={v => formatBytes(Math.abs(Number(v)))} />
        <ReferenceLine y={0} stroke="var(--border)" />
        <Tooltip
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as (typeof rows)[number] | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={formatBucket(row.t, stats.unit, span)}>
                <TipRow color={color} label="Uploaded" value={formatBytes(row.up)} />
                <TipRow color="var(--muted-foreground)" label="Downloaded" value={formatBytes(-row.down)} />
                {row.gapHours && <div className="pt-1 text-yellow-600 dark:text-yellow-500">Includes a recording gap of {row.gapHours} h</div>}
              </TipShell>
            )
          }}
        />
        <Bar dataKey="up" stackId="a" fill={color} radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
        <Bar dataKey="down" stackId="a" fill="var(--muted-foreground)" fillOpacity={0.5} radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  )
}

/** Adds above the baseline, deletes below it. */
export function ChurnBars({ stats, height = 200 }: { stats: Stats; height?: number }) {
  const span = stats.buckets.length
  const rows = stats.buckets.map(b => ({ t: b.t, added: b.added, completed: b.completed, deleted: -b.deleted }))
  return (
    <ResponsiveContainer width="100%" height={height}>
      <BarChart data={rows} stackOffset="sign" margin={{ top: 4, right: 4, bottom: 0, left: 0 }} barCategoryGap={span > 60 ? 0.5 : "18%"}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" tickLine={false} axisLine={false} tick={AXIS_TICK} minTickGap={48} tickFormatter={t => axisTick(Number(t), stats.unit, span)} />
        <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={36} allowDecimals={false} tickFormatter={v => String(Math.abs(Number(v)))} />
        <ReferenceLine y={0} stroke="var(--border)" />
        <Tooltip
          cursor={{ fill: "var(--muted)", opacity: 0.5 }}
          isAnimationActive={false}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as (typeof rows)[number] | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={formatBucket(row.t, stats.unit, span)}>
                <TipRow color="var(--series-1)" label="Added" value={String(row.added)} />
                <TipRow label="Completed" value={String(row.completed)} />
                <TipRow color="var(--series-2)" label="Deleted" value={String(-row.deleted)} />
              </TipShell>
            )
          }}
        />
        <Bar dataKey="added" stackId="a" fill="var(--series-1)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
        <Bar dataKey="deleted" stackId="a" fill="var(--series-2)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
      </BarChart>
    </ResponsiveContainer>
  )
}

function dayLabel(t: number): string {
  return new Date(t).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
}

/** Total size and Unique size share one byte axis. */
export function LibrarySizeChart({ points, height = 200 }: { points: LibraryPoint[]; height?: number }) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      <AreaChart data={points} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" tickLine={false} axisLine={false} tick={AXIS_TICK} minTickGap={56} tickFormatter={t => axisTick(Number(t), "day", points.length)} />
        <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={72} domain={["auto", "auto"]} tickFormatter={v => formatBytes(Number(v))} />
        <Tooltip
          isAnimationActive={false}
          cursor={{ stroke: "var(--border)" }}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as LibraryPoint | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={dayLabel(row.t)}>
                <TipRow color="var(--series-1)" label="Total size" value={formatBytes(row.totalSize)} />
                <TipRow color="var(--series-3)" label="Unique size" value={formatBytes(row.uniqueSize)} />
                <TipRow label="Linked copies" value={formatBytes(row.totalSize - row.uniqueSize)} />
              </TipShell>
            )
          }}
        />
        <Area dataKey="totalSize" stroke="var(--series-1)" strokeWidth={2} fill="var(--series-1)" fillOpacity={0.1} isAnimationActive={false} />
        <Area dataKey="uniqueSize" stroke="var(--series-3)" strokeWidth={2} fill="var(--series-3)" fillOpacity={0.1} isAnimationActive={false} />
      </AreaChart>
    </ResponsiveContainer>
  )
}

export function LibraryLine({ points, field, label, format, height = 160 }: { points: LibraryPoint[]; field: "torrents" | "freeSpace"; label: string; format: (v: number) => string; height?: number }) {
  return (
    <ResponsiveContainer width="100%" height={height}>
      <LineChart data={points} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
        <CartesianGrid vertical={false} stroke="var(--border)" />
        <XAxis dataKey="t" tickLine={false} axisLine={false} tick={AXIS_TICK} minTickGap={56} tickFormatter={t => axisTick(Number(t), "day", points.length)} />
        <YAxis tickLine={false} axisLine={false} tick={AXIS_TICK} width={72} domain={["auto", "auto"]} tickFormatter={v => format(Number(v))} />
        <Tooltip
          isAnimationActive={false}
          cursor={{ stroke: "var(--border)" }}
          content={(p: TipProps) => {
            const row = p.active ? (p.payload?.[0]?.payload as LibraryPoint | undefined) : undefined
            if (!row) return null
            return (
              <TipShell title={dayLabel(row.t)}>
                <TipRow color="var(--series-1)" label={label} value={format(row[field])} />
              </TipShell>
            )
          }}
        />
        <Line dataKey={field} stroke="var(--series-1)" strokeWidth={2} dot={false} isAnimationActive={false} />
      </LineChart>
    </ResponsiveContainer>
  )
}

export function Spark({ values, color = "var(--series-1)", className }: { values: number[]; color?: string; className?: string }) {
  const max = Math.max(...values, 1)
  const w = 100
  const h = 24
  const step = w / Math.max(values.length - 1, 1)
  const points = values.map((v, i) => `${(i * step).toFixed(1)},${(h - 1 - (v / max) * (h - 2)).toFixed(1)}`).join(" ")
  return (
    <svg viewBox={`0 0 ${w} ${h}`} preserveAspectRatio="none" className={cn("h-6 w-24", className)} aria-hidden>
      <polygon points={`0,${h} ${points} ${w},${h}`} fill={color} opacity={0.12} />
      <polyline points={points} fill="none" stroke={color} strokeWidth={1.5} strokeLinejoin="round" vectorEffect="non-scaling-stroke" />
    </svg>
  )
}

export function ShareBar({ value, max, color }: { value: number; max: number; color: string }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
      <div className="h-full rounded-full" style={{ width: `${Math.max(1, (value / max) * 100)}%`, background: color }} />
    </div>
  )
}
