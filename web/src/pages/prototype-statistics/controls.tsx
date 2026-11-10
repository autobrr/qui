/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: controls and small parts shared by the Statistics page variants (issue #3991).

import { Badge } from "@/components/ui/badge"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn, formatBytes } from "@/lib/utils"
import { ShareBar, Spark, seriesColor } from "./charts"
import type { Dimension, GroupRow, PeriodKey, Stats, TorrentRow } from "./mock-data"
import { DIMENSIONS, INSTANCES, PERIODS } from "./mock-data"

export interface VariantProps {
  stats: Stats
  period: PeriodKey
  setPeriod: (period: PeriodKey) => void
  instance: string
  setInstance: (instance: string) => void
}

export function Segmented<T extends string>({ value, onChange, options, className }: { value: T; onChange: (value: T) => void; options: { key: T; label: string }[]; className?: string }) {
  return (
    <Tabs value={value} onValueChange={v => onChange(v as T)} className={className}>
      <TabsList>
        {options.map(o => (
          <TabsTrigger key={o.key} value={o.key} className="px-3">{o.label}</TabsTrigger>
        ))}
      </TabsList>
    </Tabs>
  )
}

export function PeriodPicker({ period, setPeriod }: Pick<VariantProps, "period" | "setPeriod">) {
  return <Segmented value={period} onChange={setPeriod} options={[...PERIODS.map(p => ({ key: p.key, label: p.label }))]} />
}

