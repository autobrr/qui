/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { AlertTriangle, ArrowRight, Download, ExternalLink, Info, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import type { SelfUpdate } from "@/hooks/useSelfUpdate"

interface InstallUpdateButtonProps {
  // The parent owns the hook and renders its RestartOverlay outside the
  // ["latest-version"] condition: that query turns null while qui is down.
  selfUpdate: SelfUpdate
  // The request installs this tag, so the notes the user reads match what
  // qui installs.
  release: { tag_name: string; html_url: string }
  className?: string
}

export function InstallUpdateButton({ selfUpdate, release, className }: InstallUpdateButtonProps) {
  const { t } = useTranslation("settings")
  const {
    selfUpdateAvailable, selfUpdateUnavailableReason, supportChecking, supportCheckFailed, retrySupportCheck,
    currentVersion, confirmOpen, setConfirmOpen, install, pending, error,
  } = selfUpdate
  const supported = selfUpdateAvailable && !supportCheckFailed && !supportChecking
  const guidanceByReason = {
    container: { reason: t("application.update.unavailable.container.reason"), step: t("application.update.unavailable.container.step"), url: "https://getqui.com/docs/getting-started/docker/" },
    disabled: { reason: t("application.update.unavailable.disabled.reason"), step: t("application.update.unavailable.disabled.step"), url: "https://getqui.com/docs/configuration/reference/#settings" },
    development: { reason: t("application.update.unavailable.development.reason"), step: t("application.update.unavailable.development.step"), url: "https://getqui.com/docs/getting-started/installation/#manual-download" },
    directory: { reason: t("application.update.unavailable.directory.reason"), step: t("application.update.unavailable.directory.step"), url: "https://getqui.com/docs/getting-started/installation/#updating" },
  }
  const guidance = selfUpdateUnavailableReason ? guidanceByReason[selfUpdateUnavailableReason] : undefined


  return (
    <>
      <Button variant="outline" size="sm" className={className} disabled={supportChecking} onClick={() => setConfirmOpen(true)}>
        {supportChecking ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : <Download className="mr-2 h-4 w-4" />}
        {t("application.update.button")}
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {supportChecking ? (
                <span className="flex items-center gap-2"><Loader2 className="h-4 w-4 animate-spin" aria-hidden />{t("application.update.support.checkingTitle")}</span>
              ) : supportCheckFailed ? t("application.update.support.failedTitle") : supported ? t("application.update.confirmTitle") : t("application.update.button")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {supportChecking ? t("application.update.support.checkingDescription") : supportCheckFailed ? t("application.update.support.failedDescription") : supported ? t("application.update.confirmDescription") : t("application.update.unavailable.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2 rounded-md border bg-muted/40 px-4 py-3">
            <dl className="flex items-center gap-4">
              <div>
                <dt className="text-xs text-muted-foreground">{t("application.update.currentVersion")}</dt>
                <dd className="font-mono text-sm">v{currentVersion}</dd>
              </div>
              <ArrowRight className="h-4 w-4 text-muted-foreground" aria-hidden />
              <div>
                <dt className="text-xs text-muted-foreground">{t("application.update.targetVersion")}</dt>
                <dd className="font-mono text-sm font-semibold">{release.tag_name}</dd>
              </div>
            </dl>
            <a className="ml-auto inline-flex items-center gap-1 text-sm underline" href={release.html_url} target="_blank" rel="noopener noreferrer">
              {t("application.update.releaseNotes")}
              <ExternalLink className="h-3.5 w-3.5" />
            </a>
          </div>
          {supported && (
            <p className="flex gap-2 text-xs text-muted-foreground">
              <Info className="mt-px h-3.5 w-3.5 shrink-0" aria-hidden />
              {t("application.update.runningWork")}
            </p>
          )}
          {!supportChecking && !supported && (supportCheckFailed || guidance) && (
            <div className={`space-y-3 rounded-md border p-4 ${supportCheckFailed ? "border-destructive/40 bg-destructive/5" : "border-blue-500/30 bg-blue-500/5"}`}>
              <h3 className={`flex items-center gap-2 text-sm font-medium ${supportCheckFailed ? "text-destructive" : "text-blue-600 dark:text-blue-400"}`}>
                {supportCheckFailed ? <AlertTriangle className="h-4 w-4 shrink-0 text-destructive" aria-hidden /> : <Info className="h-4 w-4 shrink-0 text-blue-500" aria-hidden />}
                {t("application.update.unavailable.nextStep")}
              </h3>
              {!supportCheckFailed && guidance && <p className="text-sm leading-relaxed">{guidance.reason}</p>}
              <p className="text-sm leading-relaxed text-muted-foreground">{supportCheckFailed ? t("application.update.support.retryDescription") : guidance?.step}</p>
              {!supportCheckFailed && guidance && (
                <a className="inline-flex items-center gap-1 text-sm underline underline-offset-4" href={guidance.url} target="_blank" rel="noopener noreferrer">
                  {t("application.update.unavailable.guide")}<ExternalLink className="h-3.5 w-3.5" aria-hidden />
                </a>
              )}
            </div>
          )}
          {supported && error && (
            <p className="text-sm text-destructive">{error.message}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>{t(supported ? "common:actions.cancel" : "common:actions.close")}</AlertDialogCancel>
            {supportCheckFailed && <Button onClick={retrySupportCheck}>{t("application.update.support.retry")}</Button>}
            {supported && (
              <Button onClick={() => install(release.tag_name)} disabled={pending}>
                {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                {t("application.update.confirm")}
              </Button>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
