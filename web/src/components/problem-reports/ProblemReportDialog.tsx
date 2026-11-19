/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useMutation, useQuery } from "@tanstack/react-query"
import type { TFunction } from "i18next"
import { MessageSquareWarning } from "lucide-react"
import { useId, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useProblemReportsAvailable } from "@/hooks/useProblemReportsAvailable"
import { api } from "@/lib/api"
import type { ProblemReportFacts } from "@/types"

/** The icon button that opens the report dialog. Renders nothing while Problem reports are unavailable. */
export function ProblemReportButton({ className, onOpen }: { className?: string; onOpen: () => void }) {
  const { t } = useTranslation("common")
  const available = useProblemReportsAvailable()
  if (!available) return null

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <Button variant="ghost" size="icon" className={className} onClick={onOpen} aria-label={t("problemReports.reportProblem")}>
          <MessageSquareWarning className="h-3.5 w-3.5" />
        </Button>
      </TooltipTrigger>
      <TooltipContent side="top" className="text-xs">
        {t("problemReports.reportProblem")}
      </TooltipContent>
    </Tooltip>
  )
}

/** The menu row that opens the report dialog. Renders nothing while Problem reports are unavailable. */
export function ProblemReportMenuItem({ onOpen }: { onOpen: () => void }) {
  const { t } = useTranslation("common")
  const available = useProblemReportsAvailable()
  if (!available) return null

  return (
    <DropdownMenuItem onSelect={onOpen}>
      <MessageSquareWarning className="h-4 w-4" />
      {t("problemReports.reportProblem")}
    </DropdownMenuItem>
  )
}

export function ProblemReportDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-2xl">
        {/* The dialog unmounts its content when it closes, so each open starts a new report. */}
        <ProblemReportForm onSent={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  )
}

function ProblemReportForm({ onSent }: { onSent: () => void }) {
  const { t } = useTranslation("common")
  const textId = useId()
  const [text, setText] = useState("")
  // One id for each open, so that a retry of the same report stores no second row.
  const [submissionId] = useState(newSubmissionId)

  const factsQuery = useQuery({
    queryKey: ["problem-reports", "facts", "menu"],
    queryFn: () => api.getProblemReportFacts("menu"),
    gcTime: 0,
  })
  const sendMutation = useMutation({
    mutationFn: (facts: ProblemReportFacts) => api.sendProblemReport({
      submissionId,
      text,
      startedFrom: "menu",
      ...reportFacts(facts),
      browser: {
        route: window.location.pathname,
        userAgent: navigator.userAgent,
        viewportWidth: window.innerWidth,
        viewportHeight: window.innerHeight,
        devicePixelRatio: window.devicePixelRatio,
      },
      failedRequests: [],
    }),
    onSuccess: () => toast.success(t("problemReports.sent")),
  })

  const facts = factsQuery.data
  // Typing re-renders the form, so the facts text is built once per load.
  const factsText = useMemo(() => facts && JSON.stringify(reportFacts(facts), null, 2), [facts])
  const canSend = facts !== undefined && text.trim() !== "" && !sendMutation.isPending

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("problemReports.reportProblem")}</DialogTitle>
        <DialogDescription>{t("problemReports.description")}</DialogDescription>
      </DialogHeader>

      <div className="grid gap-2">
        <Label htmlFor={textId}>{t("problemReports.textLabel")}</Label>
        <Textarea id={textId} value={text} onChange={(event) => setText(event.target.value)} className="max-h-48" required />
      </div>

      <div className="grid min-w-0 gap-2">
        <p className="text-sm font-medium">{t("problemReports.factsLabel")}</p>
        {factsText ? (
          <pre className="max-h-64 overflow-auto rounded-md bg-muted p-3 text-xs">{factsText}</pre>
        ) : factsQuery.isError ? (
          <p role="alert" className="text-sm text-destructive">{t("problemReports.factsFailed", { message: factsQuery.error.message })}</p>
        ) : (
          <p className="text-sm text-muted-foreground">{t("problemReports.loadingFacts")}</p>
        )}
      </div>

      {sendMutation.isError && (
        <p role="alert" className="text-sm text-destructive">{sendErrorMessage(t, sendMutation.error)}</p>
      )}

      <DialogFooter>
        <Button disabled={!canSend} onClick={() => facts && sendMutation.mutate(facts, { onSuccess: onSent })}>
          {sendMutation.isPending ? t("problemReports.sending") : t("problemReports.send")}
        </Button>
      </DialogFooter>
    </>
  )
}

/** The part of the facts that a menu report carries: the warning lines, not the whole log. */
function reportFacts(facts: ProblemReportFacts) {
  return {
    qui: facts.qui,
    instances: facts.instances,
    notices: facts.notices,
    logs: { scope: "warnings" as const, lines: facts.logs.warnings },
  }
}

function sendErrorMessage(t: TFunction<"common">, error: Error): string {
  switch (error.message) {
    case "anonymous_full":
      return t("problemReports.errors.anonymousFull")
    case "sign_in_required":
      return t("problemReports.errors.signInRequired")
    case "account_limit":
      return t("problemReports.errors.accountLimit")
    case "reporter_full":
      return t("problemReports.errors.reporterFull")
    case "too_large":
      return t("problemReports.errors.tooLarge")
    case "token_rejected":
      return t("problemReports.errors.tokenRejected")
    case "unavailable":
      return t("problemReports.errors.unavailable")
    default:
      return error.message
  }
}

function newSubmissionId(): string {
  // getRandomValues, not randomUUID: randomUUID needs a secure context, and qui often runs on plain http.
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 0x0f) | 0x40
  bytes[8] = (bytes[8] & 0x3f) | 0x80
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("")
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`
}
