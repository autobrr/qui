/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant B, "Ledger": the table is the page. A row opens an inspector at the side.

import { Button } from "@/components/ui/button"
import { cn, formatBytes } from "@/lib/utils"
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, X } from "lucide-react"
import { Fragment, useState } from "react"
import { GroupBars, ShareBar, Spark, StackedBars, UpDownBars, formatBucket, seriesColor } from "./charts"
import type { VariantProps } from "./controls"
import { InstanceFilter, PeriodPicker, RecordingNote, Segmented, SeriesDot, TorrentBadges, formatRatio } from "./controls"
import type { Dimension, GroupRow, Stats } from "./mock-data"
import { DIMENSIONS, getBreakdown, getGroupTorrents, sumOf } from "./mock-data"

type SortKey = "name" | "uploaded" | "downloaded" | "ratio" | "buffer" | "torrents" | "size" | "seeded"

const COLUMNS: { key: SortKey; label: string; live?: boolean }[] = [
  { key: "uploaded", label: "Uploaded" },
  { key: "downloaded", label: "Downloaded" },
  { key: "ratio", label: "Ratio" },
  { key: "buffer", label: "Buffer" },
  { key: "torrents", label: "Torrents", live: true },
  { key: "size", label: "Size", live: true },
  { key: "seeded", label: "Seeded", live: true },
]

function cell(row: GroupRow, key: SortKey): string {
  if (key === "ratio") return formatRatio(row.ratio)
  if (key === "torrents") return row.torrents.toLocaleString()
  if (key === "name") return row.name
  return formatBytes(row[key])
}

function Inspector({ stats, row, dimension, onClose }: { stats: Stats; row: GroupRow | null; dimension: Dimension; onClose: () => void }) {
  const rank = row ? stats.groups.findIndex(g => g.key === row.key) : -1
  const torrents = row && dimension === "tracker" ? stats.topTorrents.filter(t => t.group === row.name) : stats.topTorrents
  return (
    <aside className="flex flex-col gap-5 border-t p-4 lg:w-[340px] lg:shrink-0 lg:border-l lg:border-t-0">
      <div className="flex items-start justify-between gap-2">
        <div>
          <div className="text-xs uppercase tracking-wide text-muted-foreground">{row ? DIMENSIONS.find(d => d.key === dimension)?.label : "All trackers"}</div>
          <div className="flex items-center gap-2 text-lg font-semibold">
            {row && <SeriesDot index={row.colorIndex} />}
            {row ? row.name : "Period summary"}
          </div>
        </div>
        {row && (
          <Button variant="ghost" size="icon" className="size-7" onClick={onClose} aria-label="Close">
            <X className="size-4" />
          </Button>
        )}
      </div>

      <dl className="grid grid-cols-2 gap-x-4 gap-y-3 text-sm">
        {[
          ["Uploaded", formatBytes(row ? row.uploaded : stats.totals.uploaded)],
          ["Downloaded", formatBytes(row ? row.downloaded : stats.totals.downloaded)],
          ["Ratio", formatRatio(row ? row.ratio : stats.totals.ratio)],
          ["Buffer", formatBytes(row ? row.buffer : stats.totals.buffer)],
          ["From cross-seeds", formatBytes((row ? row.uploaded : stats.totals.uploaded) * 0.71)],
          ["Share of upload", row ? `${((row.uploaded / stats.totals.uploaded) * 100).toFixed(1)}%` : "100%"],
        ].map(([label, value]) => (
          <div key={label}>
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className="font-medium tabular-nums">{value}</dd>
          </div>
        ))}
      </dl>

      <div>
        <div className="mb-2 text-xs font-medium text-muted-foreground">Upload per {stats.unit}</div>
        {row && rank >= 0 && rank < 5 && dimension === "tracker" ? (
          <StackedBars stats={stats} metric="up" height={130} only={rank} hideAxis />
        ) : (
          <StackedBars stats={stats} metric="up" height={130} hideAxis />
        )}
      </div>

      <div>
        <div className="mb-2 text-xs font-medium text-muted-foreground">Top torrents</div>
        {torrents.length === 0 && <div className="text-sm text-muted-foreground">No torrent of this group is in the top list.</div>}
        <ul className="space-y-2.5">
          {torrents.slice(0, 5).map(t => (
            <li key={t.name} className="space-y-1">
              <div className="flex items-baseline gap-2">
                <span className={cn("min-w-0 flex-1 truncate text-sm", t.deleted && "text-muted-foreground")} title={t.name}>{t.name}</span>
                <span className="text-sm font-medium tabular-nums">{formatBytes(t.uploaded)}</span>
              </div>
              <div className="flex items-center gap-2 text-xs text-muted-foreground">
                {t.group} · first 24 h {formatBytes(t.first24h)}
                <TorrentBadges torrent={t} />
              </div>
            </li>
          ))}
        </ul>
      </div>

      {!row && (
        <div>
          <div className="mb-2 text-xs font-medium text-muted-foreground">Latest deletes</div>
          <ul className="space-y-2.5">
            {stats.events.filter(e => e.type === "deleted").map(e => (
              <li key={e.name} className="text-sm">
                <div className="truncate" title={e.name}>{e.name}</div>
                <div className="text-xs text-muted-foreground">
                  {e.actor} · ratio {formatRatio(e.finalRatio ?? 0)} · seeded {e.seededDays} d
                </div>
              </li>
            ))}
          </ul>
        </div>
      )}
    </aside>
  )
}

