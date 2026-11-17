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
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { TorrentName } from "@/components/ui/torrent-name"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { transferListQuery, transferPageSize, transfersApi, type TransferFilter, type TransferRow } from "@/lib/transfers-api"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { ChevronLeft, ChevronRight, FileText, RotateCcw, Trash2, X, type LucideIcon } from "lucide-react"
import { useId, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { RowActions, type RowAction } from "./RowActions"
import { canCancel, canRemove, canRetry } from "./transfer-display"
import { TransferDetailsDialog } from "./TransferDetailsDialog"
import { TransferHints } from "./TransferHints"
import { StartedBy, StateDetail, StateLabel } from "./TransferState"
import { useIncognitoDisplay } from "./use-incognito-display"
import { useInstanceName } from "./use-instance-name"

const chips: readonly TransferFilter[] = ["all", "active", "waiting", "finished", "failed"]
const retryKey = ["transfers", "retry"]
const cancelKey = ["transfers", "cancel"]
const removeKey = ["transfers", "remove"]

/** A step of a row that waits for its answer. */
type RowStep = "retry" | "cancel" | "remove"

/** The Transfers section of the Transfers page: the Transfers and the Export runs, newest first. */
export function TransfersTab() {
  const { t, i18n } = useTranslation("transfers")
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<TransferFilter>("all")
  // Page 0 is the first page.
  const [page, setPage] = useState(0)
  const top = useRef<HTMLDivElement>(null)
  const [detailsId, setDetailsId] = useState<number | null>(null)
  // The row stays while the dialog fades out.
  const [cancelling, setCancelling] = useState<{ row: TransferRow; open: boolean } | null>(null)

  const list = useQuery({
    ...transferListQuery(filter, page),
    // The rows and the counts stay while the rows of another filter or page load.
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

  const retrying = usePendingIds(retryKey)
  const cancelRuns = usePendingIds(cancelKey)
  const removing = usePendingIds(removeKey)
  const runningStep = (id: number): RowStep | null =>
    retrying.has(id) ? "retry" : cancelRuns.has(id) ? "cancel" : removing.has(id) ? "remove" : null
  const counts = list.data?.counts
  const rows = list.data?.rows ?? []
  const formatCount = useMemo(() => new Intl.NumberFormat(i18n.resolvedLanguage ?? i18n.language), [i18n.resolvedLanguage, i18n.language])
  const countOf = (chip: TransferFilter) => counts && (chip === "all" ? counts.active + counts.waiting + counts.finished + counts.failed : counts[chip])
  const total = countOf(filter) ?? 0
  const pageCount = Math.max(1, Math.ceil(total / transferPageSize))
  // When rows end or go away, the page can lie past the last page. The list then shows the last page.
  if (counts && page >= pageCount) setPage(pageCount - 1)
  const turnTo = (next: number) => {
    setPage(next)
    // The buttons sit below the rows, so the top of the list comes into view.
    top.current?.scrollIntoView({ block: "nearest" })
  }

  return (
    <>
      <Section
        title={t("tabs.transfers")}
        description={t("list.description")}
        queries={[list]}
      >
        <div className="space-y-4">
          {/* A phone scrolls the filters sideways when their labels do not fit. */}
          <div ref={top} className="overflow-x-auto">
            <Tabs value={filter} onValueChange={value => { setFilter(value as TransferFilter); setPage(0) }}>
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
                      running={runningStep(row.id)}
                      onRetry={() => retry.mutate(row.id)}
                      onCancel={() => setCancelling({ row, open: true })}
                      onRemove={() => remove.mutate(row.id)}
                    />
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {pageCount > 1 && (
            <nav aria-label={t("list.pages")} className="flex items-center justify-between gap-3">
              <p className="text-sm text-muted-foreground tabular-nums">
                {t("list.range", {
                  from: formatCount.format(page * transferPageSize + 1),
                  to: formatCount.format(Math.min((page + 1) * transferPageSize, total)),
                  total: formatCount.format(total),
                })}
              </p>
              {/* A phone shows the arrows only, at the size of a finger. */}
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
    </>
  )
}

interface TransferTableRowProps {
  row: TransferRow
  /** The step of the row that waits for its answer: the row takes no other step until then. */
  running: RowStep | null
  onDetails: () => void
  onRetry: () => void
  onCancel: () => void
  onRemove: () => void
}

function TransferTableRow({ row, running, onDetails, onRetry, onCancel, onRemove }: TransferTableRowProps) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const nameId = useId()
  const started = formatRelativeTime(row.createdAt)
  const step = (name: RowStep, label: string, icon: LucideIcon, onSelect: () => void, destructive = false): RowAction =>
    ({ label, icon, onSelect, destructive, pending: running === name, disabled: running !== null })
  const actions: RowAction[] = [
    { label: t("actions.details"), icon: FileText, onSelect: onDetails },
    ...(canRetry(row) ? [step("retry", t("actions.retry"), RotateCcw, onRetry)] : []),
    ...(canCancel(row) ? [step("cancel", t("actions.cancel"), X, onCancel)] : []),
    ...(canRemove(row) ? [step("remove", t("actions.remove"), Trash2, onRemove, true)] : []),
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
          <span className="[overflow-wrap:anywhere]"><StartedBy row={row} /></span>
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
        <div className="w-max max-w-60 text-xs whitespace-normal text-muted-foreground [overflow-wrap:anywhere]"><StartedBy row={row} /></div>
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
