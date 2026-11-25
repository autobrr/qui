/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import type { TFunction } from "i18next"
import { ChevronRight, Loader2, MessageSquareWarning } from "lucide-react"
import { memo, useEffect, useMemo, useRef, useState, type RefObject } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"

import {
  finishProblemReportDraftSend,
  startProblemReportDraftSend,
  updateProblemReportDraft,
  useProblemReportDraft,
  type ProblemReportAttempt
} from "@/components/problem-reports/draft"
import { closeProblemReport, openProblemReport, useProblemReportOpenState } from "@/components/problem-reports/open"
import { SENDER_KEY, useSender, type SenderControls } from "@/components/problem-reports/sender"
import { SenderMenu, SignInNote } from "@/components/problem-reports/SenderMenu"
import { SEND_REPORT_KEY, useSentReports } from "@/components/problem-reports/sent-reports"
import { SentReportsEntry, SentReportsView } from "@/components/problem-reports/SentReports"
import { Button } from "@/components/ui/button"
import { CopyButton } from "@/components/ui/copy-button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useMediaQuery } from "@/hooks/useMediaQuery"
import { useProblemReportsAvailable } from "@/hooks/useProblemReportsAvailable"
import { api, APIError } from "@/lib/api"
import { failedRequests, type FailedRequest } from "@/lib/failed-requests"
import { cn } from "@/lib/utils"
import type {
  ListedProblemReport,
  ProblemReportBrowserFacts,
  ProblemReportFacts,
  ProblemReportLogScope,
  ProblemReportSender,
  ProblemReportStartedFrom
} from "@/types"

const PRIVACY_URL = "https://bin.getqui.com/privacy"
const WIDE_QUERY = "(min-width: 1024px)"
const PINNED_LOGS_QUERY = "(min-height: 640px)"

export function ProblemReportButton({ className }: { className?: string }) {
  const { t } = useTranslation("common")
  const available = useProblemReportsAvailable()
  if (!available) return null

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" className={className} onClick={() => openProblemReport("menu")} aria-label={t("problemReports.reportProblem")}>
          <MessageSquareWarning className="h-3.5 w-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        {t("problemReports.reportProblem")}
      </TooltipContent>
    </Tooltip>
  )
}

export function ProblemReportMenuItem() {
  const { t } = useTranslation("common")
  const available = useProblemReportsAvailable()
  if (!available) return null

  return (
    <DropdownMenuItem onSelect={() => openProblemReport("menu")}>
      <MessageSquareWarning className="h-4 w-4" />
      {t("problemReports.reportProblem")}
    </DropdownMenuItem>
  )
}

export function ProblemReportDialogRoot() {
  const { open, startedFrom, view } = useProblemReportOpenState()
  const wide = useMediaQuery(WIDE_QUERY)
  const sent = view === "sent"

  return (
    <Dialog open={open} onOpenChange={(next) => !next && closeProblemReport()}>
      <DialogContent
        className={cn(
          "gap-0 p-0",
          wide && !sent ? "flex h-[min(44rem,calc(100dvh-4rem))] flex-col overflow-hidden sm:max-w-4xl xl:max-w-5xl" : "max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md"
        )}
      >
        <div className={sent ? "hidden" : "contents"}>
          <ProblemReportForm startedFrom={startedFrom} wide={wide} hidden={sent} />
        </div>
        {sent && <SentReportsView />}
      </DialogContent>
    </Dialog>
  )
}

