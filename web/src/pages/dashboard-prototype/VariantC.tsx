/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant C, "Triage": the page opens with what needs you, most urgent first.
// Phone: the instances are one swipe pager, so they take the height of one instance.
// Desktop: a comparison matrix, an instance is a column and a metric is a row.

import { Button } from "@/components/ui/button"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { cn, formatBytes, formatDurationCompact, getRatioColor } from "@/lib/utils"
import { Link } from "@tanstack/react-router"
import { ArrowDown, ArrowUp, ChevronRight, HardDrive, Plus, Rabbit, Settings2, Turtle, Unplug, type LucideIcon } from "lucide-react"
import { useRef, useState, type ReactNode } from "react"
import {
  AddTorrentMenu, HEALTH_DOT, IssueChip, IssueLink, Sparkline, healthOf, isLowDisk, issuesOf, num, offlineReason, ratioOf,
  stubAction, sumSeries, trackerRatio, useInstanceExtras, useSpeedHistory, useTrackerRows,
  type Row, type VariantProps
} from "./shared"

export const name = "Triage"

interface TriageItem {
  key: string
  weight: number
  icon: LucideIcon
  tone: string
  text: string
  action: string
  wrap: (children: ReactNode, className: string) => ReactNode
}

const ISSUE_WEIGHT: Record<string, number> = { unregistered: 1, errored: 2, tracker_error: 4, tracker_down: 5 }

function triage(statsData: Row[]): TriageItem[] {
  const items: TriageItem[] = []
  for (const d of statsData) {
    const id = String(d.instance.id)
    if (healthOf(d) === "offline") {
      items.push({
        key: `offline${id}`, weight: 0, icon: Unplug, tone: "text-destructive",
        text: `${d.instance.name} is offline: ${offlineReason(d)}`, action: "Open settings",
        wrap: (children, className) => <Link to="/settings" search={{ tab: "instances" as const }} className={className}>{children}</Link>,
      })
      continue
    }
    if (isLowDisk(d)) {
      items.push({
        key: `disk${id}`, weight: 3, icon: HardDrive, tone: "text-destructive",
        text: `${d.instance.name} has ${formatBytes(d.serverState?.free_space_on_disk || 0)} of disk space left`, action: "Open",
        wrap: (children, className) => <Link to="/instances/$instanceId" params={{ instanceId: id }} className={className}>{children}</Link>,
      })
    }
    for (const issue of issuesOf(d)) {
      items.push({
        key: `${issue.key}${id}`, weight: ISSUE_WEIGHT[issue.key] ?? 6, icon: issue.icon, tone: issue.iconClass,
        text: `${num(issue.count)} ${issue.count === 1 ? "torrent" : "torrents"} on ${d.instance.name}: ${issue.label}`, action: "Show",
        wrap: (children, className) => <IssueLink issue={issue} className={className}>{children}</IssueLink>,
      })
    }
  }
  return items.sort((a, b) => a.weight - b.weight)
}

interface Metric {
  label: string
  raw: (d: Row) => number | undefined
  format: (value: number, unit: SpeedUnit) => string
  bar?: boolean
  sum?: boolean
}

