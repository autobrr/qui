/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useIsMutating, useMutation, useQueryClient } from "@tanstack/react-query"
import { MessageSquareWarning, Trash2 } from "lucide-react"
import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { clearProblemReportDraft, isNotSent, useProblemReportDraft, type ProblemReportDraft } from "@/components/problem-reports/draft"
import { showProblemReportView } from "@/components/problem-reports/open"
import { SEND_REPORT_KEY, SENT_REPORTS_KEY, useSentReports } from "@/components/problem-reports/sent-reports"
import { Button } from "@/components/ui/button"
import { DialogDescription, DialogTitle } from "@/components/ui/dialog"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { api, APIError } from "@/lib/api"
import { parseJsonNumber, readClientSetting, useClientSetting, writeClientSetting } from "@/lib/client-settings"
import { cn } from "@/lib/utils"
import type { ListedProblemReport } from "@/types"

const SEEN_AT_KEY = "qui-problem-reports-seen-at"
const SEEN_AT = { defaultValue: null as number | null, parse: parseJsonNumber }
const TITLE_LENGTH = 80

function changedCount(reports: ListedProblemReport[], seenAt: number | null): number {
  return reports.filter((report) => {
    const changedAt = Date.parse(report.statusChangedAt)
    return changedAt > Date.parse(report.sentAt) && (seenAt === null || changedAt > seenAt)
  }).length
}

export function SentReportsEntry({ reports }: { reports: ListedProblemReport[] | undefined }) {
  const { t } = useTranslation("common")
  const [seenAt] = useClientSetting(SEEN_AT_KEY, SEEN_AT)
  const notSent = useNotSent(useProblemReportDraft())
  if (!notSent && (!reports || reports.length === 0)) return null

  const changed = changedCount(reports ?? [], seenAt)
  return (
    <Button
      variant="ghost"
      size="sm"
      className="ml-auto h-7 gap-1.5 px-2 text-xs text-muted-foreground hover:text-foreground"
      onClick={() => showProblemReportView("sent")}
    >
      {t("problemReports.sentReports.title")}
      {changed > 0 && (
        <>
          <span aria-hidden className="rounded-full bg-primary px-1.5 text-10 font-semibold leading-4 tabular-nums text-primary-foreground">{changed}</span>
          {" "}
          <span className="sr-only">{t("problemReports.sentReports.changed", { count: changed })}</span>
        </>
      )}
    </Button>
  )
}

export function SentReportsView() {
  const { t } = useTranslation("common")
  const { data } = useSentReports()
  const reports = data ?? []
  const draft = useProblemReportDraft()
  const notSent = useNotSent(draft)
  const headingRef = useRef<HTMLHeadingElement>(null)

  useEffect(() => {
    headingRef.current?.focus()
  }, [])
  useEffect(() => {
    if (!data || data.length === 0) return
    const newest = Math.max(...data.map((report) => Date.parse(report.statusChangedAt)))
    if (newest > (readClientSetting(SEEN_AT_KEY, SEEN_AT) ?? -Infinity)) writeClientSetting(SEEN_AT_KEY, newest)
  }, [data])

  return (
    <>
      <div className="flex items-start gap-3 px-4 pr-10 pt-4">
        <MessageSquareWarning className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
        <div className="min-w-0">
          <DialogTitle ref={headingRef} tabIndex={-1} className="text-sm font-semibold leading-snug outline-none">{t("problemReports.sentReports.title")}</DialogTitle>
          <DialogDescription className="mt-0.5 text-xs text-muted-foreground">{t("problemReports.sentReports.subtitle")}</DialogDescription>
        </div>
      </div>
      <ul className="space-y-0.5 px-2 pb-3 pl-9 pt-3">
        {notSent && <NotSentRow text={draft.text} />}
        {reports.map((report) => <SentReportRow key={report.id} report={report} />)}
        {reports.length === 0 && !notSent && <li className="px-2 py-1.5 text-xs text-muted-foreground">{t("problemReports.sentReports.empty")}</li>}
      </ul>
      <div className="flex items-center gap-2 border-t px-4 py-2.5 pl-11">
        <Button size="sm" variant="outline" className="h-7 px-2.5 text-xs" onClick={() => showProblemReportView("compose")}>
          {t("problemReports.sentReports.back")}
        </Button>
      </div>
    </>
  )
}

