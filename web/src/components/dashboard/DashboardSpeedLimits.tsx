/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Download, Upload } from "lucide-react"
import { useSpeedUnits, formatSpeedWithUnit } from "@/lib/speedUnits"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"
import { useTranslation } from "react-i18next"

interface DashboardSpeedLimitsProps {
  instanceId: number
  currentDownloadSpeed: number
  currentUploadSpeed: number
}

export function DashboardSpeedLimits({
  instanceId,
  currentDownloadSpeed,
  currentUploadSpeed,
}: DashboardSpeedLimitsProps) {
  const { t } = useTranslation()
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { preferences } = useInstancePreferences(instanceId)
  const [speedUnit] = useSpeedUnits()

  const formatLimit = (limit: number) => {
    if (limit === 0) {
      return tr("dashboardSpeedLimits.values.unlimited")
    }
    // API returns KB/s, formatSpeedWithUnit expects B/s.
    return formatSpeedWithUnit(limit * 1024, speedUnit)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          {tr("dashboardSpeedLimits.title")}
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Download className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">{tr("dashboardSpeedLimits.labels.download")}</span>
            </div>
            <p className="text-lg font-semibold">
              {formatLimit(preferences?.dl_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              {tr("dashboardSpeedLimits.labels.current")}: {formatSpeedWithUnit(currentDownloadSpeed, speedUnit)}
            </p>
          </div>
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Upload className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">{tr("dashboardSpeedLimits.labels.upload")}</span>
            </div>
            <p className="text-lg font-semibold">
              {formatLimit(preferences?.up_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              {tr("dashboardSpeedLimits.labels.current")}: {formatSpeedWithUnit(currentUploadSpeed, speedUnit)}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
