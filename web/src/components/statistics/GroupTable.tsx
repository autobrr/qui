/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Facts, type FactItem } from "@/components/statistics/Facts"
import { GroupChart } from "@/components/statistics/GroupChart"
import { TorrentMarks, TorrentName } from "@/components/statistics/TopTorrents"
import { Button } from "@/components/ui/button"
import { ChartStyle, type ChartConfig } from "@/components/ui/chart"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useTopTorrents, useTorrentName } from "@/hooks/useTopTorrents"
import { useSeriesColors } from "@/hooks/useSeriesColors"
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
  showsYearView,
  TABLE_GROUPINGS,
  weekBars,
  type BarWidth,
  formatBuffer,
  formatRatio,
  sumBytes,
  type ChartUnit,
  type GroupTotals,
  type StatisticsPeriod,
  type TopTorrent
} from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import type { TableGrouping, TransferHistoryBytes, TransferHistoryGrouping, TransferHistoryInstance, TransferHistoryRow } from "@/types"
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react"
import { Fragment, useMemo, useRef, useState, type ReactNode } from "react"
import { flushSync } from "react-dom"
import { useTranslation } from "react-i18next"

const CHART_ID = "statistics-table"

const PHONE_ROWS = 10

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
  // Loads the rows of a grouping, or of each other grouping, when the user is about to pick one.
  onPrefetch?: (grouping?: TableGrouping) => void
}

/**
 * The table of the period by Tracker group, category, or release attribute. A click on a row opens its detail band below
 * it. A narrow table shows each row as a line with the upload, and the other numbers below the name.
 */