function ProblemReportForm({ startedFrom, wide, hidden }: { startedFrom: ProblemReportStartedFrom; wide: boolean; hidden: boolean }) {
  const { t } = useTranslation("common")
  const queryClient = useQueryClient()
  const textRef = useRef<HTMLTextAreaElement>(null)
  const { text, logScope, attempt } = useProblemReportDraft()
  const [browser] = useState(browserFacts)
  const [failed] = useState(failedRequests)

  const factsQuery = useQuery({
    queryKey: ["problem-reports", "facts", startedFrom],
    queryFn: () => api.getProblemReportFacts(startedFrom),
    gcTime: 0,
  })
  const senderControls = useSender()
  const sendMutation = useMutation({
    mutationKey: SEND_REPORT_KEY,
    mutationFn: ({ facts, sent }: { facts: ProblemReportFacts; sent: ProblemReportAttempt }) => api.sendProblemReport({
      submissionId: sent.submissionId,
      text: sent.text,
      startedFrom,
      qui: facts.qui,
      instances: facts.instances,
      notices: facts.notices,
      browser,
      failedRequests: [...failed],
      logs: { scope: sent.logScope, lines: logLines(facts, sent.logScope) },
    }, senderControls.sender ?? { anonymous: true }),
    onSuccess: (_report, { sent }) => {
      finishProblemReportDraftSend(sent)
      toast.success(t("problemReports.sent"))
    },
    onError: (error) => {
      if (error instanceof APIError && error.message === "token_rejected") void queryClient.invalidateQueries({ queryKey: SENDER_KEY })
    },
  })
  const sentReports = useSentReports()

  const facts = factsQuery.data
  const canSend = facts !== undefined && text.trim() !== "" && !sendMutation.isPending
  const send = () => {
    if (canSend) sendMutation.mutate({ facts, sent: startProblemReportDraftSend(newSubmissionId) }, { onSuccess: closeProblemReport })
  }

  const wasHidden = useRef(hidden)
  useEffect(() => {
    if (wasHidden.current && !hidden) textRef.current?.focus()
    wasHidden.current = hidden
  }, [hidden])

  const setText = (next: string) => updateProblemReportDraft({ text: next })
  const setLogScope = (next: ProblemReportLogScope) => updateProblemReportDraft({ logScope: next })
  const report: Report = { startedFrom, text, setText, logScope, setLogScope, facts, browser, failed, sending: sendMutation.isPending, send, textRef }
  const notes = (
    <>
      {factsQuery.isError && (
        <p role="alert" className="text-xs text-destructive">{t("problemReports.factsFailed", { message: factsQuery.error.message })}</p>
      )}
      <SignInNote signIn={senderControls.signIn} />
      {sendMutation.isError && attempt ? <Refusal error={sendMutation.error} /> : senderControls.sender && <PrivacyLine sender={senderControls.sender} />}
      {facts?.qui.newerRelease && <NewerRelease running={facts.qui.version} newer={facts.qui.newerRelease} />}
    </>
  )
  const actionBar = (
    <ActionBar
      canSend={canSend}
      sending={sendMutation.isPending}
      newerRelease={facts?.qui.newerRelease !== undefined}
      onSend={send}
      senderControls={senderControls}
      sentReports={sentReports.data}
    />
  )

  if (wide) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="grid min-h-0 flex-1 grid-cols-[minmax(0,1fr)_minmax(0,25rem)] xl:grid-cols-[minmax(0,1fr)_minmax(0,32rem)]">
          <div className="flex min-h-0 flex-col overflow-y-auto">
            <Header asTitle={!hidden} />
            <div className="flex flex-1 flex-col gap-3 px-4 pb-4 pl-11 pt-3">
              <ReportText report={report} className="min-h-32 flex-1" />
              {notes}
            </div>
          </div>
          <WhatGetsSent report={report} loading={factsQuery.isPending} />
        </div>
        {actionBar}
      </div>
    )
  }

  return (
    <div>
      <Header asTitle={!hidden} />
      <div className="space-y-3 px-4 pb-4 pl-11 pt-3">
        <ReportText report={report} className="min-h-24" />
        <SentWithIt report={report} loading={factsQuery.isPending} />
        {notes}
      </div>
      {actionBar}
    </div>
  )
}

interface Report {
  startedFrom: ProblemReportStartedFrom
  text: string
  setText: (text: string) => void
  logScope: ProblemReportLogScope
  setLogScope: (scope: ProblemReportLogScope) => void
  facts: ProblemReportFacts | undefined
  browser: ProblemReportBrowserFacts
  failed: readonly FailedRequest[]
  sending: boolean
  send: () => void
  textRef: RefObject<HTMLTextAreaElement | null>
}