const bytes = (v: number) => formatBytes(v)
const METRICS: Metric[] = [
  { label: "Download", raw: d => d.stats?.totalDownloadSpeed, format: (v, unit) => formatSpeedWithUnit(v, unit), bar: true, sum: true },
  { label: "Upload", raw: d => d.stats?.totalUploadSpeed, format: (v, unit) => formatSpeedWithUnit(v, unit), bar: true, sum: true },
  { label: "Torrents", raw: d => d.torrentCounts?.total, format: num, bar: true, sum: true },
  { label: "Active", raw: d => d.torrentCounts?.status?.active, format: num, sum: true },
  { label: "Downloading", raw: d => d.torrentCounts?.status?.downloading, format: num, sum: true },
  { label: "Seeding", raw: d => d.stats?.seeding, format: num, sum: true },
  { label: "Stopped", raw: d => d.torrentCounts?.status?.stopped, format: num, sum: true },
  { label: "Size", raw: d => d.stats?.totalSize, format: bytes, bar: true, sum: true },
  { label: "Free space", raw: d => d.serverState?.free_space_on_disk, format: bytes },
  { label: "Uploaded, all time", raw: d => d.serverState?.alltime_ul, format: bytes, bar: true, sum: true },
  { label: "Downloaded, all time", raw: d => d.serverState?.alltime_dl, format: bytes, bar: true, sum: true },
  { label: "Uploaded, session", raw: d => d.serverState?.up_info_data, format: bytes, sum: true },
  { label: "Downloaded, session", raw: d => d.serverState?.dl_info_data, format: bytes, sum: true },
  { label: "Peers", raw: d => d.serverState?.total_peer_connections, format: num, bar: true, sum: true },
]

const CELL = "relative flex h-8 items-center justify-end border-b border-border/60 px-3 whitespace-nowrap"
const HEAD_CELL = "flex h-12 items-center justify-end gap-1 border-b px-3"

function MatrixColumn({ d, unit, maxes }: { d: Row; unit: SpeedUnit; maxes: number[] }) {
  const extras = useInstanceExtras(d)
  const health = healthOf(d)
  const ok = health === "ok"
  const issues = issuesOf(d)
  const ratio = ratioOf(d)
  const connection = d.instance.connectionStatus ?? d.serverState?.connection_status ?? ""

  return (
    <>
      <div className={HEAD_CELL}>
        <span className={cn("size-2 shrink-0 rounded-full", HEALTH_DOT[health], ok && connection === "firewalled" && "bg-amber-500")} />
        <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} className="truncate font-medium hover:underline">{d.instance.name}</Link>
        {ok && (
          <>
            <button type="button" title={`Alternative speed limits ${d.altSpeedEnabled ? "on" : "off"}`} onClick={() => stubAction("Alternative speed limits")} className={cn("ml-1 grid size-6 place-items-center rounded hover:bg-muted", d.altSpeedEnabled ? "text-orange-500" : "text-muted-foreground")}>
              {d.altSpeedEnabled ? <Turtle className="size-3.5" /> : <Rabbit className="size-3.5" />}
            </button>
            <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} search={{ modal: "add-torrent" }} title={`Add torrent to ${d.instance.name}`} className="grid size-6 place-items-center rounded text-muted-foreground hover:bg-muted hover:text-foreground">
              <Plus className="size-3.5" />
            </Link>
          </>
        )}
      </div>
      <div className={cn(CELL, !ok && "text-destructive")}>{health === "offline" ? "Offline" : health === "loading" ? "Loading" : connection === "connected" ? "Connectable" : connection.charAt(0).toUpperCase() + connection.slice(1)}</div>
      <div className={CELL}>{issues.length ? issues.map(i => <IssueChip key={i.key} issue={i} />) : ok && <span className="text-muted-foreground/50">None</span>}</div>
      {METRICS.map((m, i) => {
        const value = ok ? m.raw(d) : undefined
        const low = m.label === "Free space" && isLowDisk(d)
        return (
          <div key={m.label} className={cn(CELL, low && "font-medium text-destructive", value === 0 && "text-muted-foreground/60")}>
            {m.bar && value ? <span className="absolute inset-y-1.5 left-2 rounded-sm bg-foreground/[0.08]" style={{ width: `calc(${(value / (maxes[i] || 1)) * 100}% - 1rem)` }} /> : null}
            <span className="relative">{value !== undefined ? m.format(value, unit) : ""}</span>
          </div>
        )
      })}
      <div className={cn(CELL, "font-medium")} style={{ color: ok ? getRatioColor(ratio) : undefined }}>{ok ? ratio.toFixed(2) : ""}</div>
      <div className={cn(CELL, "text-muted-foreground")}>{ok && extras.uptime !== null ? formatDurationCompact(extras.uptime) : ""}</div>
      <div className={cn(CELL, "text-muted-foreground")}>{ok ? extras.version : ""}</div>
      <div className={cn(CELL, "text-muted-foreground")}>{ok ? extras.libtorrent : ""}</div>
    </>
  )
}

