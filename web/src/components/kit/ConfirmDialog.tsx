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
  confirmLabel: string
  cancelLabel?: string
  onConfirm: () => void | Promise<unknown>
  reversible?: boolean
  confirmDisabled?: boolean
  children?: ReactNode
}

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
      return
    } finally {
      setPending(false)
    }
  }

  return (
    <AlertDialog open={open} onOpenChange={(next) => !pending && onOpenChange(next)}>
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
