/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { Link } from "@tanstack/react-router"
import { ArrowDownToLine, ChevronLeft, ChevronRight, CircleHelp, CircleX, Clock, Download, FileText, HardDrive, ListChecks, RefreshCw, Trash, Undo2 } from "lucide-react"
import type { ChangeEvent } from "react"
import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger
} from "@/components/ui/dropdown-menu"
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
  useImportBackupManifest,
  usePreviewRestore,
  useTriggerBackup,
  useUpdateBackupSettings
} from "@/hooks/useInstanceBackups"
import { useInstanceCapabilities } from "@/hooks/useInstanceCapabilities"
import { useInstances } from "@/hooks/useInstances"
import { usePersistedInstanceSelection } from "@/hooks/usePersistedInstanceSelection"
import { api } from "@/lib/api"
import type {
  BackupCategorySnapshot,
  BackupRun,
  BackupRunKind,
  BackupRunStatus,
  BackupRunsResponse,
  RestoreDiffChange,
  RestoreMode,
  RestorePlan,
  RestoreResult
} from "@/types"
import { useQueries, useQueryClient } from "@tanstack/react-query"

type SettingsFormState = {
  enabled: boolean
  hourlyEnabled: boolean
  dailyEnabled: boolean
  weeklyEnabled: boolean
  monthlyEnabled: boolean
  keepHourly: number
  keepDaily: number
  keepWeekly: number
  keepMonthly: number
  includeCategories: boolean
  includeTags: boolean
}

type SettingsToggleKey =
  | "enabled"
  | "hourlyEnabled"
  | "dailyEnabled"
  | "weeklyEnabled"
  | "monthlyEnabled"
  | "includeCategories"
  | "includeTags"

type SettingsNumericKey = "keepHourly" | "keepDaily" | "keepWeekly" | "keepMonthly"
type ExcludedTorrentMeta = {
  hash: string
  name?: string | null
  category?: string | null
  action: "add" | "update" | "delete"
}

const runKindLabelKeys: Record<BackupRunKind, string> = {
  manual: "manual",
  hourly: "hourly",
  daily: "daily",
  weekly: "weekly",
  monthly: "monthly",
  import: "import",
}

const statusLabelKeys: Record<BackupRunStatus, string> = {
  pending: "pending",
  running: "running",
  success: "success",
  failed: "failed",
  canceled: "canceled",
}

const statusVariants: Record<BackupRunStatus, "default" | "secondary" | "destructive" | "outline"> = {
  pending: "outline",
  running: "secondary",
  success: "default",
  failed: "destructive",
  canceled: "outline",
}

