/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Loader2 } from "lucide-react"
import { useState, type ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { useReturnFocus } from "@/components/kit/use-return-focus"
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

interface ConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description: ReactNode
  /** The label of the button that does the step, for example "Delete". */
  confirmLabel: string
  /** The label of the button that closes the dialog, when "Cancel" would name the step itself, for example "Keep running". */
  cancelLabel?: string
  /**
   * Runs on the confirm button. When it returns a promise, the button shows a spinner, and the dialog closes when the
   * promise resolves. A rejected promise keeps the dialog open: the caller reports the error, for example in a toast.
   */
  onConfirm: () => void | Promise<unknown>
  /** A step that the user can undo gets the plain button color, not the red one. */
  reversible?: boolean
  /** Holds the confirm button off, for example while the facts that the description needs still load. */
  confirmDisabled?: boolean
  /** More content between the description and the buttons, for example a checkbox. */
  children?: ReactNode
}

/** The question before a step that the user cannot undo. */
export function ConfirmDialog({ open, onOpenChange, title, description, confirmLabel, cancelLabel, onConfirm, reversible = false, confirmDisabled = false, children }: ConfirmDialogProps) {
  const { t } = useTranslation("common")
  const [pending, setPending] = useState(false)
  const returnFocus = useReturnFocus()

  const confirm = async () => {
    const result = onConfirm()
    if (!(result instanceof Promise)) {
      onOpenChange(false)
      return
    }
    setPending(true)
    try {
      await result
      onOpenChange(false)
    } catch {
      // The caller reports the error. The dialog stays open, so the user can try again.
    } finally {
      setPending(false)
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
      {/* Radix focuses Cancel on open. The dialog has no Radix trigger, so focus goes back to the opener here. */}
      <AlertDialogContent onOpenAutoFocus={returnFocus.remember} onCloseAutoFocus={returnFocus.restore}>
        <AlertDialogHeader className="text-left">
          <AlertDialogTitle className="[overflow-wrap:anywhere]">{title}</AlertDialogTitle>
          <AlertDialogDescription className="[overflow-wrap:anywhere]">{description}</AlertDialogDescription>
        </AlertDialogHeader>
        {children}
        <AlertDialogFooter>
          <AlertDialogCancel className="max-sm:h-11" disabled={pending}>{cancelLabel ?? t("actions.cancel")}</AlertDialogCancel>
          <Button variant={reversible ? "default" : "destructive"} className="max-sm:h-11" disabled={pending || confirmDisabled} onClick={() => void confirm()}>
            {pending && <Loader2 className="animate-spin" />}
            {confirmLabel}
          </Button>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
