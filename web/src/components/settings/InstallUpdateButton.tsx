/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Download, ExternalLink, Loader2 } from "lucide-react"
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
  const { selfUpdateAvailable, currentVersion, confirmOpen, setConfirmOpen, install, pending, error } = selfUpdate

  if (!selfUpdateAvailable) {
    return null
  }

  return (
    <>
      <Button variant="outline" size="sm" className={className} onClick={() => setConfirmOpen(true)}>
        <Download className="mr-2 h-4 w-4" />
        {t("application.update.button")}
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("application.update.confirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("application.update.confirmDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
            <dt className="text-muted-foreground">{t("application.update.currentVersion")}</dt>
            <dd className="font-mono">{currentVersion}</dd>
            <dt className="text-muted-foreground">{t("application.update.targetVersion")}</dt>
            <dd className="font-mono">{release.tag_name}</dd>
          </dl>
          <a className="inline-flex items-center gap-1 text-sm underline" href={release.html_url} target="_blank" rel="noopener noreferrer">
            {t("application.update.releaseNotes")}
            <ExternalLink className="h-3.5 w-3.5" />
          </a>
          {error && (
            <p className="text-sm text-destructive">{error.message}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>{t("common:actions.cancel")}</AlertDialogCancel>
            <Button onClick={() => install(release.tag_name)} disabled={pending}>
              {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("application.update.confirm")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  )
}
