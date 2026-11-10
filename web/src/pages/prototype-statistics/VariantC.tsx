/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant C, "Sections": one question per screen, behind tabs. Small multiples instead of one stack.

import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn, formatBytes } from "@/lib/utils"
import { ChevronDown, ChevronRight } from "lucide-react"
import { Fragment, useState } from "react"
import { ChurnBars, LibraryLine, LibrarySizeChart, Spark, StackedBars, formatBucket } from "./charts"
import type { VariantProps } from "./controls"
import { BreakdownTable, Delta, InstanceFilter, PeriodPicker, RecordingNote, Segmented, SeriesDot, TorrentBadges, formatRatio } from "./controls"
import type { Dimension, Stats } from "./mock-data"
import { DIMENSIONS, PERIODS, SERIES_COUNT, getBreakdown } from "./mock-data"

const SECTIONS = ["transfer", "breakdown", "torrents", "library", "activity"] as const
const SECTION_LABELS: Record<(typeof SECTIONS)[number], string> = { transfer: "Transfer", breakdown: "Breakdown", torrents: "Torrents", library: "Library", activity: "Activity" }

/** The top torrents as a table. A row opens the upload per day. Variant E uses it too. */
export function TorrentsTable({ stats }: { stats: Stats }) {
  const [openTorrent, setOpenTorrent] = useState<string | null>(null)
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-10" />
          <TableHead>Torrent</TableHead>
          <TableHead>Tracker</TableHead>
          <TableHead className="text-right">Uploaded</TableHead>
          <TableHead className="text-right">First 24 h</TableHead>
          <TableHead className="text-right">Size</TableHead>
          <TableHead className="text-right">Ratio</TableHead>
          <TableHead className="text-right">Trend</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {stats.topTorrents.map((t, i) => (
          <Fragment key={t.name}>
            <TableRow className="cursor-pointer" onClick={() => setOpenTorrent(o => (o === t.name ? null : t.name))}>
              <TableCell className="text-muted-foreground">
                <span className="inline-flex items-center gap-1 tabular-nums">
                  {openTorrent === t.name ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
                  {i + 1}
                </span>
              </TableCell>
              <TableCell className="max-w-[420px]">
                <div className="flex items-center gap-2">
                  <span className={cn("truncate", t.deleted && "text-muted-foreground")} title={t.name}>{t.name}</span>
                  <TorrentBadges torrent={t} />
                </div>
              </TableCell>
              <TableCell className="text-muted-foreground">{t.group}</TableCell>
              <TableCell className="text-right font-medium tabular-nums">{formatBytes(t.uploaded)}</TableCell>
              <TableCell className="text-right tabular-nums text-muted-foreground">{formatBytes(t.first24h)}</TableCell>
              <TableCell className="text-right tabular-nums text-muted-foreground">{formatBytes(t.size)}</TableCell>
              <TableCell className="text-right tabular-nums">{formatRatio(t.ratio)}</TableCell>
              <TableCell><Spark values={t.spark} className="ml-auto" /></TableCell>
            </TableRow>
            {openTorrent === t.name && (
              <TableRow className="hover:bg-transparent">
                <TableCell />
                <TableCell colSpan={7} className="py-4">
                  <div className="mb-2 text-xs text-muted-foreground">Upload per day{t.deleted && ". The torrent is deleted, and its history stays."}</div>
                  <div className="flex h-24 items-end gap-0.5">
                    {t.spark.map((v, d) => (
                      <div key={d} className="flex-1 rounded-t-[2px] bg-(--series-1)" style={{ height: `${Math.max(1, (v / Math.max(...t.spark)) * 100)}%` }} title={formatBytes((v / t.spark.reduce((s, x) => s + x, 0)) * t.uploaded)} />
                    ))}
                  </div>
                </TableCell>
              </TableRow>
            )}
          </Fragment>
        ))}
      </TableBody>
    </Table>
  )
}

