/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE variant B, "Stream": the live throughput of all instances is the page.
// Upload stacks above the axis and download below it, one shade for each instance.
// Phone: one-line strips, a tap opens a bottom sheet. Desktop: the chart, a legend
// that is also the instance table, and a side rail for attention, storage and totals.
//
// B2 is the same page after #3991. The chart shows the recorded bytes of the last
// 24 hours, with the live speed one tap away. The lifetime tracker list is gone,
// because #4014 removes the sums behind it. Its place takes the largest Tracker
// groups of the day, in the series colors of the Statistics page.

import { SizeCalculatingMark } from "@/components/dashboard/UniqueSize"
import { Button } from "@/components/ui/button"
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TrackerIconImage } from "@/components/ui/tracker-icon"
import { useTrackerIcons } from "@/hooks/useTrackerIcons"
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { cn, formatBytes, formatDuration, getRatioColor } from "@/lib/utils"
import { Link, useNavigate } from "@tanstack/react-router"
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Eye, EyeOff, HardDrive, Plus, Rabbit, Settings2, ShieldAlert, Turtle, Unplug } from "lucide-react"
import { useState, type ReactNode } from "react"
import { dayHistory, sumOf, type DayHistory } from "./history"
import {
  AddTorrentMenu, HEALTH_DOT, IssueChip, IssueLink, freeShare, healthOf, isLowDisk, issuesOf, num, offlineReason, ratioOf,
  sizeOf, stubAction, trackerRatio, useInstanceExtras, useSpeedHistory, useTrackerRows,
  type History, type Row, type VariantProps
} from "./shared"

export const name = "Stream"

// A ramp of the text color, so that the layers stay apart in every theme.
const SHADES = [88, 58, 36, 22, 13]
const shade = (index: number) => `color-mix(in oklab, var(--foreground) ${SHADES[index % SHADES.length]}%, transparent)`

// The distinct series colors of the Statistics page (#3991). On this page they mark
// Tracker groups only. An instance keeps its shade of the text color.
const SERIES =
  "[--series-1:#2a78d6] [--series-2:#eb6834] [--series-3:#1baf7a] [--series-4:#eda100] [--series-5:#e87ba4] " +
  "dark:[--series-1:#3987e5] dark:[--series-2:#d95926] dark:[--series-3:#199e70] dark:[--series-4:#c98500] dark:[--series-5:#d55181]"
const OTHER = "color-mix(in oklab, var(--muted-foreground) 45%, transparent)"

const W = 600
const UP_H = 120
const DOWN_H = 80

/**
 * Stacked paths for one half of the chart. `dir` is -1 for upload (grows up) and 1 for download.
 * `bars` draws one bar for each value, for the recorded hours. Without it the values are one area.
 */
