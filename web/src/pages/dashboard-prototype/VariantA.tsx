/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant A, "Ledger": one table, an instance is one row.
// Phone: a two-line row for each instance, the details open in place.
// Desktop: every figure of every instance in one scan, no cards.

import { Button } from "@/components/ui/button"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { cn, formatBytes, formatDuration, formatDurationCompact, getRatioColor } from "@/lib/utils"
import { Link, useNavigate } from "@tanstack/react-router"
import { ArrowDown, ArrowUp, ChevronDown, Plus, Rabbit, Settings2, Turtle } from "lucide-react"
import { useState, type ReactNode } from "react"
import {
  AddTorrentMenu, HEALTH_DOT, IssueChip, Sparkline, freeShare, healthOf, isLowDisk, issuesOf, num, offlineReason, ratioOf,
  stubAction, sumSeries, trackerRatio, useInstanceExtras, useSpeedHistory, useTrackerRows,
  type History, type Row, type VariantProps
} from "./shared"

export const name = "Ledger"

function details(d: Row, extras: ReturnType<typeof useInstanceExtras>, unit: SpeedUnit): Array<[string, string]> {
  const s = d.serverState
  const status = d.torrentCounts?.status
  const rows: Array<[string, string | undefined | null | false]> = [
    ["Host", d.instance.host],
    ["Connection", d.instance.connectionStatus ?? s?.connection_status],
    ["Seeding", num(d.stats?.seeding)],
    ["Stopped", num(status?.stopped)],
    ["Session down", s && formatBytes(s.dl_info_data)],
    ["Session up", s && formatBytes(s.up_info_data)],
    ["All-time down", s?.alltime_dl !== undefined && formatBytes(s.alltime_dl)],
    ["All-time up", s?.alltime_ul !== undefined && formatBytes(s.alltime_ul)],
    ["Peers", s?.total_peer_connections !== undefined && num(s.total_peer_connections)],
    ["DHT nodes", s && num(s.dht_nodes)],
    ["Queued I/O jobs", s?.queued_io_jobs !== undefined && num(s.queued_io_jobs)],
    ["Buffer size", s?.total_buffers_size !== undefined && formatBytes(s.total_buffers_size)],
    ["Rate limits", s && `${formatSpeedWithUnit(s.dl_rate_limit, unit)} down, ${formatSpeedWithUnit(s.up_rate_limit, unit)} up`],
    ["External IPv4", s?.last_external_address_v4],
    ["Uptime", extras.uptime !== null && formatDuration(extras.uptime)],
    ["qBittorrent", extras.version],
    ["Web API", extras.webAPI],
    ["libtorrent", extras.libtorrent],
  ]
  return rows.filter((r): r is [string, string] => Boolean(r[1]))
}

const MOBILE_DETAILS = new Set(["Seeding", "Stopped", "Session down", "Session up", "Peers", "Uptime", "qBittorrent", "Host"])

