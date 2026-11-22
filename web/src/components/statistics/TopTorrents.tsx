/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Facts, type FactItem } from "@/components/statistics/Facts"
import { GroupChart } from "@/components/statistics/GroupChart"
import { RowLimit } from "@/components/statistics/RowLimit"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TorrentName } from "@/components/ui/torrent-name"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { useTorrentName } from "@/hooks/useTopTorrents"
import { useTrackerGroupIcon } from "@/hooks/useTrackerGroupName"
import { useTransferHistoryRows } from "@/hooks/useTransferHistoryRows"
import { buildTorrentBars, formatRatio, type StatisticsPeriod, type TopTorrent } from "@/lib/statistics"
import { cn, formatBytes } from "@/lib/utils"
import type { TransferHistoryInstance, TransferHistoryRow } from "@/types"
import { ChevronDown, ChevronRight, Link2, Loader2, Trash2, type LucideIcon } from "lucide-react"
import { Fragment, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

const PHONE_ROWS = 5

interface TopTorrentsProps {
  // A new scope, another period choice or instance selection, shows five rows on a phone again.
  scope: string
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
export function TopTorrents({ scope, torrents: shown, error, period, instances, since, displayGroup }: TopTorrentsProps) {
  const { t, i18n } = useTranslation("statistics")
  const torrentName = useTorrentName()
  const groupIcon = useTrackerGroupIcon()
  const formatSeeded = new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language, { minimumFractionDigits: 2, maximumFractionDigits: 2 })
  const seeded = (torrent: TopTorrent) => torrent.size > 0 ? t("table.seededValue", { value: formatSeeded.format(torrent.uploaded / torrent.size) }) : "–"
  const narrow = useIsPhone()
  const [open, setOpen] = useState<string>()
  const [allOf, setAllOf] = useState<string>()
  const all = allOf === scope
  if (allOf !== undefined && !all) {
    setAllOf(undefined)
  }
  const instanceNames = instances.length > 1 ? new Map(instances.map(instance => [instance.instanceId, instance.name])) : undefined

  if (error) {
    return <div className="flex h-24 items-center justify-center px-4 text-sm text-destructive sm:px-6">{t("errors.load", { message: error.message })}</div>
  }
  if (!shown) {
    return <div className="flex h-24 items-center justify-center"><Loader2 className="size-6 animate-spin text-muted-foreground" /></div>
  }
  if (shown.length === 0) {
    return <div className="flex h-24 items-center justify-center px-4 text-sm text-muted-foreground sm:px-6">{t("chart.empty")}</div>
  }
  return (
    <div className="@container border-t">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-full pl-4 sm:pl-6">{t("topTorrents.columns.torrent")}</TableHead>
            <TableHead className="hidden @2xl:table-cell">{t("table.columns.tracker")}</TableHead>
            <TableHead className="pr-4 text-right sm:pr-6 @xl:pr-2">{t("table.columns.uploaded")}</TableHead>
            <TableHead className="hidden text-right @3xl:table-cell">{t("table.columns.downloaded")}</TableHead>
            <TableHead className="hidden text-right @3xl:table-cell">{t("table.columns.ratio")}</TableHead>
            <TableHead className="hidden text-right @xl:table-cell @xl:pr-6 @2xl:pr-2">{t("table.columns.seeded")}</TableHead>
            <TableHead className="hidden pr-6 text-right @2xl:table-cell">{t("topTorrents.columns.first24h")}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {(narrow && !all ? shown.slice(0, PHONE_ROWS) : shown).map(torrent => {
            const key = `${torrent.instanceId}:${torrent.hash}`
            const expanded = open === key
            const name = torrentName(torrent)
            const tracker = `${displayGroup(torrent.trackerGroup)}${instanceNames ? ` · ${instanceNames.get(torrent.instanceId)}` : ""}`
            const icon = groupIcon(torrent.trackerGroup)
            const trackerIcon = icon ? <TrackerIconImage {...icon} /> : <span className="size-4 shrink-0" />
            return (
              <Fragment key={key}>
                <TableRow className={cn("cursor-pointer", expanded && "bg-muted hover:bg-muted")} onClick={() => setOpen(expanded ? undefined : key)}>
                  <TableCell className="w-full max-w-0 pl-4 font-medium sm:pl-6">
                    <div className="flex min-w-0 items-center gap-2">
                      <button type="button" aria-expanded={expanded} className="flex min-w-0 items-center gap-2 text-left">
                        {expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />}
                        <TorrentName name={name} openOnTap={false} className={expanded ? undefined : "line-clamp-2 @2xl:line-clamp-1"} />
                      </button>
                      <span className="hidden shrink-0 items-center gap-2 @2xl:flex"><TorrentMarks torrent={torrent} /></span>
                    </div>
                    <div className="flex min-w-0 items-center gap-2 pl-5.5 text-xs font-normal text-muted-foreground @2xl:hidden">
                      <span className="flex min-w-0 items-center gap-1.5">{trackerIcon}<TruncatedText>{tracker}</TruncatedText></span>
                      <TorrentMarks torrent={torrent} />
                    </div>
                    <Facts
                      items={[
                        [t("table.columns.size"), formatBytes(torrent.size)],
                        [t("table.columns.downloaded"), formatBytes(torrent.downloaded)],
                        [t("table.columns.ratio"), formatRatio(torrent) ?? "–"],
                        ...(torrent.uploadFirst24h === null ? [] : [[t("topTorrents.columns.first24h"), formatBytes(torrent.uploadFirst24h), "@2xl:hidden"] satisfies FactItem]),
                      ]}
                      className="@3xl:hidden"
                    />
                  </TableCell>
                  <TableCell className="hidden max-w-64 text-muted-foreground @2xl:table-cell"><span className="flex items-center gap-1.5">{trackerIcon}<TruncatedText>{tracker}</TruncatedText></span></TableCell>
                  <TableCell className="pr-4 text-right tabular-nums sm:pr-6 @xl:pr-2">
                    {formatBytes(torrent.uploaded)}
                    <div className="text-xs text-muted-foreground @xl:hidden" data-narrow>{seeded(torrent)}</div>
                  </TableCell>
                  <TableCell className="hidden text-right tabular-nums @3xl:table-cell">{formatBytes(torrent.downloaded)}</TableCell>
                  <TableCell className="hidden text-right tabular-nums @3xl:table-cell">{formatRatio(torrent) ?? "–"}</TableCell>
                  <TableCell className="hidden text-right tabular-nums @xl:table-cell @xl:pr-6 @2xl:pr-2">{seeded(torrent)}</TableCell>
                  <TableCell className="hidden pr-6 text-right tabular-nums text-muted-foreground @2xl:table-cell">{torrent.uploadFirst24h === null ? "–" : formatBytes(torrent.uploadFirst24h)}</TableCell>
                </TableRow>
                {expanded && (
                  <TableRow className="hover:bg-transparent">
                    <TableCell colSpan={7} className="p-0">
                      <TorrentDays torrent={torrent} name={name} instances={instances} period={period} since={since} />
                    </TableCell>
                  </TableRow>
                )}
              </Fragment>
            )
          })}
        </TableBody>
      </Table>
      {narrow && shown.length > PHONE_ROWS && <RowLimit shown={all ? shown.length : PHONE_ROWS} total={shown.length} onAll={all ? undefined : () => setAllOf(scope)} onFewer={all ? () => setAllOf(undefined) : undefined} />}
    </div>
  )
}

export function TorrentMarks({ torrent }: { torrent: TopTorrent }) {
  const { t } = useTranslation("statistics")
  return (
    <>
      {torrent.deleted && <TorrentMark icon={Trash2} label={t("topTorrents.deleted")} />}
      {torrent.statisticsCrossSeed && <TorrentMark icon={Link2} label={t("topTorrents.crossSeed")} />}
    </>
  )
}

function TorrentMark({ icon: Icon, label }: { icon: LucideIcon; label: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap="only">
        <span role="img" aria-label={label} tabIndex={0} className="inline-flex shrink-0 p-0.5 text-muted-foreground"><Icon className="size-3.5" /></span>
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
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
    <div className="sticky left-0 w-[100cqw] space-y-2 border-l-2 bg-muted/30 px-4 py-5 sm:px-6">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground">
        <span className="min-w-0 font-medium whitespace-normal text-foreground [overflow-wrap:anywhere]">{t("detail.chartDay", { group: name })}</span>
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
