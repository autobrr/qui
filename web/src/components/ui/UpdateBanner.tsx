/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useQuery } from "@tanstack/react-query"
import { Download, X } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

export function UpdateBanner() {
  const { t } = useTranslation()
  const [dismissed, setDismissed] = useState(false)

  const { data: updateInfo } = useQuery({
    queryKey: ["latest-version"],
    queryFn: () => api.getLatestVersion(),
    // Check for updates every 2 minutes
    refetchInterval: 2 * 60 * 1000,
    // Don't show loading state on mount, run in background
    refetchOnMount: false,
    refetchOnWindowFocus: false,
  })

  // Don't show banner if dismissed or no update available
  if (dismissed || !updateInfo) {
    return null
  }

  const handleViewUpdate = () => {
    window.open(updateInfo.html_url, "_blank", "noopener,noreferrer")
  }

  const handleDismiss = () => {
    setDismissed(true)
  }

  return (
    <div className={cn(
      "mb-3 rounded-md border border-green-200 bg-green-50 p-3",
      "dark:border-green-800 dark:bg-green-950/50"
    )}>
      <div className="flex items-start gap-2">
        <Download className="h-4 w-4 text-green-600 dark:text-green-400 mt-0.5 flex-shrink-0" />
        <div className="flex-1 min-w-0">
          <p className="text-sm font-medium text-green-800 dark:text-green-200">
            {t("update.available")}
          </p>
          <p className="text-xs text-green-700 dark:text-green-300 mt-1">
            {t("update.version", { version: updateInfo.tag_name })}
          </p>
          <Button
            size="sm"
            variant="outline"
            className="mt-2 h-6 text-xs border-green-300 text-green-700 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900"
            onClick={handleViewUpdate}
          >
            {t("update.viewRelease")}
          </Button>
        </div>
        <Button
          size="icon"
          variant="ghost"
          className="h-4 w-4 text-green-600 hover:text-green-800 dark:text-green-400 dark:hover:text-green-200"
          onClick={handleDismiss}
        >
          <X className="h-3 w-3" />
          <span className="sr-only">{t("update.dismiss")}</span>
        </Button>
      </div>
    </div>
  )
}
