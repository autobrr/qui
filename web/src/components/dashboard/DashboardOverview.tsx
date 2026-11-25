/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { InstanceList, type InstanceDay } from "@/components/dashboard/InstanceList"
import { PointedLayerContext, PointLayerContext, usePointedLayer, usePointLayer } from "@/components/dashboard/pointed-layer"
import { Library, NeedsAttention, Storage } from "@/components/dashboard/SidePanels"
import { StreamChart, type StreamChartProps } from "@/components/dashboard/StreamChart"
import { TopTrackerGroups } from "@/components/dashboard/TopTrackerGroups"
import { Badge } from "@/components/ui/badge"
import { Card } from "@/components/ui/card"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDashboardChartView, type DashboardChartView } from "@/hooks/useDashboardChartView"
import { useLiveSamples } from "@/hooks/useLiveSamples"
import { usePersistedDateTimePreferences } from "@/hooks/usePersistedDateTimePreferences"
import { useClientSettingsSettled } from "@/lib/client-settings"
import { useTransferHistoryRows, useTransferHistoryStatus } from "@/hooks/useTransferHistoryRows"
import {
  buildDayChart,
  buildLiveLayers,
  instanceLayers,
  layerPeaks,
  LIVE_SLOTS,
  trackerGroupShares,
  type DashboardInstanceStats
} from "@/lib/dashboard-overview"
import { HISTORY_WRITE_MS, presetPeriod, recordedFrom, sumBytes, type StatisticsPeriod } from "@/lib/statistics"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { themeArea } from "@/lib/theme-areas"
import { formatBytes } from "@/lib/utils"
import type { TransferHistoryRow } from "@/types"
import { ArrowDown, ArrowUp, Loader2 } from "lucide-react"
import { useEffect, useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

function useLastDayPeriod(): StatisticsPeriod {
  const [period, setPeriod] = useState(() => presetPeriod("24h", new Date()))
  useEffect(() => {
    const move = () => {
      if (document.visibilityState !== "visible") {
        return
      }
      setPeriod(current => Date.now() - current.to.getTime() < HISTORY_WRITE_MS ? current : presetPeriod("24h", new Date()))
    }
    const timer = setInterval(move, 60_000)
    document.addEventListener("visibilitychange", move)
    return () => {
      clearInterval(timer)
      document.removeEventListener("visibilitychange", move)
    }
  }, [])
  return period
}

function PointedLayerProvider({ children }: { children: ReactNode }) {
  const [pointed, setPointed] = useState<number>()
  return (
    <PointLayerContext.Provider value={setPointed}>
      <PointedLayerContext.Provider value={pointed}>{children}</PointedLayerContext.Provider>
    </PointLayerContext.Provider>
  )
}

function PointedChart(props: StreamChartProps) {
  return <StreamChart {...props} pointed={usePointedLayer()} onPoint={usePointLayer()} />
}

interface ShownDay {
  period: StatisticsPeriod
  rows: TransferHistoryRow[][]
  instanceIds: number[]
}

export function DashboardOverview({ statsData }: { statsData: DashboardInstanceStats[] }) {
  const { t, i18n } = useTranslation("dashboard")
  const locale = i18n.resolvedLanguage ?? i18n.language
  const { preferences } = usePersistedDateTimePreferences()
  const hour12 = preferences.timeFormat === "12h"
  const [unit] = useSpeedUnits()

  const statusQuery = useTransferHistoryStatus()
  const statuses = useMemo(() => statusQuery.data ?? [], [statusQuery.data])
  const idsKey = statsData.map(d => d.instance.id).join(",")
  const instanceIds = useMemo(() => idsKey ? idsKey.split(",").map(Number) : [], [idsKey])
  const hasHistory = statuses.some(status => instanceIds.includes(status.instanceId))

  const period = useLastDayPeriod()
  const day = useTransferHistoryRows(hasHistory ? statuses : [], hasHistory ? { from: period.from, to: period.to, bucket: "hour", groupBy: "tracker" } : undefined)
  const [shown, setShown] = useState<ShownDay>()
  if (day.rows && shown?.rows !== day.rows) {
    setShown({ period, rows: day.rows, instanceIds: statuses.map(status => status.instanceId) })
  }
  const failed = statusQuery.error ?? day.error
  const current = failed ? undefined : shown
  const noHistory = statusQuery.isSuccess && (!hasHistory || (current !== undefined && current.rows.every((rows, index) => rows.length === 0 || !instanceIds.includes(current.instanceIds[index]))))

  const dayData = useMemo(() => {
    if (!current?.period.from) return undefined
    const range = { from: current.period.from, to: current.period.to }
    const own = buildDayChart(current.rows, current.instanceIds, new Map(current.instanceIds.map((id, index) => [id, index])), range)
    const days = new Map<number, InstanceDay>(current.instanceIds.map((id, index) => {
      const hours = own.layers.find(layer => layer.layer === index) ?? { layer: index, up: [], down: [] }
      return [id, { ...sumBytes(current.rows[index], false), hours }]
    }))
    return {
      range,
      days,
      totals: sumBytes(current.rows.flat(), false),
      shares: trackerGroupShares(current.rows.flat()),
      partial: !recordedFrom(statuses.filter(status => current.instanceIds.includes(status.instanceId)), current.period.from),
    }
  }, [current, statuses])
  const layerOf = useMemo(() => instanceLayers(instanceIds), [instanceIds])
  const dayChart = useMemo(() => current && dayData && buildDayChart(current.rows, current.instanceIds, layerOf, dayData.range), [current, dayData, layerOf])

  const [chosen, setChosen] = useDashboardChartView()
  const settingsLoaded = useClientSettingsSettled()
  const range: DashboardChartView = noHistory ? "live" : chosen
  const live = useLiveSamples(settingsLoaded && range === "live")
  const liveLayers = useMemo(() => live.data && !live.error ? buildLiveLayers(live.data, layerOf) : [], [live.data, live.error, layerOf])

  const totalUp = statsData.reduce((sum, d) => sum + (d.stats?.totalUploadSpeed ?? 0), 0)
  const totalDown = statsData.reduce((sum, d) => sum + (d.stats?.totalDownloadSpeed ?? 0), 0)
  const seeding = statsData.reduce((sum, d) => sum + (d.stats?.seeding ?? 0), 0)
  const downloading = statsData.reduce((sum, d) => sum + (d.stats?.downloading ?? 0), 0)
  const peers = statsData.reduce((sum, d) => sum + (d.serverState?.total_peer_connections ?? 0), 0)
  const remaining = statsData.reduce((sum, d) => sum + (d.stats?.totalRemainingSize ?? 0), 0)
  const percent = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 })
  const timeFormat = useMemo(() => {
    const options: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", hour12 }
    try {
      return new Intl.DateTimeFormat(locale, { ...options, timeZone: preferences.timezone })
    } catch {
      return new Intl.DateTimeFormat(locale, options)
    }
  }, [locale, hour12, preferences.timezone])
  const clock = (time: number) => timeFormat.format(time)

  const totals = dayData?.totals
  const upCaption = range === "day"
    ? totals && t("stream.dayUploaded", { size: formatBytes(totals.uploaded), percent: percent.format(totals.uploaded > 0 ? totals.statisticsCrossSeedUploaded / totals.uploaded : 0) })
    : t("stream.liveSeeding", { seeding: seeding.toLocaleString(locale), peers: peers.toLocaleString(locale) })
  const downCaption = range === "day"
    ? totals && t("stream.dayDownloaded", { size: formatBytes(totals.downloaded) })
    : t("stream.liveDownloading", { downloading: downloading.toLocaleString(locale), size: formatBytes(remaining) })

  const hours = dayChart?.hours ?? []
  const shownLayers = !settingsLoaded ? undefined : range === "day" ? dayChart?.layers : liveLayers.length > 0 ? liveLayers : undefined
  const peaks = shownLayers && layerPeaks(shownLayers)
  const peakUp = peaks && (range === "day" ? t("stream.peakHour", { size: formatBytes(peaks.up) }) : t("stream.peakSpeed", { speed: formatSpeedWithUnit(peaks.up, unit) }))
  const peakDown = peaks && (range === "day" ? t("stream.peakHour", { size: formatBytes(peaks.down) }) : t("stream.peakSpeed", { speed: formatSpeedWithUnit(peaks.down, unit) }))

  const chartError = range === "live" ? live.error : failed
  let chart: ReactNode
  if (!settingsLoaded) {
    chart = (
      <div className="flex h-36 items-center justify-center lg:h-56">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  } else if (range === "live" && !chartError) {
    chart = <PointedChart layers={liveLayers} kind="areas" slots={LIVE_SLOTS} label={t("stream.liveLabel")} className="h-36 lg:h-56" />
  } else if (range === "day" && dayChart) {
    chart = <PointedChart layers={dayChart.layers} kind="bars" gaps={hours.map(hour => hour.gapSeconds)} label={t("stream.dayLabel")} className="h-36 lg:h-56" />
  } else {
    chart = (
      <div className="flex h-36 items-center justify-center text-sm lg:h-56">
        {chartError ? <span className="text-destructive">{t("stream.loadError", { message: chartError.message })}</span> : <Loader2 className="size-6 animate-spin text-muted-foreground" />}
      </div>
    )
  }

  const axis = range === "day"
    ? hours.length > 0 && [clock(hours[0].start), clock(hours[Math.floor(hours.length / 2)].start), clock(hours[hours.length - 1].start + 3_600_000)]
    : [t("stream.liveStart"), t("stream.now")]

  const dayTrigger = <TabsTrigger value="day" disabled={noHistory || !settingsLoaded} className="px-2.5 text-xs">{t("stream.day")}</TabsTrigger>
  const controls = (
    <Tabs value={range} onValueChange={value => setChosen(value as DashboardChartView)}>
      <TabsList className="h-9 lg:h-7">
        {noHistory ? (
          <Tooltip>
            <TooltipTrigger asChild><span tabIndex={0}>{dayTrigger}</span></TooltipTrigger>
            <TooltipContent className="max-w-xs">{t("stream.noHistory")}</TooltipContent>
          </Tooltip>
        ) : dayTrigger}
        <TabsTrigger value="live" disabled={!settingsLoaded} className="px-2.5 text-xs">{t("stream.live")}</TabsTrigger>
      </TabsList>
    </Tabs>
  )

  return (
    <PointedLayerProvider>
      <div className="flex flex-col gap-6 xl:grid xl:grid-cols-[minmax(0,1fr)_19rem] xl:items-start">
        <Card {...themeArea("dashboard-card")} className="order-1 gap-0 overflow-hidden py-0 xl:order-none xl:col-start-1 xl:row-start-1">
          <div className="flex items-start justify-between gap-3 px-4 pb-2 pt-4 lg:px-6">
            <div className="flex min-w-0 flex-wrap items-end gap-x-10 gap-y-2">
              <div className="min-w-0">
                <div className="flex items-center gap-1 text-3xl font-semibold tabular-nums tracking-tight lg:text-4xl" aria-label={t("stream.uploadSpeed")}>
                  <ArrowUp className="size-6 shrink-0 text-muted-foreground" aria-hidden="true" />{formatSpeedWithUnit(totalUp, unit)}
                </div>
                <div className="line-clamp-2 min-h-4 text-xs text-muted-foreground lg:line-clamp-1">
                  <span data-testid="upload-caption">{upCaption}</span>{peakUp && <> · <span>{peakUp}</span></>}
                </div>
              </div>
              <div className="min-w-0">
                <div className="flex items-center gap-1 text-3xl font-semibold tabular-nums tracking-tight lg:text-4xl" aria-label={t("stream.downloadSpeed")}>
                  <ArrowDown className="size-6 shrink-0 text-muted-foreground" aria-hidden="true" />{formatSpeedWithUnit(totalDown, unit)}
                </div>
                <div className="line-clamp-2 min-h-4 text-xs text-muted-foreground lg:line-clamp-1">
                  <span data-testid="download-caption">{downCaption}</span>{peakDown && <> · <span>{peakDown}</span></>}
                </div>
              </div>
            </div>
            <div className="flex shrink-0 items-center gap-2">
              {range === "day" && dayData?.partial && <Badge variant="outline" className="border-yellow-500/50 py-0 text-[10px] text-yellow-600 dark:text-yellow-500">{t("statistics:partial")}</Badge>}
              {controls}
            </div>
          </div>
          {chart}
          {axis && (
            <div className="flex justify-between px-4 pb-3 pt-1 text-[10px] text-muted-foreground tabular-nums lg:px-6">
              {axis.map((label, index) => <span key={index}>{label}</span>)}
            </div>
          )}
        </Card>

        <div className="contents xl:col-start-2 xl:row-span-3 xl:row-start-1 xl:block xl:space-y-6">
          <NeedsAttention statsData={statsData} className="order-2 xl:order-none" />
          <Storage statsData={statsData} className="order-5 xl:order-none" />
          <Library statsData={statsData} className="order-6 xl:order-none" />
        </div>

        <Card {...themeArea("dashboard-card")} className="order-3 gap-0 overflow-hidden py-0 xl:order-none xl:col-start-1 xl:row-start-2">
          <InstanceList statsData={statsData} layerOf={layerOf} days={noHistory ? undefined : dayData?.days} dayUploaded={dayData?.totals.uploaded} />
        </Card>

        {!noHistory && dayData && <TopTrackerGroups shares={dayData.shares} className="order-4 xl:order-none xl:col-start-1 xl:row-start-3" />}
      </div>
    </PointedLayerProvider>
  )
}
