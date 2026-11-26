/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { ConfirmDialog } from "@/components/kit/ConfirmDialog"
import { EmptyTile } from "@/components/kit/EmptyTile"
import { Section } from "@/components/kit/Section"
import { usePendingIds } from "@/components/kit/use-pending-ids"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TorrentName } from "@/components/ui/torrent-name"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { transferListQuery, transferPageSize, transfersApi, type TransferBulkAction, type TransferBulkRequest, type TransferBulkResult, type TransferFilter, type TransferRow } from "@/lib/transfers-api"
import { cn, formatBytes } from "@/lib/utils"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, ChevronRight, FileText, RotateCcw, Trash2, X, type LucideIcon } from "lucide-react"
import { useId, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { RowActions, type RowAction } from "./RowActions"
import { canCancel, canRemove, canRetry } from "./transfer-display"
import { fittingIds, noSelection, offeredActions, tickRow, type Selection } from "./transfer-selection"
import { TransferDetailsDialog } from "./TransferDetailsDialog"
import { TransferHints } from "./TransferHints"
import { TransferSelectionBar } from "./TransferSelectionBar"
import { StartedBy, StateDetail, StateLabel } from "./TransferState"
import { useIncognitoDisplay } from "./use-incognito-display"
import { useInstanceName } from "./use-instance-name"

const chips: readonly TransferFilter[] = ["all", "active", "waiting", "finished", "failed"]
const retryKey = ["transfers", "retry"]
const cancelKey = ["transfers", "cancel"]
const removeKey = ["transfers", "remove"]
const bulkKey = ["transfers", "bulk"]
const CHECKBOX_TAP = "relative pointer-coarse:after:absolute pointer-coarse:after:-inset-3"

type RowStep = "retry" | "cancel" | "remove"

export function TransfersTab({ pageSize = transferPageSize }: { pageSize?: number }) {
  const { t, i18n } = useTranslation("transfers")
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<TransferFilter>("all")
  const [page, setPage] = useState(0)
  const top = useRef<HTMLDivElement>(null)
  const [detailsId, setDetailsId] = useState<number | null>(null)
  const [cancelling, setCancelling] = useState<{ row: TransferRow; open: boolean } | null>(null)
  const [selection, setSelection] = useState<Selection>(noSelection)
  const lastTicked = useRef<number | null>(null)
  const [asking, setAsking] = useState<{ action: "cancel" | "remove"; open: boolean; count: number } | null>(null)

  const list = useQuery({
    ...transferListQuery(filter, page, pageSize),
    placeholderData: keepPreviousData,
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
    mutationKey: cancelKey,
    mutationFn: transfersApi.cancel,
    onSuccess: () => { toast.success(t("actions.cancelled")); void refresh() },
    onError,
  })
  const remove = useMutation({
    mutationKey: removeKey,
    mutationFn: transfersApi.remove,
    onSuccess: () => { toast.success(t("actions.removed")); void refresh() },
    onError,
  })
  const bulk = useMutation({
    mutationKey: bulkKey,
    mutationFn: ({ request }: { request: TransferBulkRequest; unfit: number }) => transfersApi.bulk(request),
    onSuccess: (result: TransferBulkResult, { request, unfit }) => {
      reportBulk(request.action, { ...result, skipped: result.skipped + unfit })
      clearSelection()
      void refresh()
    },
    onError,
  })
  const reportBulk = (action: TransferBulkAction, { done, skipped, failed, reasons }: TransferBulkResult) => {
    const why = reasons.map(reason => display.text(reason)).join(" ")
    const more = [
      skipped > 0 && t("bulk.skipped", { count: skipped }),
      failed > 0 && (why ? t("bulk.failedBecause", { count: failed, reasons: why }) : t("bulk.failed", { count: failed })),
    ].filter(Boolean).join(" ")
    if (done === 0 && failed === 0) toast.info(t("bulk.nothing"))
    else if (failed > 0) toast.error(t(`bulk.done.${action}`, { count: done }), { description: more })
    else toast.success(t(`bulk.done.${action}`, { count: done }), more ? { description: more } : undefined)
  }

  const retrying = usePendingIds(retryKey)
  const cancelRuns = usePendingIds(cancelKey)
  const removing = usePendingIds(removeKey)
  const bulkStep = (id: number): RowStep | null => {
    if (!bulk.isPending) return null
    const { request } = bulk.variables
    return !("ids" in request) || request.ids.includes(id) ? request.action : null
  }
  const runningStep = (id: number): RowStep | null =>
    retrying.has(id) ? "retry" : cancelRuns.has(id) ? "cancel" : removing.has(id) ? "remove" : bulkStep(id)
  const counts = list.data?.counts
  const rows = list.data?.rows ?? []
  const stale = list.isPlaceholderData
  const formatCount = useMemo(() => new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language), [i18n.resolvedLanguage, i18n.language])
  const countOf = (chip: TransferFilter) => counts && (chip === "all" ? counts.active + counts.waiting + counts.finished + counts.failed : counts[chip])
  const total = countOf(filter) ?? 0
  const pageCount = Math.max(1, Math.ceil(total / pageSize))
  if (counts && page >= pageCount) setPage(pageCount - 1)
  const clearSelection = () => {
    setSelection(noSelection)
    lastTicked.current = null
  }
  const turnTo = (next: number) => {
    setPage(next)
    clearSelection()
    top.current?.scrollIntoView({ block: "nearest" })
  }

  const picked = selection.kind === "filter" ? rows : rows.filter(row => selection.ids.has(row.id))
  const pickedCount = selection.kind === "filter" ? total : picked.length
  const pageTick = pickedCount === 0 ? false : picked.length === rows.length ? true : "indeterminate"
  const isPicked = (row: TransferRow) => selection.kind === "filter" || selection.ids.has(row.id)
  const tick = (index: number, range: boolean) => {
    setSelection(tickRow(selection, rows, index, range ? lastTicked.current : null))
    lastTicked.current = rows[index].id
  }
  const tickPage = () => {
    if (pageTick === true) clearSelection()
    else setSelection({ kind: "rows", ids: new Set(rows.map(row => row.id)) })
  }
  const runBulk = (action: TransferBulkAction) => {
    if (selection.kind === "filter" && action !== "retry") return bulk.mutateAsync({ request: { action, filter }, unfit: 0 })
    const ids = fittingIds(action, picked.filter(row => runningStep(row.id) === null))
    return bulk.mutateAsync({ request: { action, ids }, unfit: picked.length - ids.length })
  }
  const pickAction = (action: TransferBulkAction) => {
    if (action === "retry") runBulk(action).catch(() => {})
    else setAsking({ action, open: true, count: pickedCount })
  }

  return (
    <>
      <Section
        title={t("tabs.transfers")}
        description={t("list.description")}
        queries={[list]}
      >
        <div className="space-y-4">
          <div ref={top} className="overflow-x-auto">
            <Tabs value={filter} onValueChange={value => { setFilter(value as TransferFilter); setPage(0); clearSelection() }}>
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
            <div className="-mx-6 border-t max-sm:-mx-4">
              <div aria-busy={stale} className={cn("@container transition-opacity", stale && "opacity-60")}>
                <Table>
                  <TableHeader>
                    <TableRow className="hover:bg-transparent">
                      <TableHead className="w-px pr-0 pl-6 max-sm:pl-4">
                        <Checkbox checked={pageTick} disabled={stale} onCheckedChange={tickPage} aria-label={t("bulk.pickPage")} className={cn("align-middle", CHECKBOX_TAP)} />
                      </TableHead>
                      <TableHead className="w-full">{t("columns.torrent")}</TableHead>
                      <TableHead className="hidden text-right @xl:table-cell">{t("columns.size")}</TableHead>
                      <TableHead className="hidden @lg:table-cell @lg:min-w-52">{t("columns.state")}</TableHead>
                      <TableHead className="hidden @3xl:table-cell">{t("columns.started")}</TableHead>
                      <TableHead className="pr-6 max-sm:pr-4"><span className="sr-only">{t("columns.actions")}</span></TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {rows.map((row, index) => (
                      <TransferTableRow
                        key={row.id}
                        row={row}
                        picked={isPicked(row)}
                        stale={stale}
                        onTick={range => tick(index, range)}
                        onDetails={() => setDetailsId(row.id)}
                        running={runningStep(row.id)}
                        onRetry={() => retry.mutate(row.id)}
                        onCancel={() => setCancelling({ row, open: true })}
                        onRemove={() => remove.mutate(row.id)}
                      />
                    ))}
                  </TableBody>
                </Table>
              </div>
              {pickedCount > 0 && (
                <TransferSelectionBar
                  count={pickedCount}
                  wholeChip={selection.kind === "filter"}
                  more={selection.kind === "rows" && pageTick === true && total > rows.length ? total : null}
                  actions={offeredActions(selection, filter, picked)}
                  running={bulk.isPending ? bulk.variables.request.action : null}
                  onPickAll={() => setSelection({ kind: "filter" })}
                  onClear={clearSelection}
                  onAction={pickAction}
                />
              )}
            </div>
          )}

          {pageCount > 1 && (
            <nav aria-label={t("list.pages")} className="flex items-center justify-between gap-3">
              <p className="text-sm text-muted-foreground tabular-nums">
                {t("list.range", {
                  from: formatCount.format(page * pageSize + 1),
                  to: formatCount.format(Math.min((page + 1) * pageSize, total)),
                  total: formatCount.format(total),
                })}
              </p>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" className="max-sm:size-11" disabled={page === 0} onClick={() => turnTo(page - 1)}>
                  <ChevronLeft aria-hidden="true" />
                  <span className="max-sm:sr-only">{t("list.previous")}</span>
                </Button>
                <Button variant="outline" size="sm" className="max-sm:size-11" disabled={page >= pageCount - 1} onClick={() => turnTo(page + 1)}>
                  <span className="max-sm:sr-only">{t("list.next")}</span>
                  <ChevronRight aria-hidden="true" />
                </Button>
              </div>
            </nav>
          )}
        </div>
      </Section>

      <TransferDetailsDialog transferId={detailsId} onClose={() => setDetailsId(null)} />

      {cancelling && (
        <ConfirmDialog
          open={cancelling.open}
          onOpenChange={open => setCancelling(prev => prev && { ...prev, open })}
          title={t("cancelDialog.title")}
          description={t("cancelDialog.description", { name: display.name(cancelling.row) })}
          confirmLabel={t("cancelDialog.confirm")}
          cancelLabel={t("cancelDialog.keep")}
          onConfirm={() => cancel.mutateAsync(cancelling.row.id)}
        />
      )}

      {asking && (
        <ConfirmDialog
          open={asking.open}
          onOpenChange={open => setAsking(prev => prev && { ...prev, open })}
          title={t(`bulk.${asking.action}Dialog.title`)}
          description={t(`bulk.${asking.action}Dialog.description`, { count: asking.count })}
          confirmLabel={t(`bulk.${asking.action}Dialog.confirm`)}
          cancelLabel={t(`bulk.${asking.action}Dialog.keep`)}
          onConfirm={() => runBulk(asking.action)}
        />
      )}
    </>
  )
}

