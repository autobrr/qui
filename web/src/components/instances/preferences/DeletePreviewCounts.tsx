/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useTranslation } from "react-i18next"

import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import type { AutomationPreviewResult } from "@/types"

interface DeletePreviewCountsProps {
  preview: AutomationPreviewResult
  enabled: boolean
  name?: string
}

export function DeletePreviewCounts({ preview, enabled, name }: DeletePreviewCountsProps) {
  const { t } = useTranslation("instances")
  const waiting = preview.waitingCount ?? 0

  return (
    <>
      {preview.totalMatches > 0 && (enabled ? (
        <p className="text-destructive font-medium">
          {t("preferences.workflowDialog.preview.deleteEnabledSummary", { name, count: preview.totalMatches })}
        </p>
      ) : (
        <p className="text-muted-foreground">
          {t("preferences.workflowDialog.preview.deleteDisabledSummary", { count: preview.totalMatches })}
        </p>
      ))}
      {waiting > 0 && (
        <p className="text-muted-foreground">
          {t("preferences.workflowDialog.preview.waitingSummary", { count: waiting })}
        </p>
      )}
    </>
  )
}

export function DeleteWhenCell({ waitsUntil }: { waitsUntil?: number }) {
  const { t } = useTranslation("instances")
  const { formatTimestamp } = useDateTimeFormatters()

  if (!waitsUntil) {
    return <span className="text-xs text-destructive whitespace-nowrap">{t("preferences.workflowPreview.deletesNow")}</span>
  }
  return (
    <span className="text-xs text-muted-foreground whitespace-nowrap">
      {waitsUntil * 1000 <= Date.now() ? t("preferences.workflowPreview.waitsForTracker") : t("preferences.workflowPreview.waitsUntil", { time: formatTimestamp(waitsUntil) })}
    </span>
  )
}
