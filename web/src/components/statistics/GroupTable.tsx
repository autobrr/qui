/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Facts, type FactItem } from "@/components/statistics/Facts"
import { GroupChart } from "@/components/statistics/GroupChart"
import { RowLimit } from "@/components/statistics/RowLimit"
import { TorrentMarks } from "@/components/statistics/TopTorrents"
import { CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartStyle, type ChartConfig } from "@/components/ui/chart"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { SortIcon } from "@/components/ui/sort-icon"
import { Table, TableBody, TableCell, TableFooter, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TorrentName } from "@/components/ui/torrent-name"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useTopTorrents, useTorrentName } from "@/hooks/useTopTorrents"
import { useSeriesColors } from "@/hooks/useSeriesColors"
import { useTrackerGroupIcon } from "@/hooks/useTrackerGroupName"
import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
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
import { Fragment, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

const CHART_ID = "statistics-table"

const FIRST_ROWS = 10

type Live = NonNullable<GroupTotals["live"]>

type SortColumn = "group" | "uploaded" | "downloaded" | "ratio" | "buffer" | "torrents" | "size" | "seeded"

interface Sort {
  column: SortColumn
  descending: boolean
}

const FIRST_SORT: Sort = { column: "uploaded", descending: true }

const LIVE_COLUMNS: SortColumn[] = ["torrents", "size", "seeded"]

function sortNumber(row: GroupTotals, column: Exclude<SortColumn, "group">): number | undefined {
  switch (column) {
    case "uploaded":
      return row.uploaded
    case "downloaded":
      return row.downloaded
    case "ratio":
      if (row.downloaded > 0) return row.uploaded / row.downloaded
      return row.uploaded > 0 ? Infinity : undefined
    case "buffer":
      return row.uploaded === 0 && row.downloaded === 0 ? undefined : row.uploaded - row.downloaded
    case "torrents":
      return row.live?.torrents ?? 0
    case "size":
      return row.live?.size ?? 0
    case "seeded":
      return row.live && row.live.size > 0 ? row.live.uploaded / row.live.size : undefined
  }
}

interface GroupTableProps {
  grouping: TableGrouping
  onGroupingChange: (grouping: TableGrouping) => void
  rows?: GroupTotals[]
  error?: Error
  liveLoaded: boolean
  liveError?: Error
  period: StatisticsPeriod
  since?: Date
  series: string
  listScope: string
  instances: TransferHistoryInstance[]
  seriesGroups: string[]
  displayName: (group: string, grouping: TransferHistoryGrouping) => string
  onPrefetch?: (grouping?: TableGrouping) => void
}

export function GroupTable({ listScope, grouping, onGroupingChange, rows, error, liveLoaded, liveError, period, since, series, instances, seriesGroups, displayName, onPrefetch }: GroupTableProps) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const narrow = useIsPhone()
  const [open, setOpen] = useState<string>()
  const [more, setMore] = useState<{ key: string; rows: number }>()
  const [pickedSort, setPickedSort] = useState(FIRST_SORT)
  const withBaseline = !period.from

  const scope = `${period.from?.getTime()}:${period.to.getTime()}:${instances.map(instance => instance.instanceId).join(",")}`
  const limitKey = `${listScope}:${grouping}`
  const [kept, setKept] = useState<{ scope: string; grouping: TableGrouping; limitKey: string; rows: GroupTotals[] }>()
  if (rows && (kept?.rows !== rows || kept.grouping !== grouping || kept.scope !== scope || kept.limitKey !== limitKey)) {
    setKept({ scope, grouping, limitKey, rows })
  }
  const last = kept?.scope === scope ? kept : undefined
  const shownRows = rows ?? last?.rows
  const shownGrouping = rows ? grouping : last?.grouping ?? grouping
  const shownLimitKey = rows ? limitKey : last?.limitKey ?? limitKey
  if (more !== undefined && more.key !== shownLimitKey) {
    setMore(undefined)
  }
  const stale = !rows && last !== undefined
  const tracker = shownGrouping === "tracker"

  const colors = useSeriesColors()
  const groupIcon = useTrackerGroupIcon()
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
  const periodFacts = (bytes: TransferHistoryBytes, bufferClassName?: string): FactItem[] => [
    [t("table.columns.downloaded"), formatBytes(bytes.downloaded)],
    [t("table.columns.ratio"), formatRatio(bytes) ?? "–"],
    [t("table.columns.buffer"), formatBuffer(bytes) ?? "–", bufferClassName],
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

  const sort = narrow || (!tracker && LIVE_COLUMNS.includes(pickedSort.column)) ? FIRST_SORT : pickedSort
  const sorted = useMemo(() => {
    if (!shownRows) return undefined
    const turn = sort.descending ? -1 : 1
    const column = sort.column
    if (column === "group") {
      const collator = new Intl.Collator(locale, { numeric: true, sensitivity: "base" })
      return [...shownRows].sort((a, b) => turn * collator.compare(displayName(a.group, shownGrouping), displayName(b.group, shownGrouping)))
    }
    return [...shownRows].sort((a, b) => {
      const x = sortNumber(a, column)
      const y = sortNumber(b, column)
      if (x === y) return 0
      if (x === undefined) return 1
      if (y === undefined) return -1
      return x < y ? -turn : turn
    })
  }, [shownRows, sort, locale, displayName, shownGrouping])
  const sortBy = (column: SortColumn) => setPickedSort({ column, descending: sort.column === column ? !sort.descending : column !== "group" })
  const head = (column: SortColumn, label: string, className?: string) => narrow
    ? <TableHead className={className}>{label}</TableHead>
    : <SortHead column={column} sort={sort} onSort={sortBy} className={className}>{label}</SortHead>

  const limit = more?.key === shownLimitKey ? more.rows : FIRST_ROWS
  const visible = sorted?.slice(0, limit)

  return (
    <>
      <CardHeader className="px-4 pb-4 sm:px-6">
        <CardTitle>{t("table.title")}</CardTitle>
        <CardDescription>{tracker ? t("table.descriptionTracker") : shownGrouping === "category" ? t("table.descriptionCategory") : t("table.descriptionRelease")}</CardDescription>
      </CardHeader>
      <div className="space-y-3">
        {narrow ? (
          <div className="flex items-center gap-2 px-4 sm:px-6">
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
                  {narrow ? (
                    <TableHead className="w-full pl-4 @max-xl:pr-4 sm:pl-6">
                      <div className="flex items-center justify-between gap-2">
                        <span>{groupingLabel(shownGrouping)}</span>
                        <span className="@xl:hidden">{t("table.columns.uploaded")}</span>
                      </div>
                    </TableHead>
                  ) : head("group", groupingLabel(shownGrouping), "w-full pl-6")}
                  {head("uploaded", t("table.columns.uploaded"), "hidden text-right @xl:table-cell")}
                  {head("downloaded", t("table.columns.downloaded"), "hidden text-right @xl:table-cell")}
                  {head("ratio", t("table.columns.ratio"), "hidden text-right @xl:table-cell")}
                  {head("buffer", t("table.columns.buffer"), cn("hidden pr-6 text-right @xl:table-cell", tracker && "@4xl:pr-2"))}
                  {tracker && <>
                    {head("torrents", t("table.columns.torrents"), "hidden text-right @4xl:table-cell")}
                    {head("size", t("table.columns.size"), "hidden text-right @4xl:table-cell")}
                    {head("seeded", t("table.columns.seeded"), "hidden pr-6 text-right @4xl:table-cell")}
                  </>}
                </TableRow>
              </TableHeader>
              <TableBody>
                {visible.map(row => {
                  const expanded = open === row.group
                  const seriesIndex = tracker ? seriesGroups.indexOf(row.group) : -1
                  const name = displayName(row.group, shownGrouping)
                  const icon = tracker ? groupIcon(row.group) : undefined
                  return (
                    <Fragment key={row.group}>
                      <TableRow className={cn("cursor-pointer", expanded && "bg-muted hover:bg-muted")} onClick={() => setOpen(expanded ? undefined : row.group)}>
                        <TableCell className="w-full max-w-0 pl-4 font-medium @max-xl:pr-4 sm:pl-6">
                          <button type="button" aria-expanded={expanded} className="flex w-full min-w-0 items-center gap-2 text-left">
                            {expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
                            {tracker && <span className="size-2 shrink-0 rounded-[2px]" style={{ background: seriesIndex >= 0 ? `var(--color-s${seriesIndex})` : "var(--muted-foreground)", opacity: seriesIndex >= 0 ? 1 : 0.45 }} />}
                            {tracker && (icon ? <TrackerIconImage {...icon} /> : <span className="size-4 shrink-0" />)}
                            <span className="min-w-0 flex-1 truncate">{name}</span>
                            <span className="shrink-0 tabular-nums @xl:hidden" data-narrow>{formatBytes(row.uploaded)}</span>
                          </button>
                          <Facts items={periodFacts(row, "hidden @md:inline")} className="@xl:hidden" />
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
            {shownRows.length > FIRST_ROWS && (
              <RowLimit
                shown={visible.length}
                total={shownRows.length}
                onMore={!narrow && visible.length < shownRows.length ? () => setMore({ key: shownLimitKey, rows: limit + FIRST_ROWS }) : undefined}
                onAll={narrow && visible.length < shownRows.length ? () => setMore({ key: shownLimitKey, rows: Infinity }) : undefined}
                onFewer={limit > FIRST_ROWS ? () => setMore(undefined) : undefined}
              />
            )}
          </div>
        )}
        <div className="space-y-1 px-4 text-xs text-muted-foreground sm:px-6">
          {tracker && liveError && <p className="text-destructive">{t("table.liveError", { message: liveError.message })}</p>}
          {tracker ? <p>{t("table.liveNote")}</p> : shownGrouping === "category" ? <p>{t("table.categoryNote")}</p> : <p>{t("table.releaseNote")}</p>}
          {withBaseline && <p>{t("table.baselineNote")}</p>}
        </div>
      </div>
    </>
  )
}

interface SortHeadProps {
  column: SortColumn
  sort: Sort
  onSort: (column: SortColumn) => void
  className?: string
  children: ReactNode
}

function SortHead({ column, sort, onSort, className, children }: SortHeadProps) {
  const sorted = sort.column === column ? (sort.descending ? "desc" : "asc") : false
  return (
    <TableHead className={className} aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : undefined}>
      <button type="button" className="group/sort inline-flex items-center gap-1 rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50" onClick={() => onSort(column)}>
        {column !== "group" && <span className={cn(!sorted && "opacity-0 group-hover/sort:opacity-100 group-focus-visible/sort:opacity-100")}><SortIcon sorted={sorted} /></span>}
        {children}
        {column === "group" && <span className={cn(!sorted && "opacity-0 group-hover/sort:opacity-100 group-focus-visible/sort:opacity-100")}><SortIcon sorted={sorted} /></span>}
      </button>
    </TableHead>
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
  seriesIndex: number
  name: string
  liveFacts?: FactItem[]
}

interface ShownRows {
  key: string
  rows: TransferHistoryRow[][]
  period: StatisticsPeriod
}

function GroupDetail({ group, grouping, row, total, period, since, series, instances, seriesIndex, name, liveFacts }: GroupDetailProps) {
  const colors = useSeriesColors()
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { preferences } = usePersistedDateTimePreferences()
  const hour12 = preferences.timeFormat === "12h"
  const unit: ChartUnit = grouping === "tracker" ? period.unit : "day"
  const transfer = useTransferHistoryRows(instances, {
    from: period.from,
    to: period.to,
    bucket: unit,
    groupBy: grouping,
    group,
    timeZone: grouping === "tracker" && unit === "day" ? BROWSER_TIME_ZONE : undefined,
  })

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
      return { bars: buildTorrentBars(shown.rows.flat(), shown.period, since), to: shown.period.to }
    }
    const range = chartRange(shown.period, since)
    return { bars: range && buildBars(shown.rows, range, unit), to: shown.period.to }
  }, [transfer.rows, period, last, key, since, unit, grouping])
  const bars = shownBars?.bars
  const weekly = showsYearView(period, since)
  const chartBars = useMemo(() => bars && weekly ? weekBars(bars) : bars, [bars, weekly])
  const best = bars && bestBar(bars, unit)
  const uploaded = bars?.reduce((sum, bar) => sum + barBytes(bar).uploaded, 0) ?? 0
  const recordedBars = bars && recordedUnits(bars, since, unit, grouping !== "tracker", shownBars?.to)

  const formatPercent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 1 })
  const formatStart = (start: number, width: BarWidth) => width === "hour" ? new Date(start).toLocaleString(locale, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit", hour12 }) : formatCalendarDay(new Date(start), preferences)

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
        <Fact label={t("table.columns.buffer")} value={formatBuffer(row) ?? "–"} className="@md:hidden" />
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

function GroupTorrents({ group, grouping, name, period, series, instances }: { group: string; grouping: TransferHistoryGrouping; name: string; period: StatisticsPeriod; series: string; instances: TransferHistoryInstance[] }) {
  const { t } = useTranslation("statistics")
  const torrentName = useTorrentName()
  const { torrents: loaded, error } = useTopTorrents(instances, period, GROUP_LIMIT, { grouping, name: group })

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
