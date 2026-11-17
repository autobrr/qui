/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { EmptyTile } from "@/components/kit/EmptyTile"
import { Section } from "@/components/kit/Section"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { Badge } from "@/components/ui/badge"
import { Progress } from "@/components/ui/progress"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TorrentName } from "@/components/ui/torrent-name"
import { formatNarrowAge, formatRelativeTime } from "@/lib/dateTimeUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { transfersApi, type TransferFilter, type TransferRow, type TransferState } from "@/lib/transfers-api"
import { cn } from "@/lib/utils"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { Ban, CircleCheck, CircleX, Clock, FileText, Loader2, RotateCcw, Trash2, X, type LucideIcon } from "lucide-react"
import { useId, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { RowActions, type RowAction } from "./RowActions"
import { canCancel, canRemove, canRetry, percentOf, timeLeftMs, waitsForRetry } from "./transfer-display"
import { TransferDetailsSheet } from "./TransferDetailsSheet"
import { TransferHints } from "./TransferHints"
import { useIncognitoDisplay } from "./use-incognito-display"
import { useInstanceName } from "./use-instance-name"

const chips: readonly TransferFilter[] = ["all", "active", "waiting", "finished", "failed"]
const retryKey = ["transfers", "retry"]
/** The list of the backend holds at most this many rows. */
const listLimit = 500

/** The icon of each state, in the colors of the status icons of Search history. */
const stateIcons: Record<TransferState, { icon: LucideIcon; className: string }> = {
  waiting: { icon: Clock, className: "text-muted-foreground" },
  copying: { icon: Loader2, className: "motion-safe:animate-spin" },
  checking: { icon: Loader2, className: "motion-safe:animate-spin" },
  finishing: { icon: Loader2, className: "motion-safe:animate-spin" },
  done: { icon: CircleCheck, className: "text-emerald-500" },
  failed: { icon: CircleX, className: "text-destructive" },
  cancelled: { icon: Ban, className: "text-muted-foreground" },
}

/** The Transfers section of the Transfers page: the Transfers and the Export runs, newest first. */
export function TransfersTab() {
  const { t, i18n } = useTranslation("transfers")
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<TransferFilter>("all")
  const [detailsId, setDetailsId] = useState<number | null>(null)
  // The row stays while the dialog fades out.
  const [cancelling, setCancelling] = useState<{ row: TransferRow; open: boolean } | null>(null)

  const list = useQuery({
    queryKey: ["transfers", "list", filter],
    queryFn: () => transfersApi.list(filter),
    // The rows and the counts stay while the rows of another filter load.
    placeholderData: keepPreviousData,
    // Progress and speed move while a Transfer runs.
    refetchInterval: 3000,
  })

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["transfers"] })
  const display = useIncognitoDisplay()
  const onError = (error: Error) => toast.error(t("actions.failed", { message: display.text(error.message) }))
  const retry = useMutation({
    mutationKey: retryKey,
    mutationFn: transfersApi.retry,
    onSuccess: () => { toast.success(t("actions.retried")); void refresh() },
    onError,
  })
  const cancel = useMutation({
    mutationFn: transfersApi.cancel,
    onSuccess: () => { toast.success(t("actions.cancelled")); void refresh() },
    onError,
  })
  const remove = useMutation({
    mutationFn: transfersApi.remove,
    onSuccess: () => { toast.success(t("actions.removed")); void refresh() },
    onError,
  })

  const retrying = usePendingIds(retryKey)
  const counts = list.data?.counts
  const rows = list.data?.rows ?? []
  const formatCount = useMemo(() => new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language), [i18n.resolvedLanguage, i18n.language])
  const countOf = (chip: TransferFilter) => counts && (chip === "all" ? counts.active + counts.waiting + counts.finished + counts.failed : counts[chip])

  return (
    <>
      <Section
        title={t("tabs.transfers")}
        description={t("list.description")}
        footer={rows.length >= listLimit ? t("list.limit") : undefined}
        queries={[list]}
      >
        <div className="space-y-4">
          {/* A phone scrolls the filters sideways when their labels do not fit. */}
          <div className="overflow-x-auto">
            <Tabs value={filter} onValueChange={value => setFilter(value as TransferFilter)}>
              <TabsList aria-label={t("chips.label")} className="max-sm:w-max max-sm:min-w-full">
                {chips.map(chip => (
                  <TabsTrigger key={chip} value={chip} className="gap-1.5 px-3 max-sm:gap-1 max-sm:px-1.5 max-sm:text-xs">
                    {t(`chips.${chip}`)}
                    {counts && <span className="tabular-nums text-muted-foreground">{formatCount.format(countOf(chip)!)}</span>}
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </div>

          {rows.length === 0 ? (
            <EmptyTile title={t("list.empty")} />
          ) : (
            // The table reaches the edges of the card, as the tables of Statistics do, and fits its columns to the card.
            <div className="@container -mx-6 border-t max-sm:-mx-4">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="w-full pl-6 max-sm:pl-4">{t("columns.torrent")}</TableHead>
                    <TableHead className="hidden @lg:table-cell">{t("columns.state")}</TableHead>
                    <TableHead className="hidden @2xl:table-cell">{t("columns.started")}</TableHead>
                    <TableHead pinEnd className="pr-6 max-sm:pr-4"><span className="sr-only">{t("columns.actions")}</span></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map(row => (
                    <TransferTableRow
                      key={row.id}
                      row={row}
                      onDetails={() => setDetailsId(row.id)}
                      retrying={retrying.has(row.id)}
                      onRetry={() => retry.mutate(row.id)}
                      onCancel={() => setCancelling({ row, open: true })}
                      onRemove={() => remove.mutate(row.id)}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </div>
      </Section>

      <TransferDetailsSheet transferId={detailsId} onClose={() => setDetailsId(null)} />

      {cancelling && (
        <ConfirmDialog
          open={cancelling.open}
          onOpenChange={open => setCancelling(prev => prev && { ...prev, open })}
          title={t("cancelDialog.title")}
          description={t("cancelDialog.description", { name: display.name(cancelling.row) })}
          confirmLabel={t("cancelDialog.confirm")}
          cancelLabel={t("cancelDialog.keep")}
          onConfirm={() => cancel.mutate(cancelling.row.id)}
        />
      )}
    </>
  )
}

interface TransferTableRowProps {
  row: TransferRow
  /** A Retry of the row waits for its answer. */
  retrying: boolean
  onDetails: () => void
  onRetry: () => void
  onCancel: () => void
  onRemove: () => void
}

function TransferTableRow({ row, retrying, onDetails, onRetry, onCancel, onRemove }: TransferTableRowProps) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const nameId = useId()
  const startedBy = row.startedBy === "user"
    ? t("row.byUser")
    : row.ruleName
      ? t("row.byRule", { name: row.ruleName })
      : t("row.byDeletedRule")
  const started = formatRelativeTime(row.createdAt)
  const actions: RowAction[] = [
    { label: t("actions.details"), icon: FileText, onSelect: onDetails },
    ...(canRetry(row) ? [{ label: t("actions.retry"), icon: RotateCcw, onSelect: onRetry, pending: retrying }] : []),
    ...(canCancel(row) ? [{ label: t("actions.cancel"), icon: X, onSelect: onCancel }] : []),
    ...(canRemove(row) ? [{ label: t("actions.remove"), icon: Trash2, onSelect: onRemove, destructive: true }] : []),
  ]

  return (
    <TableRow data-transfer-id={row.id}>
      <TableCell className="w-full max-w-0 pl-6 align-top whitespace-normal max-sm:pl-4">
        {/* The name wraps in full on touch, where no pointer shows its title. */}
        <div id={nameId} className="font-medium pointer-fine:line-clamp-2"><TorrentName name={display.name(row)} /></div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
          <span>{t("row.route", { source: instanceName(row.sourceInstanceId), target: instanceName(row.targetInstanceId) })}</span>
          {row.way !== "" && (
            <>
              <span aria-hidden="true">·</span>
              <span>{t([`ways.${row.way}`, "ways.fallback"])}</span>
            </>
          )}
          {row.torrents > 1 && <Mark>{t("row.crossSeeds", { count: row.torrents - 1 })}</Mark>}
          {row.kind === "export" && <Mark>{t("row.export")}</Mark>}
        </div>
        {/* A narrow table has no State or Started column, so the row names them here. The dot stays with the fact before it. */}
        <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground @2xl:hidden">
          <span className="flex items-center gap-1.5 @lg:hidden">
            <StateLabel state={row.state} />
            <span aria-hidden="true">·</span>
          </span>
          <span>{started}</span>
          <span aria-hidden="true">·</span>
          <span className="[overflow-wrap:anywhere]">{startedBy}</span>
        </div>
        <div className="@lg:hidden">
          <StateDetail row={row} />
        </div>
        {row.failureReason && <p className="mt-1 text-xs text-destructive [overflow-wrap:anywhere]">{display.text(row.failureReason)}</p>}
        {row.way === "bittorrent" && row.hints.length > 0 && <div className="mt-1"><TransferHints hints={row.hints} /></div>}
      </TableCell>
      <TableCell className="hidden align-top @lg:table-cell">
        <StateLabel state={row.state} className="text-sm" />
        <StateDetail row={row} />
      </TableCell>
      <TableCell className="hidden align-top @2xl:table-cell">
        <div>{started}</div>
        {/* The name column takes the free width, so the by-line asks for its own width up to 15rem before it wraps. */}
        <div className="w-max max-w-60 text-xs whitespace-normal text-muted-foreground [overflow-wrap:anywhere]">{startedBy}</div>
      </TableCell>
      <TableCell pinEnd className="pr-6 max-sm:pr-4">
        <RowActions actions={actions} describedBy={nameId} />
      </TableCell>
    </TableRow>
  )
}

/** A small mark after the facts of a row, as Statistics marks a deleted torrent or a cross-seed. */
function Mark({ children }: { children: string }) {
  return <Badge variant="outline" className="py-0 text-[10px] font-normal text-muted-foreground">{children}</Badge>
}

function StateLabel({ state, className }: { state: TransferState; className?: string }) {
  const { t } = useTranslation("transfers")
  const { icon: Icon, className: iconClass } = stateIcons[state] ?? stateIcons.waiting
  return (
    <span className={cn("inline-flex items-center gap-1.5 text-foreground", className)}>
      <Icon aria-hidden="true" className={cn("size-3.5 shrink-0", iconClass)} />
      {t([`states.${state}`, "states.fallback"])}
    </span>
  )
}

/** The progress of a run while its files pass, and the time of the retry of a failure that waits. */
function StateDetail({ row }: { row: TransferRow }) {
  const { t, i18n } = useTranslation("transfers")
  const [speedUnit] = useSpeedUnits()
  if (row.progress) {
    const percent = percentOf(row.progress)
    const left = timeLeftMs(row.progress)
    const facts = [
      new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language, { style: "percent" }).format(percent / 100),
      ...(row.progress.speed > 0 ? [formatSpeedWithUnit(row.progress.speed, speedUnit)] : []),
      ...(left === null ? [] : [t("row.timeLeft", { time: formatNarrowAge(left) })]),
    ]
    return (
      <div className="mt-1.5 w-full max-w-48 space-y-1">
        <Progress value={percent} aria-label={t("columns.progress")} className="h-1.5" />
        <div className="text-xs whitespace-nowrap text-muted-foreground tabular-nums">{facts.join(" · ")}</div>
      </div>
    )
  }
  if (waitsForRetry(row)) {
    return <div className="mt-1 text-xs text-muted-foreground">{t("row.retryAt", { time: formatRelativeTime(row.retryAt) })}</div>
  }
  return null
}