function Header({ asTitle }: { asTitle: boolean }) {
  const { t } = useTranslation("common")
  const Title = asTitle ? DialogTitle : "p"
  const Description = asTitle ? DialogDescription : "p"
  return (
    <div className="flex items-start gap-3 px-4 pr-10 pt-4">
      <MessageSquareWarning className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
      <div className="min-w-0">
        <Title className="text-sm font-semibold leading-snug">{t("problemReports.reportProblem")}</Title>
        <Description className="mt-0.5 text-xs text-muted-foreground">{t("problemReports.subtitle")}</Description>
      </div>
    </div>
  )
}

function ReportText({ report, className }: { report: Report; className?: string }) {
  const { t } = useTranslation("common")
  return (
    <Textarea
      ref={report.textRef}
      autoFocus
      required
      aria-label={t("problemReports.textLabel")}
      value={report.text}
      onChange={(event) => report.setText(event.target.value)}
      onKeyDown={(event) => {
        if (event.key === "Enter" && (event.metaKey || event.ctrlKey)) report.send()
      }}
      placeholder={t(`problemReports.placeholders.${report.startedFrom}`)}
      disabled={report.sending}
      className={cn("resize-none text-[13px] leading-relaxed", className)}
    />
  )
}

function ActionBar({ canSend, sending, newerRelease, onSend, senderControls, sentReports }: {
  canSend: boolean
  sending: boolean
  newerRelease: boolean
  onSend: () => void
  senderControls: SenderControls
  sentReports: ListedProblemReport[] | undefined
}) {
  const { t } = useTranslation("common")
  return (
    <div className="flex shrink-0 items-center gap-2 border-t px-4 py-2.5 pl-11">
      <Button size="sm" className="h-7 px-2.5 text-xs" disabled={!canSend} onClick={onSend}>
        {sending && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
        {sending ? t("problemReports.sending") : newerRelease ? t("problemReports.sendAnyway") : t("problemReports.send")}
      </Button>
      <SenderMenu controls={senderControls} disabled={sending} />
      <SentReportsEntry reports={sentReports} />
    </div>
  )
}

function PrivacyLine({ sender }: { sender: ProblemReportSender }) {
  const { t } = useTranslation("common")
  return (
    <p className="text-xs leading-5 text-muted-foreground text-pretty">
      {sender.anonymous
        ? t("problemReports.privacy.anonymous")
        : t("problemReports.privacy.signedIn", { provider: t(`problemReports.sender.providers.${sender.provider}`), name: sender.name })}
      {" "}
      <a href={PRIVACY_URL} target="_blank" rel="noopener noreferrer" className="underline underline-offset-2 hover:text-foreground">
        {t("problemReports.privacy.link")}
      </a>
    </p>
  )
}

function NewerRelease({ running, newer }: { running: string; newer: { version: string; url: string } }) {
  return (
    <p className="text-xs leading-5 text-muted-foreground text-pretty">
      <Trans
        i18nKey="problemReports.newerRelease"
        ns="common"
        values={{ running: withV(running), newer: newer.version }}
        components={{
          release: <a href={newer.url} target="_blank" rel="noopener noreferrer" className="font-medium text-foreground underline underline-offset-2" />,
        }}
      />
    </p>
  )
}

function Refusal({ error }: { error: Error }) {
  const { t } = useTranslation("common")
  const { title, body } = refusalMessage(t, error)
  return (
    <div role="alert" className="relative">
      <span className="absolute -left-6 top-1.5 inline-flex h-2 w-2 rounded-full bg-yellow-500 ring-[3px] ring-yellow-500/20" />
      {title && <p className="text-sm font-semibold leading-snug">{title}</p>}
      <p className={cn("text-[13px] leading-relaxed", title ? "mt-0.5 text-muted-foreground" : "text-foreground")}>{body}</p>
    </div>
  )
}

function SentWithIt({ report, loading }: { report: Report; loading: boolean }) {
  const { t } = useTranslation("common")
  const [open, setOpen] = useState<null | "details" | "logs">(null)
  const toggle = (part: "details" | "logs") => setOpen(open === part ? null : part)
  const rows = useFactRows(report)
  const { facts } = report
  const logsOpen = open === "logs" && report.logScope !== "none"

  return (
    <div className="space-y-1.5 text-[13px]">
      <p className="text-xs text-muted-foreground">{t("problemReports.sentWithIt")}</p>
      {loading ? (
        <p className="text-xs text-muted-foreground">{t("problemReports.loadingFacts")}</p>
      ) : facts && (
        <>
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1">{t("problemReports.summary", { version: withV(facts.qui.version), count: facts.instances.length })}</span>
            <Disclose open={open === "details"} onClick={() => toggle("details")} label={t("problemReports.details")} />
          </div>
          {open === "details" && <CauseBox value={rowsText(rows)} copyLabel={t("problemReports.details")} />}
          <div className="flex flex-wrap items-center gap-2">
            <span className={cn("min-w-0 flex-1", report.logScope === "none" && "text-muted-foreground line-through")}>{t("problemReports.logs.title")}</span>
            <LogScope report={report} />
            <Disclose open={logsOpen} onClick={() => toggle("logs")} label={t("problemReports.view")} disabled={report.logScope === "none"} />
          </div>
          {logsOpen && <LogBox lines={logLines(facts, report.logScope)} />}
          <LogNotes facts={facts} logScope={report.logScope} />
        </>
      )}
    </div>
  )
}

function WhatGetsSent({ report, loading }: { report: Report; loading: boolean }) {
  const { t } = useTranslation("common")
  const rows = useFactRows(report)
  const pinned = useMediaQuery(PINNED_LOGS_QUERY)
  const { facts } = report

  return (
    <aside aria-label={t("problemReports.whatGetsSent")} className={cn("min-h-0 border-l bg-muted/30", pinned ? "flex flex-col overflow-hidden" : "overflow-y-auto")}>
      <div className="px-4 pb-3 pr-10 pt-4">
        <p className="text-sm font-semibold leading-snug">{t("problemReports.whatGetsSent")}</p>
        <p className="mt-0.5 text-xs text-muted-foreground">{t("problemReports.whatGetsSentNote")}</p>
      </div>
      {loading ? (
        <p className="px-4 text-xs text-muted-foreground">{t("problemReports.loadingFacts")}</p>
      ) : facts && (
        <>
          <div className={cn("border-y", pinned && "min-h-0 flex-1 overflow-y-auto")}>
            <Rows rows={rows} className="border-y-0" />
          </div>
          <div className={cn("space-y-2 px-4 pb-4", pinned ? "shrink-0 pt-3" : "pt-4")}>
            <div className="flex items-center gap-2 text-[13px]">
              <span className={cn("min-w-0 flex-1 font-medium", report.logScope === "none" && "text-muted-foreground line-through")}>{t("problemReports.logs.title")}</span>
              <LogScope report={report} />
            </div>
            {report.logScope !== "none" && <LogBox lines={logLines(facts, report.logScope)} compact={pinned} />}
            <LogNotes facts={facts} logScope={report.logScope} />
          </div>
        </>
      )}
    </aside>
  )
}

function LogScope({ report }: { report: Report }) {
  const { t } = useTranslation("common")
  return (
    <Select value={report.logScope} onValueChange={(value) => report.setLogScope(value as ProblemReportLogScope)} disabled={report.sending}>
      <SelectTrigger size="sm" aria-label={t("problemReports.logs.scopeLabel")} className="h-7 gap-1 px-2 text-xs">
        <SelectValue />
      </SelectTrigger>
      <SelectContent align="end">
        <SelectItem value="warnings" className="text-xs">{t("problemReports.logs.scopes.warnings")}</SelectItem>
        <SelectItem value="all" className="text-xs">{t("problemReports.logs.scopes.all")}</SelectItem>
        <SelectItem value="none" className="text-xs">{t("problemReports.logs.scopes.none")}</SelectItem>
      </SelectContent>
    </Select>
  )
}

function LogNotes({ facts, logScope }: { facts: ProblemReportFacts; logScope: ProblemReportLogScope }) {
  const { t } = useTranslation("common")
  const count = logLines(facts, logScope).length
  return (
    <>
      <p className="text-xs text-muted-foreground">
        {logScope === "warnings"
          ? t("problemReports.logs.notes.warnings", { count })
          : logScope === "all"
            ? t("problemReports.logs.notes.all", { count, level: facts.qui.logLevel })
            : t("problemReports.logs.notes.none")}
      </p>
      {!logsHoldDebug(facts.qui.logLevel) && (
        <p className="text-xs text-muted-foreground">{t("problemReports.logs.debugMissing", { level: facts.qui.logLevel })}</p>
      )}
    </>
  )
}

const LogBox = memo(function LogBox({ lines, compact }: { lines: string[]; compact?: boolean }) {
  const { t } = useTranslation("common")
  if (lines.length === 0) {
    return <p className="text-xs text-muted-foreground">{t("problemReports.logs.empty")}</p>
  }
  return (
    <div className="flex items-start gap-1 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1">
      <code className={cn("min-w-0 flex-1 overflow-y-auto py-0.5 font-mono text-[11px] leading-relaxed text-muted-foreground", compact ? "max-h-36" : "max-h-52")}>
        {lines.map((line, index) => <span key={index} className="block break-words pl-4 -indent-4">{line}</span>)}
      </code>
      <CopyButton value={lines.join("\n")} label={t("problemReports.logs.title")} className="size-6" iconClassName="size-3" />
    </div>
  )
})

function CauseBox({ value, copyLabel }: { value: string; copyLabel: string }) {
  return (
    <div className="flex items-start gap-1 rounded-md border bg-muted/40 py-1 pl-2.5 pr-1">
      <code className="min-w-0 flex-1 break-words whitespace-pre-wrap py-0.5 font-mono text-[11px] leading-relaxed text-muted-foreground">{value}</code>
      <CopyButton value={value} label={copyLabel} className="size-6" iconClassName="size-3" />
    </div>
  )
}

function Disclose({ open, onClick, label, disabled }: { open: boolean; onClick: () => void; label: string; disabled?: boolean }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-expanded={open}
      disabled={disabled}
      className="flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
    >
      {label}
      <ChevronRight className={cn("h-3 w-3 transition-transform", open && "rotate-90")} />
    </button>
  )
}

