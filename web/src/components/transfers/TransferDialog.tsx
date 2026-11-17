/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { transfersApi, type TransferRequest, type TransferSummary } from "@/lib/transfers-api"
import { cn, formatBytes } from "@/lib/utils"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, Info, Loader2, ShieldCheck } from "lucide-react"
import { useMemo, useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { TransferForm } from "./TransferForm"
import { emptyTransferForm, transferRequestOf, type TransferFormValue } from "./transfer-form-value"
import { TransferHints } from "./TransferHints"
import { useIncognitoDisplay } from "./use-incognito-display"
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
  const display = useIncognitoDisplay()
  const instanceName = useInstanceName()
  const shownName = display.name({ hash, name })
  const queryClient = useQueryClient()
  const { instances } = useInstances()
  const targets = useMemo(() => instances?.filter(instance => instance.id !== sourceInstanceId), [instances, sourceInstanceId])
  const [value, setValue] = useState<TransferFormValue>(emptyTransferForm)
  const request = useMemo(() => transferRequestOf(value, sourceInstanceId, hash), [value, sourceInstanceId, hash])
  // The summary follows the form after the user stops typing in the save path.
  const debounced = useDebounce(request, 400)

  const summary = useQuery({
    queryKey: ["transfers", "summary", debounced],
    queryFn: () => transfersApi.summary(debounced!),
    enabled: open && debounced !== null,
    retry: false,
    // The last summary stays in place while the next one loads, so the panel keeps its layout.
    placeholderData: keepPreviousData,
  })

  const start = useMutation({
    mutationFn: (req: TransferRequest) => transfersApi.start(req),
    onSuccess: () => {
      toast.success(t("dialog.started", { name: shownName }))
      void queryClient.invalidateQueries({ queryKey: ["transfers", "list"] })
      onOpenChange(false)
      setValue(emptyTransferForm)
    },
    onError: (error: Error) => toast.error(t("actions.failed", { message: display.text(error.message) })),
  })

  // Start sends the request that the summary shows, once that summary is in. The summary of an older form stays in
  // place while the next one loads, also while the browser is offline and the query waits.
  const settled = request !== null && JSON.stringify(request) === JSON.stringify(debounced)
  const current = settled && summary.isSuccess && !summary.isFetching && !summary.isPlaceholderData
  const canStart = current && !start.isPending

  let state: SummaryState
  if (request === null) state = { kind: "pickTarget" }
  else if (summary.isError) state = { kind: "refused", message: display.text(summary.error.message) }
  else if (summary.data) state = { kind: "summary", summary: summary.data, current, targetInstanceId: request.targetInstanceId }
  else state = { kind: "loading" }

  // A warning that the Transfer can lose data, or cannot fit, sits next to Start, so the user reads it before the start.
  const warnings: string[] = []
  if (state.kind === "summary") {
    const { free, freeKnown, bytes, noFileAccess } = state.summary
    if (freeKnown && free < bytes) warnings.push(t("summary.notEnoughSpace"))
    if (noFileAccess) warnings.push(t("summary.noFileAccess", { instance: instanceName(state.targetInstanceId) }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* The frame of the Add torrent dialog: the top stays in place, so a phone keyboard does not push the title off. */}
      <DialogContent className="flex max-h-[90dvh] w-full max-w-[95vw] flex-col gap-0 p-0 !top-[5dvh] !translate-y-0 sm:!top-[7.5dvh] sm:max-h-[85dvh] sm:max-w-2xl lg:max-w-5xl">
        <DialogHeader className="flex-shrink-0 border-b px-5 pt-5 pb-3 pr-12 text-left sm:px-6">
          <DialogTitle>{t("dialog.title")}</DialogTitle>
          <DialogDescription className="line-clamp-2 break-all" title={shownName}>{shownName}</DialogDescription>
        </DialogHeader>

        <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6">
          <div className="grid grid-cols-[minmax(0,1fr)] gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
            <TransferForm value={value} onChange={setValue} targets={targets} />
            {/* On a wide screen the panel fills the height of the form, so neither moves the other. */}
            <div className="relative lg:min-h-80">
              <SummaryPanel state={state} hash={hash} className="min-h-80 lg:absolute lg:inset-0 lg:min-h-0" />
            </div>
          </div>
        </div>

        <div className="flex flex-shrink-0 flex-col gap-3 border-t bg-background px-5 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:px-6">
          <div className={cn("min-w-0 flex-1 space-y-1 transition-opacity", state.kind === "summary" && !state.current && "opacity-50")}>
            {warnings.length > 0 ? warnings.map(warning => <Note key={warning} tone="warning">{warning}</Note>) : (
              <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                <ShieldCheck className="mt-px size-3.5 shrink-0" aria-hidden />
                <span>{t("dialog.verified")}</span>
              </p>
            )}
          </div>
          <div className="flex gap-2 sm:justify-end">
            <Button variant="outline" className="h-11 px-5 sm:h-10" onClick={() => onOpenChange(false)}>{t("dialog.cancel")}</Button>
            <Button className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none" disabled={!canStart} onClick={() => request && start.mutate(request)}>
              {start.isPending && <Loader2 className="animate-spin" aria-hidden />}
              {start.isPending ? t("dialog.starting") : t("dialog.start")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

type SummaryState =
  | { kind: "pickTarget" }
  | { kind: "loading" }
  | { kind: "refused"; message: string }
  /** `current` is false while the summary of an older form shows and the next one loads. */
  | { kind: "summary"; summary: TransferSummary; current: boolean; targetInstanceId: number }

/**
 * What Start would start: the torrents, the size, the way, the free space, and the notes. Every state keeps the same
 * figures in the same place, so the panel keeps its height while a summary loads. The warnings sit next to Start.
 */
function SummaryPanel({ state, hash, className }: { state: SummaryState; hash: string; className?: string }) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  const shown = state.kind === "summary" ? state.summary : null
  const loading = state.kind === "loading" || (state.kind === "summary" && !state.current)
  const dim = state.kind === "summary" && !state.current && "opacity-50"
  const notEnoughSpace = shown !== null && shown.freeKnown && shown.free < shown.bytes
  // A cross-seed of another target goes to its Cross-seed instance.
  const targetInstanceId = state.kind === "summary" ? state.targetInstanceId : null

  const figure = (text: string | undefined) => {
    if (state.kind === "loading") return <span className="block h-5 w-16 animate-pulse rounded bg-muted" />
    return text ?? <span className="text-muted-foreground">—</span>
  }

  return (
    <section
      aria-labelledby="transfer-summary-title"
      aria-busy={loading}
      className={cn("flex flex-col overflow-hidden rounded-lg border bg-muted/20", className)}
    >
      <div className="flex items-center justify-between gap-2 px-4 pt-3">
        <h3 id="transfer-summary-title" className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("summary.title")}</h3>
        {loading && (
          <span role="status" className="flex items-center text-muted-foreground">
            <Loader2 className="size-3.5 animate-spin" aria-hidden />
            <span className="sr-only">{t("summary.loading")}</span>
          </span>
        )}
      </div>

      <dl className={cn("grid grid-cols-2 gap-x-4 gap-y-3 px-4 pt-3 pb-4 transition-opacity", dim)}>
        <Figure label={t("summary.torrents")}>{figure(shown ? String(shown.members.length) : undefined)}</Figure>
        <Figure label={t("summary.size")}>{figure(shown ? formatBytes(shown.bytes) : undefined)}</Figure>
        <Figure label={t("summary.way")}>{figure(shown ? t([`ways.${shown.way}`, "ways.fallback"]) : undefined)}</Figure>
        <Figure label={t("summary.free")} tone={notEnoughSpace ? "warning" : undefined}>
          {figure(shown ? (shown.freeKnown ? formatBytes(shown.free) : t("summary.unknown")) : undefined)}
        </Figure>
      </dl>

      {/* The notes stay in view above the torrents, which scroll on a wide screen. */}
      {shown && (!shown.freeKnown || shown.hints.length > 0) && (
        <div className={cn("space-y-1.5 border-t px-4 py-3 transition-opacity", dim)}>
          {!shown.freeKnown && <Note>{t("summary.freeUnknown")}</Note>}
          <TransferHints hints={shown.hints} />
        </div>
      )}

      <div className={cn("min-h-0 flex-1 overflow-y-auto border-t px-4 py-3 text-sm transition-opacity", dim)}>
        {state.kind === "pickTarget" && <p className="text-muted-foreground">{t("summary.pickTarget")}</p>}
        {state.kind === "loading" && (
          <div className="space-y-2" aria-hidden>
            <span className="block h-4 w-4/5 animate-pulse rounded bg-muted" />
            <span className="block h-4 w-3/5 animate-pulse rounded bg-muted" />
          </div>
        )}
        {state.kind === "refused" && <p role="alert" className="text-destructive">{t("summary.refused", { message: state.message })}</p>}
        {shown && (
          <ul className="space-y-1.5">
            {shown.members.map(member => (
              <li key={member.hash} className="flex items-start gap-2">
                <span className="min-w-0 flex-1 break-all text-xs leading-5">{display.name(member)}</span>
                {member.targetInstanceId !== targetInstanceId ? (
                  <Badge variant="outline" className="shrink-0 font-normal">{t("summary.crossSeedInstance", { instance: instanceName(member.targetInstanceId) })}</Badge>
                ) : member.hash !== hash && (
                  <Badge variant="secondary" className="shrink-0 font-normal">{t("summary.crossSeed")}</Badge>
                )}
              </li>
            ))}
          </ul>
        )}
      </div>
    </section>
  )
}

function Figure({ label, tone, children }: { label: string; tone?: "warning"; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className={cn("mt-0.5 truncate text-sm font-medium leading-5 tabular-nums", tone === "warning" && "text-amber-600 dark:text-amber-400")}>{children}</dd>
    </div>
  )
}

function Note({ tone, children }: { tone?: "warning"; children: string }) {
  if (tone === "warning") {
    return (
      <p role="alert" className="flex gap-1.5 text-xs text-amber-600 dark:text-amber-400">
        <AlertTriangle className="mt-px size-3.5 shrink-0" aria-hidden />
        <span>{children}</span>
      </p>
    )
  }
  return (
    <p className="flex gap-1.5 text-xs text-muted-foreground">
      <Info className="mt-px size-3.5 shrink-0" aria-hidden />
      <span>{children}</span>
    </p>
  )
}