export function GroupTable({ grouping, onGroupingChange, rows, error, liveLoaded, liveError, period, since, series, instances, seriesGroups, displayName, onPrefetch }: GroupTableProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const narrow = useIsPhone()
  const [open, setOpen] = useState<string>()
  const [allRowsOf, setAllRowsOf] = useState<string>()
  const rowLimit = useRef<HTMLDivElement>(null)
  const withBaseline = !period.from

  // Only a switch of the grouping keeps the old rows, so that the table never shows the numbers of another period or selection.
  const scope = `${period.from?.getTime()}:${period.to.getTime()}:${instances.map(instance => instance.instanceId).join(",")}`
  const [kept, setKept] = useState<{ scope: string; grouping: TableGrouping; rows: GroupTotals[] }>()
  if (rows && (kept?.rows !== rows || kept.grouping !== grouping || kept.scope !== scope)) {
    setKept({ scope, grouping, rows })
  }
  const last = kept?.scope === scope ? kept : undefined
  const shownRows = rows ?? last?.rows
  const shownGrouping = rows ? grouping : last?.grouping ?? grouping
  const stale = !rows && last !== undefined
  const tracker = shownGrouping === "tracker"

  const colors = useSeriesColors()
  const config = useMemo<ChartConfig>(() => Object.fromEntries(seriesGroups.map((group, index) => [
    `s${index}`,
    { label: group, theme: { light: colors.light[index], dark: colors.dark[index] } },
  ])), [seriesGroups, colors])
  const total = useMemo<TransferHistoryBytes | undefined>(() => shownRows && sumBytes(shownRows, false), [shownRows])
  const liveTotal = useMemo<Live | undefined>(() => shownRows && liveLoaded ? shownRows.reduce<Live>((sum, row) => ({
    torrents: sum.torrents + (row.live?.torrents ?? 0),
    size: sum.size + (row.live?.size ?? 0),
    uploaded: sum.uploaded + (row.live?.uploaded ?? 0),
  }), { torrents: 0, size: 0, uploaded: 0 }) : undefined, [shownRows, liveLoaded])
  const formatCount = new Intl.NumberFormat(locale)
  const formatSeeded = new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 })

  const liveValues = (live: Live | undefined) => {
    const values = liveLoaded ? live ?? { torrents: 0, size: 0, uploaded: 0 } : undefined
    return [
      values ? formatCount.format(values.torrents) : "–",
      values ? formatBytes(values.size) : "–",
      values && values.size > 0 ? t("table.seededValue", { value: formatSeeded.format(values.uploaded / values.size) }) : "–",
    ]
  }
  const periodFacts = (bytes: TransferHistoryBytes): FactItem[] => [
    [t("table.columns.downloaded"), formatBytes(bytes.downloaded)],
    [t("table.columns.ratio"), formatRatio(bytes) ?? "–"],
    [t("table.columns.buffer"), formatBuffer(bytes) ?? "–"],
  ]
  const liveFacts = (live: Live | undefined): FactItem[] => {
    const [torrents, size, seeded] = liveValues(live)
    return [[t("table.columns.torrents"), torrents], [t("table.columns.size"), size], [t("table.columns.seeded"), seeded]]
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
  const pick = (value: string) => {
    setOpen(undefined)
    onGroupingChange(value as TableGrouping)
  }

  const allRows = !narrow || allRowsOf === series
  const visible = shownRows && !allRows ? shownRows.slice(0, PHONE_ROWS) : shownRows

  return (
    <div className="space-y-3">
      {narrow ? (
        <div className="flex items-center gap-2 px-4">
          <span className="shrink-0 text-xs text-muted-foreground">{t("table.groupBy")}</span>
          <Select value={grouping} onValueChange={pick} onOpenChange={opened => opened && onPrefetch?.()}>
            <SelectTrigger size="sm" className="min-w-0">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TABLE_GROUPINGS.map(value => <SelectItem key={value} value={value}>{groupingLabel(value)}</SelectItem>)}
            </SelectContent>
          </Select>
          {stale && <Loader2 className="size-4 animate-spin text-muted-foreground" />}
        </div>
      ) : (
        <div className="flex items-center gap-3 overflow-x-auto px-6">
          <span className="shrink-0 text-xs text-muted-foreground">{t("table.groupBy")}</span>
          <Tabs value={grouping} onValueChange={pick}>
            <TabsList>
              {TABLE_GROUPINGS.map(value => (
                <TabsTrigger key={value} value={value} className="px-3" onPointerEnter={() => onPrefetch?.(value)} onFocus={() => onPrefetch?.(value)}>
                  {groupingLabel(value)}
                </TabsTrigger>
              ))}
            </TabsList>
          </Tabs>
          {stale && <Loader2 className="size-4 shrink-0 animate-spin text-muted-foreground" />}
        </div>
      )}
      {error ? (
        <div className="flex h-24 items-center justify-center px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: error.message })}</div>
      ) : !shownRows || !visible ? (
        <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
      ) : shownRows.length === 0 ? (
        <div className="flex h-24 items-center justify-center px-4 text-sm text-muted-foreground sm:px-6">{t("chart.empty")}</div>
      ) : (
        <div className={cn("@container border-t transition-opacity", stale && "pointer-events-none opacity-60")} aria-busy={stale} data-chart={CHART_ID}>
          <ChartStyle id={CHART_ID} config={config} />
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-full pl-4 @max-xl:pr-4 sm:pl-6">
                  <div className="flex items-center justify-between gap-2">
                    <span>{groupingLabel(shownGrouping)}</span>
                    <span className="@xl:hidden">{t("table.columns.uploaded")}</span>
                  </div>
                </TableHead>
                <TableHead className="hidden text-right @xl:table-cell">{t("table.columns.uploaded")}</TableHead>
                <TableHead className="hidden text-right @xl:table-cell">{t("table.columns.downloaded")}</TableHead>
                <TableHead className="hidden text-right @xl:table-cell">{t("table.columns.ratio")}</TableHead>
                <TableHead className={cn("hidden pr-6 text-right @xl:table-cell", tracker && "@4xl:pr-2")}>{t("table.columns.buffer")}</TableHead>
                {tracker && <>
                  <TableHead className="hidden text-right @4xl:table-cell">{t("table.columns.torrents")}</TableHead>
                  <TableHead className="hidden text-right @4xl:table-cell">{t("table.columns.size")}</TableHead>
                  <TableHead className="hidden pr-6 text-right @4xl:table-cell">{t("table.columns.seeded")}</TableHead>
                </>}
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map(row => {
                const expanded = open === row.group
                const seriesIndex = tracker ? seriesGroups.indexOf(row.group) : -1
                const name = displayName(row.group, shownGrouping)
                return (
                  <Fragment key={row.group}>
                    <TableRow className={cn("cursor-pointer", expanded && "bg-muted hover:bg-muted")} onClick={() => setOpen(expanded ? undefined : row.group)}>
                      <TableCell className="w-full max-w-0 pl-4 font-medium @max-xl:pr-4 sm:pl-6">
                        <button type="button" aria-expanded={expanded} className="flex w-full min-w-0 items-center gap-2 text-left">
                          {expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
                          {tracker && <span className="size-2 shrink-0 rounded-[2px]" style={{ background: seriesIndex >= 0 ? `var(--color-s${seriesIndex})` : "var(--muted-foreground)", opacity: seriesIndex >= 0 ? 1 : 0.45 }} />}
                          <span className="min-w-0 flex-1 truncate">{name}</span>
                          <span className="shrink-0 tabular-nums @xl:hidden" data-narrow>{formatBytes(row.uploaded)}</span>
                        </button>
                        <Facts items={periodFacts(row)} className="@xl:hidden" />
                        {tracker && <Facts items={liveFacts(row.live)} className="hidden @xl:flex @4xl:hidden" />}
                      </TableCell>
                      <PeriodCells bytes={row} trailing={!tracker} />
                      {tracker && liveValues(row.live).map((value, index) => (
                        <TableCell key={index} className={cn("hidden text-right tabular-nums text-muted-foreground @4xl:table-cell", index === 2 && "pr-6")}>{value}</TableCell>
                      ))}
                    </TableRow>
                    {expanded && total && (
                      <TableRow className="hover:bg-transparent">
                        <TableCell colSpan={columns} className="p-0">
                          <GroupDetail
                            group={row.group}
                            grouping={shownGrouping}
                            row={row}
                            total={total}
                            period={period}
                            since={since}
                            series={series}
                            instances={instances}
                            seriesIndex={seriesIndex}
                            name={name}
                            liveFacts={tracker ? liveFacts(row.live) : undefined}
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
                  <TableCell className="pl-4 @max-xl:pr-4 sm:pl-6">
                    <div className="flex items-center justify-between gap-2">
                      <span>{t("table.total")}</span>
                      <span className="tabular-nums @xl:hidden" data-narrow>{formatBytes(total.uploaded)}</span>
                    </div>
                    <Facts items={periodFacts(total)} className="pl-0 @xl:hidden" />
                    {tracker && <Facts items={liveFacts(liveTotal)} className="pl-0 @4xl:hidden" />}
                  </TableCell>
                  <PeriodCells bytes={total} trailing={!tracker} />
                  {tracker && liveValues(liveTotal).map((value, index) => (
                    <TableCell key={index} className={cn("hidden text-right tabular-nums @4xl:table-cell", index === 2 && "pr-6")}>{value}</TableCell>
                  ))}
                </TableRow>
              </TableFooter>
            )}
          </Table>
          {narrow && shownRows.length > PHONE_ROWS && (
            <div ref={rowLimit} className="flex items-center justify-between gap-2 border-t px-4 pt-3 text-xs text-muted-foreground">
              <span>{t("table.shown", { shown: formatCount.format(visible.length), total: formatCount.format(shownRows.length) })}</span>
              {allRows ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => {
                    flushSync(() => setAllRowsOf(undefined))
                    // Safari keeps the scroll offset when the list above gets shorter, so the button moves out of view.
                    rowLimit.current?.scrollIntoView({ block: "nearest" })
                  }}
                >
                  {t("table.showFewer")}
                </Button>
              ) : <Button variant="outline" size="sm" onClick={() => setAllRowsOf(series)}>{t("table.showAll")}</Button>}
            </div>
          )}
        </div>
      )}
      <div className="space-y-1 px-4 text-xs text-muted-foreground sm:px-6">
        {tracker && liveError && <p className="text-destructive">{t("table.liveError", { message: liveError.message })}</p>}
        {tracker ? <p>{t("table.liveNote")}</p> : shownGrouping === "category" ? <p>{t("table.categoryNote")}</p> : <p>{t("table.releaseNote")}</p>}
        {withBaseline && <p>{t("table.baselineNote")}</p>}
      </div>
    </div>
  )
}

function PeriodCells({ bytes, trailing }: { bytes: TransferHistoryBytes; trailing: boolean }) {
  return (
    <>
      <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatBytes(bytes.uploaded)}</TableCell>
      <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatBytes(bytes.downloaded)}</TableCell>
      <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatRatio(bytes) ?? "–"}</TableCell>
      <TableCell className={cn("hidden pr-6 text-right tabular-nums @xl:table-cell", !trailing && "@4xl:pr-2")}>{formatBuffer(bytes) ?? "–"}</TableCell>
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
  // The live numbers of a Tracker group, which a narrow table shows only here.
  liveFacts?: FactItem[]
}