function PagerPage({ d, unit }: { d: Row; unit: SpeedUnit }) {
  const extras = useInstanceExtras(d)
  const health = healthOf(d)
  const issues = issuesOf(d)
  const status = d.torrentCounts?.status
  const free = d.serverState?.free_space_on_disk
  const id = String(d.instance.id)

  if (health !== "ok") {
    return (
      <article className="flex w-[86%] shrink-0 snap-center flex-col rounded-2xl bg-muted/50 p-4">
        <p className={cn("text-sm", health === "offline" ? "text-destructive" : "animate-pulse text-muted-foreground")}>
          {health === "offline" ? `Offline: ${offlineReason(d)}` : "Loading"}
        </p>
        <p className="mt-1 truncate text-xs text-muted-foreground">{d.instance.host}</p>
        <Button asChild variant="secondary" className="mt-auto h-11">
          <Link to="/settings" search={{ tab: "instances" as const }}>Open settings</Link>
        </Button>
      </article>
    )
  }

  const facts: Array<[string, string, string?]> = [
    ["torrents", num(d.torrentCounts?.total)],
    ["active", num(status?.active)],
    ["downloading", num(status?.downloading)],
    ["size", formatBytes(d.stats?.totalSize || 0)],
    ["free", free !== undefined ? formatBytes(free) : "Unknown", isLowDisk(d) ? "text-destructive" : undefined],
    ["ratio", ratioOf(d).toFixed(2)],
  ]

  return (
    <article className="w-[86%] shrink-0 snap-center rounded-2xl bg-muted/50 p-4">
      <div className="flex gap-5 text-xl font-semibold tabular-nums tracking-tight">
        <span className={cn("flex items-center gap-1", !d.stats?.totalDownloadSpeed && "text-muted-foreground/60")}><ArrowDown className="size-4" />{formatSpeedWithUnit(d.stats?.totalDownloadSpeed || 0, unit)}</span>
        <span className={cn("flex items-center gap-1", !d.stats?.totalUploadSpeed && "text-muted-foreground/60")}><ArrowUp className="size-4" />{formatSpeedWithUnit(d.stats?.totalUploadSpeed || 0, unit)}</span>
      </div>
      <dl className="mt-3 grid grid-cols-3 gap-x-3 gap-y-2">
        {facts.map(([label, value, tone]) => (
          <div key={label} className="min-w-0">
            <dd className={cn("truncate text-sm font-medium tabular-nums", tone)}>{value}</dd>
            <dt className="text-xs text-muted-foreground">{label}</dt>
          </div>
        ))}
      </dl>
      <p className="mt-2 truncate text-xs text-muted-foreground">
        qBittorrent {extras.version}{extras.uptime !== null && `, up ${formatDurationCompact(extras.uptime)}`}, {num(d.serverState?.total_peer_connections)} peers
      </p>
      {issues.length > 0 && <div className="-ml-1.5 mt-1 flex flex-wrap">{issues.map(i => <IssueChip key={i.key} issue={i} withLabel />)}</div>}
      <div className="mt-3 flex gap-2">
        <Button asChild className="h-11 flex-1">
          <Link to="/instances/$instanceId" params={{ instanceId: id }}>Open torrents</Link>
        </Button>
        <Button asChild variant="outline" size="icon" className="size-11" aria-label="Add torrent">
          <Link to="/instances/$instanceId" params={{ instanceId: id }} search={{ modal: "add-torrent" }}><Plus /></Link>
        </Button>
        <Button variant="outline" size="icon" className="size-11" aria-label="Alternative speed limits" onClick={() => stubAction("Alternative speed limits")}>
          {d.altSpeedEnabled ? <Turtle className="text-orange-500" /> : <Rabbit />}
        </Button>
      </div>
    </article>
  )
}