interface FactRow {
  label: string
  value: string
  secondary?: string
  mono?: boolean
  lines?: string[]
}

function Rows({ rows, className }: { rows: FactRow[]; className?: string }) {
  return (
    <dl className={cn("divide-y border-y", className)}>
      {rows.map((row) => (
        <div key={row.label} className="flex items-baseline gap-3 px-4 py-2">
          <dt className="w-28 shrink-0 text-xs uppercase text-muted-foreground xl:w-36">{row.label}</dt>
          <dd className="min-w-0 flex-1">
            <p className={cn("break-words", row.mono ? "font-mono text-xs" : "text-[13px] font-medium")}>{row.value}</p>
            {row.secondary && <p className="break-words text-xs text-muted-foreground">{row.secondary}</p>}
            {row.lines?.map((line, index) => <p key={index} className="break-words font-mono text-[11px] text-muted-foreground">{line}</p>)}
          </dd>
        </div>
      ))}
    </dl>
  )
}

function rowsText(rows: FactRow[]): string {
  return rows.flatMap((row) => [
    `${row.label}: ${row.value}${row.secondary ? `, ${row.secondary}` : ""}`,
    ...(row.lines ?? []).map((line) => `  ${line}`),
  ]).join("\n")
}

function useFactRows({ facts, browser, failed }: Report): FactRow[] {
  const { t } = useTranslation("common")
  const { formatISOTimestamp } = useDateTimeFormatters()
  return useMemo(() => {
    if (!facts) return []
    const { qui } = facts
    const none = t("problemReports.facts.none")

    return [
      { label: t("problemReports.facts.version"), value: withV(qui.version), secondary: qui.commit, mono: true },
      {
        label: t("problemReports.facts.system"),
        value: `${qui.os}/${qui.arch}`,
        secondary: qui.appContainer ? t("problemReports.facts.appContainer") : undefined,
        mono: true,
      },
      { label: t("problemReports.facts.database"), value: qui.database, mono: true },
      { label: t("problemReports.facts.logLevel"), value: qui.logLevel, mono: true },
      { label: t("problemReports.facts.started"), value: formatISOTimestamp(qui.startedAt) },
      {
        label: t("problemReports.facts.experimentalFeatures"),
        value: qui.experimentalFeatures.length > 0 ? qui.experimentalFeatures.join(", ") : none,
        mono: qui.experimentalFeatures.length > 0,
      },
      ...facts.instances.map((instance, index) => ({
        label: t("problemReports.facts.instance", { number: index + 1 }),
        value: t("problemReports.facts.versions", {
          qbittorrent: instance.qbittorrentVersion ?? t("problemReports.facts.unknownVersion"),
          libtorrent: instance.libtorrentVersion ?? t("problemReports.facts.unknownVersion"),
        }),
        secondary: [
          t(`problemReports.facts.states.${instance.state}`),
          instance.torrentCount !== undefined && t("problemReports.facts.torrentCount", { count: instance.torrentCount }),
        ].filter(Boolean).join(", "),
      })),
      {
        label: t("problemReports.facts.notices"),
        value: facts.notices.length > 0 ? t("problemReports.facts.openCount", { count: facts.notices.length }) : none,
        lines: facts.notices.map((notice) => [
          t([`systemNotice.kinds.${notice.kind}.title`, "systemNotice.unknown.title"]),
          formatISOTimestamp(notice.openedAt),
          notice.cause,
        ].join(" · ")),
      },
      { label: t("problemReports.facts.page"), value: browser.route, mono: true },
      {
        label: t("problemReports.facts.browser"),
        value: browser.userAgent,
        secondary: t("problemReports.facts.screen", { width: browser.viewportWidth, height: browser.viewportHeight, ratio: browser.devicePixelRatio }),
        mono: true,
      },
      {
        label: t("problemReports.facts.failedRequests"),
        value: failed.length > 0 ? t("problemReports.facts.failedCount", { count: failed.length }) : none,
        lines: failed.map((request) => [
          `${request.method} ${request.path}`,
          request.status ?? t("problemReports.facts.noAnswer"),
          request.error,
          formatISOTimestamp(request.time),
        ].join(" · ")),
      },
    ]
  }, [facts, browser, failed, t, formatISOTimestamp])
}

