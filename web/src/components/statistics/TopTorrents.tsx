/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { GroupChart } from "@/components/statistics/GroupChart"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useTorrentName } from "@/hooks/useTopTorrents"
import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { buildTorrentBars, formatRatio, type StatisticsPeriod, type TopTorrent } from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import type { TransferHistoryInstance, TransferHistoryRow } from "@/types"
import { ChevronDown, ChevronRight, Loader2 } from "lucide-react"
import { Fragment, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

interface TopTorrentsProps {
  // Undefined while the rows load.
  torrents?: TopTorrent[]
  error?: Error
  // The period of the rows, which an open row draws.
  period: StatisticsPeriod
  instances: TransferHistoryInstance[]
  since?: Date
  displayGroup: (group: string) => string
}

/** The torrents with the most upload in the period. A click on a row shows the upload of that torrent for each day. */
export function TopTorrents({ torrents: shown, error, period, instances, since, displayGroup }: TopTorrentsProps) {
  const { t } = useTranslation("statistics")
  const torrentName = useTorrentName()
  const [open, setOpen] = useState<string>()
  const instanceNames = instances.length > 1 ? new Map(instances.map(instance => [instance.instanceId, instance.name])) : undefined

  if (error) {
    return <div className="flex h-24 items-center justify-center px-6 text-sm text-destructive">{t("errors.load", { message: error.message })}</div>
  }
  if (!shown) {
    return <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
  }
  if (shown.length === 0) {
    return <div className="flex h-24 items-center justify-center px-6 text-sm text-muted-foreground">{t("chart.empty")}</div>
  }
  return (
    <div className="@container border-t">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-full pl-6">{t("topTorrents.columns.torrent")}</TableHead>
            <TableHead className="hidden @2xl:table-cell">{t("table.columns.tracker")}</TableHead>
            <TableHead className="text-right">{t("table.columns.uploaded")}</TableHead>
            <TableHead className="hidden text-right @xl:table-cell">{t("table.columns.downloaded")}</TableHead>
            <TableHead className="hidden text-right @xl:table-cell">{t("table.columns.ratio")}</TableHead>
            <TableHead className="hidden pr-6 text-right @2xl:table-cell">{t("topTorrents.columns.first24h")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {shown.map(torrent => {
            const key = `${torrent.instanceId}:${torrent.hash}`
            const expanded = open === key
            const name = torrentName(torrent)
            return (
              <Fragment key={key}>
                <TableRow className={cn("cursor-pointer", expanded && "bg-muted hover:bg-muted")} onClick={() => setOpen(expanded ? undefined : key)}>
                  <TableCell className="w-full max-w-0 pl-6 font-medium">
                    <button type="button" aria-expanded={expanded} className="flex w-full min-w-0 items-center gap-2 text-left">
                      {expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
                      <span className="line-clamp-2 break-all whitespace-normal @2xl:line-clamp-1" title={name}>{name}</span>
                      <span className="hidden shrink-0 items-center gap-2 @2xl:flex"><TorrentMarks torrent={torrent} /></span>
                    </button>
                    <div className="flex min-w-0 items-center gap-2 pl-5.5 text-xs font-normal text-muted-foreground @2xl:hidden">
                      <span className="truncate">{displayGroup(torrent.trackerGroup)}{instanceNames && ` · ${instanceNames.get(torrent.instanceId)}`}</span>
                      <TorrentMarks torrent={torrent} />
                    </div>
                    {instanceNames && <div className="hidden truncate pl-5.5 text-xs font-normal text-muted-foreground @2xl:block">{instanceNames.get(torrent.instanceId)}</div>}
                  </TableCell>
                  <TableCell className="hidden max-w-48 truncate text-muted-foreground @2xl:table-cell">{displayGroup(torrent.trackerGroup)}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(torrent.uploaded)}</TableCell>
                  <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatBytes(torrent.downloaded)}</TableCell>
                  <TableCell className="hidden text-right tabular-nums @xl:table-cell">{formatRatio(torrent) ?? "–"}</TableCell>
                  <TableCell className="hidden pr-6 text-right tabular-nums text-muted-foreground @2xl:table-cell">{torrent.uploadFirst24h === null ? "–" : formatBytes(torrent.uploadFirst24h)}</TableCell>
                </TableRow>
                {expanded && (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={6} className="p-0">
                      <TorrentDays torrent={torrent} name={name} instances={instances} period={period} since={since} />
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}

export function TorrentMarks({ torrent }: { torrent: TopTorrent }) {
  const { t } = useTranslation("statistics")
  return (
    <>
      {torrent.deleted && <Badge variant="outline" className="shrink-0 py-0 text-[10px] font-normal text-muted-foreground">{t("topTorrents.deleted")}</Badge>}
      {torrent.statisticsCrossSeed && <Badge variant="outline" className="shrink-0 py-0 text-[10px] font-normal text-muted-foreground">{t("topTorrents.crossSeed")}</Badge>}
    </>
  )
}

interface TorrentDaysProps {
  torrent: TopTorrent
  name: string
  instances: TransferHistoryInstance[]
  period: StatisticsPeriod
  since?: Date
}

/** The band below an open torrent: its upload and download for each UTC day of the period. */
function TorrentDays({ torrent, name, instances, period, since }: TorrentDaysProps) {
  const { t } = useTranslation("statistics")
  const instance = useMemo(() => instances.filter(candidate => candidate.instanceId === torrent.instanceId), [instances, torrent.instanceId])
  const transfer = useTransferHistoryRows(instance, { from: period.from, to: period.to, bucket: "day", groupBy: "torrent", group: torrent.hash })

  // After the period moves, the band keeps its chart until the rows of the new period load.
  const [last, setLast] = useState<{ rows: TransferHistoryRow[][]; period: StatisticsPeriod }>()
  if (transfer.rows && last?.rows !== transfer.rows) {
    setLast({ rows: transfer.rows, period })
  }
  const bars = useMemo(() => {
    const shown = transfer.rows ? { rows: transfer.rows, period } : last
    return shown && buildTorrentBars(shown.rows[0] ?? [], shown.period, since)
  }, [transfer.rows, period, last, since])

  return (
    <div className="sticky left-0 w-[100cqw] space-y-2 border-l-2 bg-muted/30 px-6 py-5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="min-w-0 truncate font-medium text-foreground">{t("detail.chartDay", { group: name })}</span>
        <span>{t("detail.chartLegend")}</span>
      </div>
      {transfer.error ? (
        <div className="flex h-[200px] items-center justify-center text-sm text-destructive">{t("errors.load", { message: transfer.error.message })}</div>
      ) : !bars ? (
        <div className="flex h-[200px] items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
      ) : (
        <>
          <GroupChart bars={bars} unit="day" color={undefined} />
          <p className="text-xs text-muted-foreground">{bars.some(bar => bar.width === "year") ? t("topTorrents.yearNote") : t("topTorrents.utcNote")}</p>
        </>
      )}
    </div>
  )
}
