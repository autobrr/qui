/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

// PROTOTYPE: throwaway helpers for the dashboard variants. Strings are hardcoded
// English and actions that would write are stubs.

import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { writeRaw } from "@/lib/client-settings"
import { resolveDashboardSizes } from "@/lib/dashboard-stream"
import { getLinuxTrackerDomain, useIncognitoMode } from "@/lib/incognito"
import { TRACKER_HEALTH_DISPLAY } from "@/lib/torrent-table/row-display"
import { cn } from "@/lib/utils"
import type { DashboardInstanceStats, GlobalStats } from "@/pages/Dashboard"
import { Link } from "@tanstack/react-router"
import { AlertTriangle, type LucideIcon } from "lucide-react"
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react"
import { toast } from "sonner"

export type Row = DashboardInstanceStats

export interface VariantProps {
  statsData: Row[]
  globalStats: GlobalStats
  /** Fixture data has no past, so the speed history starts with a made-up one. */
  seedHistory: boolean
}

type Health = "ok" | "loading" | "offline"

export function healthOf(d: Row): Health {
  if (d.isLoading && !d.stats) return "loading"
  if (!d.instance.connected || !d.stats) return "offline"
  return "ok"
}

export const HEALTH_DOT: Record<Health, string> = {
  ok: "bg-green-500",
  loading: "bg-muted-foreground/50 animate-pulse",
  offline: "bg-destructive",
}

export function offlineReason(d: Row): string {
  return d.instance.recentErrors?.[0]?.errorMessage ?? "No answer from qBittorrent"
}

export const num = (n: number | undefined) => (n ?? 0).toLocaleString()

export const ratioOf = (d: Row) =>
  d.serverState?.alltime_dl ? (d.serverState.alltime_ul || 0) / d.serverState.alltime_dl : 0

/** The Unique size of the instance, or its plain size while the first Copy group build runs. */
export const sizeOf = (d: Row) => resolveDashboardSizes([d.stats])

/** Share of the disk that is still free, counting only what the torrents use. */
export function freeShare(d: Row): number | null {
  const free = d.serverState?.free_space_on_disk
  const used = sizeOf(d).totalSize
  if (free === undefined || free < 0 || !used) return null
  return free / (free + used)
}

export const isLowDisk = (d: Row) => (freeShare(d) ?? 1) < 0.05

export interface Issue {
  key: string
  label: string
  count: number
  iconClass: string
  icon: LucideIcon
  instanceId: number
  instanceName: string
}

const ISSUE_KINDS: Array<{ key: string; label: string; iconClass: string; icon: LucideIcon }> = [
  { key: "unregistered", label: "unregistered", ...TRACKER_HEALTH_DISPLAY.unregistered },
  { key: "errored", label: "errored", iconClass: "text-destructive", icon: AlertTriangle },
  { key: "tracker_error", label: "tracker error", ...TRACKER_HEALTH_DISPLAY.tracker_error },
  { key: "tracker_down", label: "tracker down", ...TRACKER_HEALTH_DISPLAY.tracker_down },
]

export function issuesOf(d: Row): Issue[] {
  return ISSUE_KINDS.flatMap(({ key, label, iconClass, icon }) => {
    const count = d.torrentCounts?.status?.[key] || 0
    return count > 0 ? [{ key, label, count, iconClass, icon, instanceId: d.instance.id, instanceName: d.instance.name }] : []
  })
}

/** Opens the instance with the torrent list filtered to the issue, as the current dashboard does. */
export function IssueLink({ issue, className, children }: { issue: Issue; className?: string; children: ReactNode }) {
  return (
    <Link
      to="/instances/$instanceId"
      params={{ instanceId: String(issue.instanceId) }}
      onClick={(e) => {
        e.stopPropagation()
        writeRaw("qui-filters-global", JSON.stringify({ status: [issue.key], excludeStatus: [] }))
      }}
      className={className}
    >
      {children}
    </Link>
  )
}

/** `asText` is for a chip inside a row that is itself a link or a button. */
export function IssueChip({ issue, withLabel, asText }: { issue: Issue; withLabel?: boolean; asText?: boolean }) {
  const Icon = issue.icon
  const className = cn("inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-xs font-medium tabular-nums", issue.iconClass)
  const content = (
    <>
      <Icon className="size-3 shrink-0" aria-hidden="true" />
      {num(issue.count)}
      {withLabel ? <span className="font-normal">{issue.label}</span> : <span className="sr-only">{issue.label}</span>}
    </>
  )
  if (asText) return <span className={className}>{content}</span>
  return <IssueLink issue={issue} className={cn(className, "hover:bg-muted")}>{content}</IssueLink>
}

export const stubAction = (what: string) => toast(`${what}: not wired in the prototype`)

