/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant E, "Combined": the page of A, the table and inspector of B, the torrent table of C, and the calendar of D for a year.

import { PageHeader } from "@/components/layout/PageHeader"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes } from "@/lib/utils"
import { useState } from "react"
import { ChurnBars, LibrarySizeChart, SeriesLegend, Spark, StackedBars } from "./charts"
import type { VariantProps } from "./controls"
import { Delta, InstanceFilter, PeriodPicker, RecordingNote, Segmented, formatDay, formatRatio } from "./controls"
import { getHeatmap, getMonths, sumOf } from "./mock-data"
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
    </div>
  )
}