function DetailList({ items, className }: { items: Array<[string, string]>; className?: string }) {
  return (
    <dl className={cn("grid gap-x-6 gap-y-1.5 text-xs", className)}>
      {items.map(([label, value]) => (
        <div key={label} className="flex min-w-0 justify-between gap-3">
          <dt className="shrink-0 text-muted-foreground">{label}</dt>
          <dd className="truncate text-right tabular-nums" title={value}>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function FreeBar({ d }: { d: Row }) {
  const share = freeShare(d)
  if (share === null) return null
  return (
    <div className="ml-auto mt-1 h-0.5 w-14 bg-muted" title={`${Math.round(share * 100)}% free beside the torrents`}>
      <div className={cn("ml-auto h-full", isLowDisk(d) ? "bg-destructive" : "bg-foreground/45")} style={{ width: `${Math.max(3, share * 100)}%` }} />
    </div>
  )
}

function AltSpeedButton({ d, className }: { d: Row; className?: string }) {
  const Icon = d.altSpeedEnabled ? Turtle : Rabbit
  return (
    <button
      type="button"
      title={`Alternative speed limits ${d.altSpeedEnabled ? "on" : "off"}`}
      onClick={(e) => { e.stopPropagation(); stubAction("Alternative speed limits") }}
      className={cn("grid place-items-center rounded-md hover:bg-muted", d.altSpeedEnabled ? "text-orange-500" : "text-muted-foreground", className)}
    >
      <Icon className="size-4" />
    </button>
  )
}

function Speed({ value, unit, history }: { value: number | undefined; unit: SpeedUnit; history?: number[] }) {
  return (
    <span className="inline-flex items-center justify-end gap-2">
      {history && <Sparkline values={history} className={cn("h-4 w-12", value ? "text-foreground/60" : "text-muted-foreground/40")} />}
      <span className={cn("min-w-[5.5rem] text-right", !value && "text-muted-foreground/60")}>{formatSpeedWithUnit(value || 0, unit)}</span>
    </span>
  )
}

function DesktopRow({ d, history, unit, open, onToggle }: { d: Row; history?: History; unit: SpeedUnit; open: boolean; onToggle: () => void }) {
  const navigate = useNavigate()
  const extras = useInstanceExtras(d)
  const health = healthOf(d)
  const issues = issuesOf(d)
  const ratio = ratioOf(d)
  const openInstance = () => void navigate({ to: "/instances/$instanceId", params: { instanceId: String(d.instance.id) } })

  const nameCell = (
    <td className="py-2.5 pl-2 pr-3">
      <span className="flex items-center gap-2.5">
        <span className={cn("size-2 shrink-0 rounded-full", HEALTH_DOT[health], health === "ok" && d.instance.connectionStatus === "firewalled" && "bg-amber-500")} />
        <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} className="truncate font-medium hover:underline" onClick={e => e.stopPropagation()}>
          {d.instance.name}
        </Link>
      </span>
    </td>
  )

  if (health !== "ok") {
    return (
      <tr className="border-b border-border/60">
        {nameCell}
        <td colSpan={12} className="px-2 py-2.5 text-sm">
          {health === "loading" ? <span className="animate-pulse text-muted-foreground">Loading</span> : (
            <span className="flex items-center gap-3">
              <span className="text-destructive">Offline: {offlineReason(d)}</span>
              <Link to="/settings" search={{ tab: "instances" as const }} className="text-muted-foreground underline underline-offset-2 hover:text-foreground">Open settings</Link>
            </span>
          )}
        </td>
      </tr>
    )
  }

  return (
    <>
      <tr onClick={openInstance} className="cursor-pointer border-b border-border/60 hover:bg-muted/40 [&>td]:px-2">
        {nameCell}
        <td className="text-right"><Speed value={d.stats?.totalDownloadSpeed} unit={unit} history={history?.dl} /></td>
        <td className="text-right"><Speed value={d.stats?.totalUploadSpeed} unit={unit} history={history?.ul} /></td>
        <td className="text-right">{num(d.torrentCounts?.total)}</td>
        <td className="text-right">
          {num(d.torrentCounts?.status?.active)}
          {(d.torrentCounts?.status?.downloading || 0) > 0 && <span className="text-muted-foreground"> ({num(d.torrentCounts?.status?.downloading)} down)</span>}
        </td>
        <td>
          {issues.length ? <span className="flex flex-wrap gap-0.5">{issues.map(i => <IssueChip key={i.key} issue={i} />)}</span> : <span className="text-muted-foreground/50">None</span>}
        </td>
        <td className="text-right">{formatBytes(d.stats?.totalSize || 0)}</td>
        <td className="py-1.5 text-right">
          <span className={cn(isLowDisk(d) && "font-medium text-destructive")}>{d.serverState?.free_space_on_disk !== undefined ? formatBytes(d.serverState.free_space_on_disk) : ""}</span>
          <FreeBar d={d} />
        </td>
        <td className="text-right font-medium" style={{ color: getRatioColor(ratio) }}>{ratio.toFixed(2)}</td>
        <td className="hidden text-right xl:table-cell">{num(d.serverState?.total_peer_connections)}</td>
        <td className="hidden text-right text-muted-foreground xl:table-cell">{extras.uptime !== null ? formatDurationCompact(extras.uptime) : ""}</td>
        <td className="hidden text-muted-foreground 2xl:table-cell">{extras.version}</td>
        <td>
          <span className="flex items-center justify-end">
            <AltSpeedButton d={d} className="size-7" />
            <Link
              to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} search={{ modal: "add-torrent" }}
              onClick={e => e.stopPropagation()} title={`Add torrent to ${d.instance.name}`}
              className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <Plus className="size-4" />
            </Link>
            <button
              type="button" aria-expanded={open} title="Details"
              onClick={(e) => { e.stopPropagation(); onToggle() }}
              className="grid size-7 place-items-center rounded-md text-muted-foreground hover:bg-muted hover:text-foreground"
            >
              <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
            </button>
          </span>
        </td>
      </tr>
      {open && (
        <tr className="border-b border-border/60 bg-muted/30">
          <td colSpan={13} className="px-7 py-3">
            <DetailList items={details(d, extras, unit)} className="grid-cols-3 xl:grid-cols-4 2xl:grid-cols-6" />
          </td>
        </tr>
      )}
    </>
  )
}

function MobileRow({ d, unit, open, onToggle }: { d: Row; unit: SpeedUnit; open: boolean; onToggle: () => void }) {
  const extras = useInstanceExtras(d)
  const health = healthOf(d)
  const issues = issuesOf(d)
  const dl = d.stats?.totalDownloadSpeed || 0
  const ul = d.stats?.totalUploadSpeed || 0

  return (
    <li className="border-b border-border/60">
      <div className="flex items-stretch">
        <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} className="min-w-0 flex-1 py-2.5 active:bg-muted/50">
          <span className="flex items-center gap-2">
            <span className={cn("size-2 shrink-0 rounded-full", HEALTH_DOT[health], health === "ok" && d.instance.connectionStatus === "firewalled" && "bg-amber-500")} />
            <span className="truncate font-medium">{d.instance.name}</span>
            {d.altSpeedEnabled && <Turtle className="size-3.5 shrink-0 text-orange-500" />}
            {health === "ok" && (
              <span className="ml-auto flex shrink-0 items-center gap-2.5 text-sm tabular-nums">
                <span className={cn("flex items-center gap-0.5", !dl && "text-muted-foreground/60")}><ArrowDown className="size-3" />{formatSpeedWithUnit(dl, unit, true)}</span>
                <span className={cn("flex items-center gap-0.5", !ul && "text-muted-foreground/60")}><ArrowUp className="size-3" />{formatSpeedWithUnit(ul, unit, true)}</span>
              </span>
            )}
          </span>
          <span className="mt-0.5 flex items-center gap-1 pl-4 text-xs text-muted-foreground">
            {health === "offline" && <span className="truncate text-destructive">Offline: {offlineReason(d)}</span>}
            {health === "loading" && <span className="animate-pulse">Loading</span>}
            {health === "ok" && (
              <>
                <span className="truncate tabular-nums">{num(d.torrentCounts?.total)} torrents, {num(d.torrentCounts?.status?.active)} active</span>
                <span className="ml-auto flex shrink-0">{issues.map(i => <IssueChip key={i.key} issue={i} asText />)}</span>
              </>
            )}
          </span>
        </Link>
        {health === "ok" && (
          <button type="button" aria-expanded={open} aria-label={`Details of ${d.instance.name}`} onClick={onToggle} className="grid w-11 shrink-0 place-items-center text-muted-foreground">
            <ChevronDown className={cn("size-4 transition-transform", open && "rotate-180")} />
          </button>
        )}
      </div>
      {open && health === "ok" && (
        <div className="pb-3 pl-4">
          {issues.length > 0 && <div className="-ml-1.5 mb-2 flex flex-wrap gap-1">{issues.map(i => <IssueChip key={i.key} issue={i} withLabel />)}</div>}
          <DetailList
            items={[
              ["Size", formatBytes(d.stats?.totalSize || 0)],
              ["Free space", d.serverState?.free_space_on_disk !== undefined ? formatBytes(d.serverState.free_space_on_disk) : "Unknown"],
              ["Ratio", ratioOf(d).toFixed(2)],
              ...details(d, extras, unit).filter(([label]) => MOBILE_DETAILS.has(label)),
            ]}
            className="grid-cols-1 min-[400px]:grid-cols-2"
          />
          <div className="mt-3 flex gap-2">
            <Button asChild size="sm" variant="secondary" className="h-10 flex-1">
              <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} search={{ modal: "add-torrent" }}><Plus />Add torrent</Link>
            </Button>
            <AltSpeedButton d={d} className="size-10 bg-secondary" />
          </div>
        </div>
      )}
    </li>
  )
}

function Figure({ value, caption, icon, spark, tone, className }: { value: ReactNode; caption: string; icon?: ReactNode; spark?: number[]; tone?: string; className?: string }) {
  return (
    <div className={cn("min-w-0", className)}>
      <dd className="flex items-end gap-2">
        <span className={cn("flex items-center gap-1 whitespace-nowrap text-2xl font-semibold tabular-nums tracking-tight", tone)}>{icon}{value}</span>
        {spark && <Sparkline values={spark} className="mb-1.5 hidden h-5 w-16 text-muted-foreground sm:block" />}
      </dd>
      <dt className="truncate text-xs text-muted-foreground">{caption}</dt>
    </div>
  )
}

export function VariantA({ statsData, globalStats: g, seedHistory }: VariantProps) {
  const [unit] = useSpeedUnits()
  const history = useSpeedHistory(statsData, seedHistory)
  const trackers = useTrackerRows(statsData)
  const { data: trackerIcons } = useTrackerIcons()
  const [openId, setOpenId] = useState<number | null>(null)
  const [allTrackers, setAllTrackers] = useState(false)
  const toggle = (id: number) => setOpenId(prev => (prev === id ? null : id))
  const issueCount = statsData.reduce((sum, d) => sum + issuesOf(d).reduce((s, i) => s + i.count, 0), 0)
  const offline = statsData.filter(d => healthOf(d) === "offline").length
  const shownTrackers = allTrackers ? trackers : trackers.slice(0, 8)
  const maxUploaded = trackers[0]?.uploaded || 1

  return (
    <div className="px-4 pb-28 pt-4 lg:px-6">
      <header className="flex items-center gap-1.5">
        <h1 className="mr-auto text-xl font-semibold tracking-tight">Dashboard</h1>
        <AddTorrentMenu statsData={statsData}>
          <Button size="sm" className="max-sm:h-10"><Plus />Add torrent</Button>
        </AddTorrentMenu>
        <Button asChild variant="ghost" size="sm" className="hidden sm:inline-flex">
          <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>Add instance</Link>
        </Button>
        <Button variant="ghost" size="icon" className="max-sm:size-10" aria-label="Dashboard settings" onClick={() => stubAction("Dashboard settings")}>
          <Settings2 />
        </Button>
      </header>

      <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-4 sm:grid-cols-3 xl:grid-cols-6">
        <Figure icon={<ArrowDown className="size-5 text-muted-foreground" />} value={formatSpeedWithUnit(g.totalDownload, unit)} caption={`${num(g.downloadingTorrents)} downloading, ${formatBytes(g.totalRemainingSize)} left`} spark={sumSeries(history, "dl")} />
        <Figure icon={<ArrowUp className="size-5 text-muted-foreground" />} value={formatSpeedWithUnit(g.totalUpload, unit)} caption={`${num(g.seedingTorrents)} seeding, ${num(g.totalPeers)} peers`} spark={sumSeries(history, "ul")} />
        <Figure className="max-sm:hidden" value={num(g.totalTorrents)} caption={`torrents, ${num(g.activeTorrents)} active`} />
        <Figure className="max-sm:hidden" value={formatBytes(g.totalSize)} caption={`total size, ${formatBytes(g.totalSeedingSize)} seeding`} />
        <Figure className="max-sm:hidden" value={g.globalRatio.toFixed(2)} caption={`ratio, ${formatBytes(g.alltimeUl)} up all time`} />
        <Figure className="max-sm:hidden" value={num(issueCount)} tone={issueCount ? "text-yellow-500" : undefined} caption={`with issues${offline ? `, ${offline} offline` : ""}`} />
      </dl>
      <p className="mt-2 text-xs text-muted-foreground sm:hidden">
        {num(g.totalTorrents)} torrents in {g.connected} of {g.total} instances, {num(issueCount)} with issues, ratio {g.globalRatio.toFixed(2)}
      </p>

      <ul className="mt-4 border-t border-border/60 lg:hidden">
        {statsData.map(d => <MobileRow key={d.instance.id} d={d} unit={unit} open={openId === d.instance.id} onToggle={() => toggle(d.instance.id)} />)}
      </ul>

      <table className="mt-8 hidden w-full text-sm tabular-nums lg:table">
        <thead>
          <tr className="border-b text-left text-xs text-muted-foreground [&>th]:px-2 [&>th]:py-2 [&>th]:font-normal">
            <th>Instance</th>
            <th className="text-right">Download</th>
            <th className="text-right">Upload</th>
            <th className="text-right">Torrents</th>
            <th className="text-right">Active</th>
            <th>Issues</th>
            <th className="text-right">Size</th>
            <th className="text-right">Free space</th>
            <th className="text-right">Ratio</th>
            <th className="hidden text-right xl:table-cell">Peers</th>
            <th className="hidden text-right xl:table-cell">Uptime</th>
            <th className="hidden 2xl:table-cell">qBittorrent</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {statsData.map(d => (
            <DesktopRow key={d.instance.id} d={d} history={history.get(d.instance.id)} unit={unit} open={openId === d.instance.id} onToggle={() => toggle(d.instance.id)} />
          ))}
        </tbody>
      </table>

      {trackers.length > 0 && (
        <section className="mt-8">
          <h2 className="text-sm font-medium">Trackers <span className="font-normal text-muted-foreground">by all-time upload</span></h2>
          <div className="mt-2 grid grid-cols-[minmax(0,1fr)_auto_auto] gap-x-4 border-t border-border/60 text-sm tabular-nums sm:grid-cols-[minmax(0,2fr)_auto_auto_minmax(0,2fr)_auto_auto_auto] sm:gap-x-6">
            {shownTrackers.map((t) => {
              const ratio = trackerRatio(t)
              return (
                <div key={t.domain} className="col-span-full grid grid-cols-subgrid items-center border-b border-border/60 py-2">
                  <span className="flex min-w-0 items-center gap-2">
                    <TrackerIconImage tracker={t.domain} trackerIcons={trackerIcons} />
                    <span className="truncate">{t.name}</span>
                  </span>
                  <span className="text-right text-muted-foreground max-sm:hidden">{num(t.count)}</span>
                  <span className="text-right text-muted-foreground max-sm:hidden">{formatBytes(t.totalSize)}</span>
                  <span className="flex items-center gap-3 max-sm:hidden">
                    <span className="h-1 flex-1 bg-muted"><span className="block h-full bg-foreground/45" style={{ width: `${(t.uploaded / maxUploaded) * 100}%` }} /></span>
                  </span>
                  <span className="text-right">{formatBytes(t.uploaded)}</span>
                  <span className="text-right text-muted-foreground max-sm:hidden">{formatBytes(t.downloaded)}</span>
                  <span className="text-right font-medium" style={{ color: getRatioColor(ratio) }}>{ratio.toFixed(2)}</span>
                </div>
              )
            })}
          </div>
          {trackers.length > 8 && (
            <button type="button" onClick={() => setAllTrackers(v => !v)} className="mt-2 py-2 text-xs text-muted-foreground underline underline-offset-2 hover:text-foreground">
              {allTrackers ? "Show fewer" : `Show all ${trackers.length} trackers`}
            </button>
          )}
        </section>
      )}
    </div>
  )
}