function Fact({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-base font-semibold tabular-nums">{value}</dd>
      {detail && <dd className="text-xs text-muted-foreground">{detail}</dd>}
    </div>
  )
}

/** The band that opens below a clicked row in variant E: what the row itself does not show. */
function RowDetail({ stats, row, dimension }: { stats: Stats; row: GroupRow; dimension: Dimension }) {
  const rank = dimension === "tracker" ? stats.groups.findIndex(g => g.key === row.key) : -1
  const share = row.uploaded / stats.totals.uploaded
  const downShare = row.downloaded / stats.totals.downloaded
  const points = stats.buckets.map(b => ({
    t: b.t,
    up: rank >= 0 ? b.up[rank] : sumOf(b.up) * share,
    down: rank >= 0 ? b.down[rank] : sumOf(b.down) * downShare,
    gapHours: b.gapHours,
  }))
  const best = points.reduce((a, b) => (b.up > a.up ? b : a), points[0])
  // A long period has too many days for bars, so the chart shows weeks.
  const weekly = points.length > 120
  const bars = weekly
    ? Array.from({ length: Math.ceil(points.length / 7) }, (_, w) => {
      const week = points.slice(w * 7, w * 7 + 7)
      return { t: week[0].t, up: sumOf(week.map(p => p.up)), down: sumOf(week.map(p => p.down)), gapHours: week.find(p => p.gapHours)?.gapHours }
    })
    : points
  const barUnit = weekly ? "week" : stats.unit
  const torrents = getGroupTorrents(stats, row, dimension)
  const color = seriesColor(row.colorIndex)
  const max = torrents[0]?.uploaded ?? 1

  return (
    <div className="sticky left-0 grid w-[100cqw] gap-x-8 gap-y-6 border-l-2 bg-muted/30 px-4 py-5 lg:grid-cols-[minmax(0,1fr)_minmax(0,24rem)]" style={{ borderLeftColor: color }}>
      <div className="min-w-0 space-y-4">
        <dl className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
          <Fact label="Share of upload" value={`${(share * 100).toFixed(1)}%`} detail={`of ${formatBytes(stats.totals.uploaded)}`} />
          <Fact label="From cross-seeds" value={formatBytes(row.uploaded * 0.71)} detail="71% of its upload" />
          <Fact label={`Best ${stats.unit}`} value={formatBytes(best.up)} detail={formatBucket(best.t, stats.unit, points.length)} />
          <Fact label={`Average per ${stats.unit}`} value={formatBytes(row.uploaded / points.length)} />
        </dl>
        <div>
          <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span className="font-medium text-foreground">{row.name} per {barUnit}</span>
            <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-[2px]" style={{ background: color }} />Uploaded</span>
            <span className="inline-flex items-center gap-1.5"><span className="size-2 rounded-[2px] bg-muted-foreground opacity-50" />Downloaded</span>
          </div>
          <GroupBars stats={stats} points={bars} color={color} height={190} />
        </div>
      </div>
      <div className="min-w-0 lg:border-l lg:pl-8">
        <div className="mb-3 text-xs font-medium text-muted-foreground">Top torrents in {row.name}</div>
        <ol className="space-y-3.5">
          {torrents.map((t, i) => (
            <li key={t.name} className="grid grid-cols-[1rem_minmax(0,1fr)_auto] items-baseline gap-x-2 gap-y-1">
              <span className="text-xs tabular-nums text-muted-foreground">{i + 1}</span>
              <span className={cn("truncate text-sm", t.deleted && "text-muted-foreground")} title={t.name}>{t.name}</span>
              <span className="text-sm font-medium tabular-nums">{formatBytes(t.uploaded)}</span>
              <span />
              <span className="col-span-2 flex items-center gap-2 text-xs text-muted-foreground">
                first 24 h {formatBytes(t.first24h)}
                <TorrentBadges torrent={t} />
              </span>
              <span />
              <span className="col-span-2"><ShareBar value={t.uploaded} max={max} color={color} /></span>
            </li>
          ))}
        </ol>
      </div>
    </div>
  )
}

