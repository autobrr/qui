/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant E, "Combined": the page of A, the table and inspector of B, the torrent table of C, the delete list of C, and the calendar of D for a year.

import { PageHeader } from "@/components/layout/PageHeader"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatBytes } from "@/lib/utils"
import { ArchiveRestore, KeyRound, Unplug, User, Zap } from "lucide-react"
import { useState } from "react"
import { ChurnBars, LibrarySizeChart, SeriesLegend, Spark, StackedBars, formatBucket } from "./charts"
import type { VariantProps } from "./controls"
import { Delta, InstanceFilter, PeriodPicker, RecordingNote, Segmented, formatDay, formatRatio } from "./controls"
import type { DeleteCause, DeleteRow, Stats } from "./mock-data"
import { getDeletes, getHeatmap, getMonths, sumOf } from "./mock-data"
import { Ledger } from "./VariantB"
import { TorrentsTable } from "./VariantC"
import { Calendar, MonthBars, Record } from "./VariantD"

function Tile({ label, value, hint, spark }: { label: string; value: string; hint?: React.ReactNode; spark?: number[] }) {
  return (
    <Card className="gap-2 py-4">
      <CardContent className="px-4">
        <div className="text-sm text-muted-foreground">{label}</div>
        <div className="mt-1 whitespace-nowrap text-2xl font-semibold">{value}</div>
        <div className="mt-1 flex min-h-6 items-center justify-between gap-2 text-xs text-muted-foreground">
          <span>{hint}</span>
          {spark && <Spark values={spark} className="w-16 shrink-0" />}
        </div>
      </CardContent>
    </Card>
  )
}

const CAUSES: Record<DeleteCause, { label: string; icon: typeof Zap }> = {
  automation: { label: "Automation", icon: Zap },
  user: { label: "You", icon: User },
  proxy: { label: "API key", icon: KeyRound },
  restore: { label: "Backup restore", icon: ArchiveRestore },
  external: { label: "Outside qui", icon: Unplug },
}

type CauseFilter = "all" | "automation" | "user" | "other"

