/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { GroupChart } from "@/components/statistics/GroupChart"
import { TorrentMarks } from "@/components/statistics/TopTorrents"
import { ChartStyle, type ChartConfig } from "@/components/ui/chart"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTopTorrents, useTorrentName } from "@/hooks/useTopTorrents"
import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import {
  BROWSER_TIME_ZONE,
  barBytes,
  bestBar,
  recordedUnits,
  buildBars,
  buildCategoryBars,
  buildTorrentBars,
  chartRange,
  type BarWidth,
  formatBuffer,
  formatRatio,
  SERIES_COLORS,
  sumBytes,
  type ChartUnit,
  type GroupTotals,
  type StatisticsPeriod,
  type TopTorrent
} from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import type { TableGrouping, TransferHistoryBytes, TransferHistoryGrouping, TransferHistoryInstance, TransferHistoryRow } from "@/types"
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react"
import { Fragment, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

const CHART_ID = "statistics-table"

const GROUPINGS: TableGrouping[] = ["tracker", "category", "releaseGroup", "resolution", "source", "contentType"]

type Live = NonNullable<GroupTotals["live"]>

interface GroupTableProps {
  grouping: TableGrouping
  onGroupingChange: (grouping: TableGrouping) => void
  // Undefined while the rows load.
  rows?: GroupTotals[]
  error?: Error
  // Set when the live numbers of each instance loaded.
  liveLoaded: boolean
  liveError?: Error
  period: StatisticsPeriod
  since?: Date
  // The period choice and the instance selection, so that the detail band keeps its chart only for the same series.
  series: string
  instances: TransferHistoryInstance[]
  // The Tracker groups that have a series color in the chart, in series order.
  seriesGroups: string[]
  displayName: (group: string, grouping: TransferHistoryGrouping) => string
}

/** The table of the period by Tracker group, category, or release attribute. A click on a row opens its detail band below it. */
export function GroupTable({ grouping, onGroupingChange, rows, error, liveLoaded, liveError, period, since, series, instances, seriesGroups, displayName }: GroupTableProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const [open, setOpen] = useState<string>()
  const tracker = grouping === "tracker"
  const withBaseline = !period.from

  const config = useMemo<ChartConfig>(() => Object.fromEntries(seriesGroups.map((group, index) => [
    `s${index}`,
    { label: group, theme: { light: SERIES_COLORS.light[index], dark: SERIES_COLORS.dark[index] } },
  ])), [seriesGroups])
  const total = useMemo<TransferHistoryBytes | undefined>(() => rows && sumBytes(rows, false), [rows])
  const liveTotal = useMemo<Live | undefined>(() => rows && liveLoaded ? rows.reduce<Live>((sum, row) => ({
    torrents: sum.torrents + (row.live?.torrents ?? 0),
    size: sum.size + (row.live?.size ?? 0),
    uploaded: sum.uploaded + (row.live?.uploaded ?? 0),
  }), { torrents: 0, size: 0, uploaded: 0 }) : undefined, [rows, liveLoaded])
  const formatCount = new Intl.NumberFormat(locale)
  const formatSeeded = new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

  const liveCells = (live: Live | undefined) => {
    const shown = liveLoaded ? live ?? { torrents: 0, size: 0, uploaded: 0 } : undefined
    return [
      shown ? formatCount.format(shown.torrents) : "–",
      shown ? formatBytes(shown.size) : "–",
      shown && shown.size > 0 ? t("table.seededValue", { value: formatSeeded.format(shown.uploaded / shown.size) }) : "–",
    ]
  }
  const columns = 5 + (tracker ? 3 : 0)
  const groupingLabel = (value: TableGrouping) => ({
    tracker: t("table.groupings.tracker"),
    category: t("table.groupings.category"),
    releaseGroup: t("table.groupings.releaseGroup"),
    resolution: t("table.groupings.resolution"),
    source: t("table.groupings.source"),
    contentType: t("table.groupings.contentType"),
  })[value]

  return (
    <div className="space-y-3">
      <div className="flex items-center gap-3 overflow-x-auto px-6">
        <span className="shrink-0 text-xs text-muted-foreground">{t("table.groupBy")}</span>
        <Tabs
          value={grouping}
          onValueChange={value => {
            setOpen(undefined)
            onGroupingChange(value as TableGrouping)
          }}
        >
          <TabsList>
            {GROUPINGS.map(value => <TabsTrigger key={value} value={value} className="px-3">{groupingLabel(value)}</TabsTrigger>)}
          </TabsList>
        </Tabs>
      </div>
      {error ? (
        <div className="flex h-24 items-center justify-center px-6 text-sm text-destructive">{t("errors.load", { message: error.message })}</div>
      ) : !rows ? (
        <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
      ) : rows.length === 0 ? (
        <div className="flex h-24 items-center justify-center px-6 text-sm text-muted-foreground">{t("chart.empty")}</div>
      ) : (
        <div className="@container border-t" data-chart={CHART_ID}>
          <ChartStyle id={CHART_ID} config={config} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-6">{groupingLabel(grouping)}</TableHead>
                <TableHead className="text-right">{t("table.columns.uploaded")}</TableHead>
                <TableHead className="text-right">{t("table.columns.downloaded")}</TableHead>
                <TableHead className="text-right">{t("table.columns.ratio")}</TableHead>
                <TableHead className={cn("text-right", !tracker && "pr-6")}>{t("table.columns.buffer")}</TableHead>
                {tracker && <>
                  <TableHead className="text-right">{t("table.columns.torrents")}</TableHead>
                  <TableHead className="text-right">{t("table.columns.size")}</TableHead>
                  <TableHead className="pr-6 text-right">{t("table.columns.seeded")}</TableHead>
                </>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map(row => {
                const expanded = open === row.group
                const seriesIndex = tracker ? seriesGroups.indexOf(row.group) : -1
                return (
                  <Fragment key={row.group}>
                    <TableRow className={cn("cursor-pointer", expanded && "bg-muted hover:bg-muted")} onClick={() => setOpen(expanded ? undefined : row.group)}>
                      <TableCell className="pl-6 font-medium">
                        <button type="button" aria-expanded={expanded} className="inline-flex max-w-64 items-center gap-2 text-left">
                          {expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
                          {tracker && <span className="size-2 shrink-0 rounded-[2px]" style={{ background: seriesIndex >= 0 ? `var(--color-s${seriesIndex})` : "var(--muted-foreground)", opacity: seriesIndex >= 0 ? 1 : 0.45 }} />}
                          <span className="truncate">{displayName(row.group, grouping)}</span>
                        </button>
                      </TableCell>
                      <PeriodCells bytes={row} trailing={!tracker} />
                      {tracker && liveCells(row.live).map((value, index) => (
                        <TableCell key={index} className={cn("text-right tabular-nums text-muted-foreground", index === 2 && "pr-6")}>{value}</TableCell>
                      ))}
                    </TableRow>
                    {expanded && total && (
                      <TableRow className="hover:bg-transparent">
                        <TableCell colSpan={columns} className="p-0">
                          <GroupDetail
                            group={row.group}
                            grouping={grouping}
                            row={row}
                            total={total}
                            period={period}
                            since={since}
                            series={series}
                            instances={instances}
                            seriesIndex={seriesIndex}
                            name={displayName(row.group, grouping)}
                          />
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                )
              })}
            </TableBody>
            {total && (
              <TableFooter>
                <TableRow>
                  <TableCell className="pl-6">{t("table.total")}</TableCell>
                  <PeriodCells bytes={total} trailing={!tracker} />
                  {tracker && liveCells(liveTotal).map((value, index) => (
                    <TableCell key={index} className={cn("text-right tabular-nums", index === 2 && "pr-6")}>{value}</TableCell>
                  ))}
                </TableRow>
              </TableFooter>
            )}
          </Table>
        </div>
      )}
      <div className="space-y-1 px-6 text-xs text-muted-foreground">
        {tracker && liveError && <p className="text-destructive">{t("table.liveError", { message: liveError.message })}</p>}
        {tracker ? <p>{t("table.liveNote")}</p> : grouping === "category" ? <p>{t("table.categoryNote")}</p> : <p>{t("table.releaseNote")}</p>}
        {withBaseline && <p>{t("table.baselineNote")}</p>}
      </div>
    </div>
  )
}

function PeriodCells({ bytes, trailing }: { bytes: TransferHistoryBytes; trailing: boolean }) {
  return (
    <>
      <TableCell className="text-right tabular-nums">{formatBytes(bytes.uploaded)}</TableCell>
      <TableCell className="text-right tabular-nums">{formatBytes(bytes.downloaded)}</TableCell>
      <TableCell className="text-right tabular-nums">{formatRatio(bytes) ?? "–"}</TableCell>
      <TableCell className={cn("text-right tabular-nums", trailing && "pr-6")}>{formatBuffer(bytes) ?? "–"}</TableCell>
    </>
  )
}

interface GroupDetailProps {
  group: string
  grouping: TransferHistoryGrouping
  row: TransferHistoryBytes
  total: TransferHistoryBytes
  period: StatisticsPeriod
  since?: Date
  series: string
  instances: TransferHistoryInstance[]
  // The index of the series color of the group, or -1.
  seriesIndex: number
  name: string
}

interface ShownRows {
  key: string
  rows: TransferHistoryRow[][]
  period: StatisticsPeriod
}

/** The band below an open row: the share of the upload, the upload from cross-seeds, the best and the average bucket, and the chart of the group. */
function GroupDetail({ group, grouping, row, total, period, since, series, instances, seriesIndex, name }: GroupDetailProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const hour12 = usePersistedDateTimePreferences().preferences.timeFormat === "12h"
  // qui stores the category totals and the bytes of each torrent for each UTC day, so only a Tracker group has hours.
  const unit: ChartUnit = grouping === "tracker" ? period.unit : "day"
  const transfer = useTransferHistoryRows(instances, {
    from: period.from,
    to: period.to,
    bucket: unit,
    groupBy: grouping,
    group,
    timeZone: grouping === "tracker" && unit === "day" ? BROWSER_TIME_ZONE : undefined,
  })

  // After the period moves, the band keeps its chart until the rows of the new period load.
  const key = `${series}:${grouping}:${group}`
  const [last, setLast] = useState<ShownRows>()
  if (transfer.rows && last?.rows !== transfer.rows) {
    setLast({ key, rows: transfer.rows, period })
  }
  const shownBars = useMemo(() => {
    const shown = transfer.rows ? { rows: transfer.rows, period } : last?.key === key ? last : undefined
    if (!shown) {
      return undefined
    }
    if (grouping === "category") {
      return { bars: buildCategoryBars(shown.rows, shown.period, since), to: shown.period.to }
    }
    if (grouping !== "tracker") {
      // A release attribute reads the torrent rows, which hold a year row for each year that qui folded.
      return { bars: buildTorrentBars(shown.rows.flat(), shown.period, since), to: shown.period.to }
    }
    const range = chartRange(shown.period, since)
    return { bars: range && buildBars(shown.rows, range, unit), to: shown.period.to }
  }, [transfer.rows, period, last, key, since, unit, grouping])
  const bars = shownBars?.bars
  const best = bars && bestBar(bars, unit)
  const uploaded = bars?.reduce((sum, bar) => sum + barBytes(bar).uploaded, 0) ?? 0
  const recordedBars = bars && recordedUnits(bars, since, unit, grouping !== "tracker", shownBars?.to)

  const formatPercent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 1 })
  const formatStart = (start: number, width: BarWidth) => width === "hour" ? new Date(start).toLocaleString(locale, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12 }) : new Date(start).toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" })

  return (
    <div className="sticky left-0 w-[100cqw] space-y-4 border-l-2 bg-muted/30 px-6 py-5" style={{ borderLeftColor: seriesIndex >= 0 ? `var(--color-s${seriesIndex})` : "var(--border)" }}>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-4">
        <Fact
          label={t("detail.share")}
          value={total.uploaded > 0 ? formatPercent.format(row.uploaded / total.uploaded) : "–"}
          detail={t("detail.shareOf", { total: formatBytes(total.uploaded) })}
        />
        <Fact
          label={t("detail.crossSeed")}
          value={formatBytes(row.statisticsCrossSeedUploaded)}
          detail={row.uploaded > 0 ? t("detail.crossSeedShare", { percent: formatPercent.format(row.statisticsCrossSeedUploaded / row.uploaded) }) : undefined}
        />
        <Fact
          label={unit === "hour" ? t("detail.bestHour") : t("detail.bestDay")}
          value={bars ? best ? formatBytes(best.uploaded) : "–" : undefined}
          detail={best && formatStart(best.bar.start, best.bar.width)}
        />
        <Fact
          label={unit === "hour" ? t("detail.averageHour") : t("detail.averageDay")}
          value={recordedBars === undefined ? undefined : recordedBars > 0 ? formatBytes(uploaded / recordedBars) : "–"}
        />
      </dl>
      <div>
        <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{unit === "hour" ? t("detail.chartHour", { group: name }) : t("detail.chartDay", { group: name })}</span>
          <span>{t("detail.chartLegend")}</span>
        </div>
        {transfer.error ? (
          <div className="flex h-[200px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: transfer.error.message })}</div>
        ) : !bars ? (
          <div className="flex h-[200px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
        ) : (
          <GroupChart bars={bars} unit={unit} color={seriesIndex >= 0 ? { light: SERIES_COLORS.light[seriesIndex], dark: SERIES_COLORS.dark[seriesIndex] } : undefined} />
        )}
      </div>
      <GroupTorrents group={group} grouping={grouping} name={name} period={period} series={series} instances={instances} />
    </div>
  )
}

const GROUP_LIMIT = 5

/** The torrents of a group with the most upload in the period. */
function GroupTorrents({ group, grouping, name, period, series, instances }: { group: string; grouping: TransferHistoryGrouping; name: string; period: StatisticsPeriod; series: string; instances: TransferHistoryInstance[] }) {
  const { t } = useTranslation("statistics")
  const torrentName = useTorrentName()
  const { torrents: loaded, error } = useTopTorrents(instances, period, GROUP_LIMIT, { grouping, name: group })

  // After the period moves, the list keeps its torrents until the torrents of the new period load, as the chart does.
  const key = `${series}:${grouping}:${group}`
  const [last, setLast] = useState<{ key: string; torrents: TopTorrent[] }>()
  if (loaded && last?.torrents !== loaded) {
    setLast({ key, torrents: loaded })
  }
  const torrents = loaded ?? (last?.key === key ? last.torrents : undefined)

  return (
    <div>
      <div className="mb-2 text-xs font-medium">{t("detail.topTorrents", { group: name })}</div>
      {error ? (
        <p className="text-sm text-destructive">{t("errors.load", { message: error.message })}</p>
      ) : !torrents ? (
        <Loader2 className="size-4 animate-spin text-muted-foreground" />
      ) : torrents.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("chart.empty")}</p>
      ) : (
        <ol className="space-y-1 text-sm">
          {torrents.map(torrent => (
            <li key={`${torrent.instanceId}:${torrent.hash}`} className="flex min-w-0 items-center gap-2">
              <span className="truncate" title={torrentName(torrent)}>{torrentName(torrent)}</span>
              <TorrentMarks torrent={torrent} />
              <span className="ml-auto shrink-0 pl-4 tabular-nums">{formatBytes(torrent.uploaded)}</span>
            </li>
          ))}
        </ol>
      )}
      {grouping === "category" && <p className="mt-2 text-xs text-muted-foreground">{t("detail.topTorrentsCategoryNote")}</p>}
    </div>
  )
}

function Fact({ label, value, detail }: { label: string; value?: string; detail?: ReactNode }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-base font-semibold tabular-nums">{value ?? "–"}</dd>
      {detail && <dd className="text-xs text-muted-foreground">{detail}</dd>}
    </div>
  )
}
