/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { BetaBadge } from "@/components/transfers/BetaBadge"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { TruncatedText } from "@/components/ui/truncated-text"
import { useDebounce } from "@/hooks/useDebounce"
import { useInstances } from "@/hooks/useInstances"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { transfersApi, type TransferRequest, type TransferSummary } from "@/lib/transfers-api"
import { cn, formatBytes } from "@/lib/utils"
import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { AlertTriangle, ChevronDown, Info, Loader2, ShieldCheck } from "lucide-react"
import { useEffect, useId, useMemo, useRef, useState, type ReactNode, type RefObject } from "react"
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
  onStarted: () => void
  sourceInstanceId: number
  hash: string
  name: string
}

const STACKED_QUERY = "(max-width: 1023px)"

const PHONE_QUERY = "(max-width: 639px)"

const fadeBelow = "[mask-image:linear-gradient(to_bottom,black_calc(100%-2.5rem),transparent)]"

export function TransferDialog({ open, onOpenChange, onStarted, sourceInstanceId, hash, name }: TransferDialogProps) {
  const { t } = useTranslation("transfers")
  const display = useIncognitoDisplay()
  const instanceName = useInstanceName()
  const shownName = display.name({ hash, name })
  const queryClient = useQueryClient()
  const stacked = useMediaQuery(STACKED_QUERY)
  const { instances } = useInstances()
  const targets = useMemo(() => instances?.filter(instance => instance.id !== sourceInstanceId), [instances, sourceInstanceId])
  const [value, setValue] = useState<TransferFormValue>(emptyTransferForm)
  const request = useMemo(() => transferRequestOf(value, sourceInstanceId, hash), [value, sourceInstanceId, hash])
  const debounced = useDebounce(request, 400)

  const summary = useQuery({
    queryKey: ["transfers", "summary", debounced],
    queryFn: async () => ({ request: debounced!, summary: await transfersApi.summary(debounced!) }),
    enabled: open && debounced !== null,
    retry: false,
    placeholderData: keepPreviousData,
  })

  const start = useMutation({
    mutationFn: (req: TransferRequest) => transfersApi.start(req),
    onSuccess: () => {
      toast.success(t("dialog.started", { name: shownName }))
      void queryClient.invalidateQueries({ queryKey: ["transfers", "list"] })
    },
    onError: (error: Error) => toast.error(t("actions.failed", { message: display.text(error.message) })),
  })

  const closeAfterStart = () => {
    onStarted()
    onOpenChange(false)
    setValue(emptyTransferForm)
  }

  const settled = request !== null && JSON.stringify(request) === JSON.stringify(debounced)
  const current = settled && summary.isSuccess && !summary.isFetching && !summary.isPlaceholderData
  const canStart = current && !start.isPending

  let state: SummaryState
  if (request === null) state = { kind: "pickTarget" }
  else if (summary.isError) state = { kind: "refused", message: display.text(summary.error.message) }
  else if (summary.data) state = { kind: "summary", summary: summary.data.summary, current, targetInstanceId: summary.data.request.targetInstanceId }
  else state = { kind: "loading" }

  const warnings: string[] = []
  if (state.kind === "summary") {
    const { free, freeKnown, bytes, noFileAccess, stayOnSource } = state.summary
    const names = { source: instanceName(sourceInstanceId), target: instanceName(state.targetInstanceId) }
    if (stayOnSource > 0) warnings.push(t(noFileAccess ? "summary.crossSeedsStay" : "summary.crossSeedsStayForced", { count: stayOnSource, ...names }))
    if (freeKnown && free < bytes) warnings.push(t("summary.notEnoughSpace"))
    if (noFileAccess) warnings.push(t("summary.noFileAccess", { instance: names.target }))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[90dvh] w-full max-w-[95vw] flex-col gap-0 p-0 !top-[5dvh] !translate-y-0 sm:!top-[7.5dvh] sm:max-h-[85dvh] sm:max-w-2xl lg:max-w-5xl">
        <DialogHeader className="flex-shrink-0 border-b px-5 pt-5 pb-3 pr-12 text-left sm:px-6">
          <DialogTitle className="flex items-center gap-2">{t("dialog.title")}<BetaBadge /></DialogTitle>
          <DialogDescription className="[overflow-wrap:anywhere]"><TruncatedText wrap className="line-clamp-2">{shownName}</TruncatedText></DialogDescription>
        </DialogHeader>

        <ScrollBody>
          <div className="grid grid-cols-[minmax(0,1fr)] gap-6 lg:grid-cols-[minmax(0,1fr)_22rem]">
            <TransferForm value={value} onChange={setValue} targets={targets} />
            {stacked ? (
              <SummaryTorrents state={state} hash={hash} />
            ) : (
              <div className="relative min-h-[28rem]">
                <SummaryPanel state={state} hash={hash} className="absolute inset-0" />
              </div>
            )}
          </div>
        </ScrollBody>

        <div className="flex flex-shrink-0 flex-col gap-3 border-t bg-background px-5 pt-3 pb-[max(0.75rem,env(safe-area-inset-bottom))] sm:flex-row sm:items-center sm:px-6">
          <div className="min-w-0 flex-1 space-y-1.5">
            {stacked && <SummaryLine state={state} />}
            <div className={cn("space-y-1 transition-opacity", state.kind === "summary" && !state.current && "opacity-50")}>
              {warnings.length > 0 ? <Warnings warnings={warnings} /> : (
                <p className="flex items-start gap-1.5 text-xs text-muted-foreground">
                  <ShieldCheck className="mt-px size-3.5 shrink-0" aria-hidden />
                  <span>{t("dialog.verified")}</span>
                </p>
              )}
            </div>
          </div>
          <div className="flex gap-2 sm:justify-end">
            <Button variant="outline" className="h-11 px-5 sm:h-10" onClick={() => onOpenChange(false)}>{t("dialog.cancel")}</Button>
            <Button className="h-11 flex-1 sm:h-10 sm:min-w-36 sm:flex-none" disabled={!canStart} onClick={() => request && start.mutate(request, { onSuccess: closeAfterStart })}>
              {start.isPending && <Loader2 className="animate-spin" aria-hidden />}
              {start.isPending ? t("dialog.starting") : t("dialog.start")}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}

function ScrollBody({ children }: { children: ReactNode }) {
  const body = useRef<HTMLDivElement>(null)
  const more = useMoreBelow(body, null)
  return <div ref={body} className={cn("min-h-0 flex-1 overflow-y-auto px-5 py-5 sm:px-6", more && fadeBelow)}>{children}</div>
}

type SummaryState =
  | { kind: "pickTarget" }
  | { kind: "loading" }
  | { kind: "refused"; message: string }
  | { kind: "summary"; summary: TransferSummary; current: boolean; targetInstanceId: number }

interface FigureItem {
  label: string
  text?: string
  tone?: "warning"
}

function useFigures(state: SummaryState): FigureItem[] {
  const { t } = useTranslation("transfers")
  const shown = state.kind === "summary" ? state.summary : null
  const notEnoughSpace = shown !== null && shown.freeKnown && shown.free < shown.bytes
  return [
    { label: t("summary.torrents"), text: shown ? String(shown.members.length) : undefined },
    { label: t("summary.way"), text: shown ? t([`ways.${shown.way}`, "ways.fallback"]) : undefined },
    { label: t("summary.size"), text: shown ? formatBytes(shown.bytes) : undefined },
    {
      label: t("summary.free"),
      text: shown ? (shown.freeKnown ? formatBytes(shown.free) : t("summary.unknown")) : undefined,
      tone: notEnoughSpace ? "warning" : undefined,
    },
  ]
}

const loadingOf = (state: SummaryState) => state.kind === "loading" || (state.kind === "summary" && !state.current)
const dimOf = (state: SummaryState) => state.kind === "summary" && !state.current && "opacity-50"
const warningTone = "text-amber-600 dark:text-amber-400"

function SummaryPanel({ state, hash, className }: { state: SummaryState; hash: string; className?: string }) {
  const { t } = useTranslation("transfers")
  const figures = useFigures(state)
  const shown = state.kind === "summary" ? state.summary : null
  const loading = loadingOf(state)
  const dim = dimOf(state)
  const list = useRef<HTMLDivElement>(null)
  const more = useMoreBelow(list, shown)

  return (
    <section
      aria-labelledby="transfer-summary-title"
      aria-busy={loading}
      className={cn("flex flex-col overflow-hidden rounded-lg border bg-muted/20", className)}
    >
      <div className="flex items-center justify-between gap-2 px-4 pt-3">
        <h3 id="transfer-summary-title" className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{t("summary.title")}</h3>
        {loading && <Planning />}
      </div>

      <dl className={cn("grid grid-cols-2 gap-x-4 gap-y-3 px-4 pt-3 pb-4 transition-opacity", dim)}>
        {figures.map(({ label, text, tone }) => (
          <div key={label} className="min-w-0">
            <dt className="text-xs text-muted-foreground">{label}</dt>
            <dd className={cn("mt-0.5 truncate text-sm font-medium leading-5 tabular-nums", tone === "warning" && warningTone)}>
              <FigureValue state={state} text={text} skeleton="block h-5 w-16" />
            </dd>
          </div>
        ))}
      </dl>

      <SummaryNotes state={state} className="border-t px-4 py-3" />

      <div
        ref={list}
        className={cn(
          "min-h-0 flex-1 overflow-y-auto border-t px-4 py-3 text-sm transition-opacity",
          dim,
          more && fadeBelow
        )}
      >
        {state.kind === "pickTarget" && <p className="text-muted-foreground">{t("summary.pickTarget")}</p>}
        {state.kind === "loading" && (
          <div className="space-y-2" aria-hidden>
            <span className="block h-4 w-4/5 animate-pulse rounded bg-muted" />
            <span className="block h-4 w-3/5 animate-pulse rounded bg-muted" />
          </div>
        )}
        {state.kind === "refused" && <p role="alert" className="text-destructive">{t("summary.refused", { message: state.message })}</p>}
        {shown && state.kind === "summary" && <MemberList summary={shown} hash={hash} targetInstanceId={state.targetInstanceId} />}
      </div>
    </section>
  )
}

function SummaryLine({ state }: { state: SummaryState }) {
  const { t } = useTranslation("transfers")
  const [, way, size, free] = useFigures(state)
  const loading = loadingOf(state)

  if (state.kind === "pickTarget") return <p className="min-h-[2.125rem] text-xs text-muted-foreground">{t("summary.pickTarget")}</p>
  if (state.kind === "refused") {
    return <p role="alert" className="min-h-[2.125rem] text-xs text-destructive [overflow-wrap:anywhere]">{t("summary.refused", { message: state.message })}</p>
  }
  return (
    <section aria-label={t("summary.title")} aria-busy={loading} className="flex min-h-[2.125rem] items-start gap-2">
      <dl className={cn("grid min-w-0 flex-1 grid-cols-2 gap-x-4 gap-y-0.5 text-xs leading-4 transition-opacity", dimOf(state))}>
        {[size, free].map(({ label, text, tone }) => (
          <div key={label} className="flex min-w-0 items-baseline gap-1">
            <dt className="min-w-0 truncate text-muted-foreground">{label}</dt>
            <dd className={cn("shrink-0 whitespace-nowrap font-medium tabular-nums", tone === "warning" && warningTone)}>
              <FigureValue state={state} text={text} skeleton="inline-block h-3 w-10 align-middle" />
            </dd>
          </div>
        ))}
        <div className="col-span-2 flex min-w-0 items-baseline gap-1">
          <dt className="shrink-0 text-muted-foreground">{way.label}</dt>
          <dd className="min-w-0 truncate font-medium">
            <FigureValue state={state} text={way.text} skeleton="inline-block h-3 w-24 align-middle" />
          </dd>
        </div>
      </dl>
      <span className="flex w-3.5 shrink-0">{loading && <Planning />}</span>
    </section>
  )
}

function SummaryTorrents({ state, hash }: { state: SummaryState; hash: string }) {
  const { t } = useTranslation("transfers")
  const [open, setOpen] = useState(false)
  const listId = useId()
  const shown = state.kind === "summary" ? state.summary : null
  const count = shown?.members.length

  return (
    <div className={cn("overflow-hidden rounded-lg border bg-muted/20 transition-opacity", dimOf(state))}>
      <SummaryNotes state={state} className="border-b px-4 py-3" />
      <button
        type="button"
        aria-expanded={open && shown !== null}
        aria-controls={listId}
        disabled={shown === null}
        onClick={() => setOpen(!open)}
        className="flex h-11 w-full items-center justify-between gap-3 px-4 text-left text-sm disabled:cursor-default"
      >
        <span className="font-medium">{t("summary.torrents")}</span>
        <span className="flex items-center gap-2 tabular-nums text-muted-foreground">
          {state.kind === "loading" ? <span className="block h-4 w-6 animate-pulse rounded bg-muted" /> : count ?? "—"}
          <ChevronDown aria-hidden className={cn("size-4 transition-transform", open && shown && "rotate-180")} />
        </span>
      </button>
      {open && shown && state.kind === "summary" && (
        <div id={listId} className="border-t px-4 py-3">
          <MemberList summary={shown} hash={hash} targetInstanceId={state.targetInstanceId} />
        </div>
      )}
    </div>
  )
}

function SummaryNotes({ state, className }: { state: SummaryState; className?: string }) {
  const { t } = useTranslation("transfers")
  const shown = state.kind === "summary" ? state.summary : null
  if (!shown || (shown.freeKnown && shown.hints.length === 0)) return null
  return (
    <div className={cn("space-y-1.5", className)}>
      {!shown.freeKnown && <Note>{t("summary.freeUnknown")}</Note>}
      <TransferHints hints={shown.hints} />
    </div>
  )
}

function MemberList({ summary, hash, targetInstanceId }: { summary: TransferSummary; hash: string; targetInstanceId: number }) {
  const { t } = useTranslation("transfers")
  const instanceName = useInstanceName()
  const display = useIncognitoDisplay()
  return (
    <ul className="space-y-1.5 text-sm">
      {summary.members.map(member => (
        <li key={member.hash} className="flex items-start gap-2">
          <span className="min-w-0 flex-1 text-xs leading-5 [overflow-wrap:anywhere]">{display.name(member)}</span>
          {member.targetInstanceId !== targetInstanceId ? (
            <Badge variant="outline" className="shrink-0 font-normal">{t("summary.crossSeedInstance", { instance: instanceName(member.targetInstanceId) })}</Badge>
          ) : member.hash !== hash && (
            <Badge variant="secondary" className="shrink-0 font-normal">{t("summary.crossSeed")}</Badge>
          )}
        </li>
      ))}
    </ul>
  )
}

function FigureValue({ state, text, skeleton }: { state: SummaryState; text?: string; skeleton: string }) {
  if (state.kind === "loading") return <span className={cn("animate-pulse rounded bg-muted", skeleton)} />
  return text ?? <span className="text-muted-foreground">—</span>
}

function Planning() {
  const { t } = useTranslation("transfers")
  return (
    <span role="status" className="flex shrink-0 items-center text-muted-foreground">
      <Loader2 className="size-3.5 animate-spin" aria-hidden />
      <span className="sr-only">{t("summary.loading")}</span>
    </span>
  )
}

function useMoreBelow(ref: RefObject<HTMLElement | null>, content: unknown): boolean {
  const [more, setMore] = useState(false)
  useEffect(() => {
    const element = ref.current
    if (!element) return
    const measure = () => setMore(element.scrollTop + element.clientHeight < element.scrollHeight - 4)
    measure()
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(measure)
    observer?.observe(element)
    if (element.firstElementChild) observer?.observe(element.firstElementChild)
    element.addEventListener("scroll", measure, { passive: true })
    return () => {
      observer?.disconnect()
      element.removeEventListener("scroll", measure)
    }
  }, [ref, content])
  return more
}

function Warnings({ warnings }: { warnings: string[] }) {
  const { t } = useTranslation("transfers")
  const phone = useMediaQuery(PHONE_QUERY)
  const [open, setOpen] = useState(false)
  const listId = useId()
  const notes = warnings.map(warning => <Note key={warning} tone="warning" oneLine={phone && !open}>{warning}</Note>)
  if (!phone) return <>{notes}</>
  return (
    <div className="relative flex items-start gap-2">
      <div id={listId} className="min-w-0 flex-1 space-y-1">{notes}</div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={listId}
        aria-label={t(open ? "dialog.warningsLess" : "dialog.warningsMore")}
        onClick={() => setOpen(!open)}
        className={cn("shrink-0 after:absolute after:-inset-y-2 after:inset-x-0", warningTone)}
      >
        <ChevronDown aria-hidden className={cn("size-4 transition-transform", open && "rotate-180")} />
      </button>
    </div>
  )
}

function Note({ tone, oneLine, children }: { tone?: "warning"; oneLine?: boolean; children: string }) {
  if (tone === "warning") {
    return (
      <p role="alert" className={cn("flex gap-1.5 text-xs", warningTone)}>
        <AlertTriangle className="mt-px size-3.5 shrink-0" aria-hidden />
        <span className={cn("min-w-0", oneLine && "truncate")}>{children}</span>
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
