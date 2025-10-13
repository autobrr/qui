/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Switch } from "@/components/ui/switch"
import { api } from "@/lib/api"
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useId, useMemo } from "react"
import { toast } from "sonner"

export function TrackerIconSettingsCard() {
  const labelId = useId()
  const queryClient = useQueryClient()

  const { data, isLoading, isError } = useQuery({
    queryKey: ["trackerIconSettings"],
    queryFn: () => api.getTrackerIconSettings(),
    staleTime: 60_000,
  })

  const mutation = useMutation({
    mutationFn: (fetchEnabled: boolean) => api.updateTrackerIconSettings(fetchEnabled),
    onSuccess: (updated) => {
      queryClient.setQueryData(["trackerIconSettings"], updated)
      toast.success(updated.fetchEnabled ? "Automatic tracker icon fetching enabled" : "Automatic tracker icon fetching disabled")
    },
    onError: () => {
      queryClient.invalidateQueries({ queryKey: ["trackerIconSettings"] })
      toast.error("Failed to update tracker icon fetching preference")
    },
  })

  const fetchEnabled = useMemo(() => data?.fetchEnabled ?? false, [data])
  const disabled = isLoading || mutation.isPending

  return (
    <Card>
      <CardHeader>
        <CardTitle>Tracker Icons</CardTitle>
        <CardDescription>
          Control whether qui fetches favicons from tracker websites automatically.
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <p className="text-sm font-medium" id={labelId}>
              Fetch missing tracker icons
            </p>
            <p className="text-sm text-muted-foreground">
              When enabled, qui downloads tracker favicons on demand and caches them in <code>tracker-icons</code> under the data directory. Disable to rely solely on icons you place there manually.
            </p>
          </div>
          <Switch
            aria-labelledby={labelId}
            checked={fetchEnabled}
            disabled={disabled}
            onCheckedChange={(next) => {
              if (disabled || next === fetchEnabled) {
                return
              }
              mutation.mutate(next)
            }}
          />
        </div>
        {isError && (
          <p className="text-sm text-destructive">
            Unable to load tracker icon settings. Please refresh the page.
          </p>
        )}
      </CardContent>
    </Card>
  )
}
