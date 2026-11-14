/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PageHeader } from "@/components/layout/PageHeader"
import { GroupTable } from "@/components/statistics/GroupTable"
import { InstanceFilter, PeriodPicker, type InstanceOption, type PeriodChoice } from "@/components/statistics/StatisticsControls"
import { TopTorrents } from "@/components/statistics/TopTorrents"
import { useTopTorrents } from "@/hooks/useTopTorrents"
import { TransferChart } from "@/components/statistics/TransferChart"
import { Badge } from "@/components/ui/badge"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { api } from "@/lib/api"
import { getLinuxCategoryName, getLinuxReleaseGroup, getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import {
  BROWSER_TIME_ZONE,
  buildBars,
  chartRange,
  comparisonEnd,
  customPeriod,
  formatBuffer,
  formatRatio,
  groupTotals,
  presetPeriod,
  previousPeriod,
  recordedFrom,
  recordingSince,
  sumBytes,
  topGroups,
  type Metric,
  type StatisticsPeriod,
  type TopTorrent
} from "@/lib/statistics"
import { formatBytes } from "@/lib/utils"
import type { LiveTrackerGroup, TableGrouping, TransferHistoryBytes, TransferHistoryGrouping, TransferHistoryRow } from "@/types"
import { useQueries, useQuery, type UseQueryResult } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

// Names that the backend gives to the Tracker groups without a tracker domain.
const PUBLIC_GROUP = "Public"
const NO_TRACKER_GROUP = "No tracker"

interface PeriodState {
  choice: PeriodChoice
  period: StatisticsPeriod
  // Set when the page moved a preset period forward, so that the old numbers stay until the new ones load.
  moved: boolean
}

// qui writes the history each 5 minutes, so a faster move shows nothing new.
const MOVE_AFTER = 5 * 60_000

interface LiveGroups {
  groups?: LiveTrackerGroup[][]
  error?: Error
}

/** Joins the live numbers of the instances that loaded once no query is pending, so that one failed instance does not hide the others. */
function combineLiveGroups(results: UseQueryResult<LiveTrackerGroup[]>[]): LiveGroups {
  return {
    groups: results.every(result => !result.isPending) ? results.map(result => result.data ?? []) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
  }
}

/** The rows of the five queries of the page, for one period and one selection. */
interface PageRows {
  // The period choice and the selected instances.
  series: string
  period: StatisticsPeriod
  chart?: TransferHistoryRow[][]
  allTime?: TransferHistoryRow[][]
  compared?: TransferHistoryRow[][]
  before?: TransferHistoryRow[][]
  // The rows of the table when it does not group by Tracker group.
  grouped?: TransferHistoryRow[][]
  torrents?: TopTorrent[]
}

// The number of torrents of the Top torrents card.
const TOP_TORRENTS = 10

function relativeChange(now: number, before: number): number | undefined {
  return before > 0 ? (now - before) / before : undefined
}

export function Statistics() {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const [incognitoMode] = useIncognitoMode()
  const [periodState, setPeriodState] = useState<PeriodState>(() => ({ choice: "30d", period: presetPeriod("30d", new Date()), moved: false }))
  const { choice } = periodState

  useEffect(() => {
    if (choice === "custom") {
      return
    }
    const move = () => {
      if (document.visibilityState !== "visible") {
        return
      }
      setPeriodState(state => Date.now() - state.period.to.getTime() < MOVE_AFTER ? state : { choice, period: presetPeriod(choice, new Date()), moved: true })
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

  // An instance that starts recording while the page is open joins at the next move of the period.
  const statusQuery = useQuery({ queryKey: ["transfer-history", "status"], queryFn: () => api.getTransferHistoryStatus(), refetchInterval: MOVE_AFTER })
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

  // The live numbers describe the torrents in qBittorrent now, so a removed or disabled instance has none.
  const liveIds = useMemo(() => {
    const active = new Set((instancesQuery.data ?? []).filter(instance => instance.isActive).map(instance => instance.id))
    return selected.map(status => status.instanceId).filter(id => active.has(id))
  }, [instancesQuery.data, selected])
  const live = useQueries({
    queries: liveIds.map(instanceId => ({
      queryKey: ["tracker-groups", instanceId],
      queryFn: () => api.getTrackerGroups(instanceId),
      enabled: byTracker,
      refetchInterval: MOVE_AFTER,
    })),
    combine: combineLiveGroups,
  })

  // A new period gives new query keys, so the queries hold no previous data to show while they load. After a move,
  // the page shows the rows of the last complete load of the same choice and selection until each new query loaded,
  // so that the numbers never mix two periods or two selections.
  const series = `${periodState.choice}:${selected.map(instance => instance.instanceId).join(",")}:${grouping}`
  const complete = ready && chart.rows !== undefined && (period.from !== undefined || allTime.rows !== undefined) && (!comparable || (compared.rows !== undefined && before.rows !== undefined)) && (byTracker || grouped.rows !== undefined) && top.torrents !== undefined
  const [lastComplete, setLastComplete] = useState<PageRows>()
  if (complete && (lastComplete?.chart !== chart.rows || lastComplete?.allTime !== allTime.rows || lastComplete?.compared !== compared.rows || lastComplete?.before !== before.rows || lastComplete?.grouped !== grouped.rows || lastComplete?.torrents !== top.torrents)) {
    setLastComplete({ series, period, chart: chart.rows, allTime: allTime.rows, compared: compared.rows, before: before.rows, grouped: grouped.rows, torrents: top.torrents })
  }
  const keep = periodState.moved && !complete && !(chart.error ?? allTime.error ?? compared.error ?? before.error ?? grouped.error ?? top.error) && lastComplete?.series === series
  const shown: PageRows = keep && lastComplete ? lastComplete : { series, period, chart: chart.rows, allTime: allTime.rows, compared: compared.rows, before: before.rows, grouped: grouped.rows, torrents: top.torrents }

  const since = useMemo(() => recordingSince(selected), [selected])
  const partial = period.from !== undefined && selected.length > 0 && !recordedFrom(selected, period.from)
  const totals = useMemo<TransferHistoryBytes | undefined>(
    () => shown.period.from ? shown.chart && sumBytes(shown.chart.flat(), false) : shown.allTime && sumBytes(shown.allTime.flat(), true),
    [shown.period.from, shown.chart, shown.allTime]
  )
  const previousTotals = useMemo(() => shown.before && sumBytes(shown.before.flat(), false), [shown.before])
  const comparedTotals = useMemo(() => shown.compared && sumBytes(shown.compared.flat(), false), [shown.compared])
  const groups = useMemo(() => topGroups(shown.chart?.flat() ?? [], metric), [shown.chart, metric])
  const bars = useMemo(() => {
    const range = chartRange(shown.period, since)
    return shown.chart && range ? buildBars(shown.chart, range, shown.period.unit) : []
  }, [shown.chart, shown.period, since])

  // The tracker rows of the table add up the rows of the chart, so that they add up to the numbers at the top.
  const tableRows = useMemo(() => {
    const rows = byTracker ? shown.period.from ? shown.chart : shown.allTime : shown.grouped
    return rows && groupTotals(rows, !shown.period.from, byTracker ? live.groups : undefined)
  }, [byTracker, shown.grouped, shown.chart, shown.allTime, shown.period.from, live.groups])

  const displayName = useCallback((group: string) => {
    if (group === PUBLIC_GROUP) return t("groups.public")
    if (group === NO_TRACKER_GROUP) return t("groups.noTracker")
    return incognitoMode ? getLinuxTrackerDomain(group) : group
  }, [incognitoMode, t])
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
  const customLabel = periodState.choice === "custom" && period.from ? t("customRange.range", { from: formatDay(period.from), to: formatDay(new Date(period.to.getTime() - 1)) }) : undefined
  const formatChange = new Intl.NumberFormat(locale, { style: "percent", signDisplay: "exceptZero", maximumFractionDigits: 0 })
  const error = statusQuery.error ?? (chart.error ? undefined : allTime.error ?? compared.error ?? before.error)
  const loading = statusQuery.isPending || (!keep && (chart.pending || allTime.pending))

  const change = (key: Metric) => {
    const change = comparedTotals && previousTotals ? relativeChange(comparedTotals[key], previousTotals[key]) : undefined
    return change === undefined ? undefined : t("tiles.change", { change: formatChange.format(change) })
  }

  return (
    <div className="space-y-6 p-4 pb-16 lg:p-6">
      <PageHeader
        title={t("title")}
        description={
          <span className="inline-flex flex-wrap items-center gap-2">
            {since ? t("recordingSince", { date: formatDay(since) }) : !statusQuery.isPending && t("notRecording")}
            {partial && <Badge variant="outline" className="border-yellow-500/50 py-0 text-yellow-600 dark:text-yellow-500">{t("partial")}</Badge>}
          </span>
        }
        actions={
          <>
            {options.length > 1 && <InstanceFilter
              options={options}
              selected={selectedIds}
              onChange={setSelectedIds}
            />}
            <PeriodPicker
              value={periodState.choice}
              customLabel={customLabel}
              onPreset={preset => setPeriodState({ choice: preset, period: presetPeriod(preset, new Date()), moved: false })}
              onCustom={(firstDay, lastDay) => setPeriodState({ choice: "custom", period: customPeriod(firstDay, lastDay, new Date()), moved: false })}
            />
          </>
        }
      />

      {error && <p className="text-sm text-destructive">{t("errors.load", { message: error.message })}</p>}

      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
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
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>{t("chart.title")}</CardTitle>
          <CardDescription>{period.unit === "hour" ? t("chart.descriptionHour") : t("chart.descriptionDay")}</CardDescription>
          <CardAction>
            <Tabs value={metric} onValueChange={value => setMetric(value as Metric)}>
              <TabsList>
                <TabsTrigger value="uploaded" className="px-3">{t("chart.upload")}</TabsTrigger>
                <TabsTrigger value="downloaded" className="px-3">{t("chart.download")}</TabsTrigger>
              </TabsList>
            </Tabs>
          </CardAction>
        </CardHeader>
        <CardContent>
          {chart.error ? (
            <div className="flex h-[300px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: chart.error.message })}</div>
          ) : loading ? (
            <div className="flex h-[300px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
          ) : bars.some(bar => bar.bytes.size > 0) ? (
            <TransferChart bars={bars} groups={groups} metric={metric} unit={shown.period.unit} displayName={displayName} />
          ) : (
            <div className="flex h-[300px] items-center justify-center text-sm text-muted-foreground">{t("chart.empty")}</div>
          )}
        </CardContent>
      </Card>

      <Card className="gap-0 overflow-hidden pb-4">
        <CardHeader className="pb-4">
          <CardTitle>{t("table.title")}</CardTitle>
          <CardDescription>{grouping === "tracker" ? t("table.descriptionTracker") : grouping === "category" ? t("table.descriptionCategory") : t("table.descriptionRelease")}</CardDescription>
        </CardHeader>
        <GroupTable
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
        />
      </Card>

      <Card className="gap-0 overflow-hidden pb-4">
        <CardHeader className="pb-4">
          <CardTitle>{t("topTorrents.title")}</CardTitle>
          <CardDescription>{t("topTorrents.description")}</CardDescription>
        </CardHeader>
        {statusQuery.error ? (
          <div className="flex h-24 items-center justify-center px-6 text-sm text-destructive">{t("errors.load", { message: statusQuery.error.message })}</div>
        ) : ready ? (
          <TopTorrents torrents={shown.torrents} error={top.error} period={shown.period} instances={selected} since={since} displayGroup={displayName} />
        ) : (
          <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
        )}
        <div className="space-y-1 px-6 pt-3 text-xs text-muted-foreground">
          <p>{t("topTorrents.note")}</p>
          {!period.from && <p>{t("table.baselineNote")}</p>}
        </div>
      </Card>

      <div className="space-y-1 text-xs text-muted-foreground">
        {!period.from && <p>{t("allTimeNote")}</p>}
        <p>{t("serverStatisticsNote")}</p>
      </div>
    </div>
  )
}

function Tile({ label, value, hint }: { label: string; value?: string; hint?: ReactNode }) {
  return (
    <Card className="gap-2 py-4">
      <CardContent className="px-4">
        <div className="text-sm text-muted-foreground">{label}</div>
        <div className="mt-1 whitespace-nowrap text-2xl font-semibold tabular-nums">{value ?? "–"}</div>
        <div className="mt-1 min-h-4 text-xs text-muted-foreground">{hint}</div>
      </CardContent>
    </Card>
  )
}