interface ShownRows {
  key: string
  rows: TransferHistoryRow[][]
  period: StatisticsPeriod
}

/** The band below an open row: the share of the upload, the upload from cross-seeds, the best and the average bucket, and the chart of the group. */
function GroupDetail({ group, grouping, row, total, period, since, series, instances, seriesIndex, name, liveFacts }: GroupDetailProps) {
  const colors = useSeriesColors()
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
  // Daily bars of a long period are too thin to read, so its chart shows weeks.
  const weekly = showsYearView(period, since)
  const chartBars = useMemo(() => bars && weekly ? weekBars(bars) : bars, [bars, weekly])
  const best = bars && bestBar(bars, unit)
  const uploaded = bars?.reduce((sum, bar) => sum + barBytes(bar).uploaded, 0) ?? 0
  const recordedBars = bars && recordedUnits(bars, since, unit, grouping !== "tracker", shownBars?.to)

  const formatPercent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 1 })
  const formatStart = (start: number, width: BarWidth) => width === "hour" ? new Date(start).toLocaleString(locale, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12 }) : new Date(start).toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" })

  return (
    <div className="sticky left-0 w-[100cqw] space-y-4 border-l-2 bg-muted/30 px-4 py-5 whitespace-normal [overflow-wrap:anywhere] sm:px-6" style={{ borderLeftColor: seriesIndex >= 0 ? `var(--color-s${seriesIndex})` : "var(--border)" }}>
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
        {liveFacts?.map(([label, value]) => <Fact key={label} label={label} value={value} className="@xl:hidden" />)}
      </dl>
      <div>
        <div className="mb-2 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
          <span className="font-medium text-foreground">{weekly ? t("detail.chartWeek", { group: name }) : unit === "hour" ? t("detail.chartHour", { group: name }) : t("detail.chartDay", { group: name })}</span>
          <span>{t("detail.chartLegend")}</span>
        </div>
        {transfer.error ? (
          <div className="flex h-[200px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: transfer.error.message })}</div>
        ) : !chartBars ? (
          <div className="flex h-[200px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
        ) : (
          <GroupChart bars={chartBars} unit={unit} color={seriesIndex >= 0 ? { light: colors.light[seriesIndex], dark: colors.dark[seriesIndex] } : undefined} />
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
              <TorrentName name={torrentName(torrent)} className="min-w-0 flex-1 @2xl:line-clamp-1" />
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

function Fact({ label, value, detail, className }: { label: string; value?: string; detail?: ReactNode; className?: string }) {
  return (
    <div className={className}>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-0.5 text-base font-semibold tabular-nums">{value ?? "–"}</dd>
      {detail && <dd className="text-xs text-muted-foreground">{detail}</dd>}
    </div>
  )
}
