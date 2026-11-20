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

/** An editor without onSubmit has nothing to save: it shows an item to read, or saves each change at once. Its footer has Close. */
type EditorSubmit =
  | {
    /** The label of the button that saves, for example "Add target" or "Save". */
    submitLabel: string
    /** Runs on the submit button and on Enter in a text field. The editor stays open: close it when the save succeeds. */
    onSubmit: () => void
  }
  | { submitLabel?: never; onSubmit?: never }

type EditorProps = EditorSubmit & {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: ReactNode
  description?: ReactNode
  /** A save runs: the submit button shows a spinner, and the editor takes no second submit. */
  pending?: boolean
  submitDisabled?: boolean
  /** Why the last save failed, above the buttons. The fields stay as the user left them. */
  error?: ReactNode
  /**
   * Buttons at the start of the footer, for a step that does not save, for example Test. A phone wraps them onto more
   * lines, and puts them over Cancel and Save. Without onSubmit, Close shares their row when it fits.
   */
  secondaryActions?: ReactNode
  /** wide: for a form with fields side by side. A phone always uses the full width. */
  size?: "default" | "wide"
  /** Where focus goes on close, when the element that opened the editor can be gone by then. The opener by default. */
  returnFocusTo?: RefObject<HTMLElement | null>
  initialFocus?: string
  pinned?: ReactNode
  /** The fields. With onSubmit, the editor is the form, so the children hold no form element: call the form library in onSubmit. */
  children: ReactNode
}

/**
 * The form to add or edit one item. A dialog on a wide screen, a sheet from the bottom on a phone. The header and the
 * footer stay in place, and only the fields scroll. On a phone, no field takes focus on open, so the keyboard stays
 * down until the user taps a field.
 */
export function Editor({ open, onOpenChange, title, description, submitLabel, onSubmit, pending = false, submitDisabled = false, error, secondaryActions, size = "default", returnFocusTo, initialFocus, pinned, children }: EditorProps) {
  const { t } = useTranslation("common")
  const phone = useIsPhone()
  const returnFocus = useReturnFocus(returnFocusTo)
  // While a save runs, the editor stays open, so the result of the save has a place to show.
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
    // The first focusable element can be a FieldHelp, whose tooltip would open on each open. On a touch screen, a focused
    // field opens the keyboard over the form, so the editor itself takes the focus.
    const touch = window.matchMedia?.("(pointer: coarse)").matches ?? false
    const field = phone || touch ? null : content.querySelector<HTMLElement>(initialFocus ?? "input:not([type=hidden]):not(:disabled), textarea:not(:disabled), [role=combobox]:not(:disabled)")
    field?.focus()
    if (!content.contains(document.activeElement)) content.focus()
  }

  const contentProps = {
    // Radix warns about a dialog without a description, unless aria-describedby is set to undefined.
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
  // Without onSubmit, the children can hold their own form, and a form cannot hold another one.
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