/** Who deleted each torrent, and the numbers it ended with (user stories 26 and 27 of the spec). */
function DeletesCard({ stats }: { stats: Stats }) {
  const [filter, setFilter] = useState<CauseFilter>("all")
  const deletes = getDeletes(stats)
  const matches = (d: DeleteRow, f: CauseFilter) => f === "all" || (f === "other" ? d.cause !== "automation" && d.cause !== "user" : d.cause === f)
  // The mock has a few rows only, so the counts scale to the deletes of the period.
  const count = (f: CauseFilter) => Math.round((deletes.filter(d => matches(d, f)).length / deletes.length) * stats.counts.deleted)
  const rows = deletes.filter(d => matches(d, filter))
  const longPeriod = stats.buckets.length > 7 && stats.unit === "day"

  return (
    <Card className="gap-0 overflow-hidden pb-0">
      <CardHeader className="pb-4">
        <CardTitle>Deleted torrents</CardTitle>
        <CardDescription>{stats.counts.deleted.toLocaleString()} deleted in the period. Who deleted each torrent, and the numbers it ended with.</CardDescription>
        <CardAction>
          <Segmented
            value={filter}
            onChange={setFilter}
            options={[
              { key: "all", label: "All" },
              { key: "automation", label: `Automations ${count("automation")}` },
              { key: "user", label: `You ${count("user")}` },
              { key: "other", label: `Other ${count("other")}` },
            ]}
          />
        </CardAction>
      </CardHeader>
      <div className="overflow-x-auto border-t">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-6">Deleted</TableHead>
              <TableHead>Torrent</TableHead>
              <TableHead>Tracker</TableHead>
              <TableHead>Deleted by</TableHead>
              <TableHead className="text-right">Final upload</TableHead>
              <TableHead className="text-right">Final ratio</TableHead>
              <TableHead className="pr-6 text-right">Seeded</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map(d => {
              const cause = CAUSES[d.cause]
              return (
                <TableRow key={d.name}>
                  <TableCell className="whitespace-nowrap pl-6 text-muted-foreground">{longPeriod ? formatDay(d.t) : formatBucket(d.t, "hour", 48)}</TableCell>
                  <TableCell className="max-w-[360px] truncate" title={d.name}>{d.name}</TableCell>
                  <TableCell className="text-muted-foreground">{d.group}</TableCell>
                  <TableCell>
                    <span className="inline-flex items-center gap-1.5 whitespace-nowrap">
                      <cause.icon className="size-3.5 text-muted-foreground" />
                      {cause.label}
                      {d.causeName && <span className="text-muted-foreground">· {d.causeName}</span>}
                    </span>
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(d.finalUploaded)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatRatio(d.finalRatio)}</TableCell>
                  <TableCell className="pr-6 text-right tabular-nums">{d.seededDays} d</TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
      <div className="border-t px-6 py-3 text-xs text-muted-foreground">
        The latest {rows.length} of {count(filter).toLocaleString()}. The real page loads more on request.
      </div>
    </Card>
  )
}

export function VariantE({ stats, period, setPeriod, instance, setInstance }: VariantProps) {
  const [metric, setMetric] = useState<"up" | "down">("up")
  const last = stats.library[stats.library.length - 1]
  const year = period === "1y" || period === "all"

  const days = getHeatmap(instance)
  const recorded = days.filter(d => d.recorded)
  const best = recorded.reduce((a, b) => (b.value > a.value ? b : a), recorded[0])
  const months = getMonths(stats)
  const bestMonth = months.reduce((a, b) => (sumOf(b.up) > sumOf(a.up) ? b : a), months[0])
  const weekend = sumOf(recorded.filter(d => [0, 6].includes(new Date(d.t).getDay())).map(d => d.value))

  return (
    <div className="space-y-6 p-4 pb-24 lg:p-6 lg:pb-24">
      <PageHeader
        title="Statistics"
        description={<RecordingNote stats={stats} />}
        actions={
          <>
            <InstanceFilter instance={instance} setInstance={setInstance} />
            <PeriodPicker period={period} setPeriod={setPeriod} />
          </>
        }
      />

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-5">
        <Tile label="Uploaded" value={formatBytes(stats.totals.uploaded)} spark={stats.buckets.map(b => sumOf(b.up))} hint={<Delta now={stats.totals.uploaded} before={stats.totals.prevUploaded} />} />
        <Tile label="Downloaded" value={formatBytes(stats.totals.downloaded)} spark={stats.buckets.map(b => sumOf(b.down))} hint={<Delta now={stats.totals.downloaded} before={stats.totals.prevDownloaded} />} />
        <Tile label="Ratio" value={formatRatio(stats.totals.ratio)} hint={`Buffer ${formatBytes(stats.totals.buffer)}`} />
        <Tile label="From cross-seeds" value={formatBytes(stats.crossSeedUploaded)} hint={`${Math.round((stats.crossSeedUploaded / stats.totals.uploaded) * 100)}% of the upload`} />
        <Tile label="Unique size" value={formatBytes(last.uniqueSize)} hint={`${formatBytes(last.totalSize)} with linked copies`} />
      </div>

      {year ? (
        <Card>
          <CardHeader>
            <CardTitle>The year</CardTitle>
            <CardDescription>Upload per day, then per month by tracker</CardDescription>
          </CardHeader>
          <CardContent className="space-y-8">
            <Calendar days={days} />
            <div className="grid gap-6 sm:grid-cols-3">
              <Record label="Best day" value={formatBytes(best.value)} detail={formatDay(best.t)} />
              <Record label="Best month" value={new Date(bestMonth.t).toLocaleDateString(undefined, { month: "long" })} detail={formatBytes(sumOf(bestMonth.up))} />
              <Record label="Weekends" value={`${Math.round((weekend / sumOf(recorded.map(d => d.value))) * 100)}%`} detail="of the upload moved on a Saturday or a Sunday" />
            </div>
            <MonthBars stats={stats} />
            <SeriesLegend groups={stats.groups} />
          </CardContent>
        </Card>
      ) : (
        <Card>
          <CardHeader>
            <CardTitle>Transfer over time</CardTitle>
            <CardDescription>By tracker, per {stats.unit}</CardDescription>
            <CardAction>
              <Segmented value={metric} onChange={setMetric} options={[{ key: "up", label: "Upload" }, { key: "down", label: "Download" }]} />
            </CardAction>
          </CardHeader>
          <CardContent className="space-y-3">
            <StackedBars stats={stats} metric={metric} height={300} />
            <SeriesLegend groups={stats.groups} />
          </CardContent>
        </Card>
      )}

      <Card className="gap-0 overflow-hidden py-0">
        <Ledger stats={stats} expand />
      </Card>

      <Card className="gap-0 overflow-hidden pb-0">
        <CardHeader className="pb-4">
          <CardTitle>Top torrents</CardTitle>
          <CardDescription>By upload in the period. Deleted torrents stay in the list. Open a row for the upload per day.</CardDescription>
        </CardHeader>
        <div className="overflow-x-auto border-t">
          <TorrentsTable stats={stats} />
        </div>
      </Card>

      <div className="grid gap-6 lg:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Library growth</CardTitle>
            <CardDescription>{last.torrents.toLocaleString()} torrents. Total size and Unique size.</CardDescription>
          </CardHeader>
          <CardContent>
            <LibrarySizeChart points={stats.library} />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Added and deleted</CardTitle>
            <CardDescription>
              {stats.counts.added.toLocaleString()} added, {stats.counts.completed.toLocaleString()} completed, {stats.counts.deleted.toLocaleString()} deleted
            </CardDescription>
          </CardHeader>
          <CardContent>
            <ChurnBars stats={stats} />
          </CardContent>
        </Card>
      </div>

      <DeletesCard stats={stats} />
    </div>
  )
}
