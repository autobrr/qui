/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useOrphanScanSettings, useUpdateOrphanScanSettings } from "@/hooks/useOrphanScan"
import type { OrphanScanSettings, OrphanScanSettingsUpdate } from "@/types"
import { AlertTriangle, Info, Loader2 } from "lucide-react"
import { useEffect, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
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
  previewSort: "size_desc",
  maxFilesPerRun: 100,
  ignorePaths: [],
  autoCleanupEnabled: false,
  autoCleanupMaxFiles: 100,
}

export function OrphanScanSettingsForm({
  instanceId,
  onSuccess,
  formId,
}: OrphanScanSettingsFormProps) {
  const { t } = useTranslation("instances")
  const settingsQuery = useOrphanScanSettings(instanceId)
  const updateMutation = useUpdateOrphanScanSettings(instanceId)

  const [settings, setSettings] = useState<typeof DEFAULT_SETTINGS>(() => ({ ...DEFAULT_SETTINGS }))
  const [ignorePathsText, setIgnorePathsText] = useState("")

  // Track acknowledgment for enabling auto-cleanup
  const [autoCleanupAcknowledged, setAutoCleanupAcknowledged] = useState(false)

  // Track whether auto-cleanup was already enabled when form loaded
  const [initialAutoCleanupEnabled, setInitialAutoCleanupEnabled] = useState(false)

  // Reset settings when query data changes
  useEffect(() => {
    if (settingsQuery.data) {
      setSettings({
        enabled: settingsQuery.data.enabled,
        gracePeriodMinutes: settingsQuery.data.gracePeriodMinutes,
        scanIntervalHours: settingsQuery.data.scanIntervalHours,
        previewSort: settingsQuery.data.previewSort ?? "size_desc",
        maxFilesPerRun: settingsQuery.data.maxFilesPerRun,
        ignorePaths: [...settingsQuery.data.ignorePaths],
        autoCleanupEnabled: settingsQuery.data.autoCleanupEnabled,
        autoCleanupMaxFiles: settingsQuery.data.autoCleanupMaxFiles,
      })
      setIgnorePathsText(settingsQuery.data.ignorePaths.join("\n"))
      setInitialAutoCleanupEnabled(settingsQuery.data.autoCleanupEnabled)
      // If auto-cleanup is already enabled, user doesn't need to re-acknowledge
      setAutoCleanupAcknowledged(settingsQuery.data.autoCleanupEnabled)
    }
  }, [settingsQuery.data])

  // Reset acknowledgment when user enables auto-cleanup (if it wasn't initially enabled)
  const handleAutoCleanupToggle = (checked: boolean) => {
    setSettings(prev => ({ ...prev, autoCleanupEnabled: checked }))
    // Only require acknowledgment if enabling and it wasn't initially enabled
    if (checked && !initialAutoCleanupEnabled) {
      setAutoCleanupAcknowledged(false)
    }
  }

  // Check if we need acknowledgment for saving
  const needsAutoCleanupAcknowledgment = settings.autoCleanupEnabled && !initialAutoCleanupEnabled && !autoCleanupAcknowledged

  const persistSettings = (nextSettings: typeof DEFAULT_SETTINGS, successMessage?: string) => {
    const payload: OrphanScanSettingsUpdate = {
      enabled: nextSettings.enabled,
      gracePeriodMinutes: Math.max(1, nextSettings.gracePeriodMinutes),
      scanIntervalHours: Math.max(1, nextSettings.scanIntervalHours),
      previewSort: nextSettings.previewSort,
      maxFilesPerRun: Math.max(1, Math.min(1000, nextSettings.maxFilesPerRun)),
      ignorePaths: nextSettings.ignorePaths.map(p => p.trim()).filter(Boolean),
      autoCleanupEnabled: nextSettings.autoCleanupEnabled,
      autoCleanupMaxFiles: Math.max(1, nextSettings.autoCleanupMaxFiles),
    }

    updateMutation.mutate(payload, {
      onSuccess: () => {
        toast.success(t("preferences.orphanScanSettings.toast.settingsSaved"), { description: successMessage })
        onSuccess?.()
      },
      onError: (error) => {
        toast.error(t("preferences.orphanScanSettings.toast.updateFailed"), {
          description: error instanceof Error ? error.message : t("preferences.orphanScanSettings.toast.updateFailedDescription"),
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
    setSettings(prev => ({ ...prev, enabled }))
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
        <p className="text-sm text-destructive">{t("preferences.orphanScanSettings.failedToLoadSettings")}</p>
        <Button variant="outline" size="sm" onClick={() => settingsQuery.refetch()}>
          {t("preferences.orphanScanSettings.retry")}
        </Button>
      </div>
    )
  }

  const headerContent = (
    <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-4">
      <div className="space-y-1">
        <div className="flex items-center gap-2">
          <h3 className="text-base font-medium">{t("preferences.orphanScanSettings.settingsTitle")}</h3>
        </div>
      </div>
      <div className="flex items-center gap-2 bg-muted/50 p-2 rounded-lg border shrink-0">
        <Label htmlFor="orphan-scan-enabled" className="font-medium text-sm cursor-pointer">
          {settings.enabled ? t("preferences.orphanScanSettings.enabled") : t("preferences.orphanScanSettings.disabled")}
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
      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">{t("preferences.orphanScanSettings.schedule")}</h3>
          <Separator className="flex-1" />
        </div>

        <div className="grid gap-6 sm:grid-cols-3">
          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Label htmlFor="scan-interval" className="text-sm font-medium">{t("preferences.orphanScanSettings.scanInterval")}</Label>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-[250px]">
                  <p>{t("preferences.orphanScanSettings.scanIntervalTooltip")}</p>
                </TooltipContent>
              </Tooltip>
            </div>
            <Select
              value={String(settings.scanIntervalHours)}
              onValueChange={(value) => {
                if (!value) return // Ignore empty values from Radix Select quirk
                setSettings(prev => ({ ...prev, scanIntervalHours: Number(value) }))
              }}
            >
              <SelectTrigger id="scan-interval" className="h-9">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="1">{t("preferences.orphanScanSettings.everyHour")}</SelectItem>
                <SelectItem value="2">{t("preferences.orphanScanSettings.every2Hours")}</SelectItem>
                <SelectItem value="6">{t("preferences.orphanScanSettings.every6Hours")}</SelectItem>
                <SelectItem value="12">{t("preferences.orphanScanSettings.every12Hours")}</SelectItem>
                <SelectItem value="24">{t("preferences.orphanScanSettings.every24Hours")}</SelectItem>
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Label htmlFor="grace-period" className="text-sm font-medium">{t("preferences.orphanScanSettings.gracePeriod")}</Label>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-[250px]">
                  <p>{t("preferences.orphanScanSettings.gracePeriodTooltip")}</p>
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
              <span className="text-sm text-muted-foreground shrink-0">{t("preferences.orphanScanSettings.minutes")}</span>
            </div>
          </div>

          <div className="space-y-2">
            <div className="flex items-center gap-2">
              <Label htmlFor="max-files" className="text-sm font-medium">{t("preferences.orphanScanSettings.maxFiles")}</Label>
              <Tooltip>
                <TooltipTrigger asChild>
                  <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                </TooltipTrigger>
                <TooltipContent className="max-w-[250px]">
                  <p>{t("preferences.orphanScanSettings.maxFilesTooltip")}</p>
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
              <span className="text-sm text-muted-foreground shrink-0">{t("preferences.orphanScanSettings.perRun")}</span>
            </div>
          </div>
        </div>

        <div className="space-y-2 sm:max-w-sm">
          <div className="flex items-center gap-2">
            <Label htmlFor="preview-sort" className="text-sm font-medium">{t("preferences.orphanScanSettings.previewSort")}</Label>
            <Tooltip>
              <TooltipTrigger asChild>
                <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
              </TooltipTrigger>
              <TooltipContent className="max-w-[300px]">
                <p>{t("preferences.orphanScanSettings.previewSortTooltip")}</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Select
            value={settings.previewSort}
            onValueChange={(value) => {
              if (!value) return
              setSettings(prev => ({ ...prev, previewSort: value as typeof settings.previewSort }))
            }}
          >
            <SelectTrigger id="preview-sort" className="h-9">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="size_desc">{t("preferences.orphanScanSettings.sizeLargestFirst")}</SelectItem>
              <SelectItem value="directory_size_desc">{t("preferences.orphanScanSettings.directorySizeThenSize")}</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">{t("preferences.orphanScanSettings.autoCleanup")}</h3>
          <Separator className="flex-1" />
        </div>

        <div className="space-y-4">
          <div className="flex items-center justify-between p-4 bg-muted/40 rounded-lg border">
            <div className="space-y-0.5">
              <div className="flex items-center gap-2">
                <Label htmlFor="auto-cleanup-enabled" className="text-sm font-medium cursor-pointer">
                  {t("preferences.orphanScanSettings.autoCleanupLabel")}
                </Label>
                <Tooltip>
                  <TooltipTrigger asChild>
                    <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-[300px]">
                    <p>{t("preferences.orphanScanSettings.autoCleanupTooltip")}</p>
                  </TooltipContent>
                </Tooltip>
              </div>
              <p className="text-xs text-muted-foreground">
                {t("preferences.orphanScanSettings.autoCleanupDescription")}
              </p>
            </div>
            <Switch
              id="auto-cleanup-enabled"
              checked={settings.autoCleanupEnabled}
              onCheckedChange={handleAutoCleanupToggle}
            />
          </div>

          {settings.autoCleanupEnabled && (
            <div className="space-y-4 pl-3 border-l-2 border-muted">
              {/* Warning banner when enabling auto-cleanup */}
              {!initialAutoCleanupEnabled && (
                <div className="rounded-lg border border-yellow-500/50 bg-yellow-500/5 p-4 space-y-3">
                  <div className="flex items-start gap-3">
                    <AlertTriangle className="h-5 w-5 text-yellow-500 shrink-0 mt-0.5" />
                    <div className="space-y-2">
                      <p className="text-sm font-medium text-foreground">
                        {t("preferences.orphanScanSettings.autoDeleteWarningTitle")}
                      </p>
                      <p className="text-sm text-muted-foreground">
                        <Trans
                          ns="instances"
                          i18nKey="preferences.orphanScanSettings.autoDeleteWarningBody"
                          components={{ span: <span className="font-medium" /> }}
                        />
                      </p>
                      <p className="text-sm text-muted-foreground">
                        <Trans
                          ns="instances"
                          i18nKey="preferences.orphanScanSettings.autoDeleteWarningPaths"
                          components={{ span: <span className="font-medium" /> }}
                        />
                      </p>
                    </div>
                  </div>
                  <div className="flex items-start gap-2 pl-8">
                    <Checkbox
                      id="auto-cleanup-acknowledged"
                      checked={autoCleanupAcknowledged}
                      onCheckedChange={(checked) => setAutoCleanupAcknowledged(checked === true)}
                    />
                    <Label
                      htmlFor="auto-cleanup-acknowledged"
                      className="text-sm text-muted-foreground cursor-pointer leading-tight"
                    >
                      {t("preferences.orphanScanSettings.autoDeleteAcknowledge")}
                    </Label>
                  </div>
                </div>
              )}

              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Label htmlFor="auto-cleanup-max-files" className="text-sm font-medium">{t("preferences.orphanScanSettings.maxFilesThreshold")}</Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-[300px]">
                      <p>{t("preferences.orphanScanSettings.maxFilesThresholdTooltip")}</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
                <div className="flex items-center gap-2">
                  <Input
                    id="auto-cleanup-max-files"
                    type="number"
                    min={1}
                    value={settings.autoCleanupMaxFiles}
                    onChange={(e) => setSettings(prev => ({ ...prev, autoCleanupMaxFiles: Number(e.target.value) || 1 }))}
                    className="h-9 w-24"
                  />
                  <span className="text-sm text-muted-foreground">{t("preferences.orphanScanSettings.files")}</span>
                </div>
                <p className="text-xs text-muted-foreground">
                  {t("preferences.orphanScanSettings.maxFilesThresholdDescription")}
                </p>
              </div>
            </div>
          )}
        </div>
      </div>

      <div className="space-y-4">
        <div className="flex items-center gap-2">
          <h3 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">{t("preferences.orphanScanSettings.exclusions")}</h3>
          <Separator className="flex-1" />
        </div>

        <div className="space-y-2">
          <div className="flex items-center gap-2">
            <Label htmlFor="ignore-paths" className="text-sm font-medium">{t("preferences.orphanScanSettings.ignorePaths")}</Label>
            <Tooltip>
              <TooltipTrigger asChild>
                <Info className="h-3.5 w-3.5 text-muted-foreground/70 cursor-help" />
              </TooltipTrigger>
              <TooltipContent className="max-w-[300px]">
                <p>{t("preferences.orphanScanSettings.ignorePathsTooltip")}</p>
              </TooltipContent>
            </Tooltip>
          </div>
          <Textarea
            id="ignore-paths"
            value={ignorePathsText}
            onChange={(e) => setIgnorePathsText(e.target.value)}
            placeholder={t("preferences.orphanScanSettings.ignorePathsPlaceholder")}
            rows={4}
            className="font-mono text-sm"
          />
          <p className="text-xs text-muted-foreground">
            {t("preferences.orphanScanSettings.ignorePathsDescription")}
          </p>
        </div>
      </div>

      {!formId && (
        <div className="flex justify-end pt-4">
          <Button type="submit" disabled={updateMutation.isPending || needsAutoCleanupAcknowledgment}>
            {updateMutation.isPending ? t("preferences.orphanScanSettings.saving") : t("preferences.orphanScanSettings.saveChanges")}
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
