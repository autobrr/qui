/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { FieldHelp } from "@/components/ui/field-help"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { useTranslation } from "react-i18next"

interface ShareOptionFieldsProps {
  idPrefix: string
  days: string
  endAtFirstDownload: boolean
  firstDownloaderOnly: boolean
  /** Shows that the lock is not available, as with an outside tracker. */
  lockUnavailable?: boolean
  disabled?: boolean
  onChange: (change: { days?: string; endAtFirstDownload?: boolean; firstDownloaderOnly?: boolean }) => void
}

/** The three options of a Share, in the Share dialog and in the defaults of Settings → Shares. */
export function ShareOptionFields({ idPrefix, days, endAtFirstDownload, firstDownloaderOnly, lockUnavailable, disabled, onChange }: ShareOptionFieldsProps) {
  const { t } = useTranslation("shares")
  return (
    <div className="space-y-4">
      <div className="space-y-1.5">
        <Label htmlFor={`${idPrefix}-days`}>{t("options.days")}</Label>
        <Input
          id={`${idPrefix}-days`}
          inputMode="numeric"
          className="w-full sm:w-32"
          value={days}
          disabled={disabled}
          onChange={(e) => onChange({ days: e.target.value })}
        />
      </div>
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-1.5">
          <Label htmlFor={`${idPrefix}-end-at-first-download`}>{t("options.endAtFirstDownload")}</Label>
          <FieldHelp>{t("options.endAtFirstDownloadDescription")}</FieldHelp>
        </div>
        <Switch
          id={`${idPrefix}-end-at-first-download`}
          checked={endAtFirstDownload}
          disabled={disabled}
          onCheckedChange={(checked) => onChange({ endAtFirstDownload: checked })}
        />
      </div>
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-0.5">
          <Label htmlFor={`${idPrefix}-first-downloader-only`}>{t("options.firstDownloaderOnly")}</Label>
          <p className="text-sm text-muted-foreground">
            {lockUnavailable ? t("options.firstDownloaderOnlyUnavailable") : t("options.firstDownloaderOnlyDescription")}
          </p>
        </div>
        <Switch
          id={`${idPrefix}-first-downloader-only`}
          checked={firstDownloaderOnly && !lockUnavailable}
          disabled={disabled || lockUnavailable}
          onCheckedChange={(checked) => onChange({ firstDownloaderOnly: checked })}
        />
      </div>
    </div>
  )
}