export function InstanceFilter({ instance, setInstance }: Pick<VariantProps, "instance" | "setInstance">) {
  return (
    <Select value={instance} onValueChange={setInstance}>
      <SelectTrigger className="h-9 w-[170px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {INSTANCES.map(i => (
          <SelectItem key={i.id} value={i.id}>
            {i.name}
            {i.removed && <span className="ml-1 text-muted-foreground">(removed)</span>}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function DimensionSelect({ dimension, setDimension }: { dimension: Dimension; setDimension: (d: Dimension) => void }) {
  return (
    <Select value={dimension} onValueChange={v => setDimension(v as Dimension)}>
      <SelectTrigger className="h-8 w-[160px]">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {DIMENSIONS.map(d => (
          <SelectItem key={d.key} value={d.key}>{d.label}</SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function formatDay(t: number): string {
  return new Date(t).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" })
}

export function formatRatio(ratio: number): string {
  return ratio > 0 ? ratio.toFixed(2) : "-"
}

export function RecordingNote({ stats, className }: { stats: Stats; className?: string }) {
  return (
    <span className={cn("inline-flex flex-wrap items-center gap-2 text-sm text-muted-foreground", className)}>
      Recording since {formatDay(stats.recordingSince)}
      {stats.partial && <Badge variant="outline" className="border-yellow-500/50 text-yellow-600 dark:text-yellow-500">Partial period</Badge>}
      {stats.period === "all" && <Badge variant="outline">Includes earlier bytes</Badge>}
    </span>
  )
}

export function Delta({ now, before }: { now: number; before: number | null }) {
  if (before === null || before === 0) return null
  const pct = ((now - before) / before) * 100
  return (
    <span className={cn("text-xs tabular-nums", pct >= 0 ? "text-emerald-600 dark:text-emerald-500" : "text-muted-foreground")}>
      {pct >= 0 ? "+" : ""}{pct.toFixed(0)}% vs previous
    </span>
  )
}

export function SeriesDot({ index }: { index: number }) {
  return <span className={cn("inline-block size-2 shrink-0 rounded-[2px]", index < 0 && "opacity-45")} style={{ background: seriesColor(index) }} />
}

interface BreakdownTableProps {
  rows: GroupRow[]
  totals: Stats["totals"]
  firstColumn: string
  /** The Tracker Breakdown card columns. Without them: name, uploaded, share, ratio. */
  full?: boolean
}

export function BreakdownTable({ rows, totals, firstColumn, full = false }: BreakdownTableProps) {
  const max = Math.max(...rows.map(r => r.uploaded))
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{firstColumn}</TableHead>
          <TableHead className="text-right">Uploaded</TableHead>
          <TableHead className="w-[18%]" />
          <TableHead className="text-right">Downloaded</TableHead>
          <TableHead className="text-right">Ratio</TableHead>
          {full && (
            <>
              <TableHead className="text-right">Buffer</TableHead>
              <TableHead className="text-right">Torrents</TableHead>
              <TableHead className="text-right">Size</TableHead>
              <TableHead className="text-right">Seeded</TableHead>
              <TableHead className="text-right">Trend</TableHead>
            </>
          )}
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map(r => (
          <TableRow key={r.key}>
            <TableCell className="font-medium">
              <span className="inline-flex items-center gap-2"><SeriesDot index={r.colorIndex} />{r.name}</span>
            </TableCell>
            <TableCell className="text-right tabular-nums">{formatBytes(r.uploaded)}</TableCell>
            <TableCell><ShareBar value={r.uploaded} max={max} color={seriesColor(r.colorIndex)} /></TableCell>
            <TableCell className="text-right tabular-nums text-muted-foreground">{formatBytes(r.downloaded)}</TableCell>
            <TableCell className="text-right tabular-nums">{formatRatio(r.ratio)}</TableCell>
            {full && (
              <>
                <TableCell className="text-right tabular-nums">{formatBytes(r.buffer)}</TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">{r.torrents.toLocaleString()}</TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">{formatBytes(r.size)}</TableCell>
                <TableCell className="text-right tabular-nums text-muted-foreground">{formatBytes(r.seeded)}</TableCell>
                <TableCell><Spark values={r.spark} color={seriesColor(r.colorIndex)} className="ml-auto" /></TableCell>
              </>
            )}
          </TableRow>
        ))}
      </TableBody>
      <TableFooter>
        <TableRow>
          <TableCell>Total</TableCell>
          <TableCell className="text-right tabular-nums">{formatBytes(totals.uploaded)}</TableCell>
          <TableCell />
          <TableCell className="text-right tabular-nums">{formatBytes(totals.downloaded)}</TableCell>
          <TableCell className="text-right tabular-nums">{formatRatio(totals.ratio)}</TableCell>
          {full && (
            <>
              <TableCell className="text-right tabular-nums">{formatBytes(totals.buffer)}</TableCell>
              <TableCell colSpan={4} />
            </>
          )}
        </TableRow>
      </TableFooter>
    </Table>
  )
}

export function TorrentBadges({ torrent }: { torrent: TorrentRow }) {
  return (
    <>
      {torrent.deleted && <Badge variant="outline" className="px-1.5 py-0 text-[10px] text-muted-foreground">Deleted</Badge>}
      {torrent.crossSeed && <Badge variant="secondary" className="px-1.5 py-0 text-[10px]">Cross-seed</Badge>}
    </>
  )
}

export function TopTorrentsList({ torrents, limit = 8 }: { torrents: TorrentRow[]; limit?: number }) {
  const max = torrents[0]?.uploaded ?? 1
  return (
    <ol className="space-y-3">
      {torrents.slice(0, limit).map((t, i) => (
        <li key={t.name} className="grid grid-cols-[1.25rem_1fr_auto] items-baseline gap-x-2 gap-y-1">
          <span className="text-xs tabular-nums text-muted-foreground">{i + 1}</span>
          <span className={cn("truncate text-sm", t.deleted && "text-muted-foreground")} title={t.name}>{t.name}</span>
          <span className="text-sm font-medium tabular-nums">{formatBytes(t.uploaded)}</span>
          <span />
          <span className="flex items-center gap-2 text-xs text-muted-foreground">
            {t.group}
            <TorrentBadges torrent={t} />
          </span>
          <span />
          <span />
          <span className="col-span-2"><ShareBar value={t.uploaded} max={max} color="var(--series-1)" /></span>
        </li>
      ))}
    </ol>
  )
}