export function VariantC({ stats, period, setPeriod, instance, setInstance }: VariantProps) {
  const [metric, setMetric] = useState<"up" | "down">("up")
  const [dimension, setDimension] = useState<Dimension>("tracker")
  const periodLabel = PERIODS.find(p => p.key === period)?.long ?? ""
  const last = stats.library[stats.library.length - 1]
  const first = stats.library[0]

  return (
    <Tabs defaultValue="transfer" className="pb-24">
      <div className="sticky top-0 z-10 flex flex-wrap items-center gap-x-4 gap-y-2 border-b bg-background px-4 py-3 lg:px-6">
        <h1 className="mr-2 text-2xl font-semibold tracking-tight">Statistics</h1>
        <TabsList>
          {SECTIONS.map(s => (
            <TabsTrigger key={s} value={s} className="px-3">{SECTION_LABELS[s]}</TabsTrigger>
          ))}
        </TabsList>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <InstanceFilter instance={instance} setInstance={setInstance} />
          <PeriodPicker period={period} setPeriod={setPeriod} />
        </div>
      </div>

      <TabsContent value="transfer" className="space-y-6 p-4 lg:p-6">
        <div className="flex flex-wrap items-end justify-between gap-4">
          <div>
            <div className="text-sm text-muted-foreground">{metric === "up" ? "Uploaded" : "Downloaded"}, {periodLabel.toLowerCase()}</div>
            <div className="text-5xl font-semibold tracking-tight">{formatBytes(metric === "up" ? stats.totals.uploaded : stats.totals.downloaded)}</div>
            <div className="mt-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
              <Delta now={stats.totals.uploaded} before={stats.totals.prevUploaded} />
              <span>Ratio {formatRatio(stats.totals.ratio)}</span>
              <span>Buffer {formatBytes(stats.totals.buffer)}</span>
              <span>Best {stats.unit}: {formatBucket(stats.peak.t, stats.unit, stats.buckets.length)}, {formatBytes(stats.peak.value)}</span>
            </div>
            <RecordingNote stats={stats} className="mt-2" />
          </div>
          <Segmented value={metric} onChange={setMetric} options={[{ key: "up", label: "Upload" }, { key: "down", label: "Download" }]} />
        </div>

        <StackedBars stats={stats} metric={metric} height={340} />

        <div>
          <div className="mb-3 text-sm font-medium">Each tracker on its own scale</div>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-5">
            {stats.groups.slice(0, SERIES_COUNT).map((g, i) => (
              <div key={g.key} className="rounded-lg border p-3">
                <div className="flex items-center justify-between gap-2 text-sm">
                  <span className="inline-flex items-center gap-2 font-medium"><SeriesDot index={i} />{g.name}</span>
                  <span className="tabular-nums text-muted-foreground">{formatBytes(metric === "up" ? g.uploaded : g.downloaded)}</span>
                </div>
                <StackedBars stats={stats} metric={metric} height={90} only={i} hideAxis />
              </div>
            ))}
          </div>
        </div>
      </TabsContent>

      <TabsContent value="breakdown" className="space-y-4 p-4 lg:p-6">
        <div className="flex flex-wrap items-center gap-3">
          <Segmented value={dimension} onChange={setDimension} options={DIMENSIONS} className="max-w-full overflow-x-auto" />
          <span className="text-sm text-muted-foreground">Uploaded, Downloaded, Ratio and Buffer are for the period. Torrents, Size and Seeded are live.</span>
        </div>
        <div className="overflow-x-auto rounded-lg border">
          <BreakdownTable rows={getBreakdown(stats, dimension)} totals={stats.totals} firstColumn={DIMENSIONS.find(d => d.key === dimension)?.label ?? ""} full />
        </div>
      </TabsContent>

      <TabsContent value="torrents" className="space-y-4 p-4 lg:p-6">
        <div className="text-sm text-muted-foreground">
          The torrents that uploaded most, {periodLabel.toLowerCase()}. Deleted torrents stay in the list. Open a row for the upload per day.
        </div>
        <div className="overflow-x-auto rounded-lg border">
          <TorrentsTable stats={stats} />
        </div>
      </TabsContent>

      <TabsContent value="library" className="grid gap-6 p-4 lg:grid-cols-2 lg:p-6">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle>Size on disk</CardTitle>
            <CardDescription>
              Unique size {formatBytes(last.uniqueSize)}, up {formatBytes(last.uniqueSize - first.uniqueSize)}. Total size {formatBytes(last.totalSize)} counts each linked copy.
            </CardDescription>
          </CardHeader>
          <CardContent><LibrarySizeChart points={stats.library} height={280} /></CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Torrents</CardTitle>
            <CardDescription>{last.torrents.toLocaleString()} now, {(last.torrents - first.torrents).toLocaleString()} more than at the start</CardDescription>
          </CardHeader>
          <CardContent><LibraryLine points={stats.library} field="torrents" label="Torrents" format={v => Math.round(v).toLocaleString()} /></CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Free disk space</CardTitle>
            <CardDescription>{formatBytes(last.freeSpace)} free, as qBittorrent reports it</CardDescription>
          </CardHeader>
          <CardContent><LibraryLine points={stats.library} field="freeSpace" label="Free" format={formatBytes} /></CardContent>
        </Card>
      </TabsContent>

      <TabsContent value="activity" className="space-y-6 p-4 lg:p-6">
        <div className="grid grid-cols-3 gap-4 sm:max-w-xl">
          {([["Added", stats.counts.added], ["Completed", stats.counts.completed], ["Deleted", stats.counts.deleted]] as const).map(([label, value]) => (
            <div key={label}>
              <div className="text-sm text-muted-foreground">{label}</div>
              <div className="text-3xl font-semibold">{value.toLocaleString()}</div>
            </div>
          ))}
        </div>
        <ChurnBars stats={stats} height={240} />
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>When</TableHead>
                <TableHead>Event</TableHead>
                <TableHead>Torrent</TableHead>
                <TableHead>By</TableHead>
                <TableHead className="text-right">Final upload</TableHead>
                <TableHead className="text-right">Final ratio</TableHead>
                <TableHead className="text-right">Seeded</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {stats.events.map(e => (
                <TableRow key={`${e.type}-${e.name}`}>
                  <TableCell className="whitespace-nowrap text-muted-foreground">{formatBucket(e.t, "hour", 48)}</TableCell>
                  <TableCell><Badge variant={e.type === "deleted" ? "outline" : "secondary"} className="capitalize">{e.type}</Badge></TableCell>
                  <TableCell className="max-w-[380px] truncate" title={e.name}>{e.name}</TableCell>
                  <TableCell className="text-muted-foreground">{e.actor || "-"}</TableCell>
                  <TableCell className="text-right tabular-nums">{e.finalUploaded ? formatBytes(e.finalUploaded) : ""}</TableCell>
                  <TableCell className="text-right tabular-nums">{e.finalRatio ? formatRatio(e.finalRatio) : ""}</TableCell>
                  <TableCell className="text-right tabular-nums">{e.seededDays ? `${e.seededDays} d` : ""}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </TabsContent>
    </Tabs>
  )
}
