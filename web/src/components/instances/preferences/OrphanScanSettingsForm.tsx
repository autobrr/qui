/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useOrphanScanSettings, useUpdateOrphanScanSettings } from "@/hooks/useOrphanScan"
import type { OrphanScanSettings, OrphanScanSettingsUpdate } from "@/types"
import { Files, Info, Loader2 } from "lucide-react"
import { useEffect, useState } from "react"
import { toast } from "sonner"

interface OrphanScanSettingsFormProps {
  instanceId: number
  onSuccess?: () => void
  /** Form ID for external submit button. When provided, the internal submit button is hidden. */
  formId?: string
}

const DEFAULT_SETTINGS: Omit<OrphanScanSettings, "id" | "instanceId" | "createdAt" | "updatedAt"> = {
  enabled: false,
  gracePeriodMinutes: 30,
  scanIntervalHours: 6,
  maxFilesPerRun: 100,
  ignorePaths: [],
}

export function OrphanScanSettingsForm({
  instanceId,
  onSuccess,
  formId,
}: OrphanScanSettingsFormProps) {
  const settingsQuery = useOrphanScanSettings(instanceId)
  const updateMutation = useUpdateOrphanScanSettings(instanceId)

  const [settings, setSettings] = useState<typeof DEFAULT_SETTINGS>(() => ({ ...DEFAULT_SETTINGS }))
  const [ignorePathsText, setIgnorePathsText] = useState("")

  // Reset settings when query data changes
  useEffect(() => {
    if (settingsQuery.data) {
      setSettings({
        enabled: settingsQuery.data.enabled,
        gracePeriodMinutes: settingsQuery.data.gracePeriodMinutes,
        scanIntervalHours: settingsQuery.data.scanIntervalHours,
        maxFilesPerRun: settingsQuery.data.maxFilesPerRun,
        ignorePaths: [...settingsQuery.data.ignorePaths],
      })
      setIgnorePathsText(settingsQuery.data.ignorePaths.join("\n"))
    }
  }, [settingsQuery.data])

  const persistSettings = (nextSettings: typeof DEFAULT_SETTINGS, successMessage = "Settings saved") => {
    const payload: OrphanScanSettingsUpdate = {
      enabled: nextSettings.enabled,
      gracePeriodMinutes: Math.max(1, nextSettings.gracePeriodMinutes),
      scanIntervalHours: Math.max(1, nextSettings.scanIntervalHours),
      maxFilesPerRun: Math.max(1, Math.min(1000, nextSettings.maxFilesPerRun)),
      ignorePaths: nextSettings.ignorePaths.map(p => p.trim()).filter(Boolean),
    }

    updateMutation.mutate(payload, {
      onSuccess: () => {
        toast.success("Orphan scan updated", { description: successMessage })
        onSuccess?.()
      },
      onError: (error) => {
        toast.error("Update failed", {
          description: error instanceof Error ? error.message : "Unable to update settings",
        })
      },
    })
  }

  const handleSubmit = (event: React.FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const ignorePaths = ignorePathsText.split("\n").map(p => p.trim()).filter(Boolean)
    persistSettings({ ...settings, ignorePaths })
  }

  const handleToggleEnabled = (enabled: boolean) => {
    const nextSettings = { ...settings, enabled }
    setSettings(nextSettings)

    if (!enabled) {
      const ignorePaths = ignorePathsText.split("\n").map(p => p.trim()).filter(Boolean)
      persistSettings({ ...nextSettings, ignorePaths }, "Scheduled scanning disabled")
    }
  }

  if (settingsQuery.isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (settingsQuery.isError) {
    return (
      <div className="flex flex-col items-center justify-center py-12 text-center space-y-2">
        <p className="text-sm text-destructive">Failed to load settings</p>
        <Button variant="outline" size="sm" onClick={() => settingsQuery.refetch()}>
          Retry
        </Button>
      </div>
    )
  }

  const headerContent = (
    <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <h3 className="text-base font-medium">Settings</h3>
        </div>
      </div>
      <div className="flex items-center gap-2 bg-muted/50 p-2 rounded-lg border shrink-0">
        <Label htmlFor="orphan-scan-enabled" className="font-medium text-sm cursor-pointer">
          {settings.enabled ? "Enabled" : "Disabled"}
        </Label>
        <Switch
          id="orphan-scan-enabled"
          checked={settings.enabled}
          onCheckedChange={handleToggleEnabled}
          disabled={updateMutation.isPending}
        />
      </div>
    </div>
  )

  const settingsContent = (
    <div className="space-y-6">
      {settings.enabled ? (
        <>
          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Schedule</h3>
              <Separator className="flex-1" />
            </div>

            <div className="grid gap-6 sm:grid-cols-3">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="scan-interval" className="text-sm font-medium">Scan Interval</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-[250px]">
                      <p>How often to automatically scan for orphan files when scheduled scanning is enabled.</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                <Select
                  value={String(settings.scanIntervalHours)}
                  onValueChange={(value) => setSettings(prev => ({ ...prev, scanIntervalHours: Number(value) }))}
                >
                  <SelectTrigger id="scan-interval" className="h-9">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="1">Every hour</SelectItem>
                    <SelectItem value="2">Every 2 hours</SelectItem>
                    <SelectItem value="6">Every 6 hours</SelectItem>
                    <SelectItem value="12">Every 12 hours</SelectItem>
                    <SelectItem value="24">Every 24 hours</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="grace-period" className="text-sm font-medium">Grace Period</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-[250px]">
                      <p>Files modified within this time window will be skipped. Prevents deleting files that are still being written or processed.</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    id="grace-period"
                    type="number"
                    min={1}
                    value={settings.gracePeriodMinutes}
                    onChange={(e) => setSettings(prev => ({ ...prev, gracePeriodMinutes: Number(e.target.value) || 1 }))}
                    className="h-9"
                  />
                  <span className="text-sm text-muted-foreground shrink-0">minutes</span>
                </div>
              </div>

              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="max-files" className="text-sm font-medium">Max Files</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-[250px]">
                      <p>Safety limit on the maximum number of files to process per scan. Prevents accidentally deleting too many files at once.</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    id="max-files"
                    type="number"
                    min={1}
                    max={1000}
                    value={settings.maxFilesPerRun}
                    onChange={(e) => setSettings(prev => ({ ...prev, maxFilesPerRun: Number(e.target.value) || 1 }))}
                    className="h-9"
                  />
                  <span className="text-sm text-muted-foreground shrink-0">per run</span>
                </div>
              </div>
            </div>
          </div>

          <div className="space-y-4">
            <div className="flex items-center gap-2">
              <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">Exclusions</h3>
              <Separator className="flex-1" />
            </div>

            <div className="space-y-2">
              <div className="flex items-center gap-2">
                <Label htmlFor="ignore-paths" className="text-sm font-medium">Ignore Paths</Label>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-[300px]">
                    <p>Paths to exclude from scanning. Files in these directories will never be flagged as orphans. Enter one path per line.</p>
                  </TooltipContent>
                </Tooltip>
              </div>
              <Textarea
                id="ignore-paths"
                value={ignorePathsText}
                onChange={(e) => setIgnorePathsText(e.target.value)}
                placeholder="/downloads/preserve&#10;/downloads/manual&#10;/data/keep"
                rows={4}
                className="font-mono text-sm"
              />
              <p className="text-xs text-muted-foreground">
                One path per line. Paths are matched as prefixes.
              </p>
            </div>
          </div>

          {!formId && (
            <div className="flex justify-end pt-4">
              <Button type="submit" disabled={updateMutation.isPending}>
                {updateMutation.isPending ? "Saving..." : "Save Changes"}
              </Button>
            </div>
          )}
        </>
      ) : (
        <div className="flex flex-col items-center justify-center py-12 text-center space-y-3 border-2 border-dashed rounded-lg">
          <div className="p-3 rounded-full bg-muted/50">
            <Files className="h-6 w-6 text-muted-foreground/50" />
          </div>
          <div className="space-y-1">
            <h3 className="font-medium text-muted-foreground">Scheduled Scanning Disabled</h3>
            <p className="text-sm text-muted-foreground/60 max-w-xs mx-auto">
              Enable scheduled scanning to automatically find and clean up orphan files at regular intervals.
            </p>
          </div>
          <Button variant="outline" onClick={() => setSettings(prev => ({ ...prev, enabled: true }))}>
            Enable Scanning
          </Button>
        </div>
      )}
    </div>
  )

  return (
    <form id={formId} onSubmit={handleSubmit} className="space-y-6">
      {headerContent}
      {settingsContent}
    </form>
  )
}
