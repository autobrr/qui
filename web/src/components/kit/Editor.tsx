/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Loader2 } from "lucide-react"
import type { FormEvent, ReactNode, RefObject } from "react"
import { useTranslation } from "react-i18next"

import { useReturnFocus } from "@/components/kit/use-return-focus"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@/components/ui/sheet"
import { useIsPhone } from "@/hooks/useMediaQuery"
import { cn } from "@/lib/utils"

type EditorSubmit =
  | {
    submitLabel: string
    onSubmit: () => void
  }
  | { submitLabel?: never; onSubmit?: never }

type EditorProps = EditorSubmit & {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  pending?: boolean
  submitDisabled?: boolean
  error?: ReactNode
  secondaryActions?: ReactNode
  size?: "default" | "wide"
  returnFocusTo?: RefObject<HTMLElement | null>
  initialFocus?: string
  pinned?: ReactNode
  children: ReactNode
}

export function Editor({ open, onOpenChange, title, description, submitLabel, onSubmit, pending = false, submitDisabled = false, error, secondaryActions, size = "default", returnFocusTo, initialFocus, pinned, children }: EditorProps) {
  const { t } = useTranslation("common")
  const phone = useIsPhone()
  const returnFocus = useReturnFocus(returnFocusTo)
  const handleOpenChange = (next: boolean) => {
    if (!pending) onOpenChange(next)
  }

  const handleSubmit = (event: FormEvent) => {
    event.preventDefault()
    if (!pending && !submitDisabled) onSubmit?.()
  }

  const handleOpenAutoFocus = (event: Event) => {
    event.preventDefault()
    returnFocus.remember()
    const content = event.currentTarget as HTMLElement
    const touch = window.matchMedia?.("(pointer: coarse)").matches ?? false
    const field = phone || touch ? null : content.querySelector<HTMLElement>(initialFocus ?? "input:not([type=hidden]):not(:disabled), textarea:not(:disabled), [role=combobox]:not(:disabled)")
    field?.focus()
    if (!content.contains(document.activeElement)) content.focus()
  }

  const contentProps = {
    ...(description ? {} : { "aria-describedby": undefined }),
    onOpenAutoFocus: handleOpenAutoFocus,
    onCloseAutoFocus: returnFocus.restore,
  }

  const body = (
    <>
      <div className={cn("min-h-0 flex-1 space-y-5 overflow-y-auto pt-1 pb-5", phone ? "px-4" : "px-6")}>{children}</div>
      {error && (
        <p role="alert" className={cn("shrink-0 border-t border-destructive/30 bg-destructive/5 py-2.5 text-sm text-destructive [overflow-wrap:anywhere]", phone ? "px-4" : "px-6")}>
          {error}
        </p>
      )}
      <div className={cn("flex shrink-0 gap-2 border-t pt-3", phone ? cn("px-4 pb-[max(0.75rem,env(safe-area-inset-bottom))]", onSubmit ? "flex-col" : "flex-wrap *:grow") : "items-center px-6 pb-4")}>
        {secondaryActions && <div className={cn("flex gap-2", phone && "flex-wrap *:h-11 *:flex-1")}>{secondaryActions}</div>}
        <div className={cn("flex gap-2", phone ? "*:h-11 *:flex-1" : "ml-auto")}>
          <Button type="button" variant="outline" disabled={pending} onClick={() => onOpenChange(false)}>
            {onSubmit ? t("actions.cancel") : t("actions.close")}
          </Button>
          {onSubmit && (
            <Button type="submit" disabled={pending || submitDisabled}>
              {pending && <Loader2 className="animate-spin" />}
              {submitLabel}
            </Button>
          )}
        </div>
      </div>
    </>
  )
  const form = (
    <>
      {pinned && <div className={cn("shrink-0 pb-2", phone ? "px-4" : "px-6")}>{pinned}</div>}
      {onSubmit
        ? <form onSubmit={handleSubmit} className="flex min-h-0 flex-1 flex-col">{body}</form>
        : <div className="flex min-h-0 flex-1 flex-col">{body}</div>}
    </>
  )

  if (phone) {
    return (
      <Sheet open={open} onOpenChange={handleOpenChange}>
        <SheetContent side="bottom" className="max-h-[92dvh] gap-0 rounded-t-xl" {...contentProps}>
          <SheetHeader className="px-4 pt-4 pr-12 pb-3 text-left">
            <SheetTitle>{title}</SheetTitle>
            {description && <SheetDescription>{description}</SheetDescription>}
          </SheetHeader>
          {form}
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className={cn("flex max-h-[85dvh] flex-col gap-0 p-0", size === "wide" ? "sm:max-w-3xl" : "sm:max-w-xl")} {...contentProps}>
        <DialogHeader className="px-6 pt-6 pr-12 pb-4 text-left">
          <DialogTitle>{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        {form}
      </DialogContent>
    </Dialog>
  )
}
