/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { EmptyTile } from "@/components/kit/EmptyTile"
import { Facts, type Fact } from "@/components/kit/Facts"
import { Badge } from "@/components/ui/badge"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { TruncatedText } from "@/components/ui/truncated-text"
import { SIDEWAYS_PHONE_QUERY, useIsMobile, useMediaQuery } from "@/hooks/useMediaQuery"
import { formatDateOnly, formatISOTimestamp, formatRelativeTime, formatTimeOnly } from "@/lib/dateTimeUtils"
import { transfersApi, type TransferDetail, type TransferMember, type TransferState, type TransferStep } from "@/lib/transfers-api"
import { cn, formatBytes } from "@/lib/utils"
import * as TabsPrimitive from "@radix-ui/react-tabs"
import { useQuery } from "@tanstack/react-query"
import { AlertTriangle, CircleX, FolderOpen, Server } from "lucide-react"
import { Fragment, useState } from "react"
import { useTranslation } from "react-i18next"
import { shownSteps, stepKey } from "./transfer-display"
import { TransferHints } from "./TransferHints"
import { StartedBy, StateDetail, StateLabel } from "./TransferState"
import { useIncognitoDisplay } from "./use-incognito-display"
import { useInstanceName } from "./use-instance-name"

const tabs = ["overview", "torrents", "steps", "log"] as const
type DetailsTab = (typeof tabs)[number]

interface TransferDetailsDialogProps {
  transferId: number | null
  onClose: () => void
}