export function AddTorrentMenu({ statsData, children }: { statsData: Row[]; children: ReactNode }) {
  const connected = statsData.filter(d => d.instance.connected)
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>{children}</DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-52">
        {connected.map(({ instance }) => (
          <Link key={instance.id} to="/instances/$instanceId" params={{ instanceId: String(instance.id) }} search={{ modal: "add-torrent" }}>
            <DropdownMenuItem className="cursor-pointer">Add to {instance.name}</DropdownMenuItem>
          </Link>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

export function useInstanceExtras(d: Row) {
  const { data, versionInfo } = useQBittorrentAppInfo(d.instance.id, {
    initialData: d.appInfo ?? undefined,
    fetchIfMissing: !d.appInfo && d.instance.connected,
  })
  const launchTime = data?.processInfo?.launchTime
  return {
    version: (data?.version || versionInfo?.appVersion || "").replace(/^v/, ""),
    webAPI: data?.webAPIVersion || versionInfo?.webAPIVersion || "",
    libtorrent: data?.buildInfo?.libtorrent || "",
    uptime: typeof launchTime === "number" && launchTime > 0 ? Math.max(0, Math.floor(Date.now() / 1000) - launchTime) : null,
  }
}

export interface History { dl: number[]; ul: number[] }

const SAMPLE_MS = 2000

function seedSeries(now: number, size: number): number[] {
  const out: number[] = []
  let v = now
  for (let i = 0; i < size; i++) {
    out.unshift(Math.max(0, v))
    v = v * (0.9 + Math.random() * 0.22) + (Math.random() - 0.5) * now * 0.08
  }
  return out
}

function flatSeries(now: number, size: number): number[] {
  const out = Array<number>(size).fill(0)
  out[size - 1] = now
  return out
}

/** Rolling speed samples per instance, kept only while the dashboard is open. */
export function useSpeedHistory(statsData: Row[], seed: boolean, size = 90): Map<number, History> {
  const latest = useRef(statsData)
  useEffect(() => { latest.current = statsData }, [statsData])
  const [history, setHistory] = useState(() => new Map<number, History>())

  useEffect(() => {
    const sample = () => setHistory((prev) => {
      const next = new Map<number, History>()
      for (const d of latest.current) {
        const dl = d.stats?.totalDownloadSpeed || 0
        const ul = d.stats?.totalUploadSpeed || 0
        const old = prev.get(d.instance.id)
        if (!old) {
          const start = seed ? seedSeries : flatSeries
          next.set(d.instance.id, { dl: start(dl, size), ul: start(ul, size) })
        } else {
          next.set(d.instance.id, { dl: [...old.dl.slice(1), dl], ul: [...old.ul.slice(1), ul] })
        }
      }
      return next
    })
    sample()
    const id = setInterval(sample, SAMPLE_MS)
    return () => clearInterval(id)
  }, [seed, size])

  return history
}

export function sumSeries(history: Map<number, History>, key: keyof History): number[] {
  let out: number[] = []
  for (const h of history.values()) {
    out = out.length ? out.map((v, i) => v + h[key][i]) : [...h[key]]
  }
  return out
}

export function Sparkline({ values, className }: { values: number[] | undefined; className?: string }) {
  if (!values || values.length < 2) return <span className={className} />
  const max = Math.max(1, ...values)
  const points = values.map((v, i) => `${(i / (values.length - 1)) * 100},${22 - (v / max) * 20}`).join(" ")
  return (
    <svg viewBox="0 0 100 24" preserveAspectRatio="none" className={className} aria-hidden="true">
      <polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.5" vectorEffect="non-scaling-stroke" strokeLinejoin="round" />
    </svg>
  )
}

export interface TrackerRow {
  domain: string
  name: string
  uploaded: number
  downloaded: number
  totalSize: number
  count: number
}

/** Tracker totals over all instances, most uploaded first. The rename and merge rules of the real table are left out. */
export function useTrackerRows(statsData: Row[]): TrackerRow[] {
  const [incognito] = useIncognitoMode()
  return useMemo(() => {
    const byDomain = new Map<string, TrackerRow>()
    for (const { torrentCounts } of statsData) {
      for (const [domain, s] of Object.entries(torrentCounts?.trackerTransfers ?? {})) {
        const row = byDomain.get(domain) ?? { domain, name: incognito ? getLinuxTrackerDomain(domain) : domain, uploaded: 0, downloaded: 0, totalSize: 0, count: 0 }
        row.uploaded += s.uploaded
        row.downloaded += s.downloaded
        row.totalSize += s.totalSize
        row.count += s.count
        byDomain.set(domain, row)
      }
    }
    return [...byDomain.values()].sort((a, b) => b.uploaded - a.uploaded)
  }, [statsData, incognito])
}

export const trackerRatio = (t: TrackerRow) => (t.downloaded > 0 ? t.uploaded / t.downloaded : 0)
