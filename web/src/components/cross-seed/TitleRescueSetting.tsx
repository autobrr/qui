/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useTranslation } from "react-i18next"

export function TitleRescueSetting({ checked, disabled = false, onCheckedChange }: { checked: boolean, disabled?: boolean, onCheckedChange: (checked: boolean) => void }) {
  const { t } = useTranslation("crossseed")

  return (
    <div className="flex items-center justify-between gap-3">
      <div className="space-y-0.5">
        <Label htmlFor="rescue-title-mismatches" className="font-medium">{t("rules.safety.rescueTitleMismatches")}</Label>
        <p className="text-xs text-muted-foreground">{t("rules.safety.rescueTitleMismatchesDescription")}</p>
      </div>
      <Switch id="rescue-title-mismatches" checked={checked} disabled={disabled} onCheckedChange={value => onCheckedChange(!!value)} />
    </div>
  )
}
