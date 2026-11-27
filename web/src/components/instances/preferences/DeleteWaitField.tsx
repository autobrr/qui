/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useState } from "react"
import { useTranslation } from "react-i18next"

import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { DEFAULT_DELETE_WAIT_SECONDS, MAX_DELETE_WAIT_SECONDS } from "@/lib/delete-wait"

const HOUR = 3600
const DAY = 24 * HOUR

function unitOf(seconds: number | null): number {
  if (seconds === null || seconds === 0 || seconds % DAY === 0) return DAY
  return HOUR
}

interface DeleteWaitFieldProps {
  seconds: number | null
  onChange: (seconds: number | null) => void
  locked: boolean
}

export function DeleteWaitField({ seconds, onChange, locked }: DeleteWaitFieldProps) {
  const { t } = useTranslation("instances")
  const [unit, setUnit] = useState(() => unitOf(seconds))

  const shown = seconds === null ? "" : String(Math.round((seconds / unit) * 100) / 100)
  const change = (text: string, nextUnit: number) => {
    setUnit(nextUnit)
    const amount = parseFloat(text)
    onChange(text === "" || Number.isNaN(amount) ? null : Math.min(Math.max(Math.round(amount * nextUnit), 0), MAX_DELETE_WAIT_SECONDS))
  }

  return (
    <div className="space-y-1">
      <div className="flex items-center gap-1.5">
        <Label htmlFor="delete-wait" size="sm">{t("preferences.workflowDialog.delete.wait")}</Label>
        <FieldHelp>{t("preferences.workflowDialog.delete.waitHelp")}</FieldHelp>
      </div>
      <div className="flex gap-2">
        <Input
          id="delete-wait"
          type="number"
          min={0}
          step="any"
          inputMode="decimal"
          value={shown}
          disabled={locked}
          placeholder={String(DEFAULT_DELETE_WAIT_SECONDS / unit)}
          onChange={(e) => change(e.target.value, unit)}
          className="h-8 w-24 text-xs"
        />
        <Select value={String(unit)} disabled={locked} onValueChange={(value) => change(shown, Number(value))}>
          <SelectTrigger className="h-8 w-24 text-xs" aria-label={t("preferences.workflowDialog.delete.waitUnit")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={String(HOUR)}>{t("automations:queryBuilder.durationUnits.hours")}</SelectItem>
            <SelectItem value={String(DAY)}>{t("automations:queryBuilder.durationUnits.days")}</SelectItem>
          </SelectContent>
        </Select>
      </div>
      {locked && <p className="text-xs text-muted-foreground">{t("preferences.workflowDialog.delete.waitLocked")}</p>}
    </div>
  )
}
