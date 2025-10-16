/*
 * Copyright (c) 2025, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { ArrowDownToLine, CircleHelp, CircleX, Clock, Download, FileText, ListChecks, RefreshCw, Trash, Undo2 } from "lucide-react"
import type { ChangeEvent } from "react"
import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"

import {
  Alert,
  AlertDescription,
  AlertTitle
} from "@/components/ui/alert"
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Progress } from "@/components/ui/progress"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import {
  useBackupManifest,
  useBackupRuns,
  useBackupSettings,
  useDeleteAllBackupRuns,
  useDeleteBackupRun,
  useExecuteRestore,
  usePreviewRestore,
  useTriggerBackup,
  useUpdateBackupSettings
} from "@/hooks/useInstanceBackups"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import { api } from "@/lib/api"
import type {
  BackupCategorySnapshot,
  BackupRun,
  BackupRunKind,
  BackupRunStatus,
  RestoreDiffChange,
  RestoreMode,
  RestorePlan,
  RestoreResult
} from "@/types"

interface InstanceBackupsProps {
  instanceId: number
}

type SettingsFormState = {
  enabled: boolean
  hourlyEnabled: boolean
  dailyEnabled: boolean
  weeklyEnabled: boolean
  monthlyEnabled: boolean
  keepLast: number
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  includeCategories: boolean
  includeTags: boolean
  customPath?: string | null
}

type SettingsToggleKey =
  | "enabled"
  | "hourlyEnabled"
  | "dailyEnabled"
  | "weeklyEnabled"
  | "monthlyEnabled"
  | "includeCategories"
  | "includeTags"

type SettingsNumericKey = "keepLast" | "keepHourly" | "keepDaily" | "keepWeekly" | "keepMonthly"

type ExcludedTorrentMeta = {
  hash: string
  name?: string | null
  category?: string | null
  action: "add" | "update" | "delete"
}

const runKindLabels: Record<BackupRunKind, string> = {
  manual: "Manual",
  hourly: "Hourly",
  daily: "Daily",
  weekly: "Weekly",
  monthly: "Monthly",
}

const statusVariants: Record<BackupRunStatus, "default" | "secondary" | "destructive" | "outline"> = {
  pending: "outline",
  running: "secondary",
  success: "default",
  failed: "destructive",
  canceled: "outline",
}

export function InstanceBackups({ instanceId }: InstanceBackupsProps) {
  const { instances } = useInstances()
  const instance = instances?.find(i => i.id === instanceId)
  const { data: capabilities, isLoading: capabilitiesLoading } = useInstanceCapabilities(instanceId)
  const supportsTorrentExport = capabilities?.supportsTorrentExport ?? true

  const { data: settings, isLoading: settingsLoading } = useBackupSettings(instanceId, { enabled: supportsTorrentExport })
  const { data: runs, isLoading: runsLoading } = useBackupRuns(instanceId, { enabled: supportsTorrentExport })
  const updateSettings = useUpdateBackupSettings(instanceId)
  const triggerBackup = useTriggerBackup(instanceId)
  const deleteRun = useDeleteBackupRun(instanceId)
  const deleteAllRuns = useDeleteAllBackupRuns(instanceId)
  const previewRestore = usePreviewRestore(instanceId)
  const executeRestore = useExecuteRestore(instanceId)
  const { formatDate } = useDateTimeFormatters()

  const [formState, setFormState] = useState<SettingsFormState | null>(null)
  const [manifestRunId, setManifestRunId] = useState<number | undefined>()
  const [manifestOpen, setManifestOpen] = useState(false)
  const [manifestSearch, setManifestSearch] = useState("")

  const [restoreDialogOpen, setRestoreDialogOpen] = useState(false)
  const [restoreTargetRun, setRestoreTargetRun] = useState<BackupRun | null>(null)
  const [restoreMode, setRestoreMode] = useState<RestoreMode>("incremental")
  const [restoreDryRun, setRestoreDryRun] = useState(true)
  const [restoreStartPaused, setRestoreStartPaused] = useState(true)
  const [restoreSkipHashCheck, setRestoreSkipHashCheck] = useState(true)
  const [restoreAutoResume, setRestoreAutoResume] = useState(true)
  const [restorePlan, setRestorePlan] = useState<RestorePlan | null>(null)
  const [restorePlanLoading, setRestorePlanLoading] = useState(false)
  const [restorePlanError, setRestorePlanError] = useState<string | null>(null)
  const [restoreResult, setRestoreResult] = useState<RestoreResult | null>(null)
  const [restoreExcludedHashes, setRestoreExcludedHashes] = useState<string[]>([])

  const { data: manifest, isLoading: manifestLoading } = useBackupManifest(instanceId, manifestRunId, { enabled: supportsTorrentExport })

  const manifestCategoryEntries = useMemo(() => {
    if (!manifest?.categories) return [] as Array<[string, BackupCategorySnapshot]>
    const entries = Object.entries(manifest.categories) as Array<[string, BackupCategorySnapshot]>
    entries.sort((a, b) => a[0].localeCompare(b[0], undefined, { sensitivity: "base" }))
    return entries
  }, [manifest])

  const manifestTags = useMemo(() => {
    if (!manifest?.tags) return [] as string[]
    const tagsList = [...manifest.tags]
    tagsList.sort((a, b) => a.localeCompare(b, undefined, { sensitivity: "base" }))
    return tagsList
  }, [manifest])

  const displayedCategoryEntries = useMemo(() => manifestCategoryEntries.slice(0, 12), [manifestCategoryEntries])
  const remainingCategoryCount = manifestCategoryEntries.length - displayedCategoryEntries.length

  const displayedTags = useMemo(() => manifestTags.slice(0, 30), [manifestTags])
  const remainingTagCount = manifestTags.length - displayedTags.length

  const restoreUnsupportedChanges = useMemo(() => {
    if (!restorePlan) return [] as Array<{ hash: string; change: RestoreDiffChange }>
    const updates = restorePlan.torrents.update ?? []
    return updates.flatMap(update => (update.changes ?? []).filter(change => !change.supported).map(change => ({ hash: update.hash, change })))
  }, [restorePlan])

  const restorePlanHasActions = useMemo(() => {
    if (!restorePlan) return false
    const categories = restorePlan.categories
    const tags = restorePlan.tags
    const torrents = restorePlan.torrents
    return Boolean(
      categories.create?.length ||
      categories.update?.length ||
      categories.delete?.length ||
      tags.create?.length ||
      tags.delete?.length ||
      torrents.add?.length ||
      torrents.update?.length ||
      torrents.delete?.length
    )
  }, [restorePlan])

  useEffect(() => {
    if (!manifestOpen) {
      setManifestSearch("")
    }
  }, [manifestOpen])

  useEffect(() => {
    setManifestSearch("")
  }, [manifestRunId])

  const filteredManifestItems = useMemo(() => {
    if (!manifest) return []
    const query = manifestSearch.trim().toLowerCase()
    if (!query) return manifest.items

    return manifest.items.filter(item => {
      const haystack = [
        item.name,
        item.category ?? "",
        item.tags?.join(", ") ?? "",
        item.hash,
      ]
        .join(" ")
        .toLowerCase()

      return haystack.includes(query)
    })
  }, [manifest, manifestSearch])

  useEffect(() => {
    if (settings) {
      setFormState({
        enabled: settings.enabled,
        hourlyEnabled: settings.hourlyEnabled,
        dailyEnabled: settings.dailyEnabled,
        weeklyEnabled: settings.weeklyEnabled,
        monthlyEnabled: settings.monthlyEnabled,
        keepLast: settings.keepLast,
        keepHourly: settings.keepHourly,
        keepDaily: settings.keepDaily,
        keepWeekly: settings.keepWeekly,
        keepMonthly: settings.keepMonthly,
        includeCategories: settings.includeCategories,
        includeTags: settings.includeTags,
        customPath: settings.customPath ?? "",
      })
    }
  }, [settings])

  const lastRun = useMemo(() => (runs && runs.length > 0 ? runs[0] : undefined), [runs])
  const hasRuns = useMemo(() => (runs?.length ?? 0) > 0, [runs])

  const hasActiveCadence = useMemo(() => {
    if (!formState) return false
    return formState.hourlyEnabled || formState.dailyEnabled || formState.weeklyEnabled || formState.monthlyEnabled
  }, [formState])

  const requiresCadenceSelection = Boolean(formState?.enabled && !hasActiveCadence)

  const saveDisabled = !formState || updateSettings.isPending || requiresCadenceSelection

  const handleToggle = (key: SettingsToggleKey) => (checked: boolean) => {
    setFormState(prev => {
      if (!prev) return prev

      const next: SettingsFormState = { ...prev, [key]: checked }

      if (checked) {
        switch (key) {
          case "hourlyEnabled":
            if (next.keepHourly < 1) next.keepHourly = 1
            break
          case "dailyEnabled":
            if (next.keepDaily < 1) next.keepDaily = 1
            break
          case "weeklyEnabled":
            if (next.keepWeekly < 1) next.keepWeekly = 1
            break
          case "monthlyEnabled":
            if (next.keepMonthly < 1) next.keepMonthly = 1
            break
        }
      }

      return next
    })
  }

  const handleNumberChange = (key: SettingsNumericKey) => (event: ChangeEvent<HTMLInputElement>) => {
    const parsed = parseInt(event.target.value, 10)
    const rawValue = Number.isNaN(parsed) ? 0 : Math.max(parsed, 0)

    setFormState(prev => {
      if (!prev) return prev

      const next: SettingsFormState = { ...prev, [key]: rawValue }

      if (key === "keepHourly" && prev.hourlyEnabled && next.keepHourly < 1) {
        next.keepHourly = 1
      }
      if (key === "keepDaily" && prev.dailyEnabled && next.keepDaily < 1) {
        next.keepDaily = 1
      }
      if (key === "keepWeekly" && prev.weeklyEnabled && next.keepWeekly < 1) {
        next.keepWeekly = 1
      }
      if (key === "keepMonthly" && prev.monthlyEnabled && next.keepMonthly < 1) {
        next.keepMonthly = 1
      }

      return next
    })
  }

  const handlePathChange = (event: ChangeEvent<HTMLInputElement>) => {
    const value = event.target.value
    setFormState(prev => (prev ? { ...prev, customPath: value === "" ? "" : value } : prev))
  }

  const handleSave = async () => {
    if (!formState) return
    try {
      await updateSettings.mutateAsync({
        ...formState,
        customPath: formState.customPath === "" ? null : formState.customPath,
      })
      toast.success("Backup settings updated")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to update backup settings"
      toast.error(message)
    }
  }

  const handleTrigger = async (kind: BackupRunKind = "manual") => {
    try {
      await triggerBackup.mutateAsync({ kind, requestedBy: "ui" })
      toast.success("Backup queued")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to queue backup"
      toast.error(message)
    }
  }

  const handleDelete = async (run: BackupRun) => {
    try {
      await deleteRun.mutateAsync(run.id)
      toast.success("Backup run deleted")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to delete backup run"
      toast.error(message)
    }
  }

  const handleDeleteAll = async () => {
    try {
      await deleteAllRuns.mutateAsync()
      toast.success("Deleted all backups")
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to delete backups"
      toast.error(message)
    }
  }

  const openManifest = (runId: number) => {
    setManifestRunId(runId)
    setManifestOpen(true)
  }

  const loadRestorePlan = async (
    mode: RestoreMode,
    run: BackupRun,
    excludeHashes: string[] = restoreExcludedHashes,
    options?: { reset?: boolean }
  ) => {
    setRestorePlanLoading(true)
    setRestorePlanError(null)
    if (options?.reset) {
      setRestorePlan(null)
    }
    try {
      const payloadExclude = excludeHashes.length > 0 ? excludeHashes : undefined
      const plan = await previewRestore.mutateAsync({ runId: run.id, mode, excludeHashes: payloadExclude })
      setRestorePlan(plan)
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to load restore plan"
      setRestorePlanError(message)
    } finally {
      setRestorePlanLoading(false)
    }
  }

  const openRestore = async (run: BackupRun) => {
    setRestoreTargetRun(run)
    setRestoreMode("incremental")
    setRestoreDryRun(true)
    setRestoreStartPaused(true)
    setRestoreSkipHashCheck(true)
    setRestoreAutoResume(true)
    setRestoreResult(null)
    setRestorePlan(null)
    setRestorePlanError(null)
    setRestoreExcludedHashes([])
    setRestoreDialogOpen(true)
    await loadRestorePlan("incremental", run, [], { reset: true })
  }

  const handleRestoreModeChange = async (value: string) => {
    if (!restoreTargetRun) return
    const nextMode = value as RestoreMode
    setRestoreMode(nextMode)
    setRestoreResult(null)
    setRestoreExcludedHashes([])
    await loadRestorePlan(nextMode, restoreTargetRun, [], { reset: true })
  }

  const handleExcludeTorrent = async (hash: string, meta: ExcludedTorrentMeta) => {
    if (!restoreTargetRun) return
    const normalizedHash = hash.trim()
    if (!normalizedHash) return
    if (restoreExcludedHashes.includes(normalizedHash)) {
      return
    }

    const nextExcludes = [...restoreExcludedHashes, normalizedHash]
    setRestoreExcludedHashes(nextExcludes)

    const label = meta.name?.trim() ? meta.name : normalizedHash
    toast.info(`Excluded ${label} from restore`)
  }

  const handleIncludeTorrent = async (hash: string, meta?: ExcludedTorrentMeta) => {
    if (!restoreTargetRun) return
    const normalizedHash = hash.trim()
    if (!normalizedHash) return
    if (!restoreExcludedHashes.includes(normalizedHash)) {
      return
    }

    const nextExcludes = restoreExcludedHashes.filter(existing => existing !== normalizedHash)
    setRestoreExcludedHashes(nextExcludes)

    const label = meta?.name?.trim() || normalizedHash
    toast.success(`Included ${label}`)
  }

  const handleResetExcluded = async () => {
    if (!restoreTargetRun || restoreExcludedHashes.length === 0) {
      return
    }
    setRestoreExcludedHashes([])
    toast.success("Included all torrents")
  }

  const handleExecuteRestore = async () => {
    if (!restoreTargetRun) return
    try {
      const result = await executeRestore.mutateAsync({
        runId: restoreTargetRun.id,
        mode: restoreMode,
        dryRun: restoreDryRun,
        excludeHashes: restoreExcludedHashes,
        startPaused: restoreStartPaused,
        skipHashCheck: restoreSkipHashCheck,
        autoResumeVerified: restoreSkipHashCheck ? restoreAutoResume : false,
      })
      setRestoreResult(result)
      setRestorePlan(result.plan)
      setRestorePlanError(null)
      const message = restoreDryRun ? "Restore dry-run completed" : "Restore executed"
      toast.success(message)
    } catch (error) {
      const message = error instanceof Error ? error.message : "Failed to execute restore"
      toast.error(message)
    }
  }

  const closeRestoreDialog = () => {
    setRestoreDialogOpen(false)
    setRestoreTargetRun(null)
    setRestorePlan(null)
    setRestorePlanError(null)
    setRestoreResult(null)
    setRestoreExcludedHashes([])
    setRestoreStartPaused(true)
    setRestoreSkipHashCheck(true)
    setRestoreAutoResume(true)
    previewRestore.reset()
    executeRestore.reset()
  }

  if (capabilitiesLoading) {
    return <div className="p-6">Loading instance capabilities...</div>
  }

  if (!supportsTorrentExport) {
    const versionRaw = capabilities?.webAPIVersion?.trim()
    const reportedVersion = versionRaw && versionRaw.length > 0 ? versionRaw : "an older web API version"

    return (
      <div className="p-6 space-y-4">
        <Alert variant="destructive">
          <AlertTitle>Backups unavailable for this instance</AlertTitle>
          <AlertDescription>
            {`qBittorrent Web API 2.8.11+ (qBittorrent 4.5.0+) is required to export .torrent files for backups. This instance reports ${reportedVersion}, so torrent exports are disabled.`}
          </AlertDescription>
        </Alert>
        <Button variant="outline" asChild>
          <Link to="/instances/$instanceId" params={{ instanceId: instanceId.toString() }}>
            Return to instance overview
          </Link>
        </Button>
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-6 p-4 lg:p-6">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
          <div>
            <h1 className="text-2xl font-semibold">Backups</h1>
            <p className="text-sm text-muted-foreground">
              Manage torrent backups for {instance?.name ?? `instance ${instanceId}`}
            </p>
          </div>
          <div className="flex gap-2">
            <Button variant="outline" asChild>
              <Link to="/instances/$instanceId" params={{ instanceId: instanceId.toString() }}>
                Back to Torrents
              </Link>
            </Button>
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 auto-rows-fr">
          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Last backup</CardTitle>
              <Clock className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="flex-1 flex flex-col">
              {runsLoading ? (
                <p className="text-sm text-muted-foreground">Loading...</p>
              ) : lastRun ? (
                <div className="flex flex-col flex-1">
                  <div className="space-y-2">
                    <Badge variant={statusVariants[lastRun.status]}>{runKindLabels[lastRun.kind]}</Badge>
                    <p className="text-sm">
                      {formatDateSafe(lastRun.completedAt ?? lastRun.requestedAt, formatDate)}
                    </p>
                  </div>
                  <div className="min-h-[44px] flex items-start pt-1 mt-auto">
                    {lastRun.status === "running" && lastRun.progressTotal && lastRun.progressTotal > 0 ? (
                      <div className="space-y-1 w-full">
                        <Progress value={lastRun.progressPercentage ?? 0} className="h-2" />
                        <p className="text-xs text-muted-foreground">
                          {lastRun.progressCurrent ?? 0} of {lastRun.progressTotal} torrents ({(lastRun.progressPercentage ?? 0).toFixed(1)}%)
                        </p>
                      </div>
                    ) : (
                      <p className="text-xs text-muted-foreground capitalize">Status: {lastRun.status}</p>
                    )}
                  </div>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">No backups yet</p>
              )}
            </CardContent>
          </Card>

          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Queued backups</CardTitle>
              <RefreshCw className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              {runsLoading ? (
                <p className="text-sm text-muted-foreground">Loading...</p>
              ) : (
                <p className="text-2xl font-bold">{runs?.filter(run => run.status === "running" || run.status === "pending").length ?? 0}</p>
              )}
              <p className="text-xs text-muted-foreground">Pending or running backups</p>
            </CardContent>
          </Card>

          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">Instance</CardTitle>
              <Download className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <p className="text-sm truncate font-semibold">{instance?.name ?? `Instance ${instanceId}`}</p>
              <p className="text-xs text-muted-foreground break-all">{instance?.host}</p>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Backup settings</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6">
            {settingsLoading || !formState ? (
              <p className="text-sm text-muted-foreground">Loading settings...</p>
            ) : (
              <div className="space-y-6">
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  <SettingToggle
                    label="Enable backups"
                    description="Turn on automatic backups for this instance. Pick at least one cadence below; leave this off for manual-only backups."
                    checked={formState.enabled}
                    onCheckedChange={handleToggle("enabled")}
                  />
                  <SettingToggle
                    label="Include categories"
                    description="Group torrents inside the archive by their category"
                    checked={formState.includeCategories}
                    onCheckedChange={handleToggle("includeCategories")}
                  />
                  <SettingToggle
                    label="Include tags"
                    description="Store torrent tags in the manifest"
                    checked={formState.includeTags}
                    onCheckedChange={handleToggle("includeTags")}
                  />
                </div>

                <Separator />

                <div className="space-y-4">
                  <div className="space-y-1">
                    <p className="text-sm font-medium">Automatic cadences</p>
                    <p className="text-xs text-muted-foreground">
                      Hourly, daily, weekly, and monthly runs start after the previous backup finishes and their interval has elapsed. Leave them all off if you only trigger backups manually.
                    </p>
                  </div>
                  <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
                    <ScheduleControl
                      label="Hourly"
                      checked={formState.hourlyEnabled}
                      onCheckedChange={handleToggle("hourlyEnabled")}
                      value={formState.keepHourly}
                      onValueChange={handleNumberChange("keepHourly")}
                      description="How many hourly snapshots to keep before older ones are pruned."
                      tooltip="Runs roughly once per hour when backups are enabled and at least an hour has passed since the last run."
                    />
                    <ScheduleControl
                      label="Daily"
                      checked={formState.dailyEnabled}
                      onCheckedChange={handleToggle("dailyEnabled")}
                      value={formState.keepDaily}
                      onValueChange={handleNumberChange("keepDaily")}
                      description="How many daily snapshots to keep before rotation."
                      tooltip="Runs about once every 24 hours after the previous job finishes."
                    />
                    <ScheduleControl
                      label="Weekly"
                      checked={formState.weeklyEnabled}
                      onCheckedChange={handleToggle("weeklyEnabled")}
                      value={formState.keepWeekly}
                      onValueChange={handleNumberChange("keepWeekly")}
                      description="How many weekly snapshots to keep before rotation."
                      tooltip="Runs once the last backup is at least seven days old."
                    />
                    <ScheduleControl
                      label="Monthly"
                      checked={formState.monthlyEnabled}
                      onCheckedChange={handleToggle("monthlyEnabled")}
                      value={formState.keepMonthly}
                      onValueChange={handleNumberChange("keepMonthly")}
                      description="How many monthly snapshots to keep before rotation."
                      tooltip="Runs when the previous run is from an earlier calendar month."
                    />
                  </div>
                </div>

                <div className="grid gap-4 md:grid-cols-2">
                  <div className="space-y-2">
                    <Label htmlFor="keep-last">Keep latest backups</Label>
                    <Input
                      id="keep-last"
                      type="number"
                      min={0}
                      value={formState.keepLast}
                      onChange={handleNumberChange("keepLast")}
                    />
                    <p className="text-xs text-muted-foreground">Maximum total backups to retain across all schedules</p>
                  </div>
                  <div className="space-y-2">
                    <div className="flex items-center gap-2">
                      <Label htmlFor="custom-path">Custom backup path</Label>
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <span
                            className="inline-flex h-5 w-5 cursor-help items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                          >
                            <CircleHelp className="h-3.5 w-3.5" />
                          </span>
                        </TooltipTrigger>
                        <TooltipContent>
                          <p>Windows users: use double backslashes (e.g., backups\\instance-1)</p>
                        </TooltipContent>
                      </Tooltip>
                    </div>
                    <Input
                      id="custom-path"
                      value={formState.customPath ?? ""}
                      onChange={handlePathChange}
                      placeholder="backups/instance-1"
                    />
                    <p className="text-xs text-muted-foreground">Relative to qui data directory. Leave empty to use default.</p>
                  </div>
                </div>

                <div className="space-y-2">
                  <div className="flex flex-wrap gap-2">
                    <Button onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
                      <ArrowDownToLine className="mr-2 h-4 w-4" /> Run manual backup
                    </Button>
                    <Button
                      variant="outline"
                      onClick={handleSave}
                      disabled={saveDisabled}
                      title={requiresCadenceSelection ? "Select at least one cadence to enable automatic backups." : undefined}
                    >
                      Save changes
                    </Button>
                  </div>
                  {requiresCadenceSelection ? (
                    <p className="text-xs text-destructive">Select at least one cadence (hourly, daily, weekly, or monthly) before saving.</p>
                  ) : (
                    <p className="text-xs text-muted-foreground">Changes apply to future backups as soon as you save.</p>
                  )}
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Dialog open={restoreDialogOpen} onOpenChange={(open) => {
          if (!open) {
            closeRestoreDialog()
          }
        }}>
          <DialogContent className="!w-[96vw] !max-w-6xl !md:w-[90vw] !h-[92vh] md:!h-[80vh] lg:!h-[75vh] overflow-hidden flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>Restore backup</DialogTitle>
              <DialogDescription>
                {restoreTargetRun ? `Run #${restoreTargetRun.id} (${runKindLabels[restoreTargetRun.kind]})` : "Select a backup to restore"}
              </DialogDescription>
            </DialogHeader>

            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">Mode</span>
                <div className="flex items-center gap-2">
                  <Select
                    value={restoreMode}
                    onValueChange={handleRestoreModeChange}
                    disabled={restorePlanLoading || !restoreTargetRun}
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder="Select restore mode" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="incremental">Incremental</SelectItem>
                      <SelectItem value="overwrite">Overwrite</SelectItem>
                      <SelectItem value="complete">Complete</SelectItem>
                    </SelectContent>
                  </Select>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        className="inline-flex h-8 w-8 cursor-help items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                      >
                        <CircleHelp className="h-4 w-4" />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent align="start" className="max-w-sm text-xs">
                      <p className="font-bold">Incremental</p>
                      <p className="mb-2">Adds missing categories, tags, and torrents without touching existing data.</p>
                      <p className="font-bold">Overwrite</p>
                      <p className="mb-2">Adds missing items and updates existing categories/torrents to match the snapshot.</p>
                      <p className="font-bold">Complete</p>
                      <p>Same as overwrite, then removes anything not recorded in the backup.</p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-dry-run"
                  checked={restoreDryRun}
                  onCheckedChange={setRestoreDryRun}
                />
                <Label htmlFor="restore-dry-run">Dry run</Label>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-start-paused"
                  checked={restoreStartPaused}
                  onCheckedChange={setRestoreStartPaused}
                />
                <Label htmlFor="restore-start-paused">Start paused</Label>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-skip-hash-check"
                  checked={restoreSkipHashCheck}
                  onCheckedChange={setRestoreSkipHashCheck}
                />
                <Label htmlFor="restore-skip-hash-check">Skip recheck</Label>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-auto-resume"
                  checked={restoreAutoResume}
                  onCheckedChange={setRestoreAutoResume}
                  disabled={!restoreSkipHashCheck}
                />
                <div className="flex items-center gap-1 text-sm">
                  <Label
                    htmlFor="restore-auto-resume"
                    className={!restoreSkipHashCheck ? "text-muted-foreground" : undefined}
                  >
                    Auto-resume completed torrents
                  </Label>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <span
                        className="inline-flex h-5 w-5 cursor-help items-center justify-center rounded-full text-muted-foreground hover:text-foreground"
                      >
                        <CircleHelp className="h-3.5 w-3.5" />
                      </span>
                    </TooltipTrigger>
                    <TooltipContent className="max-w-xs text-xs">
                      <p>
                        When enabled, torrents resume automatically if qBittorrent reports the restored data as
                        fully checked. In other words; no data is missing.
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>

              <div className="basis-full text-xs text-muted-foreground">
                {restoreSkipHashCheck && restoreAutoResume ? (
                  <span>Torrents will resume automatically once qBittorrent reports the restored data as fully checked.</span>
                ) : restoreSkipHashCheck ? (
                  <span>Auto resume is off. Torrents stay paused until you start them manually.</span>
                ) : (
                  <span>Enable skip recheck to allow automatic resume after verification.</span>
                )}
              </div>

              <div className="ml-auto flex items-center gap-2">
                {restoreExcludedHashes.length > 0 ? (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleResetExcluded}
                    disabled={restorePlanLoading}
                  >
                    <Undo2 className="mr-2 h-4 w-4" /> Re-include all
                  </Button>
                ) : null}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => restoreTargetRun && loadRestorePlan(restoreMode, restoreTargetRun, restoreExcludedHashes)}
                  disabled={restorePlanLoading || !restoreTargetRun}
                >
                  <ListChecks className="mr-2 h-4 w-4" /> Refresh plan
                </Button>
                <Button
                  onClick={handleExecuteRestore}
                  disabled={!restoreTargetRun || restorePlanLoading || executeRestore.isPending}
                >
                  {executeRestore.isPending ? "Executing..." : restoreDryRun ? "Run dry-run" : "Execute restore"}
                </Button>
              </div>
            </div>

            <Separator />



            <div className="flex-1 overflow-y-auto space-y-6">
              {!restorePlan && restorePlanLoading ? (
                <p className="text-sm text-muted-foreground">Loading restore plan...</p>
              ) : !restorePlan && restorePlanError ? (
                <p className="text-sm text-destructive">{restorePlanError}</p>
              ) : restorePlan ? (
                <div className="space-y-6">
                  {restorePlanError ? (
                    <p className="text-sm text-destructive">{restorePlanError}</p>
                  ) : null}

                  {restoreExcludedHashes.length > 0 ? (
                    <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-muted-foreground/40 bg-muted/20 px-3 py-2">
                      <div className="flex items-center gap-2 text-sm">
                        <Badge variant="secondary" className="text-[10px] uppercase">excluded</Badge>
                        <span>{restoreExcludedHashes.length} torrent{restoreExcludedHashes.length === 1 ? "" : "s"} excluded from this restore.</span>
                      </div>
                    </div>
                  ) : null}

                  {restorePlanLoading && restorePlan ? (
                    <p className="text-xs text-muted-foreground">Refreshing plan...</p>
                  ) : null}

                  {restorePlanHasActions ? (
                    <>
                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">Categories</h4>
                        {(restorePlan.categories.create?.length ||
                        restorePlan.categories.update?.length ||
                        restorePlan.categories.delete?.length) ? (
                            <div className="space-y-3">
                              {restorePlan.categories.create?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">Create</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.create.map(item => (
                                      <li key={`cat-create-${item.name}`} className="flex flex-wrap items-center gap-2">
                                        <Badge variant="outline" className="text-[10px] uppercase">create</Badge>
                                        <span>{item.name}</span>
                                        {item.savePath ? (
                                          <span className="text-xs text-muted-foreground">({item.savePath})</span>
                                        ) : null}
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              ) : null}
                              {restorePlan.categories.update?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">Update</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.update.map(item => (
                                      <li key={`cat-update-${item.name}`} className="flex flex-wrap items-center gap-2">
                                        <Badge variant="secondary" className="text-[10px] uppercase">update</Badge>
                                        <span>{item.name}</span>
                                        <span className="text-xs text-muted-foreground">
                                          {item.currentPath || "—"} → {item.desiredPath || "—"}
                                        </span>
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              ) : null}
                              {restorePlan.categories.delete?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">Delete</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.delete.map(name => (
                                      <li key={`cat-delete-${name}`} className="flex items-center gap-2">
                                        <Badge variant="destructive" className="text-[10px] uppercase">delete</Badge>
                                        <span>{name}</span>
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              ) : null}
                            </div>
                          ) : (
                            <p className="text-sm text-muted-foreground">No category changes.</p>
                          )}
                      </section>

                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">Tags</h4>
                        {(restorePlan.tags.create?.length || restorePlan.tags.delete?.length) ? (
                          <div className="space-y-3">
                            {restorePlan.tags.create?.length ? (
                              <div>
                                <p className="text-xs font-medium text-muted-foreground mb-1">Create</p>
                                <ul className="flex flex-wrap gap-2 text-sm">
                                  {restorePlan.tags.create.map(item => (
                                    <li key={`tag-create-${item.name}`}>
                                      <Badge variant="outline">{item.name}</Badge>
                                    </li>
                                  ))}
                                </ul>
                              </div>
                            ) : null}
                            {restorePlan.tags.delete?.length ? (
                              <div>
                                <p className="text-xs font-medium text-muted-foreground mb-1">Delete</p>
                                <ul className="flex flex-wrap gap-2 text-sm">
                                  {restorePlan.tags.delete.map(name => (
                                    <li key={`tag-delete-${name}`}>
                                      <Badge variant="destructive">{name}</Badge>
                                    </li>
                                  ))}
                                </ul>
                              </div>
                            ) : null}
                          </div>
                        ) : (
                          <p className="text-sm text-muted-foreground">No tag changes.</p>
                        )}
                      </section>

                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">Torrents</h4>
                        {(restorePlan.torrents.add?.length ||
                        restorePlan.torrents.update?.length ||
                        restorePlan.torrents.delete?.length) ? (
                            <div className="space-y-4">
                              {restorePlan.torrents.add?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">
                                    Add ({restorePlan.torrents.add.length})
                                  </p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.torrents.add.map(item => {
                                      const hash = item.manifest.hash
                                      const isExcluded = restoreExcludedHashes.includes(hash)
                                      return (
                                        <li
                                          key={`torrent-add-${hash}`}
                                          className={`flex flex-wrap items-center gap-2 rounded-md px-2 py-1 ${isExcluded ? "bg-muted/40 text-muted-foreground" : ""}`}
                                        >
                                          <div className="flex min-w-0 flex-1 flex-wrap items-center gap-2">
                                            <Badge variant="outline" className="text-[10px] uppercase">add</Badge>
                                            <span className="font-medium truncate">
                                              {item.manifest.name || hash}
                                            </span>
                                            <code className="text-xs text-muted-foreground">{hash}</code>
                                            {item.manifest.category ? (
                                              <span className="text-xs text-muted-foreground">• {item.manifest.category}</span>
                                            ) : null}
                                            {isExcluded ? (
                                              <Badge variant="secondary" className="text-[10px] uppercase">excluded</Badge>
                                            ) : null}
                                          </div>
                                          <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => (isExcluded? handleIncludeTorrent(hash, {
                                              hash,
                                              name: item.manifest.name,
                                              category: item.manifest.category ?? null,
                                              action: "add",
                                            }): handleExcludeTorrent(hash, {
                                              hash,
                                              name: item.manifest.name,
                                              category: item.manifest.category ?? null,
                                              action: "add",
                                            })
                                            )}
                                            disabled={restorePlanLoading}
                                            aria-label={`${isExcluded ? "Include" : "Exclude"} ${item.manifest.name || hash} from restore`}
                                          >
                                            {isExcluded ? (
                                              <>
                                                <Undo2 className="mr-1 h-3 w-3" /> Include
                                              </>
                                            ) : (
                                              <>
                                                <CircleX className="mr-1 h-3 w-3" /> Exclude
                                              </>
                                            )}
                                          </Button>
                                        </li>
                                      )
                                    })}
                                  </ul>
                                </div>
                              ) : null}
                              {restorePlan.torrents.update?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">
                                    Update ({restorePlan.torrents.update.length})
                                  </p>
                                  <div className="space-y-3">
                                    {restorePlan.torrents.update.map(update => {
                                      const isExcluded = restoreExcludedHashes.includes(update.hash)
                                      return (
                                        <div
                                          key={`torrent-update-${update.hash}`}
                                          className={`rounded-md border p-3 space-y-2 ${isExcluded ? "border-dashed bg-muted/40" : ""}`}
                                        >
                                          <div className="flex flex-wrap items-center justify-between gap-2">
                                            <div className="flex flex-col">
                                              <div className="flex items-center gap-2">
                                                <span className="text-sm font-medium">{update.desired.name || update.current.name || update.hash}</span>
                                                {isExcluded ? (
                                                  <Badge variant="secondary" className="text-[10px] uppercase">excluded</Badge>
                                                ) : null}
                                              </div>
                                              <span className="text-xs text-muted-foreground">
                                                Current category: {update.current.category || "—"}
                                              </span>
                                            </div>
                                            <div className="flex items-center gap-2">
                                              <code className="text-xs text-muted-foreground">{update.hash}</code>
                                              <Button
                                                variant="ghost"
                                                size="sm"
                                                onClick={() => (isExcluded? handleIncludeTorrent(update.hash, {
                                                  hash: update.hash,
                                                  name: update.desired.name || update.current.name || update.hash,
                                                  category: update.desired.category ?? update.current.category ?? null,
                                                  action: "update",
                                                }): handleExcludeTorrent(update.hash, {
                                                  hash: update.hash,
                                                  name: update.desired.name || update.current.name || update.hash,
                                                  category: update.desired.category ?? update.current.category ?? null,
                                                  action: "update",
                                                })
                                                )}
                                                disabled={restorePlanLoading}
                                                aria-label={`${isExcluded ? "Include" : "Exclude"} ${update.desired.name || update.current.name || update.hash} from restore`}
                                              >
                                                {isExcluded ? (
                                                  <>
                                                    <Undo2 className="mr-1 h-3 w-3" /> Include
                                                  </>
                                                ) : (
                                                  <>
                                                    <CircleX className="mr-1 h-3 w-3" /> Exclude
                                                  </>
                                                )}
                                              </Button>
                                            </div>
                                          </div>
                                          <div className="space-y-1">
                                            {update.changes.map(change => (
                                              <div key={`${update.hash}-${change.field}`} className="flex flex-wrap items-center gap-2 text-sm">
                                                <Badge
                                                  variant={change.supported ? "secondary" : "outline"}
                                                  className="text-[10px] uppercase"
                                                >
                                                  {change.supported ? "auto" : "manual"}
                                                </Badge>
                                                <span className="font-medium capitalize">{humanizeChangeField(change.field)}</span>
                                                <span className="text-xs text-muted-foreground">
                                                  {formatChangeValue(change.current)} → {formatChangeValue(change.desired)}
                                                </span>
                                                {change.message ? (
                                                  <span className="text-xs text-muted-foreground">{change.message}</span>
                                                ) : null}
                                              </div>
                                            ))}
                                          </div>
                                        </div>
                                      )
                                    })}
                                  </div>
                                </div>
                              ) : null}
                              {restorePlan.torrents.delete?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">
                                    Delete ({restorePlan.torrents.delete.length})
                                  </p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.torrents.delete.map(hash => {
                                      const isExcluded = restoreExcludedHashes.includes(hash)
                                      return (
                                        <li
                                          key={`torrent-delete-${hash}`}
                                          className={`flex flex-wrap items-center gap-2 rounded-md px-2 py-1 ${isExcluded ? "bg-muted/40 text-muted-foreground" : ""}`}
                                        >
                                          <Badge variant="destructive" className="text-[10px] uppercase">delete</Badge>
                                          <code className="text-xs text-muted-foreground">{hash}</code>
                                          {isExcluded ? (
                                            <Badge variant="secondary" className="text-[10px] uppercase">excluded</Badge>
                                          ) : null}
                                          <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => (isExcluded? handleIncludeTorrent(hash, { hash, action: "delete" }): handleExcludeTorrent(hash, { hash, action: "delete" })
                                            )}
                                            disabled={restorePlanLoading}
                                            aria-label={`${isExcluded ? "Include" : "Exclude"} ${hash} from restore`}
                                          >
                                            {isExcluded ? (
                                              <>
                                                <Undo2 className="mr-1 h-3 w-3" /> Include
                                              </>
                                            ) : (
                                              <>
                                                <CircleX className="mr-1 h-3 w-3" /> Exclude
                                              </>
                                            )}
                                          </Button>
                                        </li>
                                      )
                                    })}
                                  </ul>
                                </div>
                              ) : null}
                            </div>
                          ) : (
                            <p className="text-sm text-muted-foreground">No torrent changes.</p>
                          )}
                      </section>
                    </>
                  ) : (
                    <p className="text-sm text-muted-foreground">No changes are required for this restore mode.</p>
                  )}

                  {restoreUnsupportedChanges.length > 0 && (
                    <div className="rounded-md border border-amber-200 bg-amber-50 p-3 space-y-2 text-sm text-amber-900">
                      <p className="font-medium">Manual follow-up required</p>
                      <ul className="list-disc pl-5 space-y-1">
                        {restoreUnsupportedChanges.map(({ hash, change }, index) => (
                          <li key={`unsupported-${hash}-${change.field}-${index}`}>
                            <code className="text-xs">{hash}</code> • {humanizeChangeField(change.field)}
                            {change.message ? <span> — {change.message}</span> : null}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">Select a backup to restore.</p>
              )}
            </div>

            {restoreResult && (
              <div className="rounded-md border p-4 space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold">Last execution</h4>
                  <Badge variant={restoreResult.dryRun ? "outline" : "default"} className="text-xs">
                    {restoreResult.dryRun ? "Dry run" : "Applied"}
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">Mode: {restoreResult.mode}</p>
                <div className="grid gap-3 md:grid-cols-3 text-sm">
                  <div>
                    <p className="font-medium">Categories</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.categories.created)} / Δ{countItems(restoreResult.applied.categories.updated)} / −{countItems(restoreResult.applied.categories.deleted)}
                    </p>
                  </div>
                  <div>
                    <p className="font-medium">Tags</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.tags.created)} / −{countItems(restoreResult.applied.tags.deleted)}
                    </p>
                  </div>
                  <div>
                    <p className="font-medium">Torrents</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.torrents.added)} / Δ{countItems(restoreResult.applied.torrents.updated)} / −{countItems(restoreResult.applied.torrents.deleted)}
                    </p>
                  </div>
                </div>
                {restoreResult.warnings?.length ? (
                  <div className="rounded-md border border-amber-200 bg-amber-50 p-3 space-y-1 text-sm text-amber-900">
                    <p className="font-medium">Warnings</p>
                    <ul className="list-disc pl-5 space-y-1">
                      {restoreResult.warnings.map((warning, index) => (
                        <li key={`restore-warning-${index}`}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
                {restoreResult.errors?.length ? (
                  <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 space-y-1 text-sm text-destructive">
                    <p className="font-medium">Errors</p>
                    <ul className="list-disc pl-5 space-y-1">
                      {restoreResult.errors.map((errorItem, index) => (
                        <li key={`restore-error-${index}`}>
                          {errorItem.operation}: {errorItem.target} — {errorItem.message}
                        </li>
                      ))}
                    </ul>
                  </div>
                ) : null}
              </div>
            )}
          </DialogContent>
        </Dialog>

        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>Backup history</CardTitle>
              <div className="flex items-center gap-2">
                <AlertDialog>
                  <AlertDialogTrigger asChild>
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={deleteAllRuns.isPending || runsLoading || !hasRuns}
                    >
                      <Trash className="mr-2 h-4 w-4" /> Delete all
                    </Button>
                  </AlertDialogTrigger>
                  <AlertDialogContent>
                    <AlertDialogHeader>
                      <AlertDialogTitle>Delete all backups?</AlertDialogTitle>
                      <AlertDialogDescription>
                        This removes every stored backup archive and manifest for this instance. This action cannot be undone.
                      </AlertDialogDescription>
                    </AlertDialogHeader>
                    <AlertDialogFooter>
                      <AlertDialogCancel>Cancel</AlertDialogCancel>
                      <AlertDialogAction onClick={handleDeleteAll} disabled={deleteAllRuns.isPending}>
                        Delete all
                      </AlertDialogAction>
                    </AlertDialogFooter>
                  </AlertDialogContent>
                </AlertDialog>
                <Button variant="outline" size="sm" onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
                  <ArrowDownToLine className="mr-2 h-4 w-4" /> Queue backup
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            {runsLoading ? (
              <p className="text-sm text-muted-foreground">Loading backups...</p>
            ) : runs && runs.length > 0 ? (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Type</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Requested</TableHead>
                    <TableHead>Completed</TableHead>
                    <TableHead className="text-right">Torrents</TableHead>
                    <TableHead className="text-right">Size</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {runs.map(run => (
                    <TableRow key={run.id}>
                      <TableCell className="font-medium">{runKindLabels[run.kind]}</TableCell>
                      <TableCell>
                        {run.status === "running" && run.progressTotal && run.progressTotal > 0 ? (
                          <div className="space-y-1 min-w-[200px]">
                            <Progress value={run.progressPercentage ?? 0} className="h-2" />
                            <p className="text-xs text-muted-foreground">
                              {run.progressCurrent ?? 0} of {run.progressTotal} torrents ({(run.progressPercentage ?? 0).toFixed(1)}%)
                            </p>
                          </div>
                        ) : (
                          <Badge variant={statusVariants[run.status]} className="capitalize">{run.status}</Badge>
                        )}
                      </TableCell>
                      <TableCell>{formatDateSafe(run.requestedAt, formatDate)}</TableCell>
                      <TableCell>{formatDateSafe(run.completedAt, formatDate)}</TableCell>
                      <TableCell className="text-right">{run.torrentCount}</TableCell>
                      <TableCell className="text-right">{formatBytes(run.totalBytes)}</TableCell>
                      <TableCell className="flex justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => openManifest(run.id)}
                          aria-label="View manifest"
                        >
                          <FileText className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          onClick={() => openRestore(run)}
                          aria-label="Restore from backup"
                        >
                          <Undo2 className="h-4 w-4" />
                        </Button>
                        {run.archivePath ? (
                          <Button
                            variant="ghost"
                            size="icon"
                            asChild
                            aria-label="Download backup"
                          >
                            <a
                              href={api.getBackupDownloadUrl(instanceId, run.id)}
                              rel="noreferrer"
                            >
                              <Download className="h-4 w-4" />
                            </a>
                          </Button>
                        ) : (
                          <Button variant="ghost" size="icon" disabled aria-label="Download unavailable">
                            <Download className="h-4 w-4" />
                          </Button>
                        )}
                        <AlertDialog>
                          <AlertDialogTrigger asChild>
                            <Button variant="ghost" size="icon" aria-label="Delete backup">
                              <Trash className="h-4 w-4" />
                            </Button>
                          </AlertDialogTrigger>
                          <AlertDialogContent>
                            <AlertDialogHeader>
                              <AlertDialogTitle>Delete backup?</AlertDialogTitle>
                              <AlertDialogDescription>
                                This will remove the backup archive and manifest from disk. This action cannot be undone.
                              </AlertDialogDescription>
                            </AlertDialogHeader>
                            <AlertDialogFooter>
                              <AlertDialogCancel>Cancel</AlertDialogCancel>
                              <AlertDialogAction onClick={() => handleDelete(run)}>
                                Delete
                              </AlertDialogAction>
                            </AlertDialogFooter>
                          </AlertDialogContent>
                        </AlertDialog>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            ) : (
              <p className="text-sm text-muted-foreground">No backups have been created yet.</p>
            )}
          </CardContent>
        </Card>

        <Dialog open={manifestOpen} onOpenChange={(open) => {
          setManifestOpen(open)
          if (!open) {
            setManifestRunId(undefined)
          }
        }}>
          <DialogContent className="!w-[96vw] !max-w-7xl !md:w-[90vw] !h-[92vh] md:!h-[80vh] lg:!h-[75vh] overflow-hidden flex flex-col">
            <DialogHeader>
              <DialogTitle>Backup manifest</DialogTitle>
              <DialogDescription>
                {manifestRunId ? `Run #${manifestRunId}` : "Select a backup to view its manifest"}
              </DialogDescription>
            </DialogHeader>
            {manifestLoading ? (
              <p className="text-sm text-muted-foreground">Loading manifest...</p>
            ) : manifest ? (
              <div className="space-y-4 flex-1 flex flex-col min-h-0">
                <div className="space-y-3 text-sm">
                  <div className="flex flex-wrap gap-3 text-muted-foreground">
                    <span className="font-medium text-foreground">Torrents: {manifest.torrentCount}</span>
                    {manifestCategoryEntries.length > 0 && (
                      <span>Categories: {manifestCategoryEntries.length}</span>
                    )}
                    {manifestTags.length > 0 && <span>Tags: {manifestTags.length}</span>}
                    <span>Generated {formatDateSafe(manifest.generatedAt, formatDate)}</span>
                  </div>
                  {displayedCategoryEntries.length > 0 && (
                    <div>
                      <p className="font-medium text-foreground mb-2">Categories</p>
                      <div className="flex flex-wrap gap-2">
                        {displayedCategoryEntries.map(([name, snapshot]) => (
                          <Badge key={name} variant="secondary" title={snapshot?.savePath ?? undefined}>
                            {name}
                          </Badge>
                        ))}
                        {remainingCategoryCount > 0 && (
                          <Badge variant="outline">+{remainingCategoryCount} more</Badge>
                        )}
                      </div>
                    </div>
                  )}
                  {displayedTags.length > 0 && (
                    <div>
                      <p className="font-medium text-foreground mb-2">Tags</p>
                      <div className="flex flex-wrap gap-2">
                        {displayedTags.map(tag => (
                          <Badge key={tag} variant="outline">{tag}</Badge>
                        ))}
                        {remainingTagCount > 0 && (
                          <Badge variant="outline">+{remainingTagCount} more</Badge>
                        )}
                      </div>
                    </div>
                  )}
                </div>
                <div className="flex w-full justify-end">
                  <Input
                    value={manifestSearch}
                    onChange={event => setManifestSearch(event.target.value)}
                    placeholder="Search torrents, tags, categories..."
                    className="w-full sm:w-[18rem] md:w-[16rem]"
                    aria-label="Search backup manifest"
                  />
                </div>
                <div className="flex-1 overflow-auto pr-1">
                  <Table className="min-w-[640px] w-full">
                    <TableHeader className="sticky top-0 z-10 bg-background">
                      <TableRow>
                        <TableHead>Name</TableHead>
                        <TableHead>Category</TableHead>
                        <TableHead>Tags</TableHead>
                        <TableHead className="text-right">Size</TableHead>
                        <TableHead className="text-right">Cached Torrent</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredManifestItems.length > 0 ? (
                        filteredManifestItems.map(item => (
                          <TableRow key={item.hash + item.archivePath}>
                            <TableCell className="font-medium !max-w-md truncate">{item.name}</TableCell>
                            <TableCell>{item.category ?? "—"}</TableCell>
                            <TableCell className="max-w-sm truncate">{item.tags && item.tags.length > 0 ? item.tags.join(", ") : "—"}</TableCell>
                            <TableCell className="text-right">{formatBytes(item.sizeBytes)}</TableCell>
                            <TableCell className="text-right">
                              {item.torrentBlob && manifestRunId ? (
                                <Button variant="ghost" size="icon" asChild>
                                  <a
                                    href={api.getBackupTorrentDownloadUrl(instanceId, manifestRunId, item.hash)}
                                    download
                                    aria-label={`Download ${item.name} torrent`}
                                  >
                                    <Download className="h-4 w-4" />
                                  </a>
                                </Button>
                              ) : (
                                <span className="text-xs text-muted-foreground">—</span>
                              )}
                            </TableCell>
                          </TableRow>
                        ))
                      ) : (
                        <TableRow>
                          <TableCell colSpan={5} className="py-6 text-center text-sm text-muted-foreground">
                            {manifestSearch ? `No torrents match "${manifestSearch}".` : "No torrents found."}
                          </TableCell>
                        </TableRow>
                      )}
                    </TableBody>
                  </Table>
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">Manifest unavailable.</p>
            )}
          </DialogContent>
        </Dialog>
      </div>
    </TooltipProvider>
  )
}

function SettingToggle({
  label,
  description,
  checked,
  onCheckedChange,
}: {
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
}) {
  return (
    <div className="flex items-start justify-between gap-4 rounded-lg border p-4">
      <div>
        <p className="font-medium leading-none mb-1">{label}</p>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </div>
  )
}

function ScheduleControl({
  label,
  description,
  checked,
  onCheckedChange,
  value,
  onValueChange,
  tooltip,
}: {
  label: string
  description: string
  checked: boolean
  onCheckedChange: (checked: boolean) => void
  value: number
  onValueChange: (event: ChangeEvent<HTMLInputElement>) => void
  tooltip?: string
}) {
  return (
    <div className="space-y-3">
      <div className="flex items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Label className="font-medium">{label}</Label>
          {tooltip ? (
            <Tooltip>
              <TooltipTrigger asChild>
                <span className="inline-flex h-6 w-6 cursor-help items-center justify-center rounded-full text-muted-foreground hover:text-foreground">
                  <CircleHelp className="h-4 w-4" />
                </span>
              </TooltipTrigger>
              <TooltipContent align="start" className="max-w-xs text-xs">
                {tooltip}
              </TooltipContent>
            </Tooltip>
          ) : null}
        </div>
        <Switch checked={checked} onCheckedChange={onCheckedChange} />
      </div>
      <Input type="number" min={checked ? 1 : 0} value={value} onChange={onValueChange} disabled={!checked} />
      <p className="text-xs text-muted-foreground">{description}</p>
    </div>
  )
}

function humanizeChangeField(field: string): string {
  const mappings: Record<string, string> = {
    sizeBytes: "Size",
    infohash_v1: "Infohash v1",
    infohash_v2: "Infohash v2",
  }
  if (mappings[field]) return mappings[field]
  return field
    .replace(/_/g, " ")
    .replace(/([A-Z])/g, " $1")
    .trim()
    .replace(/^./, char => char.toUpperCase())
}

function formatChangeValue(value: unknown): string {
  if (value === null || value === undefined) return "—"
  if (Array.isArray(value)) {
    return value.length > 0 ? value.map(item => formatChangeValue(item)).join(", ") : "—"
  }
  if (typeof value === "string") {
    const trimmed = value.trim()
    return trimmed === "" ? "—" : trimmed
  }
  return String(value)
}

function countItems<T>(items?: T[] | null): number {
  return items?.length ?? 0
}

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const order = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  const value = bytes / Math.pow(1024, order)
  return `${value.toFixed(value >= 10 || order === 0 ? 0 : 1)} ${units[order]}`
}

function formatDateSafe(value: string | null | undefined, formatter: (date: Date) => string): string {
  if (!value) return "—"
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return "—"
  return formatter(parsed)
}
