/*
 * Copyright (c) 2025-2026, s0up and the autobrr contributors.
 * SPDX-License-Identifier: GPL-2.0-or-later
 */

import { useCallback, useEffect, useMemo, useState } from "react"
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
import { Trans, useTranslation } from "react-i18next"

interface DirScanTabProps {
  instances: Instance[]
}

function useCommonTr() {
  const { t } = useTranslation("common")
  return useCallback((key: string, options?: Record<string, unknown>) => String(t(key as never, options as never)), [t])
}

// Helper to format relative time from a string or Date
function formatRelativeTimeStr(date: string | Date): string {
  return formatRelativeTime(typeof date === "string" ? new Date(date) : date)
}

export function DirScanTab({ instances }: DirScanTabProps) {
  const tr = useCommonTr()
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
            toast.success(enabled ? tr("dirScanTab.toasts.scannerEnabled") : tr("dirScanTab.toasts.scannerDisabled"))
          },
          onError: (error) => {
            toast.error(tr("dirScanTab.toasts.updateSettingsFailed", { message: error.message }))
          },
        }
      )
    },
    [tr, updateSettings]
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
                {tr("dirScanTab.header.title")}
              </CardTitle>
              <CardDescription>
                {tr("dirScanTab.header.description")}
              </CardDescription>
            </div>
            <div className="flex items-center gap-4">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowSettingsDialog(true)}
              >
                <Settings2 className="size-4 mr-2" />
                {tr("dirScanTab.actions.settings")}
              </Button>
              <Label htmlFor="dir-scan-enabled" className="flex items-center gap-2">
                <Switch
                  id="dir-scan-enabled"
                  checked={settings?.enabled ?? false}
                  onCheckedChange={handleToggleEnabled}
                  disabled={updateSettings.isPending}
                />
                {settings?.enabled ? tr("dirScanTab.values.enabled") : tr("dirScanTab.values.disabled")}
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
              {tr("dirScanTab.warnings.noLocalFsAccess")}
            </p>
          </CardContent>
        </Card>
      )}

      {/* Directories List */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <div>
            <CardTitle>{tr("dirScanTab.directories.title")}</CardTitle>
            <CardDescription>
              {tr("dirScanTab.directories.description")}
            </CardDescription>
          </div>
          <Button
            onClick={handleAddDirectory}
            disabled={directoryWithLocalFs.length === 0}
          >
            <Plus className="size-4 mr-2" />
            {tr("dirScanTab.actions.addDirectory")}
          </Button>
        </CardHeader>
        <CardContent>
          {directories.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-8 text-center text-muted-foreground">
              <FolderSearch className="size-12 mb-4 opacity-50" />
              <p>{tr("dirScanTab.empty.noDirectoriesTitle")}</p>
              <p className="text-sm">{tr("dirScanTab.empty.noDirectoriesDescription")}</p>
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
  const tr = useCommonTr()
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
      onSuccess: () => toast.success(tr("dirScanTab.toasts.scanStarted")),
      onError: (error) => toast.error(tr("dirScanTab.toasts.startScanFailed", { message: error.message })),
    })
  }, [tr, triggerScan])

  const handleCancel = useCallback(() => {
    cancelScan.mutate(undefined, {
      onSuccess: () => toast.success(tr("dirScanTab.toasts.scanCanceled")),
      onError: (error) => toast.error(tr("dirScanTab.toasts.cancelScanFailed", { message: error.message })),
    })
  }, [cancelScan, tr])

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
                {tr("dirScanTab.values.disabled")}
              </Badge>
            )}
          </div>
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
            <span>{tr("dirScanTab.directoryCard.target", { name: targetInstance?.name ?? tr("dirScanTab.values.unknown") })}</span>
            <span>{tr("dirScanTab.directoryCard.intervalMinutes", { minutes: directory.scanIntervalMinutes })}</span>
            {directory.category && <span>{tr("dirScanTab.directoryCard.category", { category: directory.category })}</span>}
            {directory.lastScanAt && (
              <span>{tr("dirScanTab.directoryCard.lastScan", { time: formatRelativeTime(directory.lastScanAt) })}</span>
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
  const tr = useCommonTr()
  const statusConfig: Record<DirScanRunStatus, { icon: React.ReactNode; color: string; label: string }> = {
    queued: { icon: <Clock className="size-3" />, color: "text-blue-500", label: tr("dirScanTab.status.queued") },
    scanning: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: tr("dirScanTab.status.scanning") },
    searching: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: tr("dirScanTab.status.searching") },
    injecting: { icon: <Loader2 className="size-3 animate-spin" />, color: "text-blue-500", label: tr("dirScanTab.status.injecting") },
    success: { icon: <CheckCircle2 className="size-3" />, color: "text-green-500", label: tr("dirScanTab.status.success") },
    failed: { icon: <XCircle className="size-3" />, color: "text-red-500", label: tr("dirScanTab.status.failed") },
    canceled: { icon: <Clock className="size-3" />, color: "text-yellow-500", label: tr("dirScanTab.status.canceled") },
  }

  const config = statusConfig[run.status]
  const hasStats = run.filesFound > 0 || run.matchesFound > 0 || run.torrentsAdded > 0

  return (
    <div className={`flex items-center gap-1.5 text-xs ${config.color}`}>
      {config.icon}
      <span>{config.label}</span>
      {hasStats && (
        <span className="text-muted-foreground">
          {tr("dirScanTab.status.stats", {
            files: run.filesFound,
            matches: run.matchesFound,
            added: run.torrentsAdded,
          })}
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

function formatTrackerName(injection: DirScanRunInjection, fallback: string): string {
  return (
    injection.trackerDisplayName ||
    injection.indexerName ||
    injection.trackerDomain ||
    fallback
  )
}

function InjectionStatusBadge({ injection }: { injection: DirScanRunInjection }) {
  const tr = useCommonTr()
  const isFailed = injection.status === "failed"
  return (
    <span className={`inline-flex items-center gap-1 text-xs ${isFailed ? "text-red-500" : "text-green-500"}`}>
      {isFailed ? <XCircle className="size-3" /> : <CheckCircle2 className="size-3" />}
      <span>{isFailed ? tr("dirScanTab.status.failed") : tr("dirScanTab.status.added")}</span>
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
  const tr = useCommonTr()
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
        <TableCell>{run.filesFound}</TableCell>
        <TableCell>{run.matchesFound}</TableCell>
        <TableCell>{run.torrentsAdded}</TableCell>
        <TableCell>
          {(() => {
            if (!run.completedAt) return tr("dirScanTab.values.notAvailable")
            const start = new Date(run.startedAt).getTime()
            const end = new Date(run.completedAt).getTime()
            if (Number.isFinite(start) && Number.isFinite(end) && end >= start) {
              return formatDuration(end - start, tr)
            }
            return tr("dirScanTab.values.notAvailable")
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
                {tr("dirScanTab.runs.noInjections")}
              </p>
            ) : (
              <div className="space-y-2">
                <div className="text-sm font-medium">{tr("dirScanTab.runs.injectionsTitle")}</div>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>{tr("dirScanTab.runs.table.status")}</TableHead>
                      <TableHead>{tr("dirScanTab.runs.table.release")}</TableHead>
                      <TableHead>{tr("dirScanTab.runs.table.tracker")}</TableHead>
                      <TableHead>{tr("dirScanTab.runs.table.type")}</TableHead>
                      <TableHead>{tr("dirScanTab.runs.table.mode")}</TableHead>
                      <TableHead>{tr("dirScanTab.runs.table.time")}</TableHead>
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
                                {tr("dirScanTab.runs.showError")}
                              </summary>
                              <pre className="mt-1 whitespace-pre-wrap text-xs text-muted-foreground">
                                {inj.errorMessage}
                              </pre>
                            </details>
                          )}
                        </TableCell>
                        <TableCell>{formatTrackerName(inj, tr("dirScanTab.values.unknown"))}</TableCell>
                        <TableCell>{inj.contentType}</TableCell>
                        <TableCell>{inj.linkMode ?? tr("dirScanTab.values.notAvailable")}</TableCell>
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
  const tr = useCommonTr()
  const { data: runs = [], isLoading } = useDirScanRuns(directoryId, { limit: 10 })
  const resetFiles = useResetDirScanFiles(directoryId)
  const [expandedRunId, setExpandedRunId] = useState<number | null>(null)
  const [showResetDialog, setShowResetDialog] = useState(false)

  const handleReset = useCallback(() => {
    resetFiles.mutate(undefined, {
      onSuccess: () => {
        toast.success(tr("dirScanTab.toasts.scanProgressReset"))
        setShowResetDialog(false)
      },
      onError: (error) => {
        toast.error(tr("dirScanTab.toasts.resetScanProgressFailed", { message: error.message }))
      },
    })
  }, [resetFiles, tr])

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
          <CardTitle>{tr("dirScanTab.runs.title")}</CardTitle>
          <CardDescription>{tr("dirScanTab.runs.description")}</CardDescription>
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
          {tr("dirScanTab.actions.resetScanProgress")}
        </Button>
      </CardHeader>
      <CardContent>
        {runs.length === 0 ? (
          <p className="text-sm text-muted-foreground text-center py-4">
            {tr("dirScanTab.empty.noRuns")}
          </p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{tr("dirScanTab.runs.table.started")}</TableHead>
                <TableHead>{tr("dirScanTab.runs.table.status")}</TableHead>
                <TableHead>{tr("dirScanTab.runs.table.files")}</TableHead>
                <TableHead>{tr("dirScanTab.runs.table.matches")}</TableHead>
                <TableHead>{tr("dirScanTab.runs.table.added")}</TableHead>
                <TableHead>{tr("dirScanTab.runs.table.duration")}</TableHead>
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
            <AlertDialogTitle>{tr("dirScanTab.resetDialog.title")}</AlertDialogTitle>
            <AlertDialogDescription>
              {tr("dirScanTab.resetDialog.description")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={resetFiles.isPending}>{tr("dirScanTab.shared.cancel")}</AlertDialogCancel>
            <Button
              variant="destructive"
              onClick={handleReset}
              disabled={resetFiles.isPending}
            >
              {resetFiles.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
              {tr("dirScanTab.actions.reset")}
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
    skipPieceBoundarySafetyCheck: settings?.skipPieceBoundarySafetyCheck ?? true,
    startPaused: settings?.startPaused ?? false,
    category: settings?.category ?? "",
    tags: settings?.tags ?? [],
  }
}

function SettingsDialog({ open, onOpenChange, settings, instances }: SettingsDialogProps) {
  const tr = useCommonTr()
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
      return tr("dirScanTab.settings.placeholders.noLocalAccess")
    }
    if (categorySelectOptions.length === 0) {
      return tr("dirScanTab.settings.placeholders.typeCategory")
    }
    return tr("dirScanTab.settings.placeholders.noCategory")
  }, [tr, instanceIds.length, categorySelectOptions.length])

  const tagPlaceholder = useMemo(() => {
    if (instanceIds.length === 0) {
      return tr("dirScanTab.settings.placeholders.noLocalAccess")
    }
    if (tagSelectOptions.length === 0) {
      return tr("dirScanTab.settings.placeholders.typeTags")
    }
    return tr("dirScanTab.settings.placeholders.noTags")
  }, [tr, instanceIds.length, tagSelectOptions.length])

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
        toast.success(tr("dirScanTab.toasts.settingsSaved"))
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(tr("dirScanTab.toasts.saveSettingsFailed", { message: error.message }))
      },
    })
  }, [form, onOpenChange, tr, updateSettings])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{tr("dirScanTab.settings.title")}</DialogTitle>
          <DialogDescription>
            {tr("dirScanTab.settings.description")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 flex-1 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="match-mode">{tr("dirScanTab.settings.matchMode.label")}</Label>
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
                <SelectItem value="strict">{tr("dirScanTab.settings.matchMode.strict")}</SelectItem>
                <SelectItem value="flexible">{tr("dirScanTab.settings.matchMode.flexible")}</SelectItem>
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">
              {tr("dirScanTab.settings.matchMode.help")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="size-tolerance">{tr("dirScanTab.settings.sizeTolerance.label")}</Label>
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
              {tr("dirScanTab.settings.sizeTolerance.help")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="min-piece-ratio">{tr("dirScanTab.settings.minPieceRatio.label")}</Label>
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
              {tr("dirScanTab.settings.minPieceRatio.help")}
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="max-searchees-per-run">{tr("dirScanTab.settings.maxSearchees.label")}</Label>
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
              {tr("dirScanTab.settings.maxSearchees.help")}
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
              <Label htmlFor="max-searchee-age-enabled">{tr("dirScanTab.settings.ageFilter.label")}</Label>
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
                  <span className="text-sm text-muted-foreground">{tr("dirScanTab.settings.ageFilter.days")}</span>
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
                      {tr("dirScanTab.settings.ageFilter.presetDays", { days })}
                    </Button>
                  ))}
                </div>

                <p className="text-xs text-muted-foreground">
                  {tr("dirScanTab.settings.ageFilter.help")}
                </p>
                <p className="text-xs text-muted-foreground">
                  {tr("dirScanTab.settings.ageFilter.currentCutoff", { value: ageFilterCutoffPreview })}
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
                {tr("dirScanTab.settings.allowPartial.label")}
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    {tr("dirScanTab.settings.allowPartial.tooltip")}
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {tr("dirScanTab.settings.allowPartial.help")}
            </p>
          </div>

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
                {tr("dirScanTab.settings.skipPieceBoundary.label")}
                <Tooltip>
                  <TooltipTrigger>
                    <Info className="size-3.5 text-muted-foreground" />
                  </TooltipTrigger>
                  <TooltipContent className="max-w-xs">
                    {tr("dirScanTab.settings.skipPieceBoundary.tooltip")}
                  </TooltipContent>
                </Tooltip>
              </Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {tr("dirScanTab.settings.skipPieceBoundary.help")}
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
              <Label htmlFor="start-paused">{tr("dirScanTab.settings.startPaused.label")}</Label>
            </div>
            <p className="text-xs text-muted-foreground">
              {tr("dirScanTab.settings.startPaused.help")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{tr("dirScanTab.settings.defaultCategory.label")}</Label>
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
              {tr("dirScanTab.settings.defaultCategory.help")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{tr("dirScanTab.settings.tags.label")}</Label>
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
            {tr("dirScanTab.shared.cancel")}
          </Button>
          <Button onClick={handleSave} disabled={updateSettings.isPending}>
            {updateSettings.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {tr("dirScanTab.shared.save")}
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
  const tr = useCommonTr()
  const createDirectory = useCreateDirScanDirectory()
  const updateDirectory = useUpdateDirScanDirectory(directory?.id ?? 0)
  const isEditing = directory !== null

  const defaultTargetInstanceId = instances[0]?.id ?? 0

  const [form, setForm] = useState<DirScanDirectoryCreate>(() => ({
    path: directory?.path ?? "",
    qbitPathPrefix: directory?.qbitPathPrefix ?? "",
    category: directory?.category ?? "",
    tags: directory?.tags ?? [],
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
          toast.success(tr("dirScanTab.toasts.directoryUpdated"))
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(tr("dirScanTab.toasts.updateDirectoryFailed", { message: error.message }))
        },
      })
    } else {
      createDirectory.mutate(clampedForm, {
        onSuccess: () => {
          toast.success(tr("dirScanTab.toasts.directoryCreated"))
          onOpenChange(false)
        },
        onError: (error) => {
          toast.error(tr("dirScanTab.toasts.createDirectoryFailed", { message: error.message }))
        },
      })
    }
  }, [isEditing, form, createDirectory, onOpenChange, tr, updateDirectory])

  const isPending = createDirectory.isPending || updateDirectory.isPending

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-lg max-h-[90dvh] flex flex-col">
        <DialogHeader className="flex-shrink-0">
          <DialogTitle>{isEditing ? tr("dirScanTab.directoryDialog.editTitle") : tr("dirScanTab.directoryDialog.addTitle")}</DialogTitle>
          <DialogDescription>
            {isEditing ? tr("dirScanTab.directoryDialog.editDescription") : tr("dirScanTab.directoryDialog.addDescription")}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 flex-1 overflow-y-auto min-h-0">
          <div className="space-y-2">
            <Label htmlFor="dir-path">{tr("dirScanTab.directoryDialog.pathLabel")}</Label>
            <Input
              id="dir-path"
              placeholder={tr("dirScanTab.directoryDialog.pathPlaceholder")}
              value={form.path}
              onChange={(e) => setForm((prev) => ({ ...prev, path: e.target.value }))}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="qbit-path-prefix" className="flex items-center gap-1">
              {tr("dirScanTab.directoryDialog.qbitPathPrefixLabel")}
              <Tooltip>
                <TooltipTrigger>
                  <Info className="size-3.5 text-muted-foreground" />
                </TooltipTrigger>
                <TooltipContent className="max-w-xs">
                  {tr("dirScanTab.directoryDialog.qbitPathPrefixTooltip")}
                </TooltipContent>
              </Tooltip>
            </Label>
            <Input
              id="qbit-path-prefix"
              placeholder={tr("dirScanTab.directoryDialog.qbitPathPrefixPlaceholder")}
              value={form.qbitPathPrefix}
              onChange={(e) =>
                setForm((prev) => ({ ...prev, qbitPathPrefix: e.target.value }))
              }
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="target-instance">{tr("dirScanTab.directoryDialog.targetInstanceLabel")}</Label>
            <Select
              value={String(form.targetInstanceId)}
              onValueChange={(value) =>
                setForm((prev) => ({ ...prev, targetInstanceId: parseInt(value, 10) }))
              }
            >
              <SelectTrigger id="target-instance">
                <SelectValue placeholder={tr("dirScanTab.directoryDialog.selectInstancePlaceholder")} />
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
            <Label>{tr("dirScanTab.directoryDialog.categoryOverrideLabel")}</Label>
            <MultiSelect
              options={directoryCategoryOptions}
              selected={form.category ? [form.category] : []}
              onChange={(values) =>
                setForm((prev) => ({ ...prev, category: values.at(-1) ?? "" }))
              }
              placeholder={
                directoryCategoryOptions.length
                  ? tr("dirScanTab.directoryDialog.useGlobalCategoryPlaceholder")
                  : tr("dirScanTab.directoryDialog.typeCategoryPlaceholder")
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                {tr("dirScanTab.directoryDialog.categoryLoadError")}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              {tr("dirScanTab.directoryDialog.categoryHelp")}
            </p>
          </div>

          <div className="space-y-2">
            <Label>{tr("dirScanTab.directoryDialog.additionalTagsLabel")}</Label>
            <MultiSelect
              options={directoryTagOptions}
              selected={form.tags ?? []}
              onChange={(values) => setForm((prev) => ({ ...prev, tags: values }))}
              placeholder={
                directoryTagOptions.length
                  ? tr("dirScanTab.directoryDialog.addTagsPlaceholder")
                  : tr("dirScanTab.directoryDialog.typeTagsPlaceholder")
              }
              creatable
              disabled={isPending}
            />
            {targetInstanceMetadataError && (
              <p className="text-xs text-muted-foreground">
                {tr("dirScanTab.directoryDialog.tagLoadError")}
              </p>
            )}
            <p className="text-xs text-muted-foreground">
              <Trans
                i18nKey="dirScanTab.directoryDialog.tagsHelp"
                ns="common"
                components={{
                  dirscanTag: <span className="font-mono" />,
                  needsReviewTag: <span className="font-mono" />,
                }}
                values={{
                  dirscanTag: tr("dirScanTab.directoryDialog.examples.dirscanTag"),
                  needsReviewTag: tr("dirScanTab.directoryDialog.examples.needsReviewTag"),
                }}
              />
            </p>
          </div>

          <div className="space-y-2">
            <Label htmlFor="scan-interval">{tr("dirScanTab.directoryDialog.scanIntervalLabel")}</Label>
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
              {tr("dirScanTab.directoryDialog.scanIntervalHelp")}
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
            <Label htmlFor="dir-enabled">{tr("dirScanTab.values.enabled")}</Label>
          </div>

          {/* Regular mode warning */}
          {isRegularMode && (
            <div className="rounded-lg border border-yellow-500/50 bg-yellow-500/5 p-4 space-y-3">
              <div className="flex items-start gap-3">
                <AlertTriangle className="size-5 text-yellow-500 shrink-0 mt-0.5" />
                <div className="space-y-2">
                  <p className="text-sm font-medium text-foreground">
                    {tr("dirScanTab.directoryDialog.regularMode.title")}
                  </p>
                  <p className="text-sm text-muted-foreground">
                    <Trans
                      i18nKey="dirScanTab.directoryDialog.regularMode.bodyOne"
                      ns="common"
                      components={{
                        emphasis: <span className="font-medium" />,
                      }}
                      values={{
                        emphasis: tr("dirScanTab.directoryDialog.regularMode.bodyOneEmphasis"),
                      }}
                    />
                  </p>
                  <p className="text-sm text-muted-foreground">
                    <Trans
                      i18nKey="dirScanTab.directoryDialog.regularMode.bodyTwo"
                      ns="common"
                      components={{
                        hardlink: <span className="font-medium" />,
                        reflink: <span className="font-medium" />,
                      }}
                      values={{
                        hardlink: tr("dirScanTab.directoryDialog.regularMode.hardlinkLabel"),
                        reflink: tr("dirScanTab.directoryDialog.regularMode.reflinkLabel"),
                      }}
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
                  {tr("dirScanTab.directoryDialog.regularMode.acknowledge")}
                </Label>
              </div>
            </div>
          )}
        </div>

        <DialogFooter className="flex-shrink-0">
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {tr("dirScanTab.shared.cancel")}
          </Button>
          <Button
            onClick={handleSave}
            disabled={isPending || !form.path || !form.targetInstanceId || (isRegularMode && !regularModeAcknowledged)}
          >
            {isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {isEditing ? tr("dirScanTab.shared.save") : tr("dirScanTab.shared.create")}
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
  const tr = useCommonTr()
  const deleteDirectory = useDeleteDirScanDirectory()

  const handleDelete = useCallback(() => {
    if (!directoryId) return
    deleteDirectory.mutate(directoryId, {
      onSuccess: () => {
        toast.success(tr("dirScanTab.toasts.directoryDeleted"))
        onOpenChange(false)
      },
      onError: (error) => {
        toast.error(tr("dirScanTab.toasts.deleteDirectoryFailed", { message: error.message }))
      },
    })
  }, [deleteDirectory, directoryId, onOpenChange, tr])

  return (
    <AlertDialog open={directoryId !== null} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{tr("dirScanTab.deleteDialog.title")}</AlertDialogTitle>
          <AlertDialogDescription>
            {tr("dirScanTab.deleteDialog.description")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{tr("dirScanTab.shared.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            onClick={handleDelete}
            className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
          >
            {deleteDirectory.isPending && <Loader2 className="size-4 mr-2 animate-spin" />}
            {tr("dirScanTab.shared.delete")}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}

// Helper function
function formatDuration(
  ms: number,
  tr: (key: string, options?: Record<string, unknown>) => string
): string {
  if (ms < 1000) return tr("dirScanTab.duration.lt1s")
  const seconds = Math.floor(ms / 1000)
  if (seconds < 60) return tr("dirScanTab.duration.seconds", { seconds })
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  if (minutes < 60) {
    return tr("dirScanTab.duration.minutesSeconds", { minutes, seconds: remainingSeconds })
  }
  const hours = Math.floor(minutes / 60)
  const remainingMinutes = minutes % 60
  return tr("dirScanTab.duration.hoursMinutes", { hours, minutes: remainingMinutes })
}
