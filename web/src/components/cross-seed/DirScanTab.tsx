/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useState } from "react"
import { Trans, useTranslation } from "react-i18next"
import { toast } from "sonner"
import {
  AlertTriangle,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Clock,
  FolderSearch,
  Info,
  Loader2,
  Pause,
  Play,
  Plus,
  RotateCcw,
  Settings2,
  Trash2,
  XCircle
} from "lucide-react"

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle
} from "@/components/ui/alert-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle
} from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { MultiSelect } from "@/components/ui/multi-select"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow
} from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useDateTimeFormatters } from "@/hooks/useDateTimeFormatters"
import { useInstanceMetadata } from "@/hooks/useInstanceMetadata"
import i18n from "@/i18n"
import { formatRelativeTime } from "@/lib/dateTimeUtils"
import { api } from "@/lib/api"
import { buildCategorySelectOptions, buildTagSelectOptions } from "@/lib/category-utils"
import {
  isRunActive,
  useCancelDirScan,
  useCreateDirScanDirectory,
  useDeleteDirScanDirectory,
  useDirScanDirectories,
  useDirScanRunInjections,
  useDirScanRuns,
  useDirScanSettings,
  useDirScanStatus,
  useResetDirScanFiles,
  useTriggerDirScan,
  useUpdateDirScanDirectory,
  useUpdateDirScanSettings
} from "@/hooks/useDirScan"
import type {
  DirScanDirectory,
  DirScanDirectoryCreate,
  DirScanMatchMode,
  DirScanRun,
  DirScanRunInjection,
  DirScanRunStatus,
  Instance
} from "@/types"
import { useQueries } from "@tanstack/react-query"

interface DirScanTabProps {
  instances: Instance[]
}

// Helper to format relative time from a string or Date
function formatRelativeTimeStr(date: string | Date): string {
  return formatRelativeTime(typeof date === "string" ? new Date(date) : date)
}

function getRunDiscoveredFiles(run: DirScanRun): number {
  return run.filesFound + run.filesSkipped
}

function getRunFilesLabel(run: DirScanRun): string {
  return i18n.t("dirScan.eligible", { ns: "crossseed", count: run.filesFound })
}

function RunFilesBadge({ run }: { run: DirScanRun }) {
  const { t } = useTranslation("crossseed")
  const discovered = getRunDiscoveredFiles(run)
  const showDetails = discovered > run.filesFound

  if (!showDetails) {
    return <span className="text-muted-foreground">{getRunFilesLabel(run)}</span>
  }

  return (
    <Tooltip>
      <TooltipTrigger className="cursor-default text-muted-foreground">
        {getRunFilesLabel(run)}
      </TooltipTrigger>
      <TooltipContent>
        {t("dirScan.discovered", { count: discovered, skipped: run.filesSkipped })}
      </TooltipContent>
    </Tooltip>
  )
}

