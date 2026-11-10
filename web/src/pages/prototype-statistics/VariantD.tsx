/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant D, "Year in review": a calendar and records lead. The period picker is gone, the year is the unit.

import { cn, formatBytes } from "@/lib/utils"
import { useState } from "react"
import { ShareBar, seriesColor } from "./charts"
import type { VariantProps } from "./controls"
import { InstanceFilter, SeriesDot, TorrentBadges, formatDay, formatRatio } from "./controls"
import type { HeatDay, Stats } from "./mock-data"
import { SERIES_COUNT, getHeatmap, getMonths, sumOf } from "./mock-data"

const HEAT = ["bg-muted", "bg-[#cde2fb] dark:bg-[#184f95]", "bg-[#86b6ef] dark:bg-[#256abf]", "bg-[#3987e5] dark:bg-[#3987e5]", "bg-[#1c5cab] dark:bg-[#86b6ef]"]

function heatStep(value: number, thresholds: number[]): number {
  if (value <= 0) return 0
  return 1 + thresholds.filter(t => value > t).length
}

export function Calendar({ days }: { days: HeatDay[] }) {
  const [hover, setHover] = useState<HeatDay | null>(null)
  const sorted = days.filter(d => d.recorded).map(d => d.value).sort((a, b) => a - b)
  const thresholds = [0.35, 0.7, 0.92].map(q => sorted[Math.floor(sorted.length * q)] ?? 0)
  const weeks: HeatDay[][] = []
  for (let i = 0; i < days.length; i += 7) weeks.push(days.slice(i, i + 7))

  return (
    <div>
      <div className="mb-2 flex h-5 items-center justify-between gap-4 text-xs text-muted-foreground">
        <span>
          {hover ? (
            <>
              <span className="font-medium text-foreground">{formatDay(hover.t)}</span>
              {hover.recorded ? `: ${formatBytes(hover.value)} uploaded` : ": not recorded"}
              {hover.gapHours && `, includes a recording gap of ${hover.gapHours} h`}
            </>
          ) : "Upload per day, last 53 weeks"}
        </span>
        <span className="hidden items-center gap-1 sm:flex">
          Less
          {HEAT.map(c => <span key={c} className={cn("size-3 rounded-[3px]", c)} />)}
          More
        </span>
      </div>
      <div className="overflow-x-auto pb-1">
        <div className="flex gap-[3px]" onMouseLeave={() => setHover(null)}>
          {weeks.map((week, wi) => (
            <div key={week[0].t} className="flex flex-col gap-[3px]">
              <div className="h-4 text-[10px] leading-4 text-muted-foreground">
                {new Date(week[0].t).getDate() <= 7 && (wi === 0 || new Date(week[0].t).getMonth() !== new Date(weeks[wi - 1][0].t).getMonth()) ? new Date(week[0].t).toLocaleDateString(undefined, { month: "short" }) : ""}
              </div>
              {week.map(d => (
                <div
                  key={d.t}
                  onMouseEnter={() => setHover(d)}
                  className={cn(
                    "size-[15px] rounded-[3px]",
                    d.recorded ? HEAT[heatStep(d.value, thresholds)] : "border border-dashed border-border",
                    d.gapHours && "ring-1 ring-yellow-500",
                    hover?.t === d.t && "outline outline-1 outline-foreground"
                  )}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export function Record({ label, value, detail }: { label: string; value: string; detail: string }) {
  return (
    <div className="border-l-2 border-(--series-1) pl-3">
      <div className="text-xs uppercase tracking-wide text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-xl font-semibold" title={value}>{value}</div>
      <div className="truncate text-sm text-muted-foreground" title={detail}>{detail}</div>
    </div>
  )
}

export function MonthBars({ stats }: { stats: Stats }) {
  const months = getMonths(stats)
  const monthMax = Math.max(...months.map(m => sumOf(m.up)))
  return (
    <div className="flex h-56 items-end gap-2 sm:gap-3">
      {months.map(m => {
        const total = sumOf(m.up)
        const other = sumOf(m.up.slice(SERIES_COUNT))
        return (
          <div key={m.t} className="flex h-full min-w-0 flex-1 flex-col justify-end gap-1.5" title={`${new Date(m.t).toLocaleDateString(undefined, { month: "long", year: "numeric" })}: ${m.recorded ? formatBytes(total) : "not recorded"}`}>
            <div className="truncate text-center text-[11px] tabular-nums text-muted-foreground">{m.recorded ? formatBytes(total) : ""}</div>
            {m.recorded ? (
              <div className="flex flex-col-reverse gap-0.5 overflow-hidden rounded-t-[4px]" style={{ height: `${(total / monthMax) * 80}%` }}>
                {m.up.slice(0, SERIES_COUNT).map((v, i) => (
                  <div key={i} style={{ flexGrow: v, background: seriesColor(i) }} />
                ))}
                <div className="opacity-45" style={{ flexGrow: other, background: seriesColor(-1) }} />
              </div>
            ) : (
              <div className="h-3 rounded-t-[4px] border border-dashed border-border" />
            )}
            <div className="text-center text-xs text-muted-foreground">{new Date(m.t).toLocaleDateString(undefined, { month: "short" })}</div>
          </div>
        )
      })}
    </div>
  )
}

export function VariantD({ stats, instance, setInstance }: VariantProps) {
  const days = getHeatmap(instance)
  const months = getMonths(stats)
  const recorded = days.filter(d => d.recorded)
  const best = recorded.reduce((a, b) => (b.value > a.value ? b : a), recorded[0])
  const bestMonth = months.reduce((a, b) => (sumOf(b.up) > sumOf(a.up) ? b : a), months[0])
  const topGroup = stats.groups[0]
  const topTorrent = stats.topTorrents[0]
  const weekend = sumOf(recorded.filter(d => [0, 6].includes(new Date(d.t).getDay())).map(d => d.value))
  const groupMax = stats.groups[0].uploaded

  return (
    <div className="mx-auto max-w-[1100px] space-y-10 p-4 pb-24 lg:p-8 lg:pb-24">
      <div className="flex flex-wrap items-start justify-between gap-4">
        <div>
          <div className="text-sm text-muted-foreground">Statistics · last 12 months</div>
          <h1 className="mt-1 text-5xl font-semibold tracking-tight sm:text-6xl">{formatBytes(stats.totals.uploaded)}</h1>
          <p className="mt-2 max-w-xl text-muted-foreground">
            uploaded since recording started on {formatDay(stats.recordingSince)}. You downloaded {formatBytes(stats.totals.downloaded)}, a ratio of {formatRatio(stats.totals.ratio)}.
          </p>
        </div>
        <InstanceFilter instance={instance} setInstance={setInstance} />
      </div>

      <Calendar days={days} />

      <div className="grid gap-x-6 gap-y-6 sm:grid-cols-2 lg:grid-cols-3">
        <Record label="Best day" value={formatBytes(best.value)} detail={formatDay(best.t)} />
        <Record label="Best month" value={new Date(bestMonth.t).toLocaleDateString(undefined, { month: "long" })} detail={formatBytes(sumOf(bestMonth.up))} />
        <Record label="Top tracker" value={topGroup.name} detail={`${formatBytes(topGroup.uploaded)}, ${Math.round((topGroup.uploaded / stats.totals.uploaded) * 100)}% of the upload`} />
        <Record label="Top torrent" value={topTorrent.name} detail={`${formatBytes(topTorrent.uploaded)} on ${topTorrent.group}`} />
        <Record label="From cross-seeds" value={`${Math.round((stats.crossSeedUploaded / stats.totals.uploaded) * 100)}%`} detail={`${formatBytes(stats.crossSeedUploaded)} from torrents that downloaded nothing`} />
        <Record label="Weekends" value={`${Math.round((weekend / stats.totals.uploaded) * 100)}%`} detail="of the upload moved on a Saturday or a Sunday" />
      </div>

      <div>
        <h2 className="mb-4 text-lg font-semibold">Month by month</h2>
        <MonthBars stats={stats} />
      </div>

      <div className="grid gap-10 lg:grid-cols-2">
        <div>
          <h2 className="mb-4 text-lg font-semibold">Trackers</h2>
          <ol className="space-y-3">
            {stats.groups.slice(0, 8).map(g => (
              <li key={g.key} className="space-y-1">
                <div className="flex items-baseline gap-2 text-sm">
                  <SeriesDot index={g.colorIndex} />
                  <span className="font-medium">{g.name}</span>
                  <span className="text-xs text-muted-foreground">ratio {formatRatio(g.ratio)}</span>
                  <span className="ml-auto font-medium tabular-nums">{formatBytes(g.uploaded)}</span>
                </div>
                <ShareBar value={g.uploaded} max={groupMax} color={seriesColor(g.colorIndex)} />
              </li>
            ))}
          </ol>
        </div>
        <div>
          <h2 className="mb-4 text-lg font-semibold">Torrents</h2>
          <ol className="space-y-3">
            {stats.topTorrents.slice(0, 8).map((t, i) => (
              <li key={t.name} className="flex items-baseline gap-3 text-sm">
                <span className="w-4 text-right text-xs tabular-nums text-muted-foreground">{i + 1}</span>
                <span className="min-w-0 flex-1">
                  <span className={cn("block truncate", t.deleted && "text-muted-foreground")} title={t.name}>{t.name}</span>
                  <span className="flex items-center gap-2 text-xs text-muted-foreground">{t.group}<TorrentBadges torrent={t} /></span>
                </span>
                <span className="font-medium tabular-nums">{formatBytes(t.uploaded)}</span>
              </li>
            ))}
          </ol>
        </div>
      </div>
    </div>
  )
}