function logLines(facts: ProblemReportFacts, scope: ProblemReportLogScope): string[] {
  switch (scope) {
    case "warnings":
      return facts.logs.warnings
    case "all":
      return facts.logs.all
    case "none":
      return []
  }
}

function logsHoldDebug(level: string): boolean {
  const normalized = level.trim().toLowerCase()
  return normalized === "debug" || normalized === "trace"
}

function withV(version: string): string {
  return /^\d/.test(version) ? `v${version}` : version
}

function refusalMessage(t: TFunction<"common">, error: Error): { title?: string; body: string } {
  const reason = error instanceof APIError ? error.message : undefined
  switch (reason) {
    case "anonymous_full":
      return { title: t("problemReports.errors.anonymousFull.title"), body: t("problemReports.errors.anonymousFull.body") }
    case "sign_in_required":
      return { body: t("problemReports.errors.signInRequired") }
    case "account_limit":
      return { body: t("problemReports.errors.accountLimit") }
    case "reporter_full":
      return { body: t("problemReports.errors.reporterFull") }
    case "too_large":
      return { body: t("problemReports.errors.tooLarge") }
    case "token_rejected":
      return { body: t("problemReports.errors.tokenRejected") }
    case "unavailable":
      return { body: t("problemReports.errors.unavailable") }
    default:
      return { body: t("problemReports.errors.sendFailed", { message: error.message }) }
  }
}

function browserFacts(): ProblemReportBrowserFacts {
  return {
    route: window.location.pathname,
    userAgent: navigator.userAgent,
    viewportWidth: window.innerWidth,
    viewportHeight: window.innerHeight,
    devicePixelRatio: window.devicePixelRatio,
  }
}

function newSubmissionId(): string {
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