export function DirScanTab({ instances }: DirScanTabProps) {
  const { t } = useTranslation("crossseed")
  const { formatISOTimestamp } = useDateTimeFormatters()
  const [selectedDirectoryId, setSelectedDirectoryId] = useState<number | null>(null)
  const [showSettingsDialog, setShowSettingsDialog] = useState(false)
  const [showDirectoryDialog, setShowDirectoryDialog] = useState(false)
  const [editingDirectory, setEditingDirectory] = useState<DirScanDirectory | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)

  // Queries
  const { data: settings, isLoading: settingsLoading } = useDirScanSettings()
  const { data: directories = [], isLoading: directoriesLoading } = useDirScanDirectories()
  const updateSettings = useUpdateDirScanSettings()

  // Get status for each directory
  const directoryWithLocalFs = useMemo(
    () => instances.filter((i) => i.hasLocalFilesystemAccess),
    [instances]
  )

  const handleToggleEnabled = useCallback(
    (enabled: boolean) => {
      updateSettings.mutate(
        { enabled },
        {
          onSuccess: () => {
            toast.success(enabled ? t("dirScan.toast.scannerEnabled") : t("dirScan.toast.scannerDisabled"))
          },
          onError: (error) => {
            toast.error(t("dirScan.toast.failedToUpdateSettings", { error: error.message }))
          },
        }
      )
    },
    [t, updateSettings]
  )

  const handleAddDirectory = useCallback(() => {
    setEditingDirectory(null)
    setShowDirectoryDialog(true)
  }, [])

  const handleEditDirectory = useCallback((directory: DirScanDirectory) => {
    setEditingDirectory(directory)
    setShowDirectoryDialog(true)
  }, [])

  if (settingsLoading || directoriesLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="size-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header with Enable Switch */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <div>
              <CardTitle className="flex items-center gap-2">
                <FolderSearch className="size-5" />
                {t("dirScan.title")}
              </CardTitle>
              <CardDescription>
                {t("dirScan.description")}
              </CardDescription>
            </div>
            <div className="flex items-center gap-4">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowSettingsDialog(true)}
              >
                <Settings2 className="size-4 mr-2" />
                {t("dirScan.settings")}
              </Button>
              <Label htmlFor="dir-scan-enabled" className="flex items-center gap-2">
                <Switch
                  id="dir-scan-enabled"
                  checked={settings?.enabled ?? false}
                  onCheckedChange={handleToggleEnabled}
                  disabled={updateSettings.isPending}
                />
                {settings?.enabled ? t("dirScan.enabled") : t("dirScan.disabled")}
              </Label>
            </div>
          </div>
        </CardHeader>
      </Card>

      {/* No Local Access Warning */}
      {directoryWithLocalFs.length === 0 && (
        <Card className="border-yellow-500/50 bg-yellow-500/5">
          <CardContent className="flex items-center gap-3 py-4">
            <AlertTriangle className="size-5 text-yellow-500" />
            <p className="text-sm text-muted-foreground">
              {t("dirScan.noLocalAccessWarning")}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Directories List */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{t("dirScan.scanDirectories")}</CardTitle>
            <CardDescription>
              {t("dirScan.scanDirectoriesDescription")}
            </CardDescription>
          </div>
          <Button
            onClick={handleAddDirectory}
            disabled={directoryWithLocalFs.length === 0}
          >
            <Plus className="size-4 mr-2" />
            {t("dirScan.addDirectory")}
          </Button>
        </CardHeader>
        <CardContent>
          {directories.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-center text-muted-foreground">
              <FolderSearch className="size-12 mb-4 opacity-50" />
              <p>{t("dirScan.noDirectories")}</p>
              <p className="text-sm">{t("dirScan.addDirectoryToStart")}</p>
            </div>
          ) : (
            <div className="space-y-4">
              {directories.map((directory) => (
                <DirectoryCard
                  key={directory.id}
                  directory={directory}
                  instances={instances}
                  onEdit={handleEditDirectory}
                  onDelete={setDeleteConfirmId}
                  onSelect={setSelectedDirectoryId}
                  isSelected={selectedDirectoryId === directory.id}
                  formatRelativeTime={formatRelativeTimeStr}
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Selected Directory Details */}
      {selectedDirectoryId && (
        <DirectoryDetails
          directoryId={selectedDirectoryId}
          formatDateTime={formatISOTimestamp}
          formatRelativeTime={formatRelativeTimeStr}
        />
      )}

      {/* Settings Dialog */}
      <SettingsDialog
        open={showSettingsDialog}
        onOpenChange={setShowSettingsDialog}
        settings={settings}
        instances={directoryWithLocalFs}
      />

      {/* Directory Dialog */}
      <DirectoryDialog
        open={showDirectoryDialog}
        onOpenChange={setShowDirectoryDialog}
        directory={editingDirectory}
        instances={directoryWithLocalFs}
      />

      {/* Delete Confirmation */}
      <DeleteDirectoryDialog
        directoryId={deleteConfirmId}
        onOpenChange={(open) => !open && setDeleteConfirmId(null)}
      />
    </div>
  )
}

// Directory Card Component
interface DirectoryCardProps {
  directory: DirScanDirectory
  instances: Instance[]
  onEdit: (directory: DirScanDirectory) => void
  onDelete: (id: number) => void
  onSelect: (id: number | null) => void
  isSelected: boolean
  formatRelativeTime: (date: string | Date) => string
}

function DirectoryCard({
  directory,
  instances,
  onEdit,
  onDelete,
  onSelect,
  isSelected,
  formatRelativeTime,
}: DirectoryCardProps) {
  const { t } = useTranslation("crossseed")
  const { data: status } = useDirScanStatus(directory.id)
  const triggerScan = useTriggerDirScan(directory.id)
  const cancelScan = useCancelDirScan(directory.id)

  const targetInstance = useMemo(
    () => instances.find((i) => i.id === directory.targetInstanceId),
    [instances, directory.targetInstanceId]
  )

  const isActive = useMemo(() => {
    if (!status || ("status" in status && status.status === "idle")) return false
    return isRunActive(status as DirScanRun)
  }, [status])

  const handleTrigger = useCallback(() => {
    triggerScan.mutate(undefined, {
      onSuccess: () => toast.success(t("dirScan.toast.scanStarted")),
      onError: (error) => toast.error(t("dirScan.toast.scanStartFailed", { error: error.message })),
    })
  }, [t, triggerScan])

  const handleCancel = useCallback(() => {
    cancelScan.mutate(undefined, {
      onSuccess: () => toast.success(t("dirScan.toast.scanCanceled")),
      onError: (error) => toast.error(t("dirScan.toast.scanCancelFailed", { error: error.message })),
    })
  }, [t, cancelScan])

  return (
    <div
      className={`rounded-lg border p-4 transition-colors cursor-pointer ${
        isSelected ? "border-primary bg-primary/5" : "hover:border-muted-foreground/50"
      }`}
      onClick={() => onSelect(isSelected ? null : directory.id)}
    >
      <div className="grid grid-cols-[1fr_auto] items-start gap-4">
        <div className="min-w-0 space-y-2">
          <div className="flex items-center gap-2">
            <span className="font-mono text-sm truncate">{directory.path}</span>
            {!directory.enabled && (
              <Badge variant="secondary" className="text-xs">
                {t("dirScan.disabled")}
              </Badge>
            )}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{targetInstance?.name ? t("dirScan.target", { name: targetInstance.name }) : t("dirScan.targetUnknown")}</span>
            <span>{t("dirScan.interval", { minutes: directory.scanIntervalMinutes })}</span>
            {directory.category && <span>{t("dirScan.category", { category: directory.category })}</span>}
            {directory.lastScanAt && (
              <span>{t("dirScan.lastScan", { time: formatRelativeTime(directory.lastScanAt) })}</span>
            )}
          </div>
          {status && !("status" in status && status.status === "idle") && (
            <DirectoryStatusBadge run={status as DirScanRun} />
          )}
        </div>
        <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
          {isActive ? (
            <Button
              variant="outline"
              size="sm"
              onClick={handleCancel}
              disabled={cancelScan.isPending}
            >
              {cancelScan.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Pause className="size-4" />
              )}
            </Button>
          ) : (
            <Button
              variant="outline"
              size="sm"
              onClick={handleTrigger}
              disabled={triggerScan.isPending || !directory.enabled}
            >
              {triggerScan.isPending ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
            </Button>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onEdit(directory)}
          >
            <Settings2 className="size-4" />
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onDelete(directory.id)}
          >
            <Trash2 className="size-4 text-destructive" />
          </Button>
        </div>
      </div>
    </div>
  )
}

// Status Badge Component
function DirectoryStatusBadge({ run }: { run: DirScanRun }) {
  const { t } = useTranslation("crossseed")
  const statusConfig: Record<DirScanRunStatus, { icon: React.ReactNode; color: string; label: string }> = {
    queued: { icon: <Clock className="size-3" />, color: "text-blue-500", label: t("dirScan.statusLabels.queued") },
    scanning: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: t("dirScan.statusLabels.scanning") },
    searching: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: t("dirScan.statusLabels.searching") },
    injecting: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: t("dirScan.statusLabels.injecting") },
    success: { icon: <CheckCircle2 className="size-3" />, color: "text-green-500", label: t("dirScan.statusLabels.success") },
    failed: { icon: <XCircle className="size-3" />, color: "text-red-500", label: t("dirScan.statusLabels.failed") },
    canceled: { icon: <Clock className="size-3" />, color: "text-yellow-500", label: t("dirScan.statusLabels.canceled") },
  }

  const config = statusConfig[run.status]
  const hasStats = run.filesFound > 0 || run.filesSkipped > 0 || run.matchesFound > 0 || run.torrentsAdded > 0

  return (
    <div className={`flex items-center gap-1.5 text-xs ${config.color}`}>
      {config.icon}
      <span>{config.label}</span>
      {hasStats && (
        <span className="inline-flex items-center gap-1">
          <span className="text-muted-foreground">(</span>
          <RunFilesBadge run={run} />
          <span className="text-muted-foreground">
            {run.filesSkipped > 0 ? `, ${t("dirScan.skipped", { count: run.filesSkipped })}` : ""}
            , {t("dirScan.matches", { count: run.matchesFound })}, {t("dirScan.added", { count: run.torrentsAdded })})
          </span>
        </span>
      )}
    </div>
  )
}