export function VariantC({ statsData, globalStats: g, seedHistory }: VariantProps) {
  const [unit] = useSpeedUnits()
  const history = useSpeedHistory(statsData, seedHistory)
  const trackers = useTrackerRows(statsData)
  const { data: trackerIcons } = useTrackerIcons()
  const items = triage(statsData)
  const pager = useRef<HTMLDivElement>(null)
  const [page, setPage] = useState(0)
  const maxes = METRICS.map(m => Math.max(0, ...statsData.map(d => (healthOf(d) === "ok" ? m.raw(d) || 0 : 0))))
  const rowCount = METRICS.length + 7

  const showPage = (index: number) => {
    const el = pager.current
    const child = el?.children[index] as HTMLElement | undefined
    if (el && child) el.scrollTo({ left: child.offsetLeft - (el.clientWidth - child.clientWidth) / 2, behavior: "smooth" })
  }

  return (
    <div className="pb-28 pt-4">
      <header className="flex items-center gap-1.5 px-4 lg:px-6">
        <h1 className="mr-auto text-sm font-medium text-muted-foreground">Dashboard</h1>
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

      <div className="px-4 lg:grid lg:grid-cols-[minmax(0,1fr)_17rem] lg:gap-12 lg:px-6">
        <section>
          <p className="mt-1 text-3xl font-semibold tracking-tight lg:text-4xl">
            {items.length === 0 ? "All quiet." : `${items.length} ${items.length === 1 ? "thing needs" : "things need"} a look.`}
          </p>
          <p className="mt-1 text-sm text-muted-foreground">
            {g.connected} of {g.total} instances answer, with {num(g.totalTorrents)} torrents and {num(g.activeTorrents)} active.
          </p>
          <ul className="mt-4 border-t border-border/60">
            {items.map((item) => {
              const Icon = item.icon
              return (
                <li key={item.key} className="border-b border-border/60">
                  {item.wrap(
                    <>
                      <Icon className={cn("size-4 shrink-0", item.tone)} />
                      <span className="min-w-0 flex-1 truncate text-sm">{item.text}</span>
                      <span className="hidden shrink-0 text-xs text-muted-foreground underline underline-offset-2 sm:inline">{item.action}</span>
                      <ChevronRight className="size-4 shrink-0 text-muted-foreground sm:hidden" />
                    </>,
                    "flex min-h-11 items-center gap-3 py-2 hover:bg-muted/40 lg:px-2"
                  )}
                </li>
              )
            })}
          </ul>
        </section>

        <aside className="mt-5 flex gap-8 lg:mt-2 lg:flex-col lg:gap-5">
          {([
            ["dl", ArrowDown, g.totalDownload, g.downloadingTorrents ? `${num(g.downloadingTorrents)} downloading, ${formatBytes(g.totalRemainingSize)} left` : "nothing downloading"],
            ["ul", ArrowUp, g.totalUpload, `${num(g.seedingTorrents)} seeding, ratio ${g.globalRatio.toFixed(2)}`],
          ] as const).map(([key, Icon, value, caption]) => (
            <div key={key} className="min-w-0 flex-1 lg:flex-none">
              <div className="flex items-center gap-1 whitespace-nowrap text-xl font-semibold tabular-nums tracking-tight lg:text-2xl">
                <Icon className="size-4 text-muted-foreground lg:size-5" />{formatSpeedWithUnit(value, unit)}
              </div>
              <Sparkline values={sumSeries(history, key)} className="mt-1 block h-8 w-full text-foreground/50 lg:h-10" />
              <div className="truncate text-xs text-muted-foreground">{caption}</div>
            </div>
          ))}
        </aside>
      </div>

      <section className="mt-6 lg:hidden">
        <div className="flex gap-5 overflow-x-auto px-4" role="tablist">
          {statsData.map((d, index) => (
            <button
              key={d.instance.id} type="button" role="tab" aria-selected={page === index} onClick={() => showPage(index)}
              className={cn("flex h-10 shrink-0 items-center gap-1.5 border-b-2 text-sm", page === index ? "border-foreground font-medium" : "border-transparent text-muted-foreground")}
            >
              <span className={cn("size-1.5 rounded-full", HEALTH_DOT[healthOf(d)])} />{d.instance.name}
            </button>
          ))}
        </div>
        <div
          ref={pager}
          onScroll={(e) => {
            const el = e.currentTarget
            const step = (el.firstElementChild?.clientWidth ?? el.clientWidth) + 12
            setPage(Math.round(el.scrollLeft / step))
          }}
          className="mt-3 flex snap-x snap-mandatory gap-3 overflow-x-auto px-[7%] [scrollbar-width:none]"
        >
          {statsData.map(d => <PagerPage key={d.instance.id} d={d} unit={unit} />)}
        </div>
      </section>

      <section className="mt-10 hidden overflow-x-auto px-6 lg:block">
        <div
          className="grid grid-flow-col text-sm tabular-nums"
          style={{
            gridTemplateRows: `repeat(${rowCount}, auto)`,
            gridTemplateColumns: `11rem repeat(${statsData.length}, minmax(9.5rem, 1fr)) minmax(8rem, 0.7fr)`,
          }}
        >
          <div className={cn(HEAD_CELL, "justify-start px-0 text-xs text-muted-foreground")}>Compare instances</div>
          {["Connection", "Issues", ...METRICS.map(m => m.label), "Ratio", "Uptime", "qBittorrent", "libtorrent"].map(label => (
            <div key={label} className={cn(CELL, "justify-start px-0 text-xs text-muted-foreground")}>{label}</div>
          ))}

          {statsData.map(d => <MatrixColumn key={d.instance.id} d={d} unit={unit} maxes={maxes} />)}

          <div className={cn(HEAD_CELL, "border-l border-l-border/60 font-medium")}>All</div>
          <div className={cn(CELL, "border-l border-l-border/60")}>{g.connected} of {g.total}</div>
          <div className={cn(CELL, "border-l border-l-border/60")}>{num(statsData.reduce((sum, d) => sum + issuesOf(d).reduce((s, i) => s + i.count, 0), 0))}</div>
          {METRICS.map(m => (
            <div key={m.label} className={cn(CELL, "border-l border-l-border/60 font-medium")}>
              {m.sum ? m.format(statsData.reduce((sum, d) => sum + (m.raw(d) || 0), 0), unit) : ""}
            </div>
          ))}
          <div className={cn(CELL, "border-l border-l-border/60 font-medium")} style={{ color: getRatioColor(g.globalRatio) }}>{g.globalRatio.toFixed(2)}</div>
          <div className={cn(CELL, "border-l border-l-border/60")} />
          <div className={cn(CELL, "border-l border-l-border/60")} />
          <div className={cn(CELL, "border-l border-l-border/60")} />
        </div>
      </section>

      {trackers.length > 0 && (
        <section className="mt-8 px-4 lg:px-6">
          <h2 className="text-sm font-medium">Trackers</h2>
          <ul className="mt-2 grid gap-x-12 border-t border-border/60 text-sm tabular-nums lg:grid-cols-2 lg:border-t-0">
            {trackers.slice(0, 10).map((t) => {
              const ratio = trackerRatio(t)
              return (
                <li key={t.domain} className="flex h-9 items-center gap-3 border-b border-border/60">
                  <span className="flex min-w-0 flex-1 items-center gap-2">
                    <TrackerIconImage tracker={t.domain} trackerIcons={trackerIcons} />
                    <span className="truncate">{t.name}</span>
                  </span>
                  <span className="text-muted-foreground max-sm:hidden">{num(t.count)} torrents</span>
                  <span className="w-20 text-right">{formatBytes(t.uploaded)}</span>
                  <span className="w-10 text-right font-medium" style={{ color: getRatioColor(ratio) }}>{ratio.toFixed(1)}</span>
                </li>
              )
            })}
          </ul>
        </section>
      )}
    </div>
  )
}
