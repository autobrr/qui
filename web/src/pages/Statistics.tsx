/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { PageHeader } from "@/components/layout/PageHeader"
import { InstanceFilter, PeriodPicker, type InstanceOption, type PeriodChoice } from "@/components/statistics/StatisticsControls"
import { TransferChart } from "@/components/statistics/TransferChart"
import { Badge } from "@/components/ui/badge"
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { api } from "@/lib/api"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import {
  buildBars,
  chartRange,
  comparisonEnd,
  customPeriod,
  presetPeriod,
  previousPeriod,
  recordedFrom,
  recordingSince,
  sumBytes,
  topGroups,
  type Metric,
  type StatisticsPeriod
} from "@/lib/statistics"
import { formatBytes } from "@/lib/utils"
import type { TransferHistoryBytes, TransferHistoryInstance, TransferHistoryQuery, TransferHistoryRow } from "@/types"
import { useQueries, useQuery, type UseQueryResult } from "@tanstack/react-query"
import { Loader2 } from "lucide-react"
import { useCallback, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

// A browser that cannot find its zone reports Etc/Unknown. The days are then UTC days.
const BROWSER_TIME_ZONE = ((zone: string) => zone === "Etc/Unknown" ? undefined : zone)(Intl.DateTimeFormat().resolvedOptions().timeZone)

// Names that the backend gives to the Tracker groups without a tracker domain.
const PUBLIC_GROUP = "Public"
const NO_TRACKER_GROUP = "No tracker"

interface PeriodState {
  choice: PeriodChoice
  period: StatisticsPeriod
}

interface InstanceRows {
  rows?: TransferHistoryRow[][]
  error?: Error
  pending: boolean
}

/** Reads one transfer query for each instance. The rows are undefined while a query is pending or when no query runs. */
function useInstanceRows(instances: TransferHistoryInstance[], query: TransferHistoryQuery | undefined): InstanceRows {
  const enabled = query !== undefined
  const combine = useCallback((results: UseQueryResult<TransferHistoryRow[]>[]): InstanceRows => ({
    rows: enabled && results.every(result => result.data) ? results.map(result => result.data!) : undefined,
    error: results.find(result => result.error)?.error ?? undefined,
    pending: enabled && results.some(result => result.isPending),
  }), [enabled])
  return useQueries({
    queries: instances.map(instance => ({
      queryKey: ["transfer-history", instance.instanceId, query?.groupBy, query?.bucket, query?.timeZone, query?.from?.toISOString(), query?.to.toISOString()],
      queryFn: () => api.getTransferHistory(instance.instanceId, query!),
      enabled,
    })),
    combine,
  })
}

function relativeChange(now: number, before: number): number | undefined {
  return before > 0 ? (now - before) / before : undefined
}

export function Statistics() {
  const { t, i18n } = useTranslation("statistics")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const [incognitoMode] = useIncognitoMode()
  const [periodState, setPeriodState] = useState<PeriodState>(() => ({ choice: "30d", period: presetPeriod("30d", new Date()) }))
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [metric, setMetric] = useState<Metric>("uploaded")
  const { period } = periodState

  const statusQuery = useQuery({ queryKey: ["transfer-history", "status"], queryFn: () => api.getTransferHistoryStatus() })
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
  const chart = useInstanceRows(selected, ready ? { from: period.from, to: period.to, bucket: period.unit, groupBy: "tracker", timeZone: period.unit === "day" ? BROWSER_TIME_ZONE : undefined } : undefined)
  const allTime = useInstanceRows(selected, ready && !period.from ? { to: period.to, bucket: "total", groupBy: "tracker" } : undefined)
  const compared = useInstanceRows(selected, comparable ? { from: previous.to, to: comparisonEnd(period), bucket: "total", groupBy: "tracker" } : undefined)
  const before = useInstanceRows(selected, comparable ? { from: previous.from, to: previous.to, bucket: "total", groupBy: "tracker" } : undefined)

  const since = useMemo(() => recordingSince(selected), [selected])
  const partial = period.from !== undefined && selected.length > 0 && !recordedFrom(selected, period.from)
  const totals = useMemo<TransferHistoryBytes | undefined>(
    () => period.from ? chart.rows && sumBytes(chart.rows.flat(), false) : allTime.rows && sumBytes(allTime.rows.flat(), true),
    [period.from, chart.rows, allTime.rows]
  )
  const previousTotals = useMemo(() => before.rows && sumBytes(before.rows.flat(), false), [before.rows])
  const comparedTotals = useMemo(() => compared.rows && sumBytes(compared.rows.flat(), false), [compared.rows])
  const groups = useMemo(() => topGroups(chart.rows?.flat() ?? [], metric), [chart.rows, metric])
  const bars = useMemo(() => {
    const range = chartRange(period, since)
    return chart.rows && range ? buildBars(chart.rows, range, period.unit) : []
  }, [chart.rows, period, since])

  const displayName = useCallback((group: string) => {
    if (group === PUBLIC_GROUP) return t("groups.public")
    if (group === NO_TRACKER_GROUP) return t("groups.noTracker")
    return incognitoMode ? getLinuxTrackerDomain(group) : group
  }, [incognitoMode, t])

  const formatDay = (date: Date) => date.toLocaleDateString(locale, { day: "numeric", month: "short", year: "numeric" })
  const customLabel = periodState.choice === "custom" && period.from ? t("customRange.range", { from: formatDay(period.from), to: formatDay(new Date(period.to.getTime() - 1)) }) : undefined
  const formatChange = new Intl.NumberFormat(locale, { style: "percent", signDisplay: "exceptZero", maximumFractionDigits: 0 })
  const error = statusQuery.error ?? (chart.error ? undefined : allTime.error ?? compared.error ?? before.error)
  const loading = statusQuery.isPending || chart.pending || allTime.pending

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
            {options.length > 1 && <InstanceFilter options={options} selected={selectedIds} onChange={setSelectedIds} />}
            <PeriodPicker
              value={periodState.choice}
              customLabel={customLabel}
              onPreset={preset => setPeriodState({ choice: preset, period: presetPeriod(preset, new Date()) })}
              onCustom={(firstDay, lastDay) => setPeriodState({ choice: "custom", period: customPeriod(firstDay, lastDay, new Date()) })}
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
          value={totals && (totals.downloaded > 0 ? (totals.uploaded / totals.downloaded).toFixed(2) : totals.uploaded > 0 ? "∞" : undefined)}
          hint={totals && t("tiles.buffer", { buffer: `${totals.uploaded >= totals.downloaded ? "+" : "-"}${formatBytes(Math.abs(totals.uploaded - totals.downloaded))}` })}
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
            <TransferChart bars={bars} groups={groups} metric={metric} unit={period.unit} displayName={displayName} />
          ) : (
            <div className="flex h-[300px] items-center justify-center text-sm text-muted-foreground">{t("chart.empty")}</div>
          )}
        </CardContent>
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
