/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Loader2, Power } from "lucide-react"
import { useTranslation } from "react-i18next"

import { RestartOverlay } from "@/components/settings/RestartOverlay"
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
import { useSelfUpdate } from "@/hooks/useSelfUpdate"

export function RestartButton() {
  const { t } = useTranslation("settings")
  const { restartAvailable, confirmOpen, setConfirmOpen, restart, pending, error, overlay } = useSelfUpdate()

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
          {error && (
            <p className="text-sm text-destructive">{error.message}</p>
          )}
          <AlertDialogFooter>
            <AlertDialogCancel disabled={pending}>{t("common:actions.cancel")}</AlertDialogCancel>
            <Button onClick={restart} disabled={pending}>
              {pending && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
              {t("application.restart.confirm")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <RestartOverlay state={overlay} />
    </>
  )
}
