/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { api } from "@/lib/api"
import { cn } from "@/lib/utils"
import { useMutation, useQuery } from "@tanstack/react-query"
import { Download, Loader2, X } from "lucide-react"
import { useState } from "react"
import { toast } from "sonner"

export function UpdateBanner() {
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

  const updateMutation = useMutation({
    mutationFn: () => api.triggerSelfUpdate(),
    onSuccess: ({ message }) => {
      toast.success(message)
      setDismissed(true)
    },
    onError: (error: unknown) => {
      const message = error instanceof Error ? error.message : "Failed to start self-update"
      toast.error(message)
    },
  })

  // Don't show banner if dismissed or no update available
  if (dismissed || !updateInfo) {
    return null
  }

  const handleViewUpdate = () => {
    window.open(updateInfo.html_url, "_blank", "noopener,noreferrer")
  }

  const handleSelfUpdate = () => {
    updateMutation.mutate()
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
            Update Available
          </p>
          <p className="text-xs text-green-700 dark:text-green-300 mt-1">
            Version {updateInfo.tag_name} is now available
          </p>
          <div className="mt-2 flex flex-wrap gap-2">
            {updateInfo.self_update_supported && (
              <Button
                size="sm"
                className="h-6 text-xs"
                onClick={handleSelfUpdate}
                disabled={updateMutation.isPending}
              >
                {updateMutation.isPending ? (
                  <span className="flex items-center gap-1">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    Updating...
                  </span>
                ) : (
                  "Update & Restart"
                )}
              </Button>
            )}
            <Button
              size="sm"
              variant="outline"
              className="h-6 text-xs border-green-300 text-green-700 hover:bg-green-100 dark:border-green-700 dark:text-green-300 dark:hover:bg-green-900"
              onClick={handleViewUpdate}
            >
              View Release
            </Button>
          </div>
        </div>
        <Button
          size="icon"
          variant="ghost"
          className="h-4 w-4 text-green-600 hover:text-green-800 dark:text-green-400 dark:hover:text-green-200"
          onClick={handleDismiss}
        >
          <X className="h-3 w-3" />
          <span className="sr-only">Dismiss</span>
        </Button>
      </div>
    </div>
  )
}