/**
 * The group-by control, the sortable table, and the detail of one row. Variant B shows the detail
 * in an inspector at the side. Variant E passes `expand`: the clicked row opens a band below itself.
 */
export function Ledger({ stats, expand }: { stats: Stats; expand?: boolean }) {
  const [dimension, setDimension] = useState<Dimension>("tracker")
  const [sort, setSort] = useState<{ key: SortKey; desc: boolean }>({ key: "uploaded", desc: true })
  const [selectedKey, setSelectedKey] = useState<string | null>(null)

  const rows = [...getBreakdown(stats, dimension)].sort((a, b) => {
    const dir = sort.desc ? -1 : 1
    return sort.key === "name" ? a.name.localeCompare(b.name) * dir : (a[sort.key] - b[sort.key]) * dir
  })
  const selected = rows.find(r => r.key === selectedKey) ?? null
  const max = Math.max(...rows.map(r => r.uploaded))

  const header = (key: SortKey, label: string, live?: boolean) => (
    <th key={key} className={cn("whitespace-nowrap px-2.5 py-2 font-medium", key === "name" ? "text-left" : "text-right")}>
      <button
        type="button"
        className="inline-flex items-center gap-1 hover:text-foreground"
        onClick={() => setSort(s => ({ key, desc: s.key === key ? !s.desc : true }))}
      >
        {label}
        {live && <span className="text-[10px] font-normal opacity-70">now</span>}
        {sort.key === key && (sort.desc ? <ArrowDown className="size-3" /> : <ArrowUp className="size-3" />)}
      </button>
    </th>
  )

  return (
    <div className="flex flex-1 flex-col lg:flex-row">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-3 overflow-x-auto border-b px-4 py-2">
          <span className="shrink-0 whitespace-nowrap text-xs text-muted-foreground">Group by</span>
          <Segmented value={dimension} onChange={d => { setDimension(d); setSelectedKey(null) }} options={DIMENSIONS} />
        </div>
        <div className="@container overflow-x-auto">
          <table className="w-full text-sm">
            <thead className="border-b text-xs text-muted-foreground">
              <tr>
                {header("name", DIMENSIONS.find(d => d.key === dimension)?.label ?? "")}
                {COLUMNS.map(c => header(c.key, c.label, c.live))}
                <th className="px-2.5 py-2 text-right font-medium">Trend</th>
              </tr>
            </thead>
            <tbody>
              {rows.map(r => (
                <Fragment key={r.key}>
                <tr
                  onClick={() => setSelectedKey(k => (k === r.key ? null : r.key))}
                  className={cn("cursor-pointer border-b transition-colors hover:bg-muted/50", selectedKey === r.key && "bg-muted")}
                >
                  <td className="whitespace-nowrap px-2.5 py-1.5 font-medium">
                    <span className="inline-flex items-center gap-2">
                      {expand && (selectedKey === r.key ? <ChevronDown className="size-3.5 text-muted-foreground" /> : <ChevronRight className="size-3.5 text-muted-foreground" />)}
                      <SeriesDot index={r.colorIndex} />
                      {r.name}
                    </span>
                  </td>
                  <td className="whitespace-nowrap px-2.5 py-1.5 text-right tabular-nums">
                    <span className="text-xs text-muted-foreground">{((r.uploaded / stats.totals.uploaded) * 100).toFixed(0)}%</span>
                    <span className="ml-2">{cell(r, "uploaded")}</span>
                    <span className="mt-1 block"><ShareBar value={r.uploaded} max={max} color={seriesColor(r.colorIndex)} /></span>
                  </td>
                  {COLUMNS.slice(1).map(c => (
                    <td key={c.key} className={cn("whitespace-nowrap px-2.5 py-1.5 text-right tabular-nums", c.live && "text-muted-foreground")}>{cell(r, c.key)}</td>
                  ))}
                  <td className="px-2.5 py-1.5"><Spark values={r.spark} color={seriesColor(r.colorIndex)} className="ml-auto h-5 w-16" /></td>
                </tr>
                {expand && selectedKey === r.key && (
                  <tr className="border-b">
                    <td colSpan={9} className="p-0"><RowDetail stats={stats} row={r} dimension={dimension} /></td>
                  </tr>
                )}
                </Fragment>
              ))}
            </tbody>
            <tfoot className="text-sm font-medium">
              <tr>
                <td className="px-2.5 py-2">Total</td>
                <td className="whitespace-nowrap px-2.5 py-2 text-right tabular-nums">{formatBytes(stats.totals.uploaded)}</td>
                <td className="whitespace-nowrap px-2.5 py-2 text-right tabular-nums">{formatBytes(stats.totals.downloaded)}</td>
                <td className="px-2.5 py-2 text-right tabular-nums">{formatRatio(stats.totals.ratio)}</td>
                <td className="whitespace-nowrap px-2.5 py-2 text-right tabular-nums">{formatBytes(stats.totals.buffer)}</td>
                <td colSpan={4} />
              </tr>
            </tfoot>
          </table>
        </div>
      </div>
      {!expand && <Inspector stats={stats} row={selected} dimension={dimension} onClose={() => setSelectedKey(null)} />}
    </div>
  )
}

