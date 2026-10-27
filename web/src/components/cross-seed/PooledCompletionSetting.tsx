/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Checkbox } from "@/components/ui/checkbox"
import { FieldHelp } from "@/components/ui/field-help"
import { Label } from "@/components/ui/label"
import { useTranslation } from "react-i18next"

export function PooledCompletionSetting({
  id,
  checked,
  onCheckedChange,
}: {
  id: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  const { t } = useTranslation("crossseed")

  return (
    <div className="flex items-center gap-3">
      <Checkbox
        id={id}
        checked={checked}
        onCheckedChange={value => onCheckedChange(value === true)}
      />
      <div className="flex items-center gap-1.5">
        <Label htmlFor={id} className="font-medium cursor-pointer">
          {t("rules.postInjection.pooledPartialCompletion")}
        </Label>
        <FieldHelp>{t("rules.postInjection.pooledPartialCompletionDescription")}</FieldHelp>
      </div>
    </div>
  )
}
