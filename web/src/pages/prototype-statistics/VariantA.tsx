/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant A, "Overview": one scroll, the answer first, then the chart, then the detail.

import { PageHeader } from "@/components/layout/PageHeader"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes } from "@/lib/utils"
import { useState } from "react"
import { ChurnBars, LibrarySizeChart, SeriesLegend, Spark, StackedBars } from "./charts"
import type { VariantProps } from "./controls"
import { BreakdownTable, Delta, DimensionSelect, InstanceFilter, PeriodPicker, RecordingNote, Segmented, TopTorrentsList, formatRatio } from "./controls"
import type { Dimension } from "./mock-data"
import { DIMENSIONS, getBreakdown, sumOf } from "./mock-data"

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

export function VariantA({ stats, period, setPeriod, instance, setInstance }: VariantProps) {
  const [metric, setMetric] = useState<"up" | "down">("up")
  const [dimension, setDimension] = useState<Dimension>("tracker")
  const rows = getBreakdown(stats, dimension)
  const upSpark = stats.buckets.map(b => sumOf(b.up))
  const downSpark = stats.buckets.map(b => sumOf(b.down))
  const last = stats.library[stats.library.length - 1]

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
        <Tile label="Uploaded" value={formatBytes(stats.totals.uploaded)} spark={upSpark} hint={<Delta now={stats.totals.uploaded} before={stats.totals.prevUploaded} />} />
        <Tile label="Downloaded" value={formatBytes(stats.totals.downloaded)} spark={downSpark} hint={<Delta now={stats.totals.downloaded} before={stats.totals.prevDownloaded} />} />
        <Tile label="Ratio" value={formatRatio(stats.totals.ratio)} hint={`Buffer ${formatBytes(stats.totals.buffer)}`} />
        <Tile label="From cross-seeds" value={formatBytes(stats.crossSeedUploaded)} hint={`${Math.round((stats.crossSeedUploaded / stats.totals.uploaded) * 100)}% of the upload`} />
        <Tile label="Unique size" value={formatBytes(last.uniqueSize)} hint={`${formatBytes(last.totalSize)} with linked copies`} />
      </div>

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

      <div className="grid gap-6 xl:grid-cols-3">
        <Card className="xl:col-span-2">
          <CardHeader>
            <CardTitle>Breakdown</CardTitle>
            <CardDescription>Uploaded, Downloaded, Ratio and Buffer are for the period. Torrents, Size and Seeded are live.</CardDescription>
            <CardAction>
              <DimensionSelect dimension={dimension} setDimension={setDimension} />
            </CardAction>
          </CardHeader>
          <CardContent className="overflow-x-auto">
            <BreakdownTable rows={rows} totals={stats.totals} firstColumn={DIMENSIONS.find(d => d.key === dimension)?.label ?? ""} full />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Top torrents</CardTitle>
            <CardDescription>By upload in the period</CardDescription>
          </CardHeader>
          <CardContent>
            <TopTorrentsList torrents={stats.topTorrents} />
          </CardContent>
        </Card>
      </div>

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
