/*
 * Copyright (c) 2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useId, type ReactNode } from "react"

import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

interface SwitchRowProps {
  label: ReactNode
  /** Field help, in a tooltip after the label. */
  help?: ReactNode
  /** Text that stays under the label, for a state or a warning. */
  description?: ReactNode
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  disabled?: boolean
}

/** One on/off setting: the label and its help at the left, the switch at the right. */
export function SwitchRow({ label, help, description, checked, onCheckedChange, disabled = false }: SwitchRowProps) {
  const id = useId()
  const descriptionId = useId()

  return (
    <div className="flex items-start gap-4">
      <div className="min-w-0 flex-1">
        <div className="flex min-h-5 items-center gap-1.5">
          <Label htmlFor={id} className="cursor-pointer leading-5">{label}</Label>
          {help && <FieldHelp>{help}</FieldHelp>}
        </div>
        {description && (
          <p id={descriptionId} className="mt-0.5 text-xs text-muted-foreground [overflow-wrap:anywhere]">{description}</p>
        )}
      </div>
      {/* On a touch screen, the pseudo-element gives the switch a 44 px tap area. */}
      <Switch
        id={id}
        aria-describedby={description ? descriptionId : undefined}
        checked={checked}
        disabled={disabled}
        onCheckedChange={onCheckedChange}
        className="relative mt-0.5 pointer-coarse:after:absolute pointer-coarse:after:-inset-3"
      />
    </div>
  )
}
