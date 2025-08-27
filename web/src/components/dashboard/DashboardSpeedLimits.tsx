/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Download, Upload } from "lucide-react"
import { formatSpeed } from "@/lib/utils"
import { useInstancePreferences } from "@/hooks/useInstancePreferences"

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
  const { preferences } = useInstancePreferences(instanceId)

  const formatLimit = (limit: number) => limit === 0 ? "Unlimited" : formatSpeed(limit * 1024) // API returns KB/s, formatSpeed expects B/s

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          Speed Limits
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="grid grid-cols-2 gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Download className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">Download</span>
            </div>
            <p className="text-lg font-semibold">
              {formatLimit(preferences?.dl_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              Current: {formatSpeed(currentDownloadSpeed)}
            </p>
          </div>
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Upload className="h-4 w-4 text-muted-foreground" />
              <span className="text-sm text-muted-foreground">Upload</span>
            </div>
            <p className="text-lg font-semibold">
              {formatLimit(preferences?.up_limit || 0)}
            </p>
            <p className="text-xs text-muted-foreground">
              Current: {formatSpeed(currentUploadSpeed)}
            </p>
          </div>
        </div>
      </CardContent>
    </Card>
  )
}