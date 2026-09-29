/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Copy, Loader2 } from "lucide-react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog"
import type { RestartOverlay as RestartOverlayState } from "@/hooks/useSelfUpdate"
import { copyTextToClipboard } from "@/lib/utils"
import type { SelfUpdateResult } from "@/types"

const ROLLBACK_DOCS_URL = "https://getqui.com/docs/getting-started/installation#roll-back-an-update"

export function RestartOverlay({ state, update }: { state: RestartOverlayState; update?: SelfUpdateResult }) {
  const { t } = useTranslation("settings")

  if (state === "hidden") {
    return null
  }

  let title = t("application.restart.overlay.restartingTitle")
  let detail = t("application.restart.overlay.restartingDetail")
  if (state === "slow") {
    title = t("application.restart.overlay.slowTitle")
    detail = t("application.restart.overlay.slowDetail")
  } else if (state === "notApplied") {
    title = t("application.update.overlay.notAppliedTitle")
    detail = t("application.update.overlay.notAppliedDetail")
  } else if (state === "installed") {
    title = t("application.update.overlay.installedTitle")
    detail = t("application.update.overlay.installedDetail")
  }
  const done = state === "notApplied" || state === "installed"

  const copyRollback = async (command: string) => {
    const label = t("application.update.overlay.rollbackLabel")
    try {
      await copyTextToClipboard(command)
      toast.success(t("application.toasts.copied", { label }))
    } catch {
      toast.error(t("application.toasts.copyFailed", { label: label.toLowerCase() }))
    }
  }

  // A modal with no close path: it traps focus, so the keyboard cannot reach
  // a Settings tab whose switch would unmount the poll.
  return (
    <Dialog open>
      <DialogContent
        showCloseButton={false}
        className="inset-0 z-[100] flex max-w-none translate-x-0 translate-y-0 items-center justify-center rounded-none border-0 bg-background/95 shadow-none backdrop-blur-sm sm:max-w-none">
        <div role="status" aria-live="polite" className="flex max-w-md flex-col items-center gap-3 text-center">
          {!done && <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />}
          <DialogTitle className="font-medium">{title}</DialogTitle>
          <DialogDescription>{detail}</DialogDescription>
          {update?.backupError && (
            <p className="text-sm text-destructive">{t("application.update.overlay.noBackup", { error: update.backupError })}</p>
          )}
          {state === "slow" && update?.rollbackCommand && (
            <div className="flex w-full flex-col gap-2 text-left">
              <p className="text-sm text-muted-foreground">{t("application.update.overlay.rollbackIntro")}</p>
              <div className="flex items-start gap-2 rounded-md border bg-muted/50 p-2">
                <code className="min-w-0 flex-1 break-all font-mono text-xs">{update.rollbackCommand}</code>
                <Button
                  variant="ghost"
                  size="icon"
                  className="h-7 w-7 shrink-0"
                  onClick={() => void copyRollback(update.rollbackCommand)}
                  title={t("application.copyTitle", { label: t("application.update.overlay.rollbackLabel") })}
                >
                  <Copy className="h-3.5 w-3.5" />
                </Button>
              </div>
              <a className="text-sm underline" href={ROLLBACK_DOCS_URL} target="_blank" rel="noopener noreferrer">
                {t("application.update.overlay.rollbackDocs")}
              </a>
            </div>
          )}
          {done && (
            <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
              {t("application.update.overlay.reload")}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
