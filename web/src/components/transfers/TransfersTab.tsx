/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { usePendingIds } from "@/components/kit/use-pending-ids"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Progress } from "@/components/ui/progress"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatNarrowAge, formatRelativeTime } from "@/lib/dateTimeUtils"
import { formatSpeedWithUnit, useSpeedUnits } from "@/lib/speedUnits"
import { transfersApi, type TransferFilter, type TransferRow } from "@/lib/transfers-api"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { canCancel, canRemove, canRetry, chipOf, percentOf, timeLeftMs, waitsForRetry } from "./transfer-display"
import { TransferDetailsSheet } from "./TransferDetailsSheet"
import { TransferHints } from "./TransferHints"
import { useInstanceName } from "./use-instance-name"

const chips: readonly TransferFilter[] = ["all", "active", "waiting", "finished", "failed"]
const retryKey = ["transfers", "retry"]
/** The list of the backend holds at most this many rows. */
const listLimit = 500

const stateBadge: Record<ReturnType<typeof chipOf>, "default" | "secondary" | "destructive" | "outline"> = {
  active: "default",
  waiting: "secondary",
  finished: "outline",
  failed: "destructive",
}

/** The Transfers tab of the Transfers page: the Transfers and the Export runs, newest first. */
export function TransfersTab() {
  const { t } = useTranslation("transfers")
  const queryClient = useQueryClient()
  const [filter, setFilter] = useState<TransferFilter>("all")
  const [detailsId, setDetailsId] = useState<number | null>(null)
  const [cancelRow, setCancelRow] = useState<TransferRow | null>(null)

  const list = useQuery({
    queryKey: ["transfers", "list", filter],
    queryFn: () => transfersApi.list(filter),
    // The rows and the counts stay while the rows of another chip load.
    placeholderData: keepPreviousData,
    // Progress and speed move while a Transfer runs.
    refetchInterval: 3000,
  })

  const refresh = () => queryClient.invalidateQueries({ queryKey: ["transfers"] })
  const onError = (error: Error) => toast.error(t("actions.failed", { message: error.message }))
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

  return (
    <div className="space-y-4">
      <div role="group" aria-label={t("chips.label")} className="flex flex-wrap gap-2">
        {chips.map(chip => (
          <Button
            key={chip}
            size="sm"
            variant={filter === chip ? "default" : "outline"}
            aria-pressed={filter === chip}
            onClick={() => setFilter(chip)}
          >
            {chip === "all" ? t("chips.all") : t(`chips.${chip}`, { count: counts?.[chip] ?? 0 })}
          </Button>
        ))}
      </div>

      {list.isError ? (
        <p role="alert" className="text-sm text-destructive">{t("list.error", { message: list.error.message })}</p>
      ) : list.isPending ? (
        <p className="text-sm text-muted-foreground">{t("list.loading")}</p>
      ) : rows.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t("list.empty")}</p>
      ) : (
        <div className="overflow-x-auto rounded-md border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("columns.torrent")}</TableHead>
                <TableHead>{t("columns.route")}</TableHead>
                <TableHead>{t("columns.way")}</TableHead>
                <TableHead>{t("columns.progress")}</TableHead>
                <TableHead>{t("columns.speed")}</TableHead>
                <TableHead>{t("columns.timeLeft")}</TableHead>
                <TableHead>{t("columns.state")}</TableHead>
                <TableHead>{t("columns.started")}</TableHead>
                <TableHead pinEnd className="text-right">{t("columns.actions")}</TableHead>
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
                  onCancel={() => setCancelRow(row)}
                  onRemove={() => remove.mutate(row.id)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
      {rows.length >= listLimit && <p className="text-xs text-muted-foreground">{t("list.limit")}</p>}

      <TransferDetailsSheet transferId={detailsId} onClose={() => setDetailsId(null)} />

      <AlertDialog open={cancelRow !== null} onOpenChange={open => !open && setCancelRow(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("cancelDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription className="break-all">{t("cancelDialog.description", { name: cancelRow?.name ?? "" })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("cancelDialog.keep")}</AlertDialogCancel>
            <AlertDialogAction onClick={() => cancelRow && cancel.mutate(cancelRow.id)}>{t("cancelDialog.confirm")}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
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
  const [speedUnit] = useSpeedUnits()
  const chip = chipOf(row.state)
  const left = row.progress ? timeLeftMs(row.progress) : null

  return (
    <TableRow data-transfer-id={row.id}>
      <TableCell className="max-w-80 whitespace-normal">
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="break-all font-medium">{row.name}</span>
          {row.torrents > 1 && <Badge variant="secondary">{t("row.crossSeeds", { count: row.torrents - 1 })}</Badge>}
          {row.kind === "export" && <Badge variant="outline">{t("row.export")}</Badge>}
        </div>
        {row.failureReason && <p className="mt-1 text-xs text-destructive">{row.failureReason}</p>}
        {waitsForRetry(row) && (
          <p className="mt-1 text-xs text-muted-foreground">{t("row.retryAt", { time: formatRelativeTime(row.retryAt) })}</p>
        )}
        {row.way === "bittorrent" && <div className="mt-1"><TransferHints hints={row.hints} /></div>}
      </TableCell>
      <TableCell className="whitespace-nowrap">
        {t("row.route", { source: instanceName(row.sourceInstanceId), target: instanceName(row.targetInstanceId) })}
      </TableCell>
      <TableCell className="whitespace-nowrap">{t([`ways.${row.way}`, "ways.fallback"])}</TableCell>
      <TableCell className="min-w-28">
        {row.progress ? (
          <div className="space-y-1">
            <Progress value={percentOf(row.progress)} aria-label={t("columns.progress")} />
            <span className="text-xs tabular-nums text-muted-foreground">{percentOf(row.progress)}%</span>
          </div>
        ) : (
          <span className="text-muted-foreground">{t("row.noSpeed")}</span>
        )}
      </TableCell>
      <TableCell className="whitespace-nowrap tabular-nums">
        {row.progress && row.progress.speed > 0 ? formatSpeedWithUnit(row.progress.speed, speedUnit) : t("row.noSpeed")}
      </TableCell>
      <TableCell className="whitespace-nowrap tabular-nums">{left === null ? t("row.noSpeed") : formatNarrowAge(left)}</TableCell>
      <TableCell>
        <Badge variant={stateBadge[chip]}>{t([`states.${row.state}`, "states.fallback"])}</Badge>
      </TableCell>
      <TableCell className="whitespace-nowrap text-xs">
        <div>
          {row.startedBy === "user"
            ? t("row.byUser")
            : row.ruleName
              ? t("row.byRule", { name: row.ruleName })
              : t("row.byDeletedRule")}
        </div>
        <div className="text-muted-foreground">{formatRelativeTime(row.createdAt)}</div>
      </TableCell>
      <TableCell pinEnd className="text-right">
        <div className="flex justify-end gap-1">
          <Button size="sm" variant="ghost" onClick={onDetails}>{t("actions.details")}</Button>
          {canRetry(row) && <Button size="sm" variant="ghost" disabled={retrying} onClick={onRetry}>{t("actions.retry")}</Button>}
          {canCancel(row) && <Button size="sm" variant="ghost" onClick={onCancel}>{t("actions.cancel")}</Button>}
          {canRemove(row) && <Button size="sm" variant="ghost" onClick={onRemove}>{t("actions.remove")}</Button>}
        </div>
      </TableCell>
    </TableRow>
  )
}