export function VariantB({ stats, period, setPeriod, instance, setInstance }: VariantProps) {
  return (
    <div className="flex min-h-full flex-col pb-20">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b px-4 py-3">
        <h1 className="text-lg font-semibold">Statistics</h1>
        <PeriodPicker period={period} setPeriod={setPeriod} />
        <InstanceFilter instance={instance} setInstance={setInstance} />
        <RecordingNote stats={stats} className="ml-auto text-xs" />
      </div>

      <div className="border-b px-4 py-3">
        <div className="mb-1 flex flex-wrap items-baseline gap-x-6 gap-y-1 text-sm">
          <span><span className="mr-1.5 inline-block size-2 rounded-[2px] bg-(--series-1)" />Uploaded <b className="tabular-nums">{formatBytes(stats.totals.uploaded)}</b></span>
          <span><span className="mr-1.5 inline-block size-2 rounded-[2px] bg-(--series-2)" />Downloaded <b className="tabular-nums">{formatBytes(stats.totals.downloaded)}</b></span>
          <span className="text-muted-foreground">Ratio <b className="tabular-nums text-foreground">{formatRatio(stats.totals.ratio)}</b></span>
          <span className="text-muted-foreground">Buffer <b className="tabular-nums text-foreground">{formatBytes(stats.totals.buffer)}</b></span>
        </div>
        <UpDownBars stats={stats} height={130} />
      </div>

      <Ledger stats={stats} />
    </div>
  )
}
