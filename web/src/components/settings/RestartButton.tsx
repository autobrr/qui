/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Loader2, Power } from "lucide-react"
import { createPortal } from "react-dom"
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
import { type RestartOverlay as RestartOverlayState, useSelfUpdate } from "@/hooks/useSelfUpdate"

export function RestartButton() {
  const { t } = useTranslation("settings")
  const { restartAvailable, confirmOpen, setConfirmOpen, restart, restartPending, restartError, overlay } = useSelfUpdate()

  if (!restartAvailable) {
    return null
  }

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setConfirmOpen(true)}>
        <Power className="mr-2 h-4 w-4" />
        {t("application.restart.button")}
      </Button>
      <AlertDialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("application.restart.confirmTitle")}</AlertDialogTitle>
            <AlertDialogDescription>{t("application.restart.confirmDescription")}</AlertDialogDescription>
          </AlertDialogHeader>
          {restartError && (
            <p className="text-sm text-destructive">{restartError.message}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={restartPending}>{t("common:actions.cancel")}</AlertDialogCancel>
            <Button onClick={restart} disabled={restartPending}>
              {restartPending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("application.restart.confirm")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <RestartOverlay state={overlay} />
    </>
  )
}

function RestartOverlay({ state }: { state: RestartOverlayState }) {
  const { t } = useTranslation("settings")

  if (state === "hidden") {
    return null
  }

  const slow = state === "slow"
  return createPortal(
    <div className="fixed inset-0 z-[100] flex items-center justify-center bg-background/95 p-6 backdrop-blur-sm">
      <div role="status" aria-live="polite" className="flex max-w-md flex-col items-center gap-3 text-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        <p className="text-lg font-medium">
          {slow ? t("application.restart.overlay.slowTitle") : t("application.restart.overlay.restartingTitle")}
        </p>
        <p className="text-sm text-muted-foreground">
          {slow ? t("application.restart.overlay.slowDetail") : t("application.restart.overlay.restartingDetail")}
        </p>
      </div>
    </div>,
    document.body
  )
}