export function TransferDetailsDialog({ transferId, onClose }: TransferDetailsDialogProps) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const mobile = useIsMobile()
  const sideways = useMediaQuery(SIDEWAYS_PHONE_QUERY)
  const phone = mobile || sideways

  const [shown, setShown] = useState<{ id: number | null; open: boolean; tab: DetailsTab }>({ id: transferId, open: transferId !== null, tab: "overview" })
  if (transferId !== null && (!shown.open || transferId !== shown.id)) setShown({ id: transferId, open: true, tab: "overview" })
  if (transferId === null && shown.open) setShown({ ...shown, open: false })

  const detail = useQuery({
    queryKey: ["transfers", "detail", shown.id],
    queryFn: () => transfersApi.get(shown.id!),
    enabled: transferId !== null,
    refetchInterval: query => (query.state.data?.finishedAt ? false : 3000),
  })
  const data = detail.data

  const counts: Partial<Record<DetailsTab, number>> = data ? { torrents: data.members.length, log: data.log.length } : {}
  const label = (tab: DetailsTab) => (
    <>
      <span className="truncate">{t(`details.${tab}`)}</span>
      {counts[tab] !== undefined && <span className="tabular-nums text-muted-foreground">{counts[tab]}</span>}
    </>
  )

  return (
    <Dialog open={transferId !== null} onOpenChange={open => !open && onClose()}>
      <DialogContent className="flex h-[min(38rem,90dvh)] w-full max-w-[95vw] flex-col gap-0 p-0 !top-[5dvh] !translate-y-0 sm:!top-[7.5dvh] sm:h-[min(32rem,85dvh)] sm:max-w-2xl lg:max-w-4xl">
        <DialogHeader className="space-y-0 border-b px-6 pt-5 pb-4 pr-12 text-left max-md:px-4 max-md:pr-12">
          <DialogTitle>{t(data?.kind === "export" ? "details.exportTitle" : "details.title")}</DialogTitle>
          <DialogDescription className="[overflow-wrap:anywhere]">
            <TruncatedText wrap className="line-clamp-2">{data ? display.name(data) : t("details.loading")}</TruncatedText>
          </DialogDescription>
          {data && (
            <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1 pt-1 text-xs text-muted-foreground">
              <StateLabel state={data.state} />
              <span aria-hidden="true">·</span>
              <span>{t("row.route", { source: instanceName(data.sourceInstanceId), target: instanceName(data.targetInstanceId) })}</span>
            </div>
          )}
        </DialogHeader>

        {data && detail.isRefetchError && (
          <p role="status" className="flex shrink-0 items-start gap-1.5 border-b bg-amber-500/10 px-6 py-2 text-xs text-amber-700 max-md:px-4 dark:text-amber-400">
            <AlertTriangle className="mt-px size-3.5 shrink-0" aria-hidden="true" />
            <span className="[overflow-wrap:anywhere]">{t("details.stale", { message: display.text(detail.error.message) })}</span>
          </p>
        )}

        {data ? (
          <Tabs
            value={shown.tab}
            onValueChange={tab => setShown(prev => ({ ...prev, tab: tab as DetailsTab }))}
            orientation={phone ? "horizontal" : "vertical"}
            className={cn("min-h-0 flex-1", phone ? "flex-col" : "flex-row")}
          >
            {phone ? (
              <div className="shrink-0 overflow-x-auto border-b px-4 py-3">
                <TabsList aria-label={t("details.sections")} className="w-max min-w-full">
                  {tabs.map(tab => <TabsTrigger key={tab} value={tab} className="px-2.5">{label(tab)}</TabsTrigger>)}
                </TabsList>
              </div>
            ) : (
              <TabsPrimitive.List aria-label={t("details.sections")} className="flex w-44 shrink-0 flex-col gap-1 border-r p-3">
                {tabs.map(tab => (
                  <TabsTrigger
                    key={tab}
                    value={tab}
                    className="h-auto flex-none justify-between px-3 py-1.5 hover:bg-accent/50 data-[state=active]:bg-accent"
                  >
                    {label(tab)}
                  </TabsTrigger>
                ))}
              </TabsPrimitive.List>
            )}

            <TabsContent value="overview" className={panelClass}><Overview data={data} /></TabsContent>
            <TabsContent value="torrents" className={panelClass}><Torrents data={data} /></TabsContent>
            <TabsContent value="steps" className={panelClass}><Steps data={data} /></TabsContent>
            <TabsContent value="log" className={panelClass}><Log data={data} /></TabsContent>
          </Tabs>
        ) : (
          <div className="min-h-0 flex-1 px-6 py-5 max-md:px-4">
            {detail.isError ? (
              <p role="alert" className="text-sm text-destructive">{t("details.error", { message: display.text(detail.error.message) })}</p>
            ) : (
              <div className="space-y-3" aria-hidden="true">
                <span className="block h-4 w-2/3 animate-pulse rounded bg-muted" />
                <span className="block h-4 w-1/2 animate-pulse rounded bg-muted" />
                <span className="block h-4 w-3/5 animate-pulse rounded bg-muted" />
              </div>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  )
}

const panelClass = "min-h-0 min-w-0 overflow-y-auto px-6 py-5 max-md:px-4 max-md:py-4"

function Overview({ data }: { data: TransferDetail }) {
  const { t } = useTranslation("transfers")
  const display = useIncognitoDisplay()
  const facts: Fact[] = []
  if (data.progress || (data.state === "failed" && data.finishedAt === null && data.retryAt)) {
    facts.push({ label: t("columns.progress"), value: <StateDetail row={data} /> })
  }
  if (data.size > 0) facts.push({ label: t("summary.size"), value: formatBytes(data.size) })
  if (data.way !== "") facts.push({ label: t("summary.way"), value: t([`ways.${data.way}`, "ways.fallback"]) })
  if (data.kind === "transfer") {
    facts.push({
      label: t("form.after"),
      value: t(`form.afterOptions.${data.after}`),
      hint: data.leaveStopped ? t("details.leftStopped") : undefined,
    })
  }
  facts.push({
    label: t("columns.started"),
    value: formatISOTimestamp(data.createdAt),
    hint: <>{formatRelativeTime(data.createdAt)} · <StartedBy row={data} /></>,
  })
  if (data.finishedAt) {
    facts.push({ label: t("details.finished"), value: formatISOTimestamp(data.finishedAt), hint: formatRelativeTime(data.finishedAt) })
  }

  return (
    <div className="space-y-5">
      {data.failureReason && (
        <div role="alert" className="flex gap-2.5 rounded-lg border border-destructive/30 bg-destructive/5 p-3 text-sm">
          <CircleX className="mt-0.5 size-4 shrink-0 text-destructive" aria-hidden="true" />
          <div className="min-w-0 space-y-0.5">
            <p className="font-medium">{t("details.failure")}</p>
            <p className="text-muted-foreground [overflow-wrap:anywhere]">{display.text(data.failureReason)}</p>
          </div>
        </div>
      )}
      <Facts items={facts} />
      <TransferHints hints={data.hints} />
    </div>
  )
}

function Torrents({ data }: { data: TransferDetail }) {
  return (
    <ul className="divide-y overflow-hidden rounded-lg border">
      {data.members.map(member => <TorrentRow key={member.hash} member={member} crossSeed={member.hash !== data.hash} />)}
    </ul>
  )
}

function TorrentRow({ member, crossSeed }: { member: TransferMember; crossSeed: boolean }) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const name = display.name(member)
  const path = display.savePath(member.hash, member.savePath)
  const labels = display.incognito ? [] : [member.category, ...member.tags].filter(Boolean)

  return (
    <li className="space-y-1.5 px-4 py-3">
      <div className="flex items-start gap-2">
        <span className="min-w-0 flex-1 text-sm font-medium [overflow-wrap:anywhere]">{name}</span>
        {crossSeed && <Badge variant="secondary" className="shrink-0 font-normal">{t("summary.crossSeed")}</Badge>}
      </div>
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] items-start gap-x-2 gap-y-1 text-xs text-muted-foreground">
        <dt className="pt-px"><Server className="size-3.5" aria-label={t("details.instance")} /></dt>
        <dd>{instanceName(member.targetInstanceId)}</dd>
        <dt className="pt-px"><FolderOpen className="size-3.5" aria-label={t("form.savePath")} /></dt>
        <dd className="font-mono break-all">{path}</dd>
      </dl>
      {labels.length > 0 && (
        <div className="flex flex-wrap gap-1">
          {member.category && (
            <Tooltip>
              <TooltipTrigger asChild><Badge variant="secondary" className="font-normal">{member.category}</Badge></TooltipTrigger>
              <TooltipContent>{t("form.category")}</TooltipContent>
            </Tooltip>
          )}
          {member.tags.map(tag => (
            <Tooltip key={tag}>
              <TooltipTrigger asChild><Badge variant="outline" className="font-normal text-muted-foreground">{tag}</Badge></TooltipTrigger>
              <TooltipContent>{t("form.tags")}</TooltipContent>
            </Tooltip>
          ))}
        </div>
      )}
    </li>
  )
}

const lastDots: Partial<Record<TransferState, string>> = {
  waiting: "bg-muted-foreground",
  done: "bg-emerald-500",
  failed: "bg-destructive",
  cancelled: "bg-muted-foreground",
}

const torrentSteps = new Set(["add", "added"])

function Steps({ data }: { data: TransferDetail }) {
  const { t } = useTranslation("transfers")
  const display = useIncognitoDisplay()
  const steps = shownSteps(data.steps)
  const subject = (step: TransferStep) => {
    if (!torrentSteps.has(step.name)) return null
    const member = data.members.find(m => m.hash === step.detail)
    return member ? display.name(member) : null
  }
  const lastDot = lastDots[data.state] ?? "bg-primary motion-safe:animate-pulse"
  const days = dayBreaks(steps.map(step => step.at))

  return (
    <ol>
      {steps.map((step, index) => {
        const last = index === steps.length - 1
        const name = subject(step)
        return (
          <li key={index} className="relative pb-4 pl-6 last:pb-0">
            <span aria-hidden="true" className={cn("absolute top-1.5 left-0 size-2.5 rounded-full", last ? lastDot : "bg-muted-foreground/40")} />
            {!last && <span aria-hidden="true" className="absolute top-[18px] -bottom-1 left-[4.5px] w-px bg-border" />}
            <div className="flex items-baseline justify-between gap-3">
              <span className="text-sm">{t(stepKey(step.name) ?? "stepFallback")}</span>
              <Clock at={step.at} day={days[index]} className="text-xs" />
            </div>
            {name && <div className="mt-0.5 text-xs text-muted-foreground [overflow-wrap:anywhere]">{name}</div>}
          </li>
        )
      })}
    </ol>
  )
}

function Log({ data }: { data: TransferDetail }) {
  const { t } = useTranslation("transfers")
  const display = useIncognitoDisplay()
  if (data.log.length === 0) return <EmptyTile title={t("details.noLog")} />
  if (display.incognito) return <EmptyTile title={t("incognitoHidden")} />
  const days = dayBreaks(data.log.map(line => line.at))
  return (
    <ol className="divide-y overflow-hidden rounded-lg border bg-muted/20 font-mono text-xs">
      {data.log.map((line, index) => (
        <Fragment key={index}>
          {days[index] && <li className="bg-muted/40 px-3 py-1.5 font-sans font-medium text-muted-foreground">{days[index]}</li>}
          <li className="flex gap-3 px-3 py-2">
            <Clock at={line.at} />
            <span className="min-w-0 [overflow-wrap:anywhere]">{line.message}</span>
          </li>
        </Fragment>
      ))}
    </ol>
  )
}

function dayBreaks(times: readonly string[]): (string | null)[] {
  const days = times.map(at => formatDateOnly(Date.parse(at) / 1000))
  if (new Set(days).size < 2) return days.map(() => null)
  return days.map((day, index) => (index === 0 || day !== days[index - 1] ? day : null))
}

function Clock({ at, day, className }: { at: string; day?: string | null; className?: string }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time dateTime={at} className={cn("shrink-0 tabular-nums text-muted-foreground", className)}>
          {day && `${day} · `}
          {formatTimeOnly(Date.parse(at) / 1000, undefined, true)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{formatISOTimestamp(at)}</TooltipContent>
    </Tooltip>
  )
}