function useNotSent(draft: ProblemReportDraft): boolean {
  const sending = useIsMutating({ mutationKey: SEND_REPORT_KEY }) > 0
  return !sending && isNotSent(draft)
}

function NotSentRow({ text }: { text: string }) {
  const { t } = useTranslation("common")
  return (
    <li className="flex items-start gap-3 rounded-md px-2 py-1.5 hover:bg-accent/60">
      <span className="mt-1.5 flex h-2 w-4 items-center justify-center">
        <span className="inline-flex h-2 w-2 shrink-0 rounded-full bg-warning ring-3 ring-warning/20" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block break-words text-sm font-medium">{draftTitle(text)}</span>
        <span className="block break-words text-xs text-muted-foreground">{t("problemReports.sentReports.status.notSent")}</span>
      </span>
      <DeleteButton onClick={clearProblemReportDraft} />
    </li>
  )
}

function draftTitle(text: string): string {
  const first = text.trim().split("\n", 1)[0].trim()
  return Array.from(first).slice(0, TITLE_LENGTH).join("")
}

function DeleteButton({ onClick, disabled }: { onClick: () => void; disabled?: boolean }) {
  const { t } = useTranslation("common")
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button
          variant="ghost"
          size="icon"
          aria-label={t("problemReports.sentReports.delete")}
          disabled={disabled}
          onClick={onClick}
          className="size-6 text-muted-foreground hover:text-foreground"
        >
          <Trash2 className="size-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">{t("problemReports.sentReports.delete")}</TooltipContent>
    </Tooltip>
  )
}

function SentReportRow({ report }: { report: ListedProblemReport }) {
  const { t } = useTranslation("common")
  const { formatDateOnly } = useDateTimeFormatters()
  const queryClient = useQueryClient()
  const deleteMutation = useMutation({
    mutationFn: () => api.deleteProblemReport(report.id),
    onSuccess: async () => {
      await queryClient.cancelQueries({ queryKey: SENT_REPORTS_KEY })
      queryClient.setQueryData<ListedProblemReport[]>(SENT_REPORTS_KEY, (rows) => rows?.filter((row) => row.id !== report.id))
    },
    onError: (error) => toast.error(error instanceof APIError && error.message === "unavailable"
      ? t("problemReports.sentReports.deleteUnavailable")
      : t("problemReports.sentReports.deleteFailed", { message: error.message })),
  })

  return (
    <li className="flex items-start gap-3 rounded-md px-2 py-1.5 hover:bg-accent/60">
      <span className="mt-1.5 flex h-2 w-4 items-center justify-center"><StatusDot report={report} /></span>
      <span className="min-w-0 flex-1">
        <span className="block break-words text-sm font-medium">{report.title}</span>
        <ReportStatus report={report} />
      </span>
      <span className="mt-0.5 text-xs tabular-nums text-muted-foreground">{formatDateOnly(Math.floor(Date.parse(report.sentAt) / 1000))}</span>
      <DeleteButton onClick={() => deleteMutation.mutate()} disabled={deleteMutation.isPending} />
    </li>
  )
}

function ReportStatus({ report }: { report: ListedProblemReport }) {
  const { t } = useTranslation("common")
  const { issue } = report

  if (report.status === "issue" && issue) {
    const text = issue.closed
      ? t("problemReports.sentReports.status.issueClosed", { number: issue.number })
      : t("problemReports.sentReports.status.issue", { number: issue.number })
    return (
      <span className="block break-words text-xs text-foreground">
        {issue.url ? <a href={issue.url} target="_blank" rel="noopener noreferrer" className="underline-offset-2 hover:underline">{text}</a> : text}
      </span>
    )
  }
  return (
    <span className="block break-words text-xs text-muted-foreground">
      {report.status === "closed"
        ? report.reason
          ? t("problemReports.sentReports.status.closed", { reason: report.reason })
          : t("problemReports.sentReports.status.closedNoReason")
        : t("problemReports.sentReports.status.received")}
    </span>
  )
}

function StatusDot({ report }: { report: ListedProblemReport }) {
  const open = report.status === "issue" && report.issue?.closed !== true
  const received = report.status === "received"
  return (
    <span
      className={cn(
        "inline-flex h-2 w-2 shrink-0 rounded-full",
        received && "bg-muted-foreground ring-3 ring-muted-foreground/20",
        open && "bg-success ring-3 ring-success/20",
        !received && !open && "border border-muted-foreground/60"
      )}
    />
  )
}
