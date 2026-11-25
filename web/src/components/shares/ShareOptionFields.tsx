/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Field } from "@/components/kit/Field"
import { SwitchRow } from "@/components/kit/SwitchRow"
import { Input } from "@/components/ui/input"
import { useTranslation } from "react-i18next"

interface ShareOptionFieldsProps {
  days: string
  daysError?: string
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
  lockUnavailable?: boolean
  disabled?: boolean
  onChange: (change: { days?: string; endAtFirstDownload?: boolean; firstDownloaderOnly?: boolean }) => void
}

export function ShareOptionFields({ days, daysError, endAtFirstDownload, firstDownloaderOnly, lockUnavailable, disabled, onChange }: ShareOptionFieldsProps) {
  const { t } = useTranslation("shares")
  return (
    <div className="space-y-4">
      <Field label={t("options.days")} error={daysError}>
        <Input inputMode="numeric" className="w-full sm:w-32" value={days} disabled={disabled} onChange={(e) => onChange({ days: e.target.value })} />
      </Field>
      <SwitchRow
        label={t("options.endAtFirstDownload")}
        help={t("options.endAtFirstDownloadDescription")}
        checked={endAtFirstDownload}
        disabled={disabled}
        onCheckedChange={(checked) => onChange({ endAtFirstDownload: checked })}
      />
      <SwitchRow
        label={t("options.firstDownloaderOnly")}
        description={lockUnavailable ? t("options.firstDownloaderOnlyUnavailable") : t("options.firstDownloaderOnlyDescription")}
        checked={firstDownloaderOnly && !lockUnavailable}
        disabled={disabled || lockUnavailable}
        onCheckedChange={(checked) => onChange({ firstDownloaderOnly: checked })}
      />
    </div>
  )
}