export function InstanceBackups() {
  const { t } = useTranslation("common")
  const tr = (key: string, options?: Record<string, unknown>) => String(t(key as never, options as never))
  const { instances } = useInstances()
  const [selectedInstanceId, setSelectedInstanceId] = usePersistedInstanceSelection("backups")
  const getRunKindLabel = (kind: BackupRunKind) => tr(`instanceBackups.runKinds.${runKindLabelKeys[kind]}`)
  const getRunStatusLabel = (status: BackupRunStatus) => tr(`instanceBackups.runStatuses.${statusLabelKeys[status]}`)

  // Fetch capabilities for all instances to filter out unsupported ones
  const instanceCapabilitiesQueries = useQueries({
    queries: (instances || []).map(inst => ({
      queryKey: ["instance-capabilities", inst.id],
      queryFn: () => api.getInstanceCapabilities(inst.id),
      staleTime: 300000,
    })),
  })

  // Filter instances to only show those that support backups
  const supportedInstances = useMemo(() => {
    if (!instances) return []
    return instances.filter((_inst, index) => {
      const capabilitiesData = instanceCapabilitiesQueries[index]?.data
      // If capabilities haven't loaded yet, assume supported to avoid flickering
      return capabilitiesData?.supportsTorrentExport ?? true
    })
  }, [instances, instanceCapabilitiesQueries])

  const hasInstances = (instances?.length ?? 0) > 0
  const hasSupportedInstances = supportedInstances.length > 0

  useEffect(() => {
    if (selectedInstanceId === undefined) {
      return
    }
    if (!instances) {
      return
    }

    const stillSupported = supportedInstances.some(inst => inst.id === selectedInstanceId)
    if (!stillSupported) {
      setSelectedInstanceId(undefined)
    }
  }, [selectedInstanceId, setSelectedInstanceId, supportedInstances, instances])

  const instanceId = selectedInstanceId

  const handleInstanceSelection = (value: string) => {
    const parsed = parseInt(value, 10)
    if (Number.isNaN(parsed)) {
      setSelectedInstanceId(undefined)
      return
    }

    setSelectedInstanceId(parsed)
  }

  const instance = instances?.find(i => i.id === instanceId)

  // Only load capabilities when an instance is selected
  const { data: capabilities, isLoading: capabilitiesLoading } = useInstanceCapabilities(instanceId, { enabled: !!instanceId })
  const supportsTorrentExport = capabilities?.supportsTorrentExport ?? true

  // Only load data when instance is selected AND supports backups
  const shouldLoadData = !!instanceId && supportsTorrentExport

  // Pagination state
  const [backupsPage, setBackupsPage] = useState(1)

  const { data: settings, isLoading: settingsLoading } = useBackupSettings(instanceId ?? 0, { enabled: shouldLoadData })

  const BACKUPS_PER_PAGE = 10
  const backupsOffset = (backupsPage - 1) * BACKUPS_PER_PAGE
  const { data: runsResponse, isLoading: runsLoading } = useBackupRuns(instanceId ?? 0, {
    limit: BACKUPS_PER_PAGE,
    offset: backupsOffset,
    enabled: shouldLoadData,
  })
  const runs = runsResponse?.runs ?? []
  const queryClient = useQueryClient()
  const { data: firstPageResponse } = useBackupRuns(instanceId ?? 0, {
    limit: BACKUPS_PER_PAGE,
    offset: 0,
    enabled: shouldLoadData && backupsPage > 1,
  })
  const summaryRuns = useMemo(() => {
    if (!instanceId) return runs
    if (backupsPage === 1) return runs
    const freshest = firstPageResponse?.runs ?? queryClient.getQueryData<BackupRunsResponse>([
      "instance-backups",
      instanceId,
      "runs",
      BACKUPS_PER_PAGE,
      0,
    ])?.runs
    return freshest ?? runs
  }, [backupsPage, firstPageResponse, instanceId, queryClient, runs])
  const updateSettings = useUpdateBackupSettings(instanceId ?? 0)
  const triggerBackup = useTriggerBackup(instanceId ?? 0)
  const deleteRun = useDeleteBackupRun(instanceId ?? 0)
  const deleteAllRuns = useDeleteAllBackupRuns(instanceId ?? 0)
  const previewRestore = usePreviewRestore(instanceId ?? 0)
  const executeRestore = useExecuteRestore(instanceId ?? 0)
  const importManifest = useImportBackupManifest(instanceId ?? 0)
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

  const [importDialogOpen, setImportDialogOpen] = useState(false)
  const [importFile, setImportFile] = useState<File | null>(null)

  const backupHistoryRef = useRef<HTMLDivElement>(null)

  const { data: manifest, isLoading: manifestLoading } = useBackupManifest(instanceId ?? 0, manifestRunId, {
    enabled: supportsTorrentExport && !!instanceId,
  })

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
        keepHourly: settings.keepHourly,
        keepDaily: settings.keepDaily,
        keepWeekly: settings.keepWeekly,
        keepMonthly: settings.keepMonthly,
        includeCategories: settings.includeCategories,
        includeTags: settings.includeTags,
      })
    }
  }, [settings])

  const lastRun = summaryRuns.length > 0 ? summaryRuns[0] : undefined
  const hasRuns = summaryRuns.length > 0
  const latestCompletedRun = useMemo(() => {
    return summaryRuns.find(run => run.status === "success")
  }, [summaryRuns])

  const hasActiveCadence = useMemo(() => {
    if (!formState) return false
    return formState.hourlyEnabled || formState.dailyEnabled || formState.weeklyEnabled || formState.monthlyEnabled
  }, [formState])

  const nextScheduleInfo = useMemo<{
    state: "loading" | "ready"
    kind?: BackupRunKind
    timestamp?: string
    status: string
  }>(() => {
    if (settingsLoading || !formState) {
      return {
        state: "loading",
        status: tr("instanceBackups.nextSchedule.loadingSchedule"),
      }
    }

    if (!formState.enabled) {
      return {
        state: "ready",
        timestamp: "—",
        status: tr("instanceBackups.nextSchedule.disabled"),
      }
    }

    const cadenceDefinitions: Array<{
      kind: BackupRunKind
      enabled: boolean
      computeNext: (last: Date) => Date
    }> = [
      { kind: "hourly", enabled: formState.hourlyEnabled, computeNext: last => addIntervalMs(last, 60 * 60 * 1000) },
      { kind: "daily", enabled: formState.dailyEnabled, computeNext: last => addIntervalMs(last, 24 * 60 * 60 * 1000) },
      { kind: "weekly", enabled: formState.weeklyEnabled, computeNext: last => addIntervalMs(last, 7 * 24 * 60 * 60 * 1000) },
      { kind: "monthly", enabled: formState.monthlyEnabled, computeNext: addOneMonth },
    ]
    const enabledCadences = cadenceDefinitions.filter(c => c.enabled)

    if (enabledCadences.length === 0) {
      return {
        state: "ready",
        timestamp: "—",
        status: tr("instanceBackups.nextSchedule.selectCadence"),
      }
    }

    const enabledKinds = new Set(enabledCadences.map(c => c.kind))
    const activeRun = summaryRuns.find(
      run =>
        enabledKinds.has(run.kind) &&
        (run.status === "running" || run.status === "pending")
    )
    if (activeRun) {
      return {
        state: "ready",
        kind: activeRun.kind,
        timestamp: formatDateSafe(activeRun.startedAt ?? activeRun.requestedAt, formatDate),
        status: activeRun.status === "running"
          ? tr("instanceBackups.nextSchedule.running")
          : tr("instanceBackups.nextSchedule.queued"),
      }
    }

    const lastSuccessByKind = buildLastSuccessMap(summaryRuns)
    const now = new Date()

    let best:
      | {
        kind: BackupRunKind
        nextDate?: Date
        hasHistory: boolean
      }
      | null = null

    for (const cadence of enabledCadences) {
      const lastSuccess = lastSuccessByKind[cadence.kind]
      const candidate = {
        kind: cadence.kind,
        nextDate: lastSuccess ? cadence.computeNext(lastSuccess) : undefined,
        hasHistory: !!lastSuccess,
      }

      if (!best) {
        best = candidate
        continue
      }

      const candidateTime = candidate.nextDate?.getTime() ?? now.getTime()
      const bestTime = best.nextDate?.getTime() ?? now.getTime()
      if (candidateTime < bestTime) {
        best = candidate
      }
    }

    if (!best) {
      return {
        state: "ready",
        timestamp: "—",
        status: tr("instanceBackups.nextSchedule.waitingScheduler"),
      }
    }

    if (!best.hasHistory) {
      return {
        state: "ready",
        kind: best.kind,
        timestamp: "—",
        status: tr("instanceBackups.nextSchedule.waitingFirstBackup"),
      }
    }

    if (!best.nextDate) {
      return {
        state: "ready",
        kind: best.kind,
        timestamp: "—",
        status: tr("instanceBackups.nextSchedule.waitingInfo"),
      }
    }

    const overdue = best.nextDate.getTime() <= now.getTime()

    return {
      state: "ready",
      kind: best.kind,
      timestamp: formatDate(best.nextDate),
      status: overdue ? tr("instanceBackups.nextSchedule.overdueRetry") : "",
    }
  }, [formatDate, formState, settingsLoading, summaryRuns, tr])

  // Pagination helpers
  const hasMoreBackups = runsResponse?.hasMore ?? false
  const canGoPrevious = backupsPage > 1
  const canGoNext = hasMoreBackups
  const shouldShowPagination = runsLoading || hasRuns || canGoPrevious

  // Reset page when instance changes
  useEffect(() => {
    setBackupsPage(1)
  }, [instanceId])

  // Scroll to backup history when page changes
  useEffect(() => {
    if (backupsPage > 1 && backupHistoryRef.current) {
      backupHistoryRef.current.scrollIntoView({ behavior: "smooth", block: "start" })
    }
  }, [backupsPage])

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

  const handleSave = async () => {
    if (!formState) return
    try {
      await updateSettings.mutateAsync({
        ...formState,
      })
      toast.success(tr("instanceBackups.toasts.settingsUpdated"))
    } catch (error) {
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedUpdateSettings")
      toast.error(message)
    }
  }

  const handleTrigger = async (kind: BackupRunKind = "manual") => {
    try {
      await triggerBackup.mutateAsync({ kind, requestedBy: "ui" })
      toast.success(tr("instanceBackups.toasts.backupQueued"))
    } catch (error) {
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedQueueBackup")
      toast.error(message)
    }
  }

  const handleDelete = async (run: BackupRun) => {
    try {
      await deleteRun.mutateAsync(run.id)
      toast.success(tr("instanceBackups.toasts.backupDeleted"))
    } catch (error) {
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedDeleteBackup")
      toast.error(message)
    }
  }

  const handleDeleteAll = async () => {
    try {
      await deleteAllRuns.mutateAsync()
      toast.success(tr("instanceBackups.toasts.deletedAllBackups"))
    } catch (error) {
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedDeleteBackups")
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
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedLoadRestorePlan")
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
    toast.info(tr("instanceBackups.toasts.excludedFromRestore", { label }))
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
    toast.success(tr("instanceBackups.toasts.includedInRestore", { label }))
  }

  const handleResetExcluded = async () => {
    if (!restoreTargetRun || restoreExcludedHashes.length === 0) {
      return
    }
    setRestoreExcludedHashes([])
    toast.success(tr("instanceBackups.toasts.includedAllTorrents"))
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
      const message = restoreDryRun
        ? tr("instanceBackups.toasts.restoreDryRunCompleted")
        : tr("instanceBackups.toasts.restoreExecuted")
      toast.success(message)
    } catch (error) {
      const message = error instanceof Error ? error.message : tr("instanceBackups.toasts.failedExecuteRestore")
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

  // Show instance selector when no instance is selected
  if (!instanceId) {
    const selectionHeading = hasSupportedInstances
      ? tr("instanceBackups.selection.heading.selectInstance")
      : hasInstances
        ? tr("instanceBackups.selection.heading.noCompatibleInstances")
        : tr("instanceBackups.selection.heading.connectInstance")

    const selectionMessage = !hasInstances
      ? tr("instanceBackups.selection.message.noInstancesConfigured")
      : hasSupportedInstances
        ? tr("instanceBackups.selection.message.chooseFromDropdown")
        : tr("instanceBackups.selection.message.noSupportedInstances")

    return (
      <TooltipProvider>
        <div className="space-y-6 p-4 lg:p-6">
          <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
            <div className="flex-1 space-y-2">
              <h1 className="text-2xl font-semibold">{tr("instanceBackups.page.title")}</h1>
              <p className="text-sm text-muted-foreground">
                {tr("instanceBackups.page.description")}
              </p>
            </div>
            <div className="flex items-center gap-2">
              {hasSupportedInstances && (
                <Select
                  value={instanceId !== undefined ? instanceId.toString() : undefined}
                  onValueChange={handleInstanceSelection}
                >
                  <SelectTrigger className="!w-[240px] !max-w-[240px]">
                    <div className="flex items-center gap-2 min-w-0 overflow-hidden">
                      <HardDrive className="h-4 w-4 flex-shrink-0" />
                      <span className="truncate">
                        <SelectValue placeholder={tr("instanceBackups.selection.selectInstancePlaceholder")} />
                      </span>
                    </div>
                  </SelectTrigger>
                  <SelectContent>
                    {supportedInstances.map((inst) => (
                      <SelectItem key={inst.id} value={inst.id.toString()}>
                        <div className="flex items-center max-w-40 gap-2">
                          <span className="truncate">{inst.name}</span>
                          <span
                            className={`ml-auto h-2 w-2 rounded-full flex-shrink-0 ${
                              inst.connected ? "bg-green-500" : "bg-red-500"
                            }`}
                          />
                        </div>
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </div>
          </div>
          <Card>
            <CardContent className="flex flex-col items-center justify-center py-12 space-y-4">
              <HardDrive className="h-16 w-16 text-muted-foreground/40" />
              <div className="text-center space-y-2">
                <p className="text-lg font-medium">{selectionHeading}</p>
                <p className="text-sm text-muted-foreground max-w-md">{selectionMessage}</p>
              </div>
              {!hasSupportedInstances && (
                <Button variant="outline" asChild>
                  <Link to="/instances">
                    {tr("instanceBackups.selection.actions.goToInstances")}
                  </Link>
                </Button>
              )}
            </CardContent>
          </Card>
        </div>
      </TooltipProvider>
    )
  }

  if (capabilitiesLoading) {
    return <div className="p-6">{tr("instanceBackups.states.loadingCapabilities")}</div>
  }

  if (!supportsTorrentExport) {
    const versionRaw = capabilities?.webAPIVersion?.trim()
    const reportedVersion = versionRaw && versionRaw.length > 0
      ? versionRaw
      : tr("instanceBackups.capabilities.olderApiVersion")

    return (
      <div className="p-6 space-y-4">
        <Alert variant="destructive">
          <AlertTitle>{tr("instanceBackups.capabilities.unsupportedTitle")}</AlertTitle>
          <AlertDescription>
            {tr("instanceBackups.capabilities.unsupportedDescription", { reportedVersion })}
          </AlertDescription>
        </Alert>
        {instanceId && (
          <Button variant="outline" asChild>
            <Link to="/instances/$instanceId" params={{ instanceId: instanceId.toString() }}>
              {tr("instanceBackups.capabilities.actions.returnToInstanceOverview")}
            </Link>
          </Button>
        )}
      </div>
    )
  }

  return (
    <TooltipProvider>
      <div className="space-y-6 p-4 lg:p-6">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
          <div className="flex-1 space-y-2">
            <h1 className="text-2xl font-semibold">{tr("instanceBackups.page.title")}</h1>
            <p className="text-sm text-muted-foreground">
              {tr("instanceBackups.page.description")}
            </p>
          </div>
          <div className="flex items-center gap-2">
            {/* Instance selector */}
            {hasSupportedInstances && (
              <Select
                value={instanceId !== undefined ? instanceId.toString() : undefined}
                onValueChange={handleInstanceSelection}
              >
                <SelectTrigger className="!w-[240px] !max-w-[240px]">
                  <div className="flex items-center gap-2 min-w-0 overflow-hidden">
                    <HardDrive className="h-4 w-4 flex-shrink-0" />
                    <span className="truncate">
                      <SelectValue placeholder={tr("instanceBackups.selection.selectInstancePlaceholder")} />
                    </span>
                  </div>
                </SelectTrigger>
                <SelectContent>
                  {supportedInstances.map((inst) => (
                    <SelectItem key={inst.id} value={inst.id.toString()}>
                      <div className="flex items-center max-w-40 gap-2">
                        <span className="truncate">{inst.name}</span>
                        <span
                          className={`ml-auto h-2 w-2 rounded-full flex-shrink-0 ${
                            inst.connected ? "bg-green-500" : "bg-red-500"
                          }`}
                        />
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {instanceId && (
              <Button variant="outline" asChild>
                <Link to="/instances/$instanceId" params={{ instanceId: instanceId.toString() }}>
                  {tr("instanceBackups.page.actions.backToTorrents")}
                </Link>
              </Button>
            )}
          </div>
        </div>

        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3 auto-rows-fr">
          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{tr("instanceBackups.summary.lastBackupTitle")}</CardTitle>
              <Clock className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="flex-1 flex flex-col">
              {runsLoading ? (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.states.loading")}</p>
              ) : lastRun ? (
                <div className="flex flex-col flex-1">
                  <div className="space-y-2">
                    <Badge variant={statusVariants[lastRun.status]}>{getRunKindLabel(lastRun.kind)}</Badge>
                    <p className="text-sm">
                      {formatDateSafe(lastRun.completedAt ?? lastRun.requestedAt, formatDate)}
                    </p>
                  </div>
                  <div className="min-h-[44px] flex items-start pt-1 mt-auto">
                    {lastRun.status === "running" && lastRun.progressTotal && lastRun.progressTotal > 0 ? (
                      <div className="space-y-1 w-full">
                        <Progress value={lastRun.progressPercentage ?? 0} className="h-2" />
                        <p className="text-xs text-muted-foreground">
                          {tr("instanceBackups.summary.progressLabel", {
                            current: lastRun.progressCurrent ?? 0,
                            total: lastRun.progressTotal,
                            percentage: (lastRun.progressPercentage ?? 0).toFixed(1),
                          })}
                        </p>
                      </div>
                    ) : (
                      <p className="text-xs text-muted-foreground capitalize">
                        {tr("instanceBackups.summary.statusLabel", { status: getRunStatusLabel(lastRun.status) })}
                      </p>
                    )}
                  </div>
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.summary.noBackupsYet")}</p>
              )}
            </CardContent>
          </Card>

          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{tr("instanceBackups.summary.nextScheduledTitle")}</CardTitle>
              <RefreshCw className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="flex-1 flex flex-col">
              {nextScheduleInfo.state === "loading" ? (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.summary.loadingSchedule")}</p>
              ) : nextScheduleInfo.kind ? (
                <div className="flex flex-col flex-1">
                  <div className="space-y-2">
                    <Badge variant="default">{getRunKindLabel(nextScheduleInfo.kind)}</Badge>
                    <p className="text-sm">{nextScheduleInfo.timestamp ?? "—"}</p>
                  </div>
                  {nextScheduleInfo.status && (
                    <div className="min-h-[44px] flex items-start pt-1 mt-auto">
                      <p className="text-xs text-muted-foreground">{nextScheduleInfo.status}</p>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{nextScheduleInfo.status}</p>
              )}
            </CardContent>
          </Card>

          <Card className="flex flex-col">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{tr("instanceBackups.summary.instanceTitle")}</CardTitle>
              <Download className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <p className="text-sm truncate font-semibold">
                {instance?.name ?? tr("instanceBackups.summary.instanceFallback", { id: instanceId })}
              </p>
              <p className="text-xs text-muted-foreground break-all">{instance?.host}</p>
            </CardContent>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>{tr("instanceBackups.settings.title")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-6">
            {settingsLoading || !formState ? (
              <p className="text-sm text-muted-foreground">{tr("instanceBackups.settings.loading")}</p>
            ) : (
              <div className="space-y-6">
                <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
                  <SettingToggle
                    label={tr("instanceBackups.settings.toggles.enableBackups.label")}
                    description={tr("instanceBackups.settings.toggles.enableBackups.description")}
                    checked={formState.enabled}
                    onCheckedChange={handleToggle("enabled")}
                  />
                  <SettingToggle
                    label={tr("instanceBackups.settings.toggles.includeCategories.label")}
                    description={tr("instanceBackups.settings.toggles.includeCategories.description")}
                    checked={formState.includeCategories}
                    onCheckedChange={handleToggle("includeCategories")}
                  />
                  <SettingToggle
                    label={tr("instanceBackups.settings.toggles.includeTags.label")}
                    description={tr("instanceBackups.settings.toggles.includeTags.description")}
                    checked={formState.includeTags}
                    onCheckedChange={handleToggle("includeTags")}
                  />
                </div>

                <Separator />

                <div className="space-y-4">
                  <div className="space-y-1">
                    <p className="text-sm font-medium">{tr("instanceBackups.settings.cadences.title")}</p>
                    <p className="text-xs text-muted-foreground">
                      {tr("instanceBackups.settings.cadences.description")}
                    </p>
                  </div>
                  <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
                    <ScheduleControl
                      label={tr("instanceBackups.runKinds.hourly")}
                      checked={formState.hourlyEnabled}
                      onCheckedChange={handleToggle("hourlyEnabled")}
                      value={formState.keepHourly}
                      onValueChange={handleNumberChange("keepHourly")}
                      description={tr("instanceBackups.settings.cadences.hourly.description")}
                      tooltip={tr("instanceBackups.settings.cadences.hourly.tooltip")}
                    />
                    <ScheduleControl
                      label={tr("instanceBackups.runKinds.daily")}
                      checked={formState.dailyEnabled}
                      onCheckedChange={handleToggle("dailyEnabled")}
                      value={formState.keepDaily}
                      onValueChange={handleNumberChange("keepDaily")}
                      description={tr("instanceBackups.settings.cadences.daily.description")}
                      tooltip={tr("instanceBackups.settings.cadences.daily.tooltip")}
                    />
                    <ScheduleControl
                      label={tr("instanceBackups.runKinds.weekly")}
                      checked={formState.weeklyEnabled}
                      onCheckedChange={handleToggle("weeklyEnabled")}
                      value={formState.keepWeekly}
                      onValueChange={handleNumberChange("keepWeekly")}
                      description={tr("instanceBackups.settings.cadences.weekly.description")}
                      tooltip={tr("instanceBackups.settings.cadences.weekly.tooltip")}
                    />
                    <ScheduleControl
                      label={tr("instanceBackups.runKinds.monthly")}
                      checked={formState.monthlyEnabled}
                      onCheckedChange={handleToggle("monthlyEnabled")}
                      value={formState.keepMonthly}
                      onValueChange={handleNumberChange("keepMonthly")}
                      description={tr("instanceBackups.settings.cadences.monthly.description")}
                      tooltip={tr("instanceBackups.settings.cadences.monthly.tooltip")}
                    />
                  </div>
                </div>

                <div className="space-y-2">
                  <div className="flex flex-wrap gap-2">
                    <Button onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
                      <ArrowDownToLine className="mr-2 h-4 w-4" /> {tr("instanceBackups.settings.actions.runManualBackup")}
                    </Button>
                    <Button variant="outline" onClick={() => setImportDialogOpen(true)}>
                      <FileText className="mr-2 h-4 w-4" /> {tr("instanceBackups.settings.actions.importBackup")}
                    </Button>
                    <Button
                      variant="outline"
                      onClick={handleSave}
                      disabled={saveDisabled}
                      title={requiresCadenceSelection ? tr("instanceBackups.settings.validation.selectCadenceTitle") : undefined}
                    >
                      {tr("instanceBackups.settings.actions.saveChanges")}
                    </Button>
                  </div>
                  {requiresCadenceSelection ? (
                    <p className="text-xs text-destructive">{tr("instanceBackups.settings.validation.selectCadenceBody")}</p>
                  ) : (
                    <p className="text-xs text-muted-foreground">{tr("instanceBackups.settings.validation.changesApply")}</p>
                  )}
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        <Dialog open={restoreDialogOpen} onOpenChange={(open: boolean) => {
          if (!open) {
            closeRestoreDialog()
          }
        }}>
        <DialogContent className="!w-[96vw] !max-w-6xl !md:w-[90vw] !h-[92vh] md:!h-[80vh] lg:!h-[75vh] overflow-hidden flex flex-col gap-4">
            <DialogHeader>
              <DialogTitle>{tr("instanceBackups.restore.title")}</DialogTitle>
              <DialogDescription>
                {restoreTargetRun
                  ? tr("instanceBackups.restore.runDescription", {
                    id: restoreTargetRun.id,
                    kind: getRunKindLabel(restoreTargetRun.kind),
                  })
                  : tr("instanceBackups.restore.selectBackup")}
              </DialogDescription>
            </DialogHeader>

            <div className="flex flex-wrap items-center gap-4">
              <div className="flex items-center gap-2">
                <span className="text-sm font-medium">{tr("instanceBackups.restore.modeLabel")}</span>
                <div className="flex items-center gap-2">
                  <Select
                    value={restoreMode}
                    onValueChange={handleRestoreModeChange}
                    disabled={restorePlanLoading || !restoreTargetRun}
                  >
                    <SelectTrigger className="w-[180px]">
                      <SelectValue placeholder={tr("instanceBackups.restore.modePlaceholder")} />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="incremental">{tr("instanceBackups.restore.modes.incremental")}</SelectItem>
                      <SelectItem value="overwrite">{tr("instanceBackups.restore.modes.overwrite")}</SelectItem>
                      <SelectItem value="complete">{tr("instanceBackups.restore.modes.complete")}</SelectItem>
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
                      <p className="font-bold">{tr("instanceBackups.restore.modes.incremental")}</p>
                      <p className="mb-2">{tr("instanceBackups.restore.modeHelp.incremental")}</p>
                      <p className="font-bold">{tr("instanceBackups.restore.modes.overwrite")}</p>
                      <p className="mb-2">{tr("instanceBackups.restore.modeHelp.overwrite")}</p>
                      <p className="font-bold">{tr("instanceBackups.restore.modes.complete")}</p>
                      <p>{tr("instanceBackups.restore.modeHelp.complete")}</p>
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
                <Label htmlFor="restore-dry-run">{tr("instanceBackups.restore.options.dryRun")}</Label>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-start-paused"
                  checked={restoreStartPaused}
                  onCheckedChange={setRestoreStartPaused}
                />
                <Label htmlFor="restore-start-paused">{tr("instanceBackups.restore.options.startPaused")}</Label>
              </div>

              <div className="flex items-center gap-2">
                <Switch
                  id="restore-skip-hash-check"
                  checked={restoreSkipHashCheck}
                  onCheckedChange={setRestoreSkipHashCheck}
                />
                <Label htmlFor="restore-skip-hash-check">{tr("instanceBackups.restore.options.skipRecheck")}</Label>
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
                    {tr("instanceBackups.restore.options.autoResumeCompleted")}
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
                        {tr("instanceBackups.restore.options.autoResumeHelp")}
                      </p>
                    </TooltipContent>
                  </Tooltip>
                </div>
              </div>

              <div className="basis-full text-xs text-muted-foreground">
                {restoreSkipHashCheck && restoreAutoResume ? (
                  <span>{tr("instanceBackups.restore.options.autoResumeEnabledSummary")}</span>
                ) : restoreSkipHashCheck ? (
                  <span>{tr("instanceBackups.restore.options.autoResumeDisabledSummary")}</span>
                ) : (
                  <span>{tr("instanceBackups.restore.options.enableSkipRecheckSummary")}</span>
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
                    <Undo2 className="mr-2 h-4 w-4" /> {tr("instanceBackups.restore.actions.reincludeAll")}
                  </Button>
                ) : null}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => restoreTargetRun && loadRestorePlan(restoreMode, restoreTargetRun, restoreExcludedHashes)}
                  disabled={restorePlanLoading || !restoreTargetRun}
                >
                  <ListChecks className="mr-2 h-4 w-4" /> {tr("instanceBackups.restore.actions.refreshPlan")}
                </Button>
                <Button
                  onClick={handleExecuteRestore}
                  disabled={!restoreTargetRun || restorePlanLoading || executeRestore.isPending}
                >
                  {executeRestore.isPending
                    ? tr("instanceBackups.restore.actions.executing")
                    : restoreDryRun
                      ? tr("instanceBackups.restore.actions.runDryRun")
                      : tr("instanceBackups.restore.actions.executeRestore")}
                </Button>
              </div>
            </div>

            <Separator />



            <div className="flex-1 overflow-y-auto space-y-6">
              {!restorePlan && restorePlanLoading ? (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.loadingPlan")}</p>
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
                        <Badge variant="secondary" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.excluded")}</Badge>
                        <span>
                          {tr("instanceBackups.restore.excludedSummary", {
                            count: restoreExcludedHashes.length,
                          })}
                        </span>
                      </div>
                    </div>
                  ) : null}

                  {restorePlanLoading && restorePlan ? (
                    <p className="text-xs text-muted-foreground">{tr("instanceBackups.restore.refreshingPlan")}</p>
                  ) : null}

                  {restorePlanHasActions ? (
                    <>
                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">{tr("instanceBackups.restore.sections.categories")}</h4>
                        {(restorePlan.categories.create?.length ||
                        restorePlan.categories.update?.length ||
                        restorePlan.categories.delete?.length) ? (
                            <div className="space-y-3">
                              {restorePlan.categories.create?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">{tr("instanceBackups.restore.labels.create")}</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.create.map(item => (
                                      <li key={`cat-create-${item.name}`} className="flex flex-wrap items-center gap-2">
                                        <Badge variant="outline" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.create")}</Badge>
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
                                  <p className="text-xs font-medium text-muted-foreground mb-1">{tr("instanceBackups.restore.labels.update")}</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.update.map(item => (
                                      <li key={`cat-update-${item.name}`} className="flex flex-wrap items-center gap-2">
                                        <Badge variant="secondary" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.update")}</Badge>
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
                                  <p className="text-xs font-medium text-muted-foreground mb-1">{tr("instanceBackups.restore.labels.delete")}</p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.categories.delete.map(name => (
                                      <li key={`cat-delete-${name}`} className="flex items-center gap-2">
                                        <Badge variant="destructive" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.delete")}</Badge>
                                        <span>{name}</span>
                                      </li>
                                    ))}
                                  </ul>
                                </div>
                              ) : null}
                            </div>
                          ) : (
                            <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.states.noCategoryChanges")}</p>
                          )}
                      </section>

                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">{tr("instanceBackups.restore.sections.tags")}</h4>
                        {(restorePlan.tags.create?.length || restorePlan.tags.delete?.length) ? (
                          <div className="space-y-3">
                            {restorePlan.tags.create?.length ? (
                              <div>
                                <p className="text-xs font-medium text-muted-foreground mb-1">{tr("instanceBackups.restore.labels.create")}</p>
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
                                <p className="text-xs font-medium text-muted-foreground mb-1">{tr("instanceBackups.restore.labels.delete")}</p>
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
                          <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.states.noTagChanges")}</p>
                        )}
                      </section>

                      <section className="space-y-2">
                        <h4 className="text-sm font-semibold">{tr("instanceBackups.restore.sections.torrents")}</h4>
                        {(restorePlan.torrents.add?.length ||
                        restorePlan.torrents.update?.length ||
                        restorePlan.torrents.delete?.length) ? (
                            <div className="space-y-4">
                              {restorePlan.torrents.add?.length ? (
                                <div>
                                  <p className="text-xs font-medium text-muted-foreground mb-1">
                                    {tr("instanceBackups.restore.counts.add", { count: restorePlan.torrents.add.length })}
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
                                            <Badge variant="outline" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.add")}</Badge>
                                            <span className="font-medium truncate">
                                              {item.manifest.name || hash}
                                            </span>
                                            <code className="text-xs text-muted-foreground">{hash}</code>
                                            {item.manifest.category ? (
                                              <span className="text-xs text-muted-foreground">• {item.manifest.category}</span>
                                            ) : null}
                                            {isExcluded ? (
                                              <Badge variant="secondary" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.excluded")}</Badge>
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
                                            aria-label={tr("instanceBackups.restore.actions.toggleTorrentInRestoreAria", {
                                              action: isExcluded
                                                ? tr("instanceBackups.restore.actions.include")
                                                : tr("instanceBackups.restore.actions.exclude"),
                                              name: item.manifest.name || hash,
                                            })}
                                          >
                                            {isExcluded ? (
                                              <>
                                                <Undo2 className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.include")}
                                              </>
                                            ) : (
                                              <>
                                                <CircleX className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.exclude")}
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
                                    {tr("instanceBackups.restore.counts.update", { count: restorePlan.torrents.update.length })}
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
                                                  <Badge variant="secondary" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.excluded")}</Badge>
                                                ) : null}
                                              </div>
                                              <span className="text-xs text-muted-foreground">
                                                {tr("instanceBackups.restore.currentCategory", {
                                                  category: update.current.category || "—",
                                                })}
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
                                                aria-label={tr("instanceBackups.restore.actions.toggleTorrentInRestoreAria", {
                                                  action: isExcluded
                                                    ? tr("instanceBackups.restore.actions.include")
                                                    : tr("instanceBackups.restore.actions.exclude"),
                                                  name: update.desired.name || update.current.name || update.hash,
                                                })}
                                              >
                                                {isExcluded ? (
                                                  <>
                                                    <Undo2 className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.include")}
                                                  </>
                                                ) : (
                                                  <>
                                                    <CircleX className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.exclude")}
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
                                                  {change.supported
                                                    ? tr("instanceBackups.restore.labels.auto")
                                                    : tr("instanceBackups.restore.labels.manual")}
                                                </Badge>
                                                <span className="font-medium capitalize">{humanizeChangeField(change.field, tr)}</span>
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
                                    {tr("instanceBackups.restore.counts.delete", { count: restorePlan.torrents.delete.length })}
                                  </p>
                                  <ul className="space-y-1 text-sm">
                                    {restorePlan.torrents.delete.map(hash => {
                                      const isExcluded = restoreExcludedHashes.includes(hash)
                                      return (
                                        <li
                                          key={`torrent-delete-${hash}`}
                                          className={`flex flex-wrap items-center gap-2 rounded-md px-2 py-1 ${isExcluded ? "bg-muted/40 text-muted-foreground" : ""}`}
                                        >
                                          <Badge variant="destructive" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.delete")}</Badge>
                                          <code className="text-xs text-muted-foreground">{hash}</code>
                                          {isExcluded ? (
                                            <Badge variant="secondary" className="text-[10px] uppercase">{tr("instanceBackups.restore.labels.excluded")}</Badge>
                                          ) : null}
                                          <Button
                                            variant="ghost"
                                            size="sm"
                                            onClick={() => (isExcluded? handleIncludeTorrent(hash, { hash, action: "delete" }): handleExcludeTorrent(hash, { hash, action: "delete" })
                                            )}
                                            disabled={restorePlanLoading}
                                            aria-label={tr("instanceBackups.restore.actions.toggleTorrentInRestoreAria", {
                                              action: isExcluded
                                                ? tr("instanceBackups.restore.actions.include")
                                                : tr("instanceBackups.restore.actions.exclude"),
                                              name: hash,
                                            })}
                                          >
                                            {isExcluded ? (
                                              <>
                                                <Undo2 className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.include")}
                                              </>
                                            ) : (
                                              <>
                                                <CircleX className="mr-1 h-3 w-3" /> {tr("instanceBackups.restore.actions.exclude")}
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
                            <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.states.noTorrentChanges")}</p>
                          )}
                      </section>
                    </>
                  ) : (
                    <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.states.noChangesRequired")}</p>
                  )}

                  {restoreUnsupportedChanges.length > 0 && (
                    <div className="rounded-md border border-amber-200 bg-amber-50 p-3 space-y-2 text-sm text-amber-900">
                      <p className="font-medium">{tr("instanceBackups.restore.states.manualFollowUpRequired")}</p>
                      <ul className="list-disc pl-5 space-y-1">
                        {restoreUnsupportedChanges.map(({ hash, change }, index) => (
                          <li key={`unsupported-${hash}-${change.field}-${index}`}>
                            <code className="text-xs">{hash}</code> • {humanizeChangeField(change.field, tr)}
                            {change.message ? <span> — {change.message}</span> : null}
                          </li>
                        ))}
                      </ul>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.restore.selectBackup")}</p>
              )}
            </div>

            {restoreResult && (
              <div className="rounded-md border p-4 space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <h4 className="text-sm font-semibold">{tr("instanceBackups.restore.lastExecutionTitle")}</h4>
                  <Badge variant={restoreResult.dryRun ? "outline" : "default"} className="text-xs">
                    {restoreResult.dryRun
                      ? tr("instanceBackups.restore.results.dryRun")
                      : tr("instanceBackups.restore.results.applied")}
                  </Badge>
                </div>
                <p className="text-xs text-muted-foreground">
                  {tr("instanceBackups.restore.results.modeLabel", {
                    mode: tr(`instanceBackups.restore.modes.${restoreResult.mode}`),
                  })}
                </p>
                <div className="grid gap-3 md:grid-cols-3 text-sm">
                  <div>
                    <p className="font-medium">{tr("instanceBackups.restore.sections.categories")}</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.categories.created)} / Δ{countItems(restoreResult.applied.categories.updated)} / −{countItems(restoreResult.applied.categories.deleted)}
                    </p>
                  </div>
                  <div>
                    <p className="font-medium">{tr("instanceBackups.restore.sections.tags")}</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.tags.created)} / −{countItems(restoreResult.applied.tags.deleted)}
                    </p>
                  </div>
                  <div>
                    <p className="font-medium">{tr("instanceBackups.restore.sections.torrents")}</p>
                    <p className="text-xs text-muted-foreground">
                      +{countItems(restoreResult.applied.torrents.added)} / Δ{countItems(restoreResult.applied.torrents.updated)} / −{countItems(restoreResult.applied.torrents.deleted)}
                    </p>
                  </div>
                </div>
                {restoreResult.warnings?.length ? (
                  <div className="rounded-md border border-amber-200 bg-amber-50 p-3 space-y-1 text-sm text-amber-900">
                    <p className="font-medium">{tr("instanceBackups.restore.results.warnings")}</p>
                    <ul className="list-disc pl-5 space-y-1">
                      {restoreResult.warnings.map((warning, index) => (
                        <li key={`restore-warning-${index}`}>{warning}</li>
                      ))}
                    </ul>
                  </div>
                ) : null}
                {restoreResult.errors?.length ? (
                  <div className="rounded-md border border-destructive/40 bg-destructive/10 p-3 space-y-1 text-sm text-destructive">
                    <p className="font-medium">{tr("instanceBackups.restore.results.errors")}</p>
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

        <Dialog open={importDialogOpen} onOpenChange={(open: boolean) => {
          setImportDialogOpen(open)
          if (!open) {
            setImportFile(null)
          }
        }}>
          <DialogContent className="sm:max-w-md max-h-[90dvh] flex flex-col">
            <DialogHeader className="flex-shrink-0">
              <DialogTitle>{tr("instanceBackups.import.title")}</DialogTitle>
              <DialogDescription>
                {tr("instanceBackups.import.description")}
              </DialogDescription>
            </DialogHeader>
            <div className="flex-1 overflow-y-auto min-h-0 space-y-4">
              <div className="space-y-2">
                <Label htmlFor="manifest-file">{tr("instanceBackups.import.fileLabel")}</Label>
                <Input
                  id="manifest-file"
                  type="file"
                  accept=".json,.zip,.tar,.tgz,.zst,.br,.xz,application/json,application/zip,application/x-tar,application/gzip,application/zstd,application/x-brotli,application/x-xz"
                  onChange={(e) => {
                    const file = e.target.files?.[0]
                    setImportFile(file || null)
                  }}
                />
                {importFile && (
                  <p className="text-sm text-muted-foreground">
                    {tr("instanceBackups.import.selectedFile", {
                      name: importFile.name,
                      sizeKb: (importFile.size / 1024).toFixed(1),
                    })}
                  </p>
                )}
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setImportDialogOpen(false)}>
                {tr("instanceBackups.actions.cancel")}
              </Button>
              <Button
                onClick={async () => {
                  if (!importFile) return

                  try {
                    await importManifest.mutateAsync(importFile)
                    toast.success(tr("instanceBackups.toasts.backupImported"))
                    setImportDialogOpen(false)
                    setImportFile(null)
                  } catch (error) {
                    toast.error(tr("instanceBackups.toasts.failedImportBackup"))
                    console.error("Import error:", error)
                  }
                }}
                disabled={!importFile || importManifest.isPending}
              >
                {importManifest.isPending
                  ? tr("instanceBackups.import.importing")
                  : tr("instanceBackups.import.importAction")}
              </Button>
            </div>
          </DialogContent>
        </Dialog>

        <div ref={backupHistoryRef}>
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>{tr("instanceBackups.history.title")}</CardTitle>
                <div className="flex items-center gap-2">
                  <AlertDialog>
                    <AlertDialogTrigger asChild>
                      <Button
                        variant="destructive"
                        size="sm"
                        disabled={deleteAllRuns.isPending || runsLoading || !hasRuns}
                      >
                        <Trash className="mr-2 h-4 w-4" /> {tr("instanceBackups.history.actions.deleteAll")}
                      </Button>
                    </AlertDialogTrigger>
                    <AlertDialogContent>
                      <AlertDialogHeader>
                        <AlertDialogTitle>{tr("instanceBackups.history.deleteAllDialog.title")}</AlertDialogTitle>
                        <AlertDialogDescription>
                          {tr("instanceBackups.history.deleteAllDialog.description")}
                        </AlertDialogDescription>
                      </AlertDialogHeader>
                      <AlertDialogFooter>
                        <AlertDialogCancel>{tr("instanceBackups.actions.cancel")}</AlertDialogCancel>
                        <AlertDialogAction onClick={handleDeleteAll} disabled={deleteAllRuns.isPending}>
                          {tr("instanceBackups.history.actions.deleteAll")}
                        </AlertDialogAction>
                      </AlertDialogFooter>
                    </AlertDialogContent>
                  </AlertDialog>
                  <Button variant="outline" size="sm" onClick={() => handleTrigger("manual")} disabled={triggerBackup.isPending}>
                    <ArrowDownToLine className="mr-2 h-4 w-4" /> {tr("instanceBackups.history.actions.queueBackup")}
                  </Button>
                  <Button
                    variant="default"
                    size="sm"
                    onClick={() => latestCompletedRun && openRestore(latestCompletedRun)}
                    disabled={!latestCompletedRun || executeRestore.isPending || runsLoading}
                  >
                    <Undo2 className="mr-2 h-4 w-4" /> {tr("instanceBackups.history.actions.restoreFromLatest")}
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent className="space-y-4">
              {runsLoading ? (
                <p className="text-sm text-muted-foreground">{tr("instanceBackups.history.loadingBackups")}</p>
              ) : runs.length > 0 ? (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{tr("instanceBackups.history.columns.type")}</TableHead>
                      <TableHead>{tr("instanceBackups.history.columns.status")}</TableHead>
                      <TableHead className="w-40">{tr("instanceBackups.history.columns.requested")}</TableHead>
                      <TableHead className="w-40">{tr("instanceBackups.history.columns.completed")}</TableHead>
                      <TableHead className="text-right">{tr("instanceBackups.history.columns.torrents")}</TableHead>
                      <TableHead className="text-right">{tr("instanceBackups.history.columns.size")}</TableHead>
                      <TableHead className="text-right">{tr("instanceBackups.history.columns.actions")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {runs.map(run => (
                      <TableRow key={run.id}>
                        <TableCell className="font-medium">{getRunKindLabel(run.kind)}</TableCell>
                        <TableCell>
                          {run.status === "running" && run.progressTotal && run.progressTotal > 0 ? (
                            <div className="space-y-1 min-w-[200px]">
                              <Progress value={run.progressPercentage ?? 0} className="h-2" />
                              <p className="text-xs text-muted-foreground">
                                {tr("instanceBackups.summary.progressLabel", {
                                  current: run.progressCurrent ?? 0,
                                  total: run.progressTotal,
                                  percentage: (run.progressPercentage ?? 0).toFixed(1),
                                })}
                              </p>
                            </div>
                          ) : (
                            <Badge variant={statusVariants[run.status]} className="capitalize">
                              {getRunStatusLabel(run.status)}
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell>{formatDateSafe(run.requestedAt, formatDate)}</TableCell>
                        <TableCell>{formatDateSafe(run.completedAt, formatDate)}</TableCell>
                        <TableCell className="text-right">{run.torrentCount}</TableCell>
                        <TableCell className="text-right">{formatBytes(run.totalBytes)}</TableCell>
                        <TableCell className="flex justify-end gap-2">
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon"
                                onClick={() => openManifest(run.id)}
                                aria-label={tr("instanceBackups.history.actions.viewManifestAria")}
                              >
                                <FileText className="h-4 w-4" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{tr("instanceBackups.history.tooltips.viewManifest")}</TooltipContent>
                          </Tooltip>
                          <Tooltip>
                            <TooltipTrigger asChild>
                              <Button
                                variant="ghost"
                                size="icon"
                                onClick={() => openRestore(run)}
                                aria-label={tr("instanceBackups.history.actions.restoreFromBackupAria")}
                              >
                                <Undo2 className="h-4 w-4" />
                              </Button>
                            </TooltipTrigger>
                            <TooltipContent>{tr("instanceBackups.history.tooltips.restoreFromBackup")}</TooltipContent>
                          </Tooltip>
                          {run.status === "success" && run.torrentCount > 0 ? (
                            <Tooltip>
                              <DropdownMenu>
                                <TooltipTrigger asChild>
                                  <DropdownMenuTrigger asChild>
                                    <Button variant="ghost" size="icon" aria-label={tr("instanceBackups.history.actions.downloadBackupAria")}>
                                      <Download className="h-4 w-4" />
                                    </Button>
                                  </DropdownMenuTrigger>
                                </TooltipTrigger>
                                <TooltipContent>{tr("instanceBackups.history.tooltips.downloadBackupArchive")}</TooltipContent>
                                <DropdownMenuContent align="end">
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "zip")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.zip")}
                                    </a>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "tar.gz")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.tarGz")}
                                    </a>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "tar.zst")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.tarZst")}
                                    </a>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "tar.br")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.tarBr")}
                                    </a>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "tar.xz")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.tarXz")}
                                    </a>
                                  </DropdownMenuItem>
                                  <DropdownMenuItem asChild>
                                    <a
                                      href={api.getBackupDownloadUrl(instanceId!, run.id, "tar")}
                                      rel="noreferrer"
                                      download
                                    >
                                      {tr("instanceBackups.history.downloadFormats.tar")}
                                    </a>
                                  </DropdownMenuItem>
                                </DropdownMenuContent>
                              </DropdownMenu>
                            </Tooltip>
                          ) : (
                            <Button variant="ghost" size="icon" disabled aria-label={tr("instanceBackups.history.actions.downloadUnavailableAria")}>
                              <Download className="h-4 w-4" />
                            </Button>
                          )}
                          <Tooltip>
                            <AlertDialog>
                              <TooltipTrigger asChild>
                                <AlertDialogTrigger asChild>
                                  <Button variant="ghost" size="icon" aria-label={tr("instanceBackups.history.actions.deleteBackupAria")}>
                                    <Trash className="h-4 w-4" />
                                  </Button>
                                </AlertDialogTrigger>
                              </TooltipTrigger>
                              <TooltipContent>{tr("instanceBackups.history.tooltips.deleteBackup")}</TooltipContent>
                              <AlertDialogContent>
                                <AlertDialogHeader>
                                  <AlertDialogTitle>{tr("instanceBackups.history.deleteDialog.title")}</AlertDialogTitle>
                                  <AlertDialogDescription>
                                    {tr("instanceBackups.history.deleteDialog.description")}
                                  </AlertDialogDescription>
                                </AlertDialogHeader>
                                <AlertDialogFooter>
                                  <AlertDialogCancel>{tr("instanceBackups.actions.cancel")}</AlertDialogCancel>
                                  <AlertDialogAction onClick={() => handleDelete(run)}>
                                    {tr("instanceBackups.actions.delete")}
                                  </AlertDialogAction>
                                </AlertDialogFooter>
                              </AlertDialogContent>
                            </AlertDialog>
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              ) : (
                <p className="text-sm text-muted-foreground">
                  {canGoPrevious
                    ? tr("instanceBackups.history.empty.currentPage")
                    : tr("instanceBackups.history.empty.noneCreated")}
                </p>
              )}
              {shouldShowPagination && (
                <div className="flex items-center justify-between pt-4">
                  <p className="text-sm text-muted-foreground">
                    {tr("instanceBackups.history.pagination.pageSummary", {
                      page: backupsPage,
                      count: runs.length,
                    })}
                  </p>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setBackupsPage(p => p - 1)}
                      disabled={!canGoPrevious || runsLoading}
                    >
                      <ChevronLeft className="h-4 w-4 mr-1" />
                      {tr("instanceBackups.history.pagination.previous")}
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => setBackupsPage(p => p + 1)}
                      disabled={!canGoNext || runsLoading}
                    >
                      {tr("instanceBackups.history.pagination.next")}
                      <ChevronRight className="h-4 w-4 ml-1" />
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </div>

        <Dialog open={manifestOpen} onOpenChange={(open: boolean) => {
          setManifestOpen(open)
          if (!open) {
            setManifestRunId(undefined)
          }
        }}>
          <DialogContent className="!w-[96vw] !max-w-7xl !md:w-[90vw] !h-[92vh] md:!h-[80vh] lg:!h-[75vh] overflow-hidden flex flex-col">
            <DialogHeader>
              <DialogTitle>{tr("instanceBackups.manifest.title")}</DialogTitle>
              <DialogDescription>
                {manifestRunId
                  ? tr("instanceBackups.manifest.runDescription", { id: manifestRunId })
                  : tr("instanceBackups.manifest.selectBackup")}
              </DialogDescription>
            </DialogHeader>
            {manifestLoading ? (
              <p className="text-sm text-muted-foreground">{tr("instanceBackups.manifest.loading")}</p>
            ) : manifest ? (
              <div className="space-y-4 flex-1 flex flex-col min-h-0">
                <div className="space-y-3 text-sm">
                  <div className="flex flex-wrap gap-3 text-muted-foreground">
                    <span className="font-medium text-foreground">
                      {tr("instanceBackups.manifest.summary.torrents", { count: manifest.torrentCount })}
                    </span>
                    {manifestCategoryEntries.length > 0 && (
                      <span>{tr("instanceBackups.manifest.summary.categories", { count: manifestCategoryEntries.length })}</span>
                    )}
                    {manifestTags.length > 0 && (
                      <span>{tr("instanceBackups.manifest.summary.tags", { count: manifestTags.length })}</span>
                    )}
                    <span>{tr("instanceBackups.manifest.summary.generated", { date: formatDateSafe(manifest.generatedAt, formatDate) })}</span>
                  </div>
                  {displayedCategoryEntries.length > 0 && (
                    <div>
                      <p className="font-medium text-foreground mb-2">{tr("instanceBackups.manifest.sections.categories")}</p>
                      <div className="flex flex-wrap gap-2">
                        {displayedCategoryEntries.map(([name, snapshot]) => (
                          <Badge key={name} variant="secondary" title={snapshot?.savePath ?? undefined}>
                            {name}
                          </Badge>
                        ))}
                        {remainingCategoryCount > 0 && (
                          <Badge variant="outline">{tr("instanceBackups.manifest.summary.more", { count: remainingCategoryCount })}</Badge>
                        )}
                      </div>
                    </div>
                  )}
                  {displayedTags.length > 0 && (
                    <div>
                      <p className="font-medium text-foreground mb-2">{tr("instanceBackups.manifest.sections.tags")}</p>
                      <div className="flex flex-wrap gap-2">
                        {displayedTags.map(tag => (
                          <Badge key={tag} variant="outline">{tag}</Badge>
                        ))}
                        {remainingTagCount > 0 && (
                          <Badge variant="outline">{tr("instanceBackups.manifest.summary.more", { count: remainingTagCount })}</Badge>
                        )}
                      </div>
                    </div>
                  )}
                </div>
                <div className="flex w-full justify-end">
                  <Input
                    value={manifestSearch}
                    onChange={event => setManifestSearch(event.target.value)}
                    placeholder={tr("instanceBackups.manifest.searchPlaceholder")}
                    className="w-full sm:w-[18rem] md:w-[16rem]"
                    aria-label={tr("instanceBackups.manifest.searchAriaLabel")}
                  />
                </div>
                <div className="flex-1 overflow-auto pr-1">
                  <Table className="min-w-[640px] w-full">
                    <TableHeader className="sticky top-0 z-10 bg-background">
                      <TableRow>
                        <TableHead>{tr("instanceBackups.manifest.columns.name")}</TableHead>
                        <TableHead>{tr("instanceBackups.manifest.columns.category")}</TableHead>
                        <TableHead>{tr("instanceBackups.manifest.columns.tags")}</TableHead>
                        <TableHead className="text-right">{tr("instanceBackups.manifest.columns.size")}</TableHead>
                        <TableHead className="text-right">{tr("instanceBackups.manifest.columns.cachedTorrent")}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredManifestItems.length > 0 ? (
                        filteredManifestItems.map(item => (
                          <TableRow key={item.hash}>
                            <TableCell className="font-medium !max-w-md truncate">{item.name}</TableCell>
                            <TableCell>{item.category ?? "—"}</TableCell>
                            <TableCell className="max-w-sm truncate">{item.tags && item.tags.length > 0 ? item.tags.join(", ") : "—"}</TableCell>
                            <TableCell className="text-right">{formatBytes(item.sizeBytes)}</TableCell>
                            <TableCell className="text-right">
                              {item.torrentBlob && manifestRunId ? (
                                <Button variant="ghost" size="icon" asChild>
                                  <a
                                    href={api.getBackupTorrentDownloadUrl(instanceId!, manifestRunId, item.hash)}
                                    download
                                    aria-label={tr("instanceBackups.manifest.downloadTorrentAria", { name: item.name })}
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
                            {manifestSearch
                              ? tr("instanceBackups.manifest.empty.filtered", { query: manifestSearch })
                              : tr("instanceBackups.manifest.empty.none")}
                          </TableCell>
                        </TableRow>
                      )}
                    </TableBody>
                  </Table>
                </div>
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">{tr("instanceBackups.manifest.unavailable")}</p>
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

function humanizeChangeField(
  field: string,
  tr?: (key: string, options?: Record<string, unknown>) => string
): string {
  if (tr) {
    const translated = tr(`instanceBackups.restore.changeFields.${field}`)
    if (translated !== `instanceBackups.restore.changeFields.${field}`) {
      return translated
    }
  }
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

function buildLastSuccessMap(runs: BackupRun[]): Partial<Record<BackupRunKind, Date>> {
  const map: Partial<Record<BackupRunKind, Date>> = {}

  for (const run of runs) {
    if (run.status !== "success") continue
    if (map[run.kind]) continue
    const timestamp = parseDate(run.completedAt ?? run.requestedAt)
    if (timestamp) {
      map[run.kind] = timestamp
    }
  }

  return map
}

function addIntervalMs(date: Date, interval: number): Date {
  return new Date(date.getTime() + interval)
}

function addOneMonth(date: Date): Date {
  const next = new Date(date.getTime())
  next.setUTCMonth(next.getUTCMonth() + 1)
  return next
}

function parseDate(value?: string | null): Date | undefined {
  if (!value) return undefined
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) {
    return undefined
  }
  return parsed
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
