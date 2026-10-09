/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Info } from "lucide-react"
import { useTranslation } from "react-i18next"

export function OrphanScanRemoteLimits() {
  const { t } = useTranslation("instances")
  return (
    <div className="flex items-start gap-2 p-3 rounded-lg border bg-muted/40 text-xs text-muted-foreground">
      <Info className="h-4 w-4 shrink-0" />
      <p>{t("preferences.orphanScanOverview.remoteLimits")}</p>
    </div>
  )
}
