/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { IssueIcon, IssueLink } from "@/components/dashboard/InstanceList"
import { SizeCalculatingMark } from "@/components/dashboard/UniqueSize"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  freeShare,
  instanceHealth,
  instanceIssues,
  isFirewalled,
  issueClass,
  isLowDisk,
  type DashboardInstanceStats
} from "@/lib/dashboard-overview"
import { resolveDashboardSizes } from "@/lib/dashboard-stream"
import { themeArea } from "@/lib/theme-areas"
import { cn, formatBytes } from "@/lib/utils"
import { Link } from "@tanstack/react-router"
import { HardDrive, ShieldAlert, Unplug } from "lucide-react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

function Section({ id, title, className, children }: { id: string; title: string; className?: string; children: ReactNode }) {
  return (
    <Card {...themeArea("dashboard-card")} role="region" aria-labelledby={id} className={cn("gap-3 py-4", className)}>
      <CardHeader className="px-4 lg:px-6">
        <CardTitle id={id}>{title}</CardTitle>
      </CardHeader>
      <CardContent className="px-4 lg:px-6">{children}</CardContent>
    </Card>
  )
}

/** The instances that are offline, are firewalled, or have a disk that is almost full, and the torrent counts with a problem. */
export function NeedsAttention({ statsData, className }: { statsData: DashboardInstanceStats[]; className?: string }) {
  const { t } = useTranslation("dashboard")
  const offline = statsData.filter(d => instanceHealth(d) === "offline")
  const lowDisk = statsData.filter(d => instanceHealth(d) === "ok" && isLowDisk(d))
  const firewalled = statsData.filter(isFirewalled)
  const issues = statsData.flatMap(instanceIssues).sort((a, b) => b.count - a.count)
  const empty = offline.length + lowDisk.length + firewalled.length + issues.length === 0
  const item = "-mx-2 flex min-h-9 items-center gap-2 rounded-md px-2 py-1.5"

  return (
    <Section id="dashboard-attention" title={t("attention.title")} className={className}>
      {empty ? <p className="text-sm text-muted-foreground">{t("attention.none")}</p> : (
        <ul className="space-y-0.5 text-sm" data-testid="attention-list">
          {offline.map(d => (
            <li key={`offline-${d.instance.id}`} data-tone="red">
              <Link to="/settings" search={{ tab: "instances" as const }} className={cn(item, "text-destructive hover:bg-muted")}>
                <Unplug className="size-4 shrink-0" aria-hidden="true" /><span className="truncate">{t("attention.offline", { name: d.instance.name })}</span>
              </Link>
            </li>
          ))}
          {lowDisk.map(d => (
            <li key={`disk-${d.instance.id}`} data-tone="red" className={cn(item, "text-destructive")}>
              <HardDrive className="size-4 shrink-0" aria-hidden="true" />
              <span className="truncate">{t("attention.lowDisk", { name: d.instance.name, free: formatBytes(d.serverState?.free_space_on_disk ?? 0) })}</span>
            </li>
          ))}
          {firewalled.map(d => (
            <li key={`firewalled-${d.instance.id}`} data-tone="yellow" className={cn(item, "text-yellow-600 dark:text-yellow-500")}>
              <ShieldAlert className="size-4 shrink-0" aria-hidden="true" /><span className="truncate">{t("attention.firewalled", { name: d.instance.name })}</span>
            </li>
          ))}
          {issues.map(issue => (
            <li key={`${issue.instanceId}-${issue.kind}`}>
              <IssueLink issue={issue} className={cn(item, "hover:bg-muted")}>
                <IssueIcon issue={issue} className="size-4" />
                <span className={cn("shrink-0 tabular-nums", issueClass(issue))}>{t(`issues.${issue.kind}`, { count: issue.count })}</span>
                <span className="ml-auto truncate text-muted-foreground">{issue.instanceName}</span>
              </IssueLink>
            </li>
          ))}
        </ul>
      )}
    </Section>
  )
}

/** The free disk space of each instance that answers. */
export function Storage({ statsData, className }: { statsData: DashboardInstanceStats[]; className?: string }) {
  const { t } = useTranslation("dashboard")
  // qBittorrent reports -1 when it cannot read the free space.
  const rows = statsData.filter(d => instanceHealth(d) === "ok" && (d.serverState?.free_space_on_disk ?? -1) >= 0)
  if (rows.length === 0) return null

  return (
    <Section id="dashboard-storage" title={t("storage.title")} className={className}>
      <ul className="space-y-3">
        {rows.map(d => {
          const low = isLowDisk(d)
          return (
            <li key={d.instance.id} className="text-sm">
              <div className="flex justify-between gap-2 tabular-nums">
                <span className="truncate">{d.instance.name}</span>
                <span className={cn("shrink-0", low ? "text-destructive" : "text-muted-foreground")}>{t("storage.free", { size: formatBytes(d.serverState?.free_space_on_disk ?? 0) })}</span>
              </div>
              <div className="mt-1 h-1.5 overflow-hidden rounded-full bg-muted" aria-hidden="true">
                <div className={cn("h-full rounded-full", low ? "bg-destructive" : "bg-foreground/45")} style={{ width: `${(1 - (freeShare(d) ?? 1)) * 100}%` }} />
              </div>
            </li>
          )
        })}
      </ul>
    </Section>
  )
}

/** The torrents and the sizes of all instances, and the count of connected instances. */
export function Library({ statsData, className }: { statsData: DashboardInstanceStats[]; className?: string }) {
  const { t, i18n } = useTranslation("dashboard")
  const sizes = resolveDashboardSizes(statsData.map(d => d.stats))
  const torrents = statsData.reduce((sum, d) => sum + (d.torrentCounts?.total ?? 0), 0)
  const connected = statsData.filter(d => d.instance.connected).length
  const values: Array<{ key: string; label: string; value: string; calculating?: boolean }> = [
    { key: "torrents", label: t("library.torrents"), value: torrents.toLocaleString(i18n.resolvedLanguage ?? i18n.language) },
    { key: "instances", label: t("library.instances"), value: `${connected}/${statsData.length}` },
    { key: "total", label: t("library.totalSize"), value: formatBytes(sizes.totalSize), calculating: sizes.calculating },
    { key: "seeding", label: t("library.seedingSize"), value: formatBytes(sizes.seedingSize), calculating: sizes.calculating },
  ]

  return (
    <Section id="dashboard-library" title={t("library.title")} className={className}>
      <dl className="grid grid-cols-2 gap-2 xl:gap-y-3">
        {values.map(({ key, label, value, calculating }) => (
          <div key={key} data-testid={`library-${key}`}>
            <dd className="text-lg font-semibold tabular-nums tracking-tight">{value}</dd>
            <dt className="text-xs text-muted-foreground">{label}{calculating && <> <SizeCalculatingMark /></>}</dt>
          </div>
        ))}
      </dl>
    </Section>
  )
}