function stackPaths(series: number[][], height: number, dir: 1 | -1, bars: boolean): { paths: string[]; peak: number } {
  const length = series[0]?.length ?? 0
  if (length < 2) return { paths: [], peak: 0 }
  const totals = Array.from({ length }, (_, i) => series.reduce((sum, s) => sum + s[i], 0))
  const peak = Math.max(...totals)
  const base = Array<number>(length).fill(0)
  const step = W / length
  const x = (i: number) => (i / (length - 1)) * W
  const y = (v: number) => UP_H + dir * (v / Math.max(peak, 1)) * (height - 6)
  const paths = series.map((s) => {
    const top = s.map((v, i) => base[i] + v)
    const d = bars
      ? top.map((v, i) => `M${(i * step + 1.5).toFixed(1)},${y(base[i]).toFixed(1)}H${((i + 1) * step - 1.5).toFixed(1)}V${y(v).toFixed(1)}H${(i * step + 1.5).toFixed(1)}Z`).join("")
      : `${top.map((v, i) => `${i ? "L" : "M"}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join("")}${base.map((v, i) => `L${x(i).toFixed(1)},${y(v).toFixed(1)}`).reverse().join("")}Z`
    top.forEach((v, i) => { base[i] = v })
    return d
  })
  return { paths, peak }
}

const clock = (t: number) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })

function StreamChart({ rows, history, unit, totalUp, totalDown, upCaption, downCaption, day, controls }: {
  rows: Row[]; history: Map<number, History>; unit: SpeedUnit
  totalUp: number; totalDown: number; upCaption: string; downCaption: string
  /** Set when `history` holds the recorded hours of the day and not the live speed. */
  day?: DayHistory; controls?: ReactNode
}) {
  const series = rows.map(d => history.get(d.instance.id)).filter((h): h is History => Boolean(h))
  const up = stackPaths(series.map(h => h.ul), UP_H, -1, Boolean(day))
  const down = stackPaths(series.map(h => h.dl), DOWN_H, 1, Boolean(day))
  const peak = (v: number) => (day ? `busiest hour ${formatBytes(v)}` : `peak ${formatSpeedWithUnit(v, unit)}`)
  const gapShare = day?.gap ? (day.gap.index + 0.5) / day.hours.length : null

  return (
    <div>
      <div className="flex items-end justify-between gap-3 px-4 pb-2 lg:px-6">
        <div className="min-w-0">
          <div className="flex items-center gap-1 text-3xl font-semibold tabular-nums tracking-tight lg:text-4xl">
            <ArrowUp className="size-6 shrink-0 text-muted-foreground" />{formatSpeedWithUnit(totalUp, unit)}
          </div>
          <div className="line-clamp-2 text-xs text-muted-foreground lg:line-clamp-1">{upCaption}</div>
        </div>
        <div className="flex shrink-0 flex-col items-end gap-1.5 text-[11px] text-muted-foreground tabular-nums">
          {controls}
          <span>{peak(up.peak)}</span>
        </div>
      </div>
      <div className={cn("relative", gapShare !== null && "pt-4")}>
        <svg
          viewBox={`0 0 ${W} ${UP_H + DOWN_H}`} preserveAspectRatio="none" className="block h-36 w-full lg:h-56" role="img"
          aria-label={day ? "Upload and download of every instance in each of the last 24 hours" : "Upload and download speed of every instance over the last three minutes"}
        >
          {up.paths.map((p, i) => <path key={`u${i}`} d={p} fill={shade(i)} />)}
          {down.paths.map((p, i) => <path key={`d${i}`} d={p} fill={shade(i)} opacity={0.55} />)}
          <line x1="0" x2={W} y1={UP_H} y2={UP_H} stroke="var(--border)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
          {gapShare !== null && <line x1={gapShare * W} x2={gapShare * W} y1="0" y2={UP_H + DOWN_H} stroke="#eab308" strokeDasharray="2 3" vectorEffect="non-scaling-stroke" />}
        </svg>
        {day?.gap && gapShare !== null && (
          <span className="absolute top-0 -translate-x-1/2 whitespace-nowrap text-[10px] text-yellow-600 dark:text-yellow-500" style={{ left: `${gapShare * 100}%` }}>
            recording gap, {day.gap.hours} h
          </span>
        )}
      </div>
      {day && (
        <div className="flex justify-between px-4 pt-1 text-[10px] text-muted-foreground tabular-nums lg:px-6">
          <span>{clock(day.hours[0])}</span>
          <span>{clock(day.hours[day.hours.length / 2])}</span>
          <span>{clock(day.hours[day.hours.length - 1] + 3_600_000)}</span>
        </div>
      )}
      <div className="flex items-start justify-between gap-3 px-4 pt-2 lg:px-6">
        <div className="min-w-0">
          <div className="flex items-center gap-1 text-xl font-semibold tabular-nums tracking-tight lg:text-2xl">
            <ArrowDown className="size-5 shrink-0 text-muted-foreground" />{formatSpeedWithUnit(totalDown, unit)}
          </div>
          <div className="truncate text-xs text-muted-foreground">{downCaption}</div>
        </div>
        <div className="shrink-0 text-right text-[11px] text-muted-foreground tabular-nums">{peak(down.peak)}{!day && ", last 3 min"}</div>
      </div>
    </div>
  )
}

/** The largest Tracker groups of the day. The rows add up to the upload of the day, as the spec asks. */
function TopTrackers({ day, trackerIcons }: { day: DayHistory; trackerIcons: ReturnType<typeof useTrackerIcons>["data"] }) {
  const rows = [
    ...day.trackers.map((t, i) => ({ ...t, color: `var(--series-${i + 1})` })),
    ...(day.otherCount ? [{ domain: "", name: `Other (${day.otherCount})`, uploaded: day.otherUploaded, color: OTHER }] : []),
  ]

  return (
    <section className={cn("px-4 pt-6 lg:px-6", SERIES)}>
      <div className="flex items-baseline justify-between gap-3">
        <h2 className="text-sm font-medium">Top trackers, last 24 h</h2>
        <button type="button" onClick={() => stubAction("Statistics page")} className="flex h-8 shrink-0 items-center text-xs text-muted-foreground hover:text-foreground">
          Statistics<ChevronRight className="size-3.5" />
        </button>
      </div>
      <div className="mt-2 flex h-2 gap-px overflow-hidden rounded-full">
        {rows.map(t => <span key={t.name} style={{ width: `${(t.uploaded / day.uploaded) * 100}%`, background: t.color }} />)}
      </div>
      <ul className="mt-2 grid gap-x-10 sm:grid-cols-2 2xl:grid-cols-3">
        {rows.map(t => (
          <li key={t.name} className="flex h-9 min-w-0 items-center gap-2 border-b border-border/60 text-sm tabular-nums">
            <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: t.color }} />
            {t.domain && <TrackerIconImage tracker={t.domain} trackerIcons={trackerIcons} />}
            <span className="min-w-0 flex-1 truncate">{t.name}</span>
            <span>{formatBytes(t.uploaded)}</span>
            <span className="w-9 text-right text-muted-foreground">{Math.round((t.uploaded / day.uploaded) * 100)}%</span>
          </li>
        ))}
      </ul>
    </section>
  )
}

function Swatch({ index, d }: { index: number; d: Row }) {
  const health = healthOf(d)
  if (health !== "ok") return <span className={cn("size-2.5 shrink-0 rounded-full", HEALTH_DOT[health])} />
  return <span className="size-2.5 shrink-0 rounded-[3px]" style={{ background: shade(index) }} />
}

const connectionOf = (d: Row) => d.instance.connectionStatus ?? d.serverState?.connection_status ?? ""

/** The values of one instance, for the bottom sheet and for the detail band. Incognito mode hides a masked value. */
function useFacts(d: Row, recorded: boolean) {
  const extras = useInstanceExtras(d)
  const [incognito] = useIncognitoMode()
  const status = d.torrentCounts?.status
  const free = d.serverState?.free_space_on_disk
  const size = sizeOf(d)
  const connection = connectionOf(d)
  return [
    ["Torrents", num(d.torrentCounts?.total)],
    ["Active", num(status?.active)],
    ["Downloading", num(status?.downloading)],
    [size.calculating ? "Size, calculating" : "Size", formatBytes(size.totalSize)],
    ["Free space", free !== undefined ? formatBytes(free) : "Unknown"],
    recorded ? ["Seeding", num(status?.seeding)] : ["Ratio", ratioOf(d).toFixed(2)],
    ["Peers", num(d.serverState?.total_peer_connections)],
    ["Uptime", extras.uptime !== null ? formatDuration(extras.uptime).split(" ").slice(0, 2).join(" ") : "Unknown"],
    ["qBittorrent", extras.version || "Unknown"],
    ...(recorded ? [
      ["Connection", connection ? connection[0].toUpperCase() + connection.slice(1) : "Unknown"],
      ["External address", d.serverState?.last_external_address_v4 || d.serverState?.last_external_address_v6 || "Unknown", incognito],
    ] : []),
  ] as Array<[label: string, value: string, masked?: boolean]>
}

/**
 * B2, desktop: the details of one instance in a band below its row, as the table of the
 * Statistics page opens a row. The row already shows the speeds and the torrent count.
 */
function DetailBand({ d, index, day }: { d: Row; index: number; day: History }) {
  const facts = useFacts(d, true).slice(1)
  const [incognito] = useIncognitoMode()
  const issues = issuesOf(d)
  const up = stackPaths([day.ul], UP_H, -1, true)
  const down = stackPaths([day.dl], DOWN_H, 1, true)
  const instanceId = String(d.instance.id)

  return (
    <div className="hidden gap-x-10 gap-y-4 border-t border-border/60 bg-muted/30 px-6 py-4 lg:grid lg:grid-cols-[14rem_minmax(0,1fr)]">
      <div className="min-w-0">
        <svg viewBox={`0 0 ${W} ${UP_H + DOWN_H}`} preserveAspectRatio="none" className="block h-24 w-full" role="img" aria-label={`Upload and download of ${d.instance.name} in each of the last 24 hours`}>
          {up.paths.map((p, i) => <path key={`u${i}`} d={p} fill={shade(index)} />)}
          {down.paths.map((p, i) => <path key={`d${i}`} d={p} fill={shade(index)} opacity={0.55} />)}
          <line x1="0" x2={W} y1={UP_H} y2={UP_H} stroke="var(--border)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
        </svg>
        <p className="mt-2 text-xs text-muted-foreground tabular-nums">Last 24 h: {formatBytes(sumOf(day.ul))} uploaded, {formatBytes(sumOf(day.dl))} downloaded</p>
      </div>
      <dl className="grid grid-cols-[repeat(auto-fill,minmax(6.5rem,1fr))] content-start gap-x-4 gap-y-3">
        <div className="col-span-2 min-w-0">
          <dd className={cn("truncate text-sm font-medium", incognito && "select-none blur-sm")} title={incognito ? undefined : d.instance.host}>{d.instance.host}</dd>
          <dt className="text-xs text-muted-foreground">Host</dt>
        </div>
        {facts.map(([label, value, masked]) => (
          <div key={label} className="min-w-0">
            <dd className={cn("truncate text-sm font-medium tabular-nums", masked && "select-none blur-sm")} title={masked ? undefined : value}>{value}</dd>
            <dt className="truncate text-xs text-muted-foreground">{label}</dt>
          </div>
        ))}
      </dl>
      <div className="col-span-full flex flex-wrap items-center gap-2">
        <div className="-ml-1.5 mr-auto flex flex-wrap gap-1">{issues.map(i => <IssueChip key={i.key} issue={i} withLabel />)}</div>
        <Button variant="secondary" size="sm" onClick={() => stubAction("Alternative speed limits")}>
          {d.altSpeedEnabled ? <Turtle className="text-orange-500" /> : <Rabbit />}Alternative limits
        </Button>
        <Button asChild variant="secondary" size="sm">
          <Link to="/instances/$instanceId" params={{ instanceId }} search={{ modal: "add-torrent" }}><Plus />Add torrent</Link>
        </Button>
        <Button asChild size="sm">
          <Link to="/instances/$instanceId" params={{ instanceId }}>Open torrents</Link>
        </Button>
      </div>
    </div>
  )
}

function InstanceSheet({ d, index, unit, day, onClose }: { d: Row; index: number; unit: SpeedUnit; day?: History; onClose: () => void }) {
  const facts = useFacts(d, Boolean(day))
  const [incognito] = useIncognitoMode()
  const issues = issuesOf(d)

  return (
    <DrawerContent>
      <DrawerHeader className="text-left">
        <DrawerTitle className="flex items-center gap-2 text-xl">
          <Swatch index={index} d={d} />{d.instance.name}
          {d.altSpeedEnabled && <Turtle className="size-4 text-orange-500" />}
        </DrawerTitle>
        <DrawerDescription className={cn("truncate", incognito && "select-none blur-sm")}>{d.instance.host}</DrawerDescription>
      </DrawerHeader>
      {healthOf(d) !== "ok" ? (
        <div className="px-4 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
          <p className="text-sm text-destructive">Offline: {offlineReason(d)}</p>
          <Button asChild variant="secondary" className="mt-4 h-11 w-full" onClick={onClose}>
            <Link to="/settings" search={{ tab: "instances" as const }}>Open settings</Link>
          </Button>
        </div>
      ) : (
        <div className="px-4 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
          <div className="flex gap-6 text-2xl font-semibold tabular-nums tracking-tight">
            <span className="flex items-center gap-1"><ArrowDown className="size-5 text-muted-foreground" />{formatSpeedWithUnit(d.stats?.totalDownloadSpeed || 0, unit)}</span>
            <span className="flex items-center gap-1"><ArrowUp className="size-5 text-muted-foreground" />{formatSpeedWithUnit(d.stats?.totalUploadSpeed || 0, unit)}</span>
          </div>
          {day && (
            <p className="mt-1.5 text-sm text-muted-foreground tabular-nums">
              Last 24 h: {formatBytes(sumOf(day.ul))} uploaded, {formatBytes(sumOf(day.dl))} downloaded
            </p>
          )}
          {issues.length > 0 && (
            <ul className="mt-4 border-t border-border/60">
              {issues.map((issue) => {
                const Icon = issue.icon
                return (
                  <li key={issue.key} className="border-b border-border/60">
                    <IssueLink issue={issue} className="flex h-11 items-center gap-2 text-sm">
                      <Icon className={cn("size-4", issue.iconClass)} />
                      <span className={issue.iconClass}>{num(issue.count)} {issue.label}</span>
                      <ChevronRight className="ml-auto size-4 text-muted-foreground" />
                    </IssueLink>
                  </li>
                )
              })}
            </ul>
          )}
          <dl className="mt-4 grid grid-cols-3 gap-x-4 gap-y-3">
            {facts.map(([label, value, masked]) => (
              <div key={label} className="min-w-0">
                <dd className={cn("truncate text-base font-medium tabular-nums", masked && "select-none blur-sm")}>{value}</dd>
                <dt className="text-xs text-muted-foreground">{label}</dt>
              </div>
            ))}
          </dl>
          <div className="mt-5 flex gap-2">
            <Button asChild className="h-11 flex-1" onClick={onClose}>
              <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }}>Open torrents</Link>
            </Button>
            <Button asChild variant="secondary" className="h-11" onClick={onClose}>
              <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} search={{ modal: "add-torrent" }}><Plus />Add</Link>
            </Button>
            <Button variant="secondary" size="icon" className="size-11" aria-label="Alternative speed limits" onClick={() => stubAction("Alternative speed limits")}>
              {d.altSpeedEnabled ? <Turtle className="text-orange-500" /> : <Rabbit />}
            </Button>
          </div>
        </div>
      )}
    </DrawerContent>
  )
}

const LEGEND_GRID = "lg:grid lg:grid-cols-[9rem_minmax(3rem,1fr)_5.5rem_5.5rem_6.5rem_7.5rem] lg:items-center lg:gap-x-4 lg:px-6"

function LegendRow({ d, index, unit, share, dayUp, band, onToggleBand, onOpenSheet }: {
  d: Row; index: number; unit: SpeedUnit; share: number
  /** The upload of the last 24 hours. Set in B2, where `share` is the share of that upload. */
  dayUp?: number
  /** B2, desktop: a click on the row opens `band` below it. Without `onToggleBand` the click opens the torrents. */
  band?: ReactNode; onToggleBand?: () => void
  onOpenSheet: () => void
}) {
  const health = healthOf(d)
  const issues = issuesOf(d)
  const navigate = useNavigate()
  const openInstance = () => {
    if (!onToggleBand) void navigate({ to: "/instances/$instanceId", params: { instanceId: String(d.instance.id) } })
    else if (health === "ok") onToggleBand()
    else void navigate({ to: "/settings", search: { tab: "instances" as const } })
  }
  const dl = d.stats?.totalDownloadSpeed || 0
  const ul = d.stats?.totalUploadSpeed || 0
  const up = (compact: boolean) => <span className={cn("flex items-center justify-end gap-0.5", !ul && "text-muted-foreground/60")}><ArrowUp className="size-3" />{formatSpeedWithUnit(ul, unit, compact)}</span>
  const down = (compact: boolean) => <span className={cn("flex items-center justify-end gap-0.5", !dl && "text-muted-foreground/60")}><ArrowDown className="size-3" />{formatSpeedWithUnit(dl, unit, compact)}</span>
  const nameCell = (
    <span className="flex min-w-0 items-center gap-2.5">
      <Swatch index={index} d={d} />
      <span className="truncate font-medium">{d.instance.name}</span>
      {d.altSpeedEnabled && <Turtle className="size-3.5 shrink-0 text-orange-500" />}
    </span>
  )
  const problem = (
    <span className={cn("truncate text-sm", health === "offline" ? "text-destructive" : "animate-pulse text-muted-foreground")}>
      {health === "offline" ? `Offline: ${offlineReason(d)}` : "Loading"}
    </span>
  )

  return (
    <li className="border-b border-border/60">
      <button type="button" onClick={onOpenSheet} className="flex h-12 w-full items-center gap-3 px-4 text-left active:bg-muted/50 lg:hidden">
        {nameCell}
        {health === "ok" ? (
          <span className="ml-auto flex shrink-0 items-center gap-3 text-sm tabular-nums">
            {up(true)}
            {dl > 0 && down(true)}
            <span className="flex">{issues.map(i => <IssueChip key={i.key} issue={i} asText />)}</span>
          </span>
        ) : <span className="ml-auto flex min-w-0">{problem}</span>}
        <ChevronRight className="size-4 shrink-0 text-muted-foreground" />
      </button>
      <div
        role={onToggleBand && health === "ok" ? "button" : "link"} aria-expanded={onToggleBand && health === "ok" ? Boolean(band) : undefined}
        tabIndex={0} onClick={openInstance} onKeyDown={e => e.key === "Enter" && openInstance()}
        className={cn("hidden h-11 cursor-pointer text-sm tabular-nums hover:bg-muted/40", LEGEND_GRID, band && "bg-muted/30")}
      >
        <span className="flex min-w-0 items-center gap-1.5">
          {nameCell}
          {onToggleBand && health === "ok" && <ChevronDown className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none", band && "rotate-180")} />}
        </span>
        {health === "ok" ? (
          <>
            <span className="flex items-center gap-3">
              <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted">
                <span className="block h-full rounded-full transition-[width] duration-700 motion-reduce:transition-none" style={{ width: `${share * 100}%`, background: shade(index) }} />
              </span>
              {dayUp !== undefined && <span className="hidden w-20 whitespace-nowrap text-right text-muted-foreground min-[1400px]:block">{formatBytes(dayUp)}</span>}
            </span>
            {up(false)}
            {down(false)}
            <span className="text-right text-muted-foreground">{num(d.torrentCounts?.total)}</span>
            <span className="flex justify-end">{issues.length ? issues.map(i => <IssueChip key={i.key} issue={i} />) : <span className="text-muted-foreground/50">None</span>}</span>
          </>
        ) : <span className="col-span-5 flex min-w-0">{problem}</span>}
      </div>
      {band}
    </li>
  )
}

export const VariantB2 = (props: VariantProps) => <VariantB {...props} recorded />

export function VariantB({ statsData, globalStats: g, seedHistory, recorded }: VariantProps & { recorded?: boolean }) {
  const [unit] = useSpeedUnits()
  const history = useSpeedHistory(statsData, seedHistory)
  const trackers = useTrackerRows(statsData)
  const { data: trackerIcons } = useTrackerIcons()
  const [range, setRange] = useState<"day" | "live">("day")
  const day = recorded ? dayHistory(statsData, trackers, seedHistory) : undefined
  const chartDay = range === "day" ? day : undefined
  const [incognito, setIncognito] = useIncognitoMode()
  const [sheetId, setSheetId] = useState<number | null>(null)
  const [bandId, setBandId] = useState<number | null>(null)
  const sheetIndex = statsData.findIndex(d => d.instance.id === sheetId)
  const issues = statsData.flatMap(issuesOf).sort((a, b) => b.count - a.count)
  const offline = statsData.filter(d => healthOf(d) === "offline")
  const lowDisk = statsData.filter(isLowDisk)
  const firewalled = statsData.filter(d => healthOf(d) === "ok" && connectionOf(d) === "firewalled")
  const attention = issues.length + offline.length + lowDisk.length + firewalled.length
  const topTrackers = trackers.slice(0, 10)
  const maxUploaded = trackers[0]?.uploaded || 1

  return (
    <div className="pb-28">
      <header className="flex items-center gap-1.5 px-4 pt-4 lg:px-6">
        <h1 className="mr-auto text-xl font-semibold tracking-tight">Dashboard</h1>
        <AddTorrentMenu statsData={statsData}>
          <Button size="sm" className="max-sm:h-10"><Plus />Add torrent</Button>
        </AddTorrentMenu>
        <Button asChild variant="ghost" size="sm" className="hidden sm:inline-flex">
          <Link to="/settings" search={{ tab: "instances" as const, modal: "add-instance" }}>Add instance</Link>
        </Button>
        {/* The instance cards of today have this switch next to the host. It masks hosts, addresses and tracker names. */}
        <Button variant="ghost" size="icon" className="max-sm:size-10" aria-label={incognito ? "Turn off incognito mode" : "Turn on incognito mode"} aria-pressed={incognito} onClick={() => setIncognito(!incognito)}>
          {incognito ? <EyeOff /> : <Eye />}
        </Button>
        {/* B2 has no section settings, because the sections that they ordered are gone. */}
        {!recorded && (
          <Button variant="ghost" size="icon" className="max-sm:size-10" aria-label="Dashboard settings" onClick={() => stubAction("Dashboard settings")}>
            <Settings2 />
          </Button>
        )}
      </header>

      <div className="mt-3 xl:grid xl:grid-cols-[minmax(0,1fr)_19rem]">
        <div className="min-w-0">
          <StreamChart
            rows={statsData} history={chartDay?.byInstance ?? history} unit={unit} totalUp={g.totalUpload} totalDown={g.totalDownload}
            day={chartDay}
            upCaption={chartDay
              ? `${formatBytes(chartDay.uploaded)} uploaded in 24 h, ${Math.round((chartDay.crossSeedUploaded / (chartDay.uploaded || 1)) * 100)}% from cross-seeds`
              : `${num(g.seedingTorrents)} seeding to ${num(g.totalPeers)} peers`}
            downCaption={chartDay
              ? `${formatBytes(chartDay.downloaded)} downloaded in 24 h`
              : g.downloadingTorrents ? `${num(g.downloadingTorrents)} downloading, ${formatBytes(g.totalRemainingSize)} left` : "Nothing is downloading"}
            controls={day && (
              <Tabs value={range} onValueChange={v => setRange(v as "day" | "live")}>
                <TabsList className="h-9 lg:h-7">
                  <TabsTrigger value="day" className="px-2.5 text-xs">24 h</TabsTrigger>
                  <TabsTrigger value="live" className="px-2.5 text-xs">Live</TabsTrigger>
                </TabsList>
              </Tabs>
            )}
          />

          {attention > 0 && (
            <div className="flex gap-2 overflow-x-auto px-4 py-3 lg:hidden">
              {offline.map(d => (
                <button key={d.instance.id} type="button" onClick={() => setSheetId(d.instance.id)} className="flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-destructive/10 px-3 text-sm text-destructive">
                  <Unplug className="size-3.5" />{d.instance.name} offline
                </button>
              ))}
              {lowDisk.map(d => (
                <button key={d.instance.id} type="button" onClick={() => setSheetId(d.instance.id)} className="flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-destructive/10 px-3 text-sm text-destructive">
                  <HardDrive className="size-3.5" />{d.instance.name} disk almost full
                </button>
              ))}
              {firewalled.map(d => (
                <button key={d.instance.id} type="button" onClick={() => setSheetId(d.instance.id)} className="flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-yellow-500/10 px-3 text-sm text-yellow-600 dark:text-yellow-500">
                  <ShieldAlert className="size-3.5" />{d.instance.name} firewalled
                </button>
              ))}
              {issues.map((issue) => {
                const Icon = issue.icon
                return (
                  <IssueLink key={`${issue.instanceId}${issue.key}`} issue={issue} className={cn("flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-muted px-3 text-sm", issue.iconClass)}>
                    <Icon className="size-3.5" />{num(issue.count)} {issue.label}
                  </IssueLink>
                )
              })}
            </div>
          )}

          <div className={cn("hidden pb-1.5 pt-4 text-xs text-muted-foreground", LEGEND_GRID)}>
            <span>Instance</span>
            <span className="whitespace-nowrap">{day ? "Uploaded, 24 h" : "Share of the upload"}</span>
            <span className="text-right">Upload</span>
            <span className="text-right">Download</span>
            <span className="text-right">Torrents</span>
            <span className="text-right">Issues</span>
          </div>
          <ul className="border-t border-border/60">
            {statsData.map((d, index) => {
              const id = d.instance.id
              const hours = day?.byInstance.get(id)
              const dayUp = day && sumOf(hours?.ul)
              return (
                <LegendRow
                  key={id} d={d} index={index} unit={unit} dayUp={dayUp}
                  onToggleBand={day ? () => setBandId(bandId === id ? null : id) : undefined}
                  band={hours && bandId === id && healthOf(d) === "ok" ? <DetailBand d={d} index={index} day={hours} /> : undefined}
                  share={day ? (dayUp || 0) / (day.uploaded || 1) : g.totalUpload ? (d.stats?.totalUploadSpeed || 0) / g.totalUpload : 0}
                  onOpenSheet={() => setSheetId(d.instance.id)}
                />
              )
            })}
          </ul>

          {day && day.trackers.length > 0 && day.uploaded > 0 && <TopTrackers day={day} trackerIcons={trackerIcons} />}

          {!day && topTrackers.length > 0 && (
            <section className="px-4 pt-6 lg:px-6">
              <h2 className="text-sm font-medium">Where the upload went</h2>
              <ul className="mt-2 grid gap-x-10 lg:grid-cols-2">
                {topTrackers.map((t) => {
                  const ratio = trackerRatio(t)
                  return (
                    <li key={t.domain} className="relative flex h-9 min-w-0 items-center gap-2 text-sm tabular-nums">
                      <span className="absolute inset-y-1.5 left-0 rounded-sm bg-foreground/[0.07]" style={{ width: `${(t.uploaded / maxUploaded) * 100}%` }} />
                      <span className="relative flex min-w-0 flex-1 items-center gap-2 pl-1.5">
                        <TrackerIconImage tracker={t.domain} trackerIcons={trackerIcons} />
                        <span className="truncate">{t.name}</span>
                      </span>
                      <span className="relative text-muted-foreground max-sm:hidden">{num(t.count)} torrents</span>
                      <span className="relative w-20 text-right">{formatBytes(t.uploaded)}</span>
                      <span className="relative w-10 text-right font-medium" style={{ color: getRatioColor(ratio) }}>{ratio.toFixed(1)}</span>
                    </li>
                  )
                })}
              </ul>
            </section>
          )}
        </div>

        <aside className="space-y-7 px-4 pt-6 lg:px-6 xl:border-l xl:border-border/60 xl:pt-2">
          <section className="max-lg:hidden">
            <h2 className="text-sm font-medium">Needs attention</h2>
            {attention === 0 ? <p className="mt-2 text-sm text-muted-foreground">Nothing. Every instance answers and no torrent reports a problem.</p> : (
              <ul className="mt-2 space-y-0.5 text-sm">
                {offline.map(d => (
                  <li key={d.instance.id}>
                    <Link to="/settings" search={{ tab: "instances" as const }} className="-mx-2 flex items-center gap-2 rounded-md px-2 py-1.5 text-destructive hover:bg-muted">
                      <Unplug className="size-4 shrink-0" /><span className="truncate">{d.instance.name} is offline</span>
                    </Link>
                  </li>
                ))}
                {lowDisk.map(d => (
                  <li key={d.instance.id}>
                    <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} className="-mx-2 flex items-center gap-2 rounded-md px-2 py-1.5 text-destructive hover:bg-muted">
                      <HardDrive className="size-4 shrink-0" /><span className="truncate">{d.instance.name} has {formatBytes(d.serverState?.free_space_on_disk || 0)} left</span>
                    </Link>
                  </li>
                ))}
                {firewalled.map(d => (
                  <li key={d.instance.id}>
                    <Link to="/instances/$instanceId" params={{ instanceId: String(d.instance.id) }} className="-mx-2 flex items-center gap-2 rounded-md px-2 py-1.5 text-yellow-600 hover:bg-muted dark:text-yellow-500">
                      <ShieldAlert className="size-4 shrink-0" /><span className="truncate">{d.instance.name} is firewalled</span>
                    </Link>
                  </li>
                ))}
                {issues.map((issue) => {
                  const Icon = issue.icon
                  return (
                    <li key={`${issue.instanceId}${issue.key}`}>
                      <IssueLink issue={issue} className="-mx-2 flex items-center gap-2 rounded-md px-2 py-1.5 hover:bg-muted">
                        <Icon className={cn("size-4 shrink-0", issue.iconClass)} />
                        <span className={cn("tabular-nums", issue.iconClass)}>{num(issue.count)} {issue.label}</span>
                        <span className="ml-auto truncate text-muted-foreground">{issue.instanceName}</span>
                      </IssueLink>
                    </li>
                  )
                })}
              </ul>
            )}
          </section>

          <section>
            <h2 className="text-sm font-medium">Storage</h2>
            <ul className="mt-2 space-y-3">
              {statsData.filter(d => healthOf(d) === "ok").map((d) => {
                const share = freeShare(d)
                return (
                  <li key={d.instance.id} className="text-sm">
                    <div className="flex justify-between gap-2 tabular-nums">
                      <span className="truncate">{d.instance.name}</span>
                      <span className={cn("shrink-0", isLowDisk(d) ? "text-destructive" : "text-muted-foreground")}>{formatBytes(d.serverState?.free_space_on_disk || 0)} free</span>
                    </div>
                    <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted" title={`${formatBytes(sizeOf(d).totalSize)} of torrents`}>
                      <div className={cn("h-full rounded-full", isLowDisk(d) ? "bg-destructive" : "bg-foreground/45")} style={{ width: `${(1 - (share ?? 1)) * 100}%` }} />
                    </div>
                  </li>
                )
              })}
            </ul>
          </section>

          <section>
            {/* B2 has no all-time counter of qBittorrent. The Statistics page shows the recorded totals. */}
            <h2 className="text-sm font-medium">{recorded ? "Library" : "All time"}</h2>
            <dl className={cn("mt-2 grid gap-2 xl:grid-cols-2 xl:gap-y-3", recorded ? "grid-cols-2" : "grid-cols-3")}>
              {([
                ...(recorded ? [] : [
                  ["Uploaded", formatBytes(g.alltimeUl)],
                  ["Downloaded", formatBytes(g.alltimeDl)],
                  ["Ratio", g.globalRatio.toFixed(2), getRatioColor(g.globalRatio)],
                ]),
                ["Torrents", num(g.totalTorrents)],
                ["Total size", formatBytes(g.totalSize)],
                ...(recorded ? [["Seeding size", formatBytes(g.totalSeedingSize)]] : []),
                ["Instances", `${g.connected} of ${g.total}`],
              ] as Array<[label: string, value: string, color?: string]>).map(([label, value, color]) => (
                <div key={label}>
                  <dd className="text-lg font-semibold tabular-nums tracking-tight" style={{ color }}>{value}</dd>
                  <dt className="text-xs text-muted-foreground">{label}{label.endsWith("size") && g.sizeCalculating && <> <SizeCalculatingMark /></>}</dt>
                </div>
              ))}
            </dl>
          </section>
        </aside>
      </div>

      <Drawer open={sheetIndex >= 0} onOpenChange={open => !open && setSheetId(null)}>
        {sheetIndex >= 0 && <InstanceSheet d={statsData[sheetIndex]} index={sheetIndex} unit={unit} day={day?.byInstance.get(statsData[sheetIndex].instance.id)} onClose={() => setSheetId(null)} />}
      </Drawer>
    </div>
  )
}