interface TransferTableRowProps {
  row: TransferRow
  picked: boolean
  stale: boolean
  onTick: (range: boolean) => void
  running: RowStep | null
  onDetails: () => void
  onRetry: () => void
  onCancel: () => void
  onRemove: () => void
}

function TransferTableRow({ row, picked, stale, onTick, running, onDetails, onRetry, onCancel, onRemove }: TransferTableRowProps) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const nameId = useId()
  const started = formatRelativeTime(row.createdAt)
  const step = (name: RowStep, label: string, icon: LucideIcon, onSelect: () => void, destructive = false): RowAction =>
    ({ label, icon, onSelect, destructive, pending: running === name, disabled: stale || running !== null })
  const actions: RowAction[] = [
    { label: t("actions.details"), icon: FileText, onSelect: onDetails, disabled: stale },
    ...(canRetry(row) ? [step("retry", t("actions.retry"), RotateCcw, onRetry)] : []),
    ...(canCancel(row) ? [step("cancel", t("actions.cancel"), X, onCancel)] : []),
    ...(canRemove(row) ? [step("remove", t("actions.remove"), Trash2, onRemove, true)] : []),
  ]

  return (
    <TableRow
      data-transfer-id={row.id}
      data-state={picked ? "selected" : undefined}
      className={cn(!stale && "cursor-pointer")}
      onMouseDown={stale ? undefined : event => {
        if (event.shiftKey) event.preventDefault()
      }}
      onClick={stale ? undefined : event => {
        if (window.getSelection()?.type === "Range") return
        onTick(event.shiftKey)
      }}
    >
      <TableCell className="pr-0 pl-6 align-top max-sm:pl-4">
        <Checkbox
          checked={picked}
          disabled={stale}
          onClick={event => {
            event.stopPropagation()
            onTick(event.shiftKey)
          }}
          aria-labelledby={nameId}
          className={cn("mt-0.5", CHECKBOX_TAP)}
        />
      </TableCell>
      <TableCell className="w-full max-w-0 align-top whitespace-normal">
        <div id={nameId} className="font-medium"><TorrentName name={display.name(row)} className="pointer-fine:line-clamp-2" /></div>
        <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 gap-y-1 text-xs text-muted-foreground">
          {row.size > 0 && (
            <span className="flex items-center gap-x-1.5 @xl:hidden">
              <span className="tabular-nums">{formatBytes(row.size)}</span>
              <span aria-hidden="true">·</span>
            </span>
          )}
          <span className="flex items-center gap-x-1.5">
            <span>{t("row.route", { source: instanceName(row.sourceInstanceId), target: instanceName(row.targetInstanceId) })}</span>
            {row.way !== "" && <span aria-hidden="true" className="@max-lg:hidden">·</span>}
          </span>
          {row.way !== "" && <span className="@max-lg:hidden">{t([`ways.${row.way}`, "ways.fallback"])}</span>}
          {row.torrents > 1 && <Mark>{t("row.crossSeeds", { count: row.torrents - 1 })}</Mark>}
          {row.kind === "export" && <Mark>{t("row.export")}</Mark>}
        </div>
        <div className="mt-1 flex flex-wrap items-center gap-x-1.5 text-xs text-muted-foreground @3xl:hidden">
          <span className="flex items-center gap-1.5 @lg:hidden">
            <StateLabel state={row.state} />
            <span aria-hidden="true">·</span>
          </span>
          <span className="flex items-center gap-1.5">
            <span>{started}</span>
            <span aria-hidden="true">·</span>
          </span>
          <span className="[overflow-wrap:anywhere]"><StartedBy row={row} /></span>
        </div>
        <div className="@lg:hidden">
          <StateDetail row={row} />
        </div>
        {row.failureReason && <p className="mt-1 text-xs text-destructive [overflow-wrap:anywhere]">{display.text(row.failureReason)}</p>}
        {row.way === "bittorrent" && row.hints.length > 0 && <div className="mt-1"><TransferHints hints={row.hints} /></div>}
      </TableCell>
      <TableCell className="hidden text-right align-top tabular-nums @xl:table-cell">
        {row.size > 0 ? formatBytes(row.size) : <span aria-hidden="true" className="text-muted-foreground">–</span>}
      </TableCell>
      <TableCell className="hidden align-top @lg:table-cell">
        <StateLabel state={row.state} className="text-sm" />
        <StateDetail row={row} />
      </TableCell>
      <TableCell className="hidden align-top @3xl:table-cell">
        <div>{started}</div>
        <div className="w-max max-w-60 text-xs whitespace-normal text-muted-foreground [overflow-wrap:anywhere]"><StartedBy row={row} /></div>
      </TableCell>
      <TableCell className="cursor-default pr-6 max-sm:pr-4" onClick={event => event.stopPropagation()}>
        <RowActions actions={actions} describedBy={nameId} />
      </TableCell>
    </TableRow>
  )
}

function Mark({ children }: { children: string }) {
  return <Badge variant="outline" className="py-0 text-10 font-normal text-muted-foreground">{children}</Badge>
}
