/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { useId } from "react"
import { useTranslation } from "react-i18next"

/** The tick box of an instance removal that also deletes the transfer history of the instance. */
export function DeleteHistoryCheckbox({ checked, onCheckedChange, disabled }: { checked: boolean; onCheckedChange: (checked: boolean) => void; disabled?: boolean }) {
  const { t } = useTranslation("instances")
  const id = useId()
  return (
    <div className="flex items-start gap-2">
      <Checkbox id={id} checked={checked} disabled={disabled} onCheckedChange={value => onCheckedChange(value === true)} className="mt-0.5" />
      <div className="space-y-1">
        <Label htmlFor={id}>{t("deleteHistory.label")}</Label>
        <p className="text-sm text-muted-foreground">{t("deleteHistory.description")}</p>
      </div>
    </div>
  )
}
