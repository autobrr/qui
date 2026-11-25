/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Page } from "@/components/layout/Page"
import { DeletedTorrents } from "@/components/statistics/DeletedTorrents"
import { EventsChart } from "@/components/statistics/EventsChart"
import { GroupTable } from "@/components/statistics/GroupTable"
import { InstanceFilter, PeriodPicker, SeriesColorPicker, type InstanceOption, type PeriodChoice } from "@/components/statistics/StatisticsControls"
import { LibraryGrowthChart } from "@/components/statistics/LibraryGrowthChart"
import { TopTorrents } from "@/components/statistics/TopTorrents"
import { useStatisticsPeriod } from "@/hooks/useStatisticsPeriod"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { useTopTorrents } from "@/hooks/useTopTorrents"
import { TransferChart } from "@/components/statistics/TransferChart"
import { YearView } from "@/components/statistics/YearView"
import { Badge } from "@/components/ui/badge"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTransferHistoryLibrary } from "@/hooks/useTransferHistoryLibrary"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { usePrefetchTransferHistoryRows, useTransferHistoryRows, useTransferHistoryStatus } from "@/hooks/useTransferHistoryRows"
import { api } from "@/lib/api"
import { formatCalendarDay } from "@/lib/dateTimeUtils"
import { useTrackerGroupName } from "@/hooks/useTrackerGroupName"
import { getLinuxCategoryName, getLinuxReleaseGroup, useIncognitoMode } from "@/lib/incognito"
import {
  BROWSER_TIME_ZONE,
  buildBars,
  chartRange,
  comparisonEnd,
  customPeriod,
  formatBuffer,
  formatRatio,
  groupTotals,
  HISTORY_WRITE_MS,
  presetPeriod,
  previousPeriod,
  recordedFrom,
  recordingSince,
  showsYearView,
  sumBytes,
  TABLE_GROUPINGS,
  topGroups,
  type Metric,
  type PeriodPreset,
  type StatisticsPeriod,
  type TopTorrent
} from "@/lib/statistics"
import { buildEventDays } from "@/lib/statistics-events"
import { buildLibraryPoints, newestUniqueSize } from "@/lib/statistics-library"
import { cn, formatBytes } from "@/lib/utils"
import type { LiveTrackerGroup, TableGrouping, TransferHistoryBytes, TransferHistoryGrouping, TransferHistoryLibraryRow, TransferHistoryRow } from "@/types"
import { useQueries, useQuery, useQueryClient, type UseQueryResult } from "@tanstack/react-query"
import { getRouteApi } from "@tanstack/react-router"
import { Loader2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

interface PeriodState {
  choice: PeriodChoice
  period: StatisticsPeriod
  moved: boolean
}


interface LiveGroups {
  groups?: LiveTrackerGroup[][]
  error?: Error
}

function combineLiveGroups(results: UseQueryResult<LiveTrackerGroup[]>[]): LiveGroups {
  return {
    groups: results.every(result => !result.isPending) ? results.map(result => result.data ?? []) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
  }
}

interface PageRows {
  series: string
  period: StatisticsPeriod
  chart?: TransferHistoryRow[][]
  allTime?: TransferHistoryRow[][]
  compared?: TransferHistoryRow[][]
  before?: TransferHistoryRow[][]
  grouped?: TransferHistoryRow[][]
  torrents?: TopTorrent[]
  library?: TransferHistoryLibraryRow[][]
}

const TOP_TORRENTS = 10

function relativeChange(now: number, before: number): number | undefined {
  return before > 0 ? (now - before) / before : undefined
}

function presetState(preset: PeriodPreset): PeriodState {
  return { choice: preset, period: presetPeriod(preset, new Date()), moved: false }
}

const statisticsRoute = getRouteApi("/_authenticated/statistics")

export function StatisticsRoute() {
  const { t } = useTranslation("statistics")
  const { period } = statisticsRoute.useSearch()
  const navigate = statisticsRoute.useNavigate()
  const [, savePreset] = useStatisticsPeriod()
  const settingsLoaded = useClientSettingsSettled()
  useEffect(() => {
    if (!period || !settingsLoaded) return
    savePreset(period)
    void navigate({ search: previous => ({ ...previous, period: undefined }), replace: true })
  }, [period, settingsLoaded, savePreset, navigate])
  if (!settingsLoaded) {
    return (
      <Page title={t("title")}>
        <div className="flex h-64 items-center justify-center">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </div>
      </Page>
    )
  }
  return <Statistics initialPreset={period} />
}

export function Statistics({ initialPreset }: { initialPreset?: PeriodPreset }) {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const [incognitoMode] = useIncognitoMode()
  const narrow = useIsPhone()
  const { preferences } = usePersistedDateTimePreferences()
  const [savedPreset, savePreset] = useStatisticsPeriod()
  const [periodState, setPeriodState] = useState<PeriodState>(() => presetState(initialPreset ?? savedPreset))
  const [followedPreset, setFollowedPreset] = useState(savedPreset)
  if (followedPreset !== savedPreset) {
    setFollowedPreset(savedPreset)
    if (periodState.choice !== "custom" && periodState.choice !== savedPreset) {
      setPeriodState(presetState(savedPreset))
    }
  }
  const { choice } = periodState

  useEffect(() => {
    if (choice === "custom") {
      return
    }
    const move = () => {
      if (document.visibilityState !== "visible") {
        return
      }
      setPeriodState(state => Date.now() - state.period.to.getTime() < HISTORY_WRITE_MS ? state : { choice, period: presetPeriod(choice, new Date()), moved: true })
    }
    const timer = setInterval(move, 60_000)
    document.addEventListener("visibilitychange", move)
    return () => {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", move)
    }
  }, [choice])
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [metric, setMetric] = useState<Metric>("uploaded")
  const [grouping, setGrouping] = useState<TableGrouping>("tracker")
  const { period } = periodState

  const statusQuery = useTransferHistoryStatus()
  const instancesQuery = useQuery({ queryKey: ["instances"], queryFn: () => api.getInstances() })
  const statuses = useMemo(() => statusQuery.data ?? [], [statusQuery.data])
  const selected = useMemo(
    () => selectedIds.length === 0 ? statuses : statuses.filter(status => selectedIds.includes(status.instanceId)),
    [statuses, selectedIds]
  )
  const options = useMemo<InstanceOption[]>(() => {
    const configured = new Set((instancesQuery.data ?? []).map(instance => instance.id))
    return statuses.map(status => ({ id: status.instanceId, name: status.name, removed: instancesQuery.data !== undefined && !configured.has(status.instanceId) }))
  }, [statuses, instancesQuery.data])

  const previous = previousPeriod(period)
  const comparable = previous?.from !== undefined && selected.length > 0 && recordedFrom(selected, previous.from)
  const ready = statusQuery.isSuccess
  const chart = useTransferHistoryRows(selected, ready ? { from: period.from, to: period.to, bucket: period.unit, groupBy: "tracker", timeZone: period.unit === "day" ? BROWSER_TIME_ZONE : undefined } : undefined)
  const allTime = useTransferHistoryRows(selected, ready && !period.from ? { to: period.to, bucket: "total", groupBy: "tracker" } : undefined)
  const compared = useTransferHistoryRows(selected, comparable ? { from: previous.to, to: comparisonEnd(period), bucket: "total", groupBy: "tracker" } : undefined)
  const before = useTransferHistoryRows(selected, comparable ? { from: previous.from, to: previous.to, bucket: "total", groupBy: "tracker" } : undefined)
  const byTracker = grouping === "tracker"
  const grouped = useTransferHistoryRows(selected, ready && !byTracker ? { from: period.from, to: period.to, bucket: "total", groupBy: grouping } : undefined)
  const top = useTopTorrents(selected, period, TOP_TORRENTS)
  const library = useTransferHistoryLibrary(selected, ready ? { from: period.from, to: period.to } : undefined)

  const activeIds = useMemo(() => new Set((instancesQuery.data ?? []).filter(instance => instance.isActive).map(instance => instance.id)), [instancesQuery.data])
  const liveIds = useMemo(() => selected.map(status => status.instanceId).filter(id => activeIds.has(id)), [activeIds, selected])
  const liveQuery = (instanceId: number) => ({ queryKey: ["tracker-groups", instanceId], queryFn: () => api.getTrackerGroups(instanceId) })
  const live = useQueries({
    queries: liveIds.map(instanceId => ({ ...liveQuery(instanceId), enabled: byTracker, refetchInterval: HISTORY_WRITE_MS })),
    combine: combineLiveGroups,
  })

  const queryClient = useQueryClient()
  const prefetchRows = usePrefetchTransferHistoryRows()
  const prefetchGroupings = (only?: TableGrouping) => {
    if (!ready) {
      return
    }
    for (const other of only ? [only] : TABLE_GROUPINGS) {
      if (other === grouping) {
        continue
      }
      if (other === "tracker") {
        liveIds.forEach(instanceId => void queryClient.prefetchQuery({ ...liveQuery(instanceId), staleTime: HISTORY_WRITE_MS }))
      } else {
        prefetchRows(selected, { from: period.from, to: period.to, bucket: "total", groupBy: other })
      }
    }
  }

  const series = `${periodState.choice}:${selected.map(instance => instance.instanceId).join(",")}:${grouping}`
  const complete = ready && chart.rows !== undefined && (period.from !== undefined || allTime.rows !== undefined) && (!comparable || (compared.rows !== undefined && before.rows !== undefined)) && (byTracker || grouped.rows !== undefined) && top.torrents !== undefined && library.rows !== undefined
  const [lastComplete, setLastComplete] = useState<PageRows>()
  if (complete && (lastComplete?.chart !== chart.rows || lastComplete?.allTime !== allTime.rows || lastComplete?.compared !== compared.rows || lastComplete?.before !== before.rows || lastComplete?.grouped !== grouped.rows || lastComplete?.torrents !== top.torrents || lastComplete?.library !== library.rows)) {
    setLastComplete({ series, period, chart: chart.rows, allTime: allTime.rows, compared: compared.rows, before: before.rows, grouped: grouped.rows, torrents: top.torrents, library: library.rows })
  }
  const keep = periodState.moved && !complete && !(chart.error ?? allTime.error ?? compared.error ?? before.error ?? grouped.error ?? top.error ?? library.error) && lastComplete?.series === series
  const shown: PageRows = keep && lastComplete ? lastComplete : { series, period, chart: chart.rows, allTime: allTime.rows, compared: compared.rows, before: before.rows, grouped: grouped.rows, torrents: top.torrents, library: library.rows }

  const since = useMemo(() => recordingSince(selected), [selected])
  const partial = period.from !== undefined && selected.length > 0 && !recordedFrom(selected, period.from)
  const totals = useMemo<TransferHistoryBytes | undefined>(
    () => shown.period.from ? shown.chart && sumBytes(shown.chart.flat(), false) : shown.allTime && sumBytes(shown.allTime.flat(), true),
    [shown.period.from, shown.chart, shown.allTime]
  )
  const previousTotals = useMemo(() => shown.before && sumBytes(shown.before.flat(), false), [shown.before])
  const comparedTotals = useMemo(() => shown.compared && sumBytes(shown.compared.flat(), false), [shown.compared])
  const yearView = showsYearView(shown.period, since)
  const chartMetric: Metric = yearView ? "uploaded" : metric
  const groups = useMemo(() => topGroups(shown.chart?.flat() ?? [], chartMetric), [shown.chart, chartMetric])
  const bars = useMemo(() => {
    const range = chartRange(shown.period, since)
    return shown.chart && range ? buildBars(shown.chart, range, shown.period.unit) : []
  }, [shown.chart, shown.period, since])

  const libraryPoints = useMemo(
    () => shown.library && buildLibraryPoints(shown.library, shown.period, selected.map(status => ({
      firstRecordedAt: status.firstRecordedAt,
      recording: instancesQuery.data === undefined || activeIds.has(status.instanceId),
    }))),
    [shown.library, shown.period, selected, activeIds, instancesQuery.data]
  )
  const unique = libraryPoints && newestUniqueSize(libraryPoints)
  const newestCount = [...libraryPoints ?? []].reverse().find(point => point.torrents !== null)?.torrents ?? undefined
  const growthError = statusQuery.error ?? library.error
  const eventDays = useMemo(() => shown.library && (buildEventDays(shown.library, shown.period, since) ?? []), [shown.library, shown.period, since])

  const tableRows = useMemo(() => {
    const rows = byTracker ? shown.period.from ? shown.chart : shown.allTime : shown.grouped
    return rows && groupTotals(rows, !shown.period.from, byTracker ? live.groups : undefined)
  }, [byTracker, shown.grouped, shown.chart, shown.allTime, shown.period.from, live.groups])

  const displayName = useTrackerGroupName()
  const tableName = useCallback((group: string, by: TransferHistoryGrouping) => {
    switch (by) {
      case "tracker":
        return displayName(group)
      case "category":
        if (group === "") return t("table.noCategory")
        return incognitoMode ? getLinuxCategoryName(group) : group
      default:
        if (group === "") return t("table.none")
        if (by === "releaseGroup" && incognitoMode) return getLinuxReleaseGroup(group)
        return by === "contentType" ? t([`common:contentTypeLabels.${group}`, "common:contentTypeLabels.unknown"]) : group
    }
  }, [displayName, incognitoMode, t])

  const formatDay = (date: Date) => date.toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" })
  const formatRangeDay = (date: Date) => {
    if (!narrow) return formatCalendarDay(date, preferences)
    return date.getFullYear() === new Date().getFullYear() ? date.toLocaleDateString(locale, { day: "numeric", month: "short" }) : formatDay(date)
  }
  const customLabel = periodState.choice === "custom" && period.from ? t("customRange.range", { from: formatRangeDay(period.from), to: formatRangeDay(new Date(period.to.getTime() - 1)) }) : undefined
  const formatChange = new Intl.NumberFormat(locale, { style: "percent", signDisplay: "exceptZero", maximumFractionDigits: 0 })
  const error = statusQuery.error ?? (chart.error ? undefined : allTime.error ?? compared.error ?? before.error)
  const loading = statusQuery.isPending || (!keep && (chart.pending || allTime.pending))

  const change = (key: Metric) => {
    const change = comparedTotals && previousTotals ? relativeChange(comparedTotals[key], previousTotals[key]) : undefined
    return change === undefined ? undefined : t("tiles.change", { change: formatChange.format(change) })
  }

  const periodPicker = (
    <PeriodPicker
      value={periodState.choice}
      customLabel={customLabel}
      onPreset={preset => {
        savePreset(preset)
        setPeriodState(presetState(preset))
      }}
      onCustom={(firstDay, lastDay) => setPeriodState({ choice: "custom", period: customPeriod(firstDay, lastDay, new Date()), moved: false })}
    />
  )
  const instanceFilter = options.length > 1 && <InstanceFilter options={options} selected={selectedIds} onChange={setSelectedIds} />
  const recording = (
    <span className="inline-flex flex-wrap items-center gap-2">
      {since ? t("recordingSince", { date: formatCalendarDay(since, preferences) }) : !statusQuery.isPending && t("notRecording")}
      {partial && <Badge variant="outline" className="py-0 font-normal text-muted-foreground">{t("partial")}</Badge>}
    </span>
  )
  const listScope = `${periodState.choice === "custom" ? `${period.from?.getTime()}-${period.to.getTime()}` : periodState.choice}:${selectedIds.join(",")}`

  return (
    <Page
      title={t("title")}
      description={recording}
      controls={narrow ? (
        <>
          {periodPicker}
          {instanceFilter}
          <SeriesColorPicker />
        </>
      ) : (
        <>
          <SeriesColorPicker />
          {instanceFilter}
          {periodPicker}
        </>
      )}
    >
      <div className="@container space-y-4 sm:space-y-6">
        {error && <p className="text-sm text-destructive">{t("errors.load", { message: error.message })}</p>}

        <div className="grid grid-cols-2 max-sm:rounded-xl max-sm:border max-sm:bg-card max-sm:shadow-sm sm:grid-cols-6 sm:gap-4 xl:grid-cols-5 sideways:grid-cols-5 sideways:gap-2 max-sm:sideways:gap-0">
          <Tile label={t("tiles.uploaded")} value={totals && formatBytes(totals.uploaded)} hint={change("uploaded")} />
          <Tile label={t("tiles.downloaded")} value={totals && formatBytes(totals.downloaded)} hint={change("downloaded")} />
          <Tile
            label={t("tiles.ratio")}
            value={totals && formatRatio(totals)}
            hint={totals && formatBuffer(totals) && t("tiles.buffer", { buffer: formatBuffer(totals) })}
          />
          <Tile
            label={t("tiles.crossSeed")}
            value={totals && formatBytes(totals.statisticsCrossSeedUploaded)}
            hint={totals && totals.uploaded > 0 ? t("tiles.crossSeedShare", { percent: Math.round((totals.statisticsCrossSeedUploaded / totals.uploaded) * 100) }) : undefined}
            wide
          />
          <Tile
            label={t("tiles.unique")}
            value={unique && formatBytes(unique.uniqueSize)}
            hint={unique ? t("tiles.uniqueTotal", { size: formatBytes(unique.totalSize) }) : libraryPoints && libraryPoints.length > 0 && t("tiles.uniqueNone")}
            wide
          />
        </div>

        <Card className="gap-4 py-4 sm:gap-6 sm:py-6">
          <CardHeader className="px-4 sm:px-6">
            <CardTitle>{yearView ? t("yearView.title") : t("chart.title")}</CardTitle>
            <CardDescription>{yearView ? t("yearView.description") : period.unit === "hour" ? t("chart.descriptionHour") : t("chart.descriptionDay")}</CardDescription>
            {!yearView && <CardAction>
              <Tabs value={metric} onValueChange={value => setMetric(value as Metric)}>
                <TabsList className="max-sm:h-8">
                  <TabsTrigger value="uploaded" className="px-3 max-sm:px-2 max-sm:text-xs">{t("chart.upload")}</TabsTrigger>
                  <TabsTrigger value="downloaded" className="px-3 max-sm:px-2 max-sm:text-xs">{t("chart.download")}</TabsTrigger>
                </TabsList>
              </Tabs>
            </CardAction>}
          </CardHeader>
          <CardContent className="px-4 sm:px-6">
            {chart.error ? (
              <div className="flex h-[260px] items-center justify-center text-sm text-destructive sm:h-[300px] sideways:h-[220px]">{t("errors.load", { message: chart.error.message })}</div>
            ) : loading ? (
              <div className="flex h-[260px] items-center justify-center sm:h-[300px] sideways:h-[220px]"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
            ) : bars.some(bar => bar.bytes.size > 0) ? yearView ? (
              <YearView bars={bars} groups={groups} to={shown.period.to} since={since} displayName={displayName} />
            ) : (
              <TransferChart bars={bars} groups={groups} metric={metric} unit={shown.period.unit} displayName={displayName} />
            ) : (
              <div className="flex h-[260px] items-center justify-center text-sm text-muted-foreground sm:h-[300px] sideways:h-[220px]">{t("chart.empty")}</div>
            )}
          </CardContent>
        </Card>

        <div className="grid gap-4 sm:gap-6 @[118rem]:grid-cols-2">
          <Card className="gap-0 overflow-hidden pt-4 pb-4 sm:pt-6">
            <GroupTable
              listScope={listScope}
              grouping={grouping}
              onGroupingChange={setGrouping}
              rows={loading ? undefined : tableRows}
              error={statusQuery.error ?? chart.error ?? (period.from ? undefined : allTime.error) ?? (byTracker ? undefined : grouped.error)}
              liveLoaded={liveIds.length > 0 && live.groups !== undefined}
              liveError={live.error}
              period={shown.period}
              since={since}
              series={shown.series}
              instances={selected}
              seriesGroups={groups}
              displayName={tableName}
              onPrefetch={prefetchGroupings}
            />
          </Card>

          <Card className="gap-0 overflow-hidden pt-4 pb-4 sm:pt-6">
            <CardHeader className="px-4 pb-4 sm:px-6">
              <CardTitle>{t("topTorrents.title")}</CardTitle>
              <CardDescription>{t("topTorrents.description")}</CardDescription>
            </CardHeader>
            {statusQuery.error ? (
              <div className="flex h-24 items-center justify-center px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: statusQuery.error.message })}</div>
            ) : ready ? (
              <TopTorrents scope={listScope} torrents={shown.torrents} error={top.error} period={shown.period} instances={selected} since={since} displayGroup={displayName} />
            ) : (
              <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
            )}
            <div className="space-y-1 px-4 pt-3 text-xs text-muted-foreground sm:px-6">
              <p>{t("topTorrents.note")}</p>
              <p>{t("topTorrents.seededNote")}</p>
              {!period.from && <p>{t("table.baselineNote")}</p>}
            </div>
          </Card>
        </div>

        <div className="grid gap-4 sm:gap-6 lg:grid-cols-2">
          <Card className="gap-4 py-4 sm:gap-6 sm:py-6">
            <CardHeader className="px-4 sm:px-6">
              <CardTitle>{t("growth.title")}</CardTitle>
              <CardDescription>{newestCount !== undefined ? t("growth.description", { torrents: newestCount.toLocaleString(locale) }) : t("growth.descriptionEmpty")}</CardDescription>
            </CardHeader>
            <CardContent className="px-4 sm:px-6">
              {growthError ? (
                <div className="flex h-[240px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: growthError.message })}</div>
              ) : !libraryPoints ? (
                <div className="flex h-[240px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
              ) : libraryPoints.length > 0 ? (
                <>
                  <LibraryGrowthChart points={libraryPoints} />
                  {libraryPoints.some(point => point.totalSize !== null && point.uniqueSize === null) && <p className="pt-2 text-xs text-muted-foreground">{t("growth.gapNote")}</p>}
                </>
              ) : (
                <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">{t("growth.empty")}</div>
              )}
            </CardContent>
          </Card>
          <Card className="gap-4 py-4 sm:gap-6 sm:py-6">
            <CardHeader className="px-4 sm:px-6">
              <CardTitle>{t("events.title")}</CardTitle>
              <CardDescription>{t("events.description")}</CardDescription>
            </CardHeader>
            <CardContent className="px-4 sm:px-6">
              {growthError ? (
                <div className="flex h-[240px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: growthError.message })}</div>
              ) : !eventDays ? (
                <div className="flex h-[240px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
              ) : eventDays.some(day => day.added + day.completed + day.deleted > 0) ? (
                <EventsChart days={eventDays} />
              ) : (
                <div className="flex h-[240px] items-center justify-center text-sm text-muted-foreground">{t("events.empty")}</div>
              )}
            </CardContent>
          </Card>
        </div>

        <Card className="gap-0 overflow-hidden pt-4 pb-4 sm:pt-6">
          <CardHeader className="px-4 pb-4 sm:px-6">
            <CardTitle>{t("deletes.title")}</CardTitle>
            <CardDescription>{t("deletes.description")}</CardDescription>
          </CardHeader>
          {statusQuery.error ? (
            <div className="flex h-24 items-center justify-center px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: statusQuery.error.message })}</div>
          ) : ready ? (
            <DeletedTorrents
              scope={listScope}
              instances={selected}
              period={period}
              displayGroup={displayName}
            />
          ) : (
            <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
          )}
        </Card>

        {!period.from && <p className="text-xs text-muted-foreground">{t("allTimeNote")}</p>}
      </div>
    </Page>
  )
}

function Tile({ label, value, hint, wide = false }: { label: string; value?: string; hint?: ReactNode; wide?: boolean }) {
  return (
    <Card className={cn(
      "gap-2 py-4 max-sm:gap-0 max-sm:rounded-none max-sm:border-x-0 max-sm:border-b-0 max-sm:bg-transparent max-sm:py-3 max-sm:shadow-none max-sm:nth-[-n+2]:border-t-0 max-sm:even:border-l max-sm:last:col-span-2 xl:col-span-1 sideways:col-span-1 sideways:last:col-span-1 sideways:gap-1 sideways:py-3 max-sm:sideways:border-t-0 max-sm:sideways:border-l max-sm:sideways:first:border-l-0",
      wide ? "sm:col-span-3" : "sm:col-span-2"
    )}>
      <CardContent className="px-4 max-sm:sideways:px-2">
        <div className="text-xs text-muted-foreground sm:text-sm sideways:text-xs">{label}</div>
        <div>
          <div className="mt-0.5 whitespace-nowrap text-lg font-semibold tabular-nums sm:mt-1 sm:text-2xl sideways:mt-0.5 sideways:text-lg max-sm:sideways:text-base">{value ?? "–"}</div>
          <div className="text-xs text-muted-foreground empty:hidden sm:mt-1 sm:min-h-4 sm:empty:block">{hint}</div>
        </div>
      </CardContent>
    </Card>
  )
}
