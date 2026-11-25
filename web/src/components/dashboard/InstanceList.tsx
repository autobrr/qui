/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePointedLayer, usePointLayer } from "@/components/dashboard/pointed-layer"
import { InstanceMarkIcon } from "@/components/instances/marks/InstanceMarkIcon"
import { StreamChart } from "@/components/dashboard/StreamChart"
import { SizeCalculatingMark } from "@/components/dashboard/UniqueSize"
import { Button } from "@/components/ui/button"
import { Drawer, DrawerContent, DrawerDescription, DrawerHeader, DrawerTitle } from "@/components/ui/drawer"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDashboardOpenInstances } from "@/hooks/useDashboardOpenInstances"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useQBittorrentAppInfo } from "@/hooks/useQBittorrentAppInfo"
import { api } from "@/lib/api"
import { useClientSettingsSettled, writeClientSetting } from "@/lib/client-settings"
import {
  allTimeTransfer,
  connectionStatusOf,
  instanceHealth,
  instanceIssues,
  ISSUE_DISPLAY,
  issueClass,
  layerColor,
  MASK,
  offlineCause,
  OTHER_LAYER,
  type ChartLayer,
  type DashboardInstanceStats,
  type InstanceIssue
} from "@/lib/dashboard-overview"
import { resolveDashboardDataStatusKind, resolveDashboardSizes } from "@/lib/dashboard-stream"
import { formatErrorMessage } from "@/lib/format-error-message"
import { useIncognitoMode } from "@/lib/incognito"
import { formatSpeedWithUnit, useSpeedUnits, type SpeedUnit } from "@/lib/speedUnits"
import { formatRatio } from "@/lib/statistics"
import { cn, formatBytes, formatBytesOrFallback, formatDuration } from "@/lib/utils"
import type { TransferHistoryBytes } from "@/types"
import { useMutation, useQueryClient } from "@tanstack/react-query"
import { Link } from "@tanstack/react-router"
import { AlertCircle, ArrowDown, ArrowUp, ChevronDown, ChevronRight, ChevronsDownUp, ChevronsUpDown, Database, Plus, RefreshCcw, Turtle } from "lucide-react"
import { memo, useState, type KeyboardEvent, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

export interface InstanceDay extends TransferHistoryBytes {
  hours: ChartLayer
}

export interface InstanceListProps {
  statsData: DashboardInstanceStats[]
  layerOf: Map<number, number>
  days?: Map<number, InstanceDay>
  dayUploaded?: number
}

function useIssueLabel() {
  const { t } = useTranslation("dashboard")
  return (issue: InstanceIssue) => t(`issues.${issue.kind}`, { count: issue.count })
}

export function IssueLink({ issue, className, children }: { issue: InstanceIssue; className?: string; children: ReactNode }) {
  return (
    <Link
      to="/instances/$instanceId"
      params={{ instanceId: String(issue.instanceId) }}
      onClick={event => {
        event.stopPropagation()
        writeClientSetting("qui-filters-global", { status: [issue.kind], excludeStatus: [] })
      }}
      className={className}
    >
      {children}
    </Link>
  )
}

export function IssueIcon({ issue, className }: { issue: InstanceIssue; className?: string }) {
  const { icon: Icon, iconClass } = ISSUE_DISPLAY[issue.kind]
  return <Icon className={cn("shrink-0", iconClass, className)} aria-hidden="true" />
}

function useCount(): (value: number | undefined) => string {
  const { i18n } = useTranslation()
  const locale = i18n.resolvedLanguage ?? i18n.language
  return value => (value ?? 0).toLocaleString(locale)
}

function IssueCounts({ issues, links }: { issues: InstanceIssue[]; links: boolean }) {
  const label = useIssueLabel()
  const count = useCount()
  return (
    <span className="flex justify-end">
      {issues.map(issue => {
        const content = (
          <>
            <IssueIcon issue={issue} className="size-3" />
            {count(issue.count)}
            <span className="sr-only">{label(issue)}</span>
          </>
        )
        const className = cn("inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-xs font-medium tabular-nums", issueClass(issue))
        return links
          ? <IssueLink key={issue.kind} issue={issue} className={cn(className, "hover:bg-muted")}>{content}</IssueLink>
          : <span key={issue.kind} className={className}>{content}</span>
      })}
    </span>
  )
}

function Swatch({ d, layer }: { d: DashboardInstanceStats; layer: number }) {
  const pointed = usePointedLayer() === layer
  const health = instanceHealth(d)
  const mark = pointed || undefined
  if (health === "offline") return <span data-pointed={mark} className="size-2.5 shrink-0 rounded-full bg-destructive" />
  if (health === "loading") return <span data-pointed={mark} className="size-2.5 shrink-0 animate-pulse rounded-full bg-muted-foreground/50" />
  return <span data-pointed={mark} className="size-2.5 shrink-0 rounded-[3px]" style={{ background: pointed ? "var(--primary)" : layerColor(layer) }} />
}

function DataStatus({ d }: { d: DashboardInstanceStats }) {
  const { t } = useTranslation("dashboard")
  const kind = resolveDashboardDataStatusKind({
    streamError: d.streamError,
    fallbackActive: d.isDashboardStatsFallbackActive,
    cacheMetadata: d.cacheMetadata,
    isFirstLoad: d.isLoading && !d.stats,
    streamConnected: d.streamConnected,
  })
  const status = kind === "error"
    ? { Icon: AlertCircle, className: "text-destructive", label: t("instances.streamStatus.error") }
    : kind === "cached"
      ? { Icon: Database, className: d.cacheMetadata?.isStale ? "text-amber-500" : "text-blue-500", label: t("instances.streamStatus.cached") }
      : kind === "fallback"
        ? { Icon: RefreshCcw, className: "text-muted-foreground", label: t("instances.streamStatus.fallback") }
        : undefined
  if (!status) return null
  return (
    <Tooltip>
      <TooltipTrigger asChild openOnTap="only">
        <span role="img" aria-label={status.label} className="shrink-0">
          <status.Icon className={cn("size-3.5", status.className)} aria-hidden="true" />
        </span>
      </TooltipTrigger>
      <TooltipContent>{status.label}</TooltipContent>
    </Tooltip>
  )
}

function useOfflineText() {
  const { t } = useTranslation("dashboard")
  const [incognito] = useIncognitoMode()
  return (d: DashboardInstanceStats) => {
    const cause = offlineCause(d)
    switch (cause.kind) {
      case "password":
        return t("offline.password")
      case "error":
        return incognito ? t("offline.hidden") : t("offline.cause", { cause: formatErrorMessage(cause.message) })
      default:
        return t("offline.noAnswer")
    }
  }
}

function Speed({ value, unit, direction }: { value: number; unit: SpeedUnit; direction: "up" | "down" }) {
  const Icon = direction === "up" ? ArrowUp : ArrowDown
  return (
    <span className={cn("flex items-center justify-end gap-0.5 whitespace-nowrap", !value && "text-muted-foreground/60")}>
      <Icon className="size-3" aria-hidden="true" />{formatSpeedWithUnit(value, unit)}
    </span>
  )
}

export function InstanceList({ statsData, layerOf, days, dayUploaded }: InstanceListProps) {
  const desktop = useMediaQuery("(min-width: 1024px)")
  const [sheetId, setSheetId] = useState<number | null>(null)
  const sheet = statsData.find(d => d.instance.id === sheetId)

  if (desktop) {
    return <InstanceTable statsData={statsData} layerOf={layerOf} days={days} dayUploaded={dayUploaded} />
  }
  return (
    <>
      <InstanceLines statsData={statsData} layerOf={layerOf} onOpen={setSheetId} />
      <Drawer open={sheet !== undefined} onOpenChange={isOpen => !isOpen && setSheetId(null)}>
        {sheet && <InstanceSheet d={sheet} layer={layerOf.get(sheet.instance.id) ?? OTHER_LAYER} day={days?.get(sheet.instance.id)} hasHistory={days !== undefined} onClose={() => setSheetId(null)} />}
      </Drawer>
    </>
  )
}

function InstanceLines({ statsData, layerOf, onOpen }: { statsData: DashboardInstanceStats[]; layerOf: Map<number, number>; onOpen: (id: number) => void }) {
  const { t } = useTranslation("dashboard")
  const [unit] = useSpeedUnits()
  const offlineText = useOfflineText()

  return (
    <ul aria-label={t("instances.title")} className="border-t border-border/60">
      {statsData.map(d => {
        const health = instanceHealth(d)
        const up = d.stats?.totalUploadSpeed ?? 0
        const down = d.stats?.totalDownloadSpeed ?? 0
        return (
          <li key={d.instance.id} className="border-b border-border/60 has-data-pointed:bg-muted/50">
            <button type="button" disabled={health === "loading"} onClick={() => onOpen(d.instance.id)} className="flex min-h-14 w-full items-center gap-3 px-4 py-2 text-left active:bg-muted/50">
              <Swatch d={d} layer={layerOf.get(d.instance.id) ?? OTHER_LAYER} />
              <span className="min-w-0 flex-1">
                <span className="flex min-w-0 items-center gap-2">
                  <InstanceMarkIcon instanceId={d.instance.id} problem={!d.instance.connected} />
                  <span className="truncate font-medium">{d.instance.name}</span>
                  <DataStatus d={d} />
                  {d.altSpeedEnabled && <Turtle className="size-3.5 shrink-0 text-destructive" aria-label={t("details.altSpeedOn")} />}
                  {health === "ok" && <span className="ml-auto shrink-0"><IssueCounts issues={instanceIssues(d)} links={false} /></span>}
                </span>
                {health === "ok" ? (
                  <span className="flex items-center gap-3 text-xs text-muted-foreground tabular-nums">
                    <Speed value={up} unit={unit} direction="up" />
                    <Speed value={down} unit={unit} direction="down" />
                  </span>
                ) : (
                  <span className={cn("block truncate text-xs", health === "offline" ? "text-destructive" : "animate-pulse text-muted-foreground")}>
                    {health === "offline" ? offlineText(d) : t("instances.loading")}
                  </span>
                )}
              </span>
              {health !== "loading" && <ChevronRight className="size-4 shrink-0 text-muted-foreground" />}
            </button>
          </li>
        )
      })}
    </ul>
  )
}

function InstanceTable({ statsData, layerOf, days, dayUploaded }: InstanceListProps) {
  const { t } = useTranslation("dashboard")
  const count = useCount()
  const [unit] = useSpeedUnits()
  const offlineText = useOfflineText()
  const point = usePointLayer()
  const columns = days ? 6 : 5
  const [openIds, setOpenIds] = useDashboardOpenInstances()
  const settled = useClientSettingsSettled()
  const ids = statsData.map(d => d.instance.id)
  const allOpen = ids.length > 0 && ids.every(id => openIds.includes(id))
  const onToggle = (id: number) => setOpenIds(open => open.includes(id) ? open.filter(other => other !== id) : [...open.filter(other => ids.includes(other)), id])
  const AllIcon = allOpen ? ChevronsDownUp : ChevronsUpDown
  const allLabel = allOpen ? t("instances.collapseAll") : t("instances.expandAll")

  return (
    <Table aria-label={t("instances.title")}>
      <TableHeader>
        <TableRow className="text-xs hover:bg-transparent">
          <TableHead className="pl-6">
            <span className="flex items-center gap-1">
              {t("instances.instance")}
              <Tooltip>
                <TooltipTrigger asChild>
                  <Button variant="ghost" size="icon" className="size-6 text-muted-foreground" aria-label={allLabel} disabled={!settled} onClick={() => setOpenIds(allOpen ? [] : ids)}>
                    <AllIcon className="size-3.5" />
                  </Button>
                </TooltipTrigger>
                <TooltipContent>{allLabel}</TooltipContent>
              </Tooltip>
            </span>
          </TableHead>
          {days && <TableHead>{t("instances.uploaded24h")}</TableHead>}
          <TableHead className="text-right">{t("instances.upload")}</TableHead>
          <TableHead className="text-right">{t("instances.download")}</TableHead>
          <TableHead className="text-right">{t("instances.torrents")}</TableHead>
          <TableHead className="pr-6 text-right">{t("instances.issues")}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {statsData.map(d => {
          const id = d.instance.id
          const layer = layerOf.get(id) ?? OTHER_LAYER
          const health = instanceHealth(d)
          const canOpen = settled && health !== "loading"
          const isOpen = canOpen && openIds.includes(id)
          const day = days?.get(id)
          const issues = instanceIssues(d)
          const onKeyDown = (event: KeyboardEvent) => {
            if (canOpen && event.target === event.currentTarget && (event.key === "Enter" || event.key === " ")) {
              event.preventDefault()
              onToggle(id)
            }
          }
          return [
            <TableRow
              key={id}
              data-state={isOpen ? "selected" : undefined}
              aria-expanded={canOpen ? isOpen : undefined}
              tabIndex={canOpen ? 0 : undefined}
              onClick={() => canOpen && onToggle(id)}
              onKeyDown={onKeyDown}
              onMouseEnter={() => point(layer)}
              onMouseLeave={() => point(undefined)}
              onFocus={() => point(layer)}
              onBlur={() => point(undefined)}
              className={cn("h-11 tabular-nums has-data-pointed:bg-muted/50", canOpen && "cursor-pointer")}
            >
              <TableCell className="max-w-[16rem] pl-6">
                <span className="flex min-w-0 items-center gap-2.5">
                  <Swatch d={d} layer={layer} />
                  <InstanceMarkIcon instanceId={id} problem={!d.instance.connected} />
                  {health === "ok" ? (
                    <Link to="/instances/$instanceId" params={{ instanceId: String(id) }} onClick={event => event.stopPropagation()} className="truncate font-medium underline-offset-4 hover:underline">
                      {d.instance.name}
                    </Link>
                  ) : <span className="truncate font-medium">{d.instance.name}</span>}
                  <DataStatus d={d} />
                  {health === "ok" && <AltSpeedButton instanceId={id} altSpeedEnabled={d.altSpeedEnabled} />}
                  {canOpen && <ChevronDown className={cn("size-3.5 shrink-0 text-muted-foreground transition-transform motion-reduce:transition-none", isOpen && "rotate-180")} />}
                </span>
              </TableCell>
              {health === "ok" ? (
                <>
                  {days && (
                    <TableCell className="w-[30%]">
                      <span className="flex items-center gap-3">
                        <span className="h-1.5 flex-1 overflow-hidden rounded-full bg-muted [tr[data-state=selected]_&]:bg-foreground/10">
                          <span className="block h-full rounded-full" style={{ width: `${dayUploaded ? ((day?.uploaded ?? 0) / dayUploaded) * 100 : 0}%`, background: layerColor(layer) }} />
                        </span>
                        <span className="w-20 whitespace-nowrap text-right text-muted-foreground">{formatBytes(day?.uploaded ?? 0)}</span>
                      </span>
                    </TableCell>
                  )}
                  <TableCell className="text-right"><Speed value={d.stats?.totalUploadSpeed ?? 0} unit={unit} direction="up" /></TableCell>
                  <TableCell className="text-right"><Speed value={d.stats?.totalDownloadSpeed ?? 0} unit={unit} direction="down" /></TableCell>
                  <TableCell className="text-right text-muted-foreground">{count(d.torrentCounts?.total)}</TableCell>
                  <TableCell className="pr-6 text-right">
                    {issues.length > 0 ? <IssueCounts issues={issues} links /> : <span className="text-muted-foreground/50">{t("instances.noIssues")}</span>}
                  </TableCell>
                </>
              ) : (
                <TableCell colSpan={columns - 1} className="max-w-0 pr-6">
                  <span className={cn("block truncate text-sm", health === "offline" ? "text-destructive" : "animate-pulse text-muted-foreground")}>
                    {health === "offline" ? offlineText(d) : t("instances.loading")}
                  </span>
                </TableCell>
              )}
            </TableRow>,
            isOpen && (
              <TableRow key={`${id}-band`} className="bg-muted/30 hover:bg-muted/30">
                <TableCell colSpan={columns} className="whitespace-normal px-6 py-4">
                  {health === "offline" ? <OfflineDetails d={d} /> : <DetailBand d={d} layer={layer} day={day} hasHistory={days !== undefined} />}
                </TableCell>
              </TableRow>
            ),
          ]
        })}
      </TableBody>
    </Table>
  )
}

function OfflineDetails({ d, onClose }: { d: DashboardInstanceStats; onClose?: () => void }) {
  const { t } = useTranslation("dashboard")
  const offlineText = useOfflineText()
  return (
    <div className="space-y-3">
      <p className="break-words text-sm text-destructive">{offlineText(d)}</p>
      <Button asChild variant="secondary" size="sm" className="max-lg:h-11 max-lg:w-full" onClick={onClose}>
        <Link to="/settings" search={{ tab: "instances" as const }}>{t("details.openSettings")}</Link>
      </Button>
    </div>
  )
}

function DetailBand({ d, layer, day, hasHistory }: { d: DashboardInstanceStats; layer: number; day?: InstanceDay; hasHistory: boolean }) {
  const { t } = useTranslation("dashboard")
  return (
    <div className="@container">
      <div className="grid grid-cols-[18rem_minmax(0,1fr)] gap-x-10 gap-y-4 @[74rem]:grid-cols-[18rem_minmax(14rem,1fr)_36.5rem] @[100rem]:grid-cols-[18rem_minmax(14rem,1fr)_59rem]">
        <div className="min-w-0 space-y-2">
          <DetailSpeeds d={d} />
          <TransferTotals d={d} day={day} hasHistory={hasHistory} />
        </div>
        <div className="min-w-0">
          {hasHistory && (
            <StreamChart
              layers={day ? [{ ...day.hours, layer }] : []}
              kind="bars"
              label={t("details.chartLabel", { name: d.instance.name })}
              className="h-20 @[74rem]:h-36 @[100rem]:h-24"
            />
          )}
        </div>
        <div className="col-span-2 min-w-0 space-y-4 @[74rem]:col-span-1">
          <DetailIssues d={d} />
          <DetailFacts d={d} />
        </div>
      </div>
    </div>
  )
}

function TransferTotals({ d, day, hasHistory }: { d: DashboardInstanceStats; day?: InstanceDay; hasHistory: boolean }) {
  const { t } = useTranslation("dashboard")
  const rows = [
    ...(hasHistory ? [{ key: "day", label: t("details.last24h"), uploaded: day?.uploaded ?? 0, downloaded: day?.downloaded ?? 0 }] : []),
    { key: "all-time", label: t("details.allTime"), ...allTimeTransfer([d]) },
  ]
  return (
    <dl className="grid grid-cols-[auto_auto_auto_1fr] gap-x-3 gap-y-1 text-xs tabular-nums">
      {rows.map(row => (
        <div key={row.key} data-testid={`totals-${row.key}`} className="contents">
          <dt className="text-muted-foreground">{row.label}</dt>
          <dd className="flex items-center gap-0.5 whitespace-nowrap">
            <ArrowUp className="size-3 text-muted-foreground" aria-hidden="true" /><span className="sr-only">{t("details.uploaded")}</span>{formatBytes(row.uploaded)}
          </dd>
          <dd className="flex items-center gap-0.5 whitespace-nowrap">
            <ArrowDown className="size-3 text-muted-foreground" aria-hidden="true" /><span className="sr-only">{t("details.downloaded")}</span>{formatBytes(row.downloaded)}
          </dd>
          <dd className="whitespace-nowrap text-muted-foreground">{t("details.ratio", { ratio: formatRatio(row) ?? "–" })}</dd>
        </div>
      ))}
    </dl>
  )
}

function DetailSpeeds({ d }: { d: DashboardInstanceStats }) {
  const { t } = useTranslation("dashboard")
  const [unit] = useSpeedUnits()
  return (
    <div className="flex flex-wrap gap-x-6 gap-y-1 whitespace-nowrap text-xl font-semibold tabular-nums tracking-tight">
      <span className="flex items-center gap-1" aria-label={t("details.uploadSpeed")}><ArrowUp className="size-5 text-muted-foreground" aria-hidden="true" />{formatSpeedWithUnit(d.stats?.totalUploadSpeed ?? 0, unit)}</span>
      <span className="flex items-center gap-1" aria-label={t("details.downloadSpeed")}><ArrowDown className="size-5 text-muted-foreground" aria-hidden="true" />{formatSpeedWithUnit(d.stats?.totalDownloadSpeed ?? 0, unit)}</span>
    </div>
  )
}

function DetailIssues({ d }: { d: DashboardInstanceStats }) {
  const label = useIssueLabel()
  const issues = instanceIssues(d)
  if (issues.length === 0) return null
  return (
    <ul className="flex flex-wrap gap-x-4 gap-y-1">
      {issues.map(issue => (
        <li key={issue.kind}>
          <IssueLink issue={issue} className="flex min-h-9 items-center gap-1.5 rounded-md text-sm hover:underline">
            <IssueIcon issue={issue} className="size-4" />
            <span className={cn("tabular-nums", issueClass(issue))}>{label(issue)}</span>
          </IssueLink>
        </li>
      ))}
    </ul>
  )
}

function DetailFacts({ d }: { d: DashboardInstanceStats }) {
  const { t } = useTranslation("dashboard")
  const [incognito] = useIncognitoMode()
  const { data: appInfo, versionInfo } = useQBittorrentAppInfo(d.instance.id, {
    initialData: d.appInfo ?? undefined,
    fetchIfMissing: !d.appInfo && d.instance.connected,
  })
  const launchTime = appInfo?.processInfo?.launchTime
  const uptime = typeof launchTime === "number" && launchTime > 0 ? Math.max(0, Math.floor(Date.now() / 1000) - launchTime) : undefined
  const version = (appInfo?.version || versionInfo?.appVersion || "").replace(/^v/, "")
  const size = resolveDashboardSizes([d.stats])
  const free = d.serverState?.free_space_on_disk
  const connection = connectionStatusOf(d)
  const addresses = [d.serverState?.last_external_address_v4, d.serverState?.last_external_address_v6].filter(Boolean).join(", ")
  const unknown = t("details.unknown")
  const count = useCount()
  const facts: Array<{ key: string; label: string; value: ReactNode; wide?: boolean }> = [
    { key: "host", label: t("details.host"), value: incognito ? MASK : d.instance.host, wide: true },
    { key: "torrents", label: t("details.torrents"), value: count(d.torrentCounts?.total) },
    { key: "active", label: t("details.active"), value: count(d.torrentCounts?.status?.active) },
    { key: "downloading", label: t("details.downloading"), value: count(d.stats?.downloading) },
    { key: "seeding", label: t("details.seeding"), value: count(d.stats?.seeding) },
    { key: "size", label: t("details.size"), value: <>{formatBytes(size.totalSize)}{size.calculating && <> <SizeCalculatingMark /></>}</> },
    { key: "free", label: t("details.freeSpace"), value: formatBytesOrFallback(free ?? -1, unknown) },
    { key: "peers", label: t("details.peers"), value: count(d.serverState?.total_peer_connections) },
    { key: "uptime", label: t("details.uptime"), value: uptime !== undefined ? formatDuration(uptime) : unknown },
    { key: "version", label: t("details.version"), value: version || unknown },
    { key: "libtorrent", label: t("details.libtorrent"), value: appInfo?.buildInfo?.libtorrent || unknown },
    { key: "connection", label: t("details.connection"), value: connection ? t([`connection.${connection}`, "connection.other"]) : unknown },
    { key: "addresses", label: t("details.externalAddresses"), value: addresses ? (incognito ? MASK : addresses) : unknown, wide: true },
  ]

  return (
    <dl className="grid grid-cols-3 gap-x-4 gap-y-3 lg:grid-cols-5 @[100rem]:grid-cols-8">
      {facts.map(fact => (
        <div key={fact.key} data-testid={`fact-${fact.key}`} className={cn("min-w-0", fact.wide && "col-span-2")}>
          <dd className="truncate text-sm font-medium tabular-nums">{fact.value}</dd>
          <dt className="text-xs leading-tight text-muted-foreground">{fact.label}</dt>
        </div>
      ))}
    </dl>
  )
}

function DetailActions({ d, onClose }: { d: DashboardInstanceStats; onClose?: () => void }) {
  const { t } = useTranslation("dashboard")
  const instanceId = String(d.instance.id)
  return (
    <div className="flex flex-wrap items-center gap-2">
      <AltSpeedSwitch instanceId={d.instance.id} altSpeedEnabled={d.altSpeedEnabled} />
      <span className="flex flex-1 justify-end gap-2 max-lg:w-full">
        <Button asChild variant="secondary" size="sm" className="max-lg:h-11" onClick={onClose}>
          <Link to="/instances/$instanceId" params={{ instanceId }} search={{ modal: "add-torrent" }}><Plus />{t("details.addTorrent")}</Link>
        </Button>
        <Button asChild size="sm" className="max-lg:h-11 max-lg:flex-1" onClick={onClose}>
          <Link to="/instances/$instanceId" params={{ instanceId }}>{t("details.openTorrents")}</Link>
        </Button>
      </span>
    </div>
  )
}

type AltSpeedProps = { instanceId: number; altSpeedEnabled: boolean }

function useAltSpeed({ instanceId, altSpeedEnabled }: AltSpeedProps) {
  const { t } = useTranslation("dashboard")
  const [incognito] = useIncognitoMode()
  const queryClient = useQueryClient()
  const [answer, setAnswer] = useState<boolean>()
  if (answer !== undefined && answer === altSpeedEnabled) setAnswer(undefined)
  const enabled = answer ?? altSpeedEnabled
  const { mutate, isPending } = useMutation({
    mutationFn: () => api.toggleAlternativeSpeedLimits(instanceId),
    onMutate: () => setAnswer(!enabled),
    onSuccess: result => {
      setAnswer(result.enabled)
      return queryClient.invalidateQueries({ queryKey: ["torrents-list", instanceId] })
    },
    onError: (error: Error) => {
      setAnswer(undefined)
      toast.error(incognito ? t("common:errors.altSpeedToggleHidden") : t("common:errors.altSpeedToggle", { message: formatErrorMessage(error.message) }))
    },
  })
  return { enabled, isPending, toggle: () => mutate() }
}

const AltSpeedButton = memo(function AltSpeedButton(props: AltSpeedProps) {
  const { t } = useTranslation("dashboard")
  const { enabled, isPending, toggle } = useAltSpeed(props)
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={enabled ? t("instances.altSpeedTurnOff") : t("instances.altSpeedTurnOn")}
          disabled={isPending}
          onClick={event => {
            event.stopPropagation()
            toggle()
          }}
          className={cn("size-7 shrink-0", enabled ? "bg-destructive/10 text-destructive hover:bg-destructive/20 hover:text-destructive" : "text-muted-foreground")}
        >
          <Turtle className="size-4" />
        </Button>
      </TooltipTrigger>
      <TooltipContent>{enabled ? t("details.altSpeedOn") : t("instances.altSpeedOff")}</TooltipContent>
    </Tooltip>
  )
})

