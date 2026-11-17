/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Slot } from "@radix-ui/react-slot"
import { useId, type ReactNode } from "react"

import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"

export interface FieldControlProps {
  id: string
  "aria-describedby"?: string
  "aria-invalid"?: true
}

interface FieldProps {
  label: ReactNode
  /** Field help, in a tooltip after the label. */
  help?: ReactNode
  /** Text under the control that the user reads before a choice, or a computed preview. */
  description?: ReactNode
  /** A validation message. It shows under the control, in place of the description. */
  error?: ReactNode
  /**
   * The control. An element gets the id and the descriptions of the field. A function gets them as props, for a
   * control whose root takes no id, such as a Select: pass them to its SelectTrigger.
   */
  children: ReactNode | ((control: FieldControlProps) => ReactNode)
}

/** One labelled control in a form or a section: the label above, the control, then the description or the error. */
export function Field({ label, help, description, error, children }: FieldProps) {
  const id = useId()
  const noteId = useId()
  const note = error ?? description
  const control: FieldControlProps = {
    id,
    "aria-describedby": note ? noteId : undefined,
    "aria-invalid": error ? true : undefined,
  }

  return (
    <div className="space-y-2">
      <div className="flex min-h-5 items-center gap-1.5">
        <Label htmlFor={id} className="leading-5">{label}</Label>
        {help && <FieldHelp>{help}</FieldHelp>}
      </div>
      {typeof children === "function" ? children(control) : <Slot {...control}>{children}</Slot>}
      {note && (
        <p id={noteId} className={error ? "text-xs text-destructive" : "text-xs text-muted-foreground"}>{note}</p>
      )}
    </div>
  )
}
