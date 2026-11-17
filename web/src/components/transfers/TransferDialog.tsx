/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { transfersApi, type TransferRequest, type TransferSummary } from "@/lib/transfers-api"
import { formatBytes } from "@/lib/utils"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { TransferForm } from "./TransferForm"
import { emptyTransferForm, transferRequestOf, type TransferFormValue } from "./transfer-form-value"
import { TransferHints } from "./TransferHints"
import { useInstanceName } from "./use-instance-name"

interface TransferDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  sourceInstanceId: number
  hash: string
  name: string
}

/** The Transfer dialog: the fields of the Transfer, and the summary of what Start does. */
export function TransferDialog({ open, onOpenChange, sourceInstanceId, hash, name }: TransferDialogProps) {
  const { t } = useTranslation("transfers")
  const queryClient = useQueryClient()
  const { instances } = useInstances()
  const targets = useMemo(() => (instances ?? []).filter(instance => instance.id !== sourceInstanceId), [instances, sourceInstanceId])
  const [value, setValue] = useState<TransferFormValue>(emptyTransferForm)
  const request = transferRequestOf(value, sourceInstanceId, hash)
  // The summary follows the form after the user stops typing in the save path.
  const debounced = useDebounce(request, 400)

  const summary = useQuery({
    queryKey: ["transfers", "summary", debounced],
    queryFn: () => transfersApi.summary(debounced!),
    enabled: open && debounced !== null,
    retry: false,
  })

  const start = useMutation({
    mutationFn: (req: TransferRequest) => transfersApi.start(req),
    onSuccess: () => {
      toast.success(t("dialog.started", { name }))
      void queryClient.invalidateQueries({ queryKey: ["transfers", "list"] })
      onOpenChange(false)
      setValue(emptyTransferForm)
    },
    onError: (error: Error) => toast.error(t("actions.failed", { message: error.message })),
  })

  // Start sends the request that the summary shows, once that summary is in.
  const settled = request !== null && JSON.stringify(request) === JSON.stringify(debounced)
  const canStart = settled && summary.isSuccess && !summary.isFetching && !start.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="break-all">{t("dialog.title", { name })}</DialogTitle>
          <DialogDescription>{t("dialog.description")}</DialogDescription>
        </DialogHeader>

        <TransferForm value={value} onChange={setValue} targets={targets} />

        <section className="space-y-2 rounded-md border p-3" aria-labelledby="transfer-summary-title">
          <h3 id="transfer-summary-title" className="text-sm font-medium">{t("summary.title")}</h3>
          {request === null ? (
            <p className="text-sm text-muted-foreground">{t("summary.pickTarget")}</p>
          ) : summary.isError ? (
            <p role="alert" className="text-sm text-destructive">{t("summary.refused", { message: summary.error.message })}</p>
          ) : summary.data && settled && !summary.isFetching ? (
            <SummaryView summary={summary.data} targetInstanceId={request.targetInstanceId} />
          ) : (
            <p className="text-sm text-muted-foreground">{t("summary.loading")}</p>
          )}
        </section>

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>{t("dialog.cancel")}</Button>
          <Button disabled={!canStart} onClick={() => request && start.mutate(request)}>
            {start.isPending ? t("dialog.starting") : t("dialog.start")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** What Start would start: the torrents, the way, the free space, and the warnings. */
export function SummaryView({ summary, targetInstanceId }: { summary: TransferSummary; targetInstanceId: number }) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const notEnoughSpace = summary.freeKnown && summary.free < summary.bytes
  // A cross-seed of another target goes to its Cross-seed instance.
  const elsewhere = summary.members.filter(member => member.targetInstanceId !== targetInstanceId)

  return (
    <div className="space-y-2 text-sm">
      <p>{t("summary.torrents", { count: summary.members.length, size: formatBytes(summary.bytes) })}</p>
      <ul className="list-disc space-y-0.5 pl-5 text-xs text-muted-foreground">
        {summary.members.map(member => (
          <li key={member.hash} className="break-all">
            {member.name}
            {elsewhere.includes(member) && <> ({t("summary.crossSeedInstance", { instance: instanceName(member.targetInstanceId) })})</>}
          </li>
        ))}
      </ul>
      <p>{t("summary.way", { way: t([`ways.${summary.way}`, "ways.fallback"]) })}</p>
      {summary.freeKnown ? (
        <p>{t("summary.free", { size: formatBytes(summary.free) })}</p>
      ) : (
        <p className="text-muted-foreground">{t("summary.freeUnknown")}</p>
      )}
      {notEnoughSpace && <Warning>{t("summary.notEnoughSpace")}</Warning>}
      {summary.noFileAccess && <Warning>{t("summary.noFileAccess", { instance: instanceName(targetInstanceId) })}</Warning>}
      <TransferHints hints={summary.hints} />
    </div>
  )
}

function Warning({ children }: { children: string }) {
  return (
    <p role="alert" className="flex gap-1.5 text-amber-600 dark:text-amber-400">
      <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
      <span>{children}</span>
    </p>
  )
}