function AltSpeedSwitch(props: AltSpeedProps) {
  const { t } = useTranslation("dashboard")
  const { enabled, isPending, toggle } = useAltSpeed(props)
  const id = `alt-speed-${props.instanceId}`
  return (
    <label htmlFor={id} className="flex min-h-9 items-center gap-2 text-sm">
      {t("details.altSpeed")}
      <Switch id={id} checked={enabled} disabled={isPending} onCheckedChange={toggle} />
    </label>
  )
}

function InstanceSheet({ d, layer, day, hasHistory, onClose }: { d: DashboardInstanceStats; layer: number; day?: InstanceDay; hasHistory: boolean; onClose: () => void }) {
  const [incognito] = useIncognitoMode()
  const health = instanceHealth(d)
  return (
    <DrawerContent>
      <DrawerHeader className="text-left">
        <DrawerTitle className="flex items-center gap-2 text-xl">
          <Swatch d={d} layer={layer} />
          <InstanceMarkIcon instanceId={d.instance.id} problem={!d.instance.connected} className="size-5" />
          <span className="truncate">{d.instance.name}</span>
        </DrawerTitle>
        <DrawerDescription className="truncate">{incognito ? MASK : d.instance.host}</DrawerDescription>
      </DrawerHeader>
      <div className="max-h-[70vh] space-y-4 overflow-y-auto px-4 pb-[calc(1.5rem+env(safe-area-inset-bottom))]">
        {health === "offline" ? <OfflineDetails d={d} onClose={onClose} /> : (
          <>
            <div className="space-y-1.5">
              <DetailSpeeds d={d} />
              <TransferTotals d={d} day={day} hasHistory={hasHistory} />
            </div>
            <DetailIssues d={d} />
            <DetailFacts d={d} />
            <DetailActions d={d} onClose={onClose} />
          </>
        )}
      </div>
    </DrawerContent>
  )
}