// Directory Details Component
interface DirectoryDetailsProps {
  directoryId: number
  formatDateTime: (date: string) => string
  formatRelativeTime: (date: string | Date) => string
}

function formatTrackerName(injection: DirScanRunInjection): string {
  return (
    injection.trackerDisplayName ||
    injection.indexerName ||
    injection.trackerDomain ||
    i18n.t("dirScan.unknown", { ns: "crossseed" })
  )
}

function InjectionStatusBadge({ injection }: { injection: DirScanRunInjection }) {
  const { t } = useTranslation("crossseed")
  const isFailed = injection.status === "failed"
  return (
    <span className={`inline-flex items-center gap-1 text-xs ${isFailed ? "text-red-500" : "text-green-500"}`}>
      {isFailed ? <XCircle className="size-3" /> : <CheckCircle2 className="size-3" />}
      <span>{isFailed ? t("dirScan.statusLabels.failed") : t("dirScan.statusLabels.added")}</span>
    </span>
  )
}

function RunRow({
  directoryId,
  run,
  expanded,
  onToggle,
  formatDateTime,
  formatRelativeTime,
}: {
  directoryId: number
  run: DirScanRun
  expanded: boolean
  onToggle: () => void
  formatDateTime: (date: string) => string
  formatRelativeTime: (date: string | Date) => string
}) {
  const { t } = useTranslation("crossseed")
  const { data: injections = [], isLoading } = useDirScanRunInjections(directoryId, run.id, {
    enabled: expanded,
    active: expanded && isRunActive(run),
    limit: 50,
  })

  return (
    <>
      <TableRow>
        <TableCell>
          <div className="flex items-center gap-2">
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onToggle}>
              {expanded ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
            </Button>
            <Tooltip>
              <TooltipTrigger className="cursor-default">
                {formatRelativeTime(run.startedAt)}
              </TooltipTrigger>
              <TooltipContent>{formatDateTime(run.startedAt)}</TooltipContent>
            </Tooltip>
          </div>
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-2">
            <DirectoryStatusBadge run={run} />
            {run.status === "failed" && run.errorMessage && (
              <Tooltip>
                <TooltipTrigger className="cursor-default">
                  <Info className="size-3.5 text-muted-foreground" />
                </TooltipTrigger>
                <TooltipContent className="max-w-lg whitespace-pre-wrap">
                  {run.errorMessage}
                </TooltipContent>
              </Tooltip>
            )}
          </div>
        </TableCell>
        <TableCell>
          <RunFilesBadge run={run} />
        </TableCell>
        <TableCell>{run.matchesFound}</TableCell>
        <TableCell>{run.torrentsAdded}</TableCell>
        <TableCell>
          {(() => {
            if (!run.completedAt) return "-"
            const start = new Date(run.startedAt).getTime()
            const end = new Date(run.completedAt).getTime()
            if (Number.isFinite(start) && Number.isFinite(end) && end >= start) {
              return formatDuration(end - start)
            }
            return "-"
          })()}
        </TableCell>
      </TableRow>

      {expanded && (
        <TableRow>
          <TableCell colSpan={6} className="bg-muted/20 py-3">
            {isLoading ? (
              <div className="flex items-center justify-center py-6">
                <Loader2 className="size-5 animate-spin text-muted-foreground" />
              </div>
            ) : injections.length === 0 ? (
              <p className="text-sm text-muted-foreground text-center py-2">
                {t("dirScan.noTorrentsAddedOrFailed")}
              </p>
            ) : (
              <div className="space-y-2">
                <div className="text-sm font-medium">{t("dirScan.addedOrFailed")}</div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{t("dirScan.table.statusHead")}</TableHead>
                      <TableHead>{t("dirScan.table.release")}</TableHead>
                      <TableHead>{t("dirScan.table.tracker")}</TableHead>
                      <TableHead>{t("dirScan.table.type")}</TableHead>
                      <TableHead>{t("dirScan.table.mode")}</TableHead>
                      <TableHead>{t("dirScan.table.time")}</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {injections.map((inj) => (
                      <TableRow key={inj.id}>
                        <TableCell>
                          <InjectionStatusBadge injection={inj} />
                        </TableCell>
                        <TableCell className="max-w-[520px]">
                          <div className="truncate" title={inj.torrentName}>
                            {inj.torrentName}
                          </div>
                          <div className="text-xs text-muted-foreground">
                            <span className="font-mono">{inj.infoHash.slice(0, 8)}</span>
                          </div>
                          {inj.status === "failed" && inj.errorMessage && (
                            <details className="mt-1">
                              <summary className="text-xs text-muted-foreground cursor-pointer">
                                {t("dirScan.showError")}
                              </summary>
                              <pre className="mt-1 whitespace-pre-wrap text-xs text-muted-foreground">
                                {inj.errorMessage}
                              </pre>
                            </details>
                          )}
                        </TableCell>
                        <TableCell>{formatTrackerName(inj)}</TableCell>
                        <TableCell>{inj.contentType}</TableCell>
                        <TableCell>{inj.linkMode ?? "-"}</TableCell>
                        <TableCell>
                          <Tooltip>
                            <TooltipTrigger className="cursor-default">
                              {formatRelativeTime(inj.createdAt)}
                            </TooltipTrigger>
                            <TooltipContent>{formatDateTime(inj.createdAt)}</TooltipContent>
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </TableCell>
        </TableRow>
      )}
    </>
  )
}

function DirectoryDetails({ directoryId, formatDateTime, formatRelativeTime }: DirectoryDetailsProps) {
  const { t } = useTranslation("crossseed")
  const { data: runs = [], isLoading } = useDirScanRuns(directoryId, { limit: 10 })
  const resetFiles = useResetDirScanFiles(directoryId)
  const [expandedRunId, setExpandedRunId] = useState<number | null>(null)
  const [showResetDialog, setShowResetDialog] = useState(false)

  const handleReset = useCallback(() => {
    resetFiles.mutate(undefined, {
      onSuccess: () => {
        toast.success(t("dirScan.resetFilesSuccess"))
        setShowResetDialog(false)
      },
      onError: (error) => {
        toast.error(t("dirScan.toast.resetFailed", { error: error.message }))
      },
    })
  }, [t, resetFiles])

  if (isLoading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-8">
          <Loader2 className="size-6 animate-spin text-muted-foreground" />
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-start justify-between">
        <div>
          <CardTitle>{t("dirScan.recentScanRuns")}</CardTitle>
          <CardDescription>{t("dirScan.recentScanRunsDescription")}</CardDescription>
        </div>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setShowResetDialog(true)}
          disabled={resetFiles.isPending}
        >
          {resetFiles.isPending ? (
            <Loader2 className="size-4 mr-2 animate-spin" />
          ) : (
            <RotateCcw className="size-4 mr-2" />
          )}
          {t("dirScan.resetScanProgress")}
        </Button>
      </CardHeader>
      <CardContent>
        {runs.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            {t("dirScan.noScanRunsYet")}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t("dirScan.table.started")}</TableHead>
                <TableHead>{t("dirScan.table.status")}</TableHead>
                <TableHead>{t("dirScan.table.files")}</TableHead>
                <TableHead>{t("dirScan.table.matchesHead")}</TableHead>
                <TableHead>{t("dirScan.table.addedHead")}</TableHead>
                <TableHead>{t("dirScan.table.duration")}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {runs.map((run) => (
                <RunRow
                  key={run.id}
                  directoryId={directoryId}
                  run={run}
                  expanded={expandedRunId === run.id}
                  onToggle={() => setExpandedRunId(expandedRunId === run.id ? null : run.id)}
                  formatDateTime={formatDateTime}
                  formatRelativeTime={formatRelativeTime}
                />
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <AlertDialog
        open={showResetDialog}
        onOpenChange={(open) => {
          if (resetFiles.isPending) return
          setShowResetDialog(open)
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("dirScan.resetScanProgressTitle")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("dirScan.resetScanProgressDescription")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetFiles.isPending}>{t("dirScan.deleteDialog.cancel")}</AlertDialogCancel>
            <Button
              variant="destructive"
              onClick={handleReset}
              disabled={resetFiles.isPending}
            >
              {resetFiles.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
              {t("dirScan.resetFilesButton")}
            </Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

// Settings Dialog
interface SettingsDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  settings: ReturnType<typeof useDirScanSettings>["data"]
  instances: Instance[]
}

const ageFilterPresets = [1, 3, 7, 14, 30, 60, 90]

function buildSettingsFormState(settings: SettingsDialogProps["settings"]) {
  return {
    matchMode: (settings?.matchMode ?? "strict") as DirScanMatchMode,
    sizeTolerancePercent: settings?.sizeTolerancePercent ?? 2,
    minPieceRatio: settings?.minPieceRatio ?? 98,
    maxSearcheesPerRun: settings?.maxSearcheesPerRun ?? 0,
    maxSearcheeAgeDays: settings?.maxSearcheeAgeDays ?? 0,
    allowPartial: settings?.allowPartial ?? false,
    downloadMissingFiles: settings?.downloadMissingFiles ?? true,
    skipPieceBoundarySafetyCheck: settings?.skipPieceBoundarySafetyCheck ?? true,
    startPaused: settings?.startPaused ?? false,
    category: settings?.category ?? "",
    tags: settings?.tags ?? [],
  }
}

function SettingsDialog({ open, onOpenChange, settings, instances }: SettingsDialogProps) {
  const { t } = useTranslation("crossseed")
  const updateSettings = useUpdateDirScanSettings()
  const [form, setForm] = useState(() => buildSettingsFormState(settings))

  useEffect(() => {
    if (!open) return
    setForm(buildSettingsFormState(settings))
  }, [open, settings])

  const instanceIds = useMemo(
    () => Array.from(new Set(instances.map((i) => i.id).filter((id) => id > 0))),
    [instances]
  )

  const metadataQueries = useQueries({
    queries: instanceIds.map((instanceId) => ({
      queryKey: ["instance-metadata", instanceId],
      queryFn: async () => {
        const [categories, tags, preferences] = await Promise.all([
          api.getCategories(instanceId),
          api.getTags(instanceId),
          api.getInstancePreferences(instanceId),
        ])
        return { categories, tags, preferences }
      },
      staleTime: 60_000,
      gcTime: 1_800_000,
      refetchInterval: 30_000,
      refetchIntervalInBackground: false,
      placeholderData: (previousData: unknown) => previousData,
      enabled: open,
    })),
  })

  const aggregatedMetadata = useMemo(() => {
    const categories: Record<string, { name: string; savePath: string }> = {}
    const tags = new Set<string>()

    for (const q of metadataQueries) {
      const data = q.data as undefined | { categories: Record<string, { name: string; savePath: string }>; tags: string[] }
      if (!data) continue
      for (const [name, cat] of Object.entries(data.categories ?? {})) {
        categories[name] = cat
      }
      for (const tag of data.tags ?? []) {
        tags.add(tag)
      }
    }

    return { categories, tags: Array.from(tags) }
  }, [metadataQueries])

  const categorySelectOptions = useMemo(() => {
    const selected = form.category ? [form.category] : []
    return buildCategorySelectOptions(aggregatedMetadata.categories, selected)
  }, [aggregatedMetadata.categories, form.category])

  const tagSelectOptions = useMemo(
    () => buildTagSelectOptions(aggregatedMetadata.tags, form.tags),
    [aggregatedMetadata.tags, form.tags]
  )

  const defaultCategoryPlaceholder = useMemo(() => {
    if (instanceIds.length === 0) {
      return t("dirScan.settingsDialog.noLocalInstances")
    }
    if (categorySelectOptions.length === 0) {
      return t("dirScan.settingsDialog.typeToAddCategory")
    }
    return t("dirScan.settingsDialog.noCategory")
  }, [t, instanceIds.length, categorySelectOptions.length])

  const tagPlaceholder = useMemo(() => {
    if (instanceIds.length === 0) {
      return t("dirScan.settingsDialog.noLocalInstances")
    }
    if (tagSelectOptions.length === 0) {
      return t("dirScan.settingsDialog.typeToAddTags")
    }
    return t("dirScan.settingsDialog.noTags")
  }, [t, instanceIds.length, tagSelectOptions.length])

  const ageFilterEnabled = form.maxSearcheeAgeDays > 0
  const ageFilterCutoffPreview = useMemo(() => {
    if (!ageFilterEnabled) {
      return ""
    }
    const days = Math.max(1, form.maxSearcheeAgeDays)
    const cutoff = new Date(Date.now() - days * 24 * 60 * 60 * 1000)
    return cutoff.toLocaleString()
  }, [ageFilterEnabled, form.maxSearcheeAgeDays])

  const handleSave = useCallback(() => {
    updateSettings.mutate(form, {
      onSuccess: () => {
        toast.success(t("dirScan.toast.settingsSaved"))
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(t("dirScan.toast.settingsSaveFailed", { error: error.message }))
      },
    })
  }, [t, form, updateSettings, onOpenChange])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{t("dirScan.settingsDialog.title")}</DialogTitle>
          <DialogDescription>
            {t("dirScan.settingsDialog.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 flex-1 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="match-mode">{t("dirScan.settingsDialog.matchModeLabel")}</Label>
            <Select
              value={form.matchMode}
              onValueChange={(value: DirScanMatchMode) =>
                setForm((prev) => ({ ...prev, matchMode: value }))
              }
            >
              <SelectTrigger id="match-mode">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="strict">{t("dirScan.settingsDialog.matchModeStrict")}</SelectItem>
                <SelectItem value="flexible">{t("dirScan.settingsDialog.matchModeFlexible")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.matchModeHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="size-tolerance">{t("dirScan.settingsDialog.sizeToleranceLabel")}</Label>
            <Input
              id="size-tolerance"
              type="number"
              min={0}
              max={10}
              step={0.5}
              value={form.sizeTolerancePercent}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  sizeTolerancePercent: parseFloat(e.target.value) || 0,
                }))
              }
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.sizeToleranceHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="min-piece-ratio">{t("dirScan.settingsDialog.minPieceRatioLabel")}</Label>
            <Input
              id="min-piece-ratio"
              type="number"
              min={0}
              max={100}
              value={form.minPieceRatio}
              onChange={(e) =>
                setForm((prev) => ({
                  ...prev,
                  minPieceRatio: parseFloat(e.target.value) || 0,
                }))
              }
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.minPieceRatioHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="max-searchees-per-run">{t("dirScan.settingsDialog.maxSearcheesPerRunLabel")}</Label>
            <Input
              id="max-searchees-per-run"
              type="number"
              min={0}
              step={1}
              value={form.maxSearcheesPerRun}
              onChange={(e) =>
                setForm((prev) => {
                  const parsed = Number.parseInt(e.target.value, 10)
                  return {
                    ...prev,
                    maxSearcheesPerRun: Number.isFinite(parsed) ? Math.max(0, parsed) : 0,
                  }
                })
              }
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.maxSearcheesPerRunHelp")}
            </p>
          </div>

          <div className="space-y-2 rounded-lg border p-3">
            <div className="flex items-center gap-2">
              <Switch
                id="max-searchee-age-enabled"
                checked={ageFilterEnabled}
                onCheckedChange={(checked) => {
                  setForm((prev) => ({
                    ...prev,
                    maxSearcheeAgeDays: checked ? Math.max(prev.maxSearcheeAgeDays || 0, 7) : 0,
                  }))
                }}
              />
              <Label htmlFor="max-searchee-age-enabled">{t("dirScan.settingsDialog.maxAgeEnabled")}</Label>
            </div>

            {ageFilterEnabled && (
              <>
                <div className="flex items-center gap-2">
                  <Input
                    id="max-searchee-age-days"
                    type="number"
                    min={1}
                    step={1}
                    value={form.maxSearcheeAgeDays}
                    onChange={(e) =>
                      setForm((prev) => ({
                        ...prev,
                        maxSearcheeAgeDays: Math.max(1, Number.parseInt(e.target.value, 10) || 1),
                      }))
                    }
                    className="w-28"
                  />
                  <span className="text-sm text-muted-foreground">{t("dirScan.settingsDialog.days")}</span>
                </div>

                <div className="flex flex-wrap gap-2">
                  {ageFilterPresets.map((days) => (
                    <Button
                      key={days}
                      type="button"
                      variant={form.maxSearcheeAgeDays === days ? "default" : "outline"}
                      size="sm"
                      onClick={() =>
                        setForm((prev) => ({ ...prev, maxSearcheeAgeDays: days }))
                      }
                    >
                      {days}d
                    </Button>
                  ))}
                </div>

                <p className="text-xs text-muted-foreground">
                  {t("dirScan.settingsDialog.maxAgeHelp")}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t("dirScan.settingsDialog.maxAgeWebhookHelp")}
                </p>
                <p className="text-xs text-muted-foreground">
                  {t("dirScan.settingsDialog.currentCutoff", { cutoff: ageFilterCutoffPreview })}
                </p>
              </>
            )}
          </div>

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="allow-partial"
                checked={form.allowPartial}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, allowPartial: checked }))
                }
              />
              <Label htmlFor="allow-partial" className="flex items-center gap-1">
                {t("dirScan.settingsDialog.allowPartial")}
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    {t("dirScan.settingsDialog.allowPartialHelp")}
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.allowPartialHelp")}
            </p>
          </div>

          {form.allowPartial && (
            <div className="space-y-1">
              <div className="flex items-center gap-2">
                <Switch
                  id="download-missing-files"
                  checked={form.downloadMissingFiles}
                  onCheckedChange={(checked) =>
                    setForm((prev) => ({ ...prev, downloadMissingFiles: checked }))
                  }
                />
                <Label htmlFor="download-missing-files" className="flex items-center gap-1">
                  {t("dirScan.settingsDialog.downloadMissingFiles")}
                  <Tooltip>
                    <TooltipTrigger>
                      <Info className="size-3.5 text-muted-foreground" />
                    </TooltipTrigger>
                    <TooltipContent className="max-w-xs">
                      {t("dirScan.settingsDialog.downloadMissingFilesHelp")}
                    </TooltipContent>
                  </Tooltip>
                </Label>
              </div>
              <p className="text-xs text-muted-foreground">
                {t("dirScan.settingsDialog.downloadMissingFilesHelp")}
              </p>
            </div>
          )}

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="skip-piece-boundary"
                checked={form.skipPieceBoundarySafetyCheck}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, skipPieceBoundarySafetyCheck: checked }))
                }
              />
              <Label htmlFor="skip-piece-boundary" className="flex items-center gap-1">
                {t("dirScan.settingsDialog.skipPieceBoundarySafetyCheck")}
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    {t("dirScan.settingsDialog.skipPieceBoundarySafetyCheckHelp")}
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.skipPieceBoundarySafetyCheckDescription")}
            </p>
          </div>

          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <Switch
                id="start-paused"
                checked={form.startPaused}
                onCheckedChange={(checked) =>
                  setForm((prev) => ({ ...prev, startPaused: checked }))
                }
              />
              <Label htmlFor="start-paused">{t("dirScan.settingsDialog.startPaused")}</Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.startPausedHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{t("dirScan.settingsDialog.defaultCategory")}</Label>
            <MultiSelect
              options={categorySelectOptions}
              selected={form.category ? [form.category] : []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, category: values.at(-1) ?? "" }))
              }
              placeholder={defaultCategoryPlaceholder}
              creatable
              disabled={updateSettings.isPending}
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.settingsDialog.defaultCategoryHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{t("dirScan.settingsDialog.tags")}</Label>
            <MultiSelect
              options={tagSelectOptions}
              selected={form.tags}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, tags: values }))
              }
              placeholder={tagPlaceholder}
              creatable
              disabled={updateSettings.isPending}
            />
          </div>
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("dirScan.settingsDialog.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={updateSettings.isPending}>
            {updateSettings.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {t("dirScan.settingsDialog.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Directory Dialog
interface DirectoryDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  directory: DirScanDirectory | null
  instances: Instance[]
}

function DirectoryDialog({ open, onOpenChange, directory, instances }: DirectoryDialogProps) {
  const { t } = useTranslation("crossseed")
  const createDirectory = useCreateDirScanDirectory()
  const updateDirectory = useUpdateDirScanDirectory(directory?.id ?? 0)
  const isEditing = directory !== null

  const defaultTargetInstanceId = instances[0]?.id ?? 0

  const [form, setForm] = useState<DirScanDirectoryCreate>(() => ({
    path: directory?.path ?? "",
    qbitPathPrefix: directory?.qbitPathPrefix ?? "",
    category: directory?.category ?? "",
    tags: directory?.tags ?? [],
    allowedDownloadClients: directory?.allowedDownloadClients ?? [],
    enabled: directory?.enabled ?? true,
    targetInstanceId: directory?.targetInstanceId ?? defaultTargetInstanceId,
    scanIntervalMinutes: directory?.scanIntervalMinutes ?? 1440,
  }))

  // Track acknowledgment of regular mode warning
  const [regularModeAcknowledged, setRegularModeAcknowledged] = useState(false)

  const { data: targetInstanceMetadata, isError: targetInstanceMetadataError } = useInstanceMetadata(form.targetInstanceId)

  // Check if target instance is in regular mode (not using hardlinks or reflinks)
  const targetInstance = useMemo(
    () => instances.find((i) => i.id === form.targetInstanceId),
    [instances, form.targetInstanceId]
  )
  const isRegularMode = targetInstance && !targetInstance.useHardlinks && !targetInstance.useReflinks

  const directoryCategoryOptions = useMemo(() => {
    const selected = form.category ? [form.category] : []
    return buildCategorySelectOptions(targetInstanceMetadata?.categories ?? {}, selected)
  }, [targetInstanceMetadata?.categories, form.category])

  const directoryTagOptions = useMemo(
    () => buildTagSelectOptions(targetInstanceMetadata?.tags ?? [], form.tags ?? []),
    [targetInstanceMetadata?.tags, form.tags]
  )

  // Reset form when directory or dialog state changes
  useEffect(() => {
    if (!open) return
    // Reset acknowledgment when dialog opens
    setRegularModeAcknowledged(false)
    if (directory) {
      setForm({
        path: directory.path,
        qbitPathPrefix: directory.qbitPathPrefix ?? "",
        category: directory.category ?? "",
        tags: directory.tags ?? [],
        allowedDownloadClients: directory.allowedDownloadClients ?? [],
        enabled: directory.enabled,
        targetInstanceId: directory.targetInstanceId,
        scanIntervalMinutes: directory.scanIntervalMinutes,
      })
    } else {
      setForm({
        path: "",
        qbitPathPrefix: "",
        category: "",
        tags: [],
        allowedDownloadClients: [],
        enabled: true,
        targetInstanceId: defaultTargetInstanceId,
        scanIntervalMinutes: 1440,
      })
    }
  }, [open, directory, defaultTargetInstanceId])

  // Reset acknowledgment when instance changes to regular mode
  useEffect(() => {
    if (isRegularMode) {
      setRegularModeAcknowledged(false)
    }
  }, [form.targetInstanceId, isRegularMode])

  const handleSave = useCallback(() => {
    // Ensure scanIntervalMinutes is clamped to minimum 60
    const clampedForm = {
      ...form,
      scanIntervalMinutes: Math.max(form.scanIntervalMinutes ?? 1440, 60),
    }

    if (isEditing) {
      updateDirectory.mutate(clampedForm, {
        onSuccess: () => {
          toast.success(t("dirScan.toast.directoryUpdated"))
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(t("dirScan.toast.directoryUpdateFailed", { error: error.message }))
        },
      })
    } else {
      createDirectory.mutate(clampedForm, {
        onSuccess: () => {
          toast.success(t("dirScan.toast.directoryCreated"))
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(t("dirScan.toast.directoryCreateFailed", { error: error.message }))
        },
      })
    }
  }, [t, isEditing, form, createDirectory, updateDirectory, onOpenChange])

  const isPending = createDirectory.isPending || updateDirectory.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{isEditing ? t("dirScan.directoryDialog.editTitle") : t("dirScan.directoryDialog.addTitle")}</DialogTitle>
          <DialogDescription>
            {isEditing ? t("dirScan.directoryDialog.editDescription") : t("dirScan.directoryDialog.addDescription")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 flex-1 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="dir-path">{t("dirScan.directoryDialog.pathLabel")}</Label>
            <Input
              id="dir-path"
              placeholder={t("dirScan.directoryDialog.pathPlaceholder")}
              value={form.path}
              onChange={(e) => setForm((prev) => ({ ...prev, path: e.target.value }))}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="qbit-path-prefix" className="flex items-center gap-1">
              {t("dirScan.directoryDialog.qbitPathPrefixLabel")}
              <Tooltip>
                <TooltipTrigger>
                  <Info className="size-3.5 text-muted-foreground" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  {t("dirScan.directoryDialog.qbitPathPrefixHelp")}
                </TooltipContent>
              </Tooltip>
            </Label>
            <Input
              id="qbit-path-prefix"
              placeholder={t("dirScan.directoryDialog.qbitPathPrefixPlaceholder")}
              value={form.qbitPathPrefix}
              onChange={(e) =>
                setForm((prev) => ({ ...prev, qbitPathPrefix: e.target.value }))
              }
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="target-instance">{t("dirScan.directoryDialog.instanceLabel")}</Label>
            <Select
              value={String(form.targetInstanceId)}
              onValueChange={(value) =>
                setForm((prev) => ({ ...prev, targetInstanceId: parseInt(value, 10) }))
              }
            >
              <SelectTrigger id="target-instance">
                <SelectValue placeholder={t("dirScan.directoryDialog.selectInstance")} />
              </SelectTrigger>
              <SelectContent>
                {instances.map((instance) => (
                  <SelectItem key={instance.id} value={String(instance.id)}>
                    {instance.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>

          <div className="space-y-2">
            <Label>{t("dirScan.directoryDialog.categoryOverrideLabel")}</Label>
            <MultiSelect
              options={directoryCategoryOptions}
              selected={form.category ? [form.category] : []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, category: values.at(-1) ?? "" }))
              }
              placeholder={
                directoryCategoryOptions.length ? t("dirScan.directoryDialog.useGlobalCategory") : t("dirScan.directoryDialog.typeToAddCategory")
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                {t("dirScan.directoryDialog.categoryLoadError")}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              {t("dirScan.directoryDialog.categoryOverrideHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{t("dirScan.directoryDialog.additionalTagsLabel")}</Label>
            <MultiSelect
              options={directoryTagOptions}
              selected={form.tags ?? []}
              onChange={(values) => setForm((prev) => ({ ...prev, tags: values }))}
              placeholder={
                directoryTagOptions.length ? t("dirScan.directoryDialog.addTagsOptional") : t("dirScan.directoryDialog.typeToAddTags")
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                {t("dirScan.directoryDialog.tagsLoadError")}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              <Trans
                ns="crossseed"
                i18nKey="dirScan.tagsDescription"
                components={{
                  dirscan: <span className="font-mono" />,
                  needsReview: <span className="font-mono" />,
                }}
              />
            </p>
          </div>

          <div className="space-y-2">
            <Label>{t("dirScan.directoryDialog.allowedDownloadClientsLabel")}</Label>
            <MultiSelect
              options={(form.allowedDownloadClients ?? []).map((value) => ({ label: value, value }))}
              selected={form.allowedDownloadClients ?? []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, allowedDownloadClients: values }))
              }
              placeholder={t("dirScan.directoryDialog.allowedDownloadClientsPlaceholder")}
              creatable
              disabled={isPending}
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.directoryDialog.allowedDownloadClientsHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="scan-interval">{t("dirScan.directoryDialog.intervalLabel")}</Label>
            <Input
              id="scan-interval"
              type="number"
              min={60}
              value={form.scanIntervalMinutes}
              onChange={(e) => {
                const parsed = parseInt(e.target.value, 10)
                setForm((prev) => ({
                  ...prev,
                  scanIntervalMinutes: Number.isNaN(parsed) ? 1440 : Math.max(parsed, 60),
                }))
              }}
            />
            <p className="text-xs text-muted-foreground">
              {t("dirScan.directoryDialog.intervalHelp")}
            </p>
          </div>

          <div className="flex items-center gap-2">
            <Switch
              id="dir-enabled"
              checked={form.enabled}
              onCheckedChange={(checked) =>
                setForm((prev) => ({ ...prev, enabled: checked }))
              }
            />
            <Label htmlFor="dir-enabled">{t("dirScan.directoryDialog.enabledLabel")}</Label>
          </div>

          {/* Regular mode warning */}
          {isRegularMode && (
            <div className="rounded-lg border border-yellow-500/50 bg-yellow-500/5 p-4 space-y-3">
              <div className="flex items-start gap-3">
                <AlertTriangle className="size-5 text-yellow-500 shrink-0 mt-0.5" />
                <div className="space-y-2">
                  <p className="text-sm font-medium text-foreground">
                    {t("dirScan.directoryDialog.regularModeTitle")}
                  </p>
                  <p className="text-sm text-muted-foreground">
                    <Trans
                      ns="crossseed"
                      i18nKey="dirScan.orphanWarning"
                      components={{ strong: <span className="font-medium" /> }}
                    />
                  </p>
                  <p className="text-sm text-muted-foreground">
                    <Trans
                      ns="crossseed"
                      i18nKey="dirScan.directoryDialog.regularModeHelp"
                      components={{ hardlink: <span className="font-medium" />, reflink: <span className="font-medium" /> }}
                    />
                  </p>
                </div>
              </div>
              <div className="flex items-start gap-2 pl-8">
                <Checkbox
                  id="regular-mode-acknowledged"
                  checked={regularModeAcknowledged}
                  onCheckedChange={(checked) => setRegularModeAcknowledged(checked === true)}
                />
                <Label
                  htmlFor="regular-mode-acknowledged"
                  className="text-sm text-muted-foreground cursor-pointer leading-tight"
                >
                  {t("dirScan.directoryDialog.regularModeAcknowledge")}
                </Label>
              </div>
            </div>
          )}
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("dirScan.directoryDialog.cancel")}
          </Button>
          <Button
            onClick={handleSave}
            disabled={isPending || !form.path || !form.targetInstanceId || (isRegularMode && !regularModeAcknowledged)}
          >
            {isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {isEditing ? t("dirScan.directoryDialog.saveButton") : t("dirScan.directoryDialog.addButton")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// Delete Confirmation Dialog
interface DeleteDirectoryDialogProps {
  directoryId: number | null
  onOpenChange: (open: boolean) => void
}

function DeleteDirectoryDialog({ directoryId, onOpenChange }: DeleteDirectoryDialogProps) {
  const { t } = useTranslation("crossseed")
  const deleteDirectory = useDeleteDirScanDirectory()

  const handleDelete = useCallback(() => {
    if (!directoryId) return
    deleteDirectory.mutate(directoryId, {
      onSuccess: () => {
        toast.success(t("dirScan.toast.directoryDeleted"))
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(t("dirScan.toast.directoryDeleteFailed", { error: error.message }))
      },
    })
  }, [t, directoryId, deleteDirectory, onOpenChange])

  return (
    <AlertDialog open={directoryId !== null} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("dirScan.deleteDialog.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("dirScan.deleteDialog.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("dirScan.deleteDialog.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {deleteDirectory.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {t("dirScan.deleteDialog.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// Helper function
function formatDuration(ms: number): string {
  if (ms < 1000) return "<1s"
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  if (minutes < 60) return `${minutes}m ${remainingSeconds}s`
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return `${hours}h ${remainingMinutes}m`
}